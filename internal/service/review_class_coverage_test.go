package service

import (
	"context"
	"testing"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/lease"
	"example.com/steel-fireproofing-evidence-closure/internal/task"
)

func TestModel_IndependentReviewCoverage(t *testing.T) {
	firstHalf := []string{
		string(task.ReviewMass),
		string(task.ReviewCoatPrefix),
		string(task.ReviewCuring),
	}
	secondHalf := []string{
		string(task.ReviewThickness),
		string(task.ReviewBond),
		string(task.ReviewRecoat),
	}
	tests := []struct {
		name          string
		reviewClasses [][]string
		wantAccepted  bool
	}{
		{
			name:          "complementary approvals do not satisfy per-class independence",
			reviewClasses: [][]string{firstHalf, secondHalf},
			wantAccepted:  false,
		},
		{
			name:          "two reviewers approving every class are accepted",
			reviewClasses: [][]string{allClasses(), allClasses()},
			wantAccepted:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, done := newTestService(t)
			defer done()
			ctx := context.Background()
			taskID := lockStandard(t, svc)
			fullAcceptanceSetup(t, svc, taskID)

			for i, classes := range tt.reviewClasses {
				reviewerID := []string{"rev-a", "rev-b"}[i]
				cmd := ReviewCmd{ReviewerID: reviewerID, Approved: true, Classes: classes}
				if _, err := svc.SubmitReview(ctx, taskID, "caller", "review-"+reviewerID, mustJSON(t, cmd), 0, 1, lease.LogicalClock(100+i), cmd); err != nil {
					t.Fatalf("submit review %s: %v", reviewerID, err)
				}
			}

			result, terminalErr := svc.SubmitTerminal(ctx, taskID, TerminalCmd{
				State: task.TerminalAccepted, Decider: "acceptance-officer",
			})
			stored, getErr := svc.Store().GetTask(ctx, taskID)
			if getErr != nil {
				t.Fatalf("get stored task: %v", getErr)
			}

			if !tt.wantAccepted {
				if terminalErr == nil {
					t.Errorf("acceptance succeeded with complementary per-class approvals")
				} else {
					assertCode(t, terminalErr, errs.CodeInvalidInput)
				}
				if result != nil {
					t.Errorf("failed acceptance result = %+v, want nil", result)
				}
				if stored.Terminal != nil || stored.EvidenceRoot != "" || stored.CredentialID != "" {
					t.Errorf("failed acceptance persisted terminal=%+v evidence_root=%q credential_id=%q", stored.Terminal, stored.EvidenceRoot, stored.CredentialID)
				}
				return
			}

			if terminalErr != nil {
				t.Fatalf("acceptance with two full-class reviewers: %v", terminalErr)
			}
			if result == nil || result.State != task.TerminalAccepted || result.EvidenceRoot == "" || result.CredentialID == "" {
				t.Fatalf("successful acceptance result = %+v", result)
			}
			if stored.Terminal == nil || stored.Terminal.State != task.TerminalAccepted || stored.EvidenceRoot != result.EvidenceRoot || stored.CredentialID != result.CredentialID {
				t.Errorf("persisted acceptance = %+v root=%q credential=%q; result=%+v", stored.Terminal, stored.EvidenceRoot, stored.CredentialID, result)
			}
		})
	}
}
