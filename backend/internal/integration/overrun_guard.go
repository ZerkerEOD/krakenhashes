package integration

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

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
// re-dispatched remainder is sized correctly. Recovery (truncate/re-gap) is
// handled by the existing stop + heartbeat-sweeper path — this guard only stops
// and self-heals. Errors are logged and never propagate.
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
		}
	}
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
