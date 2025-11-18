# Job Execution Settings

## Overview

The Job Execution Settings page allows administrators to configure how KrakenHashes executes and distributes password cracking jobs across agents. These settings control chunking behavior, agent coordination, job control, and rule splitting strategies.

## Accessing Job Execution Settings

1. Navigate to the **Admin Panel**
2. Click on **Settings** in the navigation menu
3. Select **Job Execution Settings**

The settings are organized into four main categories for easier management.

## Settings Categories

### Job Chunking

Job chunking divides large password cracking tasks into smaller, manageable pieces that can be distributed across multiple agents. This improves resource utilization and allows for better job scheduling.

| Setting | Description | Default | Range | Notes |
|---------|-------------|---------|--------|-------|
| **Default Chunk Duration** | How long each job chunk should run | 15 minutes | 1+ minutes | Shorter chunks provide more flexibility but increase overhead |
| **Chunk Fluctuation Percentage** | Allowed variance for the final chunk | 10% | 0-100% | Prevents creating very small final chunks |

#### Best Practices for Chunking
- **Short jobs (< 1 hour)**: Use 5-10 minute chunks for better distribution
- **Long jobs (> 24 hours)**: Use 30-60 minute chunks to reduce overhead
- **Mixed agent speeds**: Shorter chunks help balance workload

### Agent Configuration

These settings control how agents behave and interact with the backend server.

| Setting | Description | Default | Range | Notes |
|---------|-------------|---------|--------|-------|
| **Hashlist Retention** | How long agents keep hashlists after job completion | 7 days | 1+ days | Reduces re-download for recurring jobs |
| **Max Concurrent Jobs per Agent** | Maximum jobs an agent can run simultaneously | 1 | 1-10 | Higher values for powerful multi-GPU systems |
| **Progress Reporting Interval** | How often agents send progress updates | 30 seconds | 1+ seconds | Lower values increase server load |
| **Benchmark Cache Duration** | How long to cache agent performance benchmarks | 30 days | 1+ days | Reduces benchmark frequency |
| **Speedtest Timeout** | Maximum time to wait for speedtest completion | 30 seconds | 60-600 seconds | Increase for slower systems |
| **Reconnect Grace Period** | Time to wait for agents to reconnect after server restart | 5 minutes | 1-60 minutes | Prevents unnecessary task reassignment |

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
| **Allow Job Interruption** | Higher priority jobs can interrupt running jobs | Enabled | On/Off | Ensures critical jobs run immediately |
| **Agent Overflow Allocation Mode** | How to distribute extra agents when jobs hit max_agents limit | `fifo` | `fifo`, `round_robin` | Controls fairness vs speed tradeoff |
| **Real-time Crack Notifications** | Send notifications when hashes are cracked | Enabled | On/Off | Can increase server load for large jobs |
| **Job Refresh Interval** | How often the UI refreshes job status | 5 seconds | 1-60 seconds | Lower values increase server load |
| **Max Chunk Retry Attempts** | Number of times to retry failed chunks | 3 | 0-10 | Set to 0 to disable retries |
| **Jobs Per Page** | Default pagination size for job lists | 25 | 5-100 | Adjust based on UI preferences |

#### Job Interruption Behavior
When enabled, the system will:
1. Pause lower priority jobs when higher priority jobs arrive
2. Save the state of interrupted jobs
3. Resume interrupted jobs once higher priority jobs complete
4. Maintain crack progress for all interrupted jobs

#### Agent Overflow Allocation Mode

This setting controls how "overflow" agents (agents beyond max_agents limits) are distributed among jobs at the **same priority level**.

**Important**: This setting **only applies** to overflow agents when multiple jobs exist at the same priority. Higher priority jobs always override max_agents limits and take ALL available agents.

**Available Modes:**

##### FIFO Mode (Default)

**Behavior**: Oldest job gets all overflow agents

```sql
-- Set FIFO mode
UPDATE system_settings
SET value = 'fifo'
WHERE key = 'agent_overflow_allocation_mode';
```

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

```sql
-- Set round-robin mode
UPDATE system_settings
SET value = 'round_robin'
WHERE key = 'agent_overflow_allocation_mode';
```

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

**Critical**: The overflow allocation mode **does not affect** priority-based allocation:

```
Scenario: 2 jobs, 10 agents available

Job A: Priority 100, max_agents = 3
Job B: Priority 50,  max_agents = 5

Result (regardless of overflow mode):
- Job A gets ALL 10 agents (higher priority overrides max_agents)
- Job B gets 0 agents (lower priority, waits for Job A to finish)
```

##### Configuration via SQL

```sql
-- View current setting
SELECT key, value, description
FROM system_settings
WHERE key = 'agent_overflow_allocation_mode';

-- Change to FIFO mode (default)
UPDATE system_settings
SET value = 'fifo'
WHERE key = 'agent_overflow_allocation_mode';

-- Change to round-robin mode
UPDATE system_settings
SET value = 'round_robin'
WHERE key = 'agent_overflow_allocation_mode';
```

##### When to Use Each Mode

| Scenario | Recommended Mode | Reason |
|----------|------------------|--------|
| Production cracking (default) | FIFO | Finish jobs faster by concentrating resources |
| Multiple test jobs | Round-robin | See results from all tests simultaneously |
| Multi-client environment | Round-robin | Fair distribution across clients |
| Single large job | Either | No difference (only one job) |
| Time-critical job | FIFO | Ensures oldest/most important finishes first |
| Parallel research | Round-robin | Compare multiple approaches simultaneously |

### Rule Splitting

Rule splitting automatically divides large rule files to improve distribution across agents. This is especially useful for rule files that would otherwise exceed the chunk duration.

| Setting | Description | Default | Range | Notes |
|---------|-------------|---------|--------|-------|
| **Enable Rule Splitting** | Automatically split large rule files | Enabled | On/Off | Improves distribution for large rule sets |
| **Rule Split Threshold** | Split when estimated time exceeds chunk duration by this factor | 2.0× | 1.1-10× | Lower values create more chunks |
| **Minimum Rules to Split** | Don't split files with fewer rules than this | 100 | 10+ | Prevents splitting small files |
| **Maximum Rule Chunks** | Maximum chunks to create per rule file | 100 | 2-10000 | Limits memory usage |
| **Rule Chunk Directory** | Directory for temporary rule chunks | `/tmp/rule_chunks` | Any valid path | Must be writable by backend |

#### Rule Splitting Algorithm
The system automatically:
1. Estimates job duration based on hashlist size and rule count
2. Compares estimated duration to chunk duration × threshold
3. If exceeding threshold, splits rules into appropriate chunks
4. Distributes chunks across available agents
5. Cleans up temporary chunks after job completion

## Performance Considerations

### Network Load
- **Progress Reporting Interval**: Each update creates network traffic
- **Job Refresh Interval**: Affects UI responsiveness and server load
- Calculate: `(Number of Agents × Active Jobs) / Reporting Interval = Updates per second`

### Storage Requirements
- **Hashlist Retention**: `Average Hashlist Size × Number of Unique Jobs × Retention Days`
- **Rule Chunks**: `Original Rule File Size × Active Jobs using that rule`
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
- Enable **Rule Splitting** for large rule files
- Adjust **Chunk Fluctuation Percentage** to avoid tiny chunks

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