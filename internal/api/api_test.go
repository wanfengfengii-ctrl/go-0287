package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/service"
	"example.com/steel-fireproofing-evidence-closure/internal/store"
)

func newTestServer(t *testing.T) (*Server, func()) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return NewServer(service.New(st), nil), func() { st.Close() }
}

func TestHealthLive(t *testing.T) {
	s, done := newTestServer(t)
	defer done()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("live status = %d, want 200", rec.Code)
	}
}

func TestHealthReadyNotReady(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	s := NewServer(service.New(st), func() bool { return false })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready status = %d, want 503", rec.Code)
	}
}

func TestLockComputesSectionFactorAndThickness(t *testing.T) {
	s, done := newTestServer(t)
	defer done()
	body := `{
		"task_id": "t1", "floor": "A", "fire_compartment": "F1", "catalog_version": 1,
		"rule_summary": "public integer section-factor and thickness rules v1",
		"material_proof_summary": "mp", "equipment_calibration_summary": "ec",
		"primer_compatibility": {"primer-a": {"coating-x": true}},
		"members": [{
			"id": "beam-1", "floor": "A", "fire_compartment": "F1", "type": "beam",
			"section": {"height_mm": 400, "width_mm": 200}, "fire_rating_minutes": 60,
			"material_batch_ids": ["coating-x"], "primer_batch_id": "primer-a",
			"spray_zones": [{"id": "zone-1"}],
			"exposed_faces": [{"id": "face-1", "member_id": "beam-1", "spray_zone_id": "zone-1",
				"boundary": {"min_x": 0, "min_y": 0, "max_x": 1000, "max_y": 1000},
				"grid_points": [{"id": "p1", "face_id": "face-1", "point": {"x": 100, "y": 100}, "spray_zone_id": "zone-1"}]}]
		}],
		"coat_plan": {"coat_count": 3, "wet_film_per_coat_um": 200, "open_time_ticks": 10,
			"curing_window_ticks": 20, "min_dry_film_um": 37500000, "max_dispersion_um": 10000000,
			"min_bond_strength_kpa": 500000}
	}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/t1/lock", strings.NewReader(body))
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("lock status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp service.LockResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Members) != 1 {
		t.Fatalf("members = %d, want 1", len(resp.Members))
	}
	// Section factor 15 1/m, thickness 37.5 um.
	if got := resp.Members[0].SectionFactor; got != 15_000_000 {
		t.Fatalf("SectionFactor = %d, want 15000000", got)
	}
	if got := resp.Members[0].TargetThickness; got != 37_500_000 {
		t.Fatalf("TargetThickness = %d, want 37500000", got)
	}
}

func TestCommandRequiresIdempotencyKey(t *testing.T) {
	s, done := newTestServer(t)
	defer done()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/t1/commands", strings.NewReader(`{}`))
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestErrorEnvelopeShape(t *testing.T) {
	rec := httptest.NewRecorder()
	de := errs.New(errs.CodePointDuplicate, "duplicate point").
		WithReasons("floor=A", "compartment=F1", "point=P1").
		WithOperation("op-1").
		WithVersion(3)
	writeError(rec, http.StatusBadRequest, de)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var env ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if env.Code != "POINT_DUPLICATE" {
		t.Fatalf("code = %q, want POINT_DUPLICATE", env.Code)
	}
	if len(env.OrderedReasons) != 3 || env.OrderedReasons[0] != "floor=A" {
		t.Fatalf("ordered_reasons = %v", env.OrderedReasons)
	}
}
