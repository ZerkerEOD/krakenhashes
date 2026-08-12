# Task Lifecycle and Statuses

A job's keyspace is dispatched to agents as tasks (chunks). This page explains what
you see in the task list on the Job Details page, why tasks sometimes vanish or end
in an unexpected status, and how to confirm what happened.

The short version: **a task being stopped is normal and costs you nothing.** Work is
tracked by keyspace interval, not by task row, so any range a stopped task did not
finish simply re-opens and is re-issued on the next dispatch cycle. Only an
agent-*reported* failure produces a `failed` task, and only a `failed` task fails the
job.

## Common Questions

### Why did my task disappear from the job's task list?

Because it was stopped before it had made any progress and before it had cracked
anything, so there was nothing worth keeping. The task row and its keyspace interval
are both deleted, the whole original range re-opens, and the next dispatch cycle
re-issues it — usually to a different agent. There is no failure row and no audit
trail entry for a deleted task.

Any of these can stop a task:

- An operator pressing **Stop Job**
- A higher-priority job preempting it
- The chunk-overrun guard cutting short a chunk that was running far longer than its
  target duration
- The agent shutting down gracefully (service stop, restart, or upgrade)
- The agent disconnecting
- The agent missing heartbeats long enough to be evicted

All six take the same path and have the same three possible outcomes — see
[What Happens When a Task Is Stopped](#what-happens-when-a-task-is-stopped).

If a job's task list looks shorter than the number of chunks you expected, this is
why. The task rows are not a complete record of every chunk ever dispatched.

### Why does this task say "cancelled"?

A `cancelled` task is a stopped task whose row had to be kept. There are two ways to
get one:

- **The job or task was cancelled by an operator**, or the whole job was cancelled.
- **The task was stopped with no usable progress, but it had already cracked
  hashes.** The row survives so the crack attribution survives with it — cracked
  hashes point back at the task that found them, and features such as loopback
  delta runs join on that link. Deleting the row would orphan those cracks.

In the second case nothing went wrong and no work was lost. The task's keyspace
interval is released and the range is re-dispatched.

### Why did my whole job fail when only one chunk had a problem?

Because one `failed` task fails the entire job, permanently. The check is a
`COUNT(*) > 0` over the job's tasks — if a single row is in `failed`, the job is
failed, and it stays failed even after the re-opened range has been redone
successfully by another agent.

This is exactly why benign stops no longer produce a `failed` task. `failed` is
reserved for failures the **agent reported**: hashcat could not run, a required
wordlist or rule file was missing, the binary was unusable, and so on. A disconnect,
a preemption, or an operator stop never lands here.

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
state:

- If the crack handshake is already satisfied, the sweep completes the task
  immediately.
- If the agent is gone and the remaining cracks are never coming, the sweep
  terminalises the task as `completed` or `cancelled` and releases its keyspace for
  re-dispatch.

The sweep never marks such a task `failed` — deliberately, because that would fail
the whole job over an agent that merely went away. Worst case, a task sits in
`processing` for one sweep interval.

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

## Task Status Reference

These are the only values `job_tasks.status` can hold; they are enforced by a
database CHECK constraint.

| Status | Meaning |
|--------|---------|
| `pending` | Task has been created but not yet assigned to an agent. |
| `assigned` | Task has been assigned to an agent that has not started it yet. |
| `reconnect_pending` | The task's agent disconnected and the server is waiting out the grace period. The task is not handed to another agent — it is closed out under one of the three stop outcomes above, and its *range* is re-dispatched as a new task. |
| `running` | hashcat is actively processing the task's range on the agent. |
| `processing` | hashcat has finished and the agent is streaming its cracked hashes. Transient; a 5-minute sweep guarantees the task leaves this state. |
| `completed` | The task finished its range and all crack batches were received. **Also** the outcome of a stopped task that kept its progress, in which case it is 100% of a **truncated** range — `completed` does not imply the task ran its full original range. |
| `failed` | **The agent reported a failure** (hashcat could not run, a required file was missing, and so on). One `failed` task permanently fails its entire job. Benign stops never produce this. |
| `cancelled` | Operator cancellation, or a benign stop of a task that had cracks but no usable progress. Not an error. |
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
the task will complete on its own. If it is static and the agent is offline, the
sweep will terminalise the task within its next interval.

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
