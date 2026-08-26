package task

import (
	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/fixedpoint"
)

// DryFilmReading is one dry-film thickness measurement at a grid point, in
// micrometres (stored as a raw fixed-point value).
type DryFilmReading struct {
	FaceID     string     `json:"face_id"`
	PointID    string     `json:"point_id"`
	ValueUM    int64      `json:"value_um_raw"`
	Generation Generation `json:"generation"`
}

// BondSpecimenResult is a pull-off bond strength measurement in kilopascals.
type BondSpecimenResult struct {
	SpecimenID string     `json:"specimen_id"`
	ValueKPa   int64      `json:"value_kpa_raw"`
	Generation Generation `json:"generation"`
}

// CumulativeThickness sums a set of dry-film readings with overflow checks,
// returning the total raw fixed-point thickness in micrometres.
func CumulativeThickness(readings []int64) (fixedpoint.Value, error) {
	var acc fixedpoint.Value
	for _, r := range readings {
		v, err := fixedpoint.Add(acc, fixedpoint.New(r))
		if err != nil {
			return 0, errs.New(errs.CodeFixedPointOverflow, "cumulative thickness overflow")
		}
		acc = v
	}
	return acc, nil
}

// Dispersion returns max-min across a set of readings; a single reading has
// zero dispersion.
func Dispersion(readings []int64) (fixedpoint.Value, error) {
	if len(readings) == 0 {
		return 0, errs.New(errs.CodeInvalidInput, "no readings for dispersion")
	}
	minV, maxV := readings[0], readings[0]
	for _, r := range readings[1:] {
		if r < minV {
			minV = r
		}
		if r > maxV {
			maxV = r
		}
	}
	return fixedpoint.Sub(fixedpoint.New(maxV), fixedpoint.New(minV))
}

// UnitAreaUsage returns grams-per-square-millimetre of issued coating over an
// area, scaled to a fixed-point value.
func UnitAreaUsage(issuedGrams int64, areaMM2 int64) (fixedpoint.Value, error) {
	if areaMM2 <= 0 {
		return 0, errs.New(errs.CodeFixedPointOverflow, "area must be positive")
	}
	return fixedpoint.Div(fixedpoint.New(issuedGrams), fixedpoint.New(areaMM2))
}

// BondStrength returns kilopascals of pull-off force over an area, as a
// fixed-point value.
func BondStrength(forceN int64, areaMM2 int64) (fixedpoint.Value, error) {
	if areaMM2 <= 0 {
		return 0, errs.New(errs.CodeFixedPointOverflow, "area must be positive")
	}
	return fixedpoint.Div(fixedpoint.New(forceN), fixedpoint.New(areaMM2))
}
