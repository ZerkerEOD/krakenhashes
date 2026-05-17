package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	wsservice "github.com/ZerkerEOD/krakenhashes/backend/internal/services/websocket"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// BenchmarkGap describes one (agent, unit-combo) tuple that lacks a
// cached speed in agent_benchmarks.
type BenchmarkGap struct {
	AgentID    int
	UnitID     uuid.UUID
	AttackMode int
	HashType   int
	SaltCount  *int // nil for non-salted; the actual count for salted
}

// IdentifyMissingBenchmarks scans the (agent, unit) cross-product and
// returns the subset where agent_benchmarks lacks a cached row for the
// unit's (attack_mode, hash_type, salt_count) combo.
//
// The salt_count match is NULL-safe via IS NOT DISTINCT FROM so
// non-salted lookups (salt_count IS NULL) and salted exact matches
// both work cleanly.
//
// To bound work, the function processes at most one missing-benchmark
// gap per agent per cycle — agents can only run one benchmark at a
// time, so additional gaps for the same agent are deferred to future
// cycles.
func IdentifyMissingBenchmarks(
	ctx context.Context,
	database *db.DB,
	units []*models.SchedulingUnit,
	agentIDs []int,
	compatFn CompatibilityFn,
) ([]BenchmarkGap, error) {
	if len(units) == 0 || len(agentIDs) == 0 {
		return nil, nil
	}

	// Pre-resolve each unit's (hash_type, salt_count) — same query
	// shape as cycle.lookupHashTypeAndSalt.
	type combo struct {
		hashType  int
		saltCount *int
	}
	unitCombos := map[uuid.UUID]combo{}
	for _, u := range units {
		var ht int
		var isSalted bool
		var totalHashes int
		err := database.QueryRowContext(ctx, `
			SELECT h.hash_type_id, ht.is_salted, h.total_hashes
			FROM job_executions je
			JOIN hashlists h ON h.id = je.hashlist_id
			JOIN hash_types ht ON ht.id = h.hash_type_id
			WHERE je.id = $1
		`, u.ParentJobID).Scan(&ht, &isSalted, &totalHashes)
		if err != nil {
			debug.Warning("benchmark: lookup hash_type for unit %s failed: %v", u.ID, err)
			continue
		}
		c := combo{hashType: ht}
		if isSalted && totalHashes > 0 {
			sc := totalHashes
			c.saltCount = &sc
		}
		unitCombos[u.ID] = c
	}

	pickedPerAgent := map[int]bool{}
	var gaps []BenchmarkGap

	for _, agentID := range agentIDs {
		if pickedPerAgent[agentID] {
			continue
		}
		// Try units in priority order (units arg is already sorted
		// per SelectSchedulableUnits's GetSchedulable contract).
		for _, u := range units {
			if pickedPerAgent[agentID] {
				break
			}
			c, ok := unitCombos[u.ID]
			if !ok {
				continue
			}
			if !compatFn(u.ID, agentID) {
				continue
			}
			has, err := agentHasBenchmarkFor(ctx, database, agentID, int(u.AttackMode), c.hashType, c.saltCount)
			if err != nil {
				debug.Warning("benchmark: check (agent=%d combo=%d/%d/%v): %v", agentID, u.AttackMode, c.hashType, c.saltCount, err)
				continue
			}
			if has {
				continue
			}
			// Storm guard: if this (agent, job, combo) has an active
			// blocklist entry (typically because a recent benchmark
			// failed via HandleBenchmarkResult ->
			// AttributeBenchmarkFailure -> AddBlocklistEntry), skip.
			// Without this check, every cycle re-fires the same
			// failing benchmark, hammering the agent. Global entries
			// (job_execution_id IS NULL) also match.
			blocked, err := agentBenchmarkBlocklisted(ctx, database, agentID, u.ParentJobID, int(u.AttackMode), c.hashType)
			if err != nil {
				debug.Warning("benchmark: blocklist check (agent=%d unit=%s): %v", agentID, u.ID, err)
				continue
			}
			if blocked {
				continue
			}
			gaps = append(gaps, BenchmarkGap{
				AgentID:    agentID,
				UnitID:     u.ID,
				AttackMode: int(u.AttackMode),
				HashType:   c.hashType,
				SaltCount:  c.saltCount,
			})
			pickedPerAgent[agentID] = true
		}
	}
	return gaps, nil
}

// agentBenchmarkBlocklisted reports whether agent_benchmark_blocklist
// has an active uncleared entry that prevents (agent, attack_mode,
// hash_type) from being benchmarked. Job-scoped entries (matching
// parent_job_id) and global entries (job_execution_id IS NULL) both
// count. Matches the legacy IsBlocklisted query shape at
// benchmark_repository.go:852.
func agentBenchmarkBlocklisted(ctx context.Context, database *db.DB, agentID int, parentJobID uuid.UUID, attackMode, hashType int) (bool, error) {
	var exists bool
	err := database.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM agent_benchmark_blocklist
			WHERE agent_id = $1
			  AND (job_execution_id = $2 OR job_execution_id IS NULL)
			  AND attack_mode = $3
			  AND hash_type = $4
			  AND cleared_at IS NULL
			  AND expires_at > NOW()
		)
	`, agentID, parentJobID, attackMode, hashType).Scan(&exists)
	return exists, err
}

// agentHasBenchmarkFor reports whether agent_benchmarks already has a
// row for the (agent, attack_mode, hash_type, salt_count) combo.
// NULL-safe equality on salt_count via IS NOT DISTINCT FROM.
func agentHasBenchmarkFor(ctx context.Context, database *db.DB, agentID, attackMode, hashType int, saltCount *int) (bool, error) {
	var sc interface{}
	if saltCount != nil {
		sc = *saltCount
	}
	var exists bool
	err := database.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM agent_benchmarks
			WHERE agent_id = $1 AND attack_mode = $2 AND hash_type = $3
			  AND salt_count IS NOT DISTINCT FROM $4
		)
	`, agentID, attackMode, hashType, sc).Scan(&exists)
	return exists, err
}

// DispatchBenchmarks fires off benchmark_request messages for each
// gap. Fire-and-forget — the agent eventually sends back a
// benchmark_result, which the existing HandleBenchmarkResult handler
// stores into agent_benchmarks. The next cycle's IsCompatible /
// readAgentSpeeds lookup picks it up.
//
// Returns the set of agent IDs that were dispatched a benchmark
// this cycle. The caller (cycle.go) should exclude these from
// AllocateAgentsByPriority — agents can only run one
// benchmark-or-task at a time.
func DispatchBenchmarks(
	ctx context.Context,
	database *db.DB,
	wsSender WSSender,
	binaryResolver BinaryResolver,
	gaps []BenchmarkGap,
	unitsByID map[uuid.UUID]*models.SchedulingUnit,
) (busyAgents map[int]bool, errs []error) {
	busyAgents = map[int]bool{}
	if len(gaps) == 0 {
		return busyAgents, nil
	}

	for _, g := range gaps {
		unit, ok := unitsByID[g.UnitID]
		if !ok {
			continue
		}
		req, buildErr := buildBenchmarkRequest(ctx, database, binaryResolver, unit, g)
		if buildErr != nil {
			errs = append(errs, fmt.Errorf("benchmark: build request for agent %d unit %s: %w", g.AgentID, g.UnitID, buildErr))
			continue
		}
		body, mErr := json.Marshal(req)
		if mErr != nil {
			errs = append(errs, fmt.Errorf("benchmark: marshal request: %w", mErr))
			continue
		}
		msg := &wsservice.Message{
			Type:    wsservice.TypeBenchmarkRequest,
			Payload: body,
		}
		if sErr := wsSender.SendMessage(g.AgentID, msg); sErr != nil {
			errs = append(errs, fmt.Errorf("benchmark: send to agent %d: %w", g.AgentID, sErr))
			continue
		}
		busyAgents[g.AgentID] = true
		debug.Info("scheduler-v2 benchmark dispatched: agent=%d combo=mode%d/type%d/salts=%v requestID=%s",
			g.AgentID, g.AttackMode, g.HashType, g.SaltCount, req.RequestID)
	}
	return busyAgents, errs
}

// buildBenchmarkRequest constructs the BenchmarkRequestPayload for a
// (agent, unit) pair. Mirrors the dispatch enrichment in
// cycle.sendAssignment but produces the benchmark payload instead of
// the task payload. The benchmark runs the SAME inputs as a real
// chunk would (real-world speed test) so the cached speed is
// representative.
func buildBenchmarkRequest(
	ctx context.Context,
	database *db.DB,
	binaryResolver BinaryResolver,
	unit *models.SchedulingUnit,
	g BenchmarkGap,
) (*wsservice.BenchmarkRequestPayload, error) {

	// Reuse BuildTaskAssignment to get the per-attack-mode field
	// layout. KeyspaceStart=0 KeyspaceEnd=1 is a placeholder; the
	// agent's benchmark code reads test_duration / timeout_duration
	// for the actual loop length, not the keyspace range.
	taskPayload, err := BuildTaskAssignment(unit, uuid.New(), 0, 1)
	if err != nil {
		return nil, fmt.Errorf("build task layout: %w", err)
	}

	// Enrich with hashlist + binary.
	var hashlistID int64
	var hashType int
	var originalFilePath string
	err = database.QueryRowContext(ctx, `
		SELECT je.hashlist_id, h.hash_type_id, COALESCE(h.original_file_path, '')
		FROM job_executions je
		JOIN hashlists h ON h.id = je.hashlist_id
		WHERE je.id = $1
	`, unit.ParentJobID).Scan(&hashlistID, &hashType, &originalFilePath)
	if err != nil {
		return nil, fmt.Errorf("lookup parent + hashlist: %w", err)
	}

	binaryPath := ""
	if binaryResolver != nil {
		if binID, berr := binaryResolver.DetermineBinaryForTask(ctx, g.AgentID, unit.ParentJobID); berr == nil {
			binaryPath = fmt.Sprintf("binaries/%d", binID)
		}
	}

	hashlistPath := fmt.Sprintf("hashlists/%d.hash", hashlistID)

	req := &wsservice.BenchmarkRequestPayload{
		RequestID:               uuid.New().String(),
		JobExecutionID:          unit.ParentJobID.String(),
		AttackMode:              g.AttackMode,
		HashType:                g.HashType,
		BinaryPath:              binaryPath,
		HashlistID:              hashlistID,
		HashlistPath:            hashlistPath,
		WordlistPaths:           taskPayload.WordlistPaths,
		RulePaths:               taskPayload.RulePaths,
		Mask:                    taskPayload.Mask,
		CustomCharsets:          taskPayload.CustomCharsets,
		CharsetFiles:            taskPayload.CharsetFiles,
		HexCharset:              taskPayload.HexCharset,
		AssociationWordlistPath: taskPayload.AssociationWordlistPath,
		TestDuration:            10,
		TimeoutDuration:         30,
		MinStatusUpdates:        2,
	}
	if unit.AttackMode == AttackModeAssociation && originalFilePath != "" {
		req.HashlistPath = originalFilePath
	}
	return req, nil
}

// BenchmarkPhaseTimeout is the hard ceiling on dispatch alone (not
// the wait for results). The new scheduler is fire-and-forget; this
// just bounds the cycle's per-iteration work.
const BenchmarkPhaseTimeout = 5 * time.Second

// WithBenchmarkTimeout returns a derived context with the dispatch
// timeout. Exported in case tests want to override.
func WithBenchmarkTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, BenchmarkPhaseTimeout)
}
