package store

import (
	"context"
	"database/sql"

	"example.com/steel-fireproofing-evidence-closure/internal/task"
)

// InsertCoat persists a coat application; the primary key enforces that a
// (face, coat_index, generation) may be recorded at most once.
func InsertCoat(tx *sql.Tx, taskID string, c task.CoatApplication) error {
	_, err := tx.Exec(`
		INSERT INTO coats(task_id, face_id, coat_index, generation, spray_zone_id, material_batch_id, area_mm2, issued_grams, recovered_grams, approved_waste, logical_time)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		taskID, c.FaceID, c.CoatIndex, int64(c.Generation), c.SprayZoneID, c.MaterialBatchID,
		c.AreaMM2, c.IssuedGrams, c.RecoveredGrams, c.ApprovedWaste, int64(c.LogicalTime))
	return err
}

// CoatPrefixTx loads the highest contiguous completed coat index per face for
// a generation by replaying the stored coat rows inside a transaction.
func CoatPrefixTx(tx *sql.Tx, taskID string, gen task.Generation) (*task.CoatPrefix, error) {
	rows, err := tx.Query(`SELECT face_id, coat_index FROM coats WHERE task_id = ? AND generation = ?`, taskID, int64(gen))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cp := task.NewCoatPrefix()
	for rows.Next() {
		var face string
		var idx int
		if err := rows.Scan(&face, &idx); err != nil {
			return nil, err
		}
		if cp.Completed(face) < idx {
			cp.Record(face, idx)
		}
	}
	return cp, rows.Err()
}

// CoatPrefix is the non-transactional convenience form of CoatPrefixTx.
func (s *Store) CoatPrefix(ctx context.Context, taskID string, gen task.Generation) (*task.CoatPrefix, error) {
	var cp *task.CoatPrefix
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		cp, err = CoatPrefixTx(tx, taskID, gen)
		return err
	})
	return cp, err
}

// CoatsTx returns every coat application for a task generation inside a
// transaction.
func CoatsTx(tx *sql.Tx, taskID string, gen task.Generation) ([]task.CoatApplication, error) {
	rows, err := tx.Query(`
		SELECT face_id, coat_index, generation, spray_zone_id, material_batch_id, area_mm2, issued_grams, recovered_grams, approved_waste, logical_time
		FROM coats WHERE task_id = ? AND generation = ? ORDER BY face_id, coat_index`, taskID, int64(gen))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []task.CoatApplication
	for rows.Next() {
		var c task.CoatApplication
		if err := rows.Scan(&c.FaceID, &c.CoatIndex, &c.Generation, &c.SprayZoneID, &c.MaterialBatchID, &c.AreaMM2, &c.IssuedGrams, &c.RecoveredGrams, &c.ApprovedWaste, &c.LogicalTime); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Coats is the non-transactional convenience form of CoatsTx.
func (s *Store) Coats(ctx context.Context, taskID string, gen task.Generation) ([]task.CoatApplication, error) {
	var out []task.CoatApplication
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = CoatsTx(tx, taskID, gen)
		return err
	})
	return out, err
}

// InsertDryFilm persists a dry-film reading.
func InsertDryFilm(tx *sql.Tx, taskID string, r task.DryFilmReading) error {
	_, err := tx.Exec(`INSERT INTO dry_film(task_id, face_id, point_id, generation, value_um) VALUES(?, ?, ?, ?, ?)`,
		taskID, r.FaceID, r.PointID, int64(r.Generation), r.ValueUM)
	return err
}

// DryFilm returns dry-film readings for a point within a generation.
func (s *Store) DryFilm(ctx context.Context, taskID, faceID, pointID string, gen task.Generation) ([]int64, error) {
	rows, err := s.db.Query(`SELECT value_um FROM dry_film WHERE task_id = ? AND face_id = ? AND point_id = ? AND generation = ? ORDER BY value_um`,
		taskID, faceID, pointID, int64(gen))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// InsertBond persists a bond strength result.
func InsertBond(tx *sql.Tx, taskID string, r task.BondSpecimenResult) error {
	_, err := tx.Exec(`INSERT INTO bond(task_id, specimen_id, generation, value_kpa) VALUES(?, ?, ?, ?)`,
		taskID, r.SpecimenID, int64(r.Generation), r.ValueKPa)
	return err
}

// Bond returns bond results for a generation.
func (s *Store) Bond(ctx context.Context, taskID string, gen task.Generation) ([]task.BondSpecimenResult, error) {
	rows, err := s.db.Query(`SELECT specimen_id, value_kpa FROM bond WHERE task_id = ? AND generation = ? ORDER BY specimen_id`, taskID, int64(gen))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []task.BondSpecimenResult
	for rows.Next() {
		var r task.BondSpecimenResult
		r.Generation = gen
		if err := rows.Scan(&r.SpecimenID, &r.ValueKPa); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertCuring persists a curing sample.
func InsertCuring(tx *sql.Tx, taskID string, c task.CuringSample) error {
	_, err := tx.Exec(`INSERT INTO curing(task_id, face_id, coat_index, temp_c, humidity_rh, logical_time) VALUES(?, ?, ?, ?, ?, ?)`,
		taskID, c.FaceID, c.CoatIndex, c.TempC, c.HumidityRH, int64(c.LogicalTime))
	return err
}

// Curing returns curing samples for a face/coat ordered by logical time.
func (s *Store) Curing(ctx context.Context, taskID, faceID string, coatIndex int) ([]task.CuringSample, error) {
	rows, err := s.db.Query(`SELECT face_id, coat_index, temp_c, humidity_rh, logical_time FROM curing WHERE task_id = ? AND face_id = ? AND coat_index = ? ORDER BY logical_time`,
		taskID, faceID, coatIndex)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []task.CuringSample
	for rows.Next() {
		var c task.CuringSample
		if err := rows.Scan(&c.FaceID, &c.CoatIndex, &c.TempC, &c.HumidityRH, &c.LogicalTime); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
