package scheduler

import (
	"context"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

// SchedulingUnitReader is the subset of the SchedulingUnit repository the
// selector needs. Defined as an interface so tests can swap a fake
// implementation in.
type SchedulingUnitReader interface {
	GetSchedulable(ctx context.Context) ([]*models.SchedulingUnit, error)
}

// IntervalGapReader is the subset of the KeyspaceInterval repository the
// selector needs: given a unit, return whether it has any undispatched
// ranges.
type IntervalGapReader interface {
	UndispatchedRanges(ctx context.Context, unitID uuid.UUID) ([]models.UndispatchedRange, error)
}

// SelectSchedulableUnits returns the units the dispatcher should consider
// for allocation this cycle: status pending or running (parent job also
// non-terminal) AND at least one undispatched range. Accuracy is NOT a filter
// here — inaccurate units are returned so the cycle can bootstrap them with a
// benchmark; the per-allocation classification in cycle.go decides
// benchmark-vs-dispatch. Returned in priority DESC, created_at ASC order (same
// as GetSchedulable).
//
// The gap check is intentionally per-unit rather than a single fancy SQL
// join. The win from one big query is small because Postgres has to do
// the same window-function pass anyway; the per-unit version keeps the
// query simple and makes it easy to short-circuit later (e.g., a Phase B
// optimization that caches the last gap-count per unit and only refreshes
// when intervals change).
func SelectSchedulableUnits(
	ctx context.Context,
	units SchedulingUnitReader,
	intervals IntervalGapReader,
) ([]*models.SchedulingUnit, error) {
	candidates, err := units.GetSchedulable(ctx)
	if err != nil {
		return nil, fmt.Errorf("selector: get schedulable: %w", err)
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	out := make([]*models.SchedulingUnit, 0, len(candidates))
	for _, u := range candidates {
		gaps, err := intervals.UndispatchedRanges(ctx, u.ID)
		if err != nil {
			return nil, fmt.Errorf("selector: gap check for unit %s: %w", u.ID, err)
		}
		if len(gaps) == 0 {
			continue
		}
		out = append(out, u)
	}
	return out, nil
}
