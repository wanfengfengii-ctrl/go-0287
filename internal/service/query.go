package service

import (
	"context"

	"example.com/steel-fireproofing-evidence-closure/internal/lease"
	"example.com/steel-fireproofing-evidence-closure/internal/store"
	"example.com/steel-fireproofing-evidence-closure/internal/task"
)

// TaskView is the current projection of a task for GET /v1/tasks/{id}.
type TaskView struct {
	ID                string                 `json:"id"`
	Floor             string                 `json:"floor"`
	FireCompartment   string                 `json:"fire_compartment"`
	CurrentGeneration task.Generation        `json:"current_generation"`
	AggregateVersion  int64                  `json:"aggregate_version"`
	LogicalClock      lease.LogicalClock     `json:"logical_clock"`
	Terminal          *task.TerminalDecision `json:"terminal,omitempty"`
	CredentialID      string                 `json:"credential_id,omitempty"`
	MemberCount       int                    `json:"member_count"`
}

// GetTask returns the current projection of a task.
func (s *Service) GetTask(ctx context.Context, id string) (*TaskView, error) {
	t, err := s.store.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	return &TaskView{
		ID:                t.ID,
		Floor:             t.Scope.Floor,
		FireCompartment:   t.Scope.Compartment,
		CurrentGeneration: t.CurrentGeneration,
		AggregateVersion:  t.AggregateVersion,
		LogicalClock:      lease.LogicalClock(t.LogicalClock),
		Terminal:          t.Terminal,
		CredentialID:      t.CredentialID,
		MemberCount:       len(t.Scope.Members),
	}, nil
}

// EvidenceView is a page of the append-only evidence log.
type EvidenceView struct {
	Events     []task.EvidenceEvent `json:"events"`
	NextCursor int64                `json:"next_cursor"`
}

// Evidence returns a stable-cursor page of the evidence graph.
func (s *Service) Evidence(ctx context.Context, taskID string, after int64, limit int) (*EvidenceView, error) {
	events, err := s.store.Evidence(ctx, taskID, after, limit)
	if err != nil {
		return nil, err
	}
	next := after
	if len(events) > 0 {
		next = events[len(events)-1].Sequence
	}
	return &EvidenceView{Events: events, NextCursor: next}, nil
}

// MaterialBalanceView reports integer-gram mass conservation for every batch.
type MaterialBalanceView struct {
	Batches []lease.MaterialBatch `json:"batches"`
	Ledger  []store.LedgerEntry   `json:"ledger"`
}

// MaterialBalance returns batches and their ledger entries.
func (s *Service) MaterialBalance(ctx context.Context, taskID string) (*MaterialBalanceView, error) {
	batches, err := s.store.ListBatches(ctx, taskID)
	if err != nil {
		return nil, err
	}
	var ledger []store.LedgerEntry
	for _, b := range batches {
		entries, err := s.store.Ledger(ctx, taskID, b.ID)
		if err != nil {
			return nil, err
		}
		ledger = append(ledger, entries...)
	}
	return &MaterialBalanceView{Batches: batches, Ledger: ledger}, nil
}

// RecoatScope returns the ordered recoat scope for the current generation.
func (s *Service) RecoatScope(ctx context.Context, taskID string, gen task.Generation) (*task.RecoatScope, error) {
	return s.store.RecoatScope(ctx, taskID, gen)
}
