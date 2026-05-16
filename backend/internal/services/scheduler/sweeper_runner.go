package scheduler

import (
	"context"
	"strconv"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// SweeperRunner drives the heartbeat / disconnect-grace eviction loop
// every `interval` (default 10s). Reads task_heartbeat_timeout_seconds
// from system_settings each cycle so admins can tune it without a
// restart.
//
// The runner stops cleanly when its context is cancelled; the caller's
// goroutine should pass a cancellable context derived from the server
// lifecycle.
type SweeperRunner struct {
	db                 *db.DB
	systemSettingsRepo *repository.SystemSettingsRepository
	interval           time.Duration
}

// NewSweeperRunner creates a SweeperRunner with the default 10s cadence.
// Pass interval = 0 to use the default; a non-zero value overrides
// (useful for tests).
func NewSweeperRunner(database *db.DB, systemSettingsRepo *repository.SystemSettingsRepository, interval time.Duration) *SweeperRunner {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	return &SweeperRunner{
		db:                 database,
		systemSettingsRepo: systemSettingsRepo,
		interval:           interval,
	}
}

// Run blocks until ctx is cancelled, ticking once per interval. Each
// tick reads the timeout setting and calls EvictTimedOutTasks. Errors
// from any individual eviction are logged and otherwise ignored so a
// single bad row can't kill the loop.
func (r *SweeperRunner) Run(ctx context.Context) {
	debug.Info("scheduler-v2 sweeper runner starting (interval=%s)", r.interval)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			debug.Info("scheduler-v2 sweeper runner stopping: %v", ctx.Err())
			return
		case <-ticker.C:
			r.sweepOnce(ctx)
		}
	}
}

// sweepOnce performs a single eviction pass. Separated for testability.
func (r *SweeperRunner) sweepOnce(ctx context.Context) {
	timeout := r.readHeartbeatTimeoutSeconds(ctx)

	evicted, errs := EvictTimedOutTasks(ctx, r.db, timeout)
	for _, e := range errs {
		debug.Warning("sweeper: %v", e)
	}
	if len(evicted) > 0 {
		debug.Info("sweeper: evicted %d stale tasks", len(evicted))
		for _, ev := range evicted {
			debug.Debug("sweeper: task %s unit %s range [%d,%d) reason=%s",
				ev.TaskID, ev.UnitID, ev.RangeStart, ev.RangeEnd, ev.Reason)
		}
	}
}

// readHeartbeatTimeoutSeconds returns the configured timeout, or the
// 120s default if the setting can't be read or is invalid.
func (r *SweeperRunner) readHeartbeatTimeoutSeconds(ctx context.Context) int {
	const def = 120
	if r.systemSettingsRepo == nil {
		return def
	}
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	setting, err := r.systemSettingsRepo.GetSetting(readCtx, "task_heartbeat_timeout_seconds")
	if err != nil || setting == nil || setting.Value == nil {
		return def
	}
	n, err := strconv.Atoi(*setting.Value)
	if err != nil || n <= 0 {
		return def
	}
	return n
}
