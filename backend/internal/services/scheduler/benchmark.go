package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	wsservice "github.com/ZerkerEOD/krakenhashes/backend/internal/services/websocket"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// benchmarkInFlightWindow bounds how long an uncompleted
// benchmark_requests row is considered "still in flight" before the
// dispatcher will retry. Set generously because:
//   - Mock benchmarks under contention have been observed to take ~3
//     minutes to return a result.
//   - Real benchmarks on slow hash types (bcrypt, scrypt, NTLMv2) can
//     take longer than that.
//   - On the other end, an agent that crashed mid-benchmark will leave
//     an orphaned row; the staleness window is the longest we'll wait
//     before retrying without it.
//
// 5 minutes is the chosen middle ground. If real-world benchmarks need
// longer, lift this to a system setting in a follow-up.
const benchmarkInFlightWindow = 5 * time.Minute

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
			// In-flight guard: if benchmark_requests has an uncompleted
			// row for this (agent, attack_mode, hash_type) within the
			// in-flight window, the agent is still running the previous
			// benchmark — don't fire a duplicate. Without this check,
			// every cycle re-dispatches the same combo until the result
			// finally arrives in agent_benchmarks (which can take
			// minutes for slow hash types or large hashlists). The
			// agent then races multiple concurrent benchmark goroutines
			// against the same WebSocket and crashes with "concurrent
			// write to websocket connection".
			//
			// The WS handler updates completed_at on benchmark_result
			// arrival (see job_websocket_integration.go
			// HandleBenchmarkResult), so a normal completion releases
			// the guard immediately. Orphaned rows from crashed agents
			// time out after benchmarkInFlightWindow.
			inFlight, err := agentHasInFlightBenchmark(ctx, database, agentID, int(u.AttackMode), c.hashType)
			if err != nil {
				debug.Warning("benchmark: in-flight check (agent=%d unit=%s): %v", agentID, u.ID, err)
				continue
			}
			if inFlight {
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

// agentHasInFlightBenchmark reports whether benchmark_requests has an
// uncompleted row for (agent, attack_mode, hash_type) within the
// in-flight staleness window. Used by IdentifyMissingBenchmarks to
// skip combos where the agent is still running a previous benchmark.
//
// attack_mode is stored as text in benchmark_requests (legacy schema —
// see migration that created the table); we stringify the int to match.
// salt_count is NOT in benchmark_requests' UNIQUE constraint, so the
// in-flight guard is broader than agent_benchmarks's salt-aware lookup:
// any pending request for (agent, mode, type) blocks dispatch
// regardless of salt_count. That's the intended scope — agents run one
// benchmark at a time per (mode, type) regardless of salt count.
func agentHasInFlightBenchmark(ctx context.Context, database *db.DB, agentID, attackMode, hashType int) (bool, error) {
	var exists bool
	err := database.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM benchmark_requests
			WHERE agent_id = $1
			  AND attack_mode = $2
			  AND hash_type = $3
			  AND completed_at IS NULL
			  AND requested_at > NOW() - ($4::text || ' seconds')::interval
		)
	`, agentID, strconv.Itoa(attackMode), hashType, strconv.Itoa(int(benchmarkInFlightWindow.Seconds()))).Scan(&exists)
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
		// Record the in-flight tracking row BEFORE sending. The next
		// cycle's IdentifyMissingBenchmarks consults this table to
		// suppress duplicate dispatches while the agent is still
		// running the benchmark. UPSERT (with DO UPDATE) handles
		// retries cleanly:
		//   - First dispatch: INSERT new row.
		//   - Stale uncompleted row (>5min): UPDATE refreshes
		//     requested_at, clears the previous completed_at/success/
		//     error_message (treated as a fresh attempt).
		//   - Completed row reaching this point means
		//     agentHasBenchmarkFor missed the cache write (race);
		//     resetting the row is still correct — we genuinely are
		//     re-running the benchmark.
		// If the INSERT fails (DB issue), we log and STILL send the
		// WS message — losing the tracking row is recoverable (worst
		// case: one duplicate dispatch next cycle), but losing the
		// benchmark request entirely would stall the cycle.
		if _, insErr := database.ExecContext(ctx, `
			INSERT INTO benchmark_requests (agent_id, job_execution_id, attack_mode, hash_type, request_type, requested_at)
			VALUES ($1, $2, $3, $4, 'agent_speed', NOW())
			ON CONFLICT (agent_id, attack_mode, hash_type) DO UPDATE
			SET requested_at     = NOW(),
			    completed_at     = NULL,
			    success          = NULL,
			    error_message    = NULL,
			    job_execution_id = EXCLUDED.job_execution_id,
			    request_type     = EXCLUDED.request_type
		`, g.AgentID, unit.ParentJobID, strconv.Itoa(g.AttackMode), g.HashType); insErr != nil {
			debug.Warning("benchmark: failed to record in-flight row for agent %d combo=%d/%d: %v (continuing with dispatch)", g.AgentID, g.AttackMode, g.HashType, insErr)
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
	taskPayload, err := BuildTaskAssignment(unit, uuid.New(), 0, 1, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("build task layout: %w", err)
	}

	// Enrich with hashlist + binary + the job's additional_args. The
	// additional_args (and the agent's extra_parameters fetched below) MUST
	// be passed so the benchmark runs the SAME flags as a real chunk —
	// crucially -O. Optimized kernels cap candidate length, which changes both
	// the measured speed AND the effective keyspace hashcat reports via
	// progress[1]. Running the benchmark without -O while real tasks use -O
	// caches a wrong speed (chunk-duration underestimation) and an unoptimized
	// keyspace. Mirrors the task-dispatch enrichment in cycle.sendAssignment.
	var hashlistID int64
	var hashType int
	var originalFilePath string
	var jobAdditionalArgs string
	err = database.QueryRowContext(ctx, `
		SELECT je.hashlist_id, h.hash_type_id, COALESCE(h.original_file_path, ''),
		       COALESCE(je.additional_args, '')
		FROM job_executions je
		JOIN hashlists h ON h.id = je.hashlist_id
		WHERE je.id = $1
	`, unit.ParentJobID).Scan(&hashlistID, &hashType, &originalFilePath, &jobAdditionalArgs)
	if err != nil {
		return nil, fmt.Errorf("lookup parent + hashlist: %w", err)
	}

	// Agent-level extra_parameters (e.g. "-w 4 -O"). Non-fatal on error —
	// a benchmark without them is still better than none, but log it.
	var agentExtraParams string
	if err := database.QueryRowContext(ctx, `
		SELECT COALESCE(extra_parameters, '') FROM agents WHERE id = $1
	`, g.AgentID).Scan(&agentExtraParams); err != nil {
		debug.Warning("benchmark: lookup agent %d extra_parameters: %v", g.AgentID, err)
	}

	binaryPath := ""
	if binaryResolver != nil {
		if binID, berr := binaryResolver.DetermineBinaryForTask(ctx, g.AgentID, unit.ParentJobID); berr == nil {
			binaryPath = fmt.Sprintf("binaries/%d", binID)
		}
	}

	hashlistPath := fmt.Sprintf("hashlists/%d.hash", hashlistID)

	// Resolve the speed-test timeouts from admin settings (compression-aware)
	// using the shared resolver — the SAME logic the legacy integration path
	// uses. Previously these were hardcoded to 10s/30s/2 here, which is far too
	// short for a "cold" agent to compile kernels and emit status updates,
	// causing spurious BENCHMARK_TIMEOUT failures and 24h blocklists.
	testDuration, timeoutDuration, minStatusUpdates := ResolveSpeedTestParameters(
		dbIntSettingReader(ctx, database), taskPayload.WordlistPaths)

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
		ExtraParameters:         agentExtraParams,
		JobAdditionalArgs:       jobAdditionalArgs,
		TestDuration:            testDuration,
		TimeoutDuration:         timeoutDuration,
		MinStatusUpdates:        minStatusUpdates,
	}
	// NOTE: EnabledDevices is intentionally not set here — the benchmark runs
	// on all of the agent's devices. Deriving the enabled subset needs the
	// device repo's runtime-options parsing (GetHashcatDeviceID), which this
	// free function doesn't have wired. Minor follow-up if device-limited
	// agents need device-scoped benchmark speed.
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
