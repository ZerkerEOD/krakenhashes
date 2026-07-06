package scheduler

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	wsservice "github.com/ZerkerEOD/krakenhashes/backend/internal/services/websocket"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// PreemptionCandidate describes a victim task the preemption algorithm
// has chosen to stop so a higher-priority unit can take its agent.
type PreemptionCandidate struct {
	TaskID  uuid.UUID
	AgentID int
	UnitID  uuid.UUID // the victim unit (will get a gap once the agent stops)
	Reason  string
}

// runningTaskSnapshot is the joined task + unit row preemption walks
// over. Package-private; FindAndPreempt and pickPreemptionVictim share
// it.
type runningTaskSnapshot struct {
	TaskID       uuid.UUID
	UnitID       uuid.UUID
	AgentID      int
	UnitPriority int
	CreatedAt    int64 // nanos for newest-first comparison
	JobExecID    uuid.UUID
}

// FindAndPreempt looks for high-priority schedulable units that got
// zero allocations this cycle and tries to free agents for them by
// stopping the newest task at the lowest priority tier whose agent
// is compatible with the starving unit.
//
// The algorithm (plan §9.5):
//  1. For each starving high-priority unit (sorted priority DESC),
//     look at currently-running scheduler-v2 tasks.
//  2. Filter to tasks whose unit's priority is strictly lower than
//     the starving unit's priority.
//  3. Filter to tasks whose agent is compatible with the starving
//     unit.
//  4. Pick the NEWEST such task (highest created_at) at the LOWEST
//     priority — minimizes wasted invested progress.
//  5. Send job_stop with reason="preempted" to the agent.
//  6. The agent's existing stop handler triggers SIGTERM to
//     hashcat. Hashcat exits and the agent sends a final
//     job_progress, which the existing graceful-shutdown handler
//     routes to RecoverTaskByID. RecoverTaskByID truncates the
//     interval (preserving progress as a gap) and marks the task
//     completed. The freed agent shows up idle in the next cycle.
//
// Returns the list of preemptions issued. Per-task errors accumulate
// in errs so a single bad agent doesn't abort the loop.
func FindAndPreempt(
	ctx context.Context,
	database *db.DB,
	wsSender WSSender,
	starvingUnits []UnitInfo,
	compatFn CompatibilityFn,
) (preempted []PreemptionCandidate, errs []error) {

	if len(starvingUnits) == 0 {
		return nil, nil
	}

	running, err := loadRunningTaskSnapshots(ctx, database)
	if err != nil {
		errs = append(errs, fmt.Errorf("preemption: load running tasks: %w", err))
		return nil, errs
	}
	if len(running) == 0 {
		return nil, errs
	}

	// Track agents already chosen as victims this cycle so we don't
	// double-preempt the same agent for two starving units.
	chosenAgents := map[int]bool{}

	for _, starver := range starvingUnits {
		victim, ok := pickPreemptionVictim(starver, running, chosenAgents, compatFn)
		if !ok {
			continue
		}
		chosenAgents[victim.AgentID] = true

		stopPayload := wsservice.JobStopPayload{
			TaskID:         victim.TaskID.String(),
			JobExecutionID: victim.JobExecID.String(),
			Reason:         "preempted by higher priority",
			StopID:         uuid.New().String(),
		}
		body, mErr := json.Marshal(stopPayload)
		if mErr != nil {
			errs = append(errs, fmt.Errorf("preemption: marshal stop payload for task %s: %w", victim.TaskID, mErr))
			continue
		}
		msg := &wsservice.Message{
			Type:    wsservice.TypeJobStop,
			Payload: body,
		}
		if sErr := wsSender.SendMessage(victim.AgentID, msg); sErr != nil {
			errs = append(errs, fmt.Errorf("preemption: send stop to agent %d for task %s: %w", victim.AgentID, victim.TaskID, sErr))
			continue
		}

		preempted = append(preempted, PreemptionCandidate{
			TaskID:  victim.TaskID,
			AgentID: victim.AgentID,
			UnitID:  victim.UnitID,
			Reason:  "preempted by higher priority",
		})
		debug.Info("scheduler-v2 preemption: stopped task %s on agent %d (priority %d) to free for starver %s (priority %d)",
			victim.TaskID, victim.AgentID, victim.UnitPriority, starver.ID, starver.Priority)
	}
	return preempted, errs
}

// loadRunningTaskSnapshots queries every (task, unit) pair currently
// running or assigned on the scheduler-v2 side. EXTRACT(EPOCH ...) *
// 1e9 converts the timestamp to nanoseconds so we can compare with
// the UnitInfo.CreatedAtNanos format used elsewhere in the package.
func loadRunningTaskSnapshots(ctx context.Context, database *db.DB) ([]runningTaskSnapshot, error) {
	// Priority comes from job_executions live (migration 000153 dropped
	// the denormalized scheduling_units.priority column). The double
	// JOIN here is unavoidable: tasks → scheduling_units gives the
	// parent_job_id, which then resolves to the job's current priority.
	rows, err := database.QueryContext(ctx, `
		SELECT t.id, t.scheduling_unit_id, t.agent_id, je.priority,
		       EXTRACT(EPOCH FROM t.created_at) * 1000000000, u.parent_job_id
		FROM job_tasks t
		JOIN scheduling_units u ON u.id = t.scheduling_unit_id
		JOIN job_executions je ON je.id = u.parent_job_id
		WHERE t.status IN ('assigned', 'running')
		  AND t.scheduling_unit_id IS NOT NULL
		  AND t.agent_id IS NOT NULL
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []runningTaskSnapshot
	for rows.Next() {
		var r runningTaskSnapshot
		var createdAtF float64
		if err := rows.Scan(&r.TaskID, &r.UnitID, &r.AgentID, &r.UnitPriority, &createdAtF, &r.JobExecID); err != nil {
			return nil, fmt.Errorf("scan running task: %w", err)
		}
		r.CreatedAt = int64(createdAtF)
		out = append(out, r)
	}
	return out, rows.Err()
}

// pickPreemptionVictim implements the "lowest priority tier, then
// newest within that tier, with a compatible agent" rule. Returns the
// chosen snapshot plus ok=true, or (zero, false) if nothing matches.
func pickPreemptionVictim(
	starver UnitInfo,
	running []runningTaskSnapshot,
	chosenAgents map[int]bool,
	compatFn CompatibilityFn,
) (runningTaskSnapshot, bool) {
	var best runningTaskSnapshot
	bestSet := false

	for _, r := range running {
		if r.UnitPriority >= starver.Priority {
			continue
		}
		if chosenAgents[r.AgentID] {
			continue
		}
		if !compatFn(starver.ID, r.AgentID) {
			continue
		}
		// Prefer lower priority first; within the same priority,
		// prefer newer creation time (least invested progress).
		if !bestSet ||
			r.UnitPriority < best.UnitPriority ||
			(r.UnitPriority == best.UnitPriority && r.CreatedAt > best.CreatedAt) {
			best = r
			bestSet = true
		}
	}
	return best, bestSet
}
