package service

import (
	"context"
	"database/sql"

	"example.com/steel-fireproofing-evidence-closure/internal/catalog"
	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/lease"
	"example.com/steel-fireproofing-evidence-closure/internal/store"
	"example.com/steel-fireproofing-evidence-closure/internal/task"
)

// SurfacePrepCmd records base-surface handover confirmation and treatment.
type SurfacePrepCmd struct {
	MemberID         string `json:"member_id"`
	TreatmentSummary string `json:"treatment_summary"`
}

// SurfacePrep records a member's base-surface confirmation.
func (s *Service) SurfacePrep(ctx context.Context, taskID, caller, opID string, requestJSON []byte, expectedVersion int64, gen task.Generation, clock lease.LogicalClock, c SurfacePrepCmd) (any, error) {
	return s.execute(ctx, taskID, caller, opID, requestJSON, expectedVersion, gen, clock, task.EvidenceSurfacePrep, c, func(tx *sql.Tx, t *store.StoredTask) (any, error) {
		if !memberExists(t.Scope, c.MemberID) {
			return nil, errs.New(errs.CodeMemberMismatch, "member not in task scope")
		}
		if err := store.SetMemberSurfacePrepared(tx, taskID, c.MemberID); err != nil {
			return nil, err
		}
		return map[string]string{"member_id": c.MemberID, "status": "surface_prepared"}, nil
	})
}

// PrimerCmd records a compatible primer application for a member.
type PrimerCmd struct {
	MemberID string `json:"member_id"`
}

// Primer records a member's compatible primer application.
func (s *Service) Primer(ctx context.Context, taskID, caller, opID string, requestJSON []byte, expectedVersion int64, gen task.Generation, clock lease.LogicalClock, c PrimerCmd) (any, error) {
	return s.execute(ctx, taskID, caller, opID, requestJSON, expectedVersion, gen, clock, task.EvidenceSurfacePrep, c, func(tx *sql.Tx, t *store.StoredTask) (any, error) {
		m, ok := findMember(t.Scope, c.MemberID)
		if !ok {
			return nil, errs.New(errs.CodeMemberMismatch, "member not in task scope")
		}
		if !t.Scope.Snapshot.PrimerCompatible(m.Member.PrimerBatchID, coatingOf(m.Member)) {
			return nil, errs.New(errs.CodePrimerIncompatible, "primer incompatible with coating")
		}
		if err := store.SetMemberPrimerApplied(tx, taskID, c.MemberID); err != nil {
			return nil, err
		}
		return map[string]string{"member_id": c.MemberID, "status": "primer_applied"}, nil
	})
}

// MaterialOpenCmd opens a coating batch with an initial integer-gram quantity.
type MaterialOpenCmd struct {
	BatchID      string `json:"batch_id"`
	OpenedGrams  int64  `json:"opened_grams"`
	ProofSummary string `json:"proof_summary"`
}

// MaterialOpen opens a material batch.
func (s *Service) MaterialOpen(ctx context.Context, taskID, caller, opID string, requestJSON []byte, expectedVersion int64, gen task.Generation, clock lease.LogicalClock, c MaterialOpenCmd) (any, error) {
	return s.execute(ctx, taskID, caller, opID, requestJSON, expectedVersion, gen, clock, task.EvidenceSurfacePrep, c, func(tx *sql.Tx, t *store.StoredTask) (any, error) {
		if c.OpenedGrams < 0 {
			return nil, errs.New(errs.CodeInvalidInput, "opened grams must be non-negative")
		}
		b := lease.MaterialBatch{ID: c.BatchID, ProofSummary: c.ProofSummary, OpenedGrams: c.OpenedGrams}
		if err := store.PutBatch(tx, taskID, b); err != nil {
			return nil, err
		}
		return map[string]any{"batch_id": c.BatchID, "balance": b.Balance()}, nil
	})
}

// MaterialIssueCmd issues coating grams and acquires a sprayer lease in one
// transaction.
type MaterialIssueCmd struct {
	BatchID     string `json:"batch_id"`
	Grams       int64  `json:"grams"`
	EquipmentID string `json:"equipment_id"`
	LeaseTicks  int64  `json:"lease_ticks"`
}

// MaterialIssue deducts grams and acquires an exclusive sprayer lease
// atomically. A failure rolls back both the deduction and the lease.
func (s *Service) MaterialIssue(ctx context.Context, taskID, caller, opID string, requestJSON []byte, expectedVersion int64, gen task.Generation, clock lease.LogicalClock, c MaterialIssueCmd) (any, error) {
	return s.execute(ctx, taskID, caller, opID, requestJSON, expectedVersion, gen, clock, task.EvidenceSurfacePrep, c, func(tx *sql.Tx, t *store.StoredTask) (any, error) {
		b, err := store.GetBatch(tx, taskID, c.BatchID)
		if err != nil {
			return nil, err
		}
		bal, err := b.Issue(c.Grams)
		if err != nil {
			return nil, err
		}
		if err := store.AcquireLease(tx, taskID, c.EquipmentID, taskID, clock, clock+lease.LogicalClock(c.LeaseTicks)); err != nil {
			return nil, err
		}
		if err := store.PutBatch(tx, taskID, *b); err != nil {
			return nil, err
		}
		if err := store.AppendLedger(tx, taskID, c.BatchID, store.LedgerEntry{
			BatchID: c.BatchID, OperationID: opID, Kind: "issue", Grams: c.Grams, LogicalTime: clock,
		}); err != nil {
			return nil, err
		}
		return map[string]any{"batch_id": c.BatchID, "balance": bal}, nil
	})
}

// WetFilmPoint is a per-point wet-film reading recorded with a coat.
type WetFilmPoint struct {
	PointID string `json:"point_id"`
	ValueUM int64  `json:"value_um"`
}

// CoatCmd records one completed coat on one face, enforcing the contiguous
// prefix and the base-surface/primer prerequisites for the first coat.
type CoatCmd struct {
	FaceID          string         `json:"face_id"`
	CoatIndex       int            `json:"coat_index"`
	SprayZoneID     string         `json:"spray_zone_id"`
	MaterialBatchID string         `json:"material_batch_id"`
	AreaMM2         int64          `json:"area_mm2"`
	IssuedGrams     int64          `json:"issued_grams"`
	RecoveredGrams  int64          `json:"recovered_grams"`
	ApprovedWaste   int64          `json:"approved_waste"`
	EquipmentID     string         `json:"equipment_id"`
	LeaseTicks      int64          `json:"lease_ticks"`
	WetFilm         []WetFilmPoint `json:"wet_film"`
}

// Coat records a coat application.
func (s *Service) Coat(ctx context.Context, taskID, caller, opID string, requestJSON []byte, expectedVersion int64, gen task.Generation, clock lease.LogicalClock, c CoatCmd) (any, error) {
	return s.execute(ctx, taskID, caller, opID, requestJSON, expectedVersion, gen, clock, task.EvidenceCoatApplication, c, func(tx *sql.Tx, t *store.StoredTask) (any, error) {
		memberID, ok := faceMember(t.Scope, c.FaceID)
		if !ok {
			return nil, errs.New(errs.CodeMemberMismatch, "face not in task scope")
		}
		cp, err := store.CoatPrefixTx(tx, taskID, gen)
		if err != nil {
			return nil, err
		}
		if c.CoatIndex == 1 {
			st, err := store.GetMemberState(tx, taskID, memberID)
			if err != nil {
				return nil, err
			}
			if !st.SurfacePrepared {
				return nil, errs.New(errs.CodeCoatOutOfOrder, "base surface not confirmed")
			}
			if !st.PrimerApplied {
				return nil, errs.New(errs.CodePrimerIncompatible, "compatible primer not applied")
			}
		}
		if err := cp.Record(c.FaceID, c.CoatIndex); err != nil {
			return nil, err
		}
		if err := store.InsertCoat(tx, taskID, task.CoatApplication{
			FaceID: c.FaceID, CoatIndex: c.CoatIndex, Generation: gen,
			SprayZoneID: c.SprayZoneID, MaterialBatchID: c.MaterialBatchID,
			AreaMM2: c.AreaMM2, IssuedGrams: c.IssuedGrams,
			RecoveredGrams: c.RecoveredGrams, ApprovedWaste: c.ApprovedWaste, LogicalTime: clock,
		}); err != nil {
			return nil, err
		}
		return map[string]any{"face_id": c.FaceID, "coat_index": c.CoatIndex, "prefix": cp.Completed(c.FaceID)}, nil
	})
}

// CuringCmd records one curing trajectory sample.
type CuringCmd struct {
	FaceID     string `json:"face_id"`
	CoatIndex  int    `json:"coat_index"`
	TempC      int64  `json:"temp_c_raw"`
	HumidityRH int64  `json:"humidity_rh_raw"`
}

// Curing records a curing sample.
func (s *Service) Curing(ctx context.Context, taskID, caller, opID string, requestJSON []byte, expectedVersion int64, gen task.Generation, clock lease.LogicalClock, c CuringCmd) (any, error) {
	return s.execute(ctx, taskID, caller, opID, requestJSON, expectedVersion, gen, clock, task.EvidenceCuringSample, c, func(tx *sql.Tx, t *store.StoredTask) (any, error) {
		if _, ok := faceMember(t.Scope, c.FaceID); !ok {
			return nil, errs.New(errs.CodeMemberMismatch, "face not in task scope")
		}
		if err := store.InsertCuring(tx, taskID, task.CuringSample{
			FaceID: c.FaceID, CoatIndex: c.CoatIndex, TempC: c.TempC, HumidityRH: c.HumidityRH, LogicalTime: clock,
		}); err != nil {
			return nil, err
		}
		return map[string]string{"face_id": c.FaceID, "status": "curing_sample_recorded"}, nil
	})
}

// DryFilmCmd records a dry-film reading at a point (already promoted from a
// successful device invocation by the caller).
type DryFilmCmd struct {
	FaceID  string `json:"face_id"`
	PointID string `json:"point_id"`
	ValueUM int64  `json:"value_um"`
}

// DryFilm records a dry-film reading.
func (s *Service) DryFilm(ctx context.Context, taskID, caller, opID string, requestJSON []byte, expectedVersion int64, gen task.Generation, clock lease.LogicalClock, c DryFilmCmd) (any, error) {
	return s.execute(ctx, taskID, caller, opID, requestJSON, expectedVersion, gen, clock, task.EvidenceDryFilm, c, func(tx *sql.Tx, t *store.StoredTask) (any, error) {
		if !pointExists(t.Scope, c.FaceID, c.PointID) {
			return nil, errs.New(errs.CodeMemberMismatch, "point not in task scope")
		}
		if c.ValueUM < 0 {
			return nil, errs.New(errs.CodeInvalidInput, "dry film must be non-negative")
		}
		if err := store.InsertDryFilm(tx, taskID, task.DryFilmReading{FaceID: c.FaceID, PointID: c.PointID, ValueUM: c.ValueUM, Generation: gen}); err != nil {
			return nil, err
		}
		return map[string]string{"point_id": c.PointID, "status": "dry_film_recorded"}, nil
	})
}

// BondCmd records a bond pull-off strength result.
type BondCmd struct {
	SpecimenID string `json:"specimen_id"`
	ValueKPa   int64  `json:"value_kpa"`
}

// Bond records a bond strength result.
func (s *Service) Bond(ctx context.Context, taskID, caller, opID string, requestJSON []byte, expectedVersion int64, gen task.Generation, clock lease.LogicalClock, c BondCmd) (any, error) {
	return s.execute(ctx, taskID, caller, opID, requestJSON, expectedVersion, gen, clock, task.EvidenceBondResult, c, func(tx *sql.Tx, t *store.StoredTask) (any, error) {
		if c.ValueKPa < 0 {
			return nil, errs.New(errs.CodeInvalidInput, "bond strength must be non-negative")
		}
		if err := store.InsertBond(tx, taskID, task.BondSpecimenResult{SpecimenID: c.SpecimenID, ValueKPa: c.ValueKPa, Generation: gen}); err != nil {
			return nil, err
		}
		return map[string]string{"specimen_id": c.SpecimenID, "status": "bond_recorded"}, nil
	})
}

// helpers

func memberExists(sc store.Scope, memberID string) bool {
	_, ok := findMember(sc, memberID)
	return ok
}

func findMember(sc store.Scope, memberID string) (catalog.LockedMember, bool) {
	for _, m := range sc.Members {
		if m.Member.ID == memberID {
			return m, true
		}
	}
	return catalog.LockedMember{}, false
}

func faceMember(sc store.Scope, faceID string) (string, bool) {
	for _, m := range sc.Members {
		for _, f := range m.Member.ExposedFaces {
			if f.ID == faceID {
				return m.Member.ID, true
			}
		}
	}
	return "", false
}

func pointExists(sc store.Scope, faceID, pointID string) bool {
	for _, m := range sc.Members {
		for _, f := range m.Member.ExposedFaces {
			if f.ID != faceID {
				continue
			}
			for _, gp := range f.GridPoints {
				if gp.ID == pointID {
					return true
				}
			}
		}
	}
	return false
}

func coatingOf(m catalog.Member) string {
	if len(m.MaterialBatchIDs) == 0 {
		return ""
	}
	return m.MaterialBatchIDs[0]
}
