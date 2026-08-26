package lease

import (
	"testing"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
)

func TestMaterialBatchConservation(t *testing.T) {
	b := MaterialBatch{OpenedGrams: 1000, IssuedGrams: 300, RecoveredGrams: 100, ApprovedWasteGrams: 50}
	if got, want := b.Balance(), int64(550); got != want {
		t.Fatalf("Balance = %d, want %d", got, want)
	}
	if !b.Conserved() {
		t.Fatalf("batch should be conserved")
	}
}

func TestMaterialBatchIssueDeductsBalance(t *testing.T) {
	b := MaterialBatch{OpenedGrams: 1000}
	bal, err := b.Issue(250)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if bal != 750 || b.IssuedGrams != 250 {
		t.Fatalf("Issue balance=%d issued=%d, want 750/250", bal, b.IssuedGrams)
	}
}

func TestMaterialBatchIssueInsufficient(t *testing.T) {
	b := MaterialBatch{OpenedGrams: 100}
	_, err := b.Issue(101)
	de, ok := err.(*errs.Error)
	if !ok || de.Code != errs.CodeMaterialInsufficient {
		t.Fatalf("insufficient error = %v, want CodeMaterialInsufficient", err)
	}
	if b.IssuedGrams != 0 {
		t.Fatalf("failed issue must not mutate ledger, issued=%d", b.IssuedGrams)
	}
}

func TestMaterialBatchExpired(t *testing.T) {
	b := MaterialBatch{OpenedGrams: 100, Expired: true}
	_, err := b.Issue(10)
	de, ok := err.(*errs.Error)
	if !ok || de.Code != errs.CodeMaterialExpired {
		t.Fatalf("expired error = %v, want CodeMaterialExpired", err)
	}
}

func TestLeaseAcquireBusy(t *testing.T) {
	m := NewLeaseManager()
	if err := m.Acquire("sprayer-1", "task-a", 0, 100); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	err := m.Acquire("sprayer-1", "task-b", 0, 100)
	de, ok := err.(*errs.Error)
	if !ok || de.Code != errs.CodeLeaseBusy {
		t.Fatalf("busy error = %v, want CodeLeaseBusy", err)
	}
}

func TestLeaseExpired(t *testing.T) {
	m := NewLeaseManager()
	if err := m.Acquire("gauge-1", "task-a", 0, 50); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := m.Check("gauge-1", 60); err == nil {
		t.Fatalf("Check after expiry = nil, want error")
	} else if de, ok := err.(*errs.Error); !ok || de.Code != errs.CodeLeaseExpired {
		t.Fatalf("expiry error = %v, want CodeLeaseExpired", err)
	}
}

func TestLeaseExpiryMustBeAfterNow(t *testing.T) {
	m := NewLeaseManager()
	if err := m.Acquire("scale-1", "task-a", 10, 10); err == nil {
		t.Fatalf("Acquire with expiry<=now = nil, want error")
	}
}
