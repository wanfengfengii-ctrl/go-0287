package task

import (
	"sort"

	"example.com/steel-fireproofing-evidence-closure/internal/catalog"
)

// DefectKind discriminates the deterministic defect classes that trigger a
// recoat: insufficient thickness, dispersion, hollowing, cracking,
// detachment and insufficient bond.
type DefectKind string

const (
	DefectThicknessLow DefectKind = "thickness_low"
	DefectDispersion   DefectKind = "dispersion"
	DefectHollow       DefectKind = "hollow"
	DefectCrack        DefectKind = "crack"
	DefectDetachment   DefectKind = "detachment"
	DefectBondLow      DefectKind = "bond_low"
)

// Defect is a detection failure bound to a point, face, generation and
// material batch.
type Defect struct {
	Kind            DefectKind `json:"kind"`
	PointID         string     `json:"point_id"`
	FaceID          string     `json:"face_id"`
	MemberID        string     `json:"member_id"`
	MaterialBatchID string     `json:"material_batch_id"`
	Generation      Generation `json:"generation"`
}

// InfluenceDistanceMM is the public adjacency distance within which a defect
// propagates to neighbouring grid points.
const InfluenceDistanceMM int64 = 500

// RecoatScope is the deterministic ordered union of affected points produced
// by the defect influence-domain algorithm, plus the coat indices that must
// be redone on each affected face.
type RecoatScope struct {
	AffectedPoints []string   `json:"affected_points"` // sorted "faceID/pointID"
	CoatIndices    []int      `json:"coat_indices"`    // coat indices to redo
	Generation     Generation `json:"generation"`
}

// InfluenceDomain computes the deterministic defect influence-domain union for
// a member: for every defect, it includes the defect's spray zone, adjacent
// points within InfluenceDistanceMM in the locked adjacency graph, and points
// bound to the same material batch (via pointBatch). The result is sorted
// ascending by (face, point).
func InfluenceDomain(member catalog.Member, defects []Defect, pointBatch map[string]string) []string {
	zoneByPoint := map[string]string{}
	pointsByZone := map[string][]string{}
	pointFace := map[string]string{}
	for _, f := range member.ExposedFaces {
		for _, gp := range f.GridPoints {
			zoneByPoint[gp.ID] = gp.SprayZoneID
			pointFace[gp.ID] = f.ID
			pointsByZone[gp.SprayZoneID] = append(pointsByZone[gp.SprayZoneID], gp.ID)
		}
	}
	adj := adjacencyIndex(member.AdjacencyEdges)

	affected := map[string]bool{}
	for _, d := range defects {
		if d.PointID == "" {
			continue
		}
		affected[facePointKey(pointFace[d.PointID], d.PointID)] = true
		zone := zoneByPoint[d.PointID]
		for _, pid := range pointsByZone[zone] {
			affected[facePointKey(pointFace[pid], pid)] = true
		}
		expandAdjacent(d.PointID, pointFace, adj, InfluenceDistanceMM, affected)
		for pid, batch := range pointBatch {
			if batch == d.MaterialBatchID && batch != "" {
				affected[facePointKey(pointFace[pid], pid)] = true
			}
		}
	}
	out := make([]string, 0, len(affected))
	for k := range affected {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func facePointKey(face, point string) string { return face + "/" + point }

func expandAdjacent(start string, pointFace map[string]string, adj map[string][]catalog.AdjacencyEdge, dist int64, acc map[string]bool) {
	stack := []string{start}
	visited := map[string]bool{start: true}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, e := range adj[cur] {
			if e.DistanceMM > dist || visited[e.To] {
				continue
			}
			visited[e.To] = true
			acc[facePointKey(pointFace[e.To], e.To)] = true
			stack = append(stack, e.To)
		}
	}
}

func adjacencyIndex(edges []catalog.AdjacencyEdge) map[string][]catalog.AdjacencyEdge {
	idx := make(map[string][]catalog.AdjacencyEdge)
	for _, e := range edges {
		idx[e.From] = append(idx[e.From], e)
		idx[e.To] = append(idx[e.To], catalog.AdjacencyEdge{From: e.To, To: e.From, DistanceMM: e.DistanceMM})
	}
	return idx
}
