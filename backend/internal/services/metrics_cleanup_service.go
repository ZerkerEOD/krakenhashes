package services

import (
	"context"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// MetricsCleanupService handles cleanup of old metrics based on retention settings
type MetricsCleanupService struct {
	benchmarkRepo      *repository.BenchmarkRepository
	systemSettingsRepo *repository.SystemSettingsRepository
}

// NewMetricsCleanupService creates a new metrics cleanup service
func NewMetricsCleanupService(benchmarkRepo *repository.BenchmarkRepository, systemSettingsRepo *repository.SystemSettingsRepository) *MetricsCleanupService {
	return &MetricsCleanupService{
		benchmarkRepo:      benchmarkRepo,
		systemSettingsRepo: systemSettingsRepo,
	}
}

// StartCleanupScheduler starts the periodic cleanup process
func (s *MetricsCleanupService) StartCleanupScheduler(ctx context.Context) {
	// Run cleanup immediately on startup
	s.runCleanup(ctx)

	// Schedule cleanup to run daily at 2 AM
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			debug.Info("Metrics cleanup scheduler stopped")
			return
		case <-ticker.C:
			s.runCleanup(ctx)
		}
	}
}

// runCleanup performs the actual cleanup based on retention settings
func (s *MetricsCleanupService) runCleanup(ctx context.Context) {
	debug.Info("Starting metrics cleanup")

	// Get retention settings
	retentionDays, err := s.getRetentionDays(ctx)
	if err != nil {
		debug.Error("Failed to get retention settings: %v", err)
		return
	}

	// If retention is unlimited (0), don't delete anything
	if retentionDays == 0 {
		debug.Info("Metrics retention is unlimited, skipping cleanup")
		return
	}

	// Calculate cutoff date
	cutoffDate := time.Now().AddDate(0, 0, -retentionDays)
	debug.Info("Cleaning up metrics older than %s (%d days)", cutoffDate.Format("2006-01-02"), retentionDays)

	// Delete old realtime metrics
	err = s.benchmarkRepo.DeleteOldMetrics(ctx, models.AggregationLevelRealtime, cutoffDate)
	if err != nil {
		debug.Error("Failed to delete old realtime metrics: %v", err)
	}

	// Check if aggregation is enabled
	enableAggregation, err := s.isAggregationEnabled(ctx)
	if err != nil {
		debug.Error("Failed to check aggregation settings: %v", err)
		enableAggregation = true // Default to enabled
	}

	if enableAggregation {
		// Aggregate metrics before deletion
		s.performAggregation(ctx)
	}

	// Clean up old aggregated data (keep for longer)
	// Daily aggregations kept for 6 months
	dailyCutoff := time.Now().AddDate(0, -6, 0)
	err = s.benchmarkRepo.DeleteOldMetrics(ctx, models.AggregationLevelDaily, dailyCutoff)
	if err != nil {
		debug.Error("Failed to delete old daily metrics: %v", err)
	}

	// Weekly aggregations kept for 2 years
	weeklyCutoff := time.Now().AddDate(-2, 0, 0)
	err = s.benchmarkRepo.DeleteOldMetrics(ctx, models.AggregationLevelWeekly, weeklyCutoff)
	if err != nil {
		debug.Error("Failed to delete old weekly metrics: %v", err)
	}

	// Clean up old benchmark history
	benchRetentionDays, err := s.getBenchmarkHistoryRetentionDays(ctx)
	if err != nil {
		debug.Error("Failed to get benchmark history retention: %v", err)
	} else if benchRetentionDays > 0 {
		benchCutoff := time.Now().AddDate(0, 0, -benchRetentionDays)
		err = s.benchmarkRepo.DeleteOldBenchmarkHistory(ctx, benchCutoff)
		if err != nil {
			debug.Error("Failed to delete old benchmark history: %v", err)
		} else {
			debug.Info("Cleaned up benchmark history older than %d days", benchRetentionDays)
		}
	}

	debug.Info("Metrics cleanup completed")
}

// performAggregation aggregates realtime metrics to daily and weekly levels
func (s *MetricsCleanupService) performAggregation(ctx context.Context) {
	debug.Info("Starting metrics aggregation")

	// Aggregate realtime to daily for data older than 24 hours
	dayOldData := time.Now().Add(-24 * time.Hour)
	err := s.benchmarkRepo.AggregateMetrics(ctx, models.AggregationLevelRealtime, models.AggregationLevelDaily, dayOldData)
	if err != nil {
		debug.Error("Failed to aggregate realtime to daily: %v", err)
	}

	// Aggregate daily to weekly for data older than 7 days
	weekOldData := time.Now().AddDate(0, 0, -7)
	err = s.benchmarkRepo.AggregateMetrics(ctx, models.AggregationLevelDaily, models.AggregationLevelWeekly, weekOldData)
	if err != nil {
		debug.Error("Failed to aggregate daily to weekly: %v", err)
	}

	debug.Info("Metrics aggregation completed")
}

// getRetentionDays retrieves the metrics retention setting from system settings
func (s *MetricsCleanupService) getRetentionDays(ctx context.Context) (int, error) {
	// Get real-time retention days (primary retention setting)
	setting, err := s.systemSettingsRepo.GetSetting(ctx, "metrics_retention_realtime_days")
	if err != nil {
		// If setting doesn't exist, default to 7 days
		return 7, nil
	}

	// Check if value is nil
	if setting.Value == nil {
		return 7, nil
	}

	var days int
	_, err = fmt.Sscanf(*setting.Value, "%d", &days)
	if err != nil {
		return 7, fmt.Errorf("invalid retention days value: %s", *setting.Value)
	}

	return days, nil
}

// getBenchmarkHistoryRetentionDays retrieves the benchmark history retention setting
func (s *MetricsCleanupService) getBenchmarkHistoryRetentionDays(ctx context.Context) (int, error) {
	setting, err := s.systemSettingsRepo.GetSetting(ctx, "benchmark_history_retention_days")
	if err != nil {
		return 365, nil // Default 1 year
	}
	if setting.Value == nil {
		return 365, nil
	}
	var days int
	_, err = fmt.Sscanf(*setting.Value, "%d", &days)
	if err != nil {
		return 365, fmt.Errorf("invalid benchmark history retention value: %s", *setting.Value)
	}
	return days, nil
}

// isAggregationEnabled checks if metrics aggregation is enabled
func (s *MetricsCleanupService) isAggregationEnabled(ctx context.Context) (bool, error) {
	setting, err := s.systemSettingsRepo.GetSetting(ctx, "enable_aggregation")
	if err != nil {
		// Default to enabled if setting doesn't exist
		return true, nil
	}

	// Check if value is nil
	if setting.Value == nil {
		return true, nil // Default to enabled
	}
	
	return *setting.Value == "true", nil
}