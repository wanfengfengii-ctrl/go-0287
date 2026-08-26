package catalog

import (
	"strings"
	"testing"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
)

func lockSnapshot() CatalogSnapshot {
	return CatalogSnapshot{
		Version:              1,
		RuleSummary:          "public integer section-factor and thickness rules v1",
		MaterialProofSummary: "material-proof-v1",
		PrimerCompatibility:  map[string]map[string]bool{"primer-a": {"coating-x": true}},
		ThicknessRuleVersion: 1,
	}
}

func lockPlan() CoatPlan {
	return CoatPlan{
		CoatCount: 3, WetFilmPerCoatUM: 200, OpenTimeTicks: 10, CuringWindowTicks: 20,
		MinDryFilmUM: 37_500_000, MaxDispersionUM: 10_000_000, MinBondStrengthKPa: 500_000,
	}
}

func beamMember() Member {
	return Member{
		ID: "beam-1", Floor: "A", FireCompartment: "F1", Type: MemberBeam,
		Section: Section{HeightMM: 400, WidthMM: 200}, FireRatingMinutes: 60,
		MaterialBatchIDs: []string{"coating-x"}, PrimerBatchID: "primer-a",
		SprayZones: []SprayZone{{ID: "zone-1"}},
		ExposedFaces: []ExposedFace{{
			ID: "face-1", MemberID: "beam-1", SprayZoneID: "zone-1",
			Boundary: Boundary{MinX: 0, MinY: 0, MaxX: 1000, MaxY: 1000},
			GridPoints: []GridPoint{
				{ID: "p1", FaceID: "face-1", Point: Point{X: 100, Y: 100}, SprayZoneID: "zone-1"},
				{ID: "p2", FaceID: "face-1", Point: Point{X: 200, Y: 200}, SprayZoneID: "zone-1"},
			},
		}},
	}
}

func TestLockScopeComputesSectionFactorAndThickness(t *testing.T) {
	locked, err := ValidateLockScope("A", "F1", []Member{beamMember()}, lockSnapshot(), lockPlan())
	if err != nil {
		t.Fatalf("ValidateLockScope: %v", err)
	}
	if len(locked) != 1 {
		t.Fatalf("locked members = %d, want 1", len(locked))
	}
	lm := locked[0]
	// Section factor 15 1/m, target thickness 37.5 um.
	if lm.SectionFactor != 15_000_000 {
		t.Fatalf("SectionFactor = %d, want 15000000", lm.SectionFactor)
	}
	if lm.TargetThickness != 37_500_000 {
		t.Fatalf("TargetThickness = %d, want 37500000", lm.TargetThickness)
	}
}

func TestLockScopeRejectsDegenerateSection(t *testing.T) {
	for name, sec := range map[string]Section{
		"zero_height": {HeightMM: 0, WidthMM: 200},
		"negative":    {HeightMM: -1, WidthMM: 200},
		"overlong":    {HeightMM: 1 << 21, WidthMM: 200},
		"overflow":    {HeightMM: 1 << 30, WidthMM: 1 << 30},
	} {
		m := beamMember()
		m.Section = sec
		_, err := ValidateLockScope("A", "F1", []Member{m}, lockSnapshot(), lockPlan())
		if err == nil {
			t.Fatalf("%s: expected validation error", name)
		}
		de, ok := err.(*errs.Error)
		if !ok {
			t.Fatalf("%s: error is not *errs.Error: %v", name, err)
		}
		if de.Code != errs.CodeFixedPointOverflow && de.Code != errs.CodeInvalidInput {
			t.Fatalf("%s: code = %s, want FIXED_POINT_OVERFLOW or INVALID_INPUT", name, de.Code)
		}
	}
}

func TestLockScopeOrdersRejectionReasons(t *testing.T) {
	// A member with a duplicate point and a point outside its boundary.
	m := beamMember()
	m.ExposedFaces[0].GridPoints = append(m.ExposedFaces[0].GridPoints,
		GridPoint{ID: "p1", FaceID: "face-1", Point: Point{X: 100, Y: 100}, SprayZoneID: "zone-1"},
		GridPoint{ID: "p9", FaceID: "face-1", Point: Point{X: 9999, Y: 9999}, SprayZoneID: "zone-1"},
	)
	_, err := ValidateLockScope("A", "F1", []Member{m}, lockSnapshot(), lockPlan())
	if err == nil {
		t.Fatalf("expected validation error")
	}
	de, ok := err.(*errs.Error)
	if !ok {
		t.Fatalf("error is not *errs.Error: %v", err)
	}
	if len(de.Reasons) < 2 {
		t.Fatalf("reasons = %v, want at least 2", de.Reasons)
	}
	// Reasons must be sorted ascending (they share the same floor/compartment
	// prefix so ordering is determined by member/face/point).
	if !sortedStrings(de.Reasons) {
		t.Fatalf("reasons not sorted: %v", de.Reasons)
	}
	if !strings.Contains(de.Reasons[0], "POINT_DUPLICATE") && !strings.Contains(de.Reasons[0], "FACE_INCOMPLETE") {
		t.Fatalf("unexpected first reason: %s", de.Reasons[0])
	}
}

func sortedStrings(s []string) bool {
	for i := 1; i < len(s); i++ {
		if s[i-1] > s[i] {
			return false
		}
	}
	return true
}
