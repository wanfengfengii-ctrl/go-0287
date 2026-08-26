package store

import (
	"context"
	"database/sql"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/lease"
)

// PutInvocation inserts or updates a device invocation row.
func PutInvocation(tx *sql.Tx, taskID string, d lease.DeviceInvocation) error {
	_, err := tx.Exec(`
		INSERT INTO invocations(task_id, id, operation_id, equipment_id, kind, logical_time, fault, attempt, next_retry_at, status, reading_ref)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(task_id, id) DO UPDATE SET
			fault=excluded.fault, attempt=excluded.attempt, next_retry_at=excluded.next_retry_at,
			status=excluded.status, reading_ref=excluded.reading_ref`,
		taskID, d.ID, d.OperationID, d.EquipmentID, string(d.Kind), int64(d.LogicalTime),
		string(d.Fault), d.Attempt, int64(d.NextRetryAt), string(d.Status), d.ReadingRef)
	return err
}

// GetInvocationTx loads a single device invocation inside a transaction.
func GetInvocationTx(tx *sql.Tx, taskID, id string) (*lease.DeviceInvocation, error) {
	var d lease.DeviceInvocation
	err := tx.QueryRow(`
		SELECT id, operation_id, equipment_id, kind, logical_time, fault, attempt, next_retry_at, status, reading_ref
		FROM invocations WHERE task_id = ? AND id = ?`, taskID, id).
		Scan(&d.ID, &d.OperationID, &d.EquipmentID, &d.Kind, &d.LogicalTime, &d.Fault, &d.Attempt, &d.NextRetryAt, &d.Status, &d.ReadingRef)
	if err == sql.ErrNoRows {
		return nil, errs.New(errs.CodeNotFound, "device invocation not found")
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// GetInvocation is the non-transactional convenience form of GetInvocationTx.
func (s *Store) GetInvocation(ctx context.Context, taskID, id string) (*lease.DeviceInvocation, error) {
	var d *lease.DeviceInvocation
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		d, err = GetInvocationTx(tx, taskID, id)
		return err
	})
	return d, err
}

// PendingInvocations returns invocations not yet succeeded, ordered by
// logical time, used by recovery and readiness.
func (s *Store) PendingInvocations(ctx context.Context, taskID string) ([]lease.DeviceInvocation, error) {
	rows, err := s.db.Query(`
		SELECT id, operation_id, equipment_id, kind, logical_time, fault, attempt, next_retry_at, status, reading_ref
		FROM invocations WHERE task_id = ? AND status != ? ORDER BY logical_time`,
		taskID, string(lease.InvocationSucceeded))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []lease.DeviceInvocation
	for rows.Next() {
		var d lease.DeviceInvocation
		if err := rows.Scan(&d.ID, &d.OperationID, &d.EquipmentID, &d.Kind, &d.LogicalTime, &d.Fault, &d.Attempt, &d.NextRetryAt, &d.Status, &d.ReadingRef); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
