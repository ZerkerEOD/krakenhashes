package integration

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

const (
	overrunGuardInterval    = 30 * time.Second
	defaultOverrunTolerance = 20 // percent
	// overrunResignalCooldown bounds how often the guard re-stops the same task.
	// After a stop, the agent needs time to wind hashcat down and go silent,
	// after which the heartbeat sweeper recovers the task; this is comfortably
	// longer than the default heartbeat timeout so we don't double-signal during
	// a normal wind-down, while still re-stopping a task whose agent ignores it.
	overrunResignalCooldown = 5 * time.Minute
)

// runOverrunGuard ticks the chunk-overrun guard until ctx is cancelled.
func (m *JobIntegrationManager) runOverrunGuard(ctx context.Context) {
	debug.Info("chunk-overrun guard starting (interval=%s)", overrunGuardInterval)
	ticker := time.NewTicker(overrunGuardInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			debug.Info("chunk-overrun guard stopping: %v", ctx.Err())
			return
		case <-ticker.C:
			m.capOverrunTasks(ctx)
		}
	}
}

// capOverrunTasks finds running tasks past chunk_duration × (1 + tolerance),
// sends each agent a stop, and records the agent's measured speed so the
// re-dispatched remainder is sized correctly. This guard only stops and
// self-heals; the truncate/re-gap recovery runs when the agent responds to the
// stop — its final job_progress{status:"stopped"} and its task_stop_ack both
// route to RecoverTaskByID, whichever lands first (GH #77). The heartbeat
// sweeper is the backstop for an agent that answers neither. Errors are logged
// and never propagate.
func (m *JobIntegrationManager) capOverrunTasks(ctx context.Context) {
	if m.jobTaskRepo == nil || m.wsIntegration == nil {
		return
	}
	if !m.overrunGuardEnabled(ctx) {
		return
	}
	factor := 1.0 + float64(m.overrunTolerancePercent(ctx))/100.0

	tasks, err := m.jobTaskRepo.ListOverrunRunningTasks(ctx, factor)
	if err != nil {
		debug.Warning("overrun guard: list tasks: %v", err)
		return
	}
	if len(tasks) == 0 {
		return
	}

	m.pruneOverrunSignaled()
	for _, t := range tasks {
		if m.recentlySignaledOverrun(t.TaskID) {
			continue
		}
		reason := fmt.Sprintf("chunk time limit exceeded: %ds elapsed > %ds target (×%.2f tolerance)",
			t.ElapsedSeconds, t.ChunkDuration, factor)
		if err := m.wsIntegration.SendJobStop(ctx, t.TaskID, reason); err != nil {
			debug.Warning("overrun guard: stop task %s (agent %d): %v", t.TaskID, t.AgentID, err)
			continue
		}
		m.markOverrunSignaled(t.TaskID)
		debug.Info("overrun guard: stopped task %s on agent %d (%ds > %ds ×%.2f)",
			t.TaskID, t.AgentID, t.ElapsedSeconds, t.ChunkDuration, factor)

		// Self-heal: feed the measured speed back so the re-dispatched remainder
		// is sized to the agent's real (slower) rate and does not overrun again.
		if task, gerr := m.jobTaskRepo.GetByID(ctx, t.TaskID); gerr == nil && task != nil {
			if serr := m.jobExecutionService.RecordRunningTaskObservedSpeed(ctx, task); serr != nil {
				debug.Warning("overrun guard: record speed for task %s: %v", t.TaskID, serr)
			}
			// GH #77: a chunk that blew its whole time budget without any
			// evidence of work — no restore_point advance past its own range
			// start, no effective keyspace processed, nothing cracked (see
			// OverrunTask.MadeProgress) — did nothing at all: the agent is
			// wedged (hashcat stuck in autotune, a driver hang, a download
			// that never finishes). The speed self-heal above cannot
			// help: it early-returns on a zero observation, so the agent keeps
			// its optimistic benchmark and the next cycle hands it the very same
			// re-opened gap, forever.
			//
			// Charge the failure to the agent instead, through the same
			// per-(agent, attack_mode, hash_type) policy engine task failures
			// use, so its cooldown/blocklist machinery routes the gap elsewhere.
			//
			// Zero progress is only meaningful AT the overrun threshold, never
			// earlier: with hashcat's --slow-candidates (-S) a perfectly healthy
			// agent legitimately reports restore_point == range_start and 0 H/s
			// for the first ~12 minutes of every chunk while the host-side
			// candidate generator spins up. A chunk that is past
			// chunk_duration x tolerance is by definition past any legitimate
			// startup stall, because chunk_duration was sized from that agent's
			// own measured speed.
			if !t.MadeProgress {
				m.attributeStalledTask(ctx, task, reason)
			}
		}
	}
}

// attributeStalledTask charges a zero-progress overrun to the agent via
// AttributeBenchmarkFailure — the same generic agent/job/mode/hash_type policy
// engine the task-failure path in HandleJobProgress uses (it is misnamed; its
// body is not benchmark-specific). Attack mode and hash type are not carried on
// the task row, so they come from the parent job and its hashlist.
// Best-effort: every step logs and returns.
func (m *JobIntegrationManager) attributeStalledTask(ctx context.Context, task *models.JobTask, reason string) {
	if m.jobSchedulingService == nil || m.jobExecutionService == nil || m.wsIntegration == nil || task.AgentID == nil {
		return
	}
	jobExec, jeErr := m.jobExecutionService.GetJobExecutionByID(ctx, task.JobExecutionID)
	if jeErr != nil || jobExec == nil {
		debug.Warning("overrun guard: load job %s for stalled-task attribution: %v", task.JobExecutionID, jeErr)
		return
	}
	hashlist, hlErr := m.wsIntegration.hashlistRepo.GetByID(ctx, jobExec.HashlistID)
	if hlErr != nil || hashlist == nil {
		debug.Warning("overrun guard: load hashlist %d for stalled-task attribution: %v", jobExec.HashlistID, hlErr)
		return
	}
	entityID := task.JobExecutionID.String()
	if task.IncrementLayerID != nil {
		entityID = task.IncrementLayerID.String()
	}
	msg := "no keyspace progress before chunk time limit: " + reason
	if attrErr := m.jobSchedulingService.AttributeBenchmarkFailure(
		ctx, *task.AgentID, jobExec.AttackMode, hashlist.HashTypeID, entityID, msg,
	); attrErr != nil {
		debug.Warning("overrun guard: stalled-task attribution for task %s (agent=%d, job=%s): %v",
			task.ID, *task.AgentID, task.JobExecutionID, attrErr)
		return
	}
	debug.Info("overrun guard: charged agent %d a failure for zero-progress task %s (job %s)",
		*task.AgentID, task.ID, task.JobExecutionID)
}

// --- debounce bookkeeping ---

func (m *JobIntegrationManager) recentlySignaledOverrun(taskID uuid.UUID) bool {
	m.overrunMu.Lock()
	defer m.overrunMu.Unlock()
	at, ok := m.overrunSignaled[taskID]
	return ok && time.Since(at) < overrunResignalCooldown
}

func (m *JobIntegrationManager) markOverrunSignaled(taskID uuid.UUID) {
	m.overrunMu.Lock()
	defer m.overrunMu.Unlock()
	m.overrunSignaled[taskID] = time.Now()
}

func (m *JobIntegrationManager) pruneOverrunSignaled() {
	m.overrunMu.Lock()
	defer m.overrunMu.Unlock()
	for id, at := range m.overrunSignaled {
		if time.Since(at) >= overrunResignalCooldown {
			delete(m.overrunSignaled, id)
		}
	}
}

// --- settings ---

// overrunGuardEnabled reports whether the guard is active (default true).
func (m *JobIntegrationManager) overrunGuardEnabled(ctx context.Context) bool {
	if m.systemSettingsRepo == nil {
		return true
	}
	setting, err := m.systemSettingsRepo.GetSetting(ctx, "chunk_overrun_guard_enabled")
	if err != nil || setting == nil || setting.Value == nil {
		return true
	}
	v := strings.TrimSpace(strings.ToLower(*setting.Value))
	return v != "false" && v != "0"
}

// overrunTolerancePercent returns the grace-window percent (default 20).
func (m *JobIntegrationManager) overrunTolerancePercent(ctx context.Context) int {
	if m.systemSettingsRepo == nil {
		return defaultOverrunTolerance
	}
	setting, err := m.systemSettingsRepo.GetSetting(ctx, "chunk_overrun_tolerance_percent")
	if err != nil || setting == nil || setting.Value == nil {
		return defaultOverrunTolerance
	}
	n, err := strconv.Atoi(strings.TrimSpace(*setting.Value))
	if err != nil || n < 0 {
		return defaultOverrunTolerance
	}
	return n
}
