package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/lease"
	"example.com/steel-fireproofing-evidence-closure/internal/task"
)

func TestModel_IdempotencyAtomicCommit(t *testing.T) {
	type outcome struct {
		response any
		err      error
	}
	type delivery struct {
		opID  string
		clock lease.LogicalClock
		cmd   DryFilmCmd
		body  []byte
	}

	marshalResponse := func(t *testing.T, v any) string {
		t.Helper()
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal response: %v", err)
		}
		return string(b)
	}

	snapshot := func(t *testing.T, svc *Service, taskID string) (*TaskView, int) {
		t.Helper()
		view, err := svc.GetTask(context.Background(), taskID)
		if err != nil {
			t.Fatalf("get task: %v", err)
		}
		evidence, err := svc.Evidence(context.Background(), taskID, 0, 500)
		if err != nil {
			t.Fatalf("get evidence: %v", err)
		}
		return view, len(evidence.Events)
	}

	tests := []struct {
		name string
		run  func(*testing.T)
	}{
		{
			name: "concurrent identical retries share one commit",
			run: func(t *testing.T) {
				const deliveries = 2
				svc, done := newTestService(t)
				defer done()
				ctx := context.Background()
				taskID := lockStandard(t, svc)
				before, beforeEvents := snapshot(t, svc, taskID)

				cmd := DryFilmCmd{FaceID: "face-1", PointID: "p1", ValueUM: 40_000_000}
				body := mustJSON(t, cmd)
				results := make(chan outcome, deliveries)
				start := make(chan struct{})
				for i := 0; i < deliveries; i++ {
					go func() {
						<-start
						response, err := svc.DryFilm(ctx, taskID, "inspector", "measure-p1", body, 0, 1, 17, cmd)
						results <- outcome{response: response, err: err}
					}()
				}
				close(start)

				got := make([]outcome, deliveries)
				for i := range got {
					got[i] = <-results
				}
				for i, result := range got {
					if result.err != nil {
						t.Fatalf("retry %d: %v", i, result.err)
					}
				}
				first := marshalResponse(t, got[0].response)
				for i := 1; i < len(got); i++ {
					if retry := marshalResponse(t, got[i].response); first != retry {
						t.Fatalf("retry responses differ: %s != %s", first, retry)
					}
				}

				readings, err := svc.Store().DryFilm(ctx, taskID, "face-1", "p1", task.Generation(1))
				if err != nil {
					t.Fatalf("dry-film projection: %v", err)
				}
				if len(readings) != 1 || readings[0] != cmd.ValueUM {
					t.Fatalf("dry-film projection = %v, want one reading %d", readings, cmd.ValueUM)
				}
				after, afterEvents := snapshot(t, svc, taskID)
				if after.AggregateVersion != before.AggregateVersion+1 {
					t.Fatalf("aggregate version = %d, want %d", after.AggregateVersion, before.AggregateVersion+1)
				}
				if after.LogicalClock != lease.LogicalClock(17) {
					t.Fatalf("logical clock = %d, want 17", after.LogicalClock)
				}
				if afterEvents != beforeEvents+1 {
					t.Fatalf("evidence events = %d, want %d", afterEvents, beforeEvents+1)
				}
			},
		},
		{
			name: "projection and idempotency roll back when evidence cannot commit",
			run: func(t *testing.T) {
				svc, done := newTestService(t)
				defer done()
				ctx := context.Background()
				taskID := lockStandard(t, svc)
				before, beforeEvents := snapshot(t, svc, taskID)
				if err := svc.Store().Transact(ctx, func(tx *sql.Tx) error {
					_, err := tx.Exec(`CREATE TRIGGER reject_evidence BEFORE INSERT ON events BEGIN SELECT RAISE(ABORT, 'evidence unavailable'); END`)
					return err
				}); err != nil {
					t.Fatalf("install evidence failure: %v", err)
				}

				cmd := DryFilmCmd{FaceID: "face-1", PointID: "p1", ValueUM: 40_000_000}
				if _, err := svc.DryFilm(ctx, taskID, "inspector", "measure-p1", mustJSON(t, cmd), 0, 1, 17, cmd); err == nil {
					t.Fatal("command succeeded although its evidence could not commit")
				}
				readings, err := svc.Store().DryFilm(ctx, taskID, "face-1", "p1", task.Generation(1))
				if err != nil {
					t.Fatalf("dry-film projection: %v", err)
				}
				after, afterEvents := snapshot(t, svc, taskID)
				if len(readings) != 0 || after.AggregateVersion != before.AggregateVersion || after.LogicalClock != before.LogicalClock || afterEvents != beforeEvents {
					t.Fatalf("failed atomic commit left residue: readings=%v, before=%+v/%d after=%+v/%d", readings, before, beforeEvents, after, afterEvents)
				}
			},
		},
		{
			name: "sequential retry returns the committed result",
			run: func(t *testing.T) {
				svc, done := newTestService(t)
				defer done()
				ctx := context.Background()
				taskID := lockStandard(t, svc)
				before, beforeEvents := snapshot(t, svc, taskID)
				cmd := DryFilmCmd{FaceID: "face-1", PointID: "p1", ValueUM: 40_000_000}
				body := mustJSON(t, cmd)

				first, err := svc.DryFilm(ctx, taskID, "inspector", "measure-p1", body, 0, 1, 11, cmd)
				if err != nil {
					t.Fatalf("first delivery: %v", err)
				}
				second, err := svc.DryFilm(ctx, taskID, "inspector", "measure-p1", body, 0, 1, 11, cmd)
				if err != nil {
					t.Fatalf("retry: %v", err)
				}
				if marshalResponse(t, first) != marshalResponse(t, second) {
					t.Fatalf("sequential retry did not return the committed result")
				}
				after, afterEvents := snapshot(t, svc, taskID)
				if after.AggregateVersion != before.AggregateVersion+1 || afterEvents != beforeEvents+1 {
					t.Fatalf("retry advanced state twice: version %d->%d, events %d->%d", before.AggregateVersion, after.AggregateVersion, beforeEvents, afterEvents)
				}
			},
		},
		{
			name: "operation id reuse with different digest conflicts",
			run: func(t *testing.T) {
				svc, done := newTestService(t)
				defer done()
				ctx := context.Background()
				taskID := lockStandard(t, svc)
				first := DryFilmCmd{FaceID: "face-1", PointID: "p1", ValueUM: 40_000_000}
				if _, err := svc.DryFilm(ctx, taskID, "inspector", "measure-p1", mustJSON(t, first), 0, 1, 10, first); err != nil {
					t.Fatalf("first delivery: %v", err)
				}
				before, beforeEvents := snapshot(t, svc, taskID)
				other := DryFilmCmd{FaceID: "face-1", PointID: "p1", ValueUM: 41_000_000}
				_, err := svc.DryFilm(ctx, taskID, "inspector", "measure-p1", mustJSON(t, other), 0, 1, 12, other)
				assertCode(t, err, errs.CodeIdempotencyConflict)
				after, afterEvents := snapshot(t, svc, taskID)
				if after.AggregateVersion != before.AggregateVersion || after.LogicalClock != before.LogicalClock || afterEvents != beforeEvents {
					t.Fatalf("conflict changed state: before=%+v/%d after=%+v/%d", before, beforeEvents, after, afterEvents)
				}
			},
		},
		{
			name: "distinct concurrent operations both commit",
			run: func(t *testing.T) {
				svc, done := newTestService(t)
				defer done()
				ctx := context.Background()
				taskID := lockStandard(t, svc)
				before, beforeEvents := snapshot(t, svc, taskID)
				results := make(chan error, 2)
				start := make(chan struct{})
				commands := []delivery{
					{opID: "measure-p1", clock: 20, cmd: DryFilmCmd{FaceID: "face-1", PointID: "p1", ValueUM: 40_000_000}},
					{opID: "measure-p2", clock: 21, cmd: DryFilmCmd{FaceID: "face-1", PointID: "p2", ValueUM: 40_000_001}},
				}
				for i := range commands {
					commands[i].body = mustJSON(t, commands[i].cmd)
				}
				for _, item := range commands {
					go func(d delivery) {
						<-start
						_, err := svc.DryFilm(ctx, taskID, "inspector", d.opID, d.body, 0, 1, d.clock, d.cmd)
						results <- err
					}(item)
				}
				close(start)
				for i := 0; i < 2; i++ {
					if err := <-results; err != nil {
						t.Fatalf("distinct operation: %v", err)
					}
				}
				after, afterEvents := snapshot(t, svc, taskID)
				if after.AggregateVersion != before.AggregateVersion+2 || afterEvents != beforeEvents+2 {
					t.Fatalf("distinct operations did not both advance: version %d->%d, events %d->%d", before.AggregateVersion, after.AggregateVersion, beforeEvents, afterEvents)
				}
			},
		},
		{
			name: "business error rolls back and does not reserve operation id",
			run: func(t *testing.T) {
				svc, done := newTestService(t)
				defer done()
				ctx := context.Background()
				taskID := lockStandard(t, svc)
				before, beforeEvents := snapshot(t, svc, taskID)
				bad := DryFilmCmd{FaceID: "face-1", PointID: "p1", ValueUM: -1}
				_, err := svc.DryFilm(ctx, taskID, "inspector", "measure-p1", mustJSON(t, bad), 0, 1, 9, bad)
				assertCode(t, err, errs.CodeInvalidInput)
				unchanged, unchangedEvents := snapshot(t, svc, taskID)
				if unchanged.AggregateVersion != before.AggregateVersion || unchanged.LogicalClock != before.LogicalClock || unchangedEvents != beforeEvents {
					t.Fatalf("business error left residue: before=%+v/%d after=%+v/%d", before, beforeEvents, unchanged, unchangedEvents)
				}

				good := DryFilmCmd{FaceID: "face-1", PointID: "p1", ValueUM: 40_000_000}
				if _, err := svc.DryFilm(ctx, taskID, "inspector", "measure-p1", mustJSON(t, good), 0, 1, 10, good); err != nil {
					t.Fatalf("corrected command using rolled-back operation id: %v", err)
				}
				after, afterEvents := snapshot(t, svc, taskID)
				if after.AggregateVersion != before.AggregateVersion+1 || afterEvents != beforeEvents+1 {
					t.Fatalf("corrected command did not commit once: version %d->%d, events %d->%d", before.AggregateVersion, after.AggregateVersion, beforeEvents, afterEvents)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, tc.run)
	}
}
