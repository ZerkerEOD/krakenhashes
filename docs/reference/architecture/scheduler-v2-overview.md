# Scheduler v2 Overview

KrakenHashes runs a rewritten job scheduler — internally called **scheduler-v2** — that decides
which agents work on which jobs, divides work into chunks, and tracks completion. It replaced the
original ("v1") scheduler. This page gives a conceptual model of how v2 works and what changed, and
records the now-deprecated v1 behavior.

!!! info "Status"
    Scheduler-v2 is the **active** scheduler. It runs a full scheduling cycle every few seconds. The
    legacy v1 scheduler is **deprecated** — its code is retained for one release as a rollback option
    but it no longer runs. See [Legacy v1 scheduler (deprecated)](#legacy-v1-scheduler-deprecated).

## What changed, and why it matters

If you operated KrakenHashes before the rewrite, here's what's different in practice:

- **Accurate keyspace from the start.** New jobs are benchmarked before dispatch so progress
  percentages and time estimates reflect real work instead of a guess that lurches when the first
  results arrive.
- **No wasted or duplicated work.** Dispatch is driven by **coverage gaps** — the exact ranges of a
  job's keyspace that haven't been attempted yet — so agents never redo covered work, and changes to
  a job's resources only affect undispatched work (forward-only, no "deficit" bookkeeping).
- **Fairer, predictable agent allocation.** Agents are assigned by job priority with configurable
  overflow behavior, and the rules guarantee a compatible agent is never idle while a compatible job
  has dispatchable work.
- **Problems surface early.** Hashes are validated at upload, jobs that can't possibly run (wrong
  attack mode for the hashlist) fail fast, and when an agent sits idle the scheduler records *why* so
  it's visible in diagnostics.

## The scheduling cycle

The scheduler runs a single, self-contained **cycle** on a short interval (a few seconds). Each cycle
performs these steps and commits its own work:

1. **Evict timed-out tasks** — reclaim tasks from agents that stopped reporting so their keyspace
   becomes dispatchable again.
2. **Refresh the compatibility cache** — keep the agent↔job binary-version compatibility map fresh
   (re-evaluated periodically; misses fall through to a lazy single-pair check).
3. **Select schedulable units** — find the jobs (and increment-mode layers) that have uncovered
   keyspace and are otherwise ready to run.
4. **Allocate agents by priority** — assign idle, compatible agents to those units (see
   [Agent allocation](#agent-allocation-and-overflow-modes)). An agent is only considered
   dispatchable once it reports its startup file map is ready (`file_map_ready`); an agent still
   building that map is skipped for the cycle so it isn't handed a chunk it would only reject.
5. **Dispatch one chunk per agent** — create the next chunk for each allocated agent from its job's
   first uncovered coverage gap.
6. **Commit** — persist the cycle's intervals and tasks.

A unit is a single schedulable piece of work. For ordinary jobs there is one unit per job; for
[increment-mode](increment-mode.md) jobs there is one unit per length layer, all sharing the parent
job's agent cap.

## Coverage-gap (interval) dispatch

Each job's progress is tracked as **intervals** over its **base keyspace** — the positions in the
base wordlist or mask, before rules are applied. When the scheduler dispatches a chunk it records the
interval that chunk covers; the next chunk is carved from the **first remaining gap**. Because work
is tracked over the base keyspace:

- The same range is never dispatched twice.
- Adding or changing a wordlist, rule, or the potfile affects only the **undispatched** remainder —
  there's no attempt to retroactively "make up" work (see [Job Update System](job-update-system.md)).
- A job is complete when there are no uncovered base-keyspace gaps left and all dispatched tasks have
  finished (see [Job Completion System](job-completion-system.md)).

## Benchmark bootstrap and accurate keyspace

Before a brand-new job is dispatched, the scheduler arranges a **forced benchmark** that runs the
job's real configuration so hashcat reports the actual keyspace; that value replaces the initial
estimate, so progress and ETA are accurate immediately. The scheduler also keeps per-agent **speed
benchmarks** for each `(attack mode, hash type, salt count)` combination, used to size chunks to a
target duration.

Benchmarks are **salt-aware** (salt count is part of the cache key) and cached for a configurable
period before they're refreshed. Hashlists are re-downloaded fresh for benchmarking so the keyspace
reflects the current uncracked count. For the full benchmark flow, see
[Benchmark Workflow](benchmark-workflow.md).

## Compatibility matrix

Not every agent can run every job — an agent pinned to one hashcat binary version may be incompatible
with a job that requires another. The scheduler maintains a **compatibility cache** that maps which
agents can run which units (by binary-version pattern matching) and uses it during allocation so it
never pairs an incompatible agent with a job. See
[Binary Version Patterns](binary-version-patterns.md).

## Agent allocation and overflow modes

Within a cycle, idle compatible agents are allocated to units by **priority tier** (highest first),
with each unit filled toward its `max_agents` cap. What happens to *surplus* agents once every unit
at a tier is at its cap is governed by the **`agent_overflow_allocation_mode`** system setting. Five
modes are available:

| Mode (`agent_overflow_allocation_mode`) | UI label | Behavior |
|------|----------|----------|
| `fifo` | Priority – FIFO | Surplus at a tier goes entirely to the **oldest** job at that tier. Concentrates on the top tier that can use the agents. |
| `round_robin` | Priority – Round Robin | Surplus is spread one agent at a time across the tier's jobs. |
| `enforce_max_agents` | (strict) | No overflow at all — once every job is at its `max_agents`, remaining agents stay idle. Most predictable; can leave capacity unused. |
| `max_agents_fifo` | Max Agents – FIFO | Phase 1 fills **every** job at every tier to its cap (no tier starves), then surplus piles on the highest-priority job with remaining work. |
| `max_agents_round_robin` | Max Agents – Round Robin | Same Phase 1, then surplus rotates across units highest-priority-first. |

The "Priority" family concentrates agents on the highest tier that can use them; the "Max Agents"
family guarantees every job its baseline cap first and then accelerates higher-priority work with the
extras. A core invariant holds in all modes: **a compatible agent is never left idle while a
compatible job still has dispatchable work** — except that an agent still building its startup file
map is intentionally skipped until it reports `file_map_ready` (fail-open: an agent that never
reports readiness stays eligible). Higher-priority running tasks can also interrupt
lower-priority ones when interruption is enabled — see [Job Priority](../../admin-guide/advanced/job-priority.md).

## Chunking and dispatch

Each allocated agent receives **one chunk per cycle**, sized so it runs for roughly the target chunk
duration at the agent's benchmarked speed. Chunks are computed over the **base keyspace** (using
hashcat `--skip`/`--limit`), and when a job's rules would make a single chunk run too long, the rules
are split so each chunk still fits the target. For the full chunking model — including salted-hash
adjustments and rule splitting — see [Chunking System](chunking.md) and
[Rule Splitting](rule-splitting.md).

## Validation, fast-fail, and diagnostics

Scheduler-v2 surfaces problems earlier:

- **Hash validation at upload** rejects malformed hashes when a hashlist is created, instead of
  letting a job fail partway through.
- **Fast-fail** marks a job failed immediately if its hashlist has no hashes valid for the chosen
  attack mode, rather than burning agent time.
- **Error classification** sorts benchmark/task failures into categories (transient agent issues,
  persistent agent issues, job-config errors, fatal hashlist errors) and reacts accordingly —
  retry-with-cooldown, blocklist the agent, or fail the affected jobs and flag the hashlist.
- **Idle diagnostics** record a deduplicated reason whenever an agent could be working but isn't, so
  the cause is visible in [System Diagnostics](../../admin-guide/operations/diagnostics.md).

## Configuration

The scheduler reads several system settings (Admin → Settings). The most relevant:

| Setting | Controls |
|---------|----------|
| `agent_overflow_allocation_mode` | Surplus-agent policy (the five modes above). |
| `job_interruption_enabled` | Whether higher-priority jobs may interrupt running lower-priority tasks. |
| `default_chunk_duration` | Target running time per chunk (drives chunk sizing). |
| `benchmark_cache_duration_hours` | How long a benchmark stays valid before re-benchmarking. |
| `speedtest_timeout_seconds` | Timeout for an agent speed benchmark. |
| `keyspace_calculation_timeout_minutes` | Timeout for hashcat keyspace queries on large attacks. |
| `rule_split_enabled`, `rule_split_threshold`, `rule_split_min_rules` | Automatic rule splitting. |

See [Job Settings](../../admin-guide/operations/job-settings.md), [Job Priority](../../admin-guide/advanced/job-priority.md),
and [Job Chunking System](../../admin-guide/advanced/chunking.md) for operator-facing detail.

## Legacy v1 scheduler (deprecated)

!!! warning "Deprecated — applies to pre-2.1 behavior"
    The original scheduler ("v1") has been replaced by scheduler-v2 and **no longer runs**. Its
    source is kept for one release as a rollback safety net only. The behavior below is historical and
    is documented so older notes and screenshots still make sense — it does **not** describe the
    current system.

How v1 differed from the current scheduler:

- **Estimate-first keyspace.** v1 started jobs from an estimated keyspace and only captured the
  accurate value from the first progress update, so early progress and ETAs could jump. v2 benchmarks
  first for an accurate keyspace up front.
- **Benchmark ordering.** v1 could attempt to allocate before an agent had a usable benchmark, which
  could stall the first dispatch of a new job. v2 plans benchmarks as part of the cycle so allocation
  isn't blocked.
- **No compatibility matrix.** v1 lacked the binary-version compatibility cache that v2 uses to avoid
  pairing incompatible agent/job versions.
- **Salt-unaware benchmark caching.** v1 keyed benchmarks without salt count; v2 includes salt count
  so salted-hash speeds stay accurate.
- **Forward-only updates carried over.** Like v2, v1 did not track "deficit" work when resources
  changed — only undispatched work is affected — but v2 makes this explicit through interval-based
  coverage tracking.

## See also

- [Job Priority](../../admin-guide/advanced/job-priority.md) — priority scale, interruption, and how it interacts with allocation
- [Job Chunking System](../../admin-guide/advanced/chunking.md) — operator view of chunk sizing
- [Chunking System (architecture)](chunking.md) — base- vs effective-keyspace, salted hashes
- [Benchmark Workflow](benchmark-workflow.md) — benchmark planning and caching
- [Job Completion System](job-completion-system.md) — how completion is detected
- [Job Update System](job-update-system.md) — forward-only resource updates
