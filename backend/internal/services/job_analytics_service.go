package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
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

// GetSuccessRates returns success rate analytics grouped by job configuration,
// with preset matching and resolved wordlist/rule names
func (s *JobAnalyticsService) GetSuccessRates(ctx context.Context, filter *repository.JobAnalyticsFilter) ([]repository.SuccessRateEntry, error) {
	// 1. Get raw grouped data
	rows, err := s.repo.GetSuccessRates(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to get success rates: %w", err)
	}

	// 2. Build lookup maps
	wordlistNames, err := s.repo.GetWordlistNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get wordlist names: %w", err)
	}
	ruleNames, err := s.repo.GetRuleNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get rule names: %w", err)
	}
	presets, err := s.repo.GetPresetFingerprints(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get preset fingerprints: %w", err)
	}

	// 3. Enrich each row
	entries := make([]repository.SuccessRateEntry, 0, len(rows))
	for _, row := range rows {
		entry := repository.SuccessRateEntry{
			AttackMode:    row.AttackMode,
			HashType:      row.HashType,
			HashTypeName:  row.HashTypeName,
			Mask:          row.Mask,
			IncrementMode: row.IncrementMode,
			TotalRuns:     row.RunCount,
			TotalCracks:   row.TotalCracks,
			TotalHashes:   row.TotalHashes,
			SuccessRate:   row.SuccessRate,
			AvgDuration:   row.AvgDuration,
			TotalCompute:  row.TotalCompute,
		}

		if row.IncrementMin.Valid {
			v := int(row.IncrementMin.Int32)
			entry.IncrementMin = &v
		}
		if row.IncrementMax.Valid {
			v := int(row.IncrementMax.Int32)
			entry.IncrementMax = &v
		}

		// Attack mode label
		if label, ok := AttackModeLabels[row.AttackMode]; ok {
			entry.AttackModeLabel = label
		} else {
			entry.AttackModeLabel = fmt.Sprintf("Mode %d", row.AttackMode)
		}

		// Resolve wordlist and rule names
		wlNames := resolveResourceNames(row.WordlistIDs, wordlistNames)
		rlNames := resolveResourceNames(row.RuleIDs, ruleNames)
		entry.WordlistNames = strings.Join(wlNames, ", ")
		entry.RuleNames = strings.Join(rlNames, ", ")

		// Preset matching
		rowWLSorted := sortedJSONIDs(row.WordlistIDs)
		rowRLSorted := sortedJSONIDs(row.RuleIDs)

		for _, p := range presets {
			if p.AttackMode != row.AttackMode {
				continue
			}
			if p.Mask != row.Mask {
				continue
			}
			if p.IncrementMode != row.IncrementMode {
				continue
			}
			if !nullInt32Eq(p.IncrementMin, row.IncrementMin) || !nullInt32Eq(p.IncrementMax, row.IncrementMax) {
				continue
			}
			pWL := sortedJSONIDs(p.WordlistIDs)
			pRL := sortedJSONIDs(p.RuleIDs)
			if sliceEqual(pWL, rowWLSorted) && sliceEqual(pRL, rowRLSorted) {
				entry.IsPreset = true
				entry.PresetName = p.Name
				break
			}
		}

		// Build display name
		entry.DisplayName = buildDisplayName(entry, wlNames, rlNames)

		entries = append(entries, entry)
	}

	return entries, nil
}

// resolveResourceNames parses a JSONB ID array and resolves names from a lookup map
func resolveResourceNames(jsonIDs string, nameMap map[int]string) []string {
	ids := parseJSONIDArray(jsonIDs)
	names := make([]string, 0, len(ids))
	for _, idStr := range ids {
		id, err := strconv.Atoi(idStr)
		if err != nil {
			continue
		}
		if name, ok := nameMap[id]; ok {
			names = append(names, name)
		} else {
			names = append(names, fmt.Sprintf("ID:%d", id))
		}
	}
	return names
}

// parseJSONIDArray parses a JSONB text like ["1","3"] into a string slice
func parseJSONIDArray(jsonText string) []string {
	if jsonText == "" || jsonText == "[]" || jsonText == "null" {
		return nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(jsonText), &ids); err != nil {
		// Try parsing as int array in case IDs are stored as numbers
		var intIDs []int
		if err2 := json.Unmarshal([]byte(jsonText), &intIDs); err2 == nil {
			for _, id := range intIDs {
				ids = append(ids, strconv.Itoa(id))
			}
			return ids
		}
		return nil
	}
	return ids
}

// sortedJSONIDs parses and returns sorted ID strings for order-independent comparison
func sortedJSONIDs(jsonText string) []string {
	ids := parseJSONIDArray(jsonText)
	sort.Strings(ids)
	return ids
}

// sliceEqual compares two sorted string slices
func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// nullInt32Eq compares two sql.NullInt32 values
func nullInt32Eq(a, b sql.NullInt32) bool {
	if a.Valid != b.Valid {
		return false
	}
	if !a.Valid {
		return true // both NULL
	}
	return a.Int32 == b.Int32
}

// buildDisplayName constructs the user-facing name for a success rate entry
func buildDisplayName(entry repository.SuccessRateEntry, wlNames, rlNames []string) string {
	var configParts []string

	// Build resource description based on attack mode
	switch entry.AttackMode {
	case 3: // Brute-force
		if entry.Mask != "" {
			maskDesc := "Mask: " + entry.Mask
			if entry.IncrementMode != "" && entry.IncrementMode != "off" {
				if entry.IncrementMin != nil && entry.IncrementMax != nil {
					maskDesc += fmt.Sprintf(" (increment %d-%d)", *entry.IncrementMin, *entry.IncrementMax)
				}
			}
			configParts = append(configParts, maskDesc)
		}
	case 6, 7: // Hybrid modes
		if len(wlNames) > 0 {
			configParts = append(configParts, strings.Join(wlNames, ", "))
		}
		if entry.Mask != "" {
			configParts = append(configParts, "Mask: "+entry.Mask)
		}
	default: // Straight, Combination, Association
		if len(wlNames) > 0 {
			configParts = append(configParts, strings.Join(wlNames, ", "))
		}
		if len(rlNames) > 0 {
			configParts = append(configParts, strings.Join(rlNames, ", "))
		}
	}

	configDisplay := strings.Join(configParts, " + ")
	if configDisplay == "" {
		configDisplay = "Unknown Configuration"
	}

	if entry.IsPreset {
		return fmt.Sprintf("%s (%s)", entry.PresetName, configDisplay)
	}
	return configDisplay
}
