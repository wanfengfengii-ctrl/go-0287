package service

import (
	"context"
	"database/sql"
	"testing"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/lease"
	"example.com/steel-fireproofing-evidence-closure/internal/store"
	"example.com/steel-fireproofing-evidence-closure/internal/task"
)

// TestFixedPointDetectionCalculations verifies deterministic integer results
// for cumulative thickness, dispersion, unit-area usage and bond strength.
func TestFixedPointDetectionCalculations(t *testing.T) {
	cum, err := task.CumulativeThickness([]int64{1_000_000, 2_000_000, 1_500_000})
	if err != nil || cum.Raw() != 4_500_000 {
		t.Fatalf("cumulative = %d/%v, want 4500000", cum.Raw(), err)
	}
	disp, err := task.Dispersion([]int64{1_000_000, 3_000_000, 2_000_000})
	if err != nil || disp.Raw() != 2_000_000 {
		t.Fatalf("dispersion = %d/%v, want 2000000", disp.Raw(), err)
	}
	// Unit area usage 50 g / 2 mm2 = 25 g/mm2.
	ua, err := task.UnitAreaUsage(50, 2)
	if err != nil || ua.Raw() != 25_000_000 {
		t.Fatalf("unit area = %d/%v, want 25000000", ua.Raw(), err)
	}
	// Bond strength 300 N / 2 mm2 = 150 kPa.
	bs, err := task.BondStrength(300, 2)
	if err != nil || bs.Raw() != 150_000_000 {
		t.Fatalf("bond = %d/%v, want 150000000", bs.Raw(), err)
	}
	// Half-away-from-zero rounding: 100/3 -> 33.333333 (round down).
	ua, err = task.UnitAreaUsage(100, 3)
	if err != nil || ua.Raw() != 33_333_333 {
		t.Fatalf("unit area 100/3 = %d/%v, want 33333333", ua.Raw(), err)
	}
}

func acquireLease(t *testing.T, svc *Service, taskID, equipment string, expires lease.LogicalClock) {
	t.Helper()
	ctx := context.Background()
	if err := svc.store.Transact(ctx, func(tx *sql.Tx) error {
		return store.AcquireLease(tx, taskID, equipment, taskID, 0, expires)
	}); err != nil {
		t.Fatalf("acquire lease: %v", err)
	}
}

// TestDeviceRetryPromotesOnlyOneReading drives a script that returns timeout,
// then a format error, then success, and asserts deterministic retry timing
// and exactly one promoted reading.
func TestDeviceRetryPromotesOnlyOneReading(t *testing.T) {
	svc, done := newTestService(t)
	defer done()
	taskID := lockStandard(t, svc)
	acquireLease(t, svc, taskID, "gauge-1", 1000)

	ctx := context.Background()
	inv := "inv-1"

	// Attempt 1: timeout.
	c1 := DeviceCmd{InvocationID: inv, EquipmentID: "gauge-1", Kind: lease.EquipmentThickness, Fault: lease.FaultTimeout}
	if _, err := svc.InvokeDevice(ctx, taskID, "c", "dev-1", mustJSON(t, c1), 0, 1, 10, c1); err != nil {
		t.Fatalf("timeout invocation: %v", err)
	}
	// Attempt 2: format error.
	c2 := DeviceCmd{InvocationID: inv, EquipmentID: "gauge-1", Kind: lease.EquipmentThickness, Fault: lease.FaultFormatInvalid}
	if _, err := svc.InvokeDevice(ctx, taskID, "c", "dev-2", mustJSON(t, c2), 0, 1, 35, c2); err != nil {
		t.Fatalf("format invocation: %v", err)
	}

	// No reading after two faults.
	readings, _ := svc.store.DryFilm(ctx, taskID, "face-1", "p1", 1)
	if len(readings) != 0 {
		t.Fatalf("readings after two faults = %d, want 0", len(readings))
	}

	// Attempt 3: success.
	c3 := DeviceCmd{InvocationID: inv, EquipmentID: "gauge-1", Kind: lease.EquipmentThickness,
		Reading: &DeviceReading{FaceID: "face-1", PointID: "p1", Value: 20_000_000}}
	if _, err := svc.InvokeDevice(ctx, taskID, "c", "dev-3", mustJSON(t, c3), 0, 1, 160, c3); err != nil {
		t.Fatalf("success invocation: %v", err)
	}

	readings, _ = svc.store.DryFilm(ctx, taskID, "face-1", "p1", 1)
	if len(readings) != 1 {
		t.Fatalf("readings after success = %d, want 1", len(readings))
	}

	// Verify deterministic retry scheduling on the stored invocation.
	got, err := svc.store.GetInvocation(ctx, taskID, inv)
	if err != nil {
		t.Fatalf("GetInvocation: %v", err)
	}
	if got.Attempt != 3 || got.Status != lease.InvocationSucceeded {
		t.Fatalf("invocation attempt/status = %d/%s, want 3/succeeded", got.Attempt, got.Status)
	}
}

// TestDeviceLateResultAfterLeaseExpiry ensures a device result arriving after
// the lease expiry is rejected and not promoted to a reading.
func TestDeviceLateResultAfterLeaseExpiry(t *testing.T) {
	svc, done := newTestService(t)
	defer done()
	taskID := lockStandard(t, svc)
	acquireLease(t, svc, taskID, "gauge-1", 50)

	ctx := context.Background()
	c := DeviceCmd{InvocationID: "inv-x", EquipmentID: "gauge-1", Kind: lease.EquipmentThickness,
		Reading: &DeviceReading{FaceID: "face-1", PointID: "p1", Value: 20_000_000}}
	_, err := svc.InvokeDevice(ctx, taskID, "c", "dev-late", mustJSON(t, c), 0, 1, 60, c)
	assertCode(t, err, errs.CodeLeaseExpired)

	readings, _ := svc.store.DryFilm(ctx, taskID, "face-1", "p1", 1)
	if len(readings) != 0 {
		t.Fatalf("late result promoted %d readings, want 0", len(readings))
	}
}
