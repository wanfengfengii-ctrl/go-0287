package service

import (
	"context"
	"testing"

	"example.com/steel-fireproofing-evidence-closure/internal/catalog"
	"example.com/steel-fireproofing-evidence-closure/internal/store"
)

// newTestService opens an in-memory store and returns a service plus a cleanup
// function.
func newTestService(t *testing.T) (*Service, func()) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return New(st), func() { st.Close() }
}

// standardMember returns the canonical fixture: floor A, compartment F1, an
// H-beam 400x200 mm with a 60-minute rating (section factor 15 1/m, target
// thickness 37.5 um), two exposed faces and a three-coat plan.
func standardMember() catalog.Member {
	return catalog.Member{
		ID:                "beam-1",
		Floor:             "A",
		FireCompartment:   "F1",
		Type:              catalog.MemberBeam,
		Section:           catalog.Section{HeightMM: 400, WidthMM: 200},
		FireRatingMinutes: 60,
		MaterialBatchIDs:  []string{"coating-x"},
		PrimerBatchID:     "primer-a",
		SprayZones: []catalog.SprayZone{
			{ID: "zone-1"}, {ID: "zone-2"},
		},
		ExposedFaces: []catalog.ExposedFace{
			{
				ID: "face-1", MemberID: "beam-1", SprayZoneID: "zone-1",
				Boundary: catalog.Boundary{MinX: 0, MinY: 0, MaxX: 1000, MaxY: 1000},
				GridPoints: []catalog.GridPoint{
					{ID: "p1", FaceID: "face-1", Point: catalog.Point{X: 100, Y: 100}, SprayZoneID: "zone-1"},
					{ID: "p2", FaceID: "face-1", Point: catalog.Point{X: 200, Y: 200}, SprayZoneID: "zone-1"},
					{ID: "p3", FaceID: "face-1", Point: catalog.Point{X: 300, Y: 300}, SprayZoneID: "zone-1"},
				},
			},
			{
				ID: "face-2", MemberID: "beam-1", SprayZoneID: "zone-2",
				Boundary: catalog.Boundary{MinX: 0, MinY: 0, MaxX: 1000, MaxY: 1000},
				GridPoints: []catalog.GridPoint{
					{ID: "p4", FaceID: "face-2", Point: catalog.Point{X: 100, Y: 100}, SprayZoneID: "zone-2"},
					{ID: "p5", FaceID: "face-2", Point: catalog.Point{X: 200, Y: 200}, SprayZoneID: "zone-2"},
				},
			},
		},
		AdjacencyEdges: []catalog.AdjacencyEdge{
			{From: "p1", To: "p2", DistanceMM: 100},
			{From: "p2", To: "p3", DistanceMM: 100},
		},
	}
}

// standardLockRequest builds the canonical lock request for the standard
// member and plan.
func standardLockRequest(taskID string) LockRequest {
	return LockRequest{
		TaskID:          taskID,
		Floor:           "A",
		FireCompartment: "F1",
		CatalogVersion:  1,
		RuleSummary:     "public integer section-factor and thickness rules v1",
		MaterialProof:   "material-proof-v1",
		Calibration:     "calibration-v1",
		PrimerMatrix: map[string]map[string]bool{
			"primer-a": {"coating-x": true},
		},
		Members: []catalog.Member{standardMember()},
		Plan: catalog.CoatPlan{
			CoatCount:          3,
			WetFilmPerCoatUM:   200,
			OpenTimeTicks:      10,
			CuringWindowTicks:  20,
			MinDryFilmUM:       37_500_000,
			MaxDispersionUM:    10_000_000,
			MinBondStrengthKPa: 500_000,
		},
	}
}

// lockStandard locks the canonical fixture and returns the task id.
func lockStandard(t *testing.T, svc *Service) string {
	t.Helper()
	ctx := context.Background()
	res, err := svc.Lock(ctx, standardLockRequest("t1"))
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	return res.TaskID
}
