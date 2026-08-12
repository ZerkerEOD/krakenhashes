# Job Execution Settings

## Overview

The Job Execution Settings page allows administrators to configure how KrakenHashes executes and distributes password cracking jobs across agents. These settings control chunking behavior, agent coordination, job control, and rule splitting strategies.

## Accessing Job Execution Settings

1. Navigate to the **Admin Panel**
2. Click on **Settings** in the navigation menu
3. Select **Job Execution Settings**

The settings are grouped into panels for easier management — chunking, agent behavior, job control,
the scheduler's own timing and guards, and so on. Each `###` section below corresponds to a panel on
that page.

## Settings Categories

### Job Chunking

Job chunking divides large password cracking tasks into smaller, manageable pieces that can be distributed across multiple agents. This improves resource utilization and allows for better job scheduling.

| Setting | Description | Default | Range | Notes |
|---------|-------------|---------|--------|-------|
| **Default Chunk Duration** (`default_chunk_duration`) | Target running time for each job chunk | 20 minutes | 1+ minutes | Shorter chunks provide more flexibility but increase overhead. A per-job `chunk_size_seconds` override wins over this; if this setting is missing or zero the scheduler falls back to `target_chunk_seconds` (60s) |
| **Minimum Chunk Duration** (`min_chunk_seconds`) | Floor on chunk wall time | 5 seconds | 1-300 seconds | A gap worth less than this much work is dispatched whole instead of being sliced, which prevents one-candidate orphan chunks |

!!! note "`chunk_fluctuation_percentage` no longer exists"
    The old final-chunk variance knob was dropped along with the v1 scheduler
    (migration `20260707151802`). Scheduler-v2 has no look-ahead remainder merge — its tail guard is
    `min_chunk_seconds` above.

#### Best Practices for Chunking
- **Short jobs (< 1 hour)**: Use 5-10 minute chunks for better distribution
- **Long jobs (> 24 hours)**: Use 30-60 minute chunks to reduce overhead
- **Mixed agent speeds**: Shorter chunks help balance workload

### Agent Configuration

These settings control how agents behave and interact with the backend server.

| Setting | Description | Default | Range | Notes |
|---------|-------------|---------|--------|-------|
| **Hashlist Retention** (`agent_hashlist_retention_hours`) | How long agents keep hashlists after job completion | 24 hours | 1+ hours | Reduces re-download for recurring jobs |
| **Max Concurrent Jobs per Agent** (`max_concurrent_jobs_per_agent`) | Maximum jobs an agent can run simultaneously | 1 | 1-10 | Higher values for powerful multi-GPU systems |
| **Progress Reporting Interval** (`progress_reporting_interval`) | How often agents send progress updates | 5 seconds | 1+ seconds | Lower values increase server load |
| **Benchmark Cache Duration** (`benchmark_cache_duration_hours`) | How long to cache agent performance benchmarks | 168 hours (7 days) | 1+ hours | Reduces benchmark frequency |
| **Reconnect Grace Period** (`reconnect_grace_period_minutes`) | Time to wait for agents to reconnect after server restart | 5 minutes | 1-60 minutes | Prevents unnecessary task reassignment |

!!! note "`speedtest_timeout_seconds` was replaced"
    The single speedtest timeout was dropped by migration `20260707151802`. Scheduler-v2 uses
    `speed_test_timeout_seconds_uncompressed` and `speed_test_timeout_seconds_compressed` instead
    (plus a fixed grace), because a compressed wordlist has to be decompressed before hashcat can
    measure anything and needs a much longer window.

#### Reconnect Grace Period Details

The **Reconnect Grace Period** is a critical setting for maintaining job continuity during server maintenance or unexpected restarts:

- **Purpose**: Allows agents with running tasks to reconnect and continue their work without losing progress
- **How it works**: 
  - When the backend restarts, tasks transition to `reconnect_pending` state
  - Agents cache crack data locally and continue processing
  - Upon reconnection, agents report their current task status
  - If reconnected within the grace period, tasks resume automatically
- **Recommended values**:
  - **5 minutes** (default): Good for most environments
  - **10-15 minutes**: For environments with slower network recovery
  - **1-3 minutes**: For highly available setups with quick recovery

### Job Control

Control job execution behavior and user interface settings.

| Setting | Description | Default | Range | Notes |
|---------|-------------|---------|--------|-------|
| **Allow Job Interruption** (`job_interruption_enabled`) | Global gate: higher priority jobs may interrupt running work | Seeded enabled — but read the toggle, see below | On/Off | Only one of three gates; on its own it interrupts nothing |
| **Agent Overflow Allocation Mode** (`agent_overflow_allocation_mode`) | How to distribute agents left over once jobs hit their `max_agents` limit | `fifo` | `fifo`, `round_robin`, `enforce_max_agents`, `max_agents_fifo`, `max_agents_round_robin` | Controls fairness vs speed tradeoff |
| **Real-time Crack Notifications** | Send notifications when hashes are cracked | Enabled | On/Off | Can increase server load for large jobs |
| **Job Refresh Interval** | How often the UI refreshes job status | 5 seconds | 1-60 seconds | Lower values increase server load |
| **Max Chunk Retry Attempts** | Number of times to retry failed chunks | 3 | 0-10 | Set to 0 to disable retries |
| **Jobs Per Page** | Default pagination size for job lists | 25 | 5-100 | Adjust based on UI preferences |
| **Hashlist Bulk Batch Size** | Number of hashes processed per batch during import | 100,000 | 1,000-1,000,000 | Affects memory usage and import speed |

#### Job Interruption Behavior

This is the **global** gate, and it is only one of three conditions. A job also needs its own
**Allow High Priority Override** flag, and a priority above 0 — a priority-0 job never preempts
anything. All three must hold before a single task is stopped.

**Read the toggle rather than assuming its state.** The database migration seeds this row `true`, but
the scheduler's own fallback is the opposite: if the setting is missing or unreadable it is treated
as **off**, so a configuration problem can never cause running work to be stopped. Preemption being
disabled is silent by design — the waiting job simply waits — so "my high-priority job never
interrupted anything" starts here.

When all three conditions hold, the system will:
1. Stop the newest running task at the lowest priority when a higher-priority job is waiting with no
   agent available
2. Truncate that task's keyspace at its last restore point and close it out as completed for the work
   it finished — the task is **not** returned to `pending`. If it never reached a restore point there
   is nothing to keep, so the task is deleted outright (or marked `cancelled` if it had already
   produced cracks, which preserves the crack attribution). A stopped task is never marked `failed`
3. Return the unfinished remainder to the queue as undispatched work, re-dispatched as soon as an
   agent is free
4. Preserve all crack progress; no keyspace is ever re-run

See [Job Priority](../advanced/job-priority.md) for the full model.

#### Agent Overflow Allocation Mode

This setting controls what happens to **surplus** agents — the ones still idle once every job at a
priority tier has been filled to its `max_agents` cap.

**Five modes are available**, in two families. The table below is a summary; see
[Scheduler v2 Overview — Agent allocation and overflow modes](../../reference/architecture/scheduler-v2-overview.md#agent-allocation-and-overflow-modes)
for the full model rather than a second copy of it here.

| Value | UI label | Surplus behavior |
|-------|----------|------------------|
| `fifo` (default) | Priority – FIFO | All surplus at a tier goes to the **oldest** job at that tier |
| `round_robin` | Priority – Round Robin | Surplus is spread one agent at a time across the tier's jobs |
| `enforce_max_agents` | (strict) | No overflow at all — surplus agents descend to the next priority tier, and stay idle if every job everywhere is at its cap |
| `max_agents_fifo` | Max Agents – FIFO | Fill **every** job at every tier to its cap first (no tier starves), then pile the surplus on the highest-priority job with work left |
| `max_agents_round_robin` | Max Agents – Round Robin | Same first phase, then rotate the surplus across units highest-priority-first |

The "Priority" family concentrates surplus on the highest tier that can use it; the "Max Agents"
family guarantees every job its baseline cap before accelerating anything.

##### FIFO Mode (Default)

**Behavior**: Oldest job gets all overflow agents

**Use Cases:**
- **Default mode**: Best for most scenarios
- **Fairness**: Jobs get their turn in creation order
- **Completion focus**: Concentrates resources to finish jobs faster
- **Simple behavior**: Predictable allocation pattern

**Example:**
```
3 jobs at priority 50, each with max_agents = 2
15 available agents total

Job A (created first):  2 agents (max_agents)
Job B (created second): 2 agents (max_agents)
Job C (created third):  2 agents (max_agents)
Overflow:              9 agents → ALL go to Job A (oldest)

Final: Job A = 11 agents, Job B = 2 agents, Job C = 2 agents
```

##### Round-Robin Mode

**Behavior**: Distribute overflow agents evenly across all jobs at same priority

**Use Cases:**
- **Parallel progress**: Want all jobs to progress simultaneously
- **Testing**: Running multiple test jobs at once
- **Even distribution**: Prefer balanced allocation over speed
- **Multiple clients**: Each job from different client, want fairness

**Example:**
```
3 jobs at priority 50, each with max_agents = 2
15 available agents total

Job A (created first):  2 agents (max_agents)
Job B (created second): 2 agents (max_agents)
Job C (created third):  2 agents (max_agents)
Overflow:              9 agents → distributed evenly (3 each)

Final: Job A = 5 agents, Job B = 5 agents, Job C = 5 agents
```

**Allocation Logic:**
```
1. Calculate base allocation: 9 overflow / 3 jobs = 3 agents per job
2. Calculate remainder: 9 % 3 = 0 (no remainder)
3. Distribute base to all jobs: +3 each
4. If remainder exists, give to oldest jobs first
```

##### Priority-Based Behavior

The mode governs *surplus* agents; **priority still decides who is filled first**, and in the two
"Priority" modes a higher tier is filled to exhaustion before a lower tier sees an agent:

```
Scenario: 2 jobs, 10 agents available

Job A: Priority 100, max_agents = 3
Job B: Priority 50,  max_agents = 5

fifo / round_robin:      Job A takes all 10 (surplus concentrates on the top tier)
enforce_max_agents:      Job A gets 3, Job B gets 5, 2 agents stay idle
max_agents_*:            Job A gets 3 and Job B gets 5 first, then the 2 surplus
                         agents go to Job A (highest priority with work left)
```

##### Configuration via SQL

```sql
-- View current setting
SELECT key, value, description
FROM system_settings
WHERE key = 'agent_overflow_allocation_mode';

-- Valid values: 'fifo', 'round_robin', 'enforce_max_agents',
--               'max_agents_fifo', 'max_agents_round_robin'
UPDATE system_settings
SET value = 'fifo'
WHERE key = 'agent_overflow_allocation_mode';
```

##### When to Use Each Mode

| Scenario | Recommended Mode | Reason |
|----------|------------------|--------|
| Production cracking (default) | `fifo` | Finish jobs faster by concentrating resources |
| Multiple test jobs | `round_robin` | See results from all tests simultaneously |
| Multi-client environment | `round_robin` | Fair distribution across clients |
| Single large job | Either Priority mode | No difference (only one job) |
| Time-critical job | `fifo` | Ensures oldest/most important finishes first |
| Parallel research | `round_robin` | Compare multiple approaches simultaneously |
| Hard per-job agent budgets (chargeback, licensing) | `enforce_max_agents` | Caps are never exceeded, even if agents go idle |
| Every job must make some progress, but urgent work should still finish first | `max_agents_fifo` / `max_agents_round_robin` | Baseline cap for all tiers first, surplus to the highest priority |

#### Hashlist Bulk Batch Size

This setting controls how many hashes are processed in each database batch during hashlist imports and bulk operations. It directly affects memory usage and import performance.

**How It Works:**
1. When importing a large hashlist (e.g., 10 million hashes), the system divides the work into batches
2. Each batch processes up to `hashlist_bulk_batch_size` hashes at once
3. Larger batches = faster imports but higher memory usage
4. Smaller batches = lower memory usage but slower imports

**Recommended Values:**

| Environment | Batch Size | Rationale |
|-------------|------------|-----------|
| Low memory (< 4GB RAM) | 10,000-25,000 | Minimizes memory pressure |
| Standard (4-16GB RAM) | 50,000-100,000 | Balanced performance |
| High memory (16GB+ RAM) | 100,000-500,000 | Maximizes import speed |
| Very large hashlists (50M+) | 100,000 | Prevents memory exhaustion |

**Configuration via SQL:**

```sql
-- View current setting
SELECT key, value, description
FROM system_settings
WHERE key = 'hashlist_bulk_batch_size';

-- Set batch size (example: 50,000 for memory-constrained systems)
UPDATE system_settings
SET value = '50000'
WHERE key = 'hashlist_bulk_batch_size';

-- Set batch size (example: 200,000 for high-memory systems)
UPDATE system_settings
SET value = '200000'
WHERE key = 'hashlist_bulk_batch_size';
```

**Performance Impact:**
- **Import Time**: Doubling batch size typically reduces import time by 20-30%
- **Memory Usage**: Roughly linear with batch size (~10MB per 100,000 hashes)
- **Database Load**: Larger batches create fewer but larger transactions

#### Loopback Round Cap

The `loopback_max_rounds` setting caps how many delta rounds a [loopback](../../user-guide/loopback.md) session runs before it stops, even if new cracks keep appearing. It is a safety bound — most sessions go "dry" (a round finds nothing new) well before the cap.

| Setting | Description | Default | Notes |
|---------|-------------|---------|-------|
| **`loopback_max_rounds`** | Maximum delta rounds per loopback session | 10 | Applied when a session is created |

The value is read when a session starts; changing it affects **new** sessions, not ones already in flight. If the setting is missing, the backend falls back to 10.

```sql
-- View current setting
SELECT key, value, description
FROM system_settings
WHERE key = 'loopback_max_rounds';

-- Raise the cap (example: allow up to 25 rounds)
UPDATE system_settings
SET value = '25'
WHERE key = 'loopback_max_rounds';
```

See [Loopback](../../user-guide/loopback.md) for how sessions work and the
[Loopback Sessions architecture](../../reference/architecture/loopback.md) reference for internals.

### Scheduler (v2) Timing

Found in the **Scheduler (v2)** panel of the Job Execution Settings page, alongside the chunk overrun
guard below.

These knobs decide when the scheduler stops believing an agent is still working. Getting them wrong
in one direction leaves a dead task holding keyspace for hours; in the other it evicts a healthy
agent that was merely busy decompressing a 40 GB wordlist. When a task *is* declared lost, it goes
through the same truncate-and-re-open recovery as every other stop — see
[Job Priority — Job Interruption Behavior](../advanced/job-priority.md#job-interruption-behavior).

| Setting | Description | Default | Range | Notes |
|---------|-------------|---------|-------|-------|
| **Task Heartbeat Timeout** (`task_heartbeat_timeout_seconds`) | Seconds without any liveness signal before a running task is considered lost and gap-recovered | 120 | 10-3600 | "Liveness" is broader than progress: a progress update, a liveness ping, a `task_loading` message, or a new outfile crack all count |
| **Task Startup Grace** (`task_startup_grace_seconds`) | Pre-first-progress grace window after a task is started | 600 | 30-7200 | The heartbeat timer does not start until either the first progress update arrives or this window expires |
| **Network Grace** (`network_grace_seconds`) | WebSocket reconnect tolerance for a running task whose agent drops | 30 | 5-600 | Recovery only fires if the agent fails to reconnect within it — a brief network blip costs nothing |
| **Target Chunk Seconds** (`target_chunk_seconds`) | Fallback target wall time per chunk | 60 seconds | 1+ seconds | Consulted **only** when `default_chunk_duration` is missing or zero. Not exposed in the UI; set it in SQL if you need it |
| **Minimum Chunk Duration** (`min_chunk_seconds`) | Floor on chunk wall time | 5 seconds | 1-300 | Shown in the **Job Chunking** panel, not here. See [Job Chunking](#job-chunking) |

#### Task Startup Grace

`task_startup_grace_seconds` is the one operators most often need to raise. It covers everything that
happens between "task assigned" and "hashcat emits its first progress line": downloading wordlists,
rules and the hashlist, decompressing them, and hashcat's own kernel autotune. None of that produces
progress, so without the grace window a large first-time download on a slow link would be evicted as
a dead task and re-dispatched to another agent that would face the same download.

It is also the knob that governs the `--slow-candidates` startup stall described under
[Zero-Progress Overruns](#zero-progress-overruns-count-against-the-agent) below: a healthy agent on a
slow hash type legitimately reports nothing for the first several minutes of a chunk. Raise the grace
window if your fleet syncs large resources over slow links or works slow hash types; lower it only if
you would rather reclaim a wedged agent faster than tolerate a long, legitimate startup.

### Chunk Overrun Guard

Found in the **Scheduler (v2)** panel of the Job Execution Settings page.

Chunks are sized from an agent's benchmarked speed so that each one runs for roughly the target
chunk duration. When that estimate is badly wrong — the agent is slower than benchmarked, or the
work is heavier than expected — a chunk can run for hours instead of minutes, holding an agent
hostage and blocking higher-priority work. The chunk overrun guard stops such a task, re-dispatches
the unfinished remainder, and feeds the *measured* speed back so the next chunk for that agent is
sized correctly.

| Setting | Description | Default | Range | Notes |
|---------|-------------|---------|-------|-------|
| **Chunk Overrun Guard** (`chunk_overrun_guard_enabled`) | Stop tasks that run past their chunk target | Enabled | On/Off | When off, a long-running chunk is left to finish on its own |
| **Overrun Tolerance** (`chunk_overrun_tolerance_percent`) | Grace window before the guard fires | 20% | 0-200% | The guard fires once wall time exceeds `chunk_duration × (1 + tolerance/100)` |

#### What Happens When It Fires

1. The agent is sent a stop for that task.
2. The task's keyspace range is truncated at its last restore point and closed out as completed for
   the work it actually did; the remainder returns to the queue as an undispatched gap. This is the
   same truncate-and-re-open path used by preemption and agent disconnects — see
   [Job Priority — Job Interruption Behavior](../advanced/job-priority.md#job-interruption-behavior).
3. The speed actually observed on that chunk is recorded, so the re-dispatched remainder (and future
   chunks for that agent, attack mode and hash type) are sized from reality instead of the stale
   benchmark.

#### Zero-Progress Overruns Count Against the Agent

If an overrunning chunk had made **no keyspace progress at all** — its restore point never advanced
past its own range start — the guard additionally charges a failure against that agent, through the
same per-(agent, attack mode, hash type) policy engine that handles ordinary task failures. Its
cooldown and blocklist machinery then routes the re-opened range to a *different* agent.

This closes a livelock: the speed feedback above can't help a task that reported nothing, because
there is no speed to record. Without the failure attribution the wedged agent keeps its optimistic
benchmark, gets handed the very same re-opened range on the next cycle, wedges again, and the job
never advances. Typical causes are hashcat stuck in autotune, a hung GPU driver, or a file download
that never finishes.

!!! note "Why this check only applies at the overrun threshold"
    Under hashcat's `--slow-candidates` (`-S`) mode — used for slow hash types — a perfectly healthy
    agent legitimately reports zero progress for the first several minutes of a chunk while the
    host-side candidate generator spins up. Treating "no progress yet" as a fault at any earlier
    point would punish healthy agents on every slow-hash job. A task that is past
    `chunk_duration × (1 + tolerance)` is by definition past any legitimate startup stall, because
    its chunk duration was sized from that agent's own measured speed in the first place. The
    window that protects such a task *before* the overrun threshold is
    [`task_startup_grace_seconds`](#task-startup-grace).

Lower the tolerance if you want tighter turnaround on mis-sized chunks; raise it (or disable the
guard) if your workload has legitimately variable chunk times and you would rather let long chunks
run to completion.

### Rule Splitting (removed)

!!! warning "These settings no longer exist"
    Rule splitting belonged to the v1 scheduler. Migration `20260707151802_remove_rule_splitting`
    dropped its columns and deleted every one of its settings — `rule_split_enabled`,
    `rule_split_threshold`, `rule_split_min_rules`, `rule_split_max_chunks` and
    `rule_chunk_temp_dir`. No code path reads them, and the Job Execution Settings page no longer
    shows them.

Scheduler-v2 needs none of it: it splits every job over the **base keyspace** with hashcat
`--skip`/`--limit` and accounts for a rule file's cost through the job's multiplication factor, so a
heavy rule set produces smaller base-word chunks instead of physically split rule files. See
[Chunking System](../../reference/architecture/chunking.md).

## Performance Considerations

### Network Load
- **Progress Reporting Interval**: Each update creates network traffic
- **Job Refresh Interval**: Affects UI responsiveness and server load
- Calculate: `(Number of Agents × Active Jobs) / Reporting Interval = Updates per second`

### Storage Requirements
- **Hashlist Retention**: `Average Hashlist Size × Number of Unique Jobs` held for the retention
  window (`agent_hashlist_retention_hours`, 24 hours by default)
- **Benchmark Cache**: Minimal, typically < 1MB per agent

### Optimal Settings by Environment

#### Small Environment (1-5 agents)
- Chunk Duration: 10-15 minutes
- Progress Interval: 30 seconds
- Max Concurrent Jobs: 1
- Grace Period: 5 minutes

#### Medium Environment (5-20 agents)
- Chunk Duration: 15-30 minutes
- Progress Interval: 60 seconds
- Max Concurrent Jobs: 1-2
- Grace Period: 10 minutes

#### Large Environment (20+ agents)
- Chunk Duration: 30-60 minutes
- Progress Interval: 120 seconds
- Max Concurrent Jobs: 2-3
- Grace Period: 15 minutes

## Troubleshooting

### Common Issues

#### Agents Not Receiving Jobs
- Check **Max Concurrent Jobs per Agent** setting
- Verify agents are not at capacity
- Review job priority settings

#### Poor Job Distribution
- Reduce **Default Chunk Duration** for better granularity
- Raise **Minimum Chunk Duration** if agents are being handed uselessly small gaps
- Check **Agent Overflow Allocation Mode** — a "Priority" mode concentrates surplus agents on one job
  by design

#### High Server Load
- Increase **Progress Reporting Interval**
- Increase **Job Refresh Interval**
- Disable **Real-time Crack Notifications** for large jobs

#### Lost Progress After Server Restart
- Increase **Reconnect Grace Period**
- Ensure agents have stable network connections
- Check agent logs for reconnection issues

### Monitoring Settings Impact

Use the following metrics to evaluate settings effectiveness:
- Average chunk completion time vs. configured duration
- Number of retry attempts per job
- Agent utilization percentage
- Task reassignment frequency after restarts

## Related Documentation

- [Agent Management](agents.md) - Managing and monitoring agents
- [Job Chunking](../advanced/chunking.md) - Detailed chunking strategies
- [Performance Tuning](../advanced/performance.md) - System optimization
- [Rule Management](../resource-management/rules.md) - Managing rule files