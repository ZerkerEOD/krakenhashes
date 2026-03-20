package services

import (
	"context"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/google/uuid"
)

// AttackModeLabels maps attack mode numbers to human-readable labels
var AttackModeLabels = map[int]string{
	0: "Straight",
	1: "Combination",
	3: "Brute-force",
	6: "Hybrid Wordlist+Mask",
	7: "Hybrid Mask+Wordlist",
	9: "Association",
}

// JobAnalyticsService provides business logic for job performance analytics
type JobAnalyticsService struct {
	repo          *repository.JobAnalyticsRepository
	benchmarkRepo *repository.BenchmarkRepository
}

// NewJobAnalyticsService creates a new job analytics service
func NewJobAnalyticsService(repo *repository.JobAnalyticsRepository, benchmarkRepo *repository.BenchmarkRepository) *JobAnalyticsService {
	return &JobAnalyticsService{repo: repo, benchmarkRepo: benchmarkRepo}
}

// GetFilterOptions returns available filter values with attack mode labels
func (s *JobAnalyticsService) GetFilterOptions(ctx context.Context) (*repository.FilterOptions, error) {
	opts, err := s.repo.GetFilterOptions(ctx)
	if err != nil {
		return nil, err
	}

	// Add attack mode labels
	for i := range opts.AttackModes {
		if label, ok := AttackModeLabels[opts.AttackModes[i].Value]; ok {
			opts.AttackModes[i].Label = label
		} else {
			opts.AttackModes[i].Label = "Unknown"
		}
	}

	return opts, nil
}

// GetSummary returns aggregate statistics
func (s *JobAnalyticsService) GetSummary(ctx context.Context, filter *repository.JobAnalyticsFilter) (*repository.JobAnalyticsSummary, error) {
	return s.repo.GetSummary(ctx, filter)
}

// GetJobsList returns paginated jobs with metrics
func (s *JobAnalyticsService) GetJobsList(ctx context.Context, filter *repository.JobAnalyticsFilter, page, pageSize int, sortBy, sortOrder string) ([]repository.JobAnalyticsEntry, int, error) {
	return s.repo.GetJobsList(ctx, filter, page, pageSize, sortBy, sortOrder)
}

// GetTimeline returns time series data
func (s *JobAnalyticsService) GetTimeline(ctx context.Context, filter *repository.JobAnalyticsFilter, resolution string) ([]repository.TimelinePoint, error) {
	return s.repo.GetTimeline(ctx, filter, resolution)
}

// GetJobTimeline returns detail for a single job
func (s *JobAnalyticsService) GetJobTimeline(ctx context.Context, jobID uuid.UUID) ([]repository.TimelinePoint, []repository.TaskSegment, error) {
	return s.repo.GetJobTimeline(ctx, jobID)
}

// GetBenchmarkHistory returns paginated benchmark history
func (s *JobAnalyticsService) GetBenchmarkHistory(ctx context.Context, agentID *int, hashType *int, attackMode *int, limit, offset int) ([]interface{}, int, error) {
	entries, total, err := s.benchmarkRepo.GetBenchmarkHistory(ctx, agentID, hashType, attackMode, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	// Convert to interface slice for JSON
	result := make([]interface{}, len(entries))
	for i, e := range entries {
		result[i] = e
	}
	return result, total, nil
}

// GetBenchmarkTrends returns benchmark speed data over time
func (s *JobAnalyticsService) GetBenchmarkTrends(ctx context.Context, agentID int, hashType *int, attackMode *int, since time.Time) (interface{}, error) {
	return s.benchmarkRepo.GetBenchmarkTrends(ctx, agentID, hashType, attackMode, since)
}
