package catalog

import (
	"testing"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/fixedpoint"
)

func TestSectionFactorKnownValue(t *testing.T) {
	// Rectangular 400x200 mm section: perimeter 1200 mm, area 80000 mm²,
	// section factor = 1200*1000/80000 = 15 1/m.
	s := Section{HeightMM: 400, WidthMM: 200}
	sf, err := s.SectionFactor()
	if err != nil {
		t.Fatalf("SectionFactor: %v", err)
	}
	if want := fixedpoint.New(15 * fixedpoint.Scale); sf != want {
		t.Fatalf("SectionFactor = %d, want %d", sf.Raw(), want.Raw())
	}
}

func TestTargetThicknessKnownValue(t *testing.T) {
	// Section factor 15 1/m with 60-minute rating: 2.5 * 15 = 37.5 µm.
	sf := fixedpoint.New(15 * fixedpoint.Scale)
	th, err := TargetThickness(60, sf)
	if err != nil {
		t.Fatalf("TargetThickness: %v", err)
	}
	if want := fixedpoint.New(37_500_000); th != want {
		t.Fatalf("TargetThickness = %d, want %d", th.Raw(), want.Raw())
	}
}

func TestTargetThicknessUnknownRating(t *testing.T) {
	sf := fixedpoint.New(fixedpoint.Scale)
	_, err := TargetThickness(45, sf)
	de, ok := err.(*errs.Error)
	if !ok || de.Code != errs.CodeFireRatingMismatch {
		t.Fatalf("unknown rating error = %v, want CodeFireRatingMismatch", err)
	}
}

func TestSectionRejectsNonPositiveDimensions(t *testing.T) {
	for _, s := range []Section{
		{HeightMM: 0, WidthMM: 200},
		{HeightMM: 400, WidthMM: 0},
		{HeightMM: -1, WidthMM: 200},
	} {
		if _, err := s.SectionFactor(); err == nil {
			t.Fatalf("SectionFactor(%+v) = nil, want error", s)
		}
	}
}

func TestCatalogSnapshotPrimerCompatibility(t *testing.T) {
	snap := CatalogSnapshot{
		PrimerCompatibility: map[string]map[string]bool{
			"primer-a": {"coating-x": true, "coating-y": false},
		},
	}
	if !snap.PrimerCompatible("primer-a", "coating-x") {
		t.Fatalf("expected primer-a/coating-x compatible")
	}
	if snap.PrimerCompatible("primer-a", "coating-y") {
		t.Fatalf("expected primer-a/coating-y incompatible")
	}
	if snap.PrimerCompatible("primer-b", "coating-x") {
		t.Fatalf("expected unknown primer incompatible")
	}
}
