package catalog

import "example.com/steel-fireproofing-evidence-closure/internal/errs"

// CoatPlan captures the immutable multi-coat application plan locked into a
// task: the number of required coats, the prescribed wet-film thickness for
// each coat, the open-time and curing-window durations (in logical ticks) and
// the acceptance thresholds used by the detection and terminal stages.
type CoatPlan struct {
	CoatCount          int   `json:"coat_count"`
	WetFilmPerCoatUM   int64 `json:"wet_film_per_coat_um"`
	OpenTimeTicks      int64 `json:"open_time_ticks"`
	CuringWindowTicks  int64 `json:"curing_window_ticks"`
	MinDryFilmUM       int64 `json:"min_dry_film_um"`
	MaxDispersionUM    int64 `json:"max_dispersion_um"`
	MinBondStrengthKPa int64 `json:"min_bond_strength_kpa"`
}

// Validate rejects an incoherent coat plan before it can be locked.
func (p CoatPlan) Validate() error {
	switch {
	case p.CoatCount <= 0:
		return errs.New(errs.CodeInvalidInput, "coat count must be positive")
	case p.WetFilmPerCoatUM <= 0:
		return errs.New(errs.CodeInvalidInput, "wet film per coat must be positive")
	case p.OpenTimeTicks <= 0 || p.CuringWindowTicks <= 0:
		return errs.New(errs.CodeInvalidInput, "open time and curing window must be positive")
	case p.MinDryFilmUM <= 0 || p.MaxDispersionUM < 0 || p.MinBondStrengthKPa <= 0:
		return errs.New(errs.CodeInvalidInput, "invalid detection thresholds")
	}
	return nil
}

// Specimen is a bond pull-off specimen whose result is compared against the
// plan's minimum bond strength during detection and terminal arbitration.
type Specimen struct {
	ID     string `json:"id"`
	FaceID string `json:"face_id"`
	Point  Point  `json:"point"`
}
