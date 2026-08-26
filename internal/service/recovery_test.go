package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"example.com/steel-fireproofing-evidence-closure/internal/lease"
	"example.com/steel-fireproofing-evidence-closure/internal/store"
	"example.com/steel-fireproofing-evidence-closure/internal/task"
)

func openFileService(t *testing.T) (*Service, string, func()) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return New(st), path, func() { st.Close() }
}

func TestRecoveryReplayDoesNotDuplicate(t *testing.T) {
	svc, path, close1 := openFileService(t)
	ctx := context.Background()
	taskID := lockStandard(t, svc)
	openBatch(t, svc, taskID, 1000)

	cmd := issueCmd(100, "")
	body := mustJSON(t, cmd)
	if _, err := svc.MaterialIssue(ctx, taskID, "c", "op-1", body, 0, 1, 10, cmd); err != nil {
		t.Fatalf("issue: %v", err)
	}
	// Simulate a crash after the transaction commits but before the response.
	close1()

	// Restart.
	st2, err := store.Open(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer st2.Close()
	svc2 := New(st2)

	// Replay the same operation id and content.
	if _, err := svc2.MaterialIssue(ctx, taskID, "c", "op-1", body, 0, 1, 10, cmd); err != nil {
		t.Fatalf("replay issue: %v", err)
	}

	batches, _ := svc2.store.ListBatches(ctx, taskID)
	if batches[0].IssuedGrams != 100 {
		t.Fatalf("issued after replay = %d, want 100 (no duplicate deduction)", batches[0].IssuedGrams)
	}
	// open (1) + issue (1) = 2 events; the replay must not append a third.
	events, _ := svc2.store.Evidence(ctx, taskID, 0, 100)
	if len(events) != 2 {
		t.Fatalf("events after replay = %d, want 2", len(events))
	}
}

func TestRecoveryRestoresProjectionIdentically(t *testing.T) {
	svc, path, close1 := openFileService(t)
	ctx := context.Background()
	taskID := lockStandard(t, svc)

	// Build non-trivial state: surface prep, primer, a coat, a pending device
	// invocation and a recoat generation.
	if _, err := svc.SurfacePrep(ctx, taskID, "c", "p", mustJSON(t, SurfacePrepCmd{MemberID: "beam-1"}), 0, 1, 1, SurfacePrepCmd{MemberID: "beam-1"}); err != nil {
		t.Fatalf("prep: %v", err)
	}
	if _, err := svc.Primer(ctx, taskID, "c", "pr", mustJSON(t, PrimerCmd{MemberID: "beam-1"}), 0, 1, 2, PrimerCmd{MemberID: "beam-1"}); err != nil {
		t.Fatalf("primer: %v", err)
	}
	coat := CoatCmd{FaceID: "face-1", CoatIndex: 1, SprayZoneID: "zone-1", MaterialBatchID: "coating-x",
		AreaMM2: 1000, IssuedGrams: 10, EquipmentID: "sprayer-1", LeaseTicks: 1000}
	if _, err := svc.Coat(ctx, taskID, "c", "coat-1", mustJSON(t, coat), 0, 1, 5, coat); err != nil {
		t.Fatalf("coat: %v", err)
	}
	// Acquire a gauge lease and record a pending (faulted) invocation.
	acquireLease(t, svc, taskID, "gauge-1", 1000)
	dev := DeviceCmd{InvocationID: "inv-1", EquipmentID: "gauge-1", Kind: lease.EquipmentThickness, Fault: lease.FaultTimeout}
	if _, err := svc.InvokeDevice(ctx, taskID, "c", "dev-1", mustJSON(t, dev), 0, 1, 8, dev); err != nil {
		t.Fatalf("device: %v", err)
	}
	if _, err := svc.RecordDefect(ctx, taskID, "c", "d1", mustJSON(t, DefectCmd{Kind: task.DefectThicknessLow, FaceID: "face-1", PointID: "p1", MaterialBatchID: "coating-x"}), 0, 1, 9, DefectCmd{Kind: task.DefectThicknessLow, FaceID: "face-1", PointID: "p1", MaterialBatchID: "coating-x"}); err != nil {
		t.Fatalf("defect: %v", err)
	}
	if _, err := svc.CreateRecoatGeneration(ctx, taskID, 1); err != nil {
		t.Fatalf("recoat gen: %v", err)
	}

	before := captureProjection(t, svc, taskID)
	close1()

	// Restart and re-capture.
	st2, err := store.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	svc2 := New(st2)
	after := captureProjection(t, svc2, taskID)

	if !reflect.DeepEqual(before, after) {
		t.Fatalf("projection changed across restart:\nbefore=%v\nafter=%v", before, after)
	}
}

type projection struct {
	Task           *TaskView                `json:"task"`
	CoatPrefix     map[string]int           `json:"coat_prefix"`
	PendingDevices []lease.DeviceInvocation `json:"pending_devices"`
	RecoatScope    *task.RecoatScope        `json:"recoat_scope"`
}

func captureProjection(t *testing.T, svc *Service, taskID string) projection {
	t.Helper()
	ctx := context.Background()
	tv, err := svc.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	cp, err := svc.store.CoatPrefix(ctx, taskID, tv.CurrentGeneration)
	if err != nil {
		t.Fatalf("CoatPrefix: %v", err)
	}
	prefix := map[string]int{}
	coats, _ := svc.store.Coats(ctx, taskID, tv.CurrentGeneration)
	for _, c := range coats {
		prefix[c.FaceID] = cp.Completed(c.FaceID)
	}
	pending, err := svc.store.PendingInvocations(ctx, taskID)
	if err != nil {
		t.Fatalf("PendingInvocations: %v", err)
	}
	scope, _ := svc.store.RecoatScope(ctx, taskID, tv.CurrentGeneration)
	// Normalize byte comparison via JSON round-trip for deterministic ordering.
	b, _ := json.Marshal(projection{Task: tv, CoatPrefix: prefix, PendingDevices: pending, RecoatScope: scope})
	var p projection
	_ = json.Unmarshal(b, &p)
	return p
}
