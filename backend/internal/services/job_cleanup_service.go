package services

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// StuckProcessingHandler is the narrow slice of the WebSocket integration layer
// that the stale-processing backstop needs.
//
// It is an interface rather than a direct dependency because this package
// CANNOT import the packages that own the two operations. The recovery
// primitive lives in services/scheduler, and services -> services/scheduler ->
// services/websocket -> services is an import cycle; the implementation lives
// in internal/integration, which itself imports services. Same shape and same
// reason as WSHandler in hashlist_completion_service.go.
//
// Implemented by *integration.JobWebSocketIntegration and wired in
// cmd/server/main.go once routes.SetupRoutes has built the job integration
// manager.
type StuckProcessingHandler interface {
	// TryFinalizeTask completes a task whose crack handshake the DB already
	// shows as satisfied. Returns true if it performed the completion.
	TryFinalizeTask(ctx context.Context, taskID uuid.UUID) (bool, error)
	// AbandonProcessingTask terminalises a task whose cracks are never
	// arriving, releasing its keyspace. Never produces 'failed'.
	AbandonProcessingTask(ctx context.Context, taskID uuid.UUID, reason string) error
}

// JobCleanupService handles cleanup of stale jobs and tasks
type JobCleanupService struct {
	jobExecutionRepo   *repository.JobExecutionRepository
	jobTaskRepo        *repository.JobTaskRepository
	systemSettingsRepo *repository.SystemSettingsRepository
	agentRepo          *repository.AgentRepository

	// stuckProcessingHandler may be nil: this service is constructed during
	// early startup, long before the routes (and therefore the WebSocket
	// integration) exist, and CleanupStaleTasksOnStartup runs immediately.
	// Every use must be nil-guarded — a nil-pointer panic here would take down
	// the periodic sweep that every stuck job depends on.
	stuckProcessingHandler StuckProcessingHandler
}

// NewJobCleanupService creates a new job cleanup service
func NewJobCleanupService(
	jobExecutionRepo *repository.JobExecutionRepository,
	jobTaskRepo *repository.JobTaskRepository,
	systemSettingsRepo *repository.SystemSettingsRepository,
	agentRepo *repository.AgentRepository,
) *JobCleanupService {
	return &JobCleanupService{
		jobExecutionRepo:   jobExecutionRepo,
		jobTaskRepo:        jobTaskRepo,
		systemSettingsRepo: systemSettingsRepo,
		agentRepo:          agentRepo,
	}
}

// SetStuckProcessingHandler wires the WebSocket integration layer into the
// stale-processing backstop (checkForStaleProcessingTasks).
//
// Deliberately a setter and not a constructor argument: main.go builds this
// service and runs the startup cleanup before routes.SetupRoutes exists, so the
// integration instance simply isn't available yet at construction time. Until
// this is called the backstop is inert and says so in the log rather than
// panicking.
func (s *JobCleanupService) SetStuckProcessingHandler(handler StuckProcessingHandler) {
	s.stuckProcessingHandler = handler
}

// CleanupStaleTasksOnStartup cleans up tasks that were left in an incomplete state
func (s *JobCleanupService) CleanupStaleTasksOnStartup(ctx context.Context) error {
	debug.Info("Starting cleanup of stale tasks on startup with grace period for reconnection")

	// FIRST: Check for orphaned running jobs (jobs with no active tasks)
	// This must run regardless of whether there are stale tasks
	debug.Info("Checking for orphaned running jobs on startup")
	s.checkForOrphanedRunningJobs(ctx)

	// SECOND: Log tasks in processing state - these will be handled when agents reconnect
	// and send pending_outfiles message. The ProcessPendingOutfiles function will request
	// retransmit for tasks not yet completed.
	s.logProcessingTasksOnStartup(ctx)

	// THIRD: Fail jobs stuck in "preparing" — their background wordlist-generation
	// goroutine did not survive the restart, so they will never progress (GH #40).
	s.failStalePreparingJobs(ctx)

	// Get all tasks that are in assigned or running state
	staleTasks, err := s.jobTaskRepo.GetStaleTasks(ctx)
	if err != nil {
		debug.Error("Failed to get stale tasks: %v", err)
		return fmt.Errorf("failed to get stale tasks: %w", err)
	}

	if len(staleTasks) == 0 {
		debug.Info("No stale tasks found during startup cleanup")
		return nil
	}

	debug.Info("Found %d stale tasks - marking as reconnect_pending with 2-minute grace period", len(staleTasks))

	// Mark each stale task as reconnect_pending instead of failed
	for _, task := range staleTasks {
		agentID := 0
		if task.AgentID != nil {
			agentID = *task.AgentID
		}
		debug.Info("Marking task as reconnect_pending - ID: %s, Status: %s, Agent: %d, Job: %s",
			task.ID, task.Status, agentID, task.JobExecutionID)

		// Update task status to reconnect_pending
		err := s.jobTaskRepo.UpdateStatus(ctx, task.ID, models.JobTaskStatusReconnectPending)
		if err != nil {
			debug.Error("Failed to update task %s to reconnect_pending: %v", task.ID, err)
			continue
		}

		debug.Info("Successfully marked task as reconnect_pending - Task ID: %s, Agent: %d, Job: %s",
			task.ID, agentID, task.JobExecutionID)
	}

	// Convert to slice of pointers for handleGracePeriodExpiration
	taskPointers := make([]*models.JobTask, len(staleTasks))
	for i := range staleTasks {
		taskPointers[i] = &staleTasks[i]
	}

	// Start a goroutine to handle the grace period expiration
	go s.handleGracePeriodExpiration(ctx, taskPointers)

	// IMPORTANT: Do NOT mark jobs as pending here
	// Jobs should remain "running" if they have reconnect_pending tasks
	debug.Info("Jobs with reconnect_pending tasks will remain in running state awaiting agent reconnection")

	// Log final status
	debug.Info("Startup cleanup completed - %d tasks marked as reconnect_pending", len(staleTasks))

	return nil
}

// logProcessingTasksOnStartup logs tasks in processing state on startup.
// These tasks have completed hashcat work but are waiting for crack data to be fully saved.
// They will be handled when agents reconnect and send pending_outfiles message,
// which triggers ProcessPendingOutfiles to request retransmit.
func (s *JobCleanupService) logProcessingTasksOnStartup(ctx context.Context) {
	processingTasks, err := s.jobTaskRepo.GetTasksByStatuses(ctx, []string{
		string(models.JobTaskStatusProcessing),
	})
	if err != nil {
		debug.Error("Failed to get processing tasks on startup: %v", err)
		return
	}

	if len(processingTasks) == 0 {
		debug.Info("No tasks in processing state found on startup")
		return
	}

	debug.Info("Found %d tasks in processing state on startup - awaiting agent reconnection for outfile retransmit", len(processingTasks))

	// Log details of each processing task
	for _, task := range processingTasks {
		agentID := 0
		if task.AgentID != nil {
			agentID = *task.AgentID
		}
		debug.Info("Processing task awaiting retransmit - ID: %s, Job: %s, Agent: %d, ExpectedCracks: %d, ReceivedCracks: %d",
			task.ID, task.JobExecutionID, agentID, task.ExpectedCrackCount, task.ReceivedCrackCount)
	}

	// Also check for jobs in processing state
	processingJobs, err := s.jobExecutionRepo.GetJobsByStatus(ctx, models.JobExecutionStatusProcessing)
	if err != nil {
		debug.Error("Failed to get processing jobs on startup: %v", err)
		return
	}

	if len(processingJobs) > 0 {
		debug.Info("Found %d jobs in processing state on startup - awaiting task completion", len(processingJobs))
		for _, job := range processingJobs {
			debug.Info("Processing job - ID: %s", job.ID)
		}
	}
}

// failStalePreparingJobs fails any jobs left in the "preparing" state across a
// restart. Preparation (ephemeral filtered-wordlist generation) runs in an
// in-memory goroutine that does not survive a restart, so such jobs would
// otherwise be stuck forever (GH #40).
func (s *JobCleanupService) failStalePreparingJobs(ctx context.Context) {
	preparingJobs, err := s.jobExecutionRepo.GetJobsByStatus(ctx, models.JobExecutionStatusPreparing)
	if err != nil {
		debug.Error("Failed to get preparing jobs on startup: %v", err)
		return
	}
	if len(preparingJobs) == 0 {
		return
	}
	debug.Warning("Found %d job(s) stuck in 'preparing' on startup - marking failed (generation did not survive restart)", len(preparingJobs))
	for _, job := range preparingJobs {
		if err := s.jobExecutionRepo.FailExecution(ctx, job.ID, "Job preparation was interrupted by a server restart"); err != nil {
			debug.Error("Failed to fail stuck preparing job %s: %v", job.ID, err)
		}
	}
}

// staleProcessingAgentHeartbeatWindow is how fresh an agent's heartbeat must be
// for the abandon gate to accept "this agent could still retransmit its cracks".
//
// It matches the 90s heartbeat timeout main.go passes to
// AgentCleanupService.CleanupStaleAgents, which is what flips a silent 'active'
// agent to 'inactive'. Checking the heartbeat as well as the status matters
// because the status flip depends on that other sweeper getting a turn: if it
// errors or is wedged, a dead agent's row stays 'active' forever and a
// status-only predicate would block the abandon gate forever with it — turning
// the backstop back into the "logs about it every 5 minutes and never acts"
// non-fix this rework exists to remove.
const staleProcessingAgentHeartbeatWindow = 90 * time.Second

// staleProcessingHardCutoffFactor bounds how long a reachable agent can defer
// Gate 2 of the stale-processing sweep. Past this multiple of the staleness
// timeout the row is abandoned no matter how healthy its agent looks.
//
// The deferral exists because a live agent's outstanding cracks are worth a few
// extra minutes of a stale row. But "the agent is up" is not evidence that this
// task's handshake is still progressing — every write in that handshake (crack
// batches, retransmit counter bumps) touches updated_at, so a row that has not
// moved for this long is not waiting on anything. Without the bound, an agent
// that stays connected forever and never retransmits would defer the abandon on
// every single sweep, recreating the indefinitely-stuck 'processing' task this
// backstop exists to eliminate.
const staleProcessingHardCutoffFactor = 3

// checkForStaleProcessingTasks is the backstop that guarantees no task sits in
// 'processing' indefinitely, however the crack handshake breaks next.
//
// WHAT THIS REPLACED, and why it could never have worked: the previous version
// only acted when retransmit_count >= 6, and that counter is incremented in
// exactly one place — requestCrackRetransmit, reached only via
// handleCrackCountMismatch. Once a retransmit has been verified and the agent
// told to delete its outfile, no further mismatch can ever occur, so the
// counter is pinned at whatever it reached (1, in the GH #79 incident) and the
// >= 6 branch is unreachable. Below the threshold it logged "may still recover"
// every 5 minutes, forever. And the branch it guarded called
// SetTaskProcessingError, which wrote status = 'processing_error' — not a value
// in the valid_task_status CHECK constraint on job_tasks (pending, assigned,
// reconnect_pending, running, processing, completed, failed, cancelled) — so
// every call it ever made died on a constraint violation. Had it worked it
// would have been worse: HasFailedTasks counts processing_error, so
// CompleteJobExecution would call FailExecution and permanently fail a job
// whose keyspace is perfectly recoverable. The repository method is gone; see
// the note where it used to live in job_task_repository.go.
//
// The rework is two gates with very different costs:
//
// GATE 1 runs for EVERY processing task regardless of age. TryFinalizeTask is
// two indexed reads and can only fire when the DB already says the handshake is
// satisfied (batches signaled AND received >= expected), so it is safe at any
// age — and there is no reason to make a finished task wait out the staleness
// timeout for a status it has already earned. This is what shortens the
// worst-case stuck window from 30 minutes to one ticker period (5 minutes, per
// MonitorStaleTasksPeriodically in cmd/server/main.go).
//
// GATE 2 runs only for rows older than the staleness cutoff, and terminalises
// them via AbandonProcessingTask, which releases the keyspace without ever
// writing 'failed'.
func (s *JobCleanupService) checkForStaleProcessingTasks(ctx context.Context, timeout time.Duration) {
	processingTasks, err := s.jobTaskRepo.GetTasksByStatuses(ctx, []string{
		string(models.JobTaskStatusProcessing),
	})
	if err != nil {
		debug.Error("Failed to get processing tasks for stale check: %v", err)
		return
	}

	if len(processingTasks) == 0 {
		return
	}

	// Read the handler once so a concurrent SetStuckProcessingHandler can't
	// make it non-nil halfway through the loop and produce a sweep that used
	// two different worlds.
	handler := s.stuckProcessingHandler

	cutoffTime := time.Now().Add(-timeout)
	// Hard cutoff for the reachable-agent deferral below. Without it, an agent
	// that stays connected and heartbeating but never retransmits would defer
	// Gate 2 on every sweep, forever — which is the same indefinitely-stuck
	// 'processing' row this backstop exists to eliminate, just reached by a
	// different route. Past this point the agent's reachability stops buying it
	// any more patience: the row has not moved in staleProcessingHardCutoffFactor
	// × timeout, and every write in the handshake (crack batches, retransmit
	// counter bumps) touches updated_at, so nothing is happening regardless of
	// what the agent's heartbeat says.
	hardCutoffTime := time.Now().Add(-staleProcessingHardCutoffFactor * timeout)
	staleCount := 0

	for _, task := range processingTasks {
		agentID := 0
		if task.AgentID != nil {
			agentID = *task.AgentID
		}

		// GATE 1: cheap, age-independent, idempotent. TryFinalizeTask
		// re-evaluates the handshake FROM THE DATABASE and completes the task
		// if it is already satisfied — the GH #79 case, where the final crack
		// batch and the batches-complete signal raced and neither was left to
		// notice the task was done.
		if handler != nil {
			finalized, ferr := handler.TryFinalizeTask(ctx, task.ID)
			if ferr != nil {
				debug.Error("Stale-processing sweep: TryFinalizeTask failed for task %s (job %s, agent %d): %v",
					task.ID, task.JobExecutionID, agentID, ferr)
			} else if finalized {
				debug.Info("Stale-processing sweep: completed task %s (job %s, agent %d) - crack handshake was already satisfied in the DB (expected %d, received %d)",
					task.ID, task.JobExecutionID, agentID, task.ExpectedCrackCount, task.ReceivedCrackCount)
				continue
			}
		}

		// GATE 2: expensive and lossy (it gives up on cracks that may still be
		// in flight), so it only applies once the row has genuinely gone quiet.
		if !task.UpdatedAt.Before(cutoffTime) {
			continue
		}
		staleCount++

		if handler == nil {
			debug.Warning("Stale-processing backstop is unwired (SetStuckProcessingHandler was never called) - task %s (job %s, agent %d) has been in 'processing' since %v and cannot be recovered automatically",
				task.ID, task.JobExecutionID, agentID, task.UpdatedAt)
			continue
		}

		// A reachable agent may still retransmit, and its cracks are worth more
		// than a few extra minutes of a stale row. This check is an
		// optimisation, not a correctness requirement: AbandonProcessingTask is
		// safe against a live agent too, because a later crack batch simply
		// loses the guarded-UPDATE race against the terminal status and is
		// ignored.
		//
		// Bounded on purpose: the deferral stops at hardCutoffTime so a
		// permanently-connected agent that never retransmits cannot hold a row
		// in 'processing' indefinitely.
		if task.UpdatedAt.After(hardCutoffTime) && s.agentMayStillRetransmit(ctx, task.AgentID) {
			debug.Info("Processing task %s (job %s) is stale since %v but agent %d is still reachable - deferring abandonment until %v (expected %d, received %d)",
				task.ID, task.JobExecutionID, task.UpdatedAt, agentID,
				task.UpdatedAt.Add(staleProcessingHardCutoffFactor*timeout),
				task.ExpectedCrackCount, task.ReceivedCrackCount)
			continue
		}

		reason := fmt.Sprintf("crack handshake abandoned after %v (expected %d, received %d)",
			timeout, task.ExpectedCrackCount, task.ReceivedCrackCount)

		if aerr := handler.AbandonProcessingTask(ctx, task.ID, reason); aerr != nil {
			debug.Error("Failed to abandon stale processing task %s (job %s, agent %d): %v",
				task.ID, task.JobExecutionID, agentID, aerr)
			continue
		}

		debug.Warning("Abandoned stale processing task %s (job %s, agent %d): %s - keyspace released for re-dispatch",
			task.ID, task.JobExecutionID, agentID, reason)
	}

	if staleCount > 0 {
		debug.Info("Found %d stale processing tasks (not updated in %v)", staleCount, timeout)
	}
}

// agentMayStillRetransmit reports whether a stale processing task's agent is
// reachable enough that its outstanding cracks could still arrive.
//
// One indexed primary-key read — the cheapest reachability predicate available
// to this service, which holds an AgentRepository but no WebSocket hub.
//
// Every uncertain answer resolves to false (abandon). That direction is the
// safe one: abandoning a task whose agent is in fact alive costs at most a
// re-run of its keyspace and loses no crack that has already been persisted,
// whereas deferring to an agent that is never coming back is precisely the
// forever-stuck state this backstop exists to prevent.
func (s *JobCleanupService) agentMayStillRetransmit(ctx context.Context, agentID *int) bool {
	if agentID == nil {
		// The task's agent link was already cleared, so nobody is left holding
		// an outfile for it.
		return false
	}

	agent, err := s.agentRepo.GetByID(ctx, *agentID)
	if err != nil || agent == nil {
		debug.Warning("Stale-processing sweep: could not read agent %d for reachability check (%v) - treating it as unreachable", *agentID, err)
		return false
	}

	if agent.Status != models.AgentStatusActive {
		return false
	}

	// See staleProcessingAgentHeartbeatWindow: 'active' alone is not proof of
	// life, because the status only flips when the stale-agent sweeper runs.
	return !agent.LastHeartbeat.IsZero() && time.Since(agent.LastHeartbeat) <= staleProcessingAgentHeartbeatWindow
}

// handleGracePeriodExpiration handles the expiration of the grace period for reconnect_pending tasks
func (s *JobCleanupService) handleGracePeriodExpiration(ctx context.Context, tasks []*models.JobTask) {
	// Get grace period from settings or use default
	gracePeriod := 5 * time.Minute // Default 5 minutes instead of 2
	gracePeriodSetting, err := s.systemSettingsRepo.GetSetting(ctx, "reconnect_grace_period_minutes")
	if err == nil && gracePeriodSetting.Value != nil {
		if minutes, err := strconv.Atoi(*gracePeriodSetting.Value); err == nil {
			gracePeriod = time.Duration(minutes) * time.Minute
		}
	}

	debug.Info("Starting grace period timer for %d tasks - duration: %v", len(tasks), gracePeriod)

	time.Sleep(gracePeriod)

	debug.Info("Grace period expired - checking for tasks that didn't reconnect")

	// Get max retry attempts from settings
	maxRetries := 3
	retrySetting, err := s.systemSettingsRepo.GetSetting(ctx, "max_chunk_retry_attempts")
	if err == nil && retrySetting.Value != nil {
		if retries, err := strconv.Atoi(*retrySetting.Value); err == nil {
			maxRetries = retries
		}
	}

	// Group tasks by job for efficient job status updates
	jobTaskMap := make(map[uuid.UUID][]*models.JobTask)

	for _, task := range tasks {
		// Check if task is still in reconnect_pending state
		currentTask, err := s.jobTaskRepo.GetByID(ctx, task.ID)
		if err != nil {
			debug.Error("Failed to get task %s status: %v", task.ID, err)
			continue
		}

		// If task is still reconnect_pending, handle based on retry count
		if currentTask.Status == models.JobTaskStatusReconnectPending {
			agentID := 0
			if currentTask.AgentID != nil {
				agentID = *currentTask.AgentID
			}

			// Check if task can be retried
			if currentTask.RetryCount < maxRetries {
				// Reset task for retry
				err := s.jobTaskRepo.ResetTaskForRetry(ctx, currentTask.ID)
				if err != nil {
					debug.Error("Failed to reset task %s for retry: %v", currentTask.ID, err)
					// Fall back to marking as failed
					errorMsg := fmt.Sprintf("Agent failed to reconnect within grace period (attempt %d/%d)",
						currentTask.RetryCount+1, maxRetries)
					s.jobTaskRepo.MarkTaskFailedPermanently(ctx, currentTask.ID, errorMsg)
				} else {
					debug.Info("Task reset for retry after grace period - Task ID: %s, Agent: %d, Retry: %d/%d",
						currentTask.ID, agentID, currentTask.RetryCount+1, maxRetries)
				}
			} else {
				// Mark as permanently failed after all retries exhausted
				errorMsg := fmt.Sprintf("Agent failed to reconnect after %d attempts", currentTask.RetryCount)
				err := s.jobTaskRepo.MarkTaskFailedPermanently(ctx, currentTask.ID, errorMsg)
				if err != nil {
					debug.Error("Failed to mark task %s as failed: %v", currentTask.ID, err)
					continue
				}
				debug.Info("Task permanently failed after %d retries - Task ID: %s, Agent: %d",
					currentTask.RetryCount, currentTask.ID, agentID)

				// Track tasks by job for status update
				jobTaskMap[currentTask.JobExecutionID] = append(jobTaskMap[currentTask.JobExecutionID], currentTask)
			}
		} else {
			debug.Info("Task %s reconnected successfully - status: %s", currentTask.ID, currentTask.Status)
		}
	}

	// Check each affected job to see if it should be marked as pending
	for jobID, failedTasks := range jobTaskMap {
		debug.Info("Checking job %s status after grace period - %d tasks failed to reconnect", jobID, len(failedTasks))

		// Get all tasks for this job
		allTasks, err := s.jobTaskRepo.GetTasksByJobExecution(ctx, jobID)
		if err != nil {
			debug.Error("Failed to get tasks for job %s: %v", jobID, err)
			continue
		}

		// Check if any tasks are still running or reconnect_pending
		hasActiveTasks := false
		for _, task := range allTasks {
			if task.Status == models.JobTaskStatusRunning ||
				task.Status == models.JobTaskStatusReconnectPending ||
				task.Status == models.JobTaskStatusAssigned {
				hasActiveTasks = true
				break
			}
		}

		// If no active tasks remain, mark job as pending for rescheduling
		if !hasActiveTasks {
			err := s.jobExecutionRepo.UpdateStatus(ctx, jobID, models.JobExecutionStatusPending)
			if err != nil {
				debug.Error("Failed to mark job %s as pending: %v", jobID, err)
				continue
			}
			debug.Info("Job %s marked as pending - all agents failed to reconnect", jobID)
		} else {
			debug.Info("Job %s remains running - has active tasks", jobID)
		}
	}

	debug.Info("Grace period expiration handling completed")
}

// MonitorStaleTasksPeriodically checks for stale tasks periodically
func (s *JobCleanupService) MonitorStaleTasksPeriodically(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	debug.Log("Starting periodic stale task monitor", map[string]interface{}{
		"interval": interval,
	})

	for {
		select {
		case <-ctx.Done():
			debug.Log("Stale task monitor stopped", nil)
			return
		case <-ticker.C:
			s.checkForStaleTasks(ctx)
		}
	}
}

// checkForStaleTasks checks for tasks that have been assigned/running too long without updates
func (s *JobCleanupService) checkForStaleTasks(ctx context.Context) {
	// Get task timeout setting (default 5 minutes for agent heartbeat)
	taskTimeout := 5 * time.Minute
	timeoutSetting, err := s.systemSettingsRepo.GetSetting(ctx, "task_heartbeat_timeout_minutes")
	if err == nil && timeoutSetting.Value != nil {
		if minutes, err := time.ParseDuration(*timeoutSetting.Value + "m"); err == nil {
			taskTimeout = minutes
		}
	} else {
		// Fall back to task_timeout_minutes if heartbeat setting doesn't exist
		timeoutSetting, err = s.systemSettingsRepo.GetSetting(ctx, "task_timeout_minutes")
		if err == nil && timeoutSetting.Value != nil {
			if minutes, err := time.ParseDuration(*timeoutSetting.Value + "m"); err == nil {
				taskTimeout = minutes
			}
		}
	}

	// FIRST: Always check for orphaned running jobs (jobs with no active tasks at all)
	// This must run regardless of whether there are stale tasks
	s.checkForOrphanedRunningJobs(ctx)

	// SECOND: Check for stale processing tasks (tasks in processing state for too long)
	// Use a longer timeout for processing tasks (30 minutes default)
	s.checkForStaleProcessingTasks(ctx, 30*time.Minute)

	// Find tasks that haven't been updated in the timeout period
	cutoffTime := time.Now().Add(-taskTimeout)

	staleTasks, err := s.jobTaskRepo.GetTasksNotUpdatedSince(ctx, cutoffTime)
	if err != nil {
		debug.Log("Failed to check for stale tasks", map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	if len(staleTasks) == 0 {
		return
	}

	debug.Log("Found stale tasks during periodic check", map[string]interface{}{
		"count":   len(staleTasks),
		"timeout": taskTimeout,
	})

	for _, task := range staleTasks {
		// Check if task has exceeded retry limit (3 attempts)
		if task.RetryCount >= 3 {
			// Mark task as permanently failed
			errorMsg := fmt.Sprintf("Task failed after %d retry attempts (last timeout after %v without progress update)", task.RetryCount, taskTimeout)
			err := s.jobTaskRepo.MarkTaskFailedPermanently(ctx, task.ID, errorMsg)
			if err != nil {
				debug.Log("Failed to mark stale task as permanently failed", map[string]interface{}{
					"task_id": task.ID,
					"error":   err.Error(),
				})
				continue
			}

			debug.Log("Marked task as permanently failed after retries", map[string]interface{}{
				"task_id":        task.ID,
				"agent_id":       task.AgentID,
				"retry_count":    task.RetryCount,
				"timeout_period": taskTimeout,
			})

			// Update job's consecutive failures count
			s.updateJobConsecutiveFailures(ctx, task.JobExecutionID, true)

			// Update agent's consecutive failures if assigned
			if task.AgentID != nil {
				s.updateAgentConsecutiveFailures(ctx, *task.AgentID, true)
			}

			// Check if the job should be transitioned to pending after task failure
			s.checkJobForPendingTransition(ctx, task.JobExecutionID)
		} else {
			// Reset task for retry
			err := s.jobTaskRepo.ResetTaskForRetry(ctx, task.ID)
			if err != nil {
				debug.Log("Failed to reset stale task for retry", map[string]interface{}{
					"task_id": task.ID,
					"error":   err.Error(),
				})
				continue
			}

			debug.Log("Reset timed-out task for retry", map[string]interface{}{
				"task_id":        task.ID,
				"agent_id":       task.AgentID,
				"retry_count":    task.RetryCount + 1,
				"timeout_period": taskTimeout,
			})
		}
	}

	// Check if any affected jobs should be transitioned to pending
	affectedJobs := make(map[uuid.UUID]bool)
	for _, task := range staleTasks {
		affectedJobs[task.JobExecutionID] = true
	}

	for jobID := range affectedJobs {
		s.checkJobForPendingTransition(ctx, jobID)
	}

	// Run stuck job reconciliation as a safety net
	s.reconcileStuckJobs(ctx)
}

// checkJobForPendingTransition checks if a job should be transitioned to pending
func (s *JobCleanupService) checkJobForPendingTransition(ctx context.Context, jobID uuid.UUID) {
	// Check if this job has any running, assigned, or pending tasks
	allTasks, err := s.jobTaskRepo.GetTasksByJobExecution(ctx, jobID)
	if err != nil {
		debug.Log("Failed to check tasks for job", map[string]interface{}{
			"job_id": jobID,
			"error":  err.Error(),
		})
		return
	}

	// Count active tasks (running, assigned, pending, or reconnect_pending)
	activeTaskCount := 0
	for _, task := range allTasks {
		if task.Status == models.JobTaskStatusRunning ||
			task.Status == models.JobTaskStatusAssigned ||
			task.Status == models.JobTaskStatusPending ||
			task.Status == models.JobTaskStatusReconnectPending {
			activeTaskCount++
		}
	}

	// If no active tasks, transition job to pending
	if activeTaskCount == 0 {
		job, err := s.jobExecutionRepo.GetByID(ctx, jobID)
		if err != nil {
			return
		}

		if job.Status == models.JobExecutionStatusRunning {
			// Check if job has remaining keyspace to process. This MUST stay
			// consistent with the completion gate in ProcessJobCompletion,
			// otherwise the two fight: cleanup re-pends a job that completion
			// considers done, looping forever. For non-rule-split jobs that means
			// a BASE-keyspace check — max(keyspace_end) over non-failed tasks vs
			// base_keyspace — not the drift-prone effective comparison that left
			// jobs stuck a fraction short.
			hasRemainingWork := false

			if job.BaseKeyspace != nil && *job.BaseKeyspace > 0 {
				// Authoritative base-keyspace coverage check (same source the
				// dispatcher chunks from). On a transient error, default to "work
				// remains" so we don't wrongly settle the job into a terminal-ish
				// state from the cleanup path.
				hw, hwErr := s.jobTaskRepo.HasUndispatchedBaseKeyspace(ctx, jobID)
				if hwErr != nil {
					debug.Log("HasUndispatchedBaseKeyspace failed in pending check", map[string]interface{}{
						"job_id": jobID,
						"error":  hwErr.Error(),
					})
					hasRemainingWork = true
				} else {
					hasRemainingWork = hw
				}
			} else if job.EffectiveKeyspace != nil && job.EffectiveKeyspace.IsPositive() {
				// Rule-split / no-base-keyspace fallback (effective units).
				hasRemainingWork = job.DispatchedKeyspace.Cmp(*job.EffectiveKeyspace) < 0
			}

			if hasRemainingWork {
				err = s.jobExecutionRepo.UpdateStatus(ctx, jobID, models.JobExecutionStatusPending)
				if err != nil {
					debug.Log("Failed to update job status to pending", map[string]interface{}{
						"job_id": jobID,
						"error":  err.Error(),
					})
					return
				}

				debug.Log("Updated job status to pending - no active tasks but work remains", map[string]interface{}{
					"job_id":     jobID,
					"dispatched": job.DispatchedKeyspace,
					"effective":  job.EffectiveKeyspace,
				})
			} else {
				// Check if ALL tasks failed (no completed tasks at all)
				// This prevents marking jobs as "completed" when all work actually failed
				hasCompletedTasks := false
				hasFailedTasks := false
				for _, task := range allTasks {
					if task.Status == models.JobTaskStatusCompleted {
						hasCompletedTasks = true
					}
					if task.Status == models.JobTaskStatusFailed {
						hasFailedTasks = true
					}
				}

				// If ALL tasks failed with none completed, mark job as FAILED not completed
				if hasFailedTasks && !hasCompletedTasks {
					err = s.jobExecutionRepo.FailExecution(ctx, jobID, "All tasks failed")
					if err != nil {
						debug.Log("Failed to update job status to failed", map[string]interface{}{
							"job_id": jobID,
							"error":  err.Error(),
						})
						return
					}

					debug.Log("Updated job status to failed - all tasks failed, no completed tasks", map[string]interface{}{
						"job_id": jobID,
					})
					return
				}

				// Job is complete - no active tasks and no remaining work (and has completed tasks)
				err = s.jobExecutionRepo.UpdateStatus(ctx, jobID, models.JobExecutionStatusCompleted)
				if err != nil {
					debug.Log("Failed to update job status to completed", map[string]interface{}{
						"job_id": jobID,
						"error":  err.Error(),
					})
					return
				}

				debug.Log("Updated job status to completed - no active tasks and no remaining work", map[string]interface{}{
					"job_id": jobID,
				})
			}
		}
	}
}

// reconcileStuckJobs uses structural checks to identify and complete jobs that are stuck.
// This is a safety net that catches jobs where effective_keyspace drifted from potfile/wordlist updates.
func (s *JobCleanupService) reconcileStuckJobs(ctx context.Context) {
	// Find jobs that haven't been updated in 5+ minutes with no active tasks
	stuckJobs, err := s.jobExecutionRepo.GetPotentiallyStuckJobs(ctx, 5)
	if err != nil {
		debug.Log("Failed to get potentially stuck jobs", map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	if len(stuckJobs) == 0 {
		return
	}

	debug.Log("Found potentially stuck jobs for reconciliation", map[string]interface{}{
		"count": len(stuckJobs),
	})

	for _, job := range stuckJobs {
		// Skip if job is already completed
		if job.Status == models.JobExecutionStatusCompleted {
			continue
		}

		// First verify all tasks are truly complete (double-check the repository query)
		allComplete, err := s.jobTaskRepo.AreAllTasksComplete(ctx, job.ID)
		if err != nil {
			debug.Log("Failed to check task completion for stuck job", map[string]interface{}{
				"job_id": job.ID,
				"error":  err.Error(),
			})
			continue
		}

		if !allComplete {
			// Job has remaining work to dispatch, transition to pending
			if job.Status == models.JobExecutionStatusRunning {
				err = s.jobExecutionRepo.UpdateStatus(ctx, job.ID, models.JobExecutionStatusPending)
				if err == nil {
					debug.Log("Transitioned stuck job to pending - has remaining work", map[string]interface{}{
						"job_id": job.ID,
						"name":   job.Name,
					})
				}
			}
			continue
		}

		// All tasks complete AND structural checks pass - use structural completion logic
		shouldComplete := s.shouldJobCompleteStructural(ctx, &job)

		if shouldComplete {
			// CRITICAL: Check for failed tasks before completing
			hasFailed, failErr := s.jobTaskRepo.HasFailedTasks(ctx, job.ID)
			if failErr != nil {
				debug.Warning("Cleanup service: failed to check for failed tasks for job", map[string]interface{}{
					"job_id": job.ID,
					"error":  failErr.Error(),
				})
				continue
			}
			if hasFailed {
				debug.Log("Cleanup service: Job has failed tasks - marking as failed", map[string]interface{}{
					"job_id": job.ID,
				})
				if failExecErr := s.jobExecutionRepo.FailExecution(ctx, job.ID, "One or more tasks failed"); failExecErr != nil {
					debug.Error("Cleanup service failed to mark job as failed: %v", failExecErr)
				} else {
					debug.Info("Cleanup service marked job %s as failed due to failed tasks", job.ID)
				}
				continue
			}

			// Sync keyspace before completing — LEGACY path. Reads
			// chunk_actual_keyspace which the v2 dispatcher doesn't
			// populate, so for v2 jobs the SUM either lies or picks up
			// stale legacy values and overwrites the job's
			// effective_keyspace with a wrong (base-units) value.
			// Skip the sync entirely for v2-owned jobs — v2's cycle
			// and progress path own those keyspace fields.
			isV2Job, v2Err := s.jobExecutionRepo.IsSchedulerV2Job(ctx, job.ID)
			if v2Err != nil {
				debug.Warning("cleanup-service: v2 detection failed for job %s: %v (defaulting to legacy keyspace sync)", job.ID, v2Err)
			}
			if !isV2Job {
				actualKeyspace, err := s.jobTaskRepo.GetSumChunkActualKeyspace(ctx, job.ID)
				if err == nil && actualKeyspace.IsPositive() {
					if job.EffectiveKeyspace == nil || job.EffectiveKeyspace.Cmp(actualKeyspace) != 0 {
						s.jobExecutionRepo.UpdateEffectiveKeyspace(ctx, job.ID, actualKeyspace)
						s.jobExecutionRepo.UpdateDispatchedKeyspace(ctx, job.ID, actualKeyspace)
						debug.Log("Synced keyspace for stuck job before completion", map[string]interface{}{
							"job_id":          job.ID,
							"actual_keyspace": actualKeyspace,
						})
					}
				}
			}

			// Mark job as complete
			err = s.jobExecutionRepo.CompleteExecution(ctx, job.ID)
			if err != nil {
				debug.Log("Failed to complete stuck job", map[string]interface{}{
					"job_id": job.ID,
					"error":  err.Error(),
				})
				continue
			}

			debug.Log("Reconciliation completed stuck job via structural check", map[string]interface{}{
				"job_id":             job.ID,
				"name":               job.Name,
				"base_keyspace":      job.BaseKeyspace,
				"effective_keyspace": job.EffectiveKeyspace,
			})
		} else {
			// Structural check says more work needed but all tasks are complete
			// This shouldn't happen normally - transition to pending for investigation
			if job.Status == models.JobExecutionStatusRunning {
				err = s.jobExecutionRepo.UpdateStatus(ctx, job.ID, models.JobExecutionStatusPending)
				if err == nil {
					debug.Warning("Stuck job has all tasks complete but structural check failed - transitioned to pending for investigation",
						map[string]interface{}{
							"job_id":        job.ID,
							"name":          job.Name,
							"base_keyspace": job.BaseKeyspace,
							"dispatched":    job.DispatchedKeyspace,
						})
				}
			}
		}
	}
}

// shouldJobCompleteStructural performs structural checks to determine if a job should be marked complete.
// This is called after AreAllTasksComplete() returns true, so dispatch validation is already done.
// We only need additional checks for rule-split jobs.
func (s *JobCleanupService) shouldJobCompleteStructural(ctx context.Context, job *models.JobExecution) bool {
	// For increment mode jobs, rely on AreAllTasksComplete which handles increment layers
	if job.IncrementMode != "" && job.IncrementMode != "off" {
		return true
	}

	// AreAllTasksComplete() already validates dispatch
	// using effective_keyspace comparison. No additional structural check needed.
	// Note: We intentionally do NOT compare base_keyspace here because base_keyspace
	// (wordlist size) and effective_keyspace (total candidates) are different dimensions.
	return true
}

// checkForOrphanedRunningJobs finds and fixes jobs stuck in running with no active tasks
func (s *JobCleanupService) checkForOrphanedRunningJobs(ctx context.Context) {
	// Get all running jobs
	runningJobs, err := s.jobExecutionRepo.GetJobsByStatus(ctx, models.JobExecutionStatusRunning)
	if err != nil {
		debug.Log("Failed to get running jobs for orphan check", map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	for _, job := range runningJobs {
		// Check each running job for active tasks
		allTasks, err := s.jobTaskRepo.GetTasksByJobExecution(ctx, job.ID)
		if err != nil {
			debug.Log("Failed to get tasks for orphan check", map[string]interface{}{
				"job_id": job.ID,
				"error":  err.Error(),
			})
			continue
		}

		// Count different types of tasks
		runningOrAssignedCount := 0
		pendingCount := 0
		for _, task := range allTasks {
			if task.Status == models.JobTaskStatusRunning ||
				task.Status == models.JobTaskStatusAssigned ||
				task.Status == models.JobTaskStatusReconnectPending {
				runningOrAssignedCount++
			} else if task.Status == models.JobTaskStatusPending {
				pendingCount++
			}
		}

		// If no running/assigned tasks but has pending tasks, check if job is stuck
		if runningOrAssignedCount == 0 && pendingCount > 0 {
			// Check how long the job has been in this state
			// If the job hasn't been updated in 5+ minutes with only pending tasks, it's stuck
			timeSinceUpdate := time.Since(job.UpdatedAt)
			if timeSinceUpdate > 5*time.Minute {
				debug.Log("Found orphaned running job stuck with only pending tasks", map[string]interface{}{
					"job_id":            job.ID,
					"name":              job.Name,
					"pending_tasks":     pendingCount,
					"time_since_update": timeSinceUpdate.String(),
				})

				// Transition job back to pending so scheduler can handle it
				err = s.jobExecutionRepo.UpdateStatus(ctx, job.ID, models.JobExecutionStatusPending)
				if err != nil {
					debug.Log("Failed to update orphaned job status to pending", map[string]interface{}{
						"job_id": job.ID,
						"error":  err.Error(),
					})
					continue
				}

				debug.Log("Successfully marked orphaned job as pending for rescheduling", map[string]interface{}{
					"job_id": job.ID,
					"name":   job.Name,
				})
			}
		} else if runningOrAssignedCount == 0 && pendingCount == 0 {
			// No active tasks at all - completely orphaned
			debug.Log("Found orphaned running job with no active tasks", map[string]interface{}{
				"job_id":      job.ID,
				"name":        job.Name,
				"total_tasks": len(allTasks),
			})

			// Use the helper function to handle the transition
			s.checkJobForPendingTransition(ctx, job.ID)
		}
	}
}

// updateJobConsecutiveFailures updates the consecutive failure count for a job
func (s *JobCleanupService) updateJobConsecutiveFailures(ctx context.Context, jobExecutionID uuid.UUID, failed bool) {
	jobExecution, err := s.jobExecutionRepo.GetByID(ctx, jobExecutionID)
	if err != nil {
		debug.Log("Failed to get job execution for failure tracking", map[string]interface{}{
			"job_execution_id": jobExecutionID,
			"error":            err.Error(),
		})
		return
	}

	if failed {
		// Increment consecutive failures
		newCount := jobExecution.ConsecutiveFailures + 1
		err = s.jobExecutionRepo.UpdateConsecutiveFailures(ctx, jobExecutionID, newCount)
		if err != nil {
			debug.Log("Failed to update job consecutive failures", map[string]interface{}{
				"job_execution_id": jobExecutionID,
				"error":            err.Error(),
			})
			return
		}

		// Check if we've hit the threshold (3 consecutive failures)
		if newCount >= 3 {
			// Mark the entire job as failed
			err = s.jobExecutionRepo.UpdateStatus(ctx, jobExecutionID, models.JobExecutionStatusFailed)
			if err != nil {
				debug.Log("Failed to mark job as failed", map[string]interface{}{
					"job_execution_id": jobExecutionID,
					"error":            err.Error(),
				})
				return
			}

			errorMsg := fmt.Sprintf("Job failed due to %d consecutive task failures", newCount)
			err = s.jobExecutionRepo.UpdateErrorMessage(ctx, jobExecutionID, errorMsg)
			if err != nil {
				debug.Log("Failed to update job error message", map[string]interface{}{
					"job_execution_id": jobExecutionID,
					"error":            err.Error(),
				})
			}

			debug.Log("Marked job as failed due to consecutive failures", map[string]interface{}{
				"job_execution_id":     jobExecutionID,
				"consecutive_failures": newCount,
			})
		}
	} else {
		// Reset consecutive failures on success
		if jobExecution.ConsecutiveFailures > 0 {
			err = s.jobExecutionRepo.UpdateConsecutiveFailures(ctx, jobExecutionID, 0)
			if err != nil {
				debug.Log("Failed to reset job consecutive failures", map[string]interface{}{
					"job_execution_id": jobExecutionID,
					"error":            err.Error(),
				})
			}
		}
	}
}

// updateAgentConsecutiveFailures updates the consecutive failure count for an agent
func (s *JobCleanupService) updateAgentConsecutiveFailures(ctx context.Context, agentID int, failed bool) {
	agent, err := s.agentRepo.GetByID(ctx, agentID)
	if err != nil {
		debug.Log("Failed to get agent for failure tracking", map[string]interface{}{
			"agent_id": agentID,
			"error":    err.Error(),
		})
		return
	}

	if failed {
		// Increment consecutive failures
		newCount := agent.ConsecutiveFailures + 1
		err = s.agentRepo.UpdateConsecutiveFailures(ctx, agentID, newCount)
		if err != nil {
			debug.Log("Failed to update agent consecutive failures", map[string]interface{}{
				"agent_id": agentID,
				"error":    err.Error(),
			})
			return
		}

		// Check if we've hit the threshold (3 consecutive failures)
		if newCount >= 3 {
			// Mark the agent as unhealthy/error state
			errorMsg := fmt.Sprintf("Agent has %d consecutive task failures", newCount)
			err = s.agentRepo.UpdateStatus(ctx, agentID, models.AgentStatusError, &errorMsg)
			if err != nil {
				debug.Log("Failed to mark agent as error state", map[string]interface{}{
					"agent_id": agentID,
					"error":    err.Error(),
				})
				return
			}

			debug.Log("Marked agent as error state due to consecutive failures", map[string]interface{}{
				"agent_id":             agentID,
				"consecutive_failures": newCount,
			})
		}
	} else {
		// Reset consecutive failures on success
		if agent.ConsecutiveFailures > 0 {
			err = s.agentRepo.UpdateConsecutiveFailures(ctx, agentID, 0)
			if err != nil {
				debug.Log("Failed to reset agent consecutive failures", map[string]interface{}{
					"agent_id": agentID,
					"error":    err.Error(),
				})
			}
		}
	}
}
