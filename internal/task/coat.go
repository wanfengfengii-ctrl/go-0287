package task

import (
	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/lease"
)

// CoatApplication is one completed coat on one exposed face within a task
// generation. Applications on a face must form a contiguous prefix from the
// first coat; anything else is rejected without advancing state.
type CoatApplication struct {
	FaceID          string             `json:"face_id"`
	CoatIndex       int                `json:"coat_index"`
	Generation      Generation         `json:"generation"`
	SprayZoneID     string             `json:"spray_zone_id"`
	MaterialBatchID string             `json:"material_batch_id"`
	AreaMM2         int64              `json:"area_mm2"`
	IssuedGrams     int64              `json:"issued_grams"`
	RecoveredGrams  int64              `json:"recovered_grams"`
	ApprovedWaste   int64              `json:"approved_waste_grams"`
	LogicalTime     lease.LogicalClock `json:"logical_time"`
}

// CoatPrefix tracks, per face and generation, the highest contiguous coat
// index completed so far. It enforces the "continuous prefix from coat one"
// invariant from the domain rules.
type CoatPrefix struct {
	byFace map[string]int
}

// NewCoatPrefix returns an empty prefix tracker.
func NewCoatPrefix() *CoatPrefix {
	return &CoatPrefix{byFace: make(map[string]int)}
}

// Completed returns the highest contiguous coat index completed for a face.
func (c *CoatPrefix) Completed(faceID string) int { return c.byFace[faceID] }

// NextIndex returns the only coat index that may be applied next on a face.
func (c *CoatPrefix) NextIndex(faceID string) int { return c.byFace[faceID] + 1 }

// Record applies a coat index to a face, enforcing the contiguous prefix
// invariant. A repeated index yields CodeCoatDuplicate and a skipped index
// yields CodeCoatOutOfOrder; neither mutates the tracker.
func (c *CoatPrefix) Record(faceID string, index int) error {
	done := c.byFace[faceID]
	next := done + 1
	switch {
	case index <= done:
		return errs.New(errs.CodeCoatDuplicate, "coat already applied for face").
			WithReasons("face=" + faceID)
	case index > next:
		return errs.New(errs.CodeCoatOutOfOrder, "coat index skips the contiguous prefix").
			WithReasons("face=" + faceID)
	}
	c.byFace[faceID] = index
	return nil
}

// WetFilmReading is a point wet-film reading recorded during a coat.
type WetFilmReading struct {
	FaceID    string `json:"face_id"`
	PointID   string `json:"point_id"`
	CoatIndex int    `json:"coat_index"`
	ValueUM   int64  `json:"value_um"`
}

// CuringSample is a temperature/humidity trajectory point collected during a
// curing window. Samples are ordered by logical time and must cover the
// locked curing window before the next coat or detection is allowed.
type CuringSample struct {
	FaceID      string             `json:"face_id"`
	CoatIndex   int                `json:"coat_index"`
	TempC       int64              `json:"temp_c_raw"`
	HumidityRH  int64              `json:"humidity_rh_raw"`
	LogicalTime lease.LogicalClock `json:"logical_time"`
}

// CuringWindowClosed reports whether the ordered samples span at least the
// required duration (logical ticks) for a face.
func CuringWindowClosed(samples []CuringSample, required lease.LogicalClock) bool {
	if len(samples) < 2 {
		return false
	}
	first, last := samples[0].LogicalTime, samples[len(samples)-1].LogicalTime
	return last-first >= required
}
