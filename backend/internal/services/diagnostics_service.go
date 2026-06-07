package services

import (
	"context"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// DiagnosticsService buffers scheduling diagnostics in memory and flushes them
// to the database in batches. It is the answer to the "don't spam the DB with
// the same reason every 3 seconds" constraint:
//
//   - Record() coalesces recurrences of the same (scope, scope_id, reason) in
//     memory — it bumps an in-memory delta and never writes per call.
//   - ClearScope() is deduped too: it only enqueues a DB clear the first time a
//     scope transitions to "clear" (or the first time it's seen after startup),
//     not on every cycle a working agent is observed.
//   - Flushes happen on a timer, when the dirty buffer crosses a threshold, on
//     demand before a UI read (ListActiveByScope force-flushes), and on Stop().
//
// Net effect: a steady-state idle agent costs one row that's updated in batches,
// not a write per scheduler cycle.
type DiagnosticsService struct {
	repo *repository.DiagnosticsRepository

	mu      sync.Mutex
	pending map[diagKey]*diagEntry // dirty upserts accumulated since last flush
	clears  map[string]bool        // "scope|scope_id" pending a clear-all
	active  map[string]bool        // scopes with active diagnostics recorded this process
	cleared map[string]bool        // scopes already cleared & not re-recorded (dedup)

	flushThreshold int
	flushInterval  time.Duration
	flushSignal    chan struct{}
	stop           chan struct{}
	done           chan struct{}
}

type diagKey struct {
	scope, scopeID, reason string
}

type diagEntry struct {
	severity     string
	detail       string
	pendingDelta int64
}

// NewDiagnosticsService constructs the service. Call Start to begin periodic
// flushing and Stop on shutdown.
func NewDiagnosticsService(repo *repository.DiagnosticsRepository) *DiagnosticsService {
	return &DiagnosticsService{
		repo:           repo,
		pending:        make(map[diagKey]*diagEntry),
		clears:         make(map[string]bool),
		active:         make(map[string]bool),
		cleared:        make(map[string]bool),
		flushThreshold: 200,
		flushInterval:  30 * time.Second,
		flushSignal:    make(chan struct{}, 1),
		stop:           make(chan struct{}),
		done:           make(chan struct{}),
	}
}

func scopeKey(scope, scopeID string) string { return scope + "|" + scopeID }

// Record notes that `scope/scopeID` is in state `reason` (e.g. agent X is idle
// because no_compatible_job). Repeated calls with the same key coalesce in
// memory until the next flush. severity defaults to info when empty.
func (s *DiagnosticsService) Record(scope, scopeID, reason, severity, detail string) {
	if s == nil {
		return
	}
	if severity == "" {
		severity = models.DiagSeverityInfo
	}
	sk := scopeKey(scope, scopeID)
	k := diagKey{scope: scope, scopeID: scopeID, reason: reason}

	s.mu.Lock()
	s.active[sk] = true
	delete(s.cleared, sk) // active again — a future ClearScope must re-clear
	delete(s.clears, sk)  // cancel any pending clear for this scope
	e := s.pending[k]
	if e == nil {
		e = &diagEntry{}
		s.pending[k] = e
	}
	e.severity = severity
	e.detail = detail
	e.pendingDelta++
	overThreshold := len(s.pending) >= s.flushThreshold
	s.mu.Unlock()

	if overThreshold {
		s.signalFlush()
	}
}

// ClearScope marks all active diagnostics for a scope as resolved (e.g. the
// agent picked up work). Deduped: a no-op if the scope is already known clear
// and hasn't been re-recorded since, so calling it every cycle for a busy agent
// costs nothing after the first clear.
func (s *DiagnosticsService) ClearScope(scope, scopeID string) {
	if s == nil {
		return
	}
	sk := scopeKey(scope, scopeID)

	s.mu.Lock()
	switch {
	case s.active[sk]:
		// Was active this process → enqueue a clear and drop its pending rows.
		s.active[sk] = false
		s.cleared[sk] = true
		s.clears[sk] = true
		for k := range s.pending {
			if scopeKey(k.scope, k.scopeID) == sk {
				delete(s.pending, k)
			}
		}
	case !s.cleared[sk]:
		// First time we've seen this scope since startup; clear once to drop any
		// stale rows persisted before a restart, then remember it's clear.
		s.cleared[sk] = true
		s.clears[sk] = true
	default:
		// Already clear and not re-recorded — no-op (the dedup that prevents spam).
	}
	s.mu.Unlock()
}

// staleWindow bounds how recently a diagnostic must have been refreshed to be
// considered "current". The scheduler re-records active reasons every cycle
// (seconds), so anything not refreshed within this window has resolved and is
// hidden from the UI even if no explicit clear ran (e.g. agent disabled).
const staleWindow = 5 * time.Minute

// ListActiveByScope force-flushes the buffer, then returns the currently-active
// diagnostics for the scope from the DB so the UI sees an up-to-date view.
func (s *DiagnosticsService) ListActiveByScope(ctx context.Context, scope, scopeID string) ([]models.SchedulingDiagnostic, error) {
	if s == nil {
		return nil, nil
	}
	s.Flush(ctx)
	return s.repo.ListActiveByScope(ctx, scope, scopeID, time.Now().Add(-staleWindow))
}

// Flush writes the current buffer (upserts + clears) to the DB in one batch.
// Safe to call concurrently; best-effort (errors are logged, not returned to
// callers of Record/ClearScope).
func (s *DiagnosticsService) Flush(ctx context.Context) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if len(s.pending) == 0 && len(s.clears) == 0 {
		s.mu.Unlock()
		return
	}
	upserts := make([]repository.DiagUpsert, 0, len(s.pending))
	for k, e := range s.pending {
		upserts = append(upserts, repository.DiagUpsert{
			Scope:      k.scope,
			ScopeID:    k.scopeID,
			ReasonCode: k.reason,
			Severity:   e.severity,
			Detail:     e.detail,
			CountDelta: e.pendingDelta,
		})
	}
	clears := make([]repository.DiagClear, 0, len(s.clears))
	for sk := range s.clears {
		scope, scopeID := splitScopeKey(sk)
		clears = append(clears, repository.DiagClear{Scope: scope, ScopeID: scopeID})
	}
	// Reset the buffer; deltas are now owned by the DB write below.
	s.pending = make(map[diagKey]*diagEntry)
	s.clears = make(map[string]bool)
	s.mu.Unlock()

	if err := s.repo.UpsertBatch(ctx, upserts, clears); err != nil {
		debug.Warning("DiagnosticsService flush failed (%d upserts, %d clears): %v", len(upserts), len(clears), err)
	}
}

// Start launches the periodic + signal-driven flush loop.
func (s *DiagnosticsService) Start(ctx context.Context) {
	if s == nil {
		return
	}
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(s.flushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				s.Flush(context.Background())
				return
			case <-s.stop:
				s.Flush(context.Background())
				return
			case <-ticker.C:
				s.Flush(ctx)
			case <-s.flushSignal:
				s.Flush(ctx)
			}
		}
	}()
}

// Stop flushes and shuts down the flush loop.
func (s *DiagnosticsService) Stop() {
	if s == nil {
		return
	}
	select {
	case <-s.stop:
		// already stopped
	default:
		close(s.stop)
	}
	<-s.done
}

func (s *DiagnosticsService) signalFlush() {
	select {
	case s.flushSignal <- struct{}{}:
	default:
	}
}

func splitScopeKey(sk string) (scope, scopeID string) {
	for i := 0; i < len(sk); i++ {
		if sk[i] == '|' {
			return sk[:i], sk[i+1:]
		}
	}
	return sk, ""
}
