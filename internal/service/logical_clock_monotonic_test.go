package service

import (
	"context"
	"errors"
	"testing"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/lease"
	"example.com/steel-fireproofing-evidence-closure/internal/task"
)

func TestModel_RejectsCommandsBehindLogicalClock(t *testing.T) {
	tests := []struct {
		name             string
		wantReject       bool
		wantClock        lease.LogicalClock
		wantEventDelta   int
		wantVersionDelta int64
		invoke           func(context.Context, *Service, string, int64) error
		resourceCount    func(context.Context, *Service, string) int
		wantResource     int
	}{
		{
			name:       "late material command",
			wantReject: true,
			wantClock:  10,
			invoke: func(ctx context.Context, svc *Service, taskID string, version int64) error {
				cmd := MaterialIssueCmd{BatchID: "coating-x", Grams: 100, EquipmentID: "sprayer-late", LeaseTicks: 100}
				_, err := svc.MaterialIssue(ctx, taskID, "caller", "late-material", mustJSON(t, cmd), version, 1, 5, cmd)
				return err
			},
			resourceCount: func(ctx context.Context, svc *Service, taskID string) int {
				balance, err := svc.MaterialBalance(ctx, taskID)
				if err != nil {
					t.Fatalf("material balance: %v", err)
				}
				return int(balance.Batches[0].IssuedGrams)
			},
			wantResource: 100,
		},
		{
			name:       "late construction evidence",
			wantReject: true,
			wantClock:  10,
			invoke: func(ctx context.Context, svc *Service, taskID string, version int64) error {
				cmd := CuringCmd{FaceID: "face-1", CoatIndex: 1, TempC: 20, HumidityRH: 50}
				_, err := svc.Curing(ctx, taskID, "caller", "late-curing", mustJSON(t, cmd), version, 1, 5, cmd)
				return err
			},
			resourceCount: func(ctx context.Context, svc *Service, taskID string) int {
				samples, err := svc.Store().Curing(ctx, taskID, "face-1", 1)
				if err != nil {
					t.Fatalf("curing samples: %v", err)
				}
				return len(samples)
			},
		},
		{
			name:       "late device receipt",
			wantReject: true,
			wantClock:  10,
			invoke: func(ctx context.Context, svc *Service, taskID string, version int64) error {
				cmd := DeviceCmd{
					InvocationID: "late-reading", EquipmentID: "gauge-1", Kind: lease.EquipmentThickness,
					Reading: &DeviceReading{FaceID: "face-1", PointID: "p1", Value: 20_000_000},
				}
				_, err := svc.InvokeDevice(ctx, taskID, "caller", "late-device", mustJSON(t, cmd), version, 1, 5, cmd)
				return err
			},
			resourceCount: func(ctx context.Context, svc *Service, taskID string) int {
				readings, err := svc.Store().DryFilm(ctx, taskID, "face-1", "p1", task.Generation(1))
				if err != nil {
					t.Fatalf("dry-film readings: %v", err)
				}
				return len(readings)
			},
		},
		{
			name:           "idempotent replay may use older time",
			wantClock:      10,
			wantEventDelta: 0,
			invoke: func(ctx context.Context, svc *Service, taskID string, version int64) error {
				cmd := MaterialIssueCmd{BatchID: "coating-x", Grams: 100, EquipmentID: "gauge-1", LeaseTicks: 100}
				_, err := svc.MaterialIssue(ctx, taskID, "caller", "issue-at-10", mustJSON(t, cmd), version, 1, 5, cmd)
				return err
			},
			resourceCount: func(ctx context.Context, svc *Service, taskID string) int {
				balance, err := svc.MaterialBalance(ctx, taskID)
				if err != nil {
					t.Fatalf("material balance: %v", err)
				}
				return int(balance.Batches[0].IssuedGrams)
			},
			wantResource: 100,
		},
		{
			name:             "larger logical time advances normally",
			wantClock:        11,
			wantEventDelta:   1,
			wantVersionDelta: 1,
			invoke: func(ctx context.Context, svc *Service, taskID string, version int64) error {
				cmd := MaterialIssueCmd{BatchID: "coating-x", Grams: 100, EquipmentID: "sprayer-future", LeaseTicks: 100}
				_, err := svc.MaterialIssue(ctx, taskID, "caller", "future-material", mustJSON(t, cmd), version, 1, 11, cmd)
				return err
			},
			resourceCount: func(ctx context.Context, svc *Service, taskID string) int {
				balance, err := svc.MaterialBalance(ctx, taskID)
				if err != nil {
					t.Fatalf("material balance: %v", err)
				}
				return int(balance.Batches[0].IssuedGrams)
			},
			wantResource: 200,
		},
		{
			name:       "version precondition still wins",
			wantReject: true,
			wantClock:  10,
			invoke: func(ctx context.Context, svc *Service, taskID string, version int64) error {
				cmd := CuringCmd{FaceID: "face-1", CoatIndex: 1, TempC: 20, HumidityRH: 50}
				_, err := svc.Curing(ctx, taskID, "caller", "stale-version", mustJSON(t, cmd), version-1, 1, 11, cmd)
				return err
			},
			resourceCount: func(ctx context.Context, svc *Service, taskID string) int {
				samples, err := svc.Store().Curing(ctx, taskID, "face-1", 1)
				if err != nil {
					t.Fatalf("curing samples: %v", err)
				}
				return len(samples)
			},
		},
		{
			name:       "generation precondition still wins",
			wantReject: true,
			wantClock:  10,
			invoke: func(ctx context.Context, svc *Service, taskID string, version int64) error {
				cmd := CuringCmd{FaceID: "face-1", CoatIndex: 1, TempC: 20, HumidityRH: 50}
				_, err := svc.Curing(ctx, taskID, "caller", "stale-generation", mustJSON(t, cmd), version, 0, 11, cmd)
				return err
			},
			resourceCount: func(ctx context.Context, svc *Service, taskID string) int {
				samples, err := svc.Store().Curing(ctx, taskID, "face-1", 1)
				if err != nil {
					t.Fatalf("curing samples: %v", err)
				}
				return len(samples)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, done := newTestService(t)
			defer done()
			ctx := context.Background()
			taskID := lockStandard(t, svc)

			open := MaterialOpenCmd{BatchID: "coating-x", OpenedGrams: 1000, ProofSummary: "proof"}
			if _, err := svc.MaterialOpen(ctx, taskID, "caller", "open-at-1", mustJSON(t, open), 0, 1, 1, open); err != nil {
				t.Fatalf("open material: %v", err)
			}
			issue := MaterialIssueCmd{BatchID: "coating-x", Grams: 100, EquipmentID: "gauge-1", LeaseTicks: 100}
			if _, err := svc.MaterialIssue(ctx, taskID, "caller", "issue-at-10", mustJSON(t, issue), 0, 1, 10, issue); err != nil {
				t.Fatalf("issue material at logical time 10: %v", err)
			}

			beforeTask, err := svc.GetTask(ctx, taskID)
			if err != nil {
				t.Fatalf("task before command: %v", err)
			}
			beforeEvidence, err := svc.Evidence(ctx, taskID, 0, 100)
			if err != nil {
				t.Fatalf("evidence before command: %v", err)
			}
			if beforeTask.LogicalClock != 10 {
				t.Fatalf("setup logical clock = %d, want 10", beforeTask.LogicalClock)
			}

			firstErr := tc.invoke(ctx, svc, taskID, beforeTask.AggregateVersion)
			if tc.wantReject {
				if firstErr == nil {
					t.Fatalf("command succeeded, want stable rejection")
				}
				var firstDomainErr *errs.Error
				if !errors.As(firstErr, &firstDomainErr) {
					t.Fatalf("rejection is not a stable domain error: %v", firstErr)
				}
				secondErr := tc.invoke(ctx, svc, taskID, beforeTask.AggregateVersion)
				var secondDomainErr *errs.Error
				if !errors.As(secondErr, &secondDomainErr) {
					t.Fatalf("repeated rejection is not a stable domain error: %v", secondErr)
				}
				if secondDomainErr.Code != firstDomainErr.Code || secondDomainErr.Message != firstDomainErr.Message {
					t.Fatalf("rejection changed from %s/%q to %s/%q", firstDomainErr.Code, firstDomainErr.Message, secondDomainErr.Code, secondDomainErr.Message)
				}
			} else if firstErr != nil {
				t.Fatalf("command failed: %v", firstErr)
			}

			afterTask, err := svc.GetTask(ctx, taskID)
			if err != nil {
				t.Fatalf("task after command: %v", err)
			}
			if afterTask.LogicalClock != tc.wantClock {
				t.Fatalf("logical clock = %d, want %d", afterTask.LogicalClock, tc.wantClock)
			}
			if got := afterTask.AggregateVersion - beforeTask.AggregateVersion; got != tc.wantVersionDelta {
				t.Fatalf("aggregate version delta = %d, want %d", got, tc.wantVersionDelta)
			}
			afterEvidence, err := svc.Evidence(ctx, taskID, 0, 100)
			if err != nil {
				t.Fatalf("evidence after command: %v", err)
			}
			if got := len(afterEvidence.Events) - len(beforeEvidence.Events); got != tc.wantEventDelta {
				t.Fatalf("evidence event delta = %d, want %d", got, tc.wantEventDelta)
			}
			if tc.resourceCount != nil {
				if got := tc.resourceCount(ctx, svc, taskID); got != tc.wantResource {
					t.Fatalf("resource state = %d, want %d", got, tc.wantResource)
				}
			}
			for i, event := range afterEvidence.Events {
				if event.Hash == "" {
					t.Fatalf("event %d has empty hash", event.Sequence)
				}
				if i > 0 && event.PrevHash != afterEvidence.Events[i-1].Hash {
					t.Fatalf("event %d does not chain to event %d", event.Sequence, afterEvidence.Events[i-1].Sequence)
				}
				if i > 0 && event.LogicalTime < afterEvidence.Events[i-1].LogicalTime {
					t.Fatalf("event %d logical time %d is behind event %d logical time %d", event.Sequence, event.LogicalTime, afterEvidence.Events[i-1].Sequence, afterEvidence.Events[i-1].LogicalTime)
				}
			}
		})
	}
}
