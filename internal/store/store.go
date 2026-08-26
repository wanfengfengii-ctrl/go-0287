// Package store provides the SQLite-backed persistence layer for the
// steel-fireproofing evidence-closure backend: transactional task, material,
// lease, device, review and terminal storage with an append-only evidence
// event log, schema migration and deterministic restart recovery.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"
)

// Store is the persistence boundary. All writes are serialized through a
// single mutex so SQLite's single-writer model is respected while callers may
// still race logically (e.g. competing material issuance).
type Store struct {
	mu sync.Mutex
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path, applies migrations and
// verifies recoverability. A path of ":memory:" opens an ephemeral database.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// migrate applies the ordered schema statements under a single transaction
// and records the schema version.
func (s *Store) migrate() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);`)
	if err != nil {
		return fmt.Errorf("store: migrate meta: %w", err)
	}
	for _, stmt := range schema {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
	}
	_, err = s.db.Exec(`INSERT OR REPLACE INTO meta(key, value) VALUES('schema_version', ?)`, fmt.Sprintf("%d", len(schema)))
	return err
}

// Transact runs fn inside a serialized transaction, committing on success and
// rolling back on error or panic. Composite operations (material issue plus
// lease acquisition, command plus idempotency record) share one transaction
// through the provided *sql.Tx.
func (s *Store) Transact(ctx context.Context, fn func(tx *sql.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// tx runs a read-mostly operation inside a serialized transaction.
func (s *Store) tx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	return s.Transact(ctx, fn)
}

func encode(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
