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

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned by read methods when the row does not exist.
var ErrNotFound = errors.New("store: not found")

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

// Tx is the transactional repository surface. Typed methods are added alongside
// their features; Phase 2 ships runs + hash-chained audit. Secret/grant/request
// methods land in Phases 3-4.
type Tx interface {
	// AppendAudit writes a hash-chained, tamper-evident audit row.
	AppendAudit(ev AuditEvent) error

	// CreateRun inserts a new run.
	CreateRun(r Run) error
	// GetRun returns a run by id, or ErrNotFound.
	GetRun(id string) (Run, error)
	// ListRuns returns the most recent runs, newest first.
	ListRuns(limit int) ([]Run, error)
}

// AuditEvent is one append-only audit record (DESIGN.md §21).
type AuditEvent struct {
	Type     string         // e.g. "secret.created", "agentrun.created"
	RunID    string         // optional
	GrantID  string         // optional
	SecretID string         // optional
	Actor    string         // optional
	Detail   map[string]any // optional structured detail (never secret values)
}

// Run is the unit of work and audit visibility (DESIGN.md §22). Source/Input are
// opaque JSON strings owned by the caller.
type Run struct {
	ID             string
	AgentNamespace string
	AgentName      string
	SessionID      string
	Phase          string // Pending|Blocked|Waking|Running|Succeeded|Failed|...
	Source         string // JSON
	Input          string // JSON
	AssignedPod    string
	PodUID         string
	CreatedAt      string
	StartedAt      string
	CompletedAt    string
}

// NowRFC3339 is the canonical timestamp format used for stored rows.
func NowRFC3339() string { return time.Now().UTC().Format(time.RFC3339Nano) }
