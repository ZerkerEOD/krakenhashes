package services

import (
	"context"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// AgentUpdateSweeper drives the periodic auto-update maintenance loop. Each tick
// runs two passes against the AgentUpdateService:
//
//   - promote: any agent flagged update_pending that has become idle gets its
//     update started (respecting the concurrency cap). This is the source of
//     truth for "update a busy agent the moment it goes idle", since a v2 task
//     completing doesn't emit a status flip to hook.
//   - timeout: any agent that has been 'updating' longer than the configured
//     health timeout is declared failed (it should have reconnected on the new
//     version well within the window).
//
// The loop stops cleanly when ctx is cancelled.
type AgentUpdateSweeper struct {
	updateService *AgentUpdateService
	interval      time.Duration
}

// NewAgentUpdateSweeper creates a sweeper with the given cadence (default 15s).
func NewAgentUpdateSweeper(updateService *AgentUpdateService, interval time.Duration) *AgentUpdateSweeper {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	return &AgentUpdateSweeper{
		updateService: updateService,
		interval:      interval,
	}
}

// Run blocks until ctx is cancelled, ticking once per interval.
func (s *AgentUpdateSweeper) Run(ctx context.Context) {
	if s.updateService == nil {
		debug.Warning("agent update sweeper: no update service; not starting")
		return
	}
	debug.Info("agent auto-update sweeper starting (interval=%s)", s.interval)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			debug.Info("agent auto-update sweeper stopping: %v", ctx.Err())
			return
		case <-ticker.C:
			s.sweepOnce(ctx)
		}
	}
}

// sweepOnce performs a single promote + timeout pass. Separated for testability.
func (s *AgentUpdateSweeper) sweepOnce(ctx context.Context) {
	s.updateService.SweepTimedOut(ctx)
	s.updateService.PromotePending(ctx)
}
