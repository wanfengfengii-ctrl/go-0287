package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/task"
)

// InsertReview persists an independent review.
func InsertReview(tx *sql.Tx, taskID string, r task.Review) error {
	classes, err := encode(r.Classes)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		INSERT INTO reviews(task_id, reviewer_id, generation, approved, classes_json) VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(task_id, reviewer_id, generation) DO UPDATE SET approved=excluded.approved, classes_json=excluded.classes_json`,
		taskID, r.ReviewerID, int64(r.Generation), boolToInt(r.Approved), classes)
	return err
}

// Reviews returns all reviews for a generation.
func (s *Store) Reviews(ctx context.Context, taskID string, gen task.Generation) ([]task.Review, error) {
	rows, err := s.db.Query(`SELECT reviewer_id, generation, approved, classes_json FROM reviews WHERE task_id = ? AND generation = ? ORDER BY reviewer_id`,
		taskID, int64(gen))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []task.Review
	for rows.Next() {
		var r task.Review
		var approved int
		var classesJSON string
		if err := rows.Scan(&r.ReviewerID, &r.Generation, &approved, &classesJSON); err != nil {
			return nil, err
		}
		r.Approved = approved != 0
		if err := json.Unmarshal([]byte(classesJSON), &r.Classes); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DecideTerminalTx performs the conditional terminal update inside a
// transaction. It succeeds only when the terminal state is still empty
// (single-writer barrier); otherwise it returns CodeTerminalAlreadyDecided.
func DecideTerminal(tx *sql.Tx, taskID string, d task.TerminalDecision, credentialID, evidenceRoot string) error {
	var current string
	if err := tx.QueryRow(`SELECT terminal_state FROM tasks WHERE id = ?`, taskID).Scan(&current); err != nil {
		if err == sql.ErrNoRows {
			return errs.New(errs.CodeNotFound, "task not found")
		}
		return err
	}
	if current != "" {
		return errs.New(errs.CodeTerminalAlreadyDecided, "terminal decision already recorded")
	}
	_, err := tx.Exec(`
		UPDATE tasks SET terminal_state = ?, terminal_decided_by = ?, terminal_generation = ?,
			credential_id = ?, evidence_root = ? WHERE id = ?`,
		string(d.State), d.DecidedBy, int64(d.Generation), credentialID, evidenceRoot, taskID)
	return err
}
