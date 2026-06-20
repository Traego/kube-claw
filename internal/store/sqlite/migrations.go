package sqlite

import (
	"context"
	"fmt"
)

// migrations is the ordered list of schema statements. Phase 0 ships the
// schema_version + audit skeleton; tables for secrets/grants/runs/sessions/
// dedupe land in their respective phases (DESIGN.md §7).
var migrations = []string{
	`CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS audit (
		id        INTEGER PRIMARY KEY AUTOINCREMENT,
		ts        TEXT NOT NULL DEFAULT (datetime('now')),
		type      TEXT NOT NULL,
		run_id    TEXT,
		grant_id  TEXT,
		secret_id TEXT,
		actor     TEXT,
		detail    TEXT,
		prev_hash TEXT,
		row_hash  TEXT
	);`,
}

// Migrate applies pending migrations idempotently.
func (s *Store) Migrate(ctx context.Context) error {
	for i, stmt := range migrations {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migration %d: %w", i, err)
		}
	}
	return nil
}
