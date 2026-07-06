// Package keyspace provides a unit-aware keyspace value type for the scheduler
// rewrite. The c439089d-class bug — comparing a base keyspace value against an
// effective keyspace value — becomes impossible because Add / Sub / Less panic
// when the two operands have different units. Cross-unit conversion is the
// caller's responsibility: extract the raw int64 with Value(), apply the
// per-scheduling-unit amplifier explicitly, then wrap the result in a new
// Keyspace with the target unit.
package keyspace

import (
	"fmt"
)

// Unit distinguishes the two keyspace coordinate systems the scheduler reasons
// about.
type Unit int

const (
	// Base is the dispatcher's coordinate system: wordlist words, mask
	// candidates, or association wordlist entries. This is what hashcat's
	// --skip and --limit operate on.
	Base Unit = iota

	// Effective is the hashcat-internal coordinate system: base * rules *
	// salts for a salted rule-split attack, or whatever progress[1] reports
	// for a given attack. This is what the user sees as "total candidates".
	Effective
)

func (u Unit) String() string {
	switch u {
	case Base:
		return "base"
	case Effective:
		return "effective"
	default:
		return fmt.Sprintf("unit(%d)", int(u))
	}
}

// Keyspace is a non-negative int64 with an attached unit. Two-keyspace
// operations require matching units; otherwise they panic, since mixing units
// is always a programmer error.
type Keyspace struct {
	value int64
	unit  Unit
}

// New constructs a Keyspace. Panics on negative value because keyspace counts
// cannot be negative.
func New(value int64, unit Unit) Keyspace {
	if value < 0 {
		panic(fmt.Sprintf("keyspace.New: negative value %d", value))
	}
	return Keyspace{value: value, unit: unit}
}

// Zero returns a zero-valued Keyspace in the given unit.
func Zero(unit Unit) Keyspace {
	return Keyspace{value: 0, unit: unit}
}

// Value returns the raw int64. The unit is implicit in how the caller asked
// for the Keyspace, so callers that need to multiply by an amplifier do so
// explicitly: keyspace.New(k.Value()*amp, keyspace.Effective).
func (k Keyspace) Value() int64 { return k.value }

// Unit returns the keyspace's unit.
func (k Keyspace) Unit() Unit { return k.unit }

// String renders the keyspace as "<value> <unit>" for logs.
func (k Keyspace) String() string {
	return fmt.Sprintf("%d %s", k.value, k.unit)
}

// IsZero reports whether the value is zero.
func (k Keyspace) IsZero() bool { return k.value == 0 }

// Add returns a + b. Panics if units differ.
func (a Keyspace) Add(b Keyspace) Keyspace {
	a.requireSameUnit(b, "Add")
	return Keyspace{value: a.value + b.value, unit: a.unit}
}

// Sub returns a - b. Panics if units differ or if the result would be
// negative.
func (a Keyspace) Sub(b Keyspace) Keyspace {
	a.requireSameUnit(b, "Sub")
	if b.value > a.value {
		panic(fmt.Sprintf("keyspace.Sub: result would be negative (%d - %d)", a.value, b.value))
	}
	return Keyspace{value: a.value - b.value, unit: a.unit}
}

// Less reports a < b. Panics if units differ.
func (a Keyspace) Less(b Keyspace) bool {
	a.requireSameUnit(b, "Less")
	return a.value < b.value
}

// LessOrEqual reports a <= b. Panics if units differ.
func (a Keyspace) LessOrEqual(b Keyspace) bool {
	a.requireSameUnit(b, "LessOrEqual")
	return a.value <= b.value
}

// Equal reports a == b (value and unit). Returns false on unit mismatch
// rather than panicking, because equality is sometimes asked of mixed values
// during validation.
func (a Keyspace) Equal(b Keyspace) bool {
	return a.unit == b.unit && a.value == b.value
}

// Min returns whichever of a or b has the smaller value. Panics if units
// differ.
func Min(a, b Keyspace) Keyspace {
	a.requireSameUnit(b, "Min")
	if a.value < b.value {
		return a
	}
	return b
}

// Max returns whichever of a or b has the larger value. Panics if units
// differ.
func Max(a, b Keyspace) Keyspace {
	a.requireSameUnit(b, "Max")
	if a.value > b.value {
		return a
	}
	return b
}

func (a Keyspace) requireSameUnit(b Keyspace, op string) {
	if a.unit != b.unit {
		panic(fmt.Sprintf("keyspace.%s: unit mismatch (%s vs %s)", op, a.unit, b.unit))
	}
}
