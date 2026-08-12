# Job Priority and Interruption System

## Overview

KrakenHashes implements a sophisticated priority system that ensures critical password auditing tasks receive the resources they need. The system supports priority-based scheduling, automatic job interruption, and intelligent resource allocation to optimize your password cracking operations.

## Priority System Fundamentals

### Priority Scale

Job priority is an integer from **0** up to the `max_job_priority` system setting, which **defaults
to 1000**. Admins can raise or lower that ceiling (values above 1,000,000 are rejected outright), and
job and preset-job creation validates against whatever it is currently set to.

Priority is a pure ordering key — a higher number is served first — so only the bands *your*
deployment agrees on matter, not the absolute numbers. The convention below fits a 0-100 working
range; scale it if you raise the ceiling:

- **Critical Priority (90-100)**: Emergency response, security incidents
- **High Priority (70-89)**: Time-sensitive audits, compliance deadlines
- **Normal Priority (40-69)**: Standard security assessments
- **Low Priority (10-39)**: Background processing, research tasks
- **Minimal Priority (0-9)**: Non-urgent, opportunistic processing

### How Priority Affects Job Execution

1. **Job Selection Order**: Higher priority jobs are assigned to agents first
2. **Resource Allocation**: High priority jobs can use more agents simultaneously
3. **Queue Position**: Within the same priority level, jobs follow FIFO (First-In-First-Out)
4. **Interruption Rights**: Jobs with high priority override can interrupt lower priority running jobs

!!! note "How allocation works under the hood"
    The exact way surplus agents are distributed across same-priority jobs is governed by the
    `agent_overflow_allocation_mode` setting, which offers five modes (Priority FIFO/Round-Robin and
    Max-Agents FIFO/Round-Robin/strict). See
    [Scheduler v2 Overview — Agent allocation and overflow modes](../../reference/architecture/scheduler-v2-overview.md#agent-allocation-and-overflow-modes)
    for the full model.

## High Priority Override Feature

### What is High Priority Override?

The high priority override feature allows critical jobs to interrupt lower priority jobs that are currently running. This ensures that urgent tasks don't have to wait for long-running, low-priority jobs to complete.

### When to Enable High Priority Override

Enable this feature for jobs that:
- Respond to active security incidents
- Have strict compliance deadlines
- Require immediate results for business-critical decisions
- Support time-sensitive investigations

### How It Works

1. **Trigger Condition**: Interruption only occurs when **all** of the following hold:
   - The system-wide **Job Interruption Enabled** setting is on
   - The waiting job has **Allow High Priority Override** enabled — this is a per-job opt-in, so a
     job without it never stops anyone's work no matter how high its priority
   - The waiting job's priority is **greater than 0**. A priority-0 job is never counted as starving
     and so never preempts anything — it sits at the bottom of the queue, where by definition there
     is nothing lower to take an agent from
   - The waiting job got no agents this scheduling cycle and is not already at its own `max_agents`
     cap (a job blocked by its own cap is not starving — freeing agents wouldn't help it)
   - A compatible lower-priority task is currently running

2. **Interruption Process**:
   - The system picks the *newest* running task at the *lowest* priority — the one with the least
     invested progress to give up
   - Sends a stop command to the agent working that task
   - When the agent responds, the stopped task's keyspace range is **truncated at its last restore
     point** and the task is closed out as completed for the work it actually did — or, if it never
     got that far, released entirely (see [Status Transitions](#status-transitions) and
     [What happens to interrupted jobs](#what-happens-to-interrupted-jobs) below)
   - Assigns the freed agent to the high-priority job on the next cycle

3. **Automatic Resumption**:
   - The interrupted job's unfinished range returns to the queue as undispatched work and is picked
     up again as soon as an agent is free
   - No progress is lost and no keyspace is re-run
   - No manual intervention required

### Configuration

To enable high priority override for a preset job:

1. Navigate to **Jobs → Preset Jobs → [Job Name]**
2. In the Advanced Settings section, toggle **"Allow High Priority Override"**
3. Set an appropriate priority level (typically 70+)
4. Save the preset job

## Job Interruption Behavior

### Status Transitions

Interruption happens at the **task** level, not the job level. The job keeps running as a queue
entry; only the specific chunk on the freed agent is stopped.

**The stopped task** ends in one of three states, decided by how far it got and whether it cracked
anything — never `failed`:

| What the task had | Where it ends up | What happens to its keyspace |
|---|---|---|
| A restore point past its range start | `completed`, with its range **truncated** to the point actually reached (so it reads 100% of a smaller range) | The processed part stays recorded as coverage; the remainder re-opens as a gap |
| No progress, no cracks | **Deleted** — the task row disappears from the job's task list | The whole original range re-opens |
| No progress, but it produced cracks | `cancelled` — the row survives so its crack attribution does too | The whole original range re-opens |

A stopped task is **never** returned to `pending`, and never marked `failed`. `failed` is reserved
for failures the *agent reports*: `HasFailedTasks` is a `COUNT(*) > 0`, so a single `failed` row
permanently fails the entire job — which is exactly why a job that stopped as designed must not leave
one behind.

**The job** stays `running` if it still has other tasks in flight; otherwise it goes back to
`pending` and is re-dispatched as soon as an agent is free.

For the complete task-status picture, including the states that have nothing to do with preemption,
see [Task Lifecycle & Statuses](../../troubleshooting/task-lifecycle.md).

### What Happens to Interrupted Jobs?

1. **Progress Preserved**: The stopped chunk is truncated at the last restore point the agent
   reported. Everything up to that point is permanently recorded as completed keyspace. If there was
   no restore point, nothing was preserved because nothing had been searched — the range simply
   re-opens whole.
2. **Remainder Re-queued**: The unfinished part of the chunk becomes an undispatched gap in the
   job's keyspace and is handed out again on a later cycle — often to a different agent.
3. **No Work Repeated**: Because the range is tracked as an interval set rather than a single
   progress watermark, only the untouched remainder is re-run. A hole left in the middle of the
   keyspace is re-issued **before** the untouched tail.
4. **Agent Cleanup**: The agent releases its resources and becomes available for the high-priority
   job on the next cycle.

This is the same mechanism used for every other stop reason — the
[chunk overrun guard](../operations/job-settings.md#chunk-overrun-guard), agent disconnects,
heartbeat timeouts, and operator stops all truncate-and-re-open in exactly this way.

### System-Wide Interruption Control

Interruption is gated in **three** places, and all three must be open:

| Gate | Where | Effect when closed |
|------|-------|--------------------|
| **Job Interruption Enabled** (`job_interruption_enabled`) | Admin Panel → Settings → Job Execution Settings | No job interrupts anything, regardless of priority |
| **Allow High Priority Override** | Per preset job (Advanced Settings) | *That* job never interrupts anything, regardless of its priority |
| **Priority > 0** | Per job/preset job | A priority-0 job is never counted as starving, so it never preempts |

**Check the global toggle rather than assuming it.** The database migration seeds the
`job_interruption_enabled` row `true`, but the scheduler's own fallback is the opposite: if the
setting is missing or unreadable, it treats it as **off** — a fail-safe, so a configuration problem
can never cause running work to be stopped. Anything that leaves preemption disabled is silent by
design: the job simply waits, exactly as it would if no lower-priority work existed. If a
high-priority job is not interrupting anything, confirm the toggle's current value in **Admin Panel →
Settings → Job Execution Settings** before looking anywhere else.

When interruption is off (any gate):
- No running tasks are stopped to make room
- High priority jobs still get first claim on every agent that becomes free — priority always
  decides *allocation*, the gates only control *preemption*
- Effectively, jobs wait their turn instead of taking a turn away from someone else

## Best Practices

### Setting Appropriate Priorities

#### Security Incident Response (Priority: 95-100)
```
Priority: 100
Allow High Priority Override: Yes
Max Agents: Unlimited
Reason: Immediate threat mitigation required
```

#### Compliance Audit - Due Today (Priority: 80-90)
```
Priority: 85
Allow High Priority Override: Yes
Max Agents: 10
Reason: Regulatory deadline approaching
```

#### Weekly Security Assessment (Priority: 50-60)
```
Priority: 55
Allow High Priority Override: No
Max Agents: 5
Reason: Routine scheduled assessment
```

#### Research Project (Priority: 10-20)
```
Priority: 15
Allow High Priority Override: No
Max Agents: 2
Reason: Long-term analysis, no deadline
```

### Priority Strategy Guidelines

1. **Reserve High Priorities**: Don't use high priorities for routine tasks
2. **Consider Business Impact**: Align priority with actual business urgency
3. **Plan for Interruptions**: Design workflows assuming possible interruptions
4. **Monitor Resource Usage**: Track how priority affects overall throughput
5. **Document Priority Decisions**: Maintain a priority assignment guide for your team

### Avoiding Priority Inflation

To prevent "priority creep" where all jobs become high priority:

1. **Establish Clear Criteria**: Document what qualifies for each priority level
2. **Regular Review**: Audit priority assignments monthly
3. **Default to Normal**: Start with priority 50 unless justified otherwise
4. **Limit Override Usage**: Only enable override for truly critical jobs

## Priority in Workflows

### Workflow Priority Inheritance

When jobs are created from workflows:
1. Each preset job maintains its configured priority
2. Jobs execute in priority order within the workflow
3. Higher priority jobs from other workflows can interleave

### Example Workflow Priority Design

```
Emergency Response Workflow:
├── Quick Dictionary (Priority: 95)
├── Common Patterns (Priority: 90)
├── Extended Dictionary (Priority: 85)
└── Brute Force Backup (Priority: 80)

Standard Audit Workflow:
├── Leaked Passwords (Priority: 60)
├── Company Variations (Priority: 55)
├── Rule-Based Attack (Priority: 50)
└── Comprehensive Check (Priority: 45)
```

## Monitoring Priority Impact

### Key Metrics to Track

1. **Interruption Frequency**: How often jobs are interrupted
2. **Wait Time by Priority**: Average wait time per priority level
3. **Completion Time Impact**: Effect of interruptions on job completion
4. **Resource Utilization**: Agent usage across priority levels

### Identifying Issues

Watch for these warning signs:
- Frequent interruptions of the same job
- Low priority jobs never completing
- All jobs set to high priority
- Agents constantly switching between jobs

## Advanced Scenarios

### Multi-Tenant Environments

For systems serving multiple teams or clients:

1. **Priority Ranges per Tenant**: Assign priority bands to each tenant
2. **Fair Resource Sharing**: Implement quotas alongside priorities
3. **Override Restrictions**: Limit override capability to specific roles

### Scheduled Priority Changes

For jobs that change priority over time:

1. **Escalation**: Increase priority as deadlines approach
2. **De-escalation**: Reduce priority after peak hours
3. **Time-Based Rules**: Automate priority adjustments based on schedule

### Emergency Override Procedures

For critical incidents requiring immediate resources:

1. **Emergency Priority (100)**: Reserved for security incidents
2. **Administrative Override**: Allow admins to force interrupt any job
3. **Audit Trail**: Log all emergency overrides for review

## Troubleshooting

### Job Not Interrupting Lower Priority Work

Check, in this order — the first two are silent when off, so they account for most reports:

1. Is **Job Interruption Enabled** actually on right now in Admin Panel → Settings → Job Execution
   Settings? Read the toggle; don't infer it from the shipped default
2. Does the job have **Allow High Priority Override** enabled? It is a per-job opt-in
3. Is the job's priority **greater than 0**? A priority-0 job never preempts
4. Is the job already at its own `max_agents` cap? A job capped by its own limit is not treated as
   starving, because freeing an agent would not let it take one
5. Are there actually lower-priority tasks running, on agents **compatible** with this job's binary
   version? An incompatible victim is not a candidate — see
   [Binary Version Patterns](../../reference/architecture/binary-version-patterns.md)
6. Was a victim already told to stop on an earlier cycle? Tasks with a stop in flight are excluded,
   so preemption can look like it did nothing for a cycle or two

For what the stopped task should look like afterwards, and every other status a task can end in, see
[Task Lifecycle & Statuses](../../troubleshooting/task-lifecycle.md).

### Interrupted Job Not Resuming

Interruption is task-level, so the job itself usually stays `running`; "resuming" means its re-opened
gap gets dispatched again. Verify:

1. The stopped task ended as expected — `completed` on a truncated range, `cancelled`, or gone
   entirely. A `failed` task is a different problem: it means the *agent* reported a failure, and one
   such row fails the whole job
2. The job's remaining keyspace shows as uncovered work (a gap), not as fully covered
3. Agents are available, online, and compatible with the job's binary version
4. No higher-priority job is consuming every agent

### Excessive Interruptions

Solutions:
1. Review and adjust priority assignments
2. Increase agent capacity
3. Implement scheduling to reduce contention
4. Consider priority bands to limit interruption cascades

## Performance Considerations

### Impact on System Performance

- **Minimal Overhead**: Priority checks are lightweight
- **Interruption Cost**: ~5-10 seconds to stop and reassign
- **Progress Tracking**: Checkpoint frequency affects resumption granularity

### Optimizing for Priority Systems

1. **Appropriate Chunk Sizes**: Smaller chunks (5-10 minutes) for better interruption response
2. **Checkpoint Frequency**: Balance between progress saving and performance
3. **Agent Pool Size**: More agents reduce need for interruptions
4. **Priority Distribution**: Spread priorities to reduce conflicts

## Integration with Other Features

### Agent Scheduling

Priority system works with agent scheduling:
- Scheduled agents only available during defined hours
- Priority determines job selection within available windows
- Interruptions respect scheduling boundaries

### Max Agents Limits

Priority interacts with max agent settings:
- High priority jobs reach max agents first
- Lower priority jobs use remaining capacity
- Override can free agents even from max-agent-limited jobs

### Resource Management

Priority affects resource allocation:
- File sync prioritizes high-priority job requirements
- Binary selection considers job priority
- Wordlist/rule loading optimized for high-priority jobs

## Summary

The KrakenHashes priority and interruption system provides powerful tools for managing competing password auditing demands. By understanding and properly configuring priorities, you can ensure critical tasks complete quickly while maintaining efficient resource utilization across all jobs.

Key takeaways:
- Use priority levels that reflect actual business urgency
- Enable high priority override only for critical jobs
- Monitor interruption patterns to optimize settings
- Design workflows with priority strategies in mind
- Maintain clear documentation of priority policies