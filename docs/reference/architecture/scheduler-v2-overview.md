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
reports readiness stays eligible). A waiting higher-priority job can also **preempt** a running
lower-priority task to free an agent, but only when **both** gates are open: the system-wide
`job_interruption_enabled` setting is on **and** the waiting job itself has **Allow High Priority
Override** enabled. A third condition is structural rather than configurable — a job at **priority 0**
is never treated as starving, so it never preempts anything (there is nothing below it to take an
agent from). With any of those closed the job simply waits for an agent to free up naturally —
priority still decides who gets the next idle agent, it just never stops anyone's running work. See
[Job Priority](../../admin-guide/advanced/job-priority.md).

## Chunking and dispatch

Each allocated agent receives **one chunk per cycle**, sized so it runs for roughly the target chunk
duration at the agent's benchmarked speed. Chunks are computed over the **base keyspace** (using
hashcat `--skip`/`--limit`), and a job's rule multiplier is folded into the sizing — a heavy rule set
makes each chunk cover *fewer base words* rather than splitting the rule file. (v1 split rule files
physically; that mechanism, its columns and all five of its settings were removed with the v1
scheduler — [Rule Splitting](rule-splitting.md) documents the historical design only.) For the full
chunking model, including salted-hash adjustments, see [Chunking System](chunking.md).

### Stopping a task: truncate, release, or complete

Every server-initiated stop takes the same path, no matter why it fired — the chunk-overrun guard, a
preemption, an agent disconnect, a heartbeat eviction, an agent's graceful shutdown, or an operator
pressing stop. Recovery locks the task row, then reads its keyspace interval under the same lock and
branches on what the **coverage ledger** says, because the interval is the ledger and the task row is
only the work record. The invariant: a task may end `completed` only when its range is, and stays,
accounted for by an interval — if the range is going to be handed to somebody else, a `completed`
task sitting on top of it would double-count in every coverage, progress and completion query.

That produces three outcomes, not two:

1. **There is progress to keep** — the agent reported a restore point past the task's own range
   start and its interval is still open. The interval is **truncated** to
   `[range_start, restore_point)` and the task row is **completed at 100% of its new, smaller
   range**. It is not reported as a partial failure and it is *not* put back to `pending`. The
   unprocessed remainder becomes a **gap** automatically, because no interval row covers it any more,
   and the next dispatch cycle re-issues it — usually to a different agent. A chunk that happened to
   process its whole range is not a special case: the same branch matches when the restore point
   equals the range end, and simply closes the interval without shrinking it.
2. **No progress and no cracks** — there is nothing to keep, so the task row **and** its interval are
   **deleted**. The task disappears from the job's task list entirely and the whole original range
   re-opens.
3. **No progress, but the task produced cracks** — the task is marked **`cancelled`** and only the
   interval is released. The row has to survive: `hashes.cracked_by_task_id` is `ON DELETE SET NULL`,
   and the [loopback](loopback.md) delta INNER JOINs on it, so deleting the row would silently drop
   those plaintexts from the delta. Cancellation is also the fallback whenever the delete is blocked
   — cracks already counted, or a crack batch still in flight behind the stop message.

A fourth, quieter case exists for tasks whose range was **already accounted for**: if the interval
already reads `completed` — most often because the hashlist was fully cracked while the task was
still draining crack batches — the task is marked `completed` at its restore point (or `cancelled` if
it has none) and the interval is left strictly alone. Deleting coverage for a range that really was
searched would make the dispatcher re-issue finished work; that mistake is what GH #79 was.

!!! important "`failed` now means the agent *reported* a failure"
    None of the stops above produce a `failed` task. That is deliberate: `HasFailedTasks` is a
    `COUNT(*) > 0`, not a threshold, so one `failed` row permanently fails its entire job — even
    after the re-opened range has been redone successfully by another agent. A `failed` task
    therefore carries exactly one meaning: the agent reported that the task failed.

Because coverage is an interval set rather than a single watermark, a stop in the middle of a unit
leaves a **hole**, not a shortened tail. The gap query returns the lowest-start gap first, so a
mid-keyspace hole is always re-dispatched **before** the untouched tail. Concretely: if `[0,100)` is
complete, a task working `[100,150)` is stopped at 105, and another agent is already on `[150,200)`,
the next chunk handed out is `[105,150)` — not `[200, …)`.

This is why a stopped task must never be parked in `pending` with its interval left live: the gap
query counts any non-`failed` interval as covered, so such a range would look permanently done while
nobody was working it, and the job would sit at "fully covered but never complete" forever
(GH #77). The heartbeat sweeper also re-checks for that shape on every pass and heals it.

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
| `job_interruption_enabled` | Global gate for preemption. Preemption additionally requires the waiting job's own **Allow High Priority Override** flag *and* a non-zero job priority. **Do not assume its state — read the toggle.** The initial migration seeds the row `true`, but the scheduler's own fallback is the opposite: if the setting is missing or unreadable it is treated as **off**, so a configuration problem can never start stopping running work by accident. |
| `chunk_overrun_guard_enabled`, `chunk_overrun_tolerance_percent` | Stop a task that runs past `chunk_duration × (1 + tolerance)` and re-dispatch the remainder. |
| `default_chunk_duration` | Target running time per chunk (drives chunk sizing). Falls back to `target_chunk_seconds` only when unset or zero; a per-job `chunk_size_seconds` override beats both. |
| `min_chunk_seconds` | Floor on chunk wall time — a gap smaller than this many seconds of work is dispatched whole rather than sliced, which prevents one-candidate orphan chunks. |
| `task_heartbeat_timeout_seconds`, `task_startup_grace_seconds`, `network_grace_seconds` | Liveness windows that decide when a running task is considered lost and gap-recovered (see [Scheduler (v2) Timing](../../admin-guide/operations/job-settings.md#scheduler-v2-timing)). |
| `benchmark_cache_duration_hours` | How long a benchmark stays valid before re-benchmarking (default 168 = 7 days). |
| `speed_test_timeout_seconds_uncompressed`, `speed_test_timeout_seconds_compressed` | Timeouts for an agent speed benchmark; compressed wordlists get the longer window because they must be decompressed first. (These replaced the single `speedtest_timeout_seconds`, which was deleted with the v1 scheduler.) |
| `keyspace_calculation_timeout_minutes` | Timeout for hashcat keyspace queries on large attacks. |

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
