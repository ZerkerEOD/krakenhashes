package services

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	emailPkg "github.com/ZerkerEOD/krakenhashes/backend/internal/email"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// NotificationService handles notification operations
type NotificationService struct {
	db               *db.DB
	userRepo         *repository.UserRepository
	jobExecRepo      *repository.JobExecutionRepository
	jobTaskRepo      *repository.JobTaskRepository
	hashlistRepo     *repository.HashListRepository
	emailService     *emailPkg.Service
}

// NewNotificationService creates a new NotificationService
func NewNotificationService(dbConn *sql.DB) *NotificationService {
	database := &db.DB{DB: dbConn}
	return &NotificationService{
		db:           database,
		userRepo:     repository.NewUserRepository(database),
		jobExecRepo:  repository.NewJobExecutionRepository(database),
		jobTaskRepo:  repository.NewJobTaskRepository(database),
		hashlistRepo: repository.NewHashListRepository(database),
		emailService: emailPkg.NewService(dbConn),
	}
}

// SendJobCompletionEmail sends a job completion notification email
func (s *NotificationService) SendJobCompletionEmail(ctx context.Context, jobExecutionID uuid.UUID, userID uuid.UUID) error {
	// Get user details
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	// Check if user has notifications enabled
	if !user.NotifyOnJobCompletion {
		debug.Log("User has job completion notifications disabled", map[string]interface{}{
			"user_id": userID,
		})
		return nil
	}

	// Check if email provider is configured
	hasEmailProvider, err := s.db.HasActiveEmailProvider()
	if err != nil {
		return fmt.Errorf("failed to check email provider: %w", err)
	}
	if !hasEmailProvider {
		debug.Warning("No active email provider configured, skipping job completion email")
		return nil
	}

	// Get job execution details
	jobExec, err := s.jobExecRepo.GetByID(ctx, jobExecutionID)
	if err != nil {
		return fmt.Errorf("failed to get job execution: %w", err)
	}

	// Check if email has already been sent for this job
	if jobExec.CompletionEmailSent {
		debug.Log("Job completion email already sent", map[string]interface{}{
			"job_id": jobExecutionID,
			"sent_at": jobExec.CompletionEmailSentAt,
		})
		return nil
	}

	// Get hashlist details for statistics
	hashlist, err := s.hashlistRepo.GetByID(ctx, jobExec.HashlistID)
	if err != nil {
		return fmt.Errorf("failed to get hashlist: %w", err)
	}

	// Calculate statistics
	duration := ""
	if jobExec.StartedAt != nil && jobExec.CompletedAt != nil {
		dur := jobExec.CompletedAt.Sub(*jobExec.StartedAt)
		hours := int(dur.Hours())
		minutes := int(dur.Minutes()) % 60
		seconds := int(dur.Seconds()) % 60
		duration = fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}

	// Get crack count specific to this job execution (sum of all task crack_counts)
	crackedCount, err := s.jobTaskRepo.GetTotalCracksForJob(ctx, jobExecutionID)
	if err != nil {
		debug.Warning("Failed to get crack count for job %s: %v", jobExecutionID, err)
		crackedCount = 0 // Fall back to 0 if query fails
	}

	totalCount := hashlist.TotalHashes
	successRate := float64(0)
	if totalCount > 0 {
		successRate = float64(crackedCount) / float64(totalCount) * 100
	}

	// Use user's email address
	recipientEmail := user.Email

	// Get email template from database
	tmpl, err := s.emailService.GetTemplateByType(ctx, "job_completion")
	if err != nil {
		return fmt.Errorf("failed to get email template: %w", err)
	}

	// Prepare template data
	templateData := map[string]interface{}{
		"JobName":         jobExec.Name,
		"Duration":        duration,
		"HashesProcessed": totalCount,
		"CrackedCount":    crackedCount,
		"SuccessRate":     fmt.Sprintf("%.2f", successRate),
		"JobID":           jobExecutionID.String(),
		"HashlistName":    hashlist.Name,
	}

	// Send the email using the templated email method
	err = s.emailService.SendTemplatedEmail(ctx, recipientEmail, tmpl.ID, templateData)
	if err != nil {
		// Update job execution with email error
		errorMsg := err.Error()
		if updateErr := s.jobExecRepo.UpdateEmailStatus(ctx, jobExecutionID, false, nil, &errorMsg); updateErr != nil {
			debug.Error("Failed to update email error status: %v", updateErr)
		}
		return fmt.Errorf("failed to send job completion email: %w", err)
	}

	// Update job execution with successful email status
	now := time.Now()
	if err := s.jobExecRepo.UpdateEmailStatus(ctx, jobExecutionID, true, &now, nil); err != nil {
		debug.Error("Failed to update email success status: %v", err)
		// Don't fail the whole operation if we can't update the status
	}

	debug.Log("Job completion email sent successfully", map[string]interface{}{
		"recipient": recipientEmail,
		"job_id":    jobExecutionID,
	})
	return nil
}

// GetUserNotificationPreferences retrieves the notification preferences for a user
func (s *NotificationService) GetUserNotificationPreferences(ctx context.Context, userID uuid.UUID) (*models.NotificationPreferences, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Check if email provider is configured
	hasEmailProvider, err := s.db.HasActiveEmailProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to check email provider: %w", err)
	}

	prefs := &models.NotificationPreferences{
		NotifyOnJobCompletion: user.NotifyOnJobCompletion,
		EmailConfigured:       hasEmailProvider,
	}

	debug.Log("Retrieved notification preferences", map[string]interface{}{
		"user_id":                userID,
		"notify_on_completion":   prefs.NotifyOnJobCompletion,
		"email_configured":       prefs.EmailConfigured,
		"user_notify_value":      user.NotifyOnJobCompletion,
	})

	return prefs, nil
}

// UpdateUserNotificationPreferences updates the notification preferences for a user
func (s *NotificationService) UpdateUserNotificationPreferences(ctx context.Context, userID uuid.UUID, prefs *models.NotificationPreferences) error {
	// Check if email provider is configured when enabling notifications
	if prefs.NotifyOnJobCompletion {
		hasEmailProvider, err := s.db.HasActiveEmailProvider()
		if err != nil {
			return fmt.Errorf("failed to check email provider: %w", err)
		}
		if !hasEmailProvider {
			return fmt.Errorf("email notifications require an email gateway to be configured")
		}
	}

	debug.Log("Updating notification preferences", map[string]interface{}{
		"user_id":                userID,
		"notify_on_completion":   prefs.NotifyOnJobCompletion,
	})

	// Update user preferences
	err := s.userRepo.UpdateNotificationPreferences(ctx, userID, prefs.NotifyOnJobCompletion)
	if err != nil {
		return fmt.Errorf("failed to update notification preferences: %w", err)
	}

	debug.Log("Successfully updated notification preferences", map[string]interface{}{
		"user_id": userID,
	})

	return nil
}