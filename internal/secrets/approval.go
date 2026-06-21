package secrets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/traego/kube-claw/internal/store"
)

// DeliveryHash is the canonical hash of a secret's delivery spec. It binds a
// grant to an exact delivery (path/mode/env); changing any of them invalidates
// the grant and forces re-approval (DESIGN.md §6, §8).
func DeliveryHash(path, mode string, env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	fmt.Fprintf(h, "path=%s\nmode=%s\n", path, mode)
	for _, k := range keys {
		fmt.Fprintf(h, "env:%s=%s\n", k, env[k])
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// GrantBinding is what an approval binds a grant to (the agent's current state).
type GrantBinding struct {
	ImageDigest    string
	AgentSpecHash  string
	DeliveryHash   string
	ServiceAccount string
}

// ApproveRequest creates a durable grant from a pending request and marks it
// Approved, in one transaction. The binding comes from the agent's CURRENT state
// (computed by the caller), so you approve what is current (DESIGN.md §8).
func (s *Service) ApproveRequest(ctx context.Context, reqID, approver, reason string, b GrantBinding) (store.Grant, error) {
	var grant store.Grant
	err := s.Store.Tx(ctx, func(tx store.Tx) error {
		req, err := tx.GetSecretRequest(reqID)
		if err != nil {
			return err
		}
		if req.Status != "Pending" {
			return fmt.Errorf("request %s is %s, not Pending", reqID, req.Status)
		}
		grant = store.Grant{
			ID:             newID("grant"),
			AgentNamespace: req.AgentNamespace,
			AgentName:      req.AgentName,
			ServiceAccount: b.ServiceAccount,
			ImageDigest:    b.ImageDigest,
			AgentSpecHash:  b.AgentSpecHash,
			DeliveryHash:   b.DeliveryHash,
			SecretID:       req.SecretID,
			ApprovedBy:     approver,
			Reason:         reason,
		}
		if err := tx.CreateGrant(grant); err != nil {
			return err
		}
		if err := tx.SetSecretRequestStatus(reqID, "Approved"); err != nil {
			return err
		}
		return tx.AppendAudit(store.AuditEvent{
			Type: "secret.request.approved", SecretID: req.SecretID, GrantID: grant.ID,
			Actor: approver, Detail: map[string]any{"request": reqID, "reason": reason},
		})
	})
	return grant, err
}

// DenyRequest marks a request Denied.
func (s *Service) DenyRequest(ctx context.Context, reqID, approver, reason string) error {
	return s.Store.Tx(ctx, func(tx store.Tx) error {
		req, err := tx.GetSecretRequest(reqID)
		if err != nil {
			return err
		}
		if err := tx.SetSecretRequestStatus(reqID, "Denied"); err != nil {
			return err
		}
		return tx.AppendAudit(store.AuditEvent{
			Type: "secret.request.denied", SecretID: req.SecretID,
			Actor: approver, Detail: map[string]any{"request": reqID, "reason": reason},
		})
	})
}

// RevokeGrant revokes a grant and audits it.
func (s *Service) RevokeGrant(ctx context.Context, grantID, actor, reason string) error {
	return s.Store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.RevokeGrant(grantID, reason); err != nil {
			return err
		}
		return tx.AppendAudit(store.AuditEvent{
			Type: "secret.grant.revoked", GrantID: grantID, Actor: actor,
			Detail: map[string]any{"reason": reason},
		})
	})
}

// NewID exposes id generation for callers that build store rows (e.g. requests).
func NewID(prefix string) string { return newID(prefix) }
