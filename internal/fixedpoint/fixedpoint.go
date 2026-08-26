// Package fixedpoint provides deterministic integer fixed-point arithmetic
// used for every dimensional, mass, thickness and strength quantity in the
// steel-fireproofing evidence-closure domain. All values are stored as
// signed int64 raw counts of 1/Scale units, so that measurement, material
// conservation and threshold comparisons are exact and reproducible.
package fixedpoint

import (
	"errors"
	"math/big"
)

// Scale is the uniform fixed-point scaling factor shared across the domain.
// A raw value of n represents n/Scale physical units.
const Scale int64 = 1_000_000

// ErrOverflow indicates that an operation produced a result outside int64.
var ErrOverflow = errors.New("fixedpoint: result overflows int64")

// ErrDivideByZero indicates division by a zero denominator.
var ErrDivideByZero = errors.New("fixedpoint: divide by zero")

// Value is a signed fixed-point quantity stored as raw integer units
// scaled by Scale.
type Value int64

// New returns the fixed-point value whose raw representation is raw.
func New(raw int64) Value { return Value(raw) }

// Raw returns the underlying scaled integer.
func (v Value) Raw() int64 { return int64(v) }

// Add returns a+b, or ErrOverflow if the sum would exceed int64.
func Add(a, b Value) (Value, error) {
	x, y := a.Raw(), b.Raw()
	if (y > 0 && x > maxInt64-y) || (y < 0 && x < minInt64-y) {
		return 0, ErrOverflow
	}
	return Value(x + y), nil
}

// Sub returns a-b, or ErrOverflow if the difference would exceed int64.
func Sub(a, b Value) (Value, error) {
	x, y := a.Raw(), b.Raw()
	if (y < 0 && x > maxInt64+y) || (y > 0 && x < minInt64+y) {
		return 0, ErrOverflow
	}
	return Value(x - y), nil
}

// Mul returns (a*b)/Scale rounded half away from zero.
//
// It is used for products such as area usage (area × unit consumption) and
// coefficient scaling, and reports ErrOverflow if the exact product cannot
// be represented in int64.
func Mul(a, b Value) (Value, error) {
	p := new(big.Int).Mul(big.NewInt(a.Raw()), big.NewInt(b.Raw()))
	return roundHalfAwayFromZero(p, big.NewInt(Scale))
}

// Div returns (a*Scale)/b rounded half away from zero.
//
// It divides fixed-point quantities (for example computing a section factor
// from perimeter and area). A zero denominator yields ErrDivideByZero.
func Div(a, b Value) (Value, error) {
	if b.Raw() == 0 {
		return 0, ErrDivideByZero
	}
	p := new(big.Int).Mul(big.NewInt(a.Raw()), big.NewInt(Scale))
	return roundHalfAwayFromZero(p, big.NewInt(b.Raw()))
}

// roundHalfAwayFromZero divides p by d and rounds the quotient half away
// from zero, mirroring the domain's deterministic rounding rule.
func roundHalfAwayFromZero(p, d *big.Int) (Value, error) {
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(p, d, r)

	twiceAbsR := new(big.Int).Abs(r)
	twiceAbsR.Lsh(twiceAbsR, 1) // 2*|r|
	if twiceAbsR.Cmp(new(big.Int).Abs(d)) >= 0 {
		if p.Sign() >= 0 {
			q.Add(q, big.NewInt(1))
		} else {
			q.Sub(q, big.NewInt(1))
		}
	}
	if !q.IsInt64() {
		return 0, ErrOverflow
	}
	return Value(q.Int64()), nil
}

// Abs returns the absolute value of v.
func Abs(v Value) Value {
	if v.Raw() < 0 {
		return Value(-v.Raw())
	}
	return v
}

// Sign reports -1, 0, or +1 for negative, zero, or positive values.
func (v Value) Sign() int {
	switch {
	case v.Raw() < 0:
		return -1
	case v.Raw() > 0:
		return 1
	default:
		return 0
	}
}

const (
	maxInt64 = int64(^uint64(0) >> 1)
	minInt64 = -maxInt64 - 1
)
