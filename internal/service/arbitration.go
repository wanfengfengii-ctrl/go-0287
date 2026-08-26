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

// ReviewCmd records an independent reviewer's attestation over evidence
// classes for a generation.
type ReviewCmd struct {
	ReviewerID string   `json:"reviewer_id"`
	Approved   bool     `json:"approved"`
	Classes    []string `json:"classes"`
}

// SubmitReview records a review.
func (s *Service) SubmitReview(ctx context.Context, taskID, caller, opID string, requestJSON []byte, expectedVersion int64, gen task.Generation, clock lease.LogicalClock, c ReviewCmd) (any, error) {
	return s.execute(ctx, taskID, caller, opID, requestJSON, expectedVersion, gen, clock, task.EvidenceReview, c, func(tx *sql.Tx, t *store.StoredTask) (any, error) {
		if err := store.InsertReview(tx, taskID, task.Review{
			ReviewerID: c.ReviewerID, Generation: gen, Approved: c.Approved, Classes: c.Classes,
		}); err != nil {
			return nil, err
		}
		return map[string]string{"reviewer_id": c.ReviewerID, "status": "review_recorded"}, nil
	})
}

// TerminalCmd submits a terminal decision: acceptance, quarantine or cancel.
type TerminalCmd struct {
	State   task.TerminalState `json:"state"`
	Decider string             `json:"decider"`
}

// TerminalResult is the outcome of a terminal decision.
type TerminalResult struct {
	State        task.TerminalState     `json:"state"`
	Generation   task.Generation        `json:"generation"`
	CredentialID string                 `json:"credential_id,omitempty"`
	EvidenceRoot string                 `json:"evidence_root"`
	Decision     *task.TerminalDecision `json:"decision"`
}

// SubmitTerminal validates acceptance preconditions (when accepting), then
// writes the single immutable terminal decision and, on acceptance, the unique
// credential.
func (s *Service) SubmitTerminal(ctx context.Context, taskID string, c TerminalCmd) (*TerminalResult, error) {
	t, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if c.State != task.TerminalAccepted && c.State != task.TerminalQuarantined && c.State != task.TerminalCancelled {
		return nil, errs.New(errs.CodeInvalidInput, "invalid terminal state")
	}

	evidenceRoot, err := s.store.EvidenceRoot(ctx, taskID)
	if err != nil {
		return nil, err
	}

	if c.State == task.TerminalAccepted {
		reasons, err := s.acceptanceReasons(ctx, t)
		if err != nil {
			return nil, err
		}
		if len(reasons) > 0 {
			sort.Strings(reasons)
			return nil, errs.New(errs.CodeInvalidInput, "acceptance preconditions not met").WithReasons(reasons...)
		}
	}

	decision := task.TerminalDecision{State: c.State, DecidedBy: c.Decider, Generation: t.CurrentGeneration}
	credentialID := ""
	if c.State == task.TerminalAccepted {
		credentialID = task.GenerateCredential(taskID, t.CurrentGeneration, evidenceRoot).CredentialID
	}

	var out *TerminalResult
	err = s.store.Transact(ctx, func(tx *sql.Tx) error {
		if err := store.DecideTerminal(tx, taskID, decision, credentialID, evidenceRoot); err != nil {
			return err
		}
		out = &TerminalResult{
			State: c.State, Generation: t.CurrentGeneration,
			CredentialID: credentialID, EvidenceRoot: evidenceRoot, Decision: &decision,
		}
		return nil
	})
	return out, err
}

// acceptanceReasons evaluates every acceptance precondition and returns an
// ordered list of unmet conditions (empty when the task may be accepted).
func (s *Service) acceptanceReasons(ctx context.Context, t *store.StoredTask) ([]string, error) {
	var reasons []string
	gen := t.CurrentGeneration

	batches, err := s.store.ListBatches(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	for _, b := range batches {
		if !b.Conserved() {
			reasons = append(reasons, "mass_conservation:"+b.ID)
		}
	}

	coats, err := s.store.Coats(ctx, t.ID, gen)
	if err != nil {
		return nil, err
	}
	prefixByFace := map[string]int{}
	for _, c := range coats {
		if c.CoatIndex > prefixByFace[c.FaceID] {
			prefixByFace[c.FaceID] = c.CoatIndex
		}
	}
	for _, m := range t.Scope.Members {
		for _, f := range m.Member.ExposedFaces {
			if prefixByFace[f.ID] < t.Scope.Plan.CoatCount {
				reasons = append(reasons, "coat_prefix:"+f.ID)
			}
			// Curing coverage for the last applied coat.
			last := prefixByFace[f.ID]
			if last == 0 {
				continue
			}
			samples, err := s.store.Curing(ctx, t.ID, f.ID, last)
			if err != nil {
				return nil, err
			}
			if !task.CuringWindowClosed(samples, lease.LogicalClock(t.Scope.Plan.CuringWindowTicks)) {
				reasons = append(reasons, "curing_coverage:"+f.ID)
			}
			// Thickness coverage per point.
			for _, gp := range f.GridPoints {
				readings, err := s.store.DryFilm(ctx, t.ID, f.ID, gp.ID, gen)
				if err != nil {
					return nil, err
				}
				cum, err := task.CumulativeThickness(readings)
				if err != nil {
					return nil, err
				}
				if cum.Raw() < t.Scope.Plan.MinDryFilmUM {
					reasons = append(reasons, "thickness_coverage:"+f.ID+"/"+gp.ID)
				}
			}
		}
	}

	bonds, err := s.store.Bond(ctx, t.ID, gen)
	if err != nil {
		return nil, err
	}
	for _, b := range bonds {
		if b.ValueKPa < t.Scope.Plan.MinBondStrengthKPa {
			reasons = append(reasons, "bond_evidence:"+b.SpecimenID)
		}
	}

	reviews, err := s.store.Reviews(ctx, t.ID, gen)
	if err != nil {
		return nil, err
	}
	if err := task.ValidateReviewers(reviews); err != nil {
		if de, ok := err.(*errs.Error); ok {
			for _, r := range de.Reasons {
				reasons = append(reasons, "review:"+r)
			}
		} else {
			reasons = append(reasons, "review:incomplete")
		}
	}

	return reasons, nil
}
