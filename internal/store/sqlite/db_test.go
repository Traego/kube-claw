package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/traego/kube-claw/internal/store"
)

// TestOpenMigratePersist exercises Phase 0's acceptance: the store opens,
// migrates, writes an audit row in a tx, and the data survives a reopen
// (controller restart) — DESIGN.md §17 Phase 0 / §20.
func TestOpenMigratePersist(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "claw.db")

	s, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if err := s.Tx(ctx, func(tx store.Tx) error {
		return tx.AppendAudit(store.AuditEvent{Type: "secret.created"})
	}); err != nil {
		t.Fatalf("tx: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen: state must persist across "restart".
	s2, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if err := s2.Migrate(ctx); err != nil {
		t.Fatalf("migrate idempotent: %v", err)
	}

	var n int
	if err := s2.db.QueryRowContext(ctx, `SELECT count(*) FROM audit`).Scan(&n); err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if n != 1 {
		t.Fatalf("audit rows after reopen = %d, want 1", n)
	}
}

// TestTxRollback verifies a failing tx does not commit its audit row.
func TestTxRollback(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "claw.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	wantErr := context.Canceled
	err = s.Tx(ctx, func(tx store.Tx) error {
		if e := tx.AppendAudit(store.AuditEvent{Type: "secret.created"}); e != nil {
			return e
		}
		return wantErr // force rollback
	})
	if err != wantErr {
		t.Fatalf("tx err = %v, want %v", err, wantErr)
	}

	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM audit`).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 0 {
		t.Fatalf("audit rows after rollback = %d, want 0", n)
	}
}
