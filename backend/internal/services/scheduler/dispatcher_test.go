package scheduler

import "testing"

// sizeChunk is pure logic — tested here without a DB. The DB-touching
// parts of the dispatcher are integration-tested separately.

func TestSizeChunk_GapFitsInTarget(t *testing.T) {
	// 100M gap, 1 GH/s, 60s target -> target = 60G; gap (100M) fits.
	// Returns gap whole.
	got := sizeChunk(100_000_000, 1_000_000_000, 60, 5)
	if got != 100_000_000 {
		t.Fatalf("expected whole gap, got %d", got)
	}
}

func TestSizeChunk_GapLargerThanTarget(t *testing.T) {
	// 1T gap, 1 GH/s, 60s target -> target = 60G. Take target.
	got := sizeChunk(1_000_000_000_000, 1_000_000_000, 60, 5)
	if got != 60_000_000_000 {
		t.Fatalf("expected 60G chunk, got %d", got)
	}
}

func TestSizeChunk_GapSmallerThanFloor(t *testing.T) {
	// 1M gap, 1 GH/s, 60s target, 5s floor -> floor = 5G.
	// Gap (1M) is smaller than floor. Take whole gap (plan §8.4).
	got := sizeChunk(1_000_000, 1_000_000_000, 60, 5)
	if got != 1_000_000 {
		t.Fatalf("expected whole tiny gap, got %d", got)
	}
}

func TestSizeChunk_PathologicalTargetLessThanFloor(t *testing.T) {
	// Operator misconfigured target=1s, min=5s. With speed 1 GH/s and a
	// gap of 100G, target=1G but floor=5G. Honor the floor.
	got := sizeChunk(100_000_000_000, 1_000_000_000, 1, 5)
	if got != 5_000_000_000 {
		t.Fatalf("expected floor honored (5G), got %d", got)
	}
}

func TestSizeChunk_ZeroGap(t *testing.T) {
	if got := sizeChunk(0, 1_000_000_000, 60, 5); got != 0 {
		t.Fatalf("zero gap should return 0, got %d", got)
	}
}

func TestSizeChunk_NegativeGap(t *testing.T) {
	if got := sizeChunk(-1, 1_000_000_000, 60, 5); got != 0 {
		t.Fatalf("negative gap should return 0, got %d", got)
	}
}

func TestSizeChunk_ZeroSpeed(t *testing.T) {
	// target = 0 * 60 = 0 -> defensive path takes whole gap.
	if got := sizeChunk(1_000_000, 0, 60, 5); got != 1_000_000 {
		t.Fatalf("zero-speed should fall back to whole gap, got %d", got)
	}
}
