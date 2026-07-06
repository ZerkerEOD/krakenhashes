package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// Runner drives the scheduler's 3-second cycle.
type Runner struct {
	cycle    *Cycle
	interval time.Duration

	mu      sync.Mutex
	cancel  context.CancelFunc
	running bool
}

// NewRunner returns a Runner that ticks every interval (default 3s when
// interval <= 0).
func NewRunner(cycle *Cycle, interval time.Duration) *Runner {
	if interval <= 0 {
		interval = 3 * time.Second
	}
	return &Runner{cycle: cycle, interval: interval}
}

// Start begins the ticker in a background goroutine. Returns
// immediately. Calling Start twice is a no-op.
func (r *Runner) Start(parent context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return
	}
	r.running = true

	ctx, cancel := context.WithCancel(parent)
	r.cancel = cancel

	go r.loop(ctx)
	debug.Info("scheduler-v2 runner started (interval=%s)", r.interval)
}

// Stop cancels the runner's context. Idempotent.
func (r *Runner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return
	}
	if r.cancel != nil {
		r.cancel()
	}
	r.running = false
	debug.Info("scheduler-v2 runner stopped")
}

// loop runs the ticker until ctx is cancelled. Per-cycle errors are
// logged and otherwise swallowed so one bad cycle can't kill the loop.
func (r *Runner) loop(ctx context.Context) {
	// Run one cycle immediately on startup so a fresh deploy doesn't
	// wait `interval` to assign existing pending units.
	r.runCycle(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runCycle(ctx)
		}
	}
}

// runCycle invokes Cycle.RunOnce with a bounded timeout so a hung
// cycle can't pile up. 30s is generous for normal allocation work and
// well under the next tick's likely overlap.
func (r *Runner) runCycle(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	res, err := r.cycle.RunOnce(ctx)
	if err != nil {
		debug.Warning("scheduler-v2 cycle: %v", err)
		return
	}
	if res.Allocations == 0 && res.Benchmarked == 0 && res.Dispatched == 0 && len(res.Errors) == 0 {
		// Quiet path — nothing actionable happened (no work, or no idle
		// agents). Avoids an INFO line every 3s while units sit pending with
		// no agents available.
		return
	}
	debug.Info("scheduler-v2 cycle: units=%d idle_agents=%d allocations=%d benchmarked=%d dispatched=%d errors=%d",
		res.UnitsSchedulable, res.IdleAgents, res.Allocations, res.Benchmarked, res.Dispatched, len(res.Errors))
	for _, e := range res.Errors {
		debug.Warning("scheduler-v2 cycle error: %v", e)
	}
}
