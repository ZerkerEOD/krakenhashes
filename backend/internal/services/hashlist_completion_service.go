package services

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// WSHandler interface for sending WebSocket messages to agents
type WSHandler interface {
	SendMessage(agentID int, msg interface{}) error
}

// HashlistCompletionService handles auto-completion/deletion of jobs when all hashes are cracked
type HashlistCompletionService struct {
	db                    *db.DB
	jobExecRepo           *repository.JobExecutionRepository
	jobTaskRepo           *repository.JobTaskRepository
	jobIncrementLayerRepo *repository.JobIncrementLayerRepository
	hashlistRepo          *repository.HashListRepository
	wsHandler             WSHandler
}

// NewHashlistCompletionService creates a new hashlist completion service
func NewHashlistCompletionService(
	database *db.DB,
	jobExecRepo *repository.JobExecutionRepository,
	jobTaskRepo *repository.JobTaskRepository,
	jobIncrementLayerRepo *repository.JobIncrementLayerRepository,
	hashlistRepo *repository.HashListRepository,
	wsHandler WSHandler,
) *HashlistCompletionService {
	return &HashlistCompletionService{
		db:                    database,
		jobExecRepo:           jobExecRepo,
		jobTaskRepo:           jobTaskRepo,
		jobIncrementLayerRepo: jobIncrementLayerRepo,
		hashlistRepo:          hashlistRepo,
		wsHandler:             wsHandler,
	}
}

// HandleHashlistFullyCracked processes all jobs for a hashlist when all hashes are cracked.
// triggeringTaskID: Optional ID of the task that triggered this handler (won't be stopped)
func (s *HashlistCompletionService) HandleHashlistFullyCracked(ctx context.Context, hashlistID int64, triggeringTaskID *uuid.UUID) error {
	debug.Info("HandleHashlistFullyCracked called for hashlist %d", hashlistID)

	// Note: We skip database verification here because this handler is triggered by
	// hashcat status code 6 (AllHashesCracked flag), which is authoritative.
	// The database may lag behind due to async crack processing, causing a race condition
	// where the handler checks before cracks are written to DB.
	// We trust hashcat's status code 6 signal and proceed immediately.

	debug.Info("Hashlist %d - processing job completion (triggered by hashcat status code 6)",
		hashlistID)

	// 2. Get all non-completed jobs for this hashlist
	jobs, err := s.jobExecRepo.GetNonCompletedJobsByHashlistID(ctx, hashlistID)
	if err != nil {
		return fmt.Errorf("failed to get jobs for hashlist: %w", err)
	}

	if len(jobs) == 0 {
		debug.Info("No non-completed jobs found for hashlist %d", hashlistID)
		return nil
	}

	debug.Info("Found %d non-completed jobs for hashlist %d", len(jobs), hashlistID)

	// 3. Process each job
	jobsCompleted := 0
	jobsDeleted := 0
	jobsFailed := 0

	for _, job := range jobs {
		// Get task count
		taskCount, err := s.jobTaskRepo.GetTaskCountForJob(ctx, job.ID)
		if err != nil {
			debug.Error("Failed to get task count for job %s: %v", job.ID, err)
			jobsFailed++
			continue // Skip this job, process others
		}

		if taskCount > 0 {
			// Job has tasks - it has started
			debug.Info("Job %s has %d tasks - marking as completed", job.ID, taskCount)

			// Stop any active tasks
			stoppedCount, err := s.stopJobTasks(ctx, job.ID, triggeringTaskID)
			if err != nil {
				debug.Error("Failed to stop tasks for job %s: %v", job.ID, err)
				// Continue anyway - best effort
			} else if stoppedCount > 0 {
				debug.Info("Stopped %d active tasks for job %s", stoppedCount, job.ID)
			}

			// Mark job as completed with 100% progress. The triggering task ID
			// is threaded through so completeJob's reconcile can skip it for
			// the same reason stopJobTasks skips its stop signal — see the
			// comment on completeJob.
			err = s.completeJob(ctx, &job, triggeringTaskID)
			if err != nil {
				debug.Error("Failed to complete job %s: %v", job.ID, err)
				jobsFailed++
				continue
			}

			// Scheduler-v2 cascade: also mark this job's scheduling_units
			// completed so the v2 cycle stops considering them. Without
			// this, the unit stays 'pending' and the v2 selector (which
			// joins on parent job status as of Step 7a) catches the
			// completed job — but defense in depth: if Step 7a is ever
			// loosened, this cascade still prevents the runaway-dispatch
			// scenario where every subsequent task fails on an empty
			// hashlist with exit 255. Best-effort: log on error, don't
			// fail the whole completion path.
			if _, uErr := s.db.ExecContext(ctx, `
				UPDATE scheduling_units
				SET status = 'completed', updated_at = NOW()
				WHERE parent_job_id = $1
				  AND status IN ('pending', 'running')
			`, job.ID); uErr != nil {
				debug.Warning("Failed to cascade scheduling_units completed for job %s: %v", job.ID, uErr)
			}

			jobsCompleted++
			debug.Info("Job %s marked as completed (all hashes cracked)", job.ID)

		} else {
			// Job has no tasks - it never started
			debug.Info("Job %s has no tasks - deleting (never started)", job.ID)

			err = s.jobExecRepo.Delete(ctx, job.ID)
			if err != nil {
				debug.Error("Failed to delete unstarted job %s: %v", job.ID, err)
				jobsFailed++
				continue
			}

			jobsDeleted++
			debug.Info("Job %s deleted (never started, hashlist fully cracked)", job.ID)
		}
	}

	debug.Info("Hashlist %d completion processing finished: %d completed, %d deleted, %d failed",
		hashlistID, jobsCompleted, jobsDeleted, jobsFailed)

	return nil
}

// stopJobTasks sends stop signals to all agents working on tasks for a job.
// triggeringTaskID: Optional ID of the task that triggered completion (will be skipped)
// Returns the number of tasks that were stopped
func (s *HashlistCompletionService) stopJobTasks(ctx context.Context, jobID uuid.UUID, triggeringTaskID *uuid.UUID) (int, error) {
	// Get all tasks for this job
	tasks, err := s.jobTaskRepo.GetTasksByJobExecution(ctx, jobID)
	if err != nil {
		debug.Error("Failed to get tasks for job %s: %v", jobID, err)
		return 0, err
	}

	// Send stop signals to agents working on active tasks
	stoppedCount := 0
	for _, task := range tasks {
		// Skip the task that triggered this handler (it already completed)
		if triggeringTaskID != nil && task.ID == *triggeringTaskID {
			debug.Info("Skipping stop signal for task %s (triggered hashlist completion)", task.ID)
			continue
		}

		// Only send stop signals for running or assigned tasks
		if task.AgentID != nil && (task.Status == models.JobTaskStatusRunning || task.Status == models.JobTaskStatusAssigned) {
			// GH #62: Terminalize this sibling as 'cancelled'. Previously we
			// tried to detect "keyspace already crossed" and mark completed,
			// or marked the task 'processing' and forced its progress to 100%
			// — both interacted with the buggy keyspace-crossing completion
			// path and could leave the task non-terminal while hashcat was
			// still running. All hashes are already cracked, so this chunk's
			// remaining keyspace is moot: send the stop signal so the agent
			// kills hashcat, then mark the task TERMINAL as 'cancelled'.
			//
			// Invariant: the DB must NEVER be job=completed with a task in
			// {running,assigned,processing,pending}. 'cancelled' (NOT 'failed')
			// is used so HasFailedTasks does not flip the job to 'failed'.

			// Create stop message payload
			stopPayload := map[string]string{
				"task_id": task.ID.String(),
			}
			payloadJSON, err := json.Marshal(stopPayload)
			if err != nil {
				debug.Error("Failed to marshal stop payload for task %s: %v", task.ID, err)
				continue
			}

			// Create the WebSocket message
			stopMsg := map[string]interface{}{
				"type":    "job_stop",
				"payload": json.RawMessage(payloadJSON),
			}

			// Send stop signal to the agent FIRST so it kills hashcat
			if s.wsHandler != nil {
				if err := s.wsHandler.SendMessage(*task.AgentID, stopMsg); err != nil {
					debug.Error("Failed to send stop signal to agent %d for task %s: %v", *task.AgentID, task.ID, err)
				} else {
					debug.Info("Sent stop signal to agent %d for task %s (hashlist fully cracked)", *task.AgentID, task.ID)
					stoppedCount++
				}
			} else {
				debug.Warning("WebSocket handler not available, cannot send stop signal to agent %d", *task.AgentID)
			}

			// Mark the sibling TERMINAL as 'cancelled' and clear the agent's
			// busy status atomically. Do NOT force progress_percent=100 — the
			// chunk did not finish; it was stopped because the hashlist is
			// already fully cracked.
			if err := s.jobTaskRepo.CancelTaskAndClearAgentStatus(ctx, task.ID, *task.AgentID); err != nil {
				debug.Error("Failed to cancel task %s and clear agent status: %v", task.ID, err)
			}
			continue
		}

		// Tasks WITHOUT an agent (pending, or v2-rejected before the race
		// recovery wired up) have no hashcat to stop and no agent to ack.
		// They are still listed in JobDetails' "Active Tasks" and render
		// as gray segments in the per-task progress bar, polluting the
		// "this job is done" view. Mark them cancelled so they fall out
		// of the active set; their keyspace ranges have intervals that
		// are released by the v2 cascade below.
		if task.AgentID == nil &&
			(task.Status == models.JobTaskStatusPending || task.Status == models.JobTaskStatusAssigned) {
			if err := s.jobTaskRepo.UpdateStatus(ctx, task.ID, models.JobTaskStatusCancelled); err != nil {
				debug.Error("Failed to cancel orphan task %s (no agent, hashlist fully cracked): %v", task.ID, err)
			}
		}
	}

	// Scheduler-v2 cascade: also mark every still-open interval for this
	// job's units terminal. Without this, the per-task bar's "pending" gray
	// space (= dispatched-but-not-finished chunks) persists visually even
	// after the hashlist is fully cracked, because the bar's universe is the
	// full effective keyspace. We use 'completed' (NOT 'cancelled'): the
	// interval CHECK constraint (valid_interval_status, migration 000147)
	// permits only assigned/running/completed/failed, so an earlier
	// 'cancelled' write always failed the constraint and this cascade was a
	// silent no-op. 'completed' is coverage-neutral here — the gap/coverage
	// gates already count any interval with status <> 'failed' as covered
	// (keyspace_interval_repository.go, job_progress_calculation_service.go) —
	// so promoting assigned/running → completed changes no coverage total; it
	// only gives the bar a terminal state. Purely a UI-honesty fix on an
	// all-cracked completion.
	//
	// SCOPE (this is load-bearing, not a micro-optimisation): only intervals
	// whose task is ALREADY TERMINAL are closed. The previous job-wide,
	// task-status-blind form also promoted the TRIGGERING task's own interval
	// to 'completed' while that task was still 'processing' (mid crack
	// handshake — stopJobTasks deliberately skips it, and completeJob's
	// reconcile now skips it too). applyRecovery in services/scheduler uses
	// interval status as its progress oracle: a live task whose interval
	// already reads 'completed' has no open interval left to truncate or
	// re-open, which is precisely the inconsistency that made agent-disconnect
	// recovery mislabel the incident task. This restores the invariant "an
	// interval is only 'completed' when its task is terminal".
	//
	// Filtering on task status rather than excluding the triggering task by ID
	// is deliberate: it also covers a sibling whose
	// CancelTaskAndClearAgentStatus above returned an error (that loop logs and
	// continues, leaving the task non-terminal), which an ID-based exclusion
	// would miss.
	//
	// The UI-honesty purpose survives with roughly a second of lag rather than
	// being given up: when the triggering task finishes its crack handshake,
	// JobExecutionService.HandleTaskCompletion runs
	//   UPDATE job_keyspace_intervals SET status = 'completed'
	//   WHERE task_id = $1 AND status IN ('assigned', 'running')
	// (services/job_execution_service.go) and closes exactly this interval; if
	// the agent dies mid-handshake instead, the stale-'processing' backstop in
	// services/job_cleanup_service.go drives recovery, which releases it.
	// Either way the per-task bar converges to no gray gaps.
	if _, err := s.db.ExecContext(ctx, `
		UPDATE job_keyspace_intervals jki
		SET status = 'completed'
		FROM scheduling_units su
		WHERE jki.scheduling_unit_id = su.id
		  AND su.parent_job_id = $1
		  AND jki.status IN ('assigned', 'running')
		  AND NOT EXISTS (
		      SELECT 1 FROM job_tasks t
		      WHERE t.id = jki.task_id
		        AND t.status IN ('assigned', 'running', 'processing', 'pending')
		  )
	`, jobID); err != nil {
		debug.Warning("Failed to cascade open intervals to completed for job %s: %v", jobID, err)
	}

	if stoppedCount > 0 {
		debug.Info("Sent stop signals for %d tasks of job %s (hashlist fully cracked)", stoppedCount, jobID)
	}

	return stoppedCount, nil
}

// markTaskComplete100Percent updates task keyspace values to reflect 100% completion
// This is called when all hashes are cracked and the task is being forcefully completed
func (s *HashlistCompletionService) markTaskComplete100Percent(ctx context.Context, task *models.JobTask) error {
	// Calculate full keyspace for this task
	fullKeyspaceProcessed := task.KeyspaceEnd - task.KeyspaceStart
	effectiveProcessed := models.NewBigInt(fullKeyspaceProcessed)
	if task.EffectiveKeyspaceEnd != nil && task.EffectiveKeyspaceStart != nil {
		effectiveProcessed = task.EffectiveKeyspaceEnd.Sub(*task.EffectiveKeyspaceStart)
	}

	// Update task progress to 100%
	return s.jobTaskRepo.UpdateProgress(ctx, task.ID, fullKeyspaceProcessed, effectiveProcessed, nil, 100.0)
}

// completeJob marks a job as completed with 100% progress.
//
// triggeringTaskID is the task whose hashcat status-6 ("all hashes cracked")
// report started this path. It is EXCLUDED from the non-terminal-task reconcile
// below — see the comment on the reconcile for why. Everything else about job
// completion is unchanged and, in particular, is NOT delayed by it.
//
// The parameter is nil-able so the reconcile's `$2::uuid IS NULL` arm stays
// meaningful for a future caller, but the only call site today
// (HandleHashlistFullyCracked) always passes a real task ID. The public
// StopJobTasks entry point never reaches this function.
func (s *HashlistCompletionService) completeJob(ctx context.Context, job *models.JobExecution, triggeringTaskID *uuid.UUID) error {
	// Part 18d: For increment mode jobs, mark all layers as completed with 100% progress
	if job.IncrementMode != "" && job.IncrementMode != "off" && s.jobIncrementLayerRepo != nil {
		layers, err := s.jobIncrementLayerRepo.GetByJobExecutionID(ctx, job.ID)
		if err != nil {
			debug.Warning("Failed to get increment layers for job %s: %v", job.ID, err)
		} else {
			for _, layer := range layers {
				if layer.Status != models.JobIncrementLayerStatusCompleted {
					// Update layer to 100% progress
					if layer.EffectiveKeyspace != nil && layer.EffectiveKeyspace.IsPositive() {
						if err := s.jobIncrementLayerRepo.UpdateProgress(ctx, layer.ID, *layer.EffectiveKeyspace, 100.0); err != nil {
							debug.Warning("Failed to update layer %s progress: %v", layer.ID, err)
						}
					}
					// Mark layer as completed
					if err := s.jobIncrementLayerRepo.UpdateStatus(ctx, layer.ID, models.JobIncrementLayerStatusCompleted); err != nil {
						debug.Warning("Failed to complete layer %s: %v", layer.ID, err)
					} else {
						debug.Info("Marked increment layer %s as completed (all hashes cracked)", layer.ID)
					}
				}
			}
		}
	}

	// Set job progress to 100% since all hashes in the hashlist are cracked.
	// This must be done BEFORE CompleteExecution() because the polling service
	// skips completed jobs, so it would never update progress afterwards.
	if err := s.jobExecRepo.UpdateProgressPercent(ctx, job.ID, 100.0); err != nil {
		debug.Warning("Failed to set job %s progress to 100%%: %v", job.ID, err)
		// Not fatal - continue with completion
	}

	// GH #62 defense-in-depth: reconcile any still-non-terminal task for this
	// job to 'cancelled' before completing. stopJobTasks should already have
	// terminalized every sibling, but a lost WS stop message could leave a
	// task running/assigned/processing/pending.
	//
	// EXCEPT the triggering task. By the time we run, HandleJobProgress has put
	// that task in 'processing' with an expected_crack_count: it is mid crack
	// handshake, streaming us the very cracks that prove the hashlist is done.
	// Cancelling it out from under the handshake is what produced the incident:
	// the row went 'processing' → 'cancelled' here, an (then unguarded)
	// SetTaskProcessing resurrected it to 'processing', nothing was left to move
	// it out, and it sat there until the agent disconnected and was mislabelled
	// 'failed'. stopJobTasks already skips this task for exactly this reason —
	// skipping it there and cancelling it here just undoes that. Owner decision:
	// let the handshake finish.
	//
	// The GH #62 invariant is therefore DOWNGRADED, deliberately and explicitly.
	// It is no longer "the DB is NEVER job=completed with a non-terminal task";
	// it is "job=completed with at most the triggering task non-terminal, and
	// that task is EVENTUALLY terminal — within the crack handshake, or within
	// one backstop sweep". What enforces the "eventually":
	//   - the handshake paths in internal/integration/job_websocket_integration.go
	//     (TryFinalizeTask, called from every point where the handshake may have
	//     just become satisfied — it completes the task the moment
	//     received_crack_count >= expected_crack_count);
	//   - failing that (agent dies mid-handshake and the batches never arrive),
	//     the stale-'processing' backstop in services/job_cleanup_service.go.
	// Job completion itself is NOT held up waiting for either: we complete now.
	//
	// detailed_status is written alongside status because leaving it untouched
	// is exactly why the incident row read status='failed' with
	// detailed_status='running' — a task the UI showed as still running long
	// after it was over.
	//
	// Best-effort: log on error, don't abort completion.
	if res, reconErr := s.db.ExecContext(ctx, `
		UPDATE job_tasks
		SET status = 'cancelled',
		    detailed_status = 'cancelled',
		    completed_at = NOW()
		WHERE job_execution_id = $1
		  AND status IN ('assigned', 'running', 'processing', 'pending')
		  AND ($2::uuid IS NULL OR id <> $2::uuid)
	`, job.ID, triggeringTaskID); reconErr != nil {
		debug.Error("Failed to reconcile non-terminal tasks to cancelled for job %s: %v", job.ID, reconErr)
	} else if affected, _ := res.RowsAffected(); affected > 0 {
		debug.Warning("Reconciled %d non-terminal task(s) to 'cancelled' for job %s before completion — a WS stop signal was likely lost", affected, job.ID)
	}

	// Close the intervals of the tasks the reconcile just terminalised.
	//
	// stopJobTasks ran its own copy of this cascade a moment ago, but it
	// deliberately skips intervals whose task is still non-terminal — which, at
	// that point, includes every task the reconcile above was about to cancel
	// (a sibling sitting in 'processing' is skipped by stopJobTasks' per-task
	// loop too, since that loop only handles 'running'/'assigned'). Without this
	// second pass those intervals would stay 'assigned' under a 'cancelled'
	// task: the GH #77 stranding signature, and a direct violation of the
	// invariant that no interval in ('assigned','running') sits under a task in
	// ('pending','failed','cancelled').
	//
	// The NOT EXISTS is identical to stopJobTasks', so the triggering task —
	// still 'processing', still mid-handshake — keeps its open interval and its
	// coverage is booked by whoever terminalises it (HandleTaskCompletion on the
	// happy path, recovery on the backstop path). That is the one interval this
	// job is allowed to leave open, and it is transient by construction.
	if _, err := s.db.ExecContext(ctx, `
		UPDATE job_keyspace_intervals jki
		SET status = 'completed'
		FROM scheduling_units su
		WHERE jki.scheduling_unit_id = su.id
		  AND su.parent_job_id = $1
		  AND jki.status IN ('assigned', 'running')
		  AND NOT EXISTS (
		      SELECT 1 FROM job_tasks t
		      WHERE t.id = jki.task_id
		        AND t.status IN ('assigned', 'running', 'processing', 'pending')
		  )
	`, job.ID); err != nil {
		debug.Warning("Failed to close intervals of reconciled tasks for job %s: %v", job.ID, err)
	}

	// Mark job as completed (this also sets completed_at)
	err := s.jobExecRepo.CompleteExecution(ctx, job.ID)
	if err != nil {
		return fmt.Errorf("failed to complete job execution: %w", err)
	}

	// Note: Job-level progress is now calculated by the polling service (JobProgressCalculationService)
	// which runs every 2 seconds and recalculates from task data
	// Note: Job completion notifications are handled by JobExecutionService via NotificationDispatcher

	return nil
}

// StopJobTasks is a public method that stops all tasks for a job (for use by other handlers)
func (s *HashlistCompletionService) StopJobTasks(ctx context.Context, jobID uuid.UUID) (int, error) {
	return s.stopJobTasks(ctx, jobID, nil)
}
