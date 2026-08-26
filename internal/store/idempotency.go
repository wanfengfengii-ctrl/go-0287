package store

import (
	"context"
	"database/sql"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/task"
)

// IdempotencyStatus is the lifecycle of an idempotency record.
type IdempotencyStatus string

const (
	IdempotencyProcessing IdempotencyStatus = "processing"
	IdempotencyCommitted  IdempotencyStatus = "committed"
)

// IdempotencyRecord maps (caller, operation_id) to a canonical request digest
// and stored response, allowing replays to be answered deterministically.
type IdempotencyRecord struct {
	Caller        string            `json:"caller"`
	OperationID   string            `json:"operation_id"`
	RequestDigest string            `json:"request_digest"`
	Status        IdempotencyStatus `json:"status"`
	ResponseJSON  string            `json:"response_json"`
}

// DigestRequest returns the canonical digest used to compare a replay's
// normalized payload against the stored request.
func DigestRequest(payload string) string { return task.Digest([]byte(payload)) }

// GetIdempotencyTx loads an idempotency record inside a transaction.
func GetIdempotency(tx *sql.Tx, caller, operationID string) (*IdempotencyRecord, error) {
	var r IdempotencyRecord
	err := tx.QueryRow(`SELECT caller, operation_id, request_digest, status, response_json FROM idempotency WHERE caller = ? AND operation_id = ?`,
		caller, operationID).Scan(&r.Caller, &r.OperationID, &r.RequestDigest, &r.Status, &r.ResponseJSON)
	if err == sql.ErrNoRows {
		return nil, errs.New(errs.CodeNotFound, "idempotency record not found")
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// PutIdempotencyTx creates a processing idempotency record. If one already
// exists with a different digest it returns CodeIdempotencyConflict; with the
// same digest it is a no-op.
func PutIdempotency(tx *sql.Tx, caller, operationID, digest, responseJSON string) error {
	existing, err := GetIdempotency(tx, caller, operationID)
	if err != nil {
		if de, ok := err.(*errs.Error); ok && de.Code == errs.CodeNotFound {
			_, ierr := tx.Exec(`
				INSERT INTO idempotency(caller, operation_id, request_digest, status, response_json) VALUES(?, ?, ?, ?, ?)`,
				caller, operationID, digest, string(IdempotencyCommitted), responseJSON)
			return ierr
		}
		return err
	}
	if existing.RequestDigest != digest {
		return errs.New(errs.CodeIdempotencyConflict, "operation id reused with different content")
	}
	return nil
}

// PendingIdempotency returns records still in processing state, used by
// readiness to detect uncommitted transactions after a crash.
func (s *Store) PendingIdempotency(ctx context.Context) ([]IdempotencyRecord, error) {
	rows, err := s.db.Query(`SELECT caller, operation_id, request_digest, status, response_json FROM idempotency WHERE status = ?`,
		string(IdempotencyProcessing))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IdempotencyRecord
	for rows.Next() {
		var r IdempotencyRecord
		if err := rows.Scan(&r.Caller, &r.OperationID, &r.RequestDigest, &r.Status, &r.ResponseJSON); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
