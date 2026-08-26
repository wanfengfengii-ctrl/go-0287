// Package lease implements the "材料用量及设备租约管理器" component: integer
// gram material accounting with strict mass conservation, and logical-clock
// exclusive leases for spray and measurement equipment.
package lease

import (
	"example.com/steel-fireproofing-evidence-closure/internal/errs"
)

// LogicalClock is a monotonically increasing logical time measured in
// integer ticks. It orders all domain events deterministically.
type LogicalClock int64

// EquipmentKind identifies a managed device class.
type EquipmentKind string

const (
	EquipmentSprayer     EquipmentKind = "sprayer"
	EquipmentWetFilmComb EquipmentKind = "wet_film_comb"
	EquipmentScale       EquipmentKind = "scale"
	EquipmentThermoHygro EquipmentKind = "thermo_hygro"
	EquipmentThickness   EquipmentKind = "thickness_gauge"
	EquipmentPullOff     EquipmentKind = "pull_off_tester"
)

// Equipment is a device with a calibration summary.
type Equipment struct {
	ID                 string        `json:"id"`
	Kind               EquipmentKind `json:"kind"`
	CalibrationSummary string        `json:"calibration_summary"`
}

// MaterialBatch is an opened coating batch with a non-negative integer-gram
// consumable balance. Its mass ledger must always satisfy the conservation
// invariant: opened + toppedUp = issued + balance + recovered + approvedWaste.
type MaterialBatch struct {
	ID                 string `json:"id"`
	ProofSummary       string `json:"proof_summary"`
	Expired            bool   `json:"expired"`
	OpenedGrams        int64  `json:"opened_grams"`
	ToppedUpGrams      int64  `json:"topped_up_grams"`
	IssuedGrams        int64  `json:"issued_grams"`
	RecoveredGrams     int64  `json:"recovered_grams"`
	ApprovedWasteGrams int64  `json:"approved_waste_grams"`
}

// Balance returns the current consumable balance in integer grams.
func (b MaterialBatch) Balance() int64 {
	return b.OpenedGrams + b.ToppedUpGrams - b.IssuedGrams - b.RecoveredGrams - b.ApprovedWasteGrams
}

// Conserved reports whether the mass conservation invariant holds.
func (b MaterialBatch) Conserved() bool {
	return b.Balance() >= 0 && b.OpenedGrams >= 0 && b.ToppedUpGrams >= 0 &&
		b.IssuedGrams >= 0 && b.RecoveredGrams >= 0 && b.ApprovedWasteGrams >= 0
}

// Issue deducts grams from the consumable balance, returning the remaining
// balance or CodeMaterialInsufficient when the batch cannot cover the amount.
// The batch must be non-expired.
func (b *MaterialBatch) Issue(grams int64) (int64, error) {
	if grams < 0 {
		return b.Balance(), errs.New(errs.CodeInvalidInput, "issue amount must be non-negative")
	}
	if b.Expired {
		return b.Balance(), errs.New(errs.CodeMaterialExpired, "material batch is expired")
	}
	if b.Balance() < grams {
		return b.Balance(), errs.New(errs.CodeMaterialInsufficient, "material batch balance is insufficient")
	}
	b.IssuedGrams += grams
	return b.Balance(), nil
}

// Recover returns grams to the consumable pool (recovery), increasing the
// balance. Values must be non-negative.
func (b *MaterialBatch) Recover(grams int64) (int64, error) {
	if grams < 0 {
		return b.Balance(), errs.New(errs.CodeInvalidInput, "recovery amount must be non-negative")
	}
	b.RecoveredGrams += grams
	return b.Balance(), nil
}

// Lease is a logical-clock bounded exclusive lease on an equipment item.
type Lease struct {
	EquipmentID string       `json:"equipment_id"`
	OwnerID     string       `json:"owner_id"`
	ExpiresAt   LogicalClock `json:"expires_at"`
}

// Active reports whether the lease has not yet expired at clock now.
func (l Lease) Active(now LogicalClock) bool {
	return now < l.ExpiresAt
}

// LeaseManager holds exclusive leases keyed by equipment and enforces the
// "at most one active lease per equipment" invariant.
type LeaseManager struct {
	leases map[string]Lease
}

// NewLeaseManager returns an empty lease manager.
func NewLeaseManager() *LeaseManager {
	return &LeaseManager{leases: make(map[string]Lease)}
}

// Acquire grants an exclusive lease until expiresAt, or returns CodeLeaseBusy
// when the equipment is already leased and the existing lease is still active.
func (m *LeaseManager) Acquire(equipmentID, ownerID string, now, expiresAt LogicalClock) error {
	if expiresAt <= now {
		return errs.New(errs.CodeInvalidInput, "lease expiry must be after now")
	}
	if l, ok := m.leases[equipmentID]; ok && l.Active(now) {
		return errs.New(errs.CodeLeaseBusy, "equipment lease is already held")
	}
	m.leases[equipmentID] = Lease{EquipmentID: equipmentID, OwnerID: ownerID, ExpiresAt: expiresAt}
	return nil
}

// ActiveLease returns the active lease for an equipment item at now, if any.
func (m *LeaseManager) ActiveLease(equipmentID string, now LogicalClock) (Lease, bool) {
	l, ok := m.leases[equipmentID]
	if !ok || !l.Active(now) {
		return Lease{}, false
	}
	return l, true
}

// Check returns CodeLeaseExpired if the equipment's lease is missing or
// expired at now, and nil otherwise.
func (m *LeaseManager) Check(equipmentID string, now LogicalClock) error {
	if _, ok := m.ActiveLease(equipmentID, now); !ok {
		return errs.New(errs.CodeLeaseExpired, "equipment lease is expired or absent")
	}
	return nil
}

// MaterialAccount is the persistence boundary for material and lease
// mutation; implementations must apply issue/recover and lease acquire/release
// atomically within a single transaction.
type MaterialAccount interface {
	// Deduct issues grams from a batch and acquires an equipment lease in one
	// transaction; it returns the new balance.
	Deduct(batchID string, grams int64, equipmentID, ownerID string, now, expiresAt LogicalClock) (int64, error)
}
