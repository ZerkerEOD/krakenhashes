# Loopback Sessions

## Overview

**Loopback** (GH #64) re-runs the *mutating* part of an attack against only the
newly-cracked plaintexts (the "delta"), repeating until a round produces no new plaintext.
It exists because "run this preset again in a workflow" would re-try the whole wordlist for a
handful of new candidates; loopback instead loops the mutation over just the delta, so each
round is cheap and the loop terminates on its own.

The original GitHub issue asked to allow the same preset job multiple times in a workflow.
That was resolved **not** by permitting duplicates, but by this delta-only re-run mechanism.

## Concepts

### Eligibility

`models.IsMutatableAttack(mode, ruleIDs)` decides whether an attack's mutation can be re-run
against a delta wordlist:

| Attack mode | Mutatable | Rationale |
| --- | --- | --- |
| Straight (0) **with** rules | ✅ | The rules are the mutation → `delta × rules` |
| Hybrid wordlist+mask (6) | ✅ | The mask is the mutation → `delta + mask` |
| Hybrid mask+wordlist (7) | ✅ | The mask is the mutation → `mask + delta` |
| Straight (0) **without** rules | ❌ | The word was already tried verbatim |
| Brute-force (3) | ❌ | No wordlist to swap the delta into |
| Combination (1) | ❌ | No single, clean "mutation" side |
| Association (9) | ❌ | 1:1 hash↔word mapping, no loopback semantics |

Ineligible jobs are **not** re-run, but their cracks still feed the delta pool that eligible
lineages loop on.

### The delta

For each round, the delta is the set of **distinct new plaintexts cracked by the session's
own jobs** that have not yet been used as loopback input for this session:

- Cracks are scoped to the session via `hashes.cracked_by_task_id` → `job_tasks` →
  `loopback_session_jobs`, so only *this* session's work contributes.
- A per-session guard set, `loopback_session_plaintexts` (the md5 of each plaintext already
  used as input), subtracts candidates that were fed in a previous round.

Because every distinct plaintext is consumed **exactly once**, the loop is guaranteed to
terminate: a round is either strictly smaller than the pool of not-yet-tried cracks, or empty
(dry) — at which point the session completes.

The delta is read straight from the `hashes` table (synchronous), so a potfile flush is
**not** required before a round can start.

## Session Lifecycle

A **session** is one loopback group: a workflow run, or a standalone preset/custom run.

```
waiting ──▶ active ──▶ completed
   │           │
   └──────┬────┴──────▶ failed / cancelled
```

| Status | Meaning |
| --- | --- |
| `waiting` | Round-0 (original) jobs are still running; controller is waiting for them to reach a terminal state |
| `active` | At least one delta round has been spawned; controller is monitoring the current round |
| `completed` | A round came back dry, or `max_rounds` was reached |
| `failed` | The session errored (see `error_message`) |
| `cancelled` | The session was cancelled |

### The controller / monitor

`services.LoopbackService` is a **durable** controller — it survives a backend restart
because each tick re-queries the `waiting`/`active` sessions from the database rather than
holding them in memory (unlike the transient `preparing` job state, which is failed on
restart). It is started once via a `sync.Once` in `routes/user.go` `CreateJobsHandler`.

An **idle-gated poller** (≈7 s) checks `LoopbackRepository.HasActiveSessions` — backed by a
partial index on `loopback_sessions(status) WHERE status IN ('waiting','active')` — so it
costs almost nothing when no sessions are in flight.

On each tick, for a session whose current round's jobs are all terminal:

1. Stop early if the hashlist is already fully cracked (`HashlistHasUncracked`) — no wasted
   round against an exhausted list.
2. Compute the delta.
3. If the delta is empty → **complete** the session (dry).
4. Otherwise spawn one re-run per mutatable lineage and advance the round.

Re-runs reuse the pre-filter job path (`CreatePreparingFilterJob` → materialize the
ephemeral delta wordlist → `FinalizeFilterJob`). They are ordinary `job_executions`, so the
scheduler, agents, and progress tracking are entirely unchanged — the loopback layer only
decides *what* to enqueue and *when*.

!!! note "Session creation is transactional"
    `CreateSessionWithJobs` inserts the session and links its round-0 jobs in one transaction,
    closing a race where the monitor could observe a session before its round-0 jobs were
    linked and prematurely complete it as dry.

### Delta wordlist materialization

`wordlist.Manager.MaterializeEphemeralWordlist([]string, …)` writes the delta to a
`custom/__eph__*.txt` wordlist flagged `is_ephemeral` with an `owner_job_id`, inheriting the
[wordlist filtering](../../user-guide/wordlist-filtering.md) cleanup lifecycle. (This differs
from the GH #40 materializer, which *filters a parent* wordlist rather than writing an
explicit line set.)

## Configuration

Loopback config lives on the workflow or at job-start time — **never** on the preset job
definition:

| Location | Field / control |
| --- | --- |
| `job_workflows.loopback_all_eligible` | Master toggle: loop every eligible step. When true, per-step flags are ignored. |
| `job_workflow_steps.loopback_enabled` | Per-step toggle, used only when the workflow master toggle is off. |
| Create-job request (`loopback`) | Standalone preset/custom runs carry the toggle in the request payload. |

The create-job wiring is in `handlers/jobs/user_jobs.go` `CreateJobFromHashlist`: preset and
custom runs read the request `loopback` flag; workflow runs read the step/master flags. The
frontend surfaces are `JobWorkflowForm.tsx` (workflow builder — master + per-step, eligibility
aware), `CreateJobDialog.tsx` (per-run toggle on the Preset and Custom tabs; a "Loopback"
badge on the Workflow tab), and `components/jobs/LoopbackSessionsPanel.tsx` (the monitoring
panel on the Jobs page and Dashboard, backed by `GET /api/loopback-sessions`).

### `loopback_max_rounds`

A `system_settings` row (`loopback_max_rounds`, default `10`, integer) caps how many delta
rounds a session runs before it stops, even if new cracks keep appearing. It is resolved when
a session is created (`resolveMaxRounds`), falling back to `defaultLoopbackMaxRounds = 10` if
the setting is missing. See [Job Settings](../../admin-guide/operations/job-settings.md#loopback-round-cap).

## Schema

Migration `20260717120000_add_loopback`:

- `job_workflows.loopback_all_eligible BOOLEAN` — workflow master toggle.
- `job_workflow_steps.loopback_enabled BOOLEAN` — per-step toggle.
- **`loopback_sessions`** — one row per session (`hashlist_id`, `source_type`
  workflow/preset/custom, `source_workflow_id`, `status`, `current_round`, `max_rounds`,
  `created_by`). Partial index on the in-flight statuses for the monitor's idle gate.
- **`loopback_session_jobs`** — links each `job_execution` to a session per `round`, with a
  `role` (`original` | `rerun`), `is_mutatable`, and `origin_job_id` (the round-0 job a
  lineage descends from). `round 0` = original jobs; `round >= 1` = controller re-runs.
- **`loopback_session_plaintexts`** — the per-session used-set (`plaintext_md5`) that
  guarantees each plaintext is fed exactly once.
- `system_settings` row `loopback_max_rounds` (default `10`).

## Scope and caveats (v1)

- Custom loopback is **not** combined with the GH #40 wordlist filter — the two are mutually
  exclusive in the UI.
- Combination (`-a 1`) and association (`-a 9`) are intentionally excluded.
- Only the UI create-job path (`user_jobs.go`) creates sessions; the programmatic `api/v1`
  job-create path does **not** start loopback sessions.

## Related

- [Loopback (user guide)](../../user-guide/loopback.md)
- [Increment Mode](increment-mode.md)
- [Job Completion System](job-completion-system.md)
- [Wordlist Filtering](../../user-guide/wordlist-filtering.md)
- [Scheduler v2 Overview](scheduler-v2-overview.md)
