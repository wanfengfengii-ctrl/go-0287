// Package task implements the "涂层验收任务聚合" and the "厚度粘结复验及终局
// 仲裁器" components: the append-only evidence graph, task generations, the
// terminal decision barrier and the single immutable terminal decision.
package task

import (
	"crypto/sha256"
	"encoding/hex"

	"example.com/steel-fireproofing-evidence-closure/internal/catalog"
	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/lease"
)

// Generation is a monotonically increasing task generation number. Recoating
// creates a strictly greater generation; older generations are never mutated.
type Generation int64

// TerminalState is the single-writer terminal value of a task.
type TerminalState string

const (
	TerminalEmpty       TerminalState = ""
	TerminalAccepted    TerminalState = "accepted"
	TerminalQuarantined TerminalState = "quarantined"
	TerminalCancelled   TerminalState = "cancelled"
)

// TerminalDecision is the immutable terminal arbitration outcome. It may be
// written at most once.
type TerminalDecision struct {
	State      TerminalState `json:"state"`
	DecidedBy  string        `json:"decided_by"`
	Generation Generation    `json:"generation"`
}

// EvidenceType enumerates the append-only evidence event kinds.
type EvidenceType string

const (
	EvidenceSurfacePrep      EvidenceType = "surface_preparation"
	EvidenceDeviceInvocation EvidenceType = "device_invocation"
	EvidenceCoatApplication  EvidenceType = "coat_application"
	EvidenceWetFilm          EvidenceType = "wet_film"
	EvidenceCuringSample     EvidenceType = "curing_sample"
	EvidenceDryFilm          EvidenceType = "dry_film"
	EvidenceNeedleReading    EvidenceType = "needle_reading"
	EvidenceBondResult       EvidenceType = "bond_result"
	EvidenceDefect           EvidenceType = "defect"
	EvidenceReview           EvidenceType = "review"
)

// EvidenceEvent is an append-only event with an aggregate sequence number, a
// logical time, an idempotency operation key, the owning generation, a
// normalized payload digest and the previous event hash.
type EvidenceEvent struct {
	Sequence      int64              `json:"sequence"`
	LogicalTime   lease.LogicalClock `json:"logical_time"`
	OperationID   string             `json:"operation_id"`
	Generation    Generation         `json:"generation"`
	Type          EvidenceType       `json:"type"`
	PayloadDigest string             `json:"payload_digest"`
	PrevHash      string             `json:"prev_hash"`
	Hash          string             `json:"hash"`
}

// Review is an independent qualified-person review of one or more evidence
// classes for a generation.
type Review struct {
	ReviewerID string     `json:"reviewer_id"`
	Generation Generation `json:"generation"`
	Approved   bool       `json:"approved"`
	Classes    []string   `json:"classes"`
}

// AcceptanceCredential is the unique credential bound to a task, generation
// and evidence-root digest, produced only on a successful acceptance.
type AcceptanceCredential struct {
	CredentialID string     `json:"credential_id"`
	TaskID       string     `json:"task_id"`
	Generation   Generation `json:"generation"`
	EvidenceRoot string     `json:"evidence_root"`
}

// Task is the aggregate root describing a locked task scope and its current
// generation and terminal decision.
type Task struct {
	ID               string                  `json:"id"`
	Snapshot         catalog.CatalogSnapshot `json:"snapshot"`
	Members          []catalog.Member        `json:"members"`
	CurrentGen       Generation              `json:"current_generation"`
	AggregateVersion int64                   `json:"aggregate_version"`
	Terminal         *TerminalDecision       `json:"terminal,omitempty"`
}

// DecideTerminal writes the terminal decision exactly once under a single
// writer barrier. A second decision returns CodeTerminalAlreadyDecided.
func (t *Task) DecideTerminal(decider string, state TerminalState) error {
	if state != TerminalAccepted && state != TerminalQuarantined && state != TerminalCancelled {
		return errs.New(errs.CodeInvalidInput, "invalid terminal state")
	}
	if t.Terminal != nil {
		return errs.New(errs.CodeTerminalAlreadyDecided, "terminal decision already recorded")
	}
	t.Terminal = &TerminalDecision{State: state, DecidedBy: decider, Generation: t.CurrentGen}
	return nil
}

// Digest computes a stable SHA-256 digest of the given payload bytes.
func Digest(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// Store is the persistence boundary for the task aggregate: append-only
// evidence events, snapshot recovery and terminal decision arbitration.
type Store interface {
	// Append atomically appends an evidence event and advances the aggregate.
	Append(taskID string, e EvidenceEvent) error
	// Restore reconstructs a task from its snapshot and trailing events.
	Restore(taskID string) (*Task, error)
	// DecideTerminal performs the conditional terminal update.
	DecideTerminal(taskID string, d TerminalDecision) error
}
