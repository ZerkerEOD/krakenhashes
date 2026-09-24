package scheduler

import (
	"context"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// Setting reads use cycle.go's readIntSetting / readBoolSetting. readBoolSetting
// there treats only the exact string "true" as truthy, which is the stricter
// and safer test — a typo'd value reads as "off" rather than silently enabling
// tapering. The migration seeds scheduler_endgame_tapering_enabled as 'true'.

/*
 * computeEndgameShares decides, per unit, whether to switch from time-boxed
 * chunks to speed-proportional ones.
 *
 * Computed here rather than inside dispatchOne because tapering needs the SET
 * of agents working a unit, while dispatchOne only ever sees a single
 * allocation inside its own transaction. Restructuring that transaction
 * boundary would be a much larger change for no benefit.
 *
 * Returns nil when tapering is disabled or no unit qualifies, in which case
 * sizing is byte-for-byte what it was before this feature existed.
 */
func (c *Cycle) computeEndgameShares(
	ctx context.Context,
	allocations []Allocation,
	unitsByID map[uuid.UUID]*models.SchedulingUnit,
	agentSpeeds map[int]int64,
	chunkDurationSec int,
	minChunkSec int,
) map[uuid.UUID]map[int]int64 {
	if !c.readBoolSetting(ctx, "scheduler_endgame_tapering_enabled", true) {
		return nil
	}
	threshold := c.readIntSetting(ctx, "endgame_threshold_multiple", 2)
	if threshold <= 0 {
		return nil
	}

	// Group this cycle's allocations by unit.
	allocByUnit := make(map[uuid.UUID][]int)
	for _, a := range allocations {
		allocByUnit[a.UnitID] = append(allocByUnit[a.UnitID], a.AgentID)
	}
	if len(allocByUnit) == 0 {
		return nil
	}

	out := make(map[uuid.UUID]map[int]int64)

	for unitID, agentIDs := range allocByUnit {
		unit, ok := unitsByID[unitID]
		if !ok || unit.BaseKeyspace == nil || *unit.BaseKeyspace <= 0 {
			continue
		}

		remaining, err := c.undispatchedBase(ctx, unitID, *unit.BaseKeyspace)
		if err != nil {
			debug.Warning("scheduler-v2: endgame: could not read coverage for unit %s: %v", unitID, err)
			continue
		}
		if remaining <= 0 {
			continue
		}

		participants := make([]EndgameParticipant, 0, len(agentIDs))
		var aggregate int64
		for _, id := range agentIDs {
			speed := agentSpeeds[id]
			if speed <= 0 {
				speed = ConservativeAgentSpeed
			}
			participants = append(participants, EndgameParticipant{AgentID: id, Speed: speed, Allocating: true})
			aggregate += speed
		}

		// Agents already RUNNING a chunk on this unit count toward the speed
		// denominator but receive nothing now. Omitting them would
		// over-allocate to whoever happens to be idle, handing them work a
		// busy faster agent will reach first.
		busy, err := c.busyAgentSpeedsForUnit(ctx, unitID)
		if err != nil {
			debug.Warning("scheduler-v2: endgame: could not read busy agents for unit %s: %v", unitID, err)
		}
		for agentID, speed := range busy {
			if speed <= 0 {
				speed = ConservativeAgentSpeed
			}
			participants = append(participants, EndgameParticipant{AgentID: agentID, Speed: speed, Allocating: false})
			aggregate += speed
		}

		if !IsEndgame(remaining, chunkDurationSec, threshold, *unit.BaseKeyspace, unit.EffectiveKeyspace.Big(), aggregate) {
			continue
		}

		minChunkBase := sizeChunk(remaining, *unit.BaseKeyspace, unit.EffectiveKeyspace.Big(),
			aggregate, minChunkSec, 1)
		shares := ComputeEndgameShares(EndgameInput{
			UnitID:        unitID,
			RemainingBase: remaining,
			Participants:  participants,
		}, minChunkBase)
		if len(shares) > 0 {
			debug.Info("scheduler-v2: unit %s entered endgame; tapering %d chunk(s) across %d participant(s)",
				unitID, len(shares), len(participants))
			out[unitID] = shares
		}
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

// undispatchedBase returns base keyspace on a unit that no non-failed interval
// covers. Mirrors the coverage accounting the completion gate uses: intervals
// with status 'failed' have re-opened their range and are not coverage.
func (c *Cycle) undispatchedBase(ctx context.Context, unitID uuid.UUID, baseKeyspace int64) (int64, error) {
	var covered int64
	err := c.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(range_end - range_start), 0)
		FROM job_keyspace_intervals
		WHERE scheduling_unit_id = $1 AND status <> 'failed'`, unitID).Scan(&covered)
	if err != nil {
		return 0, err
	}
	remaining := baseKeyspace - covered
	if remaining < 0 {
		return 0, nil
	}
	return remaining, nil
}

// busyAgentSpeedsForUnit returns agent_id -> benchmark speed for agents with
// an in-flight task on this unit.
func (c *Cycle) busyAgentSpeedsForUnit(ctx context.Context, unitID uuid.UUID) (map[int]int64, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT DISTINCT t.agent_id, COALESCE(ab.speed, 0)
		FROM job_tasks t
		JOIN scheduling_units su ON su.id = t.scheduling_unit_id
		JOIN job_executions je ON je.id = su.parent_job_id
		JOIN hashlists h ON h.id = je.hashlist_id
		LEFT JOIN agent_benchmarks ab
		       ON ab.agent_id = t.agent_id
		      AND ab.attack_mode = su.attack_mode
		      AND ab.hash_type = h.hash_type_id
		WHERE t.scheduling_unit_id = $1
		  AND t.status IN ('assigned','running')
		  AND t.agent_id IS NOT NULL`, unitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int]int64)
	for rows.Next() {
		var agentID int
		var speed int64
		if err := rows.Scan(&agentID, &speed); err != nil {
			return nil, err
		}
		out[agentID] = speed
	}
	return out, rows.Err()
}
