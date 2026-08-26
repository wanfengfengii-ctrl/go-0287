package store

import (
	"context"
	"database/sql"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/lease"
)

// AcquireLeaseTx grants an exclusive equipment lease inside a transaction,
// enforcing the "at most one active lease per equipment" invariant. It returns
// CodeLeaseBusy when an active lease already exists.
func AcquireLease(tx *sql.Tx, taskID, equipmentID, ownerID string, now, expiresAt lease.LogicalClock) error {
	var existingExpires int64
	err := tx.QueryRow(`SELECT expires_at FROM leases WHERE task_id = ? AND equipment_id = ?`, taskID, equipmentID).
		Scan(&existingExpires)
	if err == nil && existingExpires > int64(now) {
		return errs.New(errs.CodeLeaseBusy, "equipment lease is already held")
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	_, err = tx.Exec(`
		INSERT INTO leases(task_id, equipment_id, owner_id, expires_at) VALUES(?, ?, ?, ?)
		ON CONFLICT(task_id, equipment_id) DO UPDATE SET owner_id=excluded.owner_id, expires_at=excluded.expires_at`,
		taskID, equipmentID, ownerID, int64(expiresAt))
	return err
}

// CheckLeaseTx verifies an equipment lease is present and unexpired at now.
func CheckLease(tx *sql.Tx, taskID, equipmentID string, now lease.LogicalClock) (lease.Lease, error) {
	var l lease.Lease
	err := tx.QueryRow(`SELECT equipment_id, owner_id, expires_at FROM leases WHERE task_id = ? AND equipment_id = ?`, taskID, equipmentID).
		Scan(&l.EquipmentID, &l.OwnerID, &l.ExpiresAt)
	if err == sql.ErrNoRows {
		return l, errs.New(errs.CodeLeaseExpired, "equipment lease is expired or absent")
	}
	if err != nil {
		return l, err
	}
	if !l.Active(now) {
		return l, errs.New(errs.CodeLeaseExpired, "equipment lease is expired or absent")
	}
	return l, nil
}

// GetLease returns the current lease row for an equipment item, if any.
func (s *Store) GetLease(ctx context.Context, taskID, equipmentID string) (lease.Lease, error) {
	var l lease.Lease
	err := s.db.QueryRow(`SELECT equipment_id, owner_id, expires_at FROM leases WHERE task_id = ? AND equipment_id = ?`, taskID, equipmentID).
		Scan(&l.EquipmentID, &l.OwnerID, &l.ExpiresAt)
	if err == sql.ErrNoRows {
		return l, errs.New(errs.CodeLeaseExpired, "equipment lease is expired or absent")
	}
	return l, err
}
