// Package store is the controller's persistence boundary.
//
// The v0 default implementation is SQLite (internal/store/sqlite); Postgres or
// Spanner can implement the same interface, which is also the HA path
// (DESIGN.md §7).
//
// INVARIANT: every secret-state mutation writes its audit row in the SAME
// transaction as the change. Callers mutate secret state only through Tx
// repository methods, so "forgot to audit" cannot compile. The audit log is
// hash-chained (tamper-evident), not merely insert-only.
package store

import "context"

// Store is the persistence interface backing the controller.
type Store interface {
	// Tx runs fn inside a single (serializable) transaction. fn returning a
	// non-nil error rolls back; nil commits.
	Tx(ctx context.Context, fn func(Tx) error) error

	// Migrate applies pending schema migrations idempotently.
	Migrate(ctx context.Context) error

	// Close releases the underlying handle.
	Close() error
}

// Tx is the transactional repository surface. Phase 0 establishes the seam and
// the audit invariant; typed methods (CreateSecret, CreateGrant, RevokeGrant,
// ListGrantsByAgent, ...) are added alongside their features in later phases.
type Tx interface {
	// AppendAudit writes a hash-chained, tamper-evident audit row.
	AppendAudit(ev AuditEvent) error
}

// AuditEvent is one append-only audit record (DESIGN.md §21).
type AuditEvent struct {
	Type     string         // e.g. "secret.created", "secret.grant.revoked"
	RunID    string         // optional
	GrantID  string         // optional
	SecretID string         // optional
	Actor    string         // optional
	Detail   map[string]any // optional structured detail (never secret values)
}
