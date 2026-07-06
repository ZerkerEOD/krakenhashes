package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// SyncTrigger is the minimum surface the recovery loop needs from the
// WebSocket handler. Defined here (rather than importing the handler package)
// to keep the dependency arrow services ← handlers.
type SyncTrigger interface {
	TriggerFileSync(agentID int) error
}

// AgentSyncRecovery finds agents that are connected, heartbeating, and stuck
// with sync_status='pending' — typically because a forced-benchmark flow reset
// their sync status and the subsequent sync never completed — and re-issues a
// file-sync request. Without this service those agents are permanently locked
// out of scheduling (GetAvailableAgents filters on sync_status='completed')
// because the only existing sync-start path fires on fresh WebSocket connect.
type AgentSyncRecovery struct {
	db           *db.DB
	syncTrigger  SyncTrigger
	tickInterval time.Duration
	// heartbeatWindow: only retry sync for agents that have heartbeated within
	// this window. Otherwise they're effectively offline and the normal
	// reconnect path will handle them.
	heartbeatWindow time.Duration
	// minAgeSinceStart: don't interrupt a sync that is legitimately in
	// progress. A sync that hasn't made meaningful progress in this long is
	// treated as stuck.
	minAgeSinceStart time.Duration

	ticker  *time.Ticker
	stopCh  chan struct{}
	running bool
	mu      sync.Mutex
}

// NewAgentSyncRecovery wires the service. Defaults: 60s tick, 90s heartbeat
// window, 5 min stuck threshold.
func NewAgentSyncRecovery(database *db.DB, trigger SyncTrigger) *AgentSyncRecovery {
	return &AgentSyncRecovery{
		db:               database,
		syncTrigger:      trigger,
		tickInterval:     60 * time.Second,
		heartbeatWindow:  90 * time.Second,
		minAgeSinceStart: 5 * time.Minute,
		stopCh:           make(chan struct{}),
	}
}

// Start launches the recovery loop. Safe to call once.
func (r *AgentSyncRecovery) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return fmt.Errorf("agent sync recovery already running")
	}
	r.running = true
	r.mu.Unlock()

	debug.Info("Starting agent sync recovery loop: tick=%s, heartbeat_window=%s, stuck_threshold=%s",
		r.tickInterval, r.heartbeatWindow, r.minAgeSinceStart)
	r.ticker = time.NewTicker(r.tickInterval)
	go r.loop(ctx)
	return nil
}

// Stop halts the recovery loop.
func (r *AgentSyncRecovery) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return
	}
	close(r.stopCh)
	if r.ticker != nil {
		r.ticker.Stop()
	}
	r.running = false
	debug.Info("Agent sync recovery loop stopped")
}

func (r *AgentSyncRecovery) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stopCh:
			return
		case <-r.ticker.C:
			if err := r.runOnce(ctx); err != nil {
				debug.Warning("Agent sync recovery tick failed: %v", err)
			}
		}
	}
}

// runOnce is exposed as a method so tests can step the loop deterministically.
func (r *AgentSyncRecovery) runOnce(ctx context.Context) error {
	// Heartbeating-but-stuck agents: sync_status=pending, last_heartbeat is
	// fresh, AND sync either never started (sync_started_at IS NULL) or has
	// been "in progress" longer than the stuck threshold.
	query := `
		SELECT id
		FROM agents
		WHERE status = 'active'
		  AND is_enabled = true
		  AND sync_status = 'pending'
		  AND last_heartbeat > NOW() - ($1 * INTERVAL '1 second')
		  AND (sync_started_at IS NULL OR sync_started_at < NOW() - ($2 * INTERVAL '1 second'))`

	rows, err := r.db.QueryContext(ctx, query,
		int(r.heartbeatWindow.Seconds()),
		int(r.minAgeSinceStart.Seconds()),
	)
	if err != nil {
		return fmt.Errorf("query stuck agents: %w", err)
	}
	defer rows.Close()

	var stuck []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			debug.Warning("scan stuck agent row: %v", err)
			continue
		}
		stuck = append(stuck, id)
	}
	if len(stuck) == 0 {
		return nil
	}

	debug.Info("Agent sync recovery: re-issuing file sync for %d stuck agent(s)", len(stuck))
	for _, agentID := range stuck {
		if err := r.syncTrigger.TriggerFileSync(agentID); err != nil {
			// Agent went away between the SELECT and now; benign.
			debug.Log("Sync recovery could not trigger for agent", map[string]interface{}{
				"agent_id": agentID,
				"reason":   err.Error(),
			})
			continue
		}
		debug.Info("Sync recovery re-issued file sync to agent %d", agentID)
	}
	return nil
}
