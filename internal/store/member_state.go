package store

import (
	"database/sql"
)

// MemberState captures the base-surface and primer readiness of a member.
type MemberState struct {
	SurfacePrepared bool `json:"surface_prepared"`
	PrimerApplied   bool `json:"primer_applied"`
}

// GetMemberState loads a member's readiness, defaulting to unprepared.
func GetMemberState(tx *sql.Tx, taskID, memberID string) (MemberState, error) {
	var sp, pa int
	err := tx.QueryRow(`SELECT surface_prepared, primer_applied FROM member_state WHERE task_id = ? AND member_id = ?`, taskID, memberID).
		Scan(&sp, &pa)
	if err == sql.ErrNoRows {
		return MemberState{}, nil
	}
	if err != nil {
		return MemberState{}, err
	}
	return MemberState{SurfacePrepared: sp != 0, PrimerApplied: pa != 0}, nil
}

// SetMemberSurfacePrepared marks a member's base surface as confirmed.
func SetMemberSurfacePrepared(tx *sql.Tx, taskID, memberID string) error {
	_, err := tx.Exec(`
		INSERT INTO member_state(task_id, member_id, surface_prepared, primer_applied) VALUES(?, ?, 1, 0)
		ON CONFLICT(task_id, member_id) DO UPDATE SET surface_prepared = 1`, taskID, memberID)
	return err
}

// SetMemberPrimerApplied marks a member's compatible primer as applied.
func SetMemberPrimerApplied(tx *sql.Tx, taskID, memberID string) error {
	_, err := tx.Exec(`
		INSERT INTO member_state(task_id, member_id, surface_prepared, primer_applied) VALUES(?, ?, 0, 1)
		ON CONFLICT(task_id, member_id) DO UPDATE SET primer_applied = 1`, taskID, memberID)
	return err
}
