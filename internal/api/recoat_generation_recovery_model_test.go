package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"example.com/steel-fireproofing-evidence-closure/internal/api"
	"example.com/steel-fireproofing-evidence-closure/internal/service"
	"example.com/steel-fireproofing-evidence-closure/internal/store"
	"example.com/steel-fireproofing-evidence-closure/internal/task"
)

type recoatModelFixture struct {
	handler http.Handler
	close   func()
}

func newRecoatModelFixture(t *testing.T) recoatModelFixture {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	f := recoatModelFixture{handler: api.NewServer(service.New(st), nil).Handler(), close: func() { _ = st.Close() }}

	lockBody := []byte(`{
		"task_id":"recoat-recovery","floor":"A","fire_compartment":"F1","catalog_version":1,
		"rule_summary":"rules-v1","material_proof_summary":"proof-v1","equipment_calibration_summary":"calibration-v1",
		"primer_compatibility":{"primer-a":{"coating-x":true}},
		"members":[{"id":"beam-1","floor":"A","fire_compartment":"F1","type":"beam",
			"section":{"height_mm":400,"width_mm":200},"fire_rating_minutes":60,
			"material_batch_ids":["coating-x"],"primer_batch_id":"primer-a",
			"spray_zones":[{"id":"zone-1"}],
			"exposed_faces":[{"id":"face-1","member_id":"beam-1","spray_zone_id":"zone-1",
				"boundary":{"min_x":0,"min_y":0,"max_x":1000,"max_y":1000},
				"grid_points":[
					{"id":"p3","face_id":"face-1","point":{"x":300,"y":300},"spray_zone_id":"zone-1"},
					{"id":"p1","face_id":"face-1","point":{"x":100,"y":100},"spray_zone_id":"zone-1"},
					{"id":"p2","face_id":"face-1","point":{"x":200,"y":200},"spray_zone_id":"zone-1"}
				]}]}],
		"coat_plan":{"coat_count":3,"wet_film_per_coat_um":200,"open_time_ticks":10,"curing_window_ticks":20,
			"min_dry_film_um":37500000,"max_dispersion_um":10000000,"min_bond_strength_kpa":500000}
	}`)
	mustModelRequest(t, f.handler, http.MethodPost, "/v1/tasks/recoat-recovery/lock", "", lockBody, http.StatusOK)

	defectBody := []byte(`{"expected_version":1,"generation":1,"logical_time":10,"command_type":"defect","payload":{"kind":"thickness_low","point_id":"p1","face_id":"face-1","material_batch_id":"coating-x"}}`)
	mustModelRequest(t, f.handler, http.MethodPost, "/v1/tasks/recoat-recovery/commands", "defect-op", defectBody, http.StatusOK)
	return f
}

func modelRequest(handler http.Handler, method, path, opID string, body []byte) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if opID != "" {
		req.Header.Set("Idempotency-Key", opID)
		req.Header.Set("X-Caller-ID", "recoat-client")
	}
	handler.ServeHTTP(rec, req)
	return rec
}

func mustModelRequest(t *testing.T, handler http.Handler, method, path, opID string, body []byte, wantStatus int) *httptest.ResponseRecorder {
	t.Helper()
	rec := modelRequest(handler, method, path, opID, body)
	if rec.Code != wantStatus {
		t.Fatalf("%s %s status = %d, want %d; body=%s", method, path, rec.Code, wantStatus, rec.Body.String())
	}
	return rec
}

func decodeModelJSON[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(rec.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return value
}

func TestModel_RecoatGenerationRecovery(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, f recoatModelFixture)
	}{
		{
			name: "lost response replays the committed generation with its version and evidence",
			run: func(t *testing.T, f recoatModelFixture) {
				body := []byte(`{"from_generation":1}`)
				first := mustModelRequest(t, f.handler, http.MethodPost, "/v1/tasks/recoat-recovery/recoat-generations", "generation-op", body, http.StatusOK)
				want := decodeModelJSON[task.RecoatScope](t, first)
				if want.Generation != 2 || !reflect.DeepEqual(want.AffectedPoints, []string{"face-1/p1", "face-1/p2", "face-1/p3"}) {
					t.Fatalf("created scope = %+v, want generation 2 with sorted affected points", want)
				}

				// The first response is intentionally ignored, as if the client disconnected.
				replay := mustModelRequest(t, f.handler, http.MethodPost, "/v1/tasks/recoat-recovery/recoat-generations", "generation-op", body, http.StatusOK)
				got := decodeModelJSON[task.RecoatScope](t, replay)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("replayed result = %+v, want original %+v", got, want)
				}

				taskView := decodeModelJSON[service.TaskView](t, mustModelRequest(t, f.handler, http.MethodGet, "/v1/tasks/recoat-recovery", "", nil, http.StatusOK))
				if taskView.CurrentGeneration != 2 || taskView.AggregateVersion != 3 {
					t.Fatalf("task generation/version = %d/%d, want 2/3", taskView.CurrentGeneration, taskView.AggregateVersion)
				}
				saved := decodeModelJSON[task.RecoatScope](t, mustModelRequest(t, f.handler, http.MethodGet, "/v1/tasks/recoat-recovery/recoat-scope?generation=2", "", nil, http.StatusOK))
				if !reflect.DeepEqual(saved.AffectedPoints, want.AffectedPoints) {
					t.Fatalf("saved affected points = %v, want unchanged ordering %v", saved.AffectedPoints, want.AffectedPoints)
				}

				evidence := decodeModelJSON[service.EvidenceView](t, mustModelRequest(t, f.handler, http.MethodGet, "/v1/tasks/recoat-recovery/evidence?after=0&limit=100", "", nil, http.StatusOK))
				if len(evidence.Events) != 2 {
					t.Fatalf("evidence events = %d, want defect plus one generation event", len(evidence.Events))
				}
				created := evidence.Events[1]
				if created.OperationID != "generation-op" || created.Generation != 2 || string(created.Type) != "recoat_generation" {
					t.Fatalf("generation evidence = %+v, want operation generation-op, generation 2, type recoat_generation", created)
				}
			},
		},
		{
			name: "different concurrent operations retain a single generation winner",
			run: func(t *testing.T, f recoatModelFixture) {
				body := []byte(`{"from_generation":1}`)
				start := make(chan struct{})
				statuses := make(chan int, 2)
				var wg sync.WaitGroup
				for _, opID := range []string{"generation-a", "generation-b"} {
					wg.Add(1)
					go func(opID string) {
						defer wg.Done()
						<-start
						statuses <- modelRequest(f.handler, http.MethodPost, "/v1/tasks/recoat-recovery/recoat-generations", opID, body).Code
					}(opID)
				}
				close(start)
				wg.Wait()
				close(statuses)
				counts := map[int]int{}
				for status := range statuses {
					counts[status]++
				}
				if counts[http.StatusOK] != 1 || counts[http.StatusConflict] != 1 {
					t.Fatalf("concurrent statuses = %v, want one 200 and one 409", counts)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newRecoatModelFixture(t)
			defer f.close()
			tt.run(t, f)
		})
	}
}
