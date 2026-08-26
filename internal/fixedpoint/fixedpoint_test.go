package fixedpoint

import (
	"errors"
	"testing"
)

func TestAddAndSub(t *testing.T) {
	got, err := Add(New(2*Scale), New(3*Scale))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if want := New(5 * Scale); got != want {
		t.Fatalf("Add = %d, want %d", got.Raw(), want.Raw())
	}

	got, err = Sub(New(5*Scale), New(3*Scale))
	if err != nil {
		t.Fatalf("Sub: %v", err)
	}
	if want := New(2 * Scale); got != want {
		t.Fatalf("Sub = %d, want %d", got.Raw(), want.Raw())
	}
}

func TestAddOverflow(t *testing.T) {
	if _, err := Add(New(maxInt64), New(1)); !errors.Is(err, ErrOverflow) {
		t.Fatalf("Add overflow = %v, want ErrOverflow", err)
	}
}

func TestMulRoundsHalfAwayFromZero(t *testing.T) {
	// (1.5 * 3) = 4.5 -> rounds to 5.
	got, err := Mul(New(1_500_000), New(3))
	if err != nil {
		t.Fatalf("Mul: %v", err)
	}
	if want := New(5); got != want {
		t.Fatalf("Mul = %d, want %d", got.Raw(), want.Raw())
	}

	// (-1.5 * 3) = -4.5 -> rounds half away from zero to -5.
	got, err = Mul(New(-1_500_000), New(3))
	if err != nil {
		t.Fatalf("Mul negative: %v", err)
	}
	if want := New(-5); got != want {
		t.Fatalf("Mul negative = %d, want %d", got.Raw(), want.Raw())
	}
}

func TestDivRoundsHalfAwayFromZero(t *testing.T) {
	// 2/3 = 0.666666... -> rounds to 0.666667.
	got, err := Div(New(2), New(3))
	if err != nil {
		t.Fatalf("Div: %v", err)
	}
	if want := New(666_667); got != want {
		t.Fatalf("Div = %d, want %d", got.Raw(), want.Raw())
	}

	// -2/3 -> rounds half away from zero to -0.666667.
	got, err = Div(New(-2), New(3))
	if err != nil {
		t.Fatalf("Div negative: %v", err)
	}
	if want := New(-666_667); got != want {
		t.Fatalf("Div negative = %d, want %d", got.Raw(), want.Raw())
	}
}

func TestDivByZero(t *testing.T) {
	if _, err := Div(New(1), New(0)); !errors.Is(err, ErrDivideByZero) {
		t.Fatalf("Div by zero = %v, want ErrDivideByZero", err)
	}
}

func TestDivOverflow(t *testing.T) {
	if _, err := Div(New(maxInt64), New(1)); !errors.Is(err, ErrOverflow) {
		t.Fatalf("Div overflow = %v, want ErrOverflow", err)
	}
}

func TestAbsAndSign(t *testing.T) {
	if Abs(New(-5)).Raw() != 5 {
		t.Fatalf("Abs(-5) = %d, want 5", Abs(New(-5)).Raw())
	}
	if New(0).Sign() != 0 || New(7).Sign() != 1 || New(-7).Sign() != -1 {
		t.Fatalf("Sign misreported")
	}
}
