package scheduler

import (
	"math/big"
	"testing"
)

// TestSizeChunk covers the salted-truncation regression: sizeChunk must fold
// targetSec into the big.Int divide so a sub-1 base-words/sec rate (heavily
// salted jobs) is not truncated to 0, clamped to 1, and oversized.
func TestSizeChunk(t *testing.T) {
	bigInt := func(s string) *big.Int {
		v, ok := new(big.Int).SetString(s, 10)
		if !ok {
			t.Fatalf("bad big int %q", s)
		}
		return v
	}

	cases := []struct {
		name      string
		gapBase   int64
		baseKS    int64
		effKS     *big.Int
		speed     int64
		targetSec int
		minSec    int
		want      int64
	}{
		{
			// The agent-5 WPA incident: ~264k salts/word. Old code produced
			// 1800 base words (a ~9.5h chunk); correct is ~198 (~30 min).
			name: "salted WPA regression", gapBase: 12669404, baseKS: 12669404,
			effKS: bigInt("3344101855204"), speed: 29053, targetSec: 1800, minSec: 5,
			want: 198,
		},
		{
			// No multiplier signal yet -> take the whole gap so progress starts.
			name: "no effective keyspace -> whole gap", gapBase: 1000000, baseKS: 12669404,
			effKS: nil, speed: 29053, targetSec: 1800, minSec: 5,
			want: 1000000,
		},
		{
			// Fast unsalted big wordlist: speed*base overflows int64 but big.Int
			// is safe; computed target exceeds the gap -> whole gap, no overflow.
			name: "fast unsalted -> whole gap (no overflow)", gapBase: 1470000000, baseKS: 1470000000,
			effKS: big.NewInt(1470000000), speed: 70000000000, targetSec: 1800, minSec: 5,
			want: 1470000000,
		},
		{
			// Computed chunk bigger than the remaining gap -> take the gap.
			name: "tiny remaining gap", gapBase: 50, baseKS: 12669404,
			effKS: bigInt("3344101855204"), speed: 29053, targetSec: 1800, minSec: 5,
			want: 50,
		},
		{
			// Extreme multiplier: even the whole target window is < 1 base word;
			// dispatch the smallest indivisible unit (1), not a clamped-large chunk.
			name: "extreme multiplier -> 1 base word", gapBase: 1000, baseKS: 1000,
			effKS: bigInt("1000000000000000"), speed: 1000, targetSec: 1800, minSec: 5,
			want: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sizeChunk(tc.gapBase, tc.baseKS, tc.effKS, tc.speed, tc.targetSec, tc.minSec)
			if got != tc.want {
				t.Fatalf("sizeChunk = %d, want %d", got, tc.want)
			}
		})
	}
}
