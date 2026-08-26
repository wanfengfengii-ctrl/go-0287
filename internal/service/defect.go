package service

import (
	"context"
	"database/sql"
	"sort"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/lease"
	"example.com/steel-fireproofing-evidence-closure/internal/store"
	"example.com/steel-fireproofing-evidence-closure/internal/task"
)

// DefectCmd records a deterministic detection defect.
type DefectCmd struct {
	Kind            task.DefectKind `json:"kind"`
	PointID         string          `json:"point_id"`
	FaceID          string          `json:"face_id"`
	MaterialBatchID string          `json:"material_batch_id"`
}

// RecordDefect appends a defect to the current generation.
func (s *Service) RecordDefect(ctx context.Context, taskID, caller, opID string, requestJSON []byte, expectedVersion int64, gen task.Generation, clock lease.LogicalClock, c DefectCmd) (any, error) {
	return s.execute(ctx, taskID, caller, opID, requestJSON, expectedVersion, gen, clock, task.EvidenceDefect, c, func(tx *sql.Tx, t *store.StoredTask) (any, error) {
		memberID, ok := faceMember(t.Scope, c.FaceID)
		if !ok || !pointExists(t.Scope, c.FaceID, c.PointID) {
			return nil, errs.New(errs.CodeMemberMismatch, "defect point not in task scope")
		}
		if err := store.InsertDefect(tx, taskID, task.Defect{
			Kind: c.Kind, PointID: c.PointID, FaceID: c.FaceID, MemberID: memberID,
			MaterialBatchID: c.MaterialBatchID, Generation: gen,
		}); err != nil {
			return nil, err
		}
		return map[string]string{"point_id": c.PointID, "kind": string(c.Kind), "status": "defect_recorded"}, nil
	})
}

// EvaluateDefects computes the deterministic defect influence domain for a
// generation without mutating state.
func (s *Service) EvaluateDefects(ctx context.Context, taskID string, gen task.Generation) (*task.RecoatScope, error) {
	t, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	defects, err := s.store.Defects(ctx, taskID, gen)
	if err != nil {
		return nil, err
	}
	coats, err := s.store.Coats(ctx, taskID, gen)
	if err != nil {
		return nil, err
	}
	return buildRecoatScope(t, gen, defects, coats), nil
}

// CreateRecoatGeneration atomically advances the task to the next generation
// and persists the recoat scope. Concurrent or stale requests yield
// CodeGenerationConflict.
func (s *Service) CreateRecoatGeneration(ctx context.Context, taskID string, from task.Generation) (*task.RecoatScope, error) {
	var result *task.RecoatScope
	err := s.store.Transact(ctx, func(tx *sql.Tx) error {
		t, err := s.store.GetTaskTx(ctx, tx, taskID)
		if err != nil {
			return err
		}
		next, err := store.IncrementGeneration(tx, taskID, from)
		if err != nil {
			return err
		}
		defects, err := store.DefectsTx(tx, taskID, from)
		if err != nil {
			return err
		}
		coats, err := store.CoatsTx(tx, taskID, from)
		if err != nil {
			return err
		}
		sc := buildRecoatScope(t, from, defects, coats)
		sc.Generation = next
		if err := store.PutRecoatScope(tx, taskID, *sc); err != nil {
			return err
		}
		result = sc
		return nil
	})
	return result, err
}

// buildRecoatScope is the single canonical influence-domain algorithm shared
// by the preview (evaluate) and commit (recoat-generation) paths.
func buildRecoatScope(t *store.StoredTask, gen task.Generation, defects []task.Defect, coats []task.CoatApplication) *task.RecoatScope {
	if len(defects) == 0 {
		return &task.RecoatScope{AffectedPoints: []string{}, CoatIndices: coatRange(t.Scope.Plan.CoatCount), Generation: gen}
	}
	pointBatch := map[string]string{}
	for _, c := range coats {
		for _, m := range t.Scope.Members {
			for _, f := range m.Member.ExposedFaces {
				if f.ID != c.FaceID {
					continue
				}
				for _, gp := range f.GridPoints {
					pointBatch[gp.ID] = c.MaterialBatchID
				}
			}
		}
	}
	affected := map[string]bool{}
	for _, m := range t.Scope.Members {
		var memberDefects []task.Defect
		for _, d := range defects {
			if d.MemberID == m.Member.ID {
				memberDefects = append(memberDefects, d)
			}
		}
		if len(memberDefects) == 0 {
			continue
		}
		for _, p := range task.InfluenceDomain(m.Member, memberDefects, pointBatch) {
			affected[p] = true
		}
	}
	out := make([]string, 0, len(affected))
	for k := range affected {
		out = append(out, k)
	}
	sort.Strings(out)
	return &task.RecoatScope{AffectedPoints: out, CoatIndices: coatRange(t.Scope.Plan.CoatCount), Generation: gen}
}

func coatRange(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i + 1
	}
	return out
}
