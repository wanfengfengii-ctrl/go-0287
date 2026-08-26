package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"example.com/steel-fireproofing-evidence-closure/internal/catalog"
	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/task"
)

// Scope is the immutable locked scope of a task, persisted as JSON.
type Scope struct {
	Floor       string                  `json:"floor"`
	Compartment string                  `json:"compartment"`
	Members     []catalog.LockedMember  `json:"members"`
	Snapshot    catalog.CatalogSnapshot `json:"snapshot"`
	Plan        catalog.CoatPlan        `json:"plan"`
	Specimens   []catalog.Specimen      `json:"specimens"`
}

// StoredTask is the materialized projection of a task reconstructed from the
// tasks table and its children.
type StoredTask struct {
	ID                string                 `json:"id"`
	Scope             Scope                  `json:"scope"`
	CurrentGeneration task.Generation        `json:"current_generation"`
	AggregateVersion  int64                  `json:"aggregate_version"`
	LogicalClock      int64                  `json:"logical_clock"`
	Terminal          *task.TerminalDecision `json:"terminal,omitempty"`
	EvidenceRoot      string                 `json:"evidence_root"`
	CredentialID      string                 `json:"credential_id,omitempty"`
}

// LockTask atomically validates-then-persists a new locked task. It writes the
// scope, initializes generation 1 and appends the lock event. Any failure
// rolls back, leaving no task residue.
func (s *Store) LockTask(ctx context.Context, id string, sc Scope, locked []catalog.LockedMember, gen task.Generation) (*StoredTask, error) {
	sc.Members = locked
	var out *StoredTask
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM tasks WHERE id = ?`, id).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			return errs.New(errs.CodeInvalidInput, "task already locked")
		}
		scopeJSON, err := encode(sc)
		if err != nil {
			return err
		}
		planJSON, err := encode(sc.Plan)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`
			INSERT INTO tasks(id, floor, compartment, scope_json, plan_json, current_generation,
				aggregate_version, logical_clock, locked_at)
			VALUES(?, ?, ?, ?, ?, ?, 1, 0, ?)`,
			id, sc.Floor, sc.Compartment, scopeJSON, planJSON, int64(gen), nowUnix()); err != nil {
			return err
		}
		out, err = s.GetTaskTx(ctx, tx, id)
		return err
	})
	return out, err
}

// GetTask loads a task projection or returns CodeNotFound.
func (s *Store) GetTask(ctx context.Context, id string) (*StoredTask, error) {
	var out *StoredTask
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = s.GetTaskTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Store) GetTaskTx(ctx context.Context, tx *sql.Tx, id string) (*StoredTask, error) {
	var scopeJSON, planJSON, terminalState, decidedBy, credentialID, evidenceRoot string
	var gen, version, clock, terminalGen, lockedAt int64
	err := tx.QueryRow(`
		SELECT scope_json, plan_json, current_generation, aggregate_version, logical_clock,
			terminal_state, terminal_decided_by, terminal_generation, evidence_root, credential_id, locked_at
		FROM tasks WHERE id = ?`, id).
		Scan(&scopeJSON, &planJSON, &gen, &version, &clock, &terminalState, &decidedBy, &terminalGen, &evidenceRoot, &credentialID, &lockedAt)
	if err == sql.ErrNoRows {
		return nil, errs.New(errs.CodeNotFound, "task not found")
	}
	if err != nil {
		return nil, err
	}
	var sc Scope
	if err := json.Unmarshal([]byte(scopeJSON), &sc); err != nil {
		return nil, err
	}
	st := &StoredTask{
		ID:                id,
		Scope:             sc,
		CurrentGeneration: task.Generation(gen),
		AggregateVersion:  version,
		LogicalClock:      clock,
		EvidenceRoot:      evidenceRoot,
		CredentialID:      credentialID,
	}
	if terminalState != "" {
		st.Terminal = &task.TerminalDecision{
			State:      task.TerminalState(terminalState),
			DecidedBy:  decidedBy,
			Generation: task.Generation(terminalGen),
		}
	}
	return st, nil
}

// AdvanceClock advances a task's logical clock to at least the given logical
// time and increments its aggregate version, within the provided transaction.
func AdvanceClock(tx *sql.Tx, id string, logicalTime int64) (int64, int64, error) {
	var clock, version int64
	if err := tx.QueryRow(`SELECT logical_clock, aggregate_version FROM tasks WHERE id = ?`, id).Scan(&clock, &version); err != nil {
		return 0, 0, err
	}
	newClock := clock
	if logicalTime > newClock {
		newClock = logicalTime
	}
	newVersion := version + 1
	if _, err := tx.Exec(`UPDATE tasks SET logical_clock = ?, aggregate_version = ? WHERE id = ?`, newClock, newVersion, id); err != nil {
		return 0, 0, err
	}
	return newClock, newVersion, nil
}

// nowUnix returns the current wall-clock time in seconds, used only for the
// informational locked_at column and never for logical ordering.
func nowUnix() int64 { return time.Now().Unix() }
