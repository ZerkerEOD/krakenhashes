# Benchmark-Based Job Assignment Workflow

## Overview

The job scheduling service now implements a benchmark-first approach for job assignment. Before assigning work to an agent, the system verifies that the agent has a valid benchmark for the specific attack mode and hash type combination.

## Workflow

1. **Job Assignment Request**
   - Scheduler identifies an available agent and a pending job
   - Job execution details are retrieved, including the hashlist

2. **Benchmark Check**
   - System checks if agent has a benchmark for the attack mode and hash type
   - If benchmark exists, checks if it's still valid (default: 7 days cache)
   - Cache duration can be configured via `benchmark_cache_duration_hours` setting

3. **Benchmark Request (if needed)**
   - If no valid benchmark exists, system sends enhanced benchmark request
   - Request includes actual job configuration:
     - Binary version pattern (resolved via Agent Pattern → Job Pattern → Default hierarchy)
     - Wordlists and rules (if applicable)
     - Mask (for brute force attacks)
     - Hash type and attack mode
     - Test duration (30 seconds)
   - Binary version pattern is resolved using this hierarchy:
     1. Agent's binary version pattern (from agent settings)
     2. Job execution's binary version pattern
     3. System default binary
   - Job assignment is deferred until benchmark completes

4. **Benchmark Execution (Agent side)**
   - Agent receives benchmark request with full job configuration
   - Runs actual hashcat benchmark with the specific parameters
   - Reports back real-world performance metrics

5. **Job Assignment (after benchmark)**
   - Once benchmark is received and stored, agent becomes available again
   - Next scheduling cycle will find the valid benchmark
   - Chunk calculation uses accurate performance data
   - Job task is assigned with properly sized chunks

## Benefits

- **Accurate Performance Estimation**: Benchmarks use actual job configuration
- **Optimal Chunk Sizing**: Prevents under/over-utilization of agents
- **Reduced Job Failures**: Avoids assigning work that agents can't handle
- **Better Resource Utilization**: Chunks are sized based on real performance

## Configuration

- `benchmark_cache_duration_hours`: How long benchmarks remain valid (default: 168 hours / 7 days)
- `chunk_fluctuation_percentage`: Tolerance for final chunk size variations (default: 20%)
- `default_chunk_duration`: Target duration for each chunk in seconds (default: 1200 / 20 minutes)

## Implementation Details

### Key Components

1. **JobSchedulingService** - Creates benchmark plans and coordinates parallel execution
2. **JobWebSocketIntegration** (`RequestAgentBenchmark`) - Sends benchmark requests with full job configuration
3. **BenchmarkRequestPayload** - Enhanced WebSocket type with job-specific fields
4. **Binary Version Resolution** - Uses `DetermineBinaryForTask()` to resolve patterns (Agent Pattern → Job Pattern → Default)

### Error Handling

- Missing benchmarks trigger requests instead of failures
- Invalid benchmarks are detected and refreshed
- WebSocket unavailability is properly handled
- Graceful degradation if benchmark request fails

## Accurate Keyspace Tracking

In addition to benchmarking for performance estimation, the system captures accurate keyspace values from hashcat to ensure precise progress tracking.

### Why Accurate Keyspace Tracking?

When using rules or combination attacks, estimating the total keyspace can be inaccurate. For example:
- **Rule-based attacks**: Estimated keyspace = wordlist_size × rule_count, but actual keyspace varies based on rule effectiveness
- **Combination attacks**: Certain combinations may be invalid or duplicates

Hashcat provides the actual keyspace through `progress[1]` values, which the system captures to ensure accurate progress reporting.

### Keyspace Capture Workflow

1. **Initial Job Creation**
   - Job created with estimated `effective_keyspace` based on wordlists/rules
   - Flag `is_accurate_keyspace` set to `false`
   - Estimation needed for rule splitting decisions

2. **Forced Benchmark for First Agent**
   - When first agent connects (taskCount = 0), system requests benchmark
   - Benchmark includes actual job configuration (wordlists, rules, mask, hash type)
   - Agent runs hashcat benchmark and captures `progress[1]` value

3. **Accurate Keyspace Capture**
   - Backend receives benchmark result with `TotalEffectiveKeyspace` from `progress[1]`
   - Updates job execution:
     - Sets `effective_keyspace` to actual value from hashcat
     - Sets `is_accurate_keyspace` to `true`
     - Calculates `avg_rule_multiplier` = actual / estimated
   - Subsequent agents skip benchmark and use cached job-level keyspace

4. **Fallback: First Progress Update**
   - If benchmark doesn't provide keyspace, first task progress update does
   - Agent sends `progress[1]` value in first progress message with `IsFirstUpdate` flag
   - Backend updates both job-level and task-level keyspace
   - Sets `is_actual_keyspace` to `true` for the task

5. **Future Task Improvements**
   - New tasks use `avg_rule_multiplier` to improve estimated keyspace
   - Provides better estimates for chunks not yet processed
   - Helps with more accurate progress reporting across the job

### Benefits of Accurate Keyspace Tracking

- **Precise Progress**: Progress percentages reflect actual hashcat progress, not estimates
- **Better Task Distribution**: Chunk sizes calculated based on real keyspace
- **Improved Estimates**: Future tasks benefit from multiplier derived from actual values
- **Consistency**: All agents working on same job use same accurate keyspace

### Database Columns

**job_executions table:**
- `is_accurate_keyspace` (boolean): True when keyspace is from hashcat `progress[1]`
- `avg_rule_multiplier` (float): Ratio of actual/estimated keyspace for improving future estimates

**job_tasks table:**
- `is_actual_keyspace` (boolean): True when task has actual keyspace from progress update

## Hashlist Download Strategy for Benchmarks

### Always-Fresh Hashlist Downloads

To ensure accurate keyspace calculations, benchmarks ALWAYS download a fresh copy of the hashlist from the backend, even if a local copy exists. This prevents stale hash counts from affecting keyspace estimates.

**Why This Matters**:
- Hashlists change as hashes are cracked and files are regenerated
- Benchmark keyspace must reflect the CURRENT number of uncracked hashes
- Stale local copies can lead to incorrect `effective_keyspace` values
- Cross-hashlist crack propagation means files update frequently

**Implementation**:
```go
// Agent removes existing hashlist before benchmark
if _, err := os.Stat(localPath); err == nil {
    debug.Info("Removing existing hashlist to download fresh copy for benchmark")
    os.Remove(localPath)
}

// Download fresh copy from backend
fileInfo := &filesync.FileInfo{
    Name:     fmt.Sprintf("%d.hash", hashlistID),
    FileType: "hashlist",
    ID:       int(hashlistID),
    MD5Hash:  "", // Skip verification for speed
}
c.fileSync.DownloadFileFromInfo(ctx, fileInfo)
```

**Benchmark Workflow with Fresh Download**:
1. Backend requests benchmark for job execution
2. Agent receives benchmark request with hashlist ID
3. Agent deletes any existing local hashlist file
4. Agent downloads current version from backend (may be empty if all cracked)
5. Agent runs hashcat benchmark with fresh hashlist
6. Agent reports actual keyspace from `progress[1]`

**Benefits**:
- Keyspace values always accurate
- Benchmarks work correctly even after massive crack batches
- Prevents "empty hashlist" errors from hashcat
- Consistent behavior across all agents

**Performance Impact**:
- Minimal: Hashlists are typically < 10 MB
- Download completes in seconds over LAN
- Only occurs once per job (first agent)
- Subsequent agents use job-level cached keyspace

### Task Execution Strategy

Similar to benchmarks, job tasks also ALWAYS re-download hashlists:

**Rationale**:
- Ensures consistent behavior between benchmarks and tasks
- Prevents agents from working with stale data
- Handles cross-hashlist crack propagation automatically
- Eliminates edge cases with modified local files

**Implementation**:
```go
// Agent ensures fresh hashlist for each task
if _, err := os.Stat(localPath); err == nil {
    debug.Info("Removing existing hashlist to download fresh copy")
    os.Remove(localPath)
}

// Download current version
s.fileSync.DownloadFileFromInfo(ctx, fileInfo)
```

**Trade-offs**:
- Slightly higher network usage
- Guaranteed data freshness
- Simplified agent logic (no staleness checks)
- Better fault tolerance

## Parallel Benchmark Execution System

### Overview

The job scheduling service implements an intelligent parallel benchmarking system that dramatically improves benchmark completion time by executing all benchmark requests simultaneously.

**Performance Improvement:**
- **Before (Sequential)**: 15 agents × 30s = 450 seconds total
- **After (Parallel)**: 15 agents in ~12 seconds
- **Result**: 96% reduction in benchmark time (37.5x faster)

### Architecture

The parallel benchmarking system consists of three main components:

#### 1. Benchmark Planning (`job_scheduling_benchmark_planning.go`)

**Core Functions:**
- `CreateBenchmarkPlan()`: Analyzes system state and creates intelligent execution plan
- `ExecuteBenchmarkPlan()`: Sends all benchmarks in parallel using goroutines
- `WaitForBenchmarks()`: Polls database for completion with configurable timeout
- `PrioritizeForcedBenchmarkAgents()`: Gives priority to agents for job's first task

**Planning Algorithm:**
1. Identifies jobs needing benchmarks (taskCount = 0, no accurate keyspace)
2. Identifies agents needing speed benchmarks (missing hash_type/attack_mode combinations)
3. Distributes benchmark requests using round-robin allocation by priority
4. Respects benchmark cache duration (system setting: `benchmark_cache_duration_hours`)

#### 2. Benchmark Requests Table (Migration 083)

The `benchmark_requests` table enables polling-based coordination of async WebSocket benchmarks:

```sql
CREATE TABLE benchmark_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    job_execution_id UUID REFERENCES job_executions(id) ON DELETE CASCADE,
    hash_type INTEGER NOT NULL,
    attack_mode INTEGER NOT NULL,
    benchmark_type VARCHAR(50) NOT NULL,  -- 'forced' or 'agent_speed'
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    requested_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMP WITH TIME ZONE,
    result JSONB,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

**Purpose:**
- Tracks pending benchmark requests
- Enables blocking wait for completion
- Supports cleanup after each scheduling cycle
- Allows forced benchmark agent prioritization

#### 3. WebSocket Integration

**Enhanced HandleBenchmarkResult():**
- Updates `benchmark_requests` table on completion
- Sets forced benchmark completion metadata for prioritization
- Maintains compatibility with existing keyspace tracking
- Updates both job-level and agent-level benchmark data

### Benchmark Types

The system supports two types of benchmarks:

#### Forced Benchmarks
- **Purpose**: Obtain accurate keyspace from hashcat for new jobs
- **Trigger**: Job with taskCount = 0 and `is_accurate_keyspace = false`
- **Behavior**: Runs full hashcat benchmark with actual job configuration
- **Result**: Updates `job_executions.effective_keyspace` with `progress[1]` value
- **Priority**: Agents completing forced benchmarks get first task for their job

#### Agent Speed Benchmarks
- **Purpose**: Update agent performance metrics for chunk calculations
- **Trigger**: Missing agent benchmark for hash_type + attack_mode combination
- **Behavior**: Standard hashcat speed test
- **Result**: Updates `agent_benchmarks` table
- **Duration**: Uses `speedtest_timeout_seconds` system setting

### Salt-Aware Benchmark Caching

For hash types that use per-hash salts (e.g., NetNTLMv2, bcrypt, scrypt), benchmark caching includes the **salt count** as an additional cache key dimension.

#### Why Salt Count Matters

Hashcat reports speed differently for salted vs non-salted hashes:

- **Non-salted hashes**: Speed = candidate throughput (e.g., 1 GH/s means 1 billion candidates/sec)
- **Salted hashes**: Speed = hash operations (e.g., 1 GH/s means candidate_rate × salt_count)

For a job with 1000 remaining hashes (salts) and a reported speed of 1 GH/s:
- **Actual candidate throughput**: 1 GH/s ÷ 1000 = 1 MH/s

As hashes get cracked, the salt count decreases, changing the effective candidate speed. A benchmark captured with 1000 salts is not accurate when only 100 salts remain.

#### Benchmark Cache Key Structure

**Non-salted hash types:**
```
(agent_id, attack_mode, hash_type)
```

**Salted hash types:**
```
(agent_id, attack_mode, hash_type, salt_count)
```

The unique constraint uses `IS NOT DISTINCT FROM` for NULL-safe salt_count comparison:
```sql
CREATE UNIQUE INDEX idx_agent_benchmarks_unique
ON agent_benchmarks(agent_id, attack_mode, hash_type, salt_count)
WHERE salt_count IS NOT NULL;
```

#### Benchmark Lookup Flow

1. **Retrieve hash type** from hashlist to check `is_salted` flag
2. **Calculate salt count** for salted hashes: `remaining_hashes = total - cracked`
3. **Query benchmark** with salt count parameter:
   ```go
   benchmark, err := benchmarkRepo.GetAgentBenchmark(
       ctx, agentID, attackMode, hashType, &saltCount,
   )
   ```
4. **Adjust speed for chunk calculations**:
   ```go
   if isSalted && remainingHashes > 0 {
       candidateSpeed = benchmarkSpeed / remainingHashes
   }
   ```

#### Cache Duration for Salted Benchmarks

Salted benchmarks follow the same `benchmark_cache_duration_hours` setting. However, since salt count changes as hashes crack:

- **Re-benchmarking triggers**: When salt count differs significantly from cached benchmark
- **Practical impact**: Jobs with high crack rates may trigger multiple benchmarks
- **Optimization**: System uses closest available salt_count benchmark when exact match unavailable

#### Database Schema

**Migration 000109** adds salt count support to `agent_benchmarks`:

```sql
ALTER TABLE agent_benchmarks ADD COLUMN salt_count INT;

-- Drop old unique constraint
ALTER TABLE agent_benchmarks DROP CONSTRAINT IF EXISTS ...;

-- New constraint includes salt_count (with NULL handling)
CREATE UNIQUE INDEX idx_agent_benchmarks_unique_with_salt
ON agent_benchmarks(agent_id, attack_mode, hash_type, COALESCE(salt_count, -1));
```

#### Example: NetNTLMv2 Job

```
Initial state:
- Hash type: 5600 (NetNTLMv2, is_salted=true)
- Total hashes: 5000
- Cracked: 0
- Salt count: 5000

Benchmark with salt_count=5000:
- Reported speed: 500 MH/s
- Candidate speed: 500 MH/s ÷ 5000 = 100 KH/s

After cracking 4000 hashes:
- Salt count: 1000
- New benchmark needed (or estimate)
- Reported speed: 500 MH/s
- Candidate speed: 500 MH/s ÷ 1000 = 500 KH/s
```

The 5x speed improvement is automatically reflected in chunk calculations.

### Execution Flow

#### Integration with Job Scheduling

The parallel benchmark system executes **within the scheduling cycle** as a blocking operation:

```go
func (s *JobSchedulingService) ScheduleJobs(ctx context.Context) {
    // ... existing code ...

    // Execute benchmarks in parallel and wait
    benchmarkPlan := s.CreateBenchmarkPlan(ctx, availableAgents, pendingJobs)
    if len(benchmarkPlan.Requests) > 0 {
        s.ExecuteBenchmarkPlan(ctx, benchmarkPlan)
        s.WaitForBenchmarks(ctx, benchmarkPlan)

        // Refresh available agents after benchmarks complete
        availableAgents = s.GetAvailableAgents(ctx)

        // Prioritize agents that completed forced benchmarks
        s.PrioritizeForcedBenchmarkAgents(ctx, &availableAgents, benchmarkPlan)
    }

    // Proceed with task assignment
    // ... existing code ...
}
```

#### Parallel Execution with Goroutines

All benchmark requests are sent simultaneously:

```go
func (s *JobSchedulingService) ExecuteBenchmarkPlan(ctx context.Context, plan *BenchmarkPlan) {
    var wg sync.WaitGroup

    for _, req := range plan.Requests {
        wg.Add(1)
        go func(request BenchmarkRequest) {
            defer wg.Done()
            s.sendBenchmarkRequest(ctx, request)
        }(req)
    }

    wg.Wait() // Wait for all goroutines to send requests
}
```

#### Polling-Based Completion Detection

The system polls the database to detect completion:

```go
func (s *JobSchedulingService) WaitForBenchmarks(ctx context.Context, plan *BenchmarkPlan) {
    timeout := time.Duration(speedtestTimeout + 5) * time.Second
    pollInterval := 500 * time.Millisecond

    deadline := time.Now().Add(timeout)

    for time.Now().Before(deadline) {
        completed, err := s.checkBenchmarkCompletion(ctx, plan.RequestIDs)
        if completed {
            return
        }
        time.Sleep(pollInterval)
    }
}
```

### Round-Robin Distribution

Benchmarks are distributed evenly across agents to prevent overloading:

**Algorithm:**
1. Group pending jobs by hash type
2. For each hash type group (ordered by priority):
   - Assign one benchmark request to each available agent
   - Use round-robin to distribute across jobs
3. Ensures even distribution and respects priority

**Example with 5 agents and 3 jobs:**
```
Agent 1 → Job A (Priority 100, Hash Type 1000)
Agent 2 → Job B (Priority 100, Hash Type 1000)
Agent 3 → Job C (Priority 50, Hash Type 1000)
Agent 4 → Job A (Priority 100, Hash Type 1000)  # Round-robin back to A
Agent 5 → Job B (Priority 100, Hash Type 1000)  # Round-robin to B
```

### Configuration

**System Settings:**
- `benchmark_cache_duration_hours` (default: 168 = 7 days): How long to cache benchmarks
- `speedtest_timeout_seconds` (default: 180): Timeout for individual benchmarks
- Parallel system adds 5s buffer: total wait = speedtest_timeout + 5s

### Benefits

1. **Dramatic Performance Improvement**: 96% reduction in benchmark time
2. **Scalability**: Handles hundreds of agents efficiently
3. **Intelligent Distribution**: Round-robin ensures fair allocation
4. **Priority Awareness**: Higher priority jobs get benchmarks first
5. **Resource Efficiency**: Blocking behavior prevents wasted task assignments
6. **Agent Prioritization**: Forced benchmark agents get first crack at their job

### Testing

**Verified with 15 mock agents + 3 jobs:**
- 10 benchmarks completed in 12 seconds (2 forced, 8 agent speed)
- Round-robin distribution working correctly
- Database tracking and cleanup functioning properly
- Mock agents handle benchmark requests correctly

## Related Systems

This benchmark workflow integrates with several other systems:

- **[Cross-Hashlist Sync](cross-hashlist-sync.md)**: Understanding why hashlists change frequently
- **[Job Update System](job-update-system.md)**: How keyspace values flow into job calculations, including progressive refinement

## Future Enhancements

1. **Benchmark History**: Track benchmark trends over time
2. **Performance Prediction**: Use ML to predict performance for new combinations
3. **Dynamic Re-benchmarking**: Trigger new benchmarks on performance anomalies
4. **Multi-GPU Optimization**: Per-device benchmark tracking
5. **Keyspace Prediction**: Use historical multipliers to improve initial estimates
6. **Intelligent Caching**: Detect when hashlist hasn't changed to skip download
7. **Adaptive Timeout**: Adjust timeout based on historical benchmark completion times
8. **Benchmark Prioritization**: Queue management for benchmark requests during high load