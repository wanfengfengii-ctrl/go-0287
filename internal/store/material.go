package store

import (
	"context"
	"database/sql"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/lease"
)

// LedgerEntry is one material ledger movement (open, issue, recover, waste).
type LedgerEntry struct {
	BatchID     string             `json:"batch_id"`
	Seq         int64              `json:"seq"`
	OperationID string             `json:"operation_id"`
	Kind        string             `json:"kind"`
	Grams       int64              `json:"grams"`
	LogicalTime lease.LogicalClock `json:"logical_time"`
}

// PutBatch inserts or updates a material batch row inside a transaction.
func PutBatch(tx *sql.Tx, taskID string, b lease.MaterialBatch) error {
	_, err := tx.Exec(`
		INSERT INTO material_batches(task_id, batch_id, proof_summary, expired, opened_grams, topped_up_grams,
			issued_grams, recovered_grams, approved_waste)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(task_id, batch_id) DO UPDATE SET
			proof_summary=excluded.proof_summary, expired=excluded.expired,
			opened_grams=excluded.opened_grams, topped_up_grams=excluded.topped_up_grams,
			issued_grams=excluded.issued_grams, recovered_grams=excluded.recovered_grams,
			approved_waste=excluded.approved_waste`,
		taskID, b.ID, b.ProofSummary, boolToInt(b.Expired), b.OpenedGrams, b.ToppedUpGrams,
		b.IssuedGrams, b.RecoveredGrams, b.ApprovedWasteGrams)
	return err
}

// GetBatch loads a material batch, returning CodeNotFound when absent.
func GetBatch(tx *sql.Tx, taskID, batchID string) (*lease.MaterialBatch, error) {
	var b lease.MaterialBatch
	var expired int
	err := tx.QueryRow(`
		SELECT batch_id, proof_summary, expired, opened_grams, topped_up_grams, issued_grams, recovered_grams, approved_waste
		FROM material_batches WHERE task_id = ? AND batch_id = ?`, taskID, batchID).
		Scan(&b.ID, &b.ProofSummary, &expired, &b.OpenedGrams, &b.ToppedUpGrams, &b.IssuedGrams, &b.RecoveredGrams, &b.ApprovedWasteGrams)
	if err == sql.ErrNoRows {
		return nil, errs.New(errs.CodeNotFound, "material batch not found")
	}
	if err != nil {
		return nil, err
	}
	b.Expired = expired != 0
	return &b, nil
}

// ListBatches returns every material batch for a task in stable id order.
func (s *Store) ListBatches(ctx context.Context, taskID string) ([]lease.MaterialBatch, error) {
	rows, err := s.db.Query(`
		SELECT batch_id, proof_summary, expired, opened_grams, topped_up_grams, issued_grams, recovered_grams, approved_waste
		FROM material_batches WHERE task_id = ? ORDER BY batch_id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []lease.MaterialBatch
	for rows.Next() {
		var b lease.MaterialBatch
		var expired int
		if err := rows.Scan(&b.ID, &b.ProofSummary, &expired, &b.OpenedGrams, &b.ToppedUpGrams, &b.IssuedGrams, &b.RecoveredGrams, &b.ApprovedWasteGrams); err != nil {
			return nil, err
		}
		b.Expired = expired != 0
		out = append(out, b)
	}
	return out, rows.Err()
}

// appendLedger writes a material ledger entry with an incrementing sequence.
func AppendLedger(tx *sql.Tx, taskID, batchID string, e LedgerEntry) error {
	var seq int64
	if err := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM material_ledger WHERE task_id = ? AND batch_id = ?`, taskID, batchID).Scan(&seq); err != nil {
		return err
	}
	e.Seq = seq + 1
	_, err := tx.Exec(`
		INSERT INTO material_ledger(task_id, batch_id, seq, operation_id, kind, grams, logical_time)
		VALUES(?, ?, ?, ?, ?, ?, ?)`,
		taskID, batchID, e.Seq, e.OperationID, e.Kind, e.Grams, int64(e.LogicalTime))
	return err
}

// Ledger returns the ordered ledger for a batch.
func (s *Store) Ledger(ctx context.Context, taskID, batchID string) ([]LedgerEntry, error) {
	rows, err := s.db.Query(`
		SELECT batch_id, seq, operation_id, kind, grams, logical_time
		FROM material_ledger WHERE task_id = ? AND batch_id = ? ORDER BY seq`, taskID, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LedgerEntry
	for rows.Next() {
		var e LedgerEntry
		if err := rows.Scan(&e.BatchID, &e.Seq, &e.OperationID, &e.Kind, &e.Grams, &e.LogicalTime); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
