package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

// BigInt is an arbitrary-precision integer used for keyspace values that can
// exceed int64 / Postgres BIGINT.
//
// Effective keyspace = base_keyspace × rule_multiplier × salt_count. For the
// large wordlists this project supports (100GB ≈ 8-10 billion lines) combined
// with big rule files and salted hash types, that product routinely passes
// 9,223,372,036,854,775,807 (the shared int64 / BIGINT ceiling). Those columns
// are stored as Postgres NUMERIC and represented here as BigInt so the value
// is never silently wrapped.
//
// Serialization rules:
//   - DB: NUMERIC, via driver.Valuer (decimal string) / sql.Scanner.
//   - JSON: a decimal STRING, NOT a number. JavaScript numbers lose precision
//     above 2^53 (~9.0e15), so the frontend treats these fields as strings.
//     UnmarshalJSON still accepts a JSON number for inbound robustness (e.g.
//     agent progress payloads that send numbers).
//
// Nullability: the zero value is a valid zero. A nil *BigInt scans and
// serializes as NULL / null, so *BigInt is the drop-in replacement for the
// former *int64 fields and existing nil checks keep working unchanged.
type BigInt struct {
	v *big.Int // nil is treated as zero
}

// NewBigInt returns a BigInt holding i.
func NewBigInt(i int64) BigInt {
	return BigInt{v: big.NewInt(i)}
}

// NewBigIntPtr returns a *BigInt holding i (never nil).
func NewBigIntPtr(i int64) *BigInt {
	b := NewBigInt(i)
	return &b
}

// BigIntFromBig returns a BigInt copy of b. A nil b yields zero.
func BigIntFromBig(b *big.Int) BigInt {
	if b == nil {
		return BigInt{}
	}
	return BigInt{v: new(big.Int).Set(b)}
}

// BigIntPtrFromBig returns a *BigInt copy of b, or nil if b is nil.
func BigIntPtrFromBig(b *big.Int) *BigInt {
	if b == nil {
		return nil
	}
	r := BigIntFromBig(b)
	return &r
}

// ParseBigInt parses a decimal string (an optional fractional part of all
// zeros is tolerated, e.g. "123.0000000000" from a NUMERIC scan).
func ParseBigInt(s string) (BigInt, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return BigInt{}, nil
	}
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		// Tolerate a zero fractional part; reject a non-zero one.
		frac := strings.TrimRight(s[dot+1:], "0")
		if frac != "" {
			return BigInt{}, fmt.Errorf("BigInt: non-integer value %q", s)
		}
		s = s[:dot]
		if s == "" || s == "-" || s == "+" {
			s += "0"
		}
	}
	z, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return BigInt{}, fmt.Errorf("BigInt: invalid decimal %q", s)
	}
	return BigInt{v: z}, nil
}

// big returns a non-nil *big.Int view (zero when unset). Callers must not
// mutate the result; arithmetic helpers always allocate fresh values.
func (b BigInt) big() *big.Int {
	if b.v == nil {
		return new(big.Int)
	}
	return b.v
}

// Big returns an independent *big.Int copy of the value.
func (b BigInt) Big() *big.Int { return new(big.Int).Set(b.big()) }

// Int64 returns the value as int64. If the value exceeds int64 the result is
// implementation-defined (big.Int.Int64 truncates); use Int64Checked when the
// magnitude is uncertain.
func (b BigInt) Int64() int64 { return b.big().Int64() }

// Int64Checked returns the int64 value and whether it fits in int64.
func (b BigInt) Int64Checked() (int64, bool) {
	z := b.big()
	if z.IsInt64() {
		return z.Int64(), true
	}
	return z.Int64(), false
}

// Sign returns -1, 0, or +1.
func (b BigInt) Sign() int { return b.big().Sign() }

// IsZero reports whether the value is zero.
func (b BigInt) IsZero() bool { return b.big().Sign() == 0 }

// IsPositive reports whether the value is > 0.
func (b BigInt) IsPositive() bool { return b.big().Sign() > 0 }

// String returns the decimal representation.
func (b BigInt) String() string { return b.big().String() }

// Cmp compares b and o (-1, 0, +1).
func (b BigInt) Cmp(o BigInt) int { return b.big().Cmp(o.big()) }

// CmpInt64 compares b and i.
func (b BigInt) CmpInt64(i int64) int { return b.big().Cmp(big.NewInt(i)) }

// Add returns b + o.
func (b BigInt) Add(o BigInt) BigInt { return BigInt{v: new(big.Int).Add(b.big(), o.big())} }

// AddInt64 returns b + i.
func (b BigInt) AddInt64(i int64) BigInt { return BigInt{v: new(big.Int).Add(b.big(), big.NewInt(i))} }

// Sub returns b - o.
func (b BigInt) Sub(o BigInt) BigInt { return BigInt{v: new(big.Int).Sub(b.big(), o.big())} }

// Mul returns b * o.
func (b BigInt) Mul(o BigInt) BigInt { return BigInt{v: new(big.Int).Mul(b.big(), o.big())} }

// MulInt64 returns b * i.
func (b BigInt) MulInt64(i int64) BigInt { return BigInt{v: new(big.Int).Mul(b.big(), big.NewInt(i))} }

// Div returns b / o (truncated). Dividing by zero returns zero.
func (b BigInt) Div(o BigInt) BigInt {
	if o.big().Sign() == 0 {
		return BigInt{}
	}
	return BigInt{v: new(big.Int).Quo(b.big(), o.big())}
}

// DivInt64 returns b / i (truncated). Dividing by zero returns zero.
func (b BigInt) DivInt64(i int64) BigInt {
	if i == 0 {
		return BigInt{}
	}
	return BigInt{v: new(big.Int).Quo(b.big(), big.NewInt(i))}
}

// Value implements driver.Valuer, emitting a decimal string for NUMERIC.
// Implemented on the value receiver so both BigInt and *BigInt satisfy
// driver.Valuer; database/sql converts a nil *BigInt to NULL before calling.
func (b BigInt) Value() (driver.Value, error) {
	return b.String(), nil
}

// Scan implements sql.Scanner for Postgres NUMERIC (returned as []byte/string
// by lib/pq), and tolerates int64/float64 from other drivers.
func (b *BigInt) Scan(src interface{}) error {
	switch s := src.(type) {
	case nil:
		b.v = nil
		return nil
	case []byte:
		parsed, err := ParseBigInt(string(s))
		if err != nil {
			return err
		}
		*b = parsed
		return nil
	case string:
		parsed, err := ParseBigInt(s)
		if err != nil {
			return err
		}
		*b = parsed
		return nil
	case int64:
		b.v = big.NewInt(s)
		return nil
	case float64:
		bi, _ := big.NewFloat(s).Int(nil)
		b.v = bi
		return nil
	default:
		return fmt.Errorf("BigInt: cannot scan %T", src)
	}
}

// MarshalJSON emits a decimal string (see type doc).
func (b BigInt) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.String())
}

// UnmarshalJSON accepts a JSON string or number.
func (b *BigInt) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "null" || s == "" {
		b.v = nil
		return nil
	}
	s = strings.Trim(s, `"`)
	parsed, err := ParseBigInt(s)
	if err != nil {
		return err
	}
	*b = parsed
	return nil
}
