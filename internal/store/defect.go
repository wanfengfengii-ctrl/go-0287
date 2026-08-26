package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/task"
)

// InsertDefect persists a defect record.
func InsertDefect(tx *sql.Tx, taskID string, d task.Defect) error {
	_, err := tx.Exec(`
		INSERT INTO defects(task_id, generation, kind, point_id, face_id, member_id, material_batch_id)
		VALUES(?, ?, ?, ?, ?, ?, ?)`,
		taskID, int64(d.Generation), string(d.Kind), d.PointID, d.FaceID, d.MemberID, d.MaterialBatchID)
	return err
}

// DefectsTx returns defects for a generation inside a transaction.
func DefectsTx(tx *sql.Tx, taskID string, gen task.Generation) ([]task.Defect, error) {
	rows, err := tx.Query(`
		SELECT generation, kind, point_id, face_id, member_id, material_batch_id
		FROM defects WHERE task_id = ? AND generation = ? ORDER BY face_id, point_id, kind`, taskID, int64(gen))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []task.Defect
	for rows.Next() {
		var d task.Defect
		if err := rows.Scan(&d.Generation, &d.Kind, &d.PointID, &d.FaceID, &d.MemberID, &d.MaterialBatchID); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Defects is the non-transactional convenience form of DefectsTx.
func (s *Store) Defects(ctx context.Context, taskID string, gen task.Generation) ([]task.Defect, error) {
	var out []task.Defect
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = DefectsTx(tx, taskID, gen)
		return err
	})
	return out, err
}

// PutRecoatScope persists a recoat scope for a generation.
func PutRecoatScope(tx *sql.Tx, taskID string, sc task.RecoatScope) error {
	affected, err := encode(sc.AffectedPoints)
	if err != nil {
		return err
	}
	coats, err := encode(sc.CoatIndices)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		INSERT INTO recoat_scopes(task_id, generation, affected_json, coat_indices_json) VALUES(?, ?, ?, ?)
		ON CONFLICT(task_id, generation) DO UPDATE SET affected_json=excluded.affected_json, coat_indices_json=excluded.coat_indices_json`,
		taskID, int64(sc.Generation), affected, coats)
	return err
}

// RecoatScope loads the recoat scope for a generation.
func (s *Store) RecoatScope(ctx context.Context, taskID string, gen task.Generation) (*task.RecoatScope, error) {
	var affectedJSON, coatsJSON string
	err := s.db.QueryRow(`SELECT affected_json, coat_indices_json FROM recoat_scopes WHERE task_id = ? AND generation = ?`, taskID, int64(gen)).
		Scan(&affectedJSON, &coatsJSON)
	if err == sql.ErrNoRows {
		return nil, errs.New(errs.CodeNotFound, "recoat scope not found")
	}
	if err != nil {
		return nil, err
	}
	sc := &task.RecoatScope{Generation: gen}
	if err := json.Unmarshal([]byte(affectedJSON), &sc.AffectedPoints); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(coatsJSON), &sc.CoatIndices); err != nil {
		return nil, err
	}
	return sc, nil
}
