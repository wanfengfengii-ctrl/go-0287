package service

import (
	"context"
	"encoding/json"
	"testing"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/lease"
)

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func prepareMember(t *testing.T, svc *Service, taskID string, clock *int64) {
	t.Helper()
	ctx := context.Background()
	*clock++
	if _, err := svc.SurfacePrep(ctx, taskID, "c", "prep-1", mustJSON(t, SurfacePrepCmd{MemberID: "beam-1"}), 0, 1, lease.LogicalClock(*clock), SurfacePrepCmd{MemberID: "beam-1", TreatmentSummary: "s"}); err != nil {
		t.Fatalf("surface prep: %v", err)
	}
	*clock++
	if _, err := svc.Primer(ctx, taskID, "c", "primer-1", mustJSON(t, PrimerCmd{MemberID: "beam-1"}), 0, 1, lease.LogicalClock(*clock), PrimerCmd{MemberID: "beam-1"}); err != nil {
		t.Fatalf("primer: %v", err)
	}
}

func coatCmd(face string, idx int) CoatCmd {
	return CoatCmd{
		FaceID: face, CoatIndex: idx, SprayZoneID: "zone-1", MaterialBatchID: "coating-x",
		AreaMM2: 1000000, IssuedGrams: 100, EquipmentID: "sprayer-1", LeaseTicks: 100,
	}
}

func TestCoatPrefixAdvancesContiguously(t *testing.T) {
	svc, done := newTestService(t)
	defer done()
	taskID := lockStandard(t, svc)
	ctx := context.Background()

	var clock int64
	prepareMember(t, svc, taskID, &clock)

	for i := 1; i <= 3; i++ {
		clock++
		opID := "coat-" + string(rune('0'+i))
		if _, err := svc.Coat(ctx, taskID, "c", opID, mustJSON(t, coatCmd("face-1", i)), 0, 1, lease.LogicalClock(clock), coatCmd("face-1", i)); err != nil {
			t.Fatalf("coat %d: %v", i, err)
		}
	}
	cp, err := svc.store.CoatPrefix(ctx, taskID, 1)
	if err != nil {
		t.Fatalf("CoatPrefix: %v", err)
	}
	if got := cp.Completed("face-1"); got != 3 {
		t.Fatalf("face-1 prefix = %d, want 3", got)
	}
}

func TestCoatRejectsSkipDuplicateWrongMemberOldGeneration(t *testing.T) {
	svc, done := newTestService(t)
	defer done()
	taskID := lockStandard(t, svc)
	ctx := context.Background()

	var clock int64
	prepareMember(t, svc, taskID, &clock)

	// Skip to the third coat before the first.
	clock++
	_, err := svc.Coat(ctx, taskID, "c", "skip-3", mustJSON(t, coatCmd("face-1", 3)), 0, 1, lease.LogicalClock(clock), coatCmd("face-1", 3))
	assertCode(t, err, errs.CodeCoatOutOfOrder)

	// Apply coat 1, then repeat coat 1 (duplicate).
	clock++
	if _, err := svc.Coat(ctx, taskID, "c", "coat-1", mustJSON(t, coatCmd("face-1", 1)), 0, 1, lease.LogicalClock(clock), coatCmd("face-1", 1)); err != nil {
		t.Fatalf("coat 1: %v", err)
	}
	clock++
	_, err = svc.Coat(ctx, taskID, "c", "dup-1", mustJSON(t, coatCmd("face-1", 1)), 0, 1, lease.LogicalClock(clock), coatCmd("face-1", 1))
	assertCode(t, err, errs.CodeCoatDuplicate)

	// Wrong member (unknown face).
	clock++
	bad := coatCmd("face-99", 1)
	_, err = svc.Coat(ctx, taskID, "c", "wrong-face", mustJSON(t, bad), 0, 1, lease.LogicalClock(clock), bad)
	assertCode(t, err, errs.CodeMemberMismatch)

	// Old generation.
	clock++
	old := coatCmd("face-1", 2)
	_, err = svc.Coat(ctx, taskID, "c", "old-gen", mustJSON(t, old), 0, 0, lease.LogicalClock(clock), old)
	assertCode(t, err, errs.CodeGenerationStale)

	// No prefix advance beyond coat 1 and no extra evidence for face-1.
	cp, err := svc.store.CoatPrefix(ctx, taskID, 1)
	if err != nil {
		t.Fatalf("CoatPrefix: %v", err)
	}
	if got := cp.Completed("face-1"); got != 1 {
		t.Fatalf("face-1 prefix = %d, want 1 (only coat 1 applied)", got)
	}
	coats, _ := svc.store.Coats(ctx, taskID, 1)
	if len(coats) != 1 {
		t.Fatalf("coats recorded = %d, want 1", len(coats))
	}
}

func assertCode(t *testing.T, err error, want errs.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %s, got nil", want)
	}
	de, ok := err.(*errs.Error)
	if !ok {
		t.Fatalf("error is not *errs.Error: %v", err)
	}
	if de.Code != want {
		t.Fatalf("code = %s, want %s", de.Code, want)
	}
}
