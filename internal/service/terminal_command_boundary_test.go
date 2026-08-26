package service

import (
	"context"
	"testing"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/lease"
	"example.com/steel-fireproofing-evidence-closure/internal/task"
)

func TestModel_TerminalDecisionRejectsEveryMutatingCommand(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		terminal task.TerminalState
		prepare  func(*Service, string)
		mutate   func(*Service, string) error
	}{
		{
			name: "surface prep", terminal: task.TerminalAccepted,
			mutate: func(s *Service, id string) error {
				cmd := SurfacePrepCmd{MemberID: "beam-1", TreatmentSummary: "late handover"}
				_, err := s.SurfacePrep(ctx, id, "late", "late-prep", mustJSON(t, cmd), 0, 1, 200, cmd)
				return err
			},
		},
		{
			name: "primer", terminal: task.TerminalAccepted,
			mutate: func(s *Service, id string) error {
				cmd := PrimerCmd{MemberID: "beam-1"}
				_, err := s.Primer(ctx, id, "late", "late-primer", mustJSON(t, cmd), 0, 1, 200, cmd)
				return err
			},
		},
		{
			name: "material open", terminal: task.TerminalAccepted,
			mutate: func(s *Service, id string) error {
				cmd := MaterialOpenCmd{BatchID: "late-batch", OpenedGrams: 10, ProofSummary: "late proof"}
				_, err := s.MaterialOpen(ctx, id, "late", "late-open", mustJSON(t, cmd), 0, 1, 200, cmd)
				return err
			},
		},
		{
			name: "material issue", terminal: task.TerminalAccepted,
			mutate: func(s *Service, id string) error {
				cmd := MaterialIssueCmd{BatchID: "coating-x", Grams: 1, EquipmentID: "late-sprayer", LeaseTicks: 10}
				_, err := s.MaterialIssue(ctx, id, "late", "late-issue", mustJSON(t, cmd), 0, 1, 200, cmd)
				return err
			},
		},
		{
			name: "coat", terminal: task.TerminalQuarantined,
			prepare: func(s *Service, id string) {
				prep := SurfacePrepCmd{MemberID: "beam-1"}
				if _, err := s.SurfacePrep(ctx, id, "setup", "setup-prep", mustJSON(t, prep), 0, 1, 1, prep); err != nil {
					t.Fatalf("prepare surface: %v", err)
				}
				primer := PrimerCmd{MemberID: "beam-1"}
				if _, err := s.Primer(ctx, id, "setup", "setup-primer", mustJSON(t, primer), 0, 1, 2, primer); err != nil {
					t.Fatalf("prepare primer: %v", err)
				}
			},
			mutate: func(s *Service, id string) error {
				cmd := CoatCmd{FaceID: "face-1", CoatIndex: 1, SprayZoneID: "zone-1", MaterialBatchID: "coating-x", AreaMM2: 1000, IssuedGrams: 10, EquipmentID: "late-sprayer", LeaseTicks: 10}
				_, err := s.Coat(ctx, id, "late", "late-coat", mustJSON(t, cmd), 0, 1, 200, cmd)
				return err
			},
		},
		{
			name: "curing", terminal: task.TerminalAccepted,
			mutate: func(s *Service, id string) error {
				cmd := CuringCmd{FaceID: "face-1", CoatIndex: 3, TempC: 20, HumidityRH: 50}
				_, err := s.Curing(ctx, id, "late", "late-curing", mustJSON(t, cmd), 0, 1, 200, cmd)
				return err
			},
		},
		{
			name: "dry film receipt", terminal: task.TerminalAccepted,
			mutate: func(s *Service, id string) error {
				cmd := DryFilmCmd{FaceID: "face-1", PointID: "p1", ValueUM: 40_000_000}
				_, err := s.DryFilm(ctx, id, "late", "late-dry-film", mustJSON(t, cmd), 0, 1, 200, cmd)
				return err
			},
		},
		{
			name: "bond receipt", terminal: task.TerminalAccepted,
			mutate: func(s *Service, id string) error {
				cmd := BondCmd{SpecimenID: "late-specimen", ValueKPa: 600_000}
				_, err := s.Bond(ctx, id, "late", "late-bond", mustJSON(t, cmd), 0, 1, 200, cmd)
				return err
			},
		},
		{
			name: "device receipt", terminal: task.TerminalAccepted,
			prepare: func(s *Service, id string) {
				open := MaterialOpenCmd{BatchID: "device-lease-batch", OpenedGrams: 10, ProofSummary: "lease setup"}
				if _, err := s.MaterialOpen(ctx, id, "setup", "device-lease-open", mustJSON(t, open), 0, 1, 20, open); err != nil {
					t.Fatalf("open device lease batch: %v", err)
				}
				issue := MaterialIssueCmd{BatchID: "device-lease-batch", Grams: 1, EquipmentID: "gauge-1", LeaseTicks: 1000}
				if _, err := s.MaterialIssue(ctx, id, "setup", "device-lease-issue", mustJSON(t, issue), 0, 1, 21, issue); err != nil {
					t.Fatalf("acquire device lease: %v", err)
				}
				if _, err := s.MaterialOpen(ctx, id, "setup", "device-lease-reconcile", mustJSON(t, open), 0, 1, 22, open); err != nil {
					t.Fatalf("reconcile device lease batch: %v", err)
				}
			},
			mutate: func(s *Service, id string) error {
				cmd := DeviceCmd{InvocationID: "late-invocation", EquipmentID: "gauge-1", Kind: lease.EquipmentThickness, Reading: &DeviceReading{FaceID: "face-1", PointID: "p1", Value: 40_000_000}}
				_, err := s.InvokeDevice(ctx, id, "late", "late-device", mustJSON(t, cmd), 0, 1, 200, cmd)
				return err
			},
		},
		{
			name: "defect", terminal: task.TerminalAccepted,
			mutate: func(s *Service, id string) error {
				cmd := DefectCmd{Kind: task.DefectThicknessLow, FaceID: "face-1", PointID: "p1", MaterialBatchID: "coating-x"}
				_, err := s.RecordDefect(ctx, id, "late", "late-defect", mustJSON(t, cmd), 0, 1, 200, cmd)
				return err
			},
		},
		{
			name: "review", terminal: task.TerminalAccepted,
			mutate: func(s *Service, id string) error {
				cmd := ReviewCmd{ReviewerID: "late-reviewer", Approved: true, Classes: allClasses()}
				_, err := s.SubmitReview(ctx, id, "late", "late-review", mustJSON(t, cmd), 0, 1, 200, cmd)
				return err
			},
		},
		{
			name: "recoat generation", terminal: task.TerminalAccepted,
			mutate: func(s *Service, id string) error {
				_, err := s.CreateRecoatGeneration(ctx, id, 1)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, done := newTestService(t)
			defer done()
			taskID := lockStandard(t, svc)

			if tt.terminal == task.TerminalAccepted {
				fullAcceptanceSetup(t, svc, taskID)
				if tt.prepare != nil {
					tt.prepare(svc, taskID)
				}
				for i, reviewer := range []string{"rev-a", "rev-b"} {
					cmd := ReviewCmd{ReviewerID: reviewer, Approved: true, Classes: allClasses()}
					opID := "accept-review-" + reviewer
					if _, err := svc.SubmitReview(ctx, taskID, "setup", opID, mustJSON(t, cmd), 0, 1, lease.LogicalClock(100+i), cmd); err != nil {
						t.Fatalf("acceptance review: %v", err)
					}
				}
			} else if tt.prepare != nil {
				tt.prepare(svc, taskID)
			}

			terminal, err := svc.SubmitTerminal(ctx, taskID, TerminalCmd{State: tt.terminal, Decider: "arbiter"})
			if err != nil {
				t.Fatalf("terminal decision: %v", err)
			}
			if tt.terminal == task.TerminalAccepted && terminal.CredentialID == "" {
				t.Fatal("accepted task has no credential")
			}
			beforeTask, err := svc.GetTask(ctx, taskID)
			if err != nil {
				t.Fatalf("task before late command: %v", err)
			}
			beforeEvidence, err := svc.Evidence(ctx, taskID, 0, 500)
			if err != nil {
				t.Fatalf("evidence before late command: %v", err)
			}
			if len(beforeEvidence.Events) == 0 || beforeEvidence.Events[len(beforeEvidence.Events)-1].Hash != terminal.EvidenceRoot {
				t.Fatal("terminal evidence root does not match the evidence page")
			}

			err = tt.mutate(svc, taskID)
			assertCode(t, err, errs.CodeTerminalAlreadyDecided)

			afterTask, err := svc.GetTask(ctx, taskID)
			if err != nil {
				t.Fatalf("task after late command: %v", err)
			}
			afterEvidence, err := svc.Evidence(ctx, taskID, 0, 500)
			if err != nil {
				t.Fatalf("evidence after late command: %v", err)
			}
			if afterTask.AggregateVersion != beforeTask.AggregateVersion {
				t.Fatalf("aggregate version advanced from %d to %d", beforeTask.AggregateVersion, afterTask.AggregateVersion)
			}
			if afterTask.CurrentGeneration != beforeTask.CurrentGeneration {
				t.Fatalf("generation advanced from %d to %d", beforeTask.CurrentGeneration, afterTask.CurrentGeneration)
			}
			if len(afterEvidence.Events) != len(beforeEvidence.Events) || afterEvidence.NextCursor != beforeEvidence.NextCursor {
				t.Fatalf("evidence page changed after terminal decision: before=%d/%d after=%d/%d", len(beforeEvidence.Events), beforeEvidence.NextCursor, len(afterEvidence.Events), afterEvidence.NextCursor)
			}
			if afterEvidence.Events[len(afterEvidence.Events)-1].Hash != terminal.EvidenceRoot {
				t.Fatal("evidence root changed after terminal decision")
			}
		})
	}
}
