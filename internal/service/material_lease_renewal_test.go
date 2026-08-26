package service

import (
	"context"
	"testing"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/lease"
)

func TestModel_MaterialIssueRefreshesExpiredLeaseAtomically(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T, *Service, string)
	}{
		{
			name: "expired lease is renewed before a successful device operation",
			run: func(t *testing.T, svc *Service, taskID string) {
				ctx := context.Background()
				first := MaterialIssueCmd{BatchID: "coating-x", Grams: 100, EquipmentID: "sprayer-1", LeaseTicks: 5}
				if _, err := svc.MaterialIssue(ctx, taskID, "crew", "issue-old", mustJSON(t, first), 0, 1, 10, first); err != nil {
					t.Fatalf("initial issue: %v", err)
				}

				renewal := MaterialIssueCmd{BatchID: "coating-x", Grams: 200, EquipmentID: "sprayer-1", LeaseTicks: 100}
				if _, err := svc.MaterialIssue(ctx, taskID, "crew", "issue-renewal", mustJSON(t, renewal), 0, 1, 20, renewal); err != nil {
					t.Fatalf("issue after expiry: %v", err)
				}

				gotLease, err := svc.Store().GetLease(ctx, taskID, "sprayer-1")
				if err != nil {
					t.Fatalf("lease after successful renewal: %v", err)
				}
				if gotLease.OwnerID != taskID || gotLease.ExpiresAt != 120 {
					t.Fatalf("renewed lease = {owner:%q expires:%d}, want {owner:%q expires:120}", gotLease.OwnerID, gotLease.ExpiresAt, taskID)
				}

				device := DeviceCmd{
					InvocationID: "sprayer-check",
					EquipmentID:  "sprayer-1",
					Kind:         lease.EquipmentSprayer,
					Fault:        lease.FaultTimeout,
				}
				if _, err := svc.InvokeDevice(ctx, taskID, "crew", "device-after-renewal", mustJSON(t, device), 0, 1, 21, device); err != nil {
					t.Fatalf("device operation within renewed lease: %v", err)
				}

				batches, err := svc.Store().ListBatches(ctx, taskID)
				if err != nil {
					t.Fatalf("list batches: %v", err)
				}
				if len(batches) != 1 || batches[0].IssuedGrams != 300 {
					t.Fatalf("issued material after two successful issues = %+v, want 300 grams", batches)
				}
			},
		},
		{
			name: "active lease remains busy and rolls back material deduction",
			run: func(t *testing.T, svc *Service, taskID string) {
				ctx := context.Background()
				first := MaterialIssueCmd{BatchID: "coating-x", Grams: 100, EquipmentID: "sprayer-1", LeaseTicks: 100}
				if _, err := svc.MaterialIssue(ctx, taskID, "crew", "issue-holder", mustJSON(t, first), 0, 1, 10, first); err != nil {
					t.Fatalf("initial issue: %v", err)
				}

				competing := MaterialIssueCmd{BatchID: "coating-x", Grams: 200, EquipmentID: "sprayer-1", LeaseTicks: 100}
				_, err := svc.MaterialIssue(ctx, taskID, "other-crew", "issue-competing", mustJSON(t, competing), 0, 1, 20, competing)
				assertCode(t, err, errs.CodeLeaseBusy)

				batches, err := svc.Store().ListBatches(ctx, taskID)
				if err != nil {
					t.Fatalf("list batches: %v", err)
				}
				if len(batches) != 1 || batches[0].IssuedGrams != 100 {
					t.Fatalf("issued material after rejected competition = %+v, want 100 grams", batches)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, done := newTestService(t)
			t.Cleanup(done)
			taskID := lockStandard(t, svc)
			openBatch(t, svc, taskID, 1_000)
			tt.run(t, svc, taskID)
		})
	}
}
