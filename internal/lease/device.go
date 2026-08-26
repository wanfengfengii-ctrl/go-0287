package lease

import "example.com/steel-fireproofing-evidence-closure/internal/errs"

// FaultType discriminates scriptable device failures. Each fault maps to a
// stable rejection code and never produces a valid reading.
type FaultType string

const (
	FaultNone          FaultType = ""
	FaultRejected      FaultType = "rejected"
	FaultDisconnected  FaultType = "disconnected"
	FaultTimeout       FaultType = "timeout"
	FaultFormatInvalid FaultType = "format_invalid"
)

// Code returns the stable error code associated with a fault type.
func (f FaultType) Code() errs.Code {
	switch f {
	case FaultRejected:
		return errs.CodeDeviceRejected
	case FaultDisconnected:
		return errs.CodeDeviceDisconnected
	case FaultTimeout:
		return errs.CodeDeviceTimeout
	case FaultFormatInvalid:
		return errs.CodeDeviceFormatInvalid
	default:
		return errs.CodeInvalidInput
	}
}

// InvocationStatus is the lifecycle state of a scripted device call.
type InvocationStatus string

const (
	InvocationPending   InvocationStatus = "pending"
	InvocationSucceeded InvocationStatus = "succeeded"
	InvocationManual    InvocationStatus = "manual"
)

// MaxRetries is the deterministic retry ceiling; beyond it an invocation is
// escalated to manual handling instead of synthesizing a reading.
const MaxRetries = 3

// DeviceInvocation records one scripted device call attempt. Failures append a
// pending invocation with a deterministic next-retry logical time; a success
// promotes at most one invocation to succeeded and may reference a valid
// reading.
type DeviceInvocation struct {
	ID          string           `json:"id"`
	OperationID string           `json:"operation_id"`
	EquipmentID string           `json:"equipment_id"`
	Kind        EquipmentKind    `json:"kind"`
	LogicalTime LogicalClock     `json:"logical_time"`
	Fault       FaultType        `json:"fault,omitempty"`
	Attempt     int              `json:"attempt"`
	NextRetryAt LogicalClock     `json:"next_retry_at"`
	Status      InvocationStatus `json:"status"`
	ReadingRef  string           `json:"reading_ref,omitempty"`
}

// RetryBackoffTicks returns the deterministic retry delay for the given
// 1-based attempt number, growing geometrically: 5, 25, 125 logical ticks.
func RetryBackoffTicks(attempt int) LogicalClock {
	ticks := LogicalClock(5)
	for i := 1; i < attempt && i < MaxRetries; i++ {
		ticks *= 5
	}
	return ticks
}

// ScheduleRetry returns the logical time at which a failed invocation should
// next be attempted, or zero with ok=false when the retry ceiling is reached.
func (d *DeviceInvocation) ScheduleRetry(now LogicalClock) (LogicalClock, bool) {
	if d.Attempt >= MaxRetries {
		return 0, false
	}
	return now + RetryBackoffTicks(d.Attempt+1), true
}
