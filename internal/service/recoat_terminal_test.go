package service

import (
	"context"
	"sync"
	"testing"

	"example.com/steel-fireproofing-evidence-closure/internal/catalog"
	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/lease"
	"example.com/steel-fireproofing-evidence-closure/internal/task"
)

// customInfluenceMember builds a member with three single-point faces whose
// adjacency distances make the influence domain of a defect at "a" equal
// exactly {a, b} (excluding c).
func customInfluenceMember() catalog.Member {
	return catalog.Member{
		ID: "beam-1", Floor: "A", FireCompartment: "F1", Type: catalog.MemberBeam,
		Section: catalog.Section{HeightMM: 400, WidthMM: 200}, FireRatingMinutes: 60,
		MaterialBatchIDs: []string{"coating-a", "coating-b"}, PrimerBatchID: "primer-a",
		SprayZones: []catalog.SprayZone{{ID: "zone-1"}, {ID: "zone-2"}, {ID: "zone-3"}},
		ExposedFaces: []catalog.ExposedFace{
			{ID: "face-1", MemberID: "beam-1", SprayZoneID: "zone-1",
				Boundary:   catalog.Boundary{MinX: 0, MinY: 0, MaxX: 1000, MaxY: 1000},
				GridPoints: []catalog.GridPoint{{ID: "a", FaceID: "face-1", Point: catalog.Point{X: 1, Y: 1}, SprayZoneID: "zone-1"}}},
			{ID: "face-2", MemberID: "beam-1", SprayZoneID: "zone-2",
				Boundary:   catalog.Boundary{MinX: 0, MinY: 0, MaxX: 1000, MaxY: 1000},
				GridPoints: []catalog.GridPoint{{ID: "b", FaceID: "face-2", Point: catalog.Point{X: 1, Y: 1}, SprayZoneID: "zone-2"}}},
			{ID: "face-3", MemberID: "beam-1", SprayZoneID: "zone-3",
				Boundary:   catalog.Boundary{MinX: 0, MinY: 0, MaxX: 1000, MaxY: 1000},
				GridPoints: []catalog.GridPoint{{ID: "c", FaceID: "face-3", Point: catalog.Point{X: 1, Y: 1}, SprayZoneID: "zone-3"}}},
		},
		AdjacencyEdges: []catalog.AdjacencyEdge{
			{From: "a", To: "b", DistanceMM: 100},
			{From: "b", To: "c", DistanceMM: 600},
		},
	}
}

func TestRecoatInfluenceDomainPreciseUnion(t *testing.T) {
	svc, done := newTestService(t)
	defer done()
	ctx := context.Background()

	req := standardLockRequest("t1")
	req.Members = []catalog.Member{customInfluenceMember()}
	req.PrimerMatrix = map[string]map[string]bool{"primer-a": {"coating-a": true}}
	if _, err := svc.Lock(ctx, req); err != nil {
		t.Fatalf("lock: %v", err)
	}

	// Bind batches to faces via coats (surface prep + primer first).
	if _, err := svc.SurfacePrep(ctx, "t1", "c", "p", mustJSON(t, SurfacePrepCmd{MemberID: "beam-1"}), 0, 1, 1, SurfacePrepCmd{MemberID: "beam-1"}); err != nil {
		t.Fatalf("prep: %v", err)
	}
	if _, err := svc.Primer(ctx, "t1", "c", "pr", mustJSON(t, PrimerCmd{MemberID: "beam-1"}), 0, 1, 2, PrimerCmd{MemberID: "beam-1"}); err != nil {
		t.Fatalf("primer: %v", err)
	}
	coat := func(face, batch string, clock int64) {
		cmd := CoatCmd{FaceID: face, CoatIndex: 1, SprayZoneID: "zone-1", MaterialBatchID: batch,
			AreaMM2: 1000, IssuedGrams: 10, EquipmentID: "sprayer-1", LeaseTicks: 100}
		if _, err := svc.Coat(ctx, "t1", "c", "coat-"+face, mustJSON(t, cmd), 0, 1, lease.LogicalClock(clock), cmd); err != nil {
			t.Fatalf("coat %s: %v", face, err)
		}
	}
	coat("face-1", "coating-a", 10)
	coat("face-2", "coating-a", 11)
	coat("face-3", "coating-b", 12)

	// Defect at a (zone-1, batch coating-a).
	if _, err := svc.RecordDefect(ctx, "t1", "c", "d1", mustJSON(t, DefectCmd{Kind: task.DefectThicknessLow, FaceID: "face-1", PointID: "a", MaterialBatchID: "coating-a"}), 0, 1, 20, DefectCmd{Kind: task.DefectThicknessLow, FaceID: "face-1", PointID: "a", MaterialBatchID: "coating-a"}); err != nil {
		t.Fatalf("record defect: %v", err)
	}

	scope, err := svc.EvaluateDefects(ctx, "t1", 1)
	if err != nil {
		t.Fatalf("EvaluateDefects: %v", err)
	}
	want := []string{"face-1/a", "face-2/b"}
	if len(scope.AffectedPoints) != len(want) {
		t.Fatalf("affected = %v, want %v", scope.AffectedPoints, want)
	}
	for i := range want {
		if scope.AffectedPoints[i] != want[i] {
			t.Fatalf("affected[%d] = %s, want %s", i, scope.AffectedPoints[i], want[i])
		}
	}
	if len(scope.CoatIndices) != 3 {
		t.Fatalf("coat indices = %v, want [1 2 3]", scope.CoatIndices)
	}
}

func TestRecoatGenerationIsolatesLateReceiptAndConcurrentCreation(t *testing.T) {
	svc, done := newTestService(t)
	defer done()
	taskID := lockStandard(t, svc)
	ctx := context.Background()

	// Record a defect and advance to generation 2.
	if _, err := svc.RecordDefect(ctx, taskID, "c", "d1", mustJSON(t, DefectCmd{Kind: task.DefectThicknessLow, FaceID: "face-1", PointID: "p1", MaterialBatchID: "coating-x"}), 0, 1, 10, DefectCmd{Kind: task.DefectThicknessLow, FaceID: "face-1", PointID: "p1", MaterialBatchID: "coating-x"}); err != nil {
		t.Fatalf("record defect: %v", err)
	}
	if _, err := svc.CreateRecoatGeneration(ctx, taskID, 1); err != nil {
		t.Fatalf("create recoat gen: %v", err)
	}

	// Late dry-film receipt for the old generation is rejected.
	late := DryFilmCmd{FaceID: "face-1", PointID: "p1", ValueUM: 40_000_000}
	_, err := svc.DryFilm(ctx, taskID, "c", "late", mustJSON(t, late), 0, 1, 20, late)
	assertCode(t, err, errs.CodeGenerationStale)

	// Two concurrent recoat-generation requests from generation 2: exactly one
	// wins.
	var wg sync.WaitGroup
	start := make(chan struct{})
	resCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := svc.CreateRecoatGeneration(ctx, taskID, 2)
			resCh <- err
		}()
	}
	close(start)
	wg.Wait()
	close(resCh)
	success := 0
	for err := range resCh {
		if err == nil {
			success++
		} else {
			assertCode(t, err, errs.CodeGenerationConflict)
		}
	}
	if success != 1 {
		t.Fatalf("recoat gen successes = %d, want 1", success)
	}
}

// fullAcceptanceSetup drives a task to a state satisfying every acceptance
// precondition except reviews.
func fullAcceptanceSetup(t *testing.T, svc *Service, taskID string) {
	t.Helper()
	ctx := context.Background()
	var clock int64

	if _, err := svc.SurfacePrep(ctx, taskID, "c", "p", mustJSON(t, SurfacePrepCmd{MemberID: "beam-1"}), 0, 1, lease.LogicalClock(1), SurfacePrepCmd{MemberID: "beam-1"}); err != nil {
		t.Fatalf("prep: %v", err)
	}
	if _, err := svc.Primer(ctx, taskID, "c", "pr", mustJSON(t, PrimerCmd{MemberID: "beam-1"}), 0, 1, 2, PrimerCmd{MemberID: "beam-1"}); err != nil {
		t.Fatalf("primer: %v", err)
	}
	if _, err := svc.MaterialOpen(ctx, taskID, "c", "open", mustJSON(t, MaterialOpenCmd{BatchID: "coating-x", OpenedGrams: 100000, ProofSummary: "p"}), 0, 1, 3, MaterialOpenCmd{BatchID: "coating-x", OpenedGrams: 100000, ProofSummary: "p"}); err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, face := range []string{"face-1", "face-2"} {
		for i := 1; i <= 3; i++ {
			clock++
			cmd := CoatCmd{FaceID: face, CoatIndex: i, SprayZoneID: "zone-1", MaterialBatchID: "coating-x",
				AreaMM2: 1000, IssuedGrams: 10, EquipmentID: "sprayer-1", LeaseTicks: 1000}
			if _, err := svc.Coat(ctx, taskID, "c", "coat-"+face+"-"+string(rune('0'+i)), mustJSON(t, cmd), 0, 1, lease.LogicalClock(clock), cmd); err != nil {
				t.Fatalf("coat %s/%d: %v", face, i, err)
			}
		}
		// Curing samples spanning the window (>= 20 ticks) for the last coat.
		clock += 2
		for k, off := range []int64{0, 30} {
			cmd := CuringCmd{FaceID: face, CoatIndex: 3, TempC: 20, HumidityRH: 50}
			if _, err := svc.Curing(ctx, taskID, "c", "cure-"+face+"-"+string(rune('0'+k)), mustJSON(t, cmd), 0, 1, lease.LogicalClock(clock+off), cmd); err != nil {
				t.Fatalf("curing: %v", err)
			}
		}
	}
	// Dry film above threshold for every point.
	for _, pt := range []struct{ f, p string }{{"face-1", "p1"}, {"face-1", "p2"}, {"face-1", "p3"}, {"face-2", "p4"}, {"face-2", "p5"}} {
		clock++
		cmd := DryFilmCmd{FaceID: pt.f, PointID: pt.p, ValueUM: 40_000_000}
		if _, err := svc.DryFilm(ctx, taskID, "c", "df-"+pt.p, mustJSON(t, cmd), 0, 1, lease.LogicalClock(clock), cmd); err != nil {
			t.Fatalf("dry film: %v", err)
		}
	}
	// Bond above threshold.
	clock++
	bc := BondCmd{SpecimenID: "spec-1", ValueKPa: 600_000}
	if _, err := svc.Bond(ctx, taskID, "c", "bond-1", mustJSON(t, bc), 0, 1, lease.LogicalClock(clock), bc); err != nil {
		t.Fatalf("bond: %v", err)
	}
}

func TestAcceptanceRejectsIncompleteEvidence(t *testing.T) {
	svc, done := newTestService(t)
	defer done()
	taskID := lockStandard(t, svc)
	fullAcceptanceSetup(t, svc, taskID)
	ctx := context.Background()

	// Only one reviewer: acceptance must be rejected without a credential.
	if _, err := svc.SubmitReview(ctx, taskID, "c", "r1", mustJSON(t, ReviewCmd{ReviewerID: "rev-a", Approved: true, Classes: allClasses()}), 0, 1, 100, ReviewCmd{ReviewerID: "rev-a", Approved: true, Classes: allClasses()}); err != nil {
		t.Fatalf("review: %v", err)
	}
	if _, err := svc.SubmitTerminal(ctx, taskID, TerminalCmd{State: task.TerminalAccepted, Decider: "rev-a"}); err == nil {
		t.Fatalf("acceptance with one reviewer succeeded, want rejection")
	} else {
		assertCode(t, err, errs.CodeInvalidInput)
	}
	got, _ := svc.GetTask(ctx, taskID)
	if got.Terminal != nil || got.CredentialID != "" {
		t.Fatalf("terminal/credential written on failed acceptance")
	}
}

func TestConcurrentTerminalSingleWinner(t *testing.T) {
	svc, done := newTestService(t)
	defer done()
	taskID := lockStandard(t, svc)
	fullAcceptanceSetup(t, svc, taskID)
	ctx := context.Background()

	// Two distinct qualified reviewers approving all classes.
	classes := allClasses()
	if _, err := svc.SubmitReview(ctx, taskID, "c", "r1", mustJSON(t, ReviewCmd{ReviewerID: "rev-a", Approved: true, Classes: classes}), 0, 1, 100, ReviewCmd{ReviewerID: "rev-a", Approved: true, Classes: classes}); err != nil {
		t.Fatalf("review a: %v", err)
	}
	if _, err := svc.SubmitReview(ctx, taskID, "c", "r2", mustJSON(t, ReviewCmd{ReviewerID: "rev-b", Approved: true, Classes: classes}), 0, 1, 101, ReviewCmd{ReviewerID: "rev-b", Approved: true, Classes: classes}); err != nil {
		t.Fatalf("review b: %v", err)
	}

	states := []task.TerminalState{task.TerminalAccepted, task.TerminalQuarantined, task.TerminalCancelled}
	var wg sync.WaitGroup
	start := make(chan struct{})
	resCh := make(chan error, 3)
	for _, st := range states {
		wg.Add(1)
		go func(st task.TerminalState) {
			defer wg.Done()
			<-start
			_, err := svc.SubmitTerminal(ctx, taskID, TerminalCmd{State: st, Decider: "arbiter"})
			resCh <- err
		}(st)
	}
	close(start)
	wg.Wait()
	close(resCh)
	success := 0
	for err := range resCh {
		if err == nil {
			success++
		} else {
			assertCode(t, err, errs.CodeTerminalAlreadyDecided)
		}
	}
	if success != 1 {
		t.Fatalf("terminal successes = %d, want 1", success)
	}
	got, _ := svc.GetTask(ctx, taskID)
	if got.Terminal == nil {
		t.Fatalf("expected a terminal decision")
	}
	if got.Terminal.State == task.TerminalAccepted && got.CredentialID == "" {
		t.Fatalf("accepted without credential")
	}
}

func allClasses() []string {
	return []string{
		string(task.ReviewMass), string(task.ReviewCoatPrefix), string(task.ReviewCuring),
		string(task.ReviewThickness), string(task.ReviewBond), string(task.ReviewRecoat),
	}
}
