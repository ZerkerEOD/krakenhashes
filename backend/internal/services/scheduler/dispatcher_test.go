package scheduler

import "testing"

// sizeChunk is pure logic — tested here without a DB. The DB-touching
// parts of the dispatcher are integration-tested separately.
//
// sizeChunk's signature after the base-keyspace refactor:
//
//	sizeChunk(gapBase, baseKeyspace, effectiveKeyspace, speed, targetSec, minSec)
//
// where baseKeyspace == effectiveKeyspace implies multiplier=1 (the
// dict-only / mask-only attacks with no rules and no salts). Most tests
// below use that shape; TestSizeChunk_WithMultiplier exercises the
// multiplier-aware path explicitly.

func TestSizeChunk_GapFitsInTarget(t *testing.T) {
	// Multiplier=1, speed=1GH/s, target=60s → basePerSec=1GH, target=60G.
	// Gap (100M) fits. Return whole gap.
	got := sizeChunk(100_000_000, 1_000_000_000, 1_000_000_000, 1_000_000_000, 60, 5)
	if got != 100_000_000 {
		t.Fatalf("expected whole gap, got %d", got)
	}
}

func TestSizeChunk_GapLargerThanTarget(t *testing.T) {
	// Multiplier=1, gap=1T → target=60G wins.
	got := sizeChunk(1_000_000_000_000, 1_000_000_000, 1_000_000_000, 1_000_000_000, 60, 5)
	if got != 60_000_000_000 {
		t.Fatalf("expected 60G chunk, got %d", got)
	}
}

func TestSizeChunk_GapSmallerThanFloor(t *testing.T) {
	// Multiplier=1, gap (1M) < floor (5G) — take whole gap (plan §8.4).
	got := sizeChunk(1_000_000, 1_000_000_000, 1_000_000_000, 1_000_000_000, 60, 5)
	if got != 1_000_000 {
		t.Fatalf("expected whole tiny gap, got %d", got)
	}
}

func TestSizeChunk_PathologicalTargetLessThanFloor(t *testing.T) {
	// target=1s, min=5s, multiplier=1, speed=1GH/s → target=1G, floor=5G.
	// Honor the floor.
	got := sizeChunk(100_000_000_000, 1_000_000_000, 1_000_000_000, 1_000_000_000, 1, 5)
	if got != 5_000_000_000 {
		t.Fatalf("expected floor honored (5G), got %d", got)
	}
}

func TestSizeChunk_ZeroGap(t *testing.T) {
	if got := sizeChunk(0, 1_000_000_000, 1_000_000_000, 1_000_000_000, 60, 5); got != 0 {
		t.Fatalf("zero gap should return 0, got %d", got)
	}
}

func TestSizeChunk_NegativeGap(t *testing.T) {
	if got := sizeChunk(-1, 1_000_000_000, 1_000_000_000, 1_000_000_000, 60, 5); got != 0 {
		t.Fatalf("negative gap should return 0, got %d", got)
	}
}

func TestSizeChunk_ZeroSpeed(t *testing.T) {
	// No multiplier signal — fall back to whole gap.
	if got := sizeChunk(1_000_000, 1_000_000_000, 1_000_000_000, 0, 60, 5); got != 1_000_000 {
		t.Fatalf("zero-speed should fall back to whole gap, got %d", got)
	}
}

func TestSizeChunk_NilBaseKeyspace(t *testing.T) {
	// baseKeyspace=0 means migration 000151 hasn't backfilled this row;
	// fall back to whole gap so we can still make progress while the
	// dispatcher logs a skip warning at the caller.
	if got := sizeChunk(1_000_000, 0, 1_000_000_000, 1_000_000_000, 60, 5); got != 1_000_000 {
		t.Fatalf("zero baseKeyspace should fall back to whole gap, got %d", got)
	}
}

func TestSizeChunk_WithMultiplier(t *testing.T) {
	// Live-job numbers from the bug report:
	//   speed             = 1.073 GH/s effective
	//   baseKeyspace      = 23,641,335 (wordlist size)
	//   effectiveKeyspace = 6,240,154,014,585 (base × ~264K rules)
	//   target            = 60s
	// → basePerSec = 1.073G × 23.6M / 6.24T ≈ 4063 base/sec
	// → target chunk = 4063 × 60 ≈ 243,800 base words
	got := sizeChunk(
		23_641_335, // gap = full wordlist
		23_641_335, // baseKeyspace
		6_240_154_014_585, // effectiveKeyspace
		1_073_000_000, // speed (effective H/s)
		60, // targetSec
		5,  // minSec
	)
	// Allow a small tolerance for integer rounding in the multiplier division.
	if got < 200_000 || got > 300_000 {
		t.Fatalf("expected ~244K base words for 60s chunk at 264K multiplier, got %d", got)
	}
}

func TestSizeChunk_SaltReductionShrinksMultiplier(t *testing.T) {
	// As salts get removed mid-job, effective_keyspace decreases. Same
	// speed and base, but smaller multiplier → larger base chunk.
	// Setup: 100 salts, 264K rules originally → mult=26.4M.
	// After 90% cracked: 10 salts left → mult=2.64M (10× smaller),
	// chunk should be 10× larger.
	speed := int64(1_073_000_000)
	base := int64(23_641_335)
	effFull := int64(6_240_154_014_585) * 100   // 100 salts
	effShrunk := int64(6_240_154_014_585) * 10  // 10 salts

	chunkFull := sizeChunk(base*1000, base, effFull, speed, 60, 5)
	chunkShrunk := sizeChunk(base*1000, base, effShrunk, speed, 60, 5)

	// After salts shrink 10×, chunk should grow ~10×.
	ratio := float64(chunkShrunk) / float64(chunkFull)
	if ratio < 9.0 || ratio > 11.0 {
		t.Fatalf("expected ~10× larger chunk after 90%% salt reduction, got %.2f× (full=%d, shrunk=%d)", ratio, chunkFull, chunkShrunk)
	}
}
