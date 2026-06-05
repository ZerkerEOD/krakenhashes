package scheduler

import (
	"context"
	"sync"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/binary/version"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// CompatCache holds (agent_id, unit_id) compatibility decisions so the
// cycle doesn't have to recompute compatibility for every running pair
// on every 3-second tick. The replacement for B.3's per-cycle rebuild.
//
// Cache shape: map[agent_id]map[unit_id]bool. Lookups are O(1) under a
// read lock. Invalidations happen on three events:
//
//   - Agent connect / disconnect / binary-version change → re-evaluate
//     all of that agent's rows by querying its binary_version against
//     every active unit's binary_version.
//   - Unit added / removed → re-evaluate that unit's column.
//   - Process startup → cold prime via WarmAll.
//
// Empty agent or empty unit row means "we haven't evaluated yet" — the
// cycle should call EvaluateAgent or EvaluateUnit and retry. We don't
// silently treat a cache miss as "incompatible."
type CompatCache struct {
	db *db.DB

	mu       sync.RWMutex
	byAgent  map[int]map[uuid.UUID]bool
	knownUID map[uuid.UUID]string // unit_id -> parent_job binary_version
	knownAID map[int]string       // agent_id -> binary_version
}

// NewCompatCache returns an empty cache. Call WarmAll once at startup
// to populate from current DB state.
func NewCompatCache(database *db.DB) *CompatCache {
	return &CompatCache{
		db:       database,
		byAgent:  map[int]map[uuid.UUID]bool{},
		knownUID: map[uuid.UUID]string{},
		knownAID: map[int]string{},
	}
}

// IsCompatible returns the cached decision for (agentID, unitID). If
// the pair isn't cached, returns (false, false) — the caller should
// trigger an Evaluate* call. The cycle's compatFn closure normally
// wraps IsCompatible with an automatic Evaluate fallback.
func (c *CompatCache) IsCompatible(agentID int, unitID uuid.UUID) (compatible, known bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if row, ok := c.byAgent[agentID]; ok {
		v, ok2 := row[unitID]
		return v, ok2
	}
	return false, false
}

// CompatFn returns a CompatibilityFn closure that hits the cache
// first and falls back to an on-demand EvaluatePair for misses. The
// cycle uses this directly as its compatFn argument to the allocator.
func (c *CompatCache) CompatFn(ctx context.Context) CompatibilityFn {
	return func(unitID uuid.UUID, agentID int) bool {
		if v, ok := c.IsCompatible(agentID, unitID); ok {
			return v
		}
		v, err := c.EvaluatePair(ctx, agentID, unitID)
		if err != nil {
			debug.Warning("compat: evaluate (agent=%d unit=%s) failed: %v", agentID, unitID, err)
			// Pessimistic on error so we never accidentally
			// dispatch to an incompatible agent.
			return false
		}
		return v
	}
}

// WarmAll rebuilds the cache from scratch. Cheap to call (one query
// each for agents and units). Use at process startup so the first
// cycle has hot data.
func (c *CompatCache) WarmAll(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.byAgent = map[int]map[uuid.UUID]bool{}
	c.knownUID = map[uuid.UUID]string{}
	c.knownAID = map[int]string{}

	// Load agents.
	rows, err := c.db.QueryContext(ctx, `
		SELECT id, COALESCE(binary_version, '')
		FROM agents
		WHERE is_enabled = true
	`)
	if err != nil {
		return err
	}
	type agentRow struct {
		ID  int
		Ver string
	}
	var agents []agentRow
	for rows.Next() {
		var a agentRow
		if err := rows.Scan(&a.ID, &a.Ver); err != nil {
			rows.Close()
			return err
		}
		agents = append(agents, a)
	}
	rows.Close()

	// Load units. Schedulable status filter matches selector.go.
	rows, err = c.db.QueryContext(ctx, `
		SELECT u.id, COALESCE(je.binary_version, '')
		FROM scheduling_units u
		JOIN job_executions je ON je.id = u.parent_job_id
		WHERE u.status IN ('pending', 'running')
	`)
	if err != nil {
		return err
	}
	type unitRow struct {
		ID  uuid.UUID
		Ver string
	}
	var units []unitRow
	for rows.Next() {
		var u unitRow
		if err := rows.Scan(&u.ID, &u.Ver); err != nil {
			rows.Close()
			return err
		}
		units = append(units, u)
	}
	rows.Close()

	// Populate cross-product cache.
	for _, a := range agents {
		c.knownAID[a.ID] = a.Ver
		row := make(map[uuid.UUID]bool, len(units))
		for _, u := range units {
			c.knownUID[u.ID] = u.Ver
			row[u.ID] = compatibleVersions(a.Ver, u.Ver)
		}
		c.byAgent[a.ID] = row
	}
	debug.Info("compat cache warmed: %d agents × %d units", len(agents), len(units))
	return nil
}

// EvaluatePair computes compatibility for one (agent, unit) pair
// without touching the cross-product. Updates the cache on success.
func (c *CompatCache) EvaluatePair(ctx context.Context, agentID int, unitID uuid.UUID) (bool, error) {
	var agentVer, unitVer string

	// First try cache for known patterns to avoid extra DB hits.
	c.mu.RLock()
	if v, ok := c.knownAID[agentID]; ok {
		agentVer = v
	}
	if v, ok := c.knownUID[unitID]; ok {
		unitVer = v
	}
	c.mu.RUnlock()

	// Anything missing → small lookup.
	if _, hasAgent := c.knownAgentVer(agentID); !hasAgent {
		var v string
		if err := c.db.QueryRowContext(ctx,
			`SELECT COALESCE(binary_version, '') FROM agents WHERE id = $1`, agentID).Scan(&v); err != nil {
			return false, err
		}
		agentVer = v
		c.mu.Lock()
		c.knownAID[agentID] = v
		c.mu.Unlock()
	}
	if _, hasUnit := c.knownUnitVer(unitID); !hasUnit {
		var v string
		if err := c.db.QueryRowContext(ctx, `
			SELECT COALESCE(je.binary_version, '')
			FROM scheduling_units u JOIN job_executions je ON je.id = u.parent_job_id
			WHERE u.id = $1
		`, unitID).Scan(&v); err != nil {
			return false, err
		}
		unitVer = v
		c.mu.Lock()
		c.knownUID[unitID] = v
		c.mu.Unlock()
	}

	result := compatibleVersions(agentVer, unitVer)

	c.mu.Lock()
	row := c.byAgent[agentID]
	if row == nil {
		row = map[uuid.UUID]bool{}
		c.byAgent[agentID] = row
	}
	row[unitID] = result
	c.mu.Unlock()

	return result, nil
}

// OnAgentChanged invalidates and re-evaluates one agent's row. Called
// from the WebSocket connect handler, the disconnect handler, and any
// agent-version-changed code path. Cheap: one query for the agent's
// current version, then walk every known unit.
func (c *CompatCache) OnAgentChanged(ctx context.Context, agentID int) {
	var newVer string
	err := c.db.QueryRowContext(ctx,
		`SELECT COALESCE(binary_version, '') FROM agents WHERE id = $1`, agentID).Scan(&newVer)
	if err != nil {
		// Agent may have been deleted — purge from cache.
		c.mu.Lock()
		delete(c.byAgent, agentID)
		delete(c.knownAID, agentID)
		c.mu.Unlock()
		return
	}

	c.mu.Lock()
	c.knownAID[agentID] = newVer
	row := make(map[uuid.UUID]bool, len(c.knownUID))
	for uid, uver := range c.knownUID {
		row[uid] = compatibleVersions(newVer, uver)
	}
	c.byAgent[agentID] = row
	c.mu.Unlock()
}

// OnUnitChanged invalidates and re-evaluates one unit's column across
// every known agent. Called when a scheduling_unit is created (Phase
// E hook), updated, or completes.
func (c *CompatCache) OnUnitChanged(ctx context.Context, unitID uuid.UUID) {
	var newVer string
	err := c.db.QueryRowContext(ctx, `
		SELECT COALESCE(je.binary_version, '')
		FROM scheduling_units u JOIN job_executions je ON je.id = u.parent_job_id
		WHERE u.id = $1
	`, unitID).Scan(&newVer)
	if err != nil {
		// Unit gone — purge column.
		c.mu.Lock()
		delete(c.knownUID, unitID)
		for _, row := range c.byAgent {
			delete(row, unitID)
		}
		c.mu.Unlock()
		return
	}

	c.mu.Lock()
	c.knownUID[unitID] = newVer
	for aid, ver := range c.knownAID {
		if row, ok := c.byAgent[aid]; ok {
			row[unitID] = compatibleVersions(ver, newVer)
		}
	}
	c.mu.Unlock()
}

func (c *CompatCache) knownAgentVer(agentID int) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.knownAID[agentID]
	return v, ok
}

func (c *CompatCache) knownUnitVer(unitID uuid.UUID) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.knownUID[unitID]
	return v, ok
}

// compatibleVersions wraps version.IsCompatibleStr with the
// empty-string-matches-anything convention used by the legacy
// scheduler. Empty unit version means "any binary"; empty agent
// version means "no specific requirement."
func compatibleVersions(agentVer, unitVer string) bool {
	if unitVer == "" {
		return true
	}
	if agentVer == "" {
		// Agent didn't declare a version — be permissive and let
		// the agent's actual hashcat handle the call. Matches the
		// legacy fallback at job_scheduling_benchmark_planning.go.
		return true
	}
	return version.IsCompatibleStr(agentVer, unitVer)
}
