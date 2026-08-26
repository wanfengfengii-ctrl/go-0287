// Package service is the application layer that orchestrates the domain
// packages (catalog, task, lease) over the persistent store. It exposes the
// complete command surface: task locking, material and lease management,
// coat application, device invocation, defect/recoat arbitration, review and
// terminal decisions, with idempotency and version precondition enforcement.
package service

import (
	"context"
	"database/sql"
	"encoding/json"

	"example.com/steel-fireproofing-evidence-closure/internal/catalog"
	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/lease"
	"example.com/steel-fireproofing-evidence-closure/internal/store"
	"example.com/steel-fireproofing-evidence-closure/internal/task"
)

// Service coordinates domain logic and persistence.
type Service struct {
	store *store.Store
}

// New returns a service backed by the given store.
func New(s *store.Store) *Service { return &Service{store: s} }

// Store exposes the underlying store for readiness and smoke inspection.
func (s *Service) Store() *store.Store { return s.store }

// LockRequest is the public lock payload.
type LockRequest struct {
	TaskID          string                     `json:"task_id"`
	Floor           string                     `json:"floor"`
	FireCompartment string                     `json:"fire_compartment"`
	CatalogVersion  int64                      `json:"catalog_version"`
	RuleSummary     string                     `json:"rule_summary"`
	MaterialProof   string                     `json:"material_proof_summary"`
	Calibration     string                     `json:"equipment_calibration_summary"`
	PrimerMatrix    map[string]map[string]bool `json:"primer_compatibility"`
	Members         []catalog.Member           `json:"members"`
	Plan            catalog.CoatPlan           `json:"coat_plan"`
	Specimens       []catalog.Specimen         `json:"specimens"`
}

// LockResult is the response of a successful lock.
type LockResult struct {
	TaskID           string                 `json:"task_id"`
	CatalogVersion   int64                  `json:"catalog_version"`
	RuleSummary      string                 `json:"rule_summary"`
	Generation       task.Generation        `json:"generation"`
	AggregateVersion int64                  `json:"aggregate_version"`
	Members          []catalog.LockedMember `json:"members"`
}

// Lock validates and atomically locks a task. On any validation failure the
// store is left with no task residue.
func (s *Service) Lock(ctx context.Context, req LockRequest) (*LockResult, error) {
	snapshot := catalog.CatalogSnapshot{
		Version:                     req.CatalogVersion,
		RuleSummary:                 req.RuleSummary,
		MaterialProofSummary:        req.MaterialProof,
		PrimerCompatibility:         req.PrimerMatrix,
		EquipmentCalibrationSummary: req.Calibration,
		ThicknessRuleVersion:        1,
	}
	locked, err := catalog.ValidateLockScope(req.Floor, req.FireCompartment, req.Members, snapshot, req.Plan)
	if err != nil {
		return nil, err
	}
	sc := store.Scope{
		Floor:       req.Floor,
		Compartment: req.FireCompartment,
		Snapshot:    snapshot,
		Plan:        req.Plan,
		Specimens:   req.Specimens,
	}
	st, err := s.store.LockTask(ctx, req.TaskID, sc, locked, task.Generation(1))
	if err != nil {
		return nil, err
	}
	return &LockResult{
		TaskID:           st.ID,
		CatalogVersion:   snapshot.Version,
		RuleSummary:      snapshot.RuleSummary,
		Generation:       st.CurrentGeneration,
		AggregateVersion: st.AggregateVersion,
		Members:          locked,
	}, nil
}

// commandHandler runs a command's business logic inside an open transaction
// and returns a JSON-serializable response.
type commandHandler func(tx *sql.Tx, t *store.StoredTask) (any, error)

// execute runs a command under idempotency, version and generation
// preconditions, appends an evidence event and commits the idempotency record.
func (s *Service) execute(ctx context.Context, taskID, caller, opID string, requestJSON []byte,
	expectedVersion int64, gen task.Generation, logicalTime lease.LogicalClock,
	evType task.EvidenceType, payload any, handler commandHandler) (any, error) {

	digest := store.DigestRequest(string(requestJSON))
	var response any
	var replayed bool

	err := s.store.Transact(ctx, func(tx *sql.Tx) error {
		// Idempotency: replay with same content returns the stored response;
		// different content is a stable conflict.
		if existing, err := store.GetIdempotency(tx, caller, opID); err == nil {
			if existing.RequestDigest != digest {
				return errs.New(errs.CodeIdempotencyConflict, "operation id reused with different content").
					WithOperation(opID)
			}
			if existing.Status == store.IdempotencyCommitted {
				var stored any
				if err := json.Unmarshal([]byte(existing.ResponseJSON), &stored); err != nil {
					return err
				}
				response = stored
				replayed = true
				return nil
			}
		}

		t, err := s.store.GetTaskTx(ctx, tx, taskID)
		if err != nil {
			return err
		}
		if expectedVersion != 0 && expectedVersion != t.AggregateVersion {
			return errs.New(errs.CodeGenerationStale, "aggregate version mismatch").
				WithVersion(t.AggregateVersion).WithOperation(opID)
		}
		if gen != t.CurrentGeneration {
			return errs.New(errs.CodeGenerationStale, "generation mismatch").
				WithVersion(t.AggregateVersion).WithOperation(opID)
		}

		resp, err := handler(tx, t)
		if err != nil {
			return err
		}

		// Advance the logical clock and aggregate version.
		if _, _, err := store.AdvanceClock(tx, taskID, int64(logicalTime)); err != nil {
			return err
		}

		response = resp
		return nil
	})
	if err != nil {
		return nil, err
	}
	if replayed {
		return response, nil
	}

	err = s.store.Transact(ctx, func(tx *sql.Tx) error {
		// Append the evidence event.
		ev := &task.EvidenceEvent{
			LogicalTime: logicalTime,
			OperationID: opID,
			Generation:  gen,
			Type:        evType,
		}
		if _, err := s.store.AppendEvent(ctx, tx, taskID, ev, payload); err != nil {
			return err
		}

		// Commit the idempotency record.
		respJSON, err := encodeResponse(response)
		if err != nil {
			return err
		}
		if err := store.PutIdempotency(tx, caller, opID, digest, respJSON); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}

func encodeResponse(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
