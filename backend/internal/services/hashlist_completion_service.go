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
	db                 *db.DB
	jobExecRepo        *repository.JobExecutionRepository
	jobTaskRepo        *repository.JobTaskRepository
	hashlistRepo       *repository.HashListRepository
	notificationService *NotificationService
	wsHandler          WSHandler
}

// NewHashlistCompletionService creates a new hashlist completion service
func NewHashlistCompletionService(
	database *db.DB,
	jobExecRepo *repository.JobExecutionRepository,
	jobTaskRepo *repository.JobTaskRepository,
	hashlistRepo *repository.HashListRepository,
	notificationService *NotificationService,
	wsHandler WSHandler,
) *HashlistCompletionService {
	return &HashlistCompletionService{
		db:                 database,
		jobExecRepo:        jobExecRepo,
		jobTaskRepo:        jobTaskRepo,
		hashlistRepo:       hashlistRepo,
		notificationService: notificationService,
		wsHandler:          wsHandler,
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
			debug.Info("Job %s (%s) has %d tasks - marking as completed", job.ID, job.Name, taskCount)

			// Stop any active tasks
			stoppedCount, err := s.stopJobTasks(ctx, job.ID, triggeringTaskID)
			if err != nil {
				debug.Error("Failed to stop tasks for job %s: %v", job.ID, err)
				// Continue anyway - best effort
			} else if stoppedCount > 0 {
				debug.Info("Stopped %d active tasks for job %s", stoppedCount, job.ID)
			}

			// Mark job as completed with 100% progress
			err = s.completeJob(ctx, &job)
			if err != nil {
				debug.Error("Failed to complete job %s: %v", job.ID, err)
				jobsFailed++
				continue
			}

			jobsCompleted++
			debug.Info("Job %s (%s) marked as completed (all hashes cracked)", job.ID, job.Name)

		} else {
			// Job has no tasks - it never started
			debug.Info("Job %s (%s) has no tasks - deleting (never started)", job.ID, job.Name)

			err = s.jobExecRepo.Delete(ctx, job.ID)
			if err != nil {
				debug.Error("Failed to delete unstarted job %s: %v", job.ID, err)
				jobsFailed++
				continue
			}

			jobsDeleted++
			debug.Info("Job %s (%s) deleted (never started, hashlist fully cracked)", job.ID, job.Name)
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
			// Check if task has completed its keyspace (race condition: task finished but status not updated yet)
			// If keyspace is complete, mark as completed instead of cancelling
			if task.KeyspaceEnd > 0 && task.KeyspaceProcessed >= task.KeyspaceEnd {
				debug.Info("Task %s has completed its keyspace (%d/%d), marking as completed instead of cancelled",
					task.ID, task.KeyspaceProcessed, task.KeyspaceEnd)

				if err := s.jobTaskRepo.UpdateStatus(ctx, task.ID, models.JobTaskStatusCompleted); err != nil {
					debug.Error("Failed to mark task %s as completed: %v", task.ID, err)
				}
				continue
			}

			// Also check effective keyspace for brute-force tasks
			if task.EffectiveKeyspaceEnd != nil && *task.EffectiveKeyspaceEnd > 0 &&
				task.EffectiveKeyspaceProcessed != nil && *task.EffectiveKeyspaceProcessed >= *task.EffectiveKeyspaceEnd {
				debug.Info("Task %s has completed its effective keyspace (%d/%d), marking as completed instead of cancelled",
					task.ID, *task.EffectiveKeyspaceProcessed, *task.EffectiveKeyspaceEnd)

				if err := s.jobTaskRepo.UpdateStatus(ctx, task.ID, models.JobTaskStatusCompleted); err != nil {
					debug.Error("Failed to mark task %s as completed: %v", task.ID, err)
				}
				continue
			}

			// Task hasn't finished - send stop signal and cancel it
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

			// Send stop signal to the agent
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

			// Update task status to cancelled
			if err := s.jobTaskRepo.UpdateStatus(ctx, task.ID, models.JobTaskStatusCancelled); err != nil {
				debug.Error("Failed to update task %s status to cancelled: %v", task.ID, err)
			}
		}
	}

	if stoppedCount > 0 {
		debug.Info("Sent stop signals for %d tasks of job %s (hashlist fully cracked)", stoppedCount, jobID)
	}

	return stoppedCount, nil
}

// completeJob marks a job as completed with 100% progress
func (s *HashlistCompletionService) completeJob(ctx context.Context, job *models.JobExecution) error {
	// Mark job as completed (this also sets completed_at)
	err := s.jobExecRepo.CompleteExecution(ctx, job.ID)
	if err != nil {
		return fmt.Errorf("failed to complete job execution: %w", err)
	}

	// Note: Job-level progress is now calculated by the polling service (JobProgressCalculationService)
	// which runs every 2 seconds and recalculates from task data

	// TODO: Re-enable with special "hashlist fully cracked" email template
	// Temporarily disabled to prevent duplicate emails (job_execution_service also sends completion email)
	// When re-enabled, use a special template for "all hashes in hashlist cracked" scenario
	// if s.notificationService != nil && job.CreatedBy != nil {
	// 	// Send job completion email notification
	// 	err = s.notificationService.SendJobCompletionEmail(ctx, job.ID, *job.CreatedBy)
	// 	if err != nil {
	// 		debug.Warning("Failed to send job completion notification for job %s: %v", job.ID, err)
	// 		// Not critical, just log - user preferences might not be set or email disabled
	// 	} else {
	// 		debug.Info("Sent job completion notification for job %s", job.ID)
	// 	}
	// }

	return nil
}

// StopJobTasks is a public method that stops all tasks for a job (for use by other handlers)
func (s *HashlistCompletionService) StopJobTasks(ctx context.Context, jobID uuid.UUID) (int, error) {
	return s.stopJobTasks(ctx, jobID, nil)
}
