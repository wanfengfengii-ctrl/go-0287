package task

import (
	"testing"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
)

func TestDecideTerminalOnce(t *testing.T) {
	tsk := &Task{ID: "t1", CurrentGen: 1}
	if err := tsk.DecideTerminal("reviewer-a", TerminalAccepted); err != nil {
		t.Fatalf("DecideTerminal: %v", err)
	}
	if tsk.Terminal == nil || tsk.Terminal.State != TerminalAccepted {
		t.Fatalf("terminal = %+v, want accepted", tsk.Terminal)
	}

	err := tsk.DecideTerminal("reviewer-b", TerminalQuarantined)
	de, ok := err.(*errs.Error)
	if !ok || de.Code != errs.CodeTerminalAlreadyDecided {
		t.Fatalf("second decision error = %v, want CodeTerminalAlreadyDecided", err)
	}
	if tsk.Terminal.State != TerminalAccepted {
		t.Fatalf("terminal mutated to %s", tsk.Terminal.State)
	}
}

func TestDecideTerminalInvalidState(t *testing.T) {
	tsk := &Task{ID: "t2"}
	if err := tsk.DecideTerminal("reviewer-a", TerminalState("bogus")); err == nil {
		t.Fatalf("invalid state = nil, want error")
	}
	if tsk.Terminal != nil {
		t.Fatalf("invalid state must not write terminal")
	}
}

func TestDigestDeterministic(t *testing.T) {
	a := Digest([]byte("payload"))
	b := Digest([]byte("payload"))
	c := Digest([]byte("other"))
	if a != b {
		t.Fatalf("digest not deterministic: %s vs %s", a, b)
	}
	if a == c {
		t.Fatalf("different payloads must differ")
	}
}
