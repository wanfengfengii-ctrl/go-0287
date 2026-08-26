// Package catalog implements the "构件与防火材料规则目录" component: it owns
// the immutable domain description of floors, fire compartments, members,
// exposed faces, the point grid and spray zones, together with the public
// integer rules that compute section factors and target dry-film thickness.
package catalog

import (
	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/fixedpoint"
)

// MemberType discriminates a steel member between a beam and a column.
type MemberType string

const (
	MemberBeam   MemberType = "beam"
	MemberColumn MemberType = "column"
)

// Section is the integer cross-section of a steel member. Dimensions are in
// millimetres and must be strictly positive.
type Section struct {
	HeightMM int64 `json:"height_mm"` // total section height
	WidthMM  int64 `json:"width_mm"`  // flange width (rectangular simplification)
}

// Validate rejects degenerate, negative, zero or over-long dimensions.
func (s Section) Validate() error {
	if s.HeightMM <= 0 || s.WidthMM <= 0 {
		return errs.New(errs.CodeInvalidInput, "section dimensions must be positive")
	}
	if s.HeightMM > maxDimension || s.WidthMM > maxDimension {
		return errs.New(errs.CodeFixedPointOverflow, "section dimension out of range")
	}
	return nil
}

// Perimeter returns the heated perimeter in millimetres: 2*(h+w).
func (s Section) Perimeter() (int64, error) {
	if err := s.Validate(); err != nil {
		return 0, err
	}
	if s.HeightMM > (maxInt64/2)-s.WidthMM {
		return 0, errs.New(errs.CodeFixedPointOverflow, "perimeter overflows int64")
	}
	return 2 * (s.HeightMM + s.WidthMM), nil
}

// Area returns the cross-sectional area in square millimetres: h*w.
func (s Section) Area() (int64, error) {
	if err := s.Validate(); err != nil {
		return 0, err
	}
	if s.HeightMM > maxInt64/s.WidthMM {
		return 0, errs.New(errs.CodeFixedPointOverflow, "area overflows int64")
	}
	return s.HeightMM * s.WidthMM, nil
}

// SectionFactor returns the heated-perimeter to area ratio in 1/m, computed
// with fixed-point half-away-from-zero rounding.
func (s Section) SectionFactor() (fixedpoint.Value, error) {
	p, err := s.Perimeter()
	if err != nil {
		return 0, err
	}
	a, err := s.Area()
	if err != nil {
		return 0, err
	}
	// perimeter[mm]/area[mm²] = 1/mm; multiply by 1000 to obtain 1/m.
	if p > maxInt64/1000 {
		return 0, errs.New(errs.CodeFixedPointOverflow, "section factor numerator overflows int64")
	}
	v, err := fixedpoint.Div(fixedpoint.New(p*1000), fixedpoint.New(a))
	if err != nil {
		return 0, mapArithErr(err)
	}
	return v, nil
}

// FireRatingMinutes is the design fire resistance in minutes.
type FireRatingMinutes = int64

// rational is a numerator/denominator pair for public thickness rules.
type rational struct{ num, den int64 }

// thicknessFactors maps a design fire rating (minutes) to the rational
// dry-film-thickness coefficient, in micrometres per 1/m of section factor.
// This is the public integer table referenced by the catalog snapshot.
var thicknessFactors = map[FireRatingMinutes]rational{
	30:  {num: 2, den: 1},  // thickness[µm] = 2 * sectionFactor
	60:  {num: 5, den: 2},  // thickness[µm] = 2.5 * sectionFactor
	90:  {num: 4, den: 1},  // thickness[µm] = 4 * sectionFactor
	120: {num: 11, den: 2}, // thickness[µm] = 5.5 * sectionFactor
}

// TargetThickness returns the required dry-film thickness in micrometres for
// the given fire rating and section factor, using the public integer rule and
// half-away-from-zero rounding. Unknown ratings yield FireRatingMismatch.
func TargetThickness(rating FireRatingMinutes, sf fixedpoint.Value) (fixedpoint.Value, error) {
	f, ok := thicknessFactors[rating]
	if !ok {
		return 0, errs.New(errs.CodeFireRatingMismatch, "unknown design fire rating")
	}
	v, err := fixedpoint.Mul(sf, fixedpoint.New(f.num))
	if err != nil {
		return 0, mapArithErr(err)
	}
	v, err = fixedpoint.Div(v, fixedpoint.New(f.den))
	if err != nil {
		return 0, mapArithErr(err)
	}
	return v, nil
}

// mapArithErr converts fixedpoint arithmetic errors into stable codes.
func mapArithErr(err error) error {
	switch err {
	case fixedpoint.ErrOverflow:
		return errs.New(errs.CodeFixedPointOverflow, "fixed-point arithmetic overflow")
	case fixedpoint.ErrDivideByZero:
		return errs.New(errs.CodeFixedPointOverflow, "division by zero")
	default:
		return err
	}
}

const (
	maxInt64     = int64(^uint64(0) >> 1)
	maxDimension = 1 << 20 // 1,048,576 mm guards against over-long input
)
