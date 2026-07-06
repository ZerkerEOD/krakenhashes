package scheduler

import (
	"context"
	"errors"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

// --- Fakes ------------------------------------------------------------------

type fakeUnitReader struct {
	units []*models.SchedulingUnit
	err   error
}

func (f *fakeUnitReader) GetSchedulable(_ context.Context) ([]*models.SchedulingUnit, error) {
	return f.units, f.err
}

type fakeIntervalReader struct {
	// gaps maps unit ID -> the gap list returned for that unit. Missing
	// IDs return empty.
	gaps map[uuid.UUID][]models.UndispatchedRange
	err  error
}

func (f *fakeIntervalReader) UndispatchedRanges(_ context.Context, unitID uuid.UUID) ([]models.UndispatchedRange, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.gaps[unitID], nil
}

// --- Tests ------------------------------------------------------------------

func TestSelector_NoCandidates(t *testing.T) {
	out, err := SelectSchedulableUnits(context.Background(), &fakeUnitReader{}, &fakeIntervalReader{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected no units, got %d", len(out))
	}
}

func TestSelector_FiltersOutUnitsWithNoGaps(t *testing.T) {
	withGap := uuid.New()
	noGap := uuid.New()

	units := &fakeUnitReader{
		units: []*models.SchedulingUnit{
			{ID: withGap},
			{ID: noGap},
		},
	}
	intervals := &fakeIntervalReader{
		gaps: map[uuid.UUID][]models.UndispatchedRange{
			withGap: {{Start: 0, End: 100}},
			// noGap returns empty
		},
	}

	out, err := SelectSchedulableUnits(context.Background(), units, intervals)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 unit with a gap, got %d", len(out))
	}
	if out[0].ID != withGap {
		t.Fatalf("expected unit %s, got %s", withGap, out[0].ID)
	}
}

func TestSelector_PropagatesUnitReadError(t *testing.T) {
	want := errors.New("db down")
	_, err := SelectSchedulableUnits(context.Background(),
		&fakeUnitReader{err: want},
		&fakeIntervalReader{})
	if !errors.Is(err, want) {
		t.Fatalf("expected wrapped %v, got %v", want, err)
	}
}

func TestSelector_PropagatesIntervalReadError(t *testing.T) {
	want := errors.New("constraint error")
	units := &fakeUnitReader{
		units: []*models.SchedulingUnit{{ID: uuid.New()}},
	}
	intervals := &fakeIntervalReader{err: want}

	_, err := SelectSchedulableUnits(context.Background(), units, intervals)
	if !errors.Is(err, want) {
		t.Fatalf("expected wrapped %v, got %v", want, err)
	}
}

func TestSelector_PreservesOrder(t *testing.T) {
	// Order of input is the order from GetSchedulable, which is already
	// priority DESC + created_at ASC. Selector must not reorder.
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	units := &fakeUnitReader{
		units: []*models.SchedulingUnit{
			{ID: ids[0]},
			{ID: ids[1]},
			{ID: ids[2]},
		},
	}
	gaps := map[uuid.UUID][]models.UndispatchedRange{}
	for _, id := range ids {
		gaps[id] = []models.UndispatchedRange{{Start: 0, End: 100}}
	}
	intervals := &fakeIntervalReader{gaps: gaps}

	out, err := SelectSchedulableUnits(context.Background(), units, intervals)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("expected 3 units, got %d", len(out))
	}
	for i, id := range ids {
		if out[i].ID != id {
			t.Fatalf("position %d: expected %s, got %s", i, id, out[i].ID)
		}
	}
}
