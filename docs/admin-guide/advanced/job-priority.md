# Job Priority and Interruption System

## Overview

KrakenHashes implements a sophisticated priority system that ensures critical password auditing tasks receive the resources they need. The system supports priority-based scheduling, automatic job interruption, and intelligent resource allocation to optimize your password cracking operations.

## Priority System Fundamentals

### Priority Scale

Jobs in KrakenHashes use a priority scale from 0 to 100:

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

1. **Trigger Condition**: Interruption only occurs when:
   - No agents are available for assignment
   - A high-priority job with override enabled is waiting
   - Lower priority jobs are currently running

2. **Interruption Process**:
   - System identifies the lowest priority running job
   - Sends stop command to agents working on that job
   - Moves interrupted job to "pending" status (not paused)
   - Assigns freed agents to the high-priority job

3. **Automatic Resumption**:
   - Interrupted jobs automatically resume when agents become available
   - Jobs maintain their progress and continue from where they stopped
   - No manual intervention required

### Configuration

To enable high priority override for a preset job:

1. Navigate to **Jobs → Preset Jobs → [Job Name]**
2. In the Advanced Settings section, toggle **"Allow High Priority Override"**
3. Set an appropriate priority level (typically 70+)
4. Save the preset job

## Job Interruption Behavior

### Status Transitions

When a job is interrupted:
- **Before**: Status = "running"
- **During Interruption**: Status changes to "pending"
- **After Resumption**: Status returns to "running"

### What Happens to Interrupted Jobs?

1. **Progress Preserved**: All completed work is saved
2. **Automatic Queue Return**: Job returns to pending queue with same priority
3. **Smart Resumption**: Job continues from last checkpoint, no work repeated
4. **Agent Cleanup**: Agents properly release resources and become available

### System-Wide Interruption Control

Administrators can enable or disable job interruption globally:

1. Navigate to **Admin Panel → System Settings**
2. Find **"Job Interruption Enabled"** setting
3. Toggle to enable/disable interruption system-wide

When disabled:
- No jobs will be interrupted regardless of priority
- High priority jobs wait in queue normally
- System operates in strict FIFO mode within priority levels

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

Check:
1. Is "Job Interruption Enabled" in system settings?
2. Does the job have "Allow High Priority Override" enabled?
3. Are there actually lower priority jobs running?
4. Do the running jobs allow interruption?

### Interrupted Job Not Resuming

Verify:
1. Job status is "pending" not "failed"
2. Agents are available and online
3. No higher priority jobs in queue
4. Job hasn't exceeded retry limits

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