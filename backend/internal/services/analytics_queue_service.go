package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// AnalyticsQueueService processes analytics reports in the background
type AnalyticsQueueService struct {
	analyticsService  *AnalyticsService
	repo              *repository.AnalyticsRepository
	stopChan          chan struct{}
	running           bool
	processing        bool
	mu                sync.Mutex
	pollInterval      time.Duration
	processingTimeout time.Duration
	// BloodHound staged-context deletion backstop.
	bloodhoundContextTTL time.Duration // how long a failed report's context may linger before the sweep clears it
	sweepInterval        time.Duration // minimum time between sweeps
	lastSweep            time.Time
}

// NewAnalyticsQueueService creates a new AnalyticsQueueService
func NewAnalyticsQueueService(analyticsService *AnalyticsService, repo *repository.AnalyticsRepository) *AnalyticsQueueService {
	return &AnalyticsQueueService{
		analyticsService:     analyticsService,
		repo:                 repo,
		stopChan:             make(chan struct{}),
		running:              false,
		processing:           false,
		pollInterval:         10 * time.Second, // Check for queued reports every 10 seconds
		processingTimeout:    60 * time.Minute, // Max 1 hour per report
		bloodhoundContextTTL: 30 * time.Minute, // Failed reports keep their context this long (for retry), then it's swept
		sweepInterval:        5 * time.Minute,  // Run the staged-context sweep at most this often
	}
}

// Start begins the background queue processing
func (s *AnalyticsQueueService) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("analytics queue service is already running")
	}

	s.running = true
	go s.processQueue()
	debug.Info("Analytics queue service started")

	return nil
}

// Stop halts the background queue processing
func (s *AnalyticsQueueService) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return fmt.Errorf("analytics queue service is not running")
	}

	close(s.stopChan)
	s.running = false
	debug.Info("Analytics queue service stopped")

	return nil
}

// IsProcessing returns true if a report is currently being processed
func (s *AnalyticsQueueService) IsProcessing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.processing
}

// processQueue is the main loop that checks for and processes queued reports
func (s *AnalyticsQueueService) processQueue() {
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			debug.Info("Analytics queue processing stopped")
			return
		case <-ticker.C:
			s.checkAndProcessNext()
		}
	}
}

// checkAndProcessNext checks for queued reports and processes the next one
func (s *AnalyticsQueueService) checkAndProcessNext() {
	// Backstop deletion of stale BloodHound-derived contexts (runs regardless of queue state).
	s.maybeSweepBloodhound()

	s.mu.Lock()
	if s.processing {
		s.mu.Unlock()
		return // Already processing a report
	}
	s.processing = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.processing = false
		s.mu.Unlock()
	}()

	ctx := context.Background()

	// Get queued reports
	queuedReports, err := s.repo.GetQueuedReports(ctx)
	if err != nil {
		debug.Error("Failed to get queued reports: %v", err)
		return
	}

	if len(queuedReports) == 0 {
		return // No reports to process
	}

	// Process the first report in the queue
	report := queuedReports[0]
	debug.Info("Processing analytics report %s for client %s", report.ID, report.ClientID)

	// Update status to processing
	if err := s.repo.UpdateStatus(ctx, report.ID, "processing"); err != nil {
		debug.Error("Failed to update report status to processing: %v", err)
		return
	}

	// Process the report with timeout
	ctx, cancel := context.WithTimeout(context.Background(), s.processingTimeout)
	defer cancel()

	if err := s.processReport(ctx, report); err != nil {
		debug.Error("Failed to process report %s: %v", report.ID, err)
		// Error is already handled in processReport
	}

	// Update queue positions for remaining reports
	if err := s.repo.UpdateQueuePositions(ctx); err != nil {
		debug.Error("Failed to update queue positions: %v", err)
	}
}

// processReport processes a single analytics report
func (s *AnalyticsQueueService) processReport(ctx context.Context, report *models.AnalyticsReport) error {
	// Generate analytics
	if err := s.analyticsService.GenerateAnalytics(ctx, report.ID); err != nil {
		// Update status to failed with error message
		if updateErr := s.repo.UpdateError(ctx, report.ID, err.Error()); updateErr != nil {
			debug.Error("Failed to update error message: %v", updateErr)
		}
		if updateErr := s.repo.UpdateStatus(ctx, report.ID, "failed"); updateErr != nil {
			debug.Error("Failed to update status to failed: %v", updateErr)
		}
		return fmt.Errorf("failed to generate analytics: %w", err)
	}

	// Update status to completed
	if err := s.repo.UpdateStatus(ctx, report.ID, "completed"); err != nil {
		debug.Error("Failed to update status to completed: %v", err)
		return fmt.Errorf("failed to update status: %w", err)
	}

	debug.Info("Successfully completed analytics report %s", report.ID)
	return nil
}

// maybeSweepBloodhound runs the staged-context deletion backstop at most once per sweepInterval. It
// clears BloodHound-derived context for completed reports (in case a per-report clear was missed or
// the process crashed) and for failed reports older than bloodhoundContextTTL.
func (s *AnalyticsQueueService) maybeSweepBloodhound() {
	s.mu.Lock()
	due := time.Since(s.lastSweep) >= s.sweepInterval
	if due {
		s.lastSweep = time.Now()
	}
	s.mu.Unlock()
	if !due {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	n, err := s.repo.SweepStaleBloodhoundContexts(ctx, s.bloodhoundContextTTL)
	if err != nil {
		debug.Warning("BloodHound context sweep failed: %v", err)
		return
	}
	if n > 0 {
		debug.Info("BloodHound context sweep cleared %d stale staged context(s)", n)
	}
}

// GetQueueStatus returns information about the current queue status
func (s *AnalyticsQueueService) GetQueueStatus(ctx context.Context) (map[string]interface{}, error) {
	queuedReports, err := s.repo.GetQueuedReports(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get queued reports: %w", err)
	}

	s.mu.Lock()
	processing := s.processing
	s.mu.Unlock()

	return map[string]interface{}{
		"queue_length":  len(queuedReports),
		"is_processing": processing,
	}, nil
}
