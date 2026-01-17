# Automatic Job Completion System

## Overview

KrakenHashes automatically detects when all hashes in a hashlist have been cracked and manages the lifecycle of related jobs to prevent failures and wasted resources.

## The Problem

Hashcat's `--remove` option removes cracked hashes from input files during execution. When all hashes are cracked:
- The hashlist file becomes empty
- Subsequent jobs targeting that hashlist fail immediately
- Resources are wasted attempting to process empty files
- Users receive confusing error messages

## The Solution

### Status Code 6 Detection

The agent monitors hashcat's JSON status output for status code 6, which indicates "all hashes cracked." This code is sent by hashcat when:
- The input file has no remaining uncracked hashes
- All work is complete for the given hashlist

### Trust Model

The system **trusts status code 6 as authoritative** without database verification because:
- Hashcat knows definitively when all hashes are cracked
- Database verification would create race conditions
- Status code 6 is a reliable signal from hashcat
- Prevents complex synchronization issues

### Job Cleanup Process

When status code 6 is received:

1. **Identify All Affected Jobs**: Query for ALL jobs (any status) targeting the same hashlist
2. **Running Jobs**:
   - Send WebSocket stop signals to active agents
   - Mark jobs as "completed" at 100% progress
   - Send completion email notifications
3. **Pending Jobs**:
   - Delete jobs that haven't started yet
   - No email notifications (jobs never ran)
4. **Prevention**: New tasks for this hashlist won't be created

### Technical Implementation

**Components:**
- `HashlistCompletionService`: Handles job cleanup logic
- `AllHashesCracked` flag in WebSocket messages
- Background processing with 5-minute timeout

**Flow:**
```
Agent detects status code 6 → Sets AllHashesCracked flag →
Backend handler triggered → HashlistCompletionService runs async →
Stop running tasks + Delete pending jobs → Send notifications
```

**Code Location:** `backend/internal/services/hashlist_completion_service.go`

## Agent-Side Implementation

### Detection

In `agent/internal/jobs/hashcat_executor.go`:
- Parses hashcat JSON status output
- Checks for `status` field equal to 6
- Sets `AllHashesCracked` flag in progress update message
- Flag sent with regular progress updates (no special message needed)

### Timing

- Detection occurs during normal progress monitoring
- No additional API calls required
- Flag transmitted with existing WebSocket infrastructure

## Backend-Side Implementation

### Message Handling

In `backend/internal/routes/websocket_with_jobs.go`:
- Checks `AllHashesCracked` flag in job progress messages
- Triggers before status-specific processing
- Runs HashlistCompletionService asynchronously

### Service Logic

`HashlistCompletionService.HandleHashlistCompletion()`:

1. **Query Affected Jobs**:
   ```sql
   SELECT * FROM job_executions
   WHERE hashlist_id = ?
   AND status IN ('pending', 'running', 'paused')
   ```

2. **Process Running Jobs**:
   - Find active tasks for each running job
   - Send stop signals via WebSocket
   - Update job status to 'completed'
   - Set progress to 100%
   - Trigger email notifications

3. **Process Pending Jobs**:
   - Delete jobs that haven't started
   - Clean up any associated data
   - No notifications needed

4. **Update Job Priority**:
   - Comprehensive processing regardless of priority
   - Handles all affected jobs in single operation

## Processing Status Workflow

### Overview

To prevent jobs from completing prematurely before all crack batches are received and processed, KrakenHashes implements a "processing" status for both jobs and tasks. This ensures accurate completion emails and proper job state management.

### The Challenge

When hashcat finishes processing a task:
1. Agent sends final progress message with `Status="completed"` and `CrackedCount` field
2. Agent begins sending crack batches asynchronously
3. Without processing status, job would complete immediately
4. Completion email would be sent before all cracks are received
5. Crack count in email would be inaccurate or zero

### Processing Status Solution

**Task Processing Workflow:**

```
Task Running → Final Progress Received → Task Processing → All Batches Received → Task Completed
                                         (cracking_completed_at set)                (completed_at set)
```

1. **Agent Sends Final Progress**:
   - Progress message includes `Status="completed"`
   - Includes `CrackedCount` field with expected number of cracks
   - Or `AllHashesCracked=true` flag with crack count

2. **Backend Transitions to Processing**:
   - Task status changes from `running` to `processing`
   - **`cracking_completed_at` timestamp set to current time** (hashcat finished)
   - `expected_crack_count` field set from progress message
   - `received_crack_count` initialized to 0
   - `batches_complete_signaled` set to false

3. **Agent Sends Crack Batches**:
   - Agent sends one or more `crack_batch` messages
   - Backend increments `received_crack_count` as batches arrive
   - Backend processes and stores each batch

4. **Agent Signals Completion**:
   - Agent sends `crack_batches_complete` WebSocket message
   - Backend sets `batches_complete_signaled` to true
   - Agent is now free to accept new work

5. **Backend Completes Task**:
   - Backend checks: `received_crack_count >= expected_crack_count AND batches_complete_signaled == true`
   - If true: Task transitions from `processing` to `completed`
   - **`completed_at` timestamp set to current time** (all batches received)
   - Agent busy status cleared
   - Job completion check triggered

**Job Processing Workflow:**

```
Job Running → All Tasks Processing → Job Processing → All Tasks Completed → Job Completed (Email Sent)
                                     (cracking_completed_at set)           (completed_at set)
```

1. **Job Enters Processing**:
   - When all tasks transition to `processing` status
   - Job status changes from `running` to `processing`
   - **`cracking_completed_at` timestamp set** (all tasks finished hashcat execution)
   - Progress shows 100% but job not yet complete

2. **Job Completes**:
   - When all tasks reach `completed` status
   - Job status changes from `processing` to `completed`
   - **`completed_at` timestamp set** (job fully finished)
   - Completion email notification sent with accurate crack count

### Email Notification Integration

**Accurate Crack Counts:**
- `GetTotalCracksForJob()` sums `crack_count` from all tasks for the job
- Provides per-job crack count instead of hashlist total
- Prevents incorrect crack counts when multiple jobs target same hashlist

**Duplicate Email Prevention:**
- Job executions track `completion_email_sent` flag
- Hashlist completion service email disabled to prevent duplicates
- Only one email sent per job completion

### Database Fields

**job_executions:**
- `status` includes `'processing'` value
- `cracking_completed_at` (TIMESTAMP WITH TIME ZONE) - When all tasks finished hashcat execution
- `completion_email_sent` (BOOLEAN)
- `completion_email_sent_at` (TIMESTAMP)
- `completion_email_error` (TEXT)

**job_tasks:**
- `status` includes `'processing'` value
- `cracking_completed_at` (TIMESTAMP WITH TIME ZONE) - When hashcat finished for this task (enters processing state)
- `expected_crack_count` (INTEGER)
- `received_crack_count` (INTEGER)
- `batches_complete_signaled` (BOOLEAN)

### Timestamp Distinction

The system uses two distinct completion timestamps:

| Timestamp | Scope | Meaning |
|-----------|-------|---------|
| `cracking_completed_at` | Task | When hashcat exited - task enters `processing` state |
| `completed_at` | Task | When all crack batches received and processed |
| `cracking_completed_at` | Job | When all tasks finished hashcat execution |
| `completed_at` | Job | When job is fully complete and email sent |

**Why Two Timestamps?**

1. **Accurate Duration Tracking**: `cracking_completed_at - started_at` gives the actual GPU cracking time
2. **Processing Overhead Visibility**: `completed_at - cracking_completed_at` shows batch processing time
3. **Debugging**: Helps identify where delays occur (cracking vs. data transmission)
4. **Analytics**: Enables reporting on actual GPU utilization vs. total job duration

### Repository Methods

**JobTaskRepository:**
- `SetTaskProcessing(taskID, expectedCracks)` - Transition task to processing
- `IncrementReceivedCrackCount(taskID, count)` - Track received batches
- `MarkBatchesComplete(taskID)` - Signal all batches sent
- `CheckTaskReadyToComplete(taskID)` - Verify completion conditions
- `GetProcessingTasksForJob(jobExecutionID)` - Query processing tasks

**JobExecutionRepository:**
- `SetJobProcessing(jobExecutionID)` - Transition job to processing
- `UpdateEmailStatus(jobExecutionID, sent, sentAt, error)` - Track email delivery

### WebSocket Messages

**From Agent to Backend:**

`crack_batches_complete`:
```json
{
  "type": "crack_batches_complete",
  "task_id": "uuid-here"
}
```

Signals that agent has finished sending all crack batches for the task.

## Task Completion Acknowledgment Protocol

### Overview

To prevent agents from getting stuck in a busy state after task completion, KrakenHashes implements a completion acknowledgment (ACK) protocol. This ensures the backend has processed the completion before the agent accepts new work.

### The Problem (GH Issue #12)

Without acknowledgments, the following race condition could occur:

1. Agent completes task, sends `job_progress` with `status=completed`
2. Message may be lost or delayed in network
3. Agent thinks it's done, but backend never receives completion
4. Agent remains in "busy" state indefinitely
5. Manual intervention required to recover

### ACK Protocol Flow

```
Agent                                    Backend
  │                                         │
  │  job_progress (status=completed)        │
  │────────────────────────────────────────>│
  │                                         │
  │         [Agent enters COMPLETING state] │
  │         [Starts ACK wait timer]         │
  │                                         │
  │                     [Process completion]│
  │                     [Atomic task update]│
  │                     [Cache completion]  │
  │                                         │
  │           task_complete_ack             │
  │<────────────────────────────────────────│
  │                                         │
  │  [ACK received, transition to IDLE]     │
  │                                         │
```

### Message Types

**Agent → Backend:**

`job_progress` with completion status:
```json
{
  "type": "job_progress",
  "payload": {
    "task_id": "uuid-here",
    "status": "completed",
    "progress_percent": 100.0,
    "cracked_count": 42
  }
}
```

**Backend → Agent:**

`task_complete_ack`:
```json
{
  "type": "task_complete_ack",
  "payload": {
    "task_id": "uuid-here",
    "timestamp": "2024-01-15T10:30:00Z",
    "success": true,
    "message": "Task completed successfully"
  }
}
```

### Retry Logic

If the agent doesn't receive an ACK within the timeout:

1. **Retry 1**: Resend completion message after 30 seconds
2. **Retry 2**: Resend again after 30 seconds
3. **Retry 3**: Final attempt after 30 seconds
4. **Timeout**: Mark task as `completion_pending` and transition to IDLE

```go
// Agent ACK wait configuration
const (
    AckWaitTimeout = 30 * time.Second
    AckMaxRetries  = 3
)
```

### Completion Pending Flag

When ACK retries are exhausted, the agent sets a `completion_pending` flag:

- Allows agent to accept new work (prevents indefinite blocking)
- Backend resolves pending completion on next state sync
- Task is eventually confirmed completed or failed

### Completion Cache (Idempotency)

The backend maintains a completion cache to handle duplicate messages:

- **Cache key**: Task ID
- **Cache TTL**: 1 hour
- **Behavior**: If completion already cached, just send ACK without reprocessing

This prevents:
- Double-counting cracks
- Duplicate keyspace updates
- Multiple completion emails

### Atomic Operations

Task completion uses atomic database operations:

```go
// backend/internal/repository/job_task_repository.go
func (r *JobTaskRepository) CompleteTaskAndClearAgentStatus(
    ctx context.Context,
    taskID uuid.UUID,
    agentID int,
) error {
    // Single transaction updates both task AND agent status
    // Prevents race condition where task completes but agent remains busy
}
```

### Stuck Detection (Safety Net)

As a last resort, agents implement stuck detection:

| Parameter | Value |
|-----------|-------|
| Check interval | 30 seconds |
| Stuck timeout | 2 minutes |
| Recovery action | Force transition to IDLE, set completion_pending |

If an agent remains in COMPLETING state for over 2 minutes, it automatically recovers.

### State Sync Protocol

The backend periodically requests state synchronization:

1. **Backend sends**: `state_sync_request` every 5 minutes
2. **Agent responds**: Current state, active task, completion_pending flag
3. **Backend resolves**: Any mismatches between agent and database state

This catches edge cases where ACK was lost but agent already moved on.

### Integration with Processing Status

The ACK protocol works alongside the processing status system:

1. Agent sends `job_progress` with `status=completed` → Backend transitions task to `processing`
2. Agent sends crack batches → Backend receives and counts them
3. Agent sends `crack_batches_complete` → Backend checks if ready to complete
4. Backend sends `task_complete_ack` → Agent transitions to IDLE
5. Backend completes task atomically when all batches received

### Error Handling

| Scenario | Agent Behavior | Backend Behavior |
|----------|---------------|------------------|
| ACK lost | Retry up to 3 times | Idempotent - just resend ACK |
| Backend down | Mark completion_pending, accept new work | Resolve on reconnection via state sync |
| Duplicate completion | N/A (agent already IDLE) | Return cached ACK |
| Task not found | Mark completion_pending | Log error, don't crash |

## Configuration

No configuration required - this feature is always active.

## Benefits

1. **Prevents Failures**: No more failed jobs due to empty hashlist files
2. **Resource Efficiency**: Stops wasting resources on completed hashlists
3. **User Experience**: Automatic cleanup without manual intervention
4. **Accurate Notifications**: Users receive completion emails with correct crack counts after all data is processed
5. **Clean State**: Queue automatically cleaned of obsolete jobs
6. **Data Integrity**: Processing status ensures all crack batches are received before job completion
7. **No Duplicate Emails**: Each job sends exactly one completion email

## Error Handling

### Timeout Protection
- 5-minute timeout for cleanup operations
- Prevents hanging if service encounters issues
- Logged errors don't block agent progress reporting

### Transaction Safety
- Database operations use transactions
- Rollback on errors ensures consistency
- Agent continues normal operation regardless of cleanup success

### WebSocket Errors
- Gracefully handles disconnected agents
- Tasks marked for stop even if agent offline
- Agent reconnection triggers cleanup on next connection

## Limitations

- Trusts hashcat status code 6 without verification
- Only handles jobs for the same hashlist (doesn't affect other hashlists)
- Requires agent to detect and report status code 6
- Depends on WebSocket connectivity for stop signals

## Testing

Tested with hashlist 85:
- 1 running job completed at 100% with stop signal sent
- 2 pending jobs deleted (never started)
- Email notifications triggered successfully
- No errors in logs

## Monitoring and Debugging

### Log Messages

Success:
```
Successfully completed job [uuid] for hashlist [id]
Successfully deleted pending job [uuid] for hashlist [id]
```

Errors:
```
Failed to stop tasks for job [uuid]: [error]
Failed to complete job [uuid]: [error]
```

### Metrics

Track in monitoring:
- Number of jobs auto-completed
- Number of pending jobs cleaned up
- Time taken for cleanup operations
- Failed cleanup attempts

## Related Documentation

- [Crack Batching System](./crack-batching-system.md) - How crack batches are transmitted and the processing status integration
- [Chunking System](./chunking.md) - How jobs are divided into chunks
- [Job Update System](./job-update-system.md) - How keyspace updates work
- [Jobs & Workflows](../../user-guide/jobs-workflows.md) - User perspective on automatic completion and processing status
- [Core Concepts](../../user-guide/core-concepts.md) - Understanding job execution flow
- [Database Schema](../database.md) - Job executions and tasks table structure with processing status fields

## Future Enhancements

Potential improvements under consideration:

- **Partial Completion Threshold**: Complete jobs when X% of hashes cracked (configurable)
- **Notification Customization**: Per-client notification preferences
- **Completion Hooks**: Custom scripts triggered on hashlist completion
- **Statistics Tracking**: Historical data on completion rates and timing
- **Manual Override**: Allow users to force completion or prevent automatic cleanup
