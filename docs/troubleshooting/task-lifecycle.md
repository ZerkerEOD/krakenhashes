# Task Lifecycle and Statuses

A job's keyspace is dispatched to agents as tasks (chunks). This page explains what
you see in the task list on the Job Details page, why tasks sometimes vanish or end
in an unexpected status, and how to confirm what happened.

The short version: **a task being stopped is normal and costs you nothing.** Work is
tracked by keyspace interval, not by task row, so any range a stopped task did not
finish simply re-opens and is re-issued on the next dispatch cycle. No stop recovery
ever produces a `failed` task, and only a `failed` task fails the job.

## Common Questions

### Why did my task disappear from the job's task list?

Because it was stopped before it had made any progress and before it had cracked
anything, so there was nothing worth keeping. The task row and its keyspace interval
are both deleted, the whole original range re-opens, and the next dispatch cycle
re-issues it — usually to a different agent. There is no failure row and no audit
trail entry for a deleted task.

Any of these can stop a task:

- A higher-priority job preempting it
- The chunk-overrun guard cutting short a chunk that was running far longer than its
  target duration
- The agent shutting down gracefully (service stop, restart, or upgrade)
- The agent disconnecting
- The agent missing heartbeats long enough to be evicted

All five take the same path and have the same three possible outcomes — see
[What Happens When a Task Is Stopped](#what-happens-when-a-task-is-stopped).

Stopping a whole job is the exception: it cancels every running and assigned task up
front, before the stop even reaches the agent, so those tasks always end `cancelled`
however much progress they had made. Their keyspace intervals are still truncated at the
restore point, so the work already done is preserved as coverage.

If a job's task list looks shorter than the number of chunks you expected, this is
why. The task rows are not a complete record of every chunk ever dispatched.

### Why does this task say "cancelled"?

A `cancelled` task is usually a stopped task whose row had to be kept. There are a few
ways to get one:

- **The hashlist was fully cracked while the task was still running.** This is by far
  the most common cause, and it shows up on **successfully completed** jobs, not
  cancelled ones. When the last hash falls, every still-active sibling task is stopped
  and marked `cancelled` — their remaining keyspace is moot — and a final reconcile
  cancels anything still non-terminal just before the job is marked `completed`. Nobody
  cancelled anything; the job finished early because there was nothing left to crack.
- **The job or task was cancelled by an operator**, or the whole job was cancelled.
  Stopping a job cancels its running and assigned tasks outright, so an operator stop
  always ends `cancelled`.
- **The task was stopped with no usable progress, but it had already cracked
  hashes.** The row survives so the crack attribution survives with it — cracked
  hashes point back at the task that found them, and features such as loopback
  delta runs join on that link. Deleting the row would orphan those cracks.

In that last case nothing went wrong and no work was lost. The task's keyspace
interval is released and the range is re-dispatched.

### Why did my whole job fail when only one chunk had a problem?

Because one `failed` task fails the entire job, permanently. The check is a
`COUNT(*) > 0` over the job's tasks — if a single row is in `failed`, the job is
failed, and it stays failed even after the re-opened range has been redone
successfully by another agent.

This is exactly why benign stops no longer produce a `failed` task. `failed` is mostly
for failures the **agent reported**: hashcat could not run, a required wordlist or rule
file was missing, the binary was unusable, and so on. **No scheduler-v2 stop recovery
writes `failed`** — not a preemption, not the chunk-overrun guard, not a disconnect, not
a heartbeat eviction, not an operator stop.

There is one server-side exception. A task whose agent keeps vanishing is retried, and
once it has burned through `max_chunk_retry_attempts` (3 by default) the cleanup service
terminalises it as `failed` on its own, with no agent report at all. Three paths do this:
an `assigned`/`running` task that has gone quiet past the timeout, an agent that never
reconnects before its grace period expires, and an agent that reconnects without the task
it was given. Their `error_message` reads like "Agent failed to reconnect after N
attempts" or "Task failed after N retry attempts" — there is no hashcat error to hunt
for, and the fix is the agent's stability or connectivity, not the job.

If a job is failed, find the one task that caused it and read its `error_message` —
see [Checking a job's tasks](#checking-a-jobs-tasks).

### Why didn't my high-priority job interrupt the running one?

Preemption requires **both** gates to be open, plus a non-zero priority:

| Gate | Where | Ships as |
|------|-------|----------|
| `job_interruption_enabled` | System settings, global | Enabled — but **read the toggle rather than assuming it** |
| `allow_high_priority_override` | The waiting job's own setting | Off |

Check the global toggle first. The initial migration seeds it enabled, but the
scheduler's own fallback is the opposite: if the setting row is missing or
unreadable, preemption is treated as **off**, so a configuration problem can never
start stopping running work by accident. Either way the symptom is silent — the
waiting job simply queues instead of interrupting anything.

If the global setting is off, no job preempts anything, regardless of priority. A job
with priority `0` also never preempts, even with both gates open.

Work through the checklist in
[Job Priority System — Job Not Interrupting Lower Priority Work](../admin-guide/advanced/job-priority.md#job-not-interrupting-lower-priority-work).

### My task has been in "processing" for a while

`processing` is a transient state, not an error. It means hashcat has finished and
the agent is streaming its cracked hashes to the server. The task waits there until
every crack batch has been received and verified, so that no crack is lost by
completing the task too early. A task with a large number of cracks legitimately
spends time here.

A background sweep runs every 5 minutes and guarantees a task always leaves this
state. It applies two gates with very different timings:

- If the crack handshake is already satisfied in the database, the sweep completes the
  task immediately, at any age. Only this gate is bounded by the sweep interval, so a
  task that is really finished leaves `processing` within 5 minutes.
- If the remaining cracks are never coming, the sweep terminalises the task as
  `completed` or `cancelled` and releases its keyspace for re-dispatch. This gate is
  age-gated: the row must have sat untouched for 30 minutes (hardcoded, not a setting),
  and while its agent is still heartbeating the gate is deferred further — up to a hard
  cutoff of three times that timeout, or 90 minutes.

The sweep never marks such a task `failed` — deliberately, because that would fail
the whole job over an agent that merely went away. Worst case, a task waiting on cracks
that never arrive sits in `processing` for 90 minutes before the backstop gives up on it.
For the full mechanism see
[Job Completion System — Stale Processing Backstop](../reference/architecture/job-completion-system.md#stale-processing-backstop).

## What Happens When a Task Is Stopped

Every server-initiated stop takes the same path. Which of three outcomes you get
depends only on what the task had produced by the time it was stopped.

| Outcome | Condition | Task row | Keyspace |
|---------|-----------|----------|----------|
| Truncated and completed | hashcat reported a restore point past the task's range start | Marked `completed` at 100% of its new, smaller range | Interval truncated at the restore point; the unprocessed remainder becomes a gap the next dispatch cycle re-issues |
| Deleted | No progress and no cracks | **Row and interval both deleted** | Whole original range re-opens |
| Cancelled | No progress, but the task produced cracks | Marked `cancelled`, row preserved for crack attribution | Interval released; range re-opens |

In all three cases **no work is lost and no work is redone.** The first outcome keeps
the completed prefix of the range and re-issues only the remainder.

There is one further case: if the task's range was **already accounted for** — for
example, the hashlist was fully cracked while the task was still draining its crack
batches — the task ends `completed` and its interval is left untouched.

Stopping the whole job does not go through this branch at all. Every running and assigned
task is cancelled first and only then is the stop sent to the agents, so when the agents'
acknowledgements arrive the recovery path finds terminal rows and leaves them alone. The
interval is still truncated at the restore point, so the work is preserved as coverage —
but the task row reads `cancelled`, never `completed`.

## Task Status Reference

These are the only values `job_tasks.status` can hold; they are enforced by a
database CHECK constraint.

| Status | Meaning |
|--------|---------|
| `pending` | Task has been created but not yet assigned to an agent. |
| `assigned` | Task has been assigned to an agent that has not started it yet. |
| `reconnect_pending` | The task's agent disconnected and the server is waiting out the grace period. The task is not handed to another agent — it is closed out under one of the three stop outcomes above, and its *range* is re-dispatched as a new task. |
| `running` | hashcat is actively processing the task's range on the agent. |
| `processing` | hashcat has finished and the agent is streaming its cracked hashes. Transient; a sweep running every 5 minutes guarantees the task leaves this state, within 90 minutes at the very worst. |
| `completed` | The task finished its range and all crack batches were received. **Also** the outcome of a stopped task that kept its progress, in which case it is 100% of a **truncated** range — `completed` does not imply the task ran its full original range. |
| `failed` | Usually **a failure the agent reported** (hashcat could not run, a required file was missing, and so on). Also written server-side when a task exhausts `max_chunk_retry_attempts` reconnect/heartbeat retries — those rows carry a retry-exhaustion `error_message`, not a hashcat one. One `failed` task permanently fails its entire job. Stop recovery never produces this. |
| `cancelled` | Most often a sibling stopped because the hashlist was fully cracked (on a *completed* job). Also operator cancellation or a job stop, or a benign stop of a task that had cracks but no usable progress. Not an error. |
| *(no row)* | Not a status. A benign stop with nothing to preserve deletes the task row outright, so the task simply is not listed. See [Why did my task disappear?](#why-did-my-task-disappear-from-the-jobs-task-list) |

`processing_error` is **not** a valid status. It was never permitted by the CHECK
constraint, so every attempt to write it failed; the code that tried has been
removed. You will not see it on any task.

## What to Check

### Checking a job's tasks

List every task for a job with its status and progress:

```sql
SELECT id,
       agent_id,
       status,
       detailed_status,
       progress_percent,
       crack_count,
       error_message,
       updated_at
FROM job_tasks
WHERE job_execution_id = 'JOB_UUID'
ORDER BY created_at;
```

Read this alongside the outcomes table above. A short list is expected — deleted
tasks leave no row.

### Checking whether a job has a failed task

This is the check that fails the job. If it returns any row, that row is the reason:

```sql
SELECT id,
       agent_id,
       status,
       error_message,
       last_failure_at
FROM job_tasks
WHERE job_execution_id = 'JOB_UUID'
  AND status = 'failed';
```

An empty result means no task failure is holding the job back, and any missing or
`cancelled` tasks you see are benign stops.

### Checking the preemption gates

```sql
-- Global gate. A missing row is treated as OFF, not on.
SELECT key, value FROM system_settings WHERE key = 'job_interruption_enabled';

-- Per-job gate and priority
SELECT id, name, priority, allow_high_priority_override, status
FROM job_executions
WHERE id = 'JOB_UUID';
```

Both gates must be enabled, and the waiting job's priority must be above `0`.

### Checking a task sitting in "processing"

```sql
SELECT id,
       agent_id,
       expected_crack_count,
       received_crack_count,
       batches_complete_signaled,
       cracking_completed_at,
       updated_at
FROM job_tasks
WHERE job_execution_id = 'JOB_UUID'
  AND status = 'processing';
```

If `received_crack_count` is still climbing, the agent is transmitting normally and
the task will complete on its own. If it is static, the sweep will terminalise the task
once `updated_at` is 30 minutes old — later still, up to 90 minutes, if the agent is
online and heartbeating.

### Backend logs

```bash
docker logs krakenhashes 2>&1 | grep -i "stale-processing sweep"
docker logs krakenhashes 2>&1 | grep -i "keyspace released for re-dispatch"
```

## Related Documentation

- [Glossary — Job and Task Statuses](../reference/glossary.md#job-and-task-statuses)
- [Job Priority System](../admin-guide/advanced/job-priority.md)
- [Database Schema — job_tasks](../reference/database.md#job_tasks)
- [General Troubleshooting Guide](../user-guide/troubleshooting.md)
