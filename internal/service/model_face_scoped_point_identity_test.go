package service

import (
	"context"
	"reflect"
	"testing"

	"example.com/steel-fireproofing-evidence-closure/internal/catalog"
	"example.com/steel-fireproofing-evidence-closure/internal/lease"
	"example.com/steel-fireproofing-evidence-closure/internal/task"
)

func TestModel_FaceScopedPointIdentityInRecoatScope(t *testing.T) {
	ctx := context.Background()
	wantPoints := []string{"face-1/adjacent", "face-1/batch-mate", "face-1/p1", "face-1/zone-mate"}
	tests := []struct {
		name           string
		wantGeneration task.Generation
		getScope       func(*Service, string) (*task.RecoatScope, error)
	}{
		{
			name:           "preview keeps duplicate point on defect face",
			wantGeneration: 1,
			getScope: func(svc *Service, taskID string) (*task.RecoatScope, error) {
				return svc.EvaluateDefects(ctx, taskID, 1)
			},
		},
		{
			name:           "generation result uses the preview scope",
			wantGeneration: 2,
			getScope: func(svc *Service, taskID string) (*task.RecoatScope, error) {
				return svc.CreateRecoatGeneration(ctx, taskID, 1)
			},
		},
		{
			name:           "persisted reinspection scope keeps duplicate point on defect face",
			wantGeneration: 2,
			getScope: func(svc *Service, taskID string) (*task.RecoatScope, error) {
				if _, err := svc.CreateRecoatGeneration(ctx, taskID, 1); err != nil {
					return nil, err
				}
				return svc.RecoatScope(ctx, taskID, 2)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, done := newTestService(t)
			defer done()

			member := catalog.Member{
				ID: "beam-duplicate-points", Floor: "A", FireCompartment: "F1", Type: catalog.MemberBeam,
				Section: catalog.Section{HeightMM: 400, WidthMM: 200}, FireRatingMinutes: 60,
				MaterialBatchIDs: []string{"coating-x", "coating-y"}, PrimerBatchID: "primer-a",
				SprayZones: []catalog.SprayZone{{ID: "left"}, {ID: "adjacent"}, {ID: "batch"}, {ID: "right"}},
				ExposedFaces: []catalog.ExposedFace{
					{
						ID: "face-1", MemberID: "beam-duplicate-points", SprayZoneID: "left",
						Boundary: catalog.Boundary{MinX: 0, MinY: 0, MaxX: 1000, MaxY: 1000},
						GridPoints: []catalog.GridPoint{
							{ID: "p1", FaceID: "face-1", Point: catalog.Point{X: 100, Y: 100}, SprayZoneID: "left"},
							{ID: "zone-mate", FaceID: "face-1", Point: catalog.Point{X: 200, Y: 100}, SprayZoneID: "left"},
							{ID: "adjacent", FaceID: "face-1", Point: catalog.Point{X: 300, Y: 100}, SprayZoneID: "adjacent"},
							{ID: "batch-mate", FaceID: "face-1", Point: catalog.Point{X: 400, Y: 100}, SprayZoneID: "batch"},
						},
					},
					{
						ID: "face-2", MemberID: "beam-duplicate-points", SprayZoneID: "right",
						Boundary: catalog.Boundary{MinX: 0, MinY: 0, MaxX: 1000, MaxY: 1000},
						GridPoints: []catalog.GridPoint{
							{ID: "p1", FaceID: "face-2", Point: catalog.Point{X: 100, Y: 100}, SprayZoneID: "right"},
							{ID: "foreign-zone-mate", FaceID: "face-2", Point: catalog.Point{X: 200, Y: 100}, SprayZoneID: "right"},
						},
					},
				},
				AdjacencyEdges: []catalog.AdjacencyEdge{{From: "p1", To: "adjacent", DistanceMM: 100}},
			}

			req := standardLockRequest("duplicate-point-task")
			req.Members = []catalog.Member{member}
			if _, err := svc.Lock(ctx, req); err != nil {
				t.Fatalf("lock duplicate point IDs on separate faces: %v", err)
			}
			prep := SurfacePrepCmd{MemberID: member.ID}
			if _, err := svc.SurfacePrep(ctx, req.TaskID, "inspector", "prep", mustJSON(t, prep), 0, 1, 1, prep); err != nil {
				t.Fatalf("surface prep: %v", err)
			}
			primer := PrimerCmd{MemberID: member.ID}
			if _, err := svc.Primer(ctx, req.TaskID, "inspector", "primer", mustJSON(t, primer), 0, 1, 2, primer); err != nil {
				t.Fatalf("primer: %v", err)
			}
			for i, coat := range []CoatCmd{
				{FaceID: "face-1", CoatIndex: 1, SprayZoneID: "left", MaterialBatchID: "coating-x", AreaMM2: 1000, IssuedGrams: 10, EquipmentID: "sprayer-1", LeaseTicks: 100},
				{FaceID: "face-2", CoatIndex: 1, SprayZoneID: "right", MaterialBatchID: "coating-y", AreaMM2: 1000, IssuedGrams: 10, EquipmentID: "sprayer-2", LeaseTicks: 100},
			} {
				if _, err := svc.Coat(ctx, req.TaskID, "inspector", "coat-"+coat.FaceID, mustJSON(t, coat), 0, 1, lease.LogicalClock(3+i), coat); err != nil {
					t.Fatalf("coat %s: %v", coat.FaceID, err)
				}
			}
			defect := DefectCmd{Kind: task.DefectThicknessLow, FaceID: "face-1", PointID: "p1", MaterialBatchID: "coating-x"}
			if _, err := svc.RecordDefect(ctx, req.TaskID, "inspector", "defect", mustJSON(t, defect), 0, 1, 5, defect); err != nil {
				t.Fatalf("record defect: %v", err)
			}

			scope, err := tc.getScope(svc, req.TaskID)
			if err != nil {
				t.Fatalf("get recoat scope: %v", err)
			}
			if scope.Generation != tc.wantGeneration {
				t.Fatalf("generation = %d, want %d", scope.Generation, tc.wantGeneration)
			}
			if !reflect.DeepEqual(scope.AffectedPoints, wantPoints) {
				t.Fatalf("affected points = %v, want %v", scope.AffectedPoints, wantPoints)
			}
		})
	}
}
