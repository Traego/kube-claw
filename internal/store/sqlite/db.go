// Package sqlite is the v0 default store.Store implementation.
//
// It is single-writer and tuned with WAL + busy_timeout. Those knobs live HERE,
// never in the store.Store interface, so the interface stays portable to
// Postgres/Spanner (DESIGN.md §7).
package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no cgo)

	"github.com/traego/kube-claw/internal/store"
)

// Store is the SQLite-backed store.Store.
type Store struct {
	db *sql.DB
}

var _ store.Store = (*Store)(nil)

// Open opens (creating if absent) the SQLite database at path and applies
// connection pragmas. Call Migrate before use.
//
// WAL gives concurrent readers alongside the single writer; busy_timeout avoids
// spurious SQLITE_BUSY under the reconciler + API + router touching the DB.
func Open(ctx context.Context, path string) (*Store, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)",
		path,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Single writer: cap the pool to one connection.
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// Tx runs fn inside a transaction, rolling back on error.
func (s *Store) Tx(ctx context.Context, fn func(store.Tx) error) error {
	sqlTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := fn(&tx{tx: sqlTx}); err != nil {
		_ = sqlTx.Rollback()
		return err
	}
	return sqlTx.Commit()
}

// tx implements store.Tx over a *sql.Tx.
type tx struct {
	tx *sql.Tx
}

// AppendAudit writes an audit row. Phase 0 records the type only; hash-chaining
// (prev_hash/row_hash) lands in Phase 2 alongside the full audit model.
func (t *tx) AppendAudit(ev store.AuditEvent) error {
	_, err := t.tx.Exec(`INSERT INTO audit (type) VALUES (?)`, ev.Type)
	if err != nil {
		return fmt.Errorf("append audit: %w", err)
	}
	return nil
}
