package service

import (
	"context"
	"database/sql"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/lease"
	"example.com/steel-fireproofing-evidence-closure/internal/store"
	"example.com/steel-fireproofing-evidence-closure/internal/task"
)

// DeviceReading is the valid measurement produced by a successful device
// invocation.
type DeviceReading struct {
	FaceID     string `json:"face_id"`
	PointID    string `json:"point_id"`
	Value      int64  `json:"value"`
	SpecimenID string `json:"specimen_id"`
}

// DeviceCmd drives one scripted device invocation. A non-empty Fault records
// a pending retry; otherwise the reading is promoted into evidence. All
// retries of one logical invocation share the same InvocationID.
type DeviceCmd struct {
	InvocationID string              `json:"invocation_id"`
	EquipmentID  string              `json:"equipment_id"`
	Kind         lease.EquipmentKind `json:"kind"`
	Fault        lease.FaultType     `json:"fault,omitempty"`
	Reading      *DeviceReading      `json:"reading,omitempty"`
}

// DeviceResult reports the outcome of a device invocation.
type DeviceResult struct {
	InvocationID string                 `json:"invocation_id"`
	Status       lease.InvocationStatus `json:"status"`
	Attempt      int                    `json:"attempt"`
	NextRetryAt  lease.LogicalClock     `json:"next_retry_at"`
	ReadingRef   string                 `json:"reading_ref,omitempty"`
}

// InvokeDevice runs a scripted device call: it verifies the equipment lease at
// the completion logical time, tracks deterministic retries and promotes at
// most one successful reading. A fault never produces a reading; a late result
// after lease expiry is rejected with CodeLeaseExpired.
func (s *Service) InvokeDevice(ctx context.Context, taskID, caller, opID string, requestJSON []byte, expectedVersion int64, gen task.Generation, clock lease.LogicalClock, c DeviceCmd) (any, error) {
	return s.execute(ctx, taskID, caller, opID, requestJSON, expectedVersion, gen, clock, task.EvidenceDeviceInvocation, c, func(tx *sql.Tx, t *store.StoredTask) (any, error) {
		if _, err := store.CheckLease(tx, taskID, c.EquipmentID, clock); err != nil {
			return nil, err
		}

		inv, err := loadOrCreateInvocation(tx, taskID, opID, c)
		if err != nil {
			return nil, err
		}

		if c.Fault != "" {
			inv.Fault = c.Fault
			inv.Status = lease.InvocationPending
			next, ok := inv.ScheduleRetry(clock)
			if !ok {
				inv.Status = lease.InvocationManual
				inv.NextRetryAt = clock
			} else {
				inv.NextRetryAt = next
			}
			if err := store.PutInvocation(tx, taskID, *inv); err != nil {
				return nil, err
			}
			return DeviceResult{
				InvocationID: inv.ID, Status: inv.Status, Attempt: inv.Attempt,
				NextRetryAt: inv.NextRetryAt,
			}, nil
		}

		// Success: promote the invocation and record exactly one reading.
		readingRef := ""
		switch c.Kind {
		case lease.EquipmentThickness:
			if c.Reading == nil || !pointExists(t.Scope, c.Reading.FaceID, c.Reading.PointID) {
				return nil, errs.New(errs.CodeInvalidInput, "thickness reading missing valid point")
			}
			if err := store.InsertDryFilm(tx, taskID, task.DryFilmReading{
				FaceID: c.Reading.FaceID, PointID: c.Reading.PointID,
				ValueUM: c.Reading.Value, Generation: gen,
			}); err != nil {
				return nil, err
			}
			readingRef = c.Reading.FaceID + "/" + c.Reading.PointID
		case lease.EquipmentPullOff:
			if c.Reading == nil || c.Reading.SpecimenID == "" {
				return nil, errs.New(errs.CodeInvalidInput, "bond reading missing specimen")
			}
			if err := store.InsertBond(tx, taskID, task.BondSpecimenResult{
				SpecimenID: c.Reading.SpecimenID, ValueKPa: c.Reading.Value, Generation: gen,
			}); err != nil {
				return nil, err
			}
			readingRef = c.Reading.SpecimenID
		default:
			return nil, errs.New(errs.CodeInvalidInput, "equipment kind cannot produce a reading")
		}

		inv.Status = lease.InvocationSucceeded
		inv.ReadingRef = readingRef
		inv.Fault = ""
		if err := store.PutInvocation(tx, taskID, *inv); err != nil {
			return nil, err
		}
		return DeviceResult{InvocationID: inv.ID, Status: inv.Status, Attempt: inv.Attempt, ReadingRef: readingRef}, nil
	})
}

func loadOrCreateInvocation(tx *sql.Tx, taskID, opID string, c DeviceCmd) (*lease.DeviceInvocation, error) {
	id := c.InvocationID
	if id == "" {
		id = opID
	}
	inv, err := store.GetInvocationTx(tx, taskID, id)
	if err != nil {
		if de, ok := err.(*errs.Error); ok && de.Code == errs.CodeNotFound {
			return &lease.DeviceInvocation{
				ID: id, OperationID: opID, EquipmentID: c.EquipmentID, Kind: c.Kind,
				LogicalTime: 0, Attempt: 1,
			}, nil
		}
		return nil, err
	}
	// A retry of the same logical invocation increments the attempt counter.
	inv.Attempt++
	return inv, nil
}
