package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"example.com/steel-fireproofing-evidence-closure/internal/api"
	"example.com/steel-fireproofing-evidence-closure/internal/catalog"
	"example.com/steel-fireproofing-evidence-closure/internal/service"
	"example.com/steel-fireproofing-evidence-closure/internal/store"
)

func TestModel_CrossTaskMaterialIssueSharesPhysicalResources(t *testing.T) {
	tests := []struct {
		name           string
		batchIDs       [2]string
		equipmentIDs   [2]string
		openedGrams    int64
		issuedGrams    int64
		wantRejectCode string
	}{
		{
			name:           "last material is shared across open tasks",
			batchIDs:       [2]string{"coating-x", "coating-x"},
			equipmentIDs:   [2]string{"sprayer-a", "sprayer-b"},
			openedGrams:    100,
			issuedGrams:    100,
			wantRejectCode: "MATERIAL_INSUFFICIENT",
		},
		{
			name:           "equipment lease is shared across open tasks",
			batchIDs:       [2]string{"coating-a", "coating-b"},
			equipmentIDs:   [2]string{"sprayer-1", "sprayer-1"},
			openedGrams:    100,
			issuedGrams:    10,
			wantRejectCode: "LEASE_BUSY",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, err := store.Open(":memory:")
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			defer st.Close()
			h := api.NewServer(service.New(st), nil).Handler()

			doJSON := func(method, path, caller, opID string, body any) *httptest.ResponseRecorder {
				t.Helper()
				encoded, err := json.Marshal(body)
				if err != nil {
					t.Fatalf("marshal request: %v", err)
				}
				req := httptest.NewRequest(method, path, bytes.NewReader(encoded))
				if caller != "" {
					req.Header.Set("X-Caller-ID", caller)
				}
				if opID != "" {
					req.Header.Set("Idempotency-Key", opID)
				}
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				return rec
			}

			command := func(taskID, caller, opID, commandType string, logicalTime int64, payload any) *httptest.ResponseRecorder {
				t.Helper()
				return doJSON(http.MethodPost, "/v1/tasks/"+taskID+"/commands", caller, opID, map[string]any{
					"expected_version": int64(0),
					"generation":       int64(1),
					"logical_time":     logicalTime,
					"command_type":     commandType,
					"payload":          payload,
				})
			}

			taskIDs := [2]string{"task-a", "task-b"}
			callers := [2]string{"client-a", "client-b"}
			for i, taskID := range taskIDs {
				memberID := fmt.Sprintf("beam-%d", i+1)
				faceID := fmt.Sprintf("face-%d", i+1)
				pointID := fmt.Sprintf("point-%d", i+1)
				lock := service.LockRequest{
					TaskID:          taskID,
					Floor:           "A",
					FireCompartment: "F1",
					CatalogVersion:  1,
					RuleSummary:     "public integer section-factor and thickness rules v1",
					MaterialProof:   "proof-v1",
					Calibration:     "calibration-v1",
					PrimerMatrix: map[string]map[string]bool{
						"primer-a": {tc.batchIDs[i]: true},
					},
					Members: []catalog.Member{{
						ID: memberID, Floor: "A", FireCompartment: "F1", Type: catalog.MemberBeam,
						Section: catalog.Section{HeightMM: 400, WidthMM: 200}, FireRatingMinutes: 60,
						MaterialBatchIDs: []string{tc.batchIDs[i]}, PrimerBatchID: "primer-a",
						SprayZones: []catalog.SprayZone{{ID: "zone-1"}},
						ExposedFaces: []catalog.ExposedFace{{
							ID: faceID, MemberID: memberID, SprayZoneID: "zone-1",
							Boundary: catalog.Boundary{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100},
							GridPoints: []catalog.GridPoint{{
								ID: pointID, FaceID: faceID, Point: catalog.Point{X: 10, Y: 10}, SprayZoneID: "zone-1",
							}},
						}},
					}},
					Plan: catalog.CoatPlan{
						CoatCount: 1, WetFilmPerCoatUM: 200, OpenTimeTicks: 10, CuringWindowTicks: 20,
						MinDryFilmUM: 1, MaxDispersionUM: 1, MinBondStrengthKPa: 1,
					},
				}
				if rec := doJSON(http.MethodPost, "/v1/tasks/"+taskID+"/lock", "", "", lock); rec.Code != http.StatusOK {
					t.Fatalf("lock %s: status=%d body=%s", taskID, rec.Code, rec.Body.String())
				}
				openPayload := map[string]any{
					"batch_id": tc.batchIDs[i], "opened_grams": tc.openedGrams, "proof_summary": "proof-v1",
				}
				if rec := command(taskID, callers[i], "open-"+taskID, "material_open", 1, openPayload); rec.Code != http.StatusOK {
					t.Fatalf("open material for %s: status=%d body=%s", taskID, rec.Code, rec.Body.String())
				}
			}

			issueBodies := [2]map[string]any{}
			for i := range taskIDs {
				issueBodies[i] = map[string]any{
					"expected_version": int64(0),
					"generation":       int64(1),
					"logical_time":     int64(10),
					"command_type":     "material_issue",
					"payload": map[string]any{
						"batch_id": tc.batchIDs[i], "grams": tc.issuedGrams,
						"equipment_id": tc.equipmentIDs[i], "lease_ticks": int64(100),
					},
				}
			}

			type result struct {
				index int
				rec   *httptest.ResponseRecorder
			}
			start := make(chan struct{})
			results := make(chan result, 2)
			var wg sync.WaitGroup
			for i := range taskIDs {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					results <- result{i, doJSON(http.MethodPost, "/v1/tasks/"+taskIDs[i]+"/commands", callers[i], "issue-"+taskIDs[i], issueBodies[i])}
				}(i)
			}
			close(start)
			wg.Wait()
			close(results)

			winner, loser := -1, -1
			for got := range results {
				switch got.rec.Code {
				case http.StatusOK:
					if winner != -1 {
						t.Fatalf("both cross-task issues succeeded; second success body=%s", got.rec.Body.String())
					}
					winner = got.index
				case http.StatusConflict:
					var envelope api.ErrorEnvelope
					if err := json.Unmarshal(got.rec.Body.Bytes(), &envelope); err != nil {
						t.Fatalf("decode rejection: %v", err)
					}
					if envelope.Code != tc.wantRejectCode {
						t.Fatalf("rejection code=%q, want %q; body=%s", envelope.Code, tc.wantRejectCode, got.rec.Body.String())
					}
					loser = got.index
				default:
					t.Fatalf("issue for %s: status=%d body=%s", taskIDs[got.index], got.rec.Code, got.rec.Body.String())
				}
			}
			if winner == -1 || loser == -1 {
				t.Fatalf("want exactly one success and one rejection, winner=%d loser=%d", winner, loser)
			}

			var losingBalance service.MaterialBalanceView
			balanceRec := doJSON(http.MethodGet, "/v1/tasks/"+taskIDs[loser]+"/material-balance", "", "", nil)
			if balanceRec.Code != http.StatusOK {
				t.Fatalf("losing balance query: status=%d body=%s", balanceRec.Code, balanceRec.Body.String())
			}
			if err := json.Unmarshal(balanceRec.Body.Bytes(), &losingBalance); err != nil {
				t.Fatalf("decode losing balance: %v", err)
			}
			for _, entry := range losingBalance.Ledger {
				if entry.OperationID == "issue-"+taskIDs[loser] {
					t.Fatalf("rejected issue left a ledger entry: %+v", entry)
				}
			}
			if tc.wantRejectCode == "LEASE_BUSY" {
				if len(losingBalance.Batches) != 1 || losingBalance.Batches[0].IssuedGrams != 0 || losingBalance.Batches[0].Balance() != tc.openedGrams {
					t.Fatalf("rejected lease transaction changed material: %+v", losingBalance.Batches)
				}
			}

			leaseProbe := command(taskIDs[loser], callers[loser], "probe-"+taskIDs[loser], "device", 11, map[string]any{
				"invocation_id": "probe-" + taskIDs[loser], "equipment_id": tc.equipmentIDs[loser], "kind": "sprayer",
			})
			var leaseEnvelope api.ErrorEnvelope
			if err := json.Unmarshal(leaseProbe.Body.Bytes(), &leaseEnvelope); err != nil {
				t.Fatalf("decode lease probe: %v", err)
			}
			if leaseProbe.Code != http.StatusBadRequest || leaseEnvelope.Code != "LEASE_EXPIRED" {
				t.Fatalf("rejected issue left a usable lease: status=%d code=%q body=%s", leaseProbe.Code, leaseEnvelope.Code, leaseProbe.Body.String())
			}

			replay := doJSON(http.MethodPost, "/v1/tasks/"+taskIDs[winner]+"/commands", callers[winner], "issue-"+taskIDs[winner], issueBodies[winner])
			if replay.Code != http.StatusOK {
				t.Fatalf("identical replay: status=%d body=%s", replay.Code, replay.Body.String())
			}
			conflictingBody := issueBodies[winner]
			conflictingPayload := map[string]any{
				"batch_id": tc.batchIDs[winner], "grams": tc.issuedGrams + 1,
				"equipment_id": tc.equipmentIDs[winner], "lease_ticks": int64(100),
			}
			conflictingBody["payload"] = conflictingPayload
			conflict := doJSON(http.MethodPost, "/v1/tasks/"+taskIDs[winner]+"/commands", callers[winner], "issue-"+taskIDs[winner], conflictingBody)
			var conflictEnvelope api.ErrorEnvelope
			if err := json.Unmarshal(conflict.Body.Bytes(), &conflictEnvelope); err != nil {
				t.Fatalf("decode idempotency conflict: %v", err)
			}
			if conflict.Code != http.StatusConflict || conflictEnvelope.Code != "IDEMPOTENCY_CONFLICT" {
				t.Fatalf("changed replay: status=%d code=%q body=%s", conflict.Code, conflictEnvelope.Code, conflict.Body.String())
			}

			var winningBalance service.MaterialBalanceView
			winnerBalanceRec := doJSON(http.MethodGet, "/v1/tasks/"+taskIDs[winner]+"/material-balance", "", "", nil)
			if err := json.Unmarshal(winnerBalanceRec.Body.Bytes(), &winningBalance); err != nil {
				t.Fatalf("decode winning balance: %v", err)
			}
			issueEntries := 0
			for _, entry := range winningBalance.Ledger {
				if entry.OperationID == "issue-"+taskIDs[winner] {
					issueEntries++
				}
			}
			if issueEntries != 1 {
				t.Fatalf("winning operation ledger entries=%d, want 1; ledger=%+v", issueEntries, winningBalance.Ledger)
			}
			for _, batch := range winningBalance.Batches {
				if !batch.Conserved() {
					t.Fatalf("winning batch does not conserve material: %+v", batch)
				}
			}
		})
	}
}
