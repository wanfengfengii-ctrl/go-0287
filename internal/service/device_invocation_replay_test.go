package service

import (
	"context"
	"errors"
	"testing"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/lease"
	"example.com/steel-fireproofing-evidence-closure/internal/task"
)

func TestModel_SucceededInvocationReplayDoesNotDuplicateEvidence(t *testing.T) {
	tests := []struct {
		name    string
		kind    lease.EquipmentKind
		reading DeviceReading
		wantRef string
	}{
		{
			name: "dry film reading",
			kind: lease.EquipmentThickness,
			reading: DeviceReading{
				FaceID: "face-1", PointID: "p1", Value: 20_000_000,
			},
			wantRef: "face-1/p1",
		},
		{
			name: "bond reading",
			kind: lease.EquipmentPullOff,
			reading: DeviceReading{
				SpecimenID: "specimen-1", Value: 600_000,
			},
			wantRef: "specimen-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, done := newTestService(t)
			defer done()
			taskID := lockStandard(t, svc)
			acquireLease(t, svc, taskID, "device-1", 100)
			ctx := context.Background()
			cmd := DeviceCmd{
				InvocationID: "invocation-1",
				EquipmentID:  "device-1",
				Kind:         tt.kind,
				Reading:      &tt.reading,
			}

			firstAny, err := svc.InvokeDevice(ctx, taskID, "frontend", "request-1", mustJSON(t, cmd), 0, 1, 10, cmd)
			if err != nil {
				t.Fatalf("first successful receipt: %v", err)
			}
			first, ok := firstAny.(DeviceResult)
			if !ok {
				t.Fatalf("first result type = %T, want DeviceResult", firstAny)
			}
			if first.Status != lease.InvocationSucceeded || first.Attempt != 1 || first.ReadingRef != tt.wantRef {
				t.Fatalf("first result = %+v, want succeeded attempt 1 with ref %q", first, tt.wantRef)
			}

			eventsBefore, err := svc.Store().Evidence(ctx, taskID, 0, 100)
			if err != nil {
				t.Fatalf("evidence before replay: %v", err)
			}
			invocationBefore, err := svc.Store().GetInvocation(ctx, taskID, cmd.InvocationID)
			if err != nil {
				t.Fatalf("invocation before replay: %v", err)
			}
			measurementCount := func() int {
				t.Helper()
				if tt.kind == lease.EquipmentThickness {
					got, err := svc.Store().DryFilm(ctx, taskID, tt.reading.FaceID, tt.reading.PointID, task.Generation(1))
					if err != nil {
						t.Fatalf("dry film readings: %v", err)
					}
					return len(got)
				}
				got, err := svc.Store().Bond(ctx, taskID, task.Generation(1))
				if err != nil {
					t.Fatalf("bond readings: %v", err)
				}
				return len(got)
			}
			if got := measurementCount(); got != 1 {
				t.Fatalf("measurements before replay = %d, want 1", got)
			}

			var replayOutcome string
			for i, opID := range []string{"request-2", "request-3"} {
				gotAny, replayErr := svc.InvokeDevice(ctx, taskID, "frontend", opID, mustJSON(t, cmd), 0, 1, lease.LogicalClock(11+i), cmd)
				outcome := "result"
				if replayErr != nil {
					var domainErr *errs.Error
					if !errors.As(replayErr, &domainErr) {
						t.Fatalf("replay %d returned unstable non-domain error: %v", i+1, replayErr)
					}
					outcome = "error:" + string(domainErr.Code)
				} else {
					got, ok := gotAny.(DeviceResult)
					if !ok {
						t.Fatalf("replay %d result type = %T, want DeviceResult", i+1, gotAny)
					}
					if got != first {
						t.Fatalf("replay %d result = %+v, want existing result %+v", i+1, got, first)
					}
				}
				if i == 0 {
					replayOutcome = outcome
				} else if outcome != replayOutcome {
					t.Fatalf("replay outcomes changed: first %q, second %q", replayOutcome, outcome)
				}

				if got := measurementCount(); got != 1 {
					t.Fatalf("measurements after replay %d = %d, want 1", i+1, got)
				}
				eventsAfter, err := svc.Store().Evidence(ctx, taskID, 0, 100)
				if err != nil {
					t.Fatalf("evidence after replay %d: %v", i+1, err)
				}
				if len(eventsAfter) != len(eventsBefore) {
					t.Fatalf("evidence events after replay %d = %d, want %d", i+1, len(eventsAfter), len(eventsBefore))
				}
				invocationAfter, err := svc.Store().GetInvocation(ctx, taskID, cmd.InvocationID)
				if err != nil {
					t.Fatalf("invocation after replay %d: %v", i+1, err)
				}
				if *invocationAfter != *invocationBefore {
					t.Fatalf("invocation changed after replay %d: before=%+v after=%+v", i+1, *invocationBefore, *invocationAfter)
				}
			}
		})
	}
}
