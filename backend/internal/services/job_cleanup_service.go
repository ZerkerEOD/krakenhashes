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

// JobCleanupService handles cleanup of stale jobs and tasks
type JobCleanupService struct {
	jobExecutionRepo   *repository.JobExecutionRepository
	jobTaskRepo        *repository.JobTaskRepository
	systemSettingsRepo *repository.SystemSettingsRepository
	agentRepo          *repository.AgentRepository
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

// CleanupStaleTasksOnStartup cleans up tasks that were left in an incomplete state
func (s *JobCleanupService) CleanupStaleTasksOnStartup(ctx context.Context) error {
	debug.Info("Starting cleanup of stale tasks on startup with grace period for reconnection")

	// FIRST: Check for orphaned running jobs (jobs with no active tasks)
	// This must run regardless of whether there are stale tasks
	debug.Info("Checking for orphaned running jobs on startup")
	s.checkForOrphanedRunningJobs(ctx)

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
			// Check if job has remaining keyspace to process
			hasRemainingWork := false

			// For jobs with effective keyspace, check if it's been fully dispatched
			// Note: DispatchedKeyspace now stores proportional effective keyspace for keyspace-split tasks
			if job.EffectiveKeyspace != nil && *job.EffectiveKeyspace > 0 {
				hasRemainingWork = job.DispatchedKeyspace < *job.EffectiveKeyspace
			} else if job.BaseKeyspace != nil && *job.BaseKeyspace > 0 {
				// Fallback for jobs that haven't received effective keyspace yet
				// This ensures keyspace-split jobs don't complete prematurely
				hasRemainingWork = job.DispatchedKeyspace < *job.BaseKeyspace
			} else if job.TotalKeyspace != nil && *job.TotalKeyspace > 0 {
				// Legacy fallback for older jobs
				hasRemainingWork = job.ProcessedKeyspace < *job.TotalKeyspace
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
					"job_id": jobID,
					"dispatched": job.DispatchedKeyspace,
					"effective": job.EffectiveKeyspace,
				})
			} else {
				// Job is complete - no active tasks and no remaining work
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
					"job_id": job.ID,
					"name": job.Name,
					"pending_tasks": pendingCount,
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
					"name": job.Name,
				})
			}
		} else if runningOrAssignedCount == 0 && pendingCount == 0 {
			// No active tasks at all - completely orphaned
			debug.Log("Found orphaned running job with no active tasks", map[string]interface{}{
				"job_id": job.ID,
				"name": job.Name,
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
