package catalog

import (
	"fmt"
	"sort"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
)

// LockScope is the complete, ordered scope validated and frozen by a task
// lock. It bundles every member with its derived section factor and target
// thickness plus the immutable catalog snapshot and coat plan.
type LockScope struct {
	Members   []Member        `json:"members"`
	Snapshot  CatalogSnapshot `json:"snapshot"`
	Plan      CoatPlan        `json:"plan"`
	Specimens []Specimen      `json:"specimens"`
}

// LockedMember is a Member augmented with the derived integer results computed
// from the public rules during lock.
type LockedMember struct {
	Member          Member `json:"member"`
	SectionFactor   int64  `json:"section_factor_raw"`
	TargetThickness int64  `json:"target_thickness_raw"`
}

// ValidateLockScope performs the complete, deterministic validation of a
// candidate lock scope. It verifies member ownership, face completeness and
// boundaries, point uniqueness and bounds, spray-zone references, adjacency
// edges, primer compatibility and numeric rule results. All rejection reasons
// are returned in ascending (floor, compartment, member, face, point,
// generation) order.
func ValidateLockScope(floor, compartment string, members []Member, snap CatalogSnapshot, plan CoatPlan) ([]LockedMember, error) {
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	var reasons []string
	locked := make([]LockedMember, 0, len(members))

	for _, m := range members {
		if m.Floor != floor || m.FireCompartment != compartment {
			reasons = append(reasons, reason(floor, compartment, m.ID, "", "", 0,
				errs.CodeMemberMismatch, "member not in floor/compartment"))
			continue
		}
		if len(m.MaterialBatchIDs) == 0 {
			reasons = append(reasons, reason(floor, compartment, m.ID, "", "", 0,
				errs.CodeInvalidInput, "member has no material batch"))
		}
		if m.PrimerBatchID == "" {
			reasons = append(reasons, reason(floor, compartment, m.ID, "", "", 0,
				errs.CodeInvalidInput, "member has no primer batch"))
		}
		if !snap.PrimerCompatible(m.PrimerBatchID, coatingFromBatches(m.MaterialBatchIDs)) {
			reasons = append(reasons, reason(floor, compartment, m.ID, "", "", 0,
				errs.CodePrimerIncompatible, "primer incompatible with coating"))
		}

		sf, err := m.Section.SectionFactor()
		if err != nil {
			reasons = append(reasons, reason(floor, compartment, m.ID, "", "", 0, codeOf(err), "invalid section"))
		} else {
			th, terr := TargetThickness(m.FireRatingMinutes, sf)
			if terr != nil {
				reasons = append(reasons, reason(floor, compartment, m.ID, "", "", 0, codeOf(terr), "invalid fire rating"))
			} else {
				locked = append(locked, LockedMember{
					Member:          m,
					SectionFactor:   sf.Raw(),
					TargetThickness: th.Raw(),
				})
			}
		}

		reasons = append(reasons, validateFaces(floor, compartment, m)...)
		reasons = append(reasons, validateAdjacency(floor, compartment, m)...)
	}

	if len(reasons) > 0 {
		sort.Strings(reasons)
		return nil, errs.New(errs.CodeInvalidInput, "lock scope validation failed").WithReasons(reasons...)
	}
	return locked, nil
}

func validateFaces(floor, compartment string, m Member) []string {
	var reasons []string
	zoneIDs := make(map[string]bool, len(m.SprayZones))
	for _, z := range m.SprayZones {
		zoneIDs[z.ID] = true
	}
	faceIDs := make(map[string]bool, len(m.ExposedFaces))
	for _, f := range m.ExposedFaces {
		if f.MemberID != m.ID {
			reasons = append(reasons, reason(floor, compartment, m.ID, f.ID, "", 0,
				errs.CodeMemberMismatch, "face member mismatch"))
		}
		if faceIDs[f.ID] {
			reasons = append(reasons, reason(floor, compartment, m.ID, f.ID, "", 0,
				errs.CodeFaceIncomplete, "duplicate face id"))
		}
		faceIDs[f.ID] = true
		if !zoneIDs[f.SprayZoneID] {
			reasons = append(reasons, reason(floor, compartment, m.ID, f.ID, "", 0,
				errs.CodeFaceIncomplete, "face references unknown spray zone"))
		}
		seen := make(map[string]bool, len(f.GridPoints))
		for _, gp := range f.GridPoints {
			if gp.FaceID != f.ID {
				reasons = append(reasons, reason(floor, compartment, m.ID, f.ID, gp.ID, 0,
					errs.CodeFaceIncomplete, "grid point face mismatch"))
			}
			if seen[gp.ID] {
				reasons = append(reasons, reason(floor, compartment, m.ID, f.ID, gp.ID, 0,
					errs.CodePointDuplicate, "duplicate grid point"))
			}
			seen[gp.ID] = true
			if !f.Boundary.Contains(gp.Point) {
				reasons = append(reasons, reason(floor, compartment, m.ID, f.ID, gp.ID, 0,
					errs.CodeFaceIncomplete, "grid point outside boundary"))
			}
			if !zoneIDs[gp.SprayZoneID] {
				reasons = append(reasons, reason(floor, compartment, m.ID, f.ID, gp.ID, 0,
					errs.CodeFaceIncomplete, "grid point references unknown spray zone"))
			}
		}
	}
	return reasons
}

func validateAdjacency(floor, compartment string, m Member) []string {
	var reasons []string
	points := make(map[string]bool)
	for _, f := range m.ExposedFaces {
		for _, gp := range f.GridPoints {
			points[gp.ID] = true
		}
	}
	for _, e := range m.AdjacencyEdges {
		if !points[e.From] || !points[e.To] {
			reasons = append(reasons, reason(floor, compartment, m.ID, "", e.From, 0,
				errs.CodeFaceIncomplete, "adjacency edge references unknown point"))
		}
		if e.DistanceMM < 0 {
			reasons = append(reasons, reason(floor, compartment, m.ID, "", e.From, 0,
				errs.CodeInvalidInput, "adjacency distance must be non-negative"))
		}
	}
	return reasons
}

// reason renders a single ordered rejection reason in the canonical sort key
// format: floor|compartment|member|face|point|generation.
func reason(floor, compartment, member, face, point string, gen int64, code errs.Code, msg string) string {
	return fmt.Sprintf("floor=%s|compartment=%s|member=%s|face=%s|point=%s|gen=%d|%s|%s",
		floor, compartment, member, face, point, gen, code, msg)
}

func codeOf(err error) errs.Code {
	if de, ok := err.(*errs.Error); ok {
		return de.Code
	}
	return errs.CodeInvalidInput
}

func coatingFromBatches(batches []string) string {
	if len(batches) == 0 {
		return ""
	}
	return batches[0]
}
