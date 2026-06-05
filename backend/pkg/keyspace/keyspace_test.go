package keyspace

import (
	"testing"
)

func TestNew(t *testing.T) {
	k := New(42, Base)
	if k.Value() != 42 {
		t.Fatalf("Value() = %d, want 42", k.Value())
	}
	if k.Unit() != Base {
		t.Fatalf("Unit() = %v, want Base", k.Unit())
	}
}

func TestNewNegativePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on negative value")
		}
	}()
	New(-1, Base)
}

func TestZero(t *testing.T) {
	k := Zero(Effective)
	if !k.IsZero() {
		t.Fatal("Zero(Effective).IsZero() should be true")
	}
	if k.Unit() != Effective {
		t.Fatalf("Zero(Effective).Unit() = %v, want Effective", k.Unit())
	}
}

func TestAddSameUnit(t *testing.T) {
	a := New(100, Base)
	b := New(50, Base)
	c := a.Add(b)
	if c.Value() != 150 {
		t.Fatalf("Add: got %d, want 150", c.Value())
	}
	if c.Unit() != Base {
		t.Fatalf("Add: unit = %v, want Base", c.Unit())
	}
}

func TestAddDifferentUnitPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on cross-unit Add")
		}
	}()
	a := New(100, Base)
	b := New(50, Effective)
	a.Add(b)
}

func TestSub(t *testing.T) {
	a := New(100, Base)
	b := New(40, Base)
	c := a.Sub(b)
	if c.Value() != 60 {
		t.Fatalf("Sub: got %d, want 60", c.Value())
	}
}

func TestSubNegativePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on negative Sub result")
		}
	}()
	a := New(10, Base)
	b := New(20, Base)
	a.Sub(b)
}

func TestSubDifferentUnitPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on cross-unit Sub")
		}
	}()
	a := New(100, Base)
	b := New(50, Effective)
	a.Sub(b)
}

func TestLess(t *testing.T) {
	a := New(10, Base)
	b := New(20, Base)
	if !a.Less(b) {
		t.Fatal("10 < 20 should be true")
	}
	if b.Less(a) {
		t.Fatal("20 < 10 should be false")
	}
}

func TestLessDifferentUnitPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on cross-unit Less")
		}
	}()
	a := New(10, Base)
	b := New(20, Effective)
	a.Less(b)
}

func TestLessOrEqual(t *testing.T) {
	a := New(10, Base)
	b := New(10, Base)
	if !a.LessOrEqual(b) {
		t.Fatal("10 <= 10 should be true")
	}
	c := New(11, Base)
	if !a.LessOrEqual(c) {
		t.Fatal("10 <= 11 should be true")
	}
	if c.LessOrEqual(a) {
		t.Fatal("11 <= 10 should be false")
	}
}

func TestEqualSameUnit(t *testing.T) {
	a := New(42, Base)
	b := New(42, Base)
	if !a.Equal(b) {
		t.Fatal("equal same-unit values should be equal")
	}
	c := New(43, Base)
	if a.Equal(c) {
		t.Fatal("unequal values should not be equal")
	}
}

func TestEqualDifferentUnit(t *testing.T) {
	a := New(42, Base)
	b := New(42, Effective)
	// Equal returns false rather than panicking on unit mismatch.
	if a.Equal(b) {
		t.Fatal("values with different units should not be equal")
	}
}

func TestMinMax(t *testing.T) {
	a := New(10, Base)
	b := New(20, Base)
	if !Min(a, b).Equal(a) {
		t.Fatal("Min should return smaller value")
	}
	if !Max(a, b).Equal(b) {
		t.Fatal("Max should return larger value")
	}
	if !Min(b, a).Equal(a) {
		t.Fatal("Min should be commutative")
	}
}

func TestMinDifferentUnitPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on cross-unit Min")
		}
	}()
	a := New(10, Base)
	b := New(20, Effective)
	Min(a, b)
}

func TestStringRepresentation(t *testing.T) {
	if got := New(42, Base).String(); got != "42 base" {
		t.Fatalf("String(): got %q, want %q", got, "42 base")
	}
	if got := New(42, Effective).String(); got != "42 effective" {
		t.Fatalf("String(): got %q, want %q", got, "42 effective")
	}
}
