package catalog

// Point is an integer coordinate within an exposed-face boundary.
type Point struct {
	X int64 `json:"x"`
	Y int64 `json:"y"`
}

// Boundary is the integer bounding box of an exposed face.
type Boundary struct {
	MinX int64 `json:"min_x"`
	MinY int64 `json:"min_y"`
	MaxX int64 `json:"max_x"`
	MaxY int64 `json:"max_y"`
}

// Contains reports whether p lies inside the closed boundary.
func (b Boundary) Contains(p Point) bool {
	return p.X >= b.MinX && p.X <= b.MaxX && p.Y >= b.MinY && p.Y <= b.MaxY
}

// Member is a locked steel member within a fire compartment.
type Member struct {
	ID                string            `json:"id"`
	Floor             string            `json:"floor"`
	FireCompartment   string            `json:"fire_compartment"`
	Type              MemberType        `json:"type"`
	Section           Section           `json:"section"`
	FireRatingMinutes FireRatingMinutes `json:"fire_rating_minutes"`
	MaterialBatchIDs  []string          `json:"material_batch_ids"`
	PrimerBatchID     string            `json:"primer_batch_id"`
	ExposedFaces      []ExposedFace     `json:"exposed_faces"`
	SprayZones        []SprayZone       `json:"spray_zones"`
	AdjacencyEdges    []AdjacencyEdge   `json:"adjacency_edges"`
}

// ExposedFace is a single fire-exposed surface of a member with its boundary
// and belonging spray zone.
type ExposedFace struct {
	ID          string      `json:"id"`
	MemberID    string      `json:"member_id"`
	Boundary    Boundary    `json:"boundary"`
	SprayZoneID string      `json:"spray_zone_id"`
	GridPoints  []GridPoint `json:"grid_points"`
}

// GridPoint is a measurement point whose integer coordinates lie inside the
// owning exposed face's boundary and whose ID is unique within that face.
type GridPoint struct {
	ID          string `json:"id"`
	FaceID      string `json:"face_id"`
	Point       Point  `json:"point"`
	SprayZoneID string `json:"spray_zone_id"`
}

// SprayZone is a contiguous spray area referenced by faces and points.
type SprayZone struct {
	ID string `json:"id"`
}

// AdjacencyEdge links two grid points within a prescribed distance used by
// the deterministic defect influence-domain algorithm.
type AdjacencyEdge struct {
	From       string `json:"from"`
	To         string `json:"to"`
	DistanceMM int64  `json:"distance_mm"`
}

// CatalogSnapshot is the immutable directory referenced by a locked task. It
// carries the directory version, rule/material-proof summaries, the primer
// compatibility matrix, equipment calibration summary and the thickness-rule
// version.
type CatalogSnapshot struct {
	Version                     int64                      `json:"version"`
	RuleSummary                 string                     `json:"rule_summary"`
	MaterialProofSummary        string                     `json:"material_proof_summary"`
	PrimerCompatibility         map[string]map[string]bool `json:"primer_compatibility"`
	EquipmentCalibrationSummary string                     `json:"equipment_calibration_summary"`
	ThicknessRuleVersion        int64                      `json:"thickness_rule_version"`
}

// PrimerCompatible reports whether the given primer and coating are
// compatible according to the snapshot's compatibility matrix.
func (c CatalogSnapshot) PrimerCompatible(primer, coating string) bool {
	row, ok := c.PrimerCompatibility[primer]
	if !ok {
		return false
	}
	return row[coating]
}
