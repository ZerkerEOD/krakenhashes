# Increment Mode Implementation

## Overview

KrakenHashes now supports hashcat's `--increment` and `--increment-inverse` modes for mask-based attacks (bruteforce and hybrid). Instead of letting hashcat handle increment mode internally (which breaks distributed task assignment), the backend decomposes increment mode into discrete "layers" - one per mask length - and schedules them independently.

## Architecture

### Layer-Based Approach

When a job is created with increment mode enabled:

1. **Job Creation**: The mask (e.g., `?l?l?l`) with `increment_min=2` and `increment_max=3` is decomposed into layers
   - Layer 0: `?l?l` (length 2)
   - Layer 1: `?l?l?l` (length 3)

2. **Independent Scheduling**: Each layer is treated like a separate job by the scheduler
   - Layers are scheduled in order (increment: shortest→longest, increment_inverse: longest→shortest)
   - When a layer runs out of work, the next scheduling cycle picks up the next layer
   - All layers share the job's `max_agents` limit

3. **Progress Aggregation**: Progress flows through three levels
   - Tasks → Layers (via polling service)
   - Layers → Job (via polling service)
   - Job progress represents completion across all layers

## Database Schema

### job_increment_layers

Created by migration `000088_create_job_increment_layers.up.sql`

```sql
CREATE TABLE job_increment_layers (
    id UUID PRIMARY KEY,
    job_execution_id UUID NOT NULL REFERENCES job_executions(id) ON DELETE CASCADE,
    layer_index INT NOT NULL,
    mask VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    base_keyspace BIGINT,
    effective_keyspace BIGINT,
    processed_keyspace BIGINT DEFAULT 0,
    dispatched_keyspace BIGINT DEFAULT 0,
    is_accurate_keyspace BOOLEAN DEFAULT FALSE,
    overall_progress_percent NUMERIC(5,2) DEFAULT 0.00,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    error_message TEXT,
    UNIQUE(job_execution_id, layer_index)
);
```

**Status Flow** (same as job_executions since layers are jobs in their own right):
- `pending` → `running` → `completed`/`failed`/`cancelled`
- Use `is_accurate_keyspace` to determine if benchmark is needed (not a separate status)

### job_tasks.increment_layer_id

Created by migration `000089_add_increment_layer_id_to_job_tasks.up.sql`

```sql
ALTER TABLE job_tasks
    ADD COLUMN increment_layer_id UUID REFERENCES job_increment_layers(id) ON DELETE CASCADE;
```

Links tasks to their specific layer.

### preset_increment_layers

Created by migration `000090_create_preset_increment_layers.up.sql`

Pre-calculated increment layers for preset jobs. When a job is created from a preset with increment mode enabled, these layers are copied to `job_increment_layers` rather than being recalculated.

```sql
CREATE TABLE preset_increment_layers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    preset_job_id UUID NOT NULL REFERENCES preset_jobs(id) ON DELETE CASCADE,
    layer_index INT NOT NULL,
    mask VARCHAR(512) NOT NULL,
    base_keyspace BIGINT,
    effective_keyspace BIGINT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(preset_job_id, layer_index)
);
```

**Purpose**: Pre-calculate layers at preset creation time rather than job creation time. This ensures:
- Consistent keyspace calculations across all jobs created from the same preset
- Faster job creation (no need to re-run hashcat --keyspace for each layer)
- Preset keyspace = sum of all layer effective_keyspaces

**Data Flow**:
1. Admin creates preset job with increment mode → `preset_increment_layers` populated
2. User creates job from preset → layers copied from `preset_increment_layers` to `job_increment_layers`
3. Job inherits preset's total keyspace

## Key Components

### 1. Mask Parser (`backend/internal/utils/mask_parser.go`)

Handles all hashcat mask placeholders:
- `?l` = lowercase (26)
- `?u` = uppercase (26)
- `?d` = digits (10)
- `?s` = special chars (33)
- `?a` = all printable (95)
- `?b` = binary (0x00-0xFF)
- `?1-?9` = custom charsets

**Key Functions**:
- `ParseMask(mask string)` - Parses mask into positions
- `GenerateIncrementLayers(mask, min, max, isInverse)` - Generates layer masks
- `GetMaskLength(mask string)` - Returns position count

### 2. Layer Initialization (`backend/internal/services/job_increment_layer_service.go`)

**`initializeIncrementLayers()`** - Called during job creation

1. **Validates increment settings**:
   - `increment_min >= 1`
   - `increment_max >= increment_min`
   - `increment_min <= mask_length`
   - `increment_max <= mask_length`

2. **Generates layer masks** using `utils.GenerateIncrementLayers()`

3. **Calculates base_keyspace** per layer:
   ```bash
   hashcat --keyspace -a 3 -m <hash_type> <layer_mask>
   ```

4. **Creates layer records** in `job_increment_layers` table

5. **Updates job.total_keyspace** to sum of all layer keyspaces

### 3. Benchmark Integration (`backend/internal/services/job_scheduling_benchmark_planning.go`)

**Modified `collectJobHashTypeInfo()`**:
- Detects jobs with increment layers
- Adds each layer needing benchmark (`is_accurate_keyspace=false`) to planning queue
- Sorting: Priority → Created Time → Layer Index

**`ForcedBenchmarkTask`** now includes:
```go
type ForcedBenchmarkTask struct {
    AgentID    int
    JobID      uuid.UUID
    LayerID    *uuid.UUID  // NEW
    Mask       string      // NEW
    HashType   int
    AttackMode models.AttackMode
    Priority   int
}
```

### 4. Task Assignment (`backend/internal/services/job_scheduling_task_assignment.go`)

**JobPlanningState** tracks current layer:
```go
type JobPlanningState struct {
    JobExecution    *models.JobExecution
    CurrentLayer    *models.JobIncrementLayer     // Active layer
    AvailableLayers []models.JobIncrementLayer   // All layers with pending work
    // ... other fields
}
```

**Layer Loading** in `CreateTaskAssignmentPlans()`:
```go
if job.IncrementMode != "" && job.IncrementMode != "off" {
    layers, err := jobIncrementLayerRepo.GetLayersWithPendingWork(ctx, job.ID)
    if len(layers) > 0 {
        state.CurrentLayer = &layers[0]  // First layer with work
    }
}
```

**Layer-Aware Chunking** in `calculateKeyspaceChunk()`:
- Uses `layer.EffectiveKeyspace` instead of `job.TotalKeyspace`
- Uses `layer.DispatchedKeyspace` for tracking
- Updates layer's `dispatched_keyspace` via `IncrementDispatchedKeyspace()`

**TaskAssignmentPlan** includes layer context:
```go
type TaskAssignmentPlan struct {
    IncrementLayerID *uuid.UUID  // Links task to layer
    LayerMask        string      // Layer-specific mask
    // ... other fields
}
```

### 5. Command Building (`backend/internal/services/job_execution_service.go`)

**`buildAttackCommand()`** signature updated:
```go
func buildAttackCommand(ctx, presetJob, job, layerMask string) (string, error)
```

**Layer-Aware Behavior**:
- When `layerMask != ""`, uses it instead of `job.Mask`
- **Skips increment flags** (`--increment`, `--increment-min`, `--increment-max`)
- Backend handles layer distribution, not hashcat

### 6. Progress Calculation (`backend/internal/services/job_progress_calculation_service.go`)

**Three-Level Aggregation**:

```go
// Regular jobs: Tasks → Job
func calculateRegularJobProgress(ctx, job) (*JobProgressUpdate, error)

// Increment jobs: Tasks → Layers → Job
func calculateIncrementJobProgress(ctx, job) (*JobProgressUpdate, error) {
    // 1. Update each layer from its tasks
    for _, layer := range layers {
        calculateAndUpdateLayerProgress(ctx, layer)
    }

    // 2. Aggregate layers to job
    totalProcessed = sum(layer.ProcessedKeyspace)
    totalEffective = sum(layer.EffectiveKeyspace)
    progressPercent = (totalProcessed / totalEffective) * 100
}
```

**Polling Frequency**: Every 2 seconds

### 7. Job Detection (`backend/internal/repository/job_execution_repository.go`)

**`GetJobsWithPendingWork()`** includes layer check:
```sql
WHERE je.status IN ('pending', 'running')
AND (
    -- Regular conditions...
    OR
    -- Increment mode job with layers that have pending work
    (je.increment_mode IS NOT NULL AND je.increment_mode != 'off'
     AND EXISTS (
        SELECT 1 FROM job_increment_layers jil
        WHERE jil.job_execution_id = je.id
          AND jil.status IN ('ready', 'running')
          AND (jil.effective_keyspace IS NULL
               OR jil.dispatched_keyspace < jil.effective_keyspace)
     ))
)
```

### 8. API Endpoints (`backend/internal/handlers/jobs/user_jobs.go`)

#### GET `/api/jobs/{id}/layers`
Returns all layers for a job with statistics.

**Response**: Array of `JobIncrementLayerWithStats`
```json
[
  {
    "id": "uuid",
    "job_execution_id": "uuid",
    "layer_index": 0,
    "mask": "?l?l",
    "status": "running",
    "base_keyspace": 676,
    "effective_keyspace": 676,
    "processed_keyspace": 338,
    "dispatched_keyspace": 676,
    "is_accurate_keyspace": true,
    "overall_progress_percent": 50.00,
    "active_task_count": 2,
    "completed_task_count": 1,
    "cracked_count": 5
  }
]
```

#### GET `/api/jobs/{id}/layers/{layer_id}/tasks`
Returns all tasks for a specific layer.

**Response**: Array of `JobTask` (filtered by `increment_layer_id`)

## Workflow Example

### Job Creation: `?l?l?l` with min=2, max=3

1. **User creates job** with:
   - Attack mode: Bruteforce (3)
   - Mask: `?l?l?l`
   - Increment mode: `increment`
   - Increment min: 2
   - Increment max: 3

2. **Validation** (`initializeIncrementLayers`):
   ```
   min=2 >= 1 ✓
   max=3 >= min=2 ✓
   min=2 <= mask_length=3 ✓
   max=3 <= mask_length=3 ✓
   ```

3. **Layer Generation**:
   ```
   Layer 0: ?l?l (index=0, base_keyspace=676)
   Layer 1: ?l?l?l (index=1, base_keyspace=17576)
   ```

4. **Job Record Created**:
   - `total_keyspace = 676 + 17576 = 18252`
   - `increment_mode = "increment"`
   - Status: `pending`

### Scheduling Cycle 1: Benchmark Layer 0

1. **Scheduler detects** Layer 0 needs benchmark (`is_accurate_keyspace=false`)

2. **Agent allocated** for forced benchmark:
   - Command: `hashcat -a 3 -m <type> ?l?l --keyspace-only`
   - Captures `progress[1]` value → `effective_keyspace`

3. **Layer updated**:
   - `effective_keyspace = 676` (from hashcat)
   - `is_accurate_keyspace = true`
   - `status = 'ready'`

### Scheduling Cycle 2: Work on Layer 0

1. **Scheduler detects** Layer 0 has pending work

2. **Agent 1 allocated**:
   - Plan created with `IncrementLayerID = layer0.id`, `LayerMask = "?l?l"`
   - Command: `hashcat -a 3 -m <type> ?l?l` (no --increment flags!)
   - Task created with `increment_layer_id = layer0.id`
   - Layer's `dispatched_keyspace += 676`

3. **Layer 0 exhausted**: `dispatched_keyspace (676) >= effective_keyspace (676)`

### Scheduling Cycle 3: Benchmark Layer 1

1. **Scheduler detects** Layer 1 needs benchmark

2. **Agent allocated** for benchmark of `?l?l?l`

3. **Layer 1 ready** for work

### Scheduling Cycle 4: Work on Layer 1

1. **Scheduler detects** Layer 1 has pending work

2. **Multiple agents allocated**:
   - Each gets chunk of Layer 1's keyspace
   - Commands use mask `?l?l?l` with `--skip` and `--limit` if splitting

### Progress Updates (Every 2 seconds)

1. **Task progress reported** by agents via WebSocket

2. **Polling service aggregates**:
   ```
   Layer 0: processed_keyspace = 676, progress = 100%
   Layer 1: processed_keyspace = 8788, progress = 50%

   Job: processed_keyspace = 9464, progress = 51.8%
   ```

### Job Completion

All layers reach `status='completed'` → Job marked as `completed`

## Key Design Decisions

### 1. Why Not Let Hashcat Handle Increment?

**Problem**: Hashcat's increment mode runs sequentially internally:
```bash
hashcat -a 3 -m 1000 hash.txt ?l?l?l --increment --increment-min=2
# Internally runs:
# 1. ?l?l
# 2. ?l?l?l
```

With `--skip` and `--limit`, hashcat applies them to the **entire** increment range, making it impossible to:
- Distribute work across agents properly
- Track progress accurately
- Resume from specific points

**Solution**: Backend decomposes into layers and schedules each independently.

### 2. Why Store base_keyspace AND effective_keyspace?

- **base_keyspace**: From `hashcat --keyspace` (fast, estimated)
- **effective_keyspace**: From `progress[1]` during benchmark (accurate, actual)

**Reason**: Rule multipliers and hashcat internals can cause differences. We calculate estimated totals immediately but refine with accurate values after benchmarks.

### 3. Why max_agents Applies Across All Layers?

**User Expectation**: "Max 5 agents" means for the entire job, not per layer.

**Implementation**: Scheduler treats layers as parts of one job:
```
Job A (Layer 0): 3 agents running
Job A (Layer 1): Can't start yet (exhausted Layer 0, max_agents=5)

Next cycle:
Job A (Layer 1): Can use up to 5 agents now
```

### 4. Why No Dynamic Layer Switching?

**Design**: Layer exhaustion = stop planning, next cycle picks up next layer

**Why**: Simpler, cleaner:
- Avoids complex state management during planning
- Natural scheduler behavior (polls for work each cycle)
- Prevents race conditions with concurrent task creation

## Testing

### Test Case 1: Single Agent

**Setup**:
- Job: Mask `?l?l?l`, increment min=2, max=3
- 1 agent available

**Expected Behavior**:
1. Agent benchmarks Layer 0 (`?l?l`)
2. Agent works on Layer 0 until complete
3. Agent benchmarks Layer 1 (`?l?l?l`)
4. Agent works on Layer 1 until complete
5. Job completes

**Verification**:
- Check layer records created correctly
- Verify tasks have correct `increment_layer_id`
- Confirm hashcat commands don't include `--increment` flags
- Validate progress aggregation

### Test Case 2: Multiple Agents

**Setup**:
- Job: Mask `?d?d?d?d`, increment min=2, max=4, max_agents=3
- 5 agents available

**Expected Behavior**:
1. One agent benchmarks Layer 0 (`?d?d`)
2. Up to 3 agents work on Layer 0 in parallel
3. Layer 0 completes
4. One agent benchmarks Layer 1 (`?d?d?d`)
5. Up to 3 agents work on Layer 1
6. Layer 1 completes
7. Process continues for Layer 2

**Verification**:
- Max 3 agents active per layer (respects max_agents)
- Work distributed evenly (--skip/--limit used correctly)
- No gaps or overlaps in keyspace coverage
- Progress accurate across all layers

## Troubleshooting

### Layer Not Getting Benchmarked

**Symptom**: Layer status is `pending` and `is_accurate_keyspace` is FALSE, no progress

**Check**:
1. Are agents available? `SELECT * FROM agents WHERE status='online'`
2. Does agent have benchmark? `SELECT * FROM agent_benchmarks WHERE agent_id=X AND hash_type_id=Y`
3. Check scheduler logs for benchmark allocation

**Fix**: Manually trigger benchmark or check agent connectivity

### Progress Not Updating

**Symptom**: Layer or job progress stuck at 0%

**Check**:
1. Is polling service running? Check logs for "Job progress calculation service"
2. Are tasks reporting progress? Check `job_tasks.effective_keyspace_processed`
3. Layer has `effective_keyspace` set? Check `job_increment_layers.effective_keyspace`

**Fix**: Restart polling service or check agent WebSocket connection

### Tasks Using Wrong Mask

**Symptom**: Tasks show job mask instead of layer mask

**Check**:
1. Task has `increment_layer_id` set? `SELECT * FROM job_tasks WHERE job_execution_id=X`
2. Layer mask correct? `SELECT mask FROM job_increment_layers WHERE job_execution_id=X`
3. Check `attack_cmd` field in task record

**Fix**: Likely scheduler bug - check task creation logic

### Validation Errors

**Symptom**: Job creation fails with increment validation error

**Common Causes**:
- `increment_min < 1`
- `increment_max < increment_min`
- `increment_min > mask_length`
- `increment_max > mask_length`

**Fix**: Adjust increment settings to valid ranges

## Files Modified/Created

### Database
- `backend/db/migrations/000088_create_job_increment_layers.up.sql`
- `backend/db/migrations/000089_add_increment_layer_id_to_job_tasks.up.sql`

### Models
- `backend/internal/models/jobs.go` - Added `JobIncrementLayer`, `JobIncrementLayerStatus`

### Repositories
- `backend/internal/repository/job_increment_layer_repository.go` - NEW
- `backend/internal/repository/job_execution_repository.go` - Updated `GetJobsWithPendingWork()`

### Services
- `backend/internal/services/job_increment_layer_service.go` - NEW
- `backend/internal/services/job_execution_service.go` - Updated `buildAttackCommand()`
- `backend/internal/services/job_progress_calculation_service.go` - Added layer aggregation
- `backend/internal/services/job_scheduling_benchmark_planning.go` - Added layer detection
- `backend/internal/services/job_scheduling_task_assignment.go` - Added layer planning

### Utilities
- `backend/internal/utils/mask_parser.go` - NEW
- `backend/internal/utils/mask_parser_test.go` - NEW

### Handlers
- `backend/internal/handlers/jobs/user_jobs.go` - Added `GetJobLayers()`, `GetJobLayerTasks()`

### Routes
- `backend/internal/routes/user.go` - Registered layer endpoints

### Main
- `backend/cmd/server/main.go` - Added `jobIncrementLayerRepo` initialization

## Future Enhancements

### 1. Dynamic Chunking Optimization
Currently uses existing chunking logic. Could optimize:
- Skip --skip/--limit for small layers that fit on one agent
- Adjust chunk size based on layer size vs agent count

### 2. Layer Parallelization
Currently runs layers sequentially. Could allow:
- Multiple layers running simultaneously
- Requires careful max_agents allocation logic

### 3. Layer Priorities
Allow users to prioritize specific layers:
- Start with longer/shorter masks first
- Custom layer ordering

### 4. Estimated Completion Time Per Layer
Show ETA for each layer based on:
- Agent speeds
- Remaining keyspace
- Historical completion times

### 5. Layer Pause/Resume
Allow pausing specific layers:
- Useful for long-running increment jobs
- Focus resources on specific mask lengths

## Summary

The increment mode implementation provides:
- ✅ Full `--increment` and `--increment-inverse` support
- ✅ Proper distributed task assignment
- ✅ Accurate progress tracking per layer and overall
- ✅ Validation at job creation
- ✅ Clean separation of concerns (layers treated as sub-jobs)
- ✅ No changes to agent code (backward compatible)
- ✅ RESTful API for layer management
- ✅ Comprehensive error handling

The system is production-ready for distributed hashcat operations with increment mode.
