package service

import (
	"context"
	"sync"
	"testing"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
)

func openBatch(t *testing.T, svc *Service, taskID string, grams int64) {
	t.Helper()
	ctx := context.Background()
	cmd := MaterialOpenCmd{BatchID: "coating-x", OpenedGrams: grams, ProofSummary: "proof-v1"}
	if _, err := svc.MaterialOpen(ctx, taskID, "c", "open-1", mustJSON(t, cmd), 0, 1, 1, cmd); err != nil {
		t.Fatalf("open batch: %v", err)
	}
}

func issueCmd(grams int64, opID string) MaterialIssueCmd {
	return MaterialIssueCmd{BatchID: "coating-x", Grams: grams, EquipmentID: "sprayer-1", LeaseTicks: 100}
}

func TestConcurrentMaterialIssueAtMostOneSucceeds(t *testing.T) {
	svc, done := newTestService(t)
	defer done()
	taskID := lockStandard(t, svc)
	openBatch(t, svc, taskID, 100)

	ctx := context.Background()
	var wg sync.WaitGroup
	start := make(chan struct{})
	errsCh := make(chan error, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			cmd := issueCmd(60, "")
			// Distinct operation ids so both attempts run fresh.
			opID := "issue-" + string(rune('a'+i))
			_, err := svc.MaterialIssue(ctx, taskID, "c", opID, mustJSON(t, cmd), 0, 1, 10, cmd)
			errsCh <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(errsCh)

	successes := 0
	for err := range errsCh {
		if err == nil {
			successes++
		} else {
			assertCode(t, err, errs.CodeMaterialInsufficient)
		}
	}
	if successes != 1 {
		t.Fatalf("successes = %d, want exactly 1", successes)
	}

	batches, err := svc.store.ListBatches(ctx, taskID)
	if err != nil {
		t.Fatalf("ListBatches: %v", err)
	}
	b := batches[0]
	// Conservation: opened = issued + balance (no recovery/waste).
	if b.OpenedGrams != b.IssuedGrams+b.Balance() {
		t.Fatalf("mass not conserved: opened=%d issued=%d balance=%d", b.OpenedGrams, b.IssuedGrams, b.Balance())
	}
	if !b.Conserved() {
		t.Fatalf("batch not conserved: %+v", b)
	}
	if b.IssuedGrams != 60 {
		t.Fatalf("issued = %d, want 60", b.IssuedGrams)
	}
}

func TestMaterialIssueIdempotency(t *testing.T) {
	svc, done := newTestService(t)
	defer done()
	taskID := lockStandard(t, svc)
	openBatch(t, svc, taskID, 1000)

	ctx := context.Background()
	cmd := issueCmd(100, "")
	body := mustJSON(t, cmd)

	// Same operation id and content: replay returns the original response and
	// deducts only once.
	r1, err := svc.MaterialIssue(ctx, taskID, "c", "op-1", body, 0, 1, 10, cmd)
	if err != nil {
		t.Fatalf("first issue: %v", err)
	}
	r2, err := svc.MaterialIssue(ctx, taskID, "c", "op-1", body, 0, 1, 10, cmd)
	if err != nil {
		t.Fatalf("replay issue: %v", err)
	}
	_ = r1
	_ = r2

	batches, _ := svc.store.ListBatches(ctx, taskID)
	if batches[0].IssuedGrams != 100 {
		t.Fatalf("issued after replay = %d, want 100 (single deduction)", batches[0].IssuedGrams)
	}

	// Same operation id with different content is a stable conflict.
	other := issueCmd(150, "")
	if _, err := svc.MaterialIssue(ctx, taskID, "c", "op-1", mustJSON(t, other), 0, 1, 10, other); err == nil {
		t.Fatalf("expected idempotency conflict, got nil")
	} else {
		assertCode(t, err, errs.CodeIdempotencyConflict)
	}
	batches, _ = svc.store.ListBatches(ctx, taskID)
	if batches[0].IssuedGrams != 100 {
		t.Fatalf("conflict must not deduct, issued=%d", batches[0].IssuedGrams)
	}
}
