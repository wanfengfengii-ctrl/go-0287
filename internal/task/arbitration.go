package task

import (
	"fmt"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
)

// ReviewClass enumerates the evidence classes an independent reviewer must
// attest to before acceptance.
type ReviewClass string

const (
	ReviewMass       ReviewClass = "mass_conservation"
	ReviewCoatPrefix ReviewClass = "coat_prefix"
	ReviewCuring     ReviewClass = "curing_coverage"
	ReviewThickness  ReviewClass = "thickness_coverage"
	ReviewBond       ReviewClass = "bond_evidence"
	ReviewRecoat     ReviewClass = "recoat_generations"
)

// RequiredReviewClasses is the full set of classes that must each be approved
// by two distinct qualified reviewers for acceptance.
var RequiredReviewClasses = []ReviewClass{
	ReviewMass, ReviewCoatPrefix, ReviewCuring, ReviewThickness, ReviewBond, ReviewRecoat,
}

// ValidateReviewers checks that two distinct reviewers have each approved
// every required class. It returns a stable rejection otherwise.
func ValidateReviewers(reviews []Review) error {
	classApprovers := map[ReviewClass]map[string]bool{}
	for _, r := range reviews {
		if !r.Approved {
			continue
		}
		for _, c := range r.Classes {
			if classApprovers[ReviewClass(c)] == nil {
				classApprovers[ReviewClass(c)] = map[string]bool{}
			}
			classApprovers[ReviewClass(c)][r.ReviewerID] = true
		}
	}
	var missing []string
	for _, rc := range RequiredReviewClasses {
		if len(classApprovers[rc]) < 2 {
			missing = append(missing, string(rc))
		}
	}
	if len(missing) > 0 {
		return errs.New(errs.CodeInvalidInput, "independent review incomplete").
			WithReasons(missing...)
	}
	return nil
}

// GenerateCredential produces the unique acceptance credential bound to a
// task, its current generation and the evidence-root digest. The credential
// identifier is deterministic given its inputs.
func GenerateCredential(taskID string, gen Generation, evidenceRoot string) AcceptanceCredential {
	id := Digest([]byte(fmt.Sprintf("%s|%d|%s", taskID, gen, evidenceRoot)))
	return AcceptanceCredential{
		CredentialID: "cred-" + id[:16],
		TaskID:       taskID,
		Generation:   gen,
		EvidenceRoot: evidenceRoot,
	}
}
