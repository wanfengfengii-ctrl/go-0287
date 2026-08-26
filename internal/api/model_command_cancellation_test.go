package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/steel-fireproofing-evidence-closure/internal/service"
	"example.com/steel-fireproofing-evidence-closure/internal/store"
)

func TestModel_CommandCancellationIsAtomic(t *testing.T) {
	lockBody := `{
		"task_id":"cancel-task","floor":"A","fire_compartment":"F1","catalog_version":1,
		"rule_summary":"rules-v1","material_proof_summary":"proof-v1","equipment_calibration_summary":"cal-v1",
		"primer_compatibility":{"primer-a":{"coating-x":true}},
		"members":[{"id":"beam-1","floor":"A","fire_compartment":"F1","type":"beam",
			"section":{"height_mm":400,"width_mm":200},"fire_rating_minutes":60,
			"material_batch_ids":["coating-x"],"primer_batch_id":"primer-a",
			"spray_zones":[{"id":"zone-1"}],
			"exposed_faces":[{"id":"face-1","member_id":"beam-1","spray_zone_id":"zone-1",
				"boundary":{"min_x":0,"min_y":0,"max_x":1000,"max_y":1000},
				"grid_points":[{"id":"p1","face_id":"face-1","point":{"x":100,"y":100},"spray_zone_id":"zone-1"}]}]}],
		"coat_plan":{"coat_count":3,"wet_film_per_coat_um":200,"open_time_ticks":10,"curing_window_ticks":20,
			"min_dry_film_um":37500000,"max_dispersion_um":10000000,"min_bond_strength_kpa":500000}
	}`

	tests := []struct {
		name            string
		cancel          bool
		wantCommitted   bool
		wantRetryStatus int
	}{
		{name: "canceled request leaves no command residue", cancel: true, wantCommitted: false, wantRetryStatus: http.StatusOK},
		{name: "live request commits all command effects", cancel: false, wantCommitted: true, wantRetryStatus: http.StatusConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, err := store.Open(":memory:")
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			defer st.Close()
			svc := service.New(st)
			handler := NewServer(svc, nil).Handler()

			lockRec := httptest.NewRecorder()
			lockReq := httptest.NewRequest(http.MethodPost, "/v1/tasks/cancel-task/lock", strings.NewReader(lockBody))
			handler.ServeHTTP(lockRec, lockReq)
			if lockRec.Code != http.StatusOK {
				t.Fatalf("lock status = %d, body = %s", lockRec.Code, lockRec.Body.String())
			}

			commandBody := `{"expected_version":0,"generation":1,"logical_time":7,"command_type":"material_open","payload":{"batch_id":"batch-1","opened_grams":1000,"proof_summary":"proof-v1"}}`
			commandReq := httptest.NewRequest(http.MethodPost, "/v1/tasks/cancel-task/commands", strings.NewReader(commandBody))
			commandReq.Header.Set("Idempotency-Key", "open-op")
			commandReq.Header.Set("X-Caller-ID", "installer")
			if tt.cancel {
				ctx, cancel := context.WithCancel(commandReq.Context())
				cancel()
				commandReq = commandReq.WithContext(ctx)
			}
			commandRec := httptest.NewRecorder()
			handler.ServeHTTP(commandRec, commandReq)
			if tt.wantCommitted {
				if commandRec.Code != http.StatusOK {
					t.Fatalf("command status = %d, body = %s", commandRec.Code, commandRec.Body.String())
				}
				var response struct {
					BatchID string `json:"batch_id"`
					Balance int64  `json:"balance"`
				}
				if err := json.Unmarshal(commandRec.Body.Bytes(), &response); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if response.BatchID != "batch-1" || response.Balance != 1000 {
					t.Fatalf("response = %+v, want batch-1 with balance 1000", response)
				}
			} else if commandRec.Code == http.StatusOK {
				t.Fatalf("canceled command returned success: %s", commandRec.Body.String())
			}

			balance, err := svc.MaterialBalance(context.Background(), "cancel-task")
			if err != nil {
				t.Fatalf("query material balance: %v", err)
			}
			view, err := svc.GetTask(context.Background(), "cancel-task")
			if err != nil {
				t.Fatalf("query task: %v", err)
			}
			evidence, err := svc.Evidence(context.Background(), "cancel-task", 0, 100)
			if err != nil {
				t.Fatalf("query evidence: %v", err)
			}
			wantCount := 0
			wantVersion := int64(1)
			if tt.wantCommitted {
				wantCount = 1
				wantVersion = 2
			}
			if len(balance.Batches) != wantCount {
				t.Errorf("material batch count = %d, want %d", len(balance.Batches), wantCount)
			}
			if view.AggregateVersion != wantVersion {
				t.Errorf("aggregate version = %d, want %d", view.AggregateVersion, wantVersion)
			}
			if len(evidence.Events) != wantCount {
				t.Errorf("evidence event count = %d, want %d", len(evidence.Events), wantCount)
			}

			changedBody := `{"expected_version":0,"generation":1,"logical_time":8,"command_type":"material_open","payload":{"batch_id":"batch-2","opened_grams":2000,"proof_summary":"proof-v2"}}`
			retryReq := httptest.NewRequest(http.MethodPost, "/v1/tasks/cancel-task/commands", strings.NewReader(changedBody))
			retryReq.Header.Set("Idempotency-Key", "open-op")
			retryReq.Header.Set("X-Caller-ID", "installer")
			retryRec := httptest.NewRecorder()
			handler.ServeHTTP(retryRec, retryReq)
			if retryRec.Code != tt.wantRetryStatus {
				t.Fatalf("changed retry status = %d, want %d, body = %s", retryRec.Code, tt.wantRetryStatus, retryRec.Body.String())
			}
		})
	}
}
