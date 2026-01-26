package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

// BenchmarkRepository handles database operations for benchmarks
type BenchmarkRepository struct {
	db *db.DB
}

// NewBenchmarkRepository creates a new benchmark repository
func NewBenchmarkRepository(db *db.DB) *BenchmarkRepository {
	return &BenchmarkRepository{db: db}
}

// CreateOrUpdateAgentBenchmark creates or updates an agent benchmark
// salt_count is used for salted hash types where benchmark speed varies with salt count
func (r *BenchmarkRepository) CreateOrUpdateAgentBenchmark(ctx context.Context, benchmark *models.AgentBenchmark) error {
	query := `
		INSERT INTO agent_benchmarks (agent_id, attack_mode, hash_type, salt_count, speed)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (agent_id, attack_mode, hash_type, salt_count)
		DO UPDATE SET speed = $5, updated_at = CURRENT_TIMESTAMP
		RETURNING id, created_at, updated_at`

	err := r.db.QueryRowContext(ctx, query,
		benchmark.AgentID,
		benchmark.AttackMode,
		benchmark.HashType,
		benchmark.SaltCount,
		benchmark.Speed,
	).Scan(&benchmark.ID, &benchmark.CreatedAt, &benchmark.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create or update agent benchmark: %w", err)
	}

	return nil
}

// GetAgentBenchmark retrieves a specific benchmark for an agent
// saltCount is used for salted hash types - use nil for non-salted hash types
// Uses IS NOT DISTINCT FROM for NULL-safe comparison of salt_count
func (r *BenchmarkRepository) GetAgentBenchmark(ctx context.Context, agentID int, attackMode models.AttackMode, hashType int, saltCount *int) (*models.AgentBenchmark, error) {
	query := `
		SELECT id, agent_id, attack_mode, hash_type, salt_count, speed, created_at, updated_at
		FROM agent_benchmarks
		WHERE agent_id = $1 AND attack_mode = $2 AND hash_type = $3 AND salt_count IS NOT DISTINCT FROM $4`

	var benchmark models.AgentBenchmark
	err := r.db.QueryRowContext(ctx, query, agentID, attackMode, hashType, saltCount).Scan(
		&benchmark.ID,
		&benchmark.AgentID,
		&benchmark.AttackMode,
		&benchmark.HashType,
		&benchmark.SaltCount,
		&benchmark.Speed,
		&benchmark.CreatedAt,
		&benchmark.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get agent benchmark: %w", err)
	}

	return &benchmark, nil
}

// GetAgentBenchmarks retrieves all benchmarks for an agent
func (r *BenchmarkRepository) GetAgentBenchmarks(ctx context.Context, agentID int) ([]models.AgentBenchmark, error) {
	query := `
		SELECT id, agent_id, attack_mode, hash_type, salt_count, speed, created_at, updated_at
		FROM agent_benchmarks
		WHERE agent_id = $1
		ORDER BY attack_mode, hash_type, salt_count NULLS FIRST`

	rows, err := r.db.QueryContext(ctx, query, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent benchmarks: %w", err)
	}
	defer rows.Close()

	var benchmarks []models.AgentBenchmark
	for rows.Next() {
		var benchmark models.AgentBenchmark
		err := rows.Scan(
			&benchmark.ID,
			&benchmark.AgentID,
			&benchmark.AttackMode,
			&benchmark.HashType,
			&benchmark.SaltCount,
			&benchmark.Speed,
			&benchmark.CreatedAt,
			&benchmark.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan agent benchmark: %w", err)
		}
		benchmarks = append(benchmarks, benchmark)
	}

	return benchmarks, nil
}

// IsRecentBenchmark checks if a benchmark is recent based on cache duration
// saltCount is used for salted hash types - use nil for non-salted hash types
// Uses IS NOT DISTINCT FROM for NULL-safe comparison of salt_count
func (r *BenchmarkRepository) IsRecentBenchmark(ctx context.Context, agentID int, attackMode models.AttackMode, hashType int, saltCount *int, cacheDuration time.Duration) (bool, error) {
	query := `
		SELECT updated_at
		FROM agent_benchmarks
		WHERE agent_id = $1 AND attack_mode = $2 AND hash_type = $3 AND salt_count IS NOT DISTINCT FROM $4`

	var updatedAt time.Time
	err := r.db.QueryRowContext(ctx, query, agentID, attackMode, hashType, saltCount).Scan(&updatedAt)

	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check benchmark recency: %w", err)
	}

	return time.Since(updatedAt) < cacheDuration, nil
}

// CreateAgentPerformanceMetric creates a new agent performance metric
func (r *BenchmarkRepository) CreateAgentPerformanceMetric(ctx context.Context, metric *models.AgentPerformanceMetric) error {
	query := `
		INSERT INTO agent_performance_metrics (
			agent_id, metric_type, value, timestamp, aggregation_level, period_start, period_end,
			device_id, device_name, task_id, attack_mode
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id`

	err := r.db.QueryRowContext(ctx, query,
		metric.AgentID,
		metric.MetricType,
		metric.Value,
		metric.Timestamp,
		metric.AggregationLevel,
		metric.PeriodStart,
		metric.PeriodEnd,
		metric.DeviceID,
		metric.DeviceName,
		metric.TaskID,
		metric.AttackMode,
	).Scan(&metric.ID)

	if err != nil {
		return fmt.Errorf("failed to create agent performance metric: %w", err)
	}

	return nil
}

// GetAgentMetrics retrieves metrics for an agent within a time range
func (r *BenchmarkRepository) GetAgentMetrics(ctx context.Context, agentID int, metricType models.MetricType, start, end time.Time, aggregationLevel models.AggregationLevel) ([]models.AgentPerformanceMetric, error) {
	query := `
		SELECT id, agent_id, metric_type, value, timestamp, aggregation_level, period_start, period_end,
		       device_id, device_name, task_id, attack_mode
		FROM agent_performance_metrics
		WHERE agent_id = $1 AND metric_type = $2 AND timestamp BETWEEN $3 AND $4 AND aggregation_level = $5
		ORDER BY timestamp ASC`

	rows, err := r.db.QueryContext(ctx, query, agentID, metricType, start, end, aggregationLevel)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent metrics: %w", err)
	}
	defer rows.Close()

	var metrics []models.AgentPerformanceMetric
	for rows.Next() {
		var metric models.AgentPerformanceMetric
		err := rows.Scan(
			&metric.ID,
			&metric.AgentID,
			&metric.MetricType,
			&metric.Value,
			&metric.Timestamp,
			&metric.AggregationLevel,
			&metric.PeriodStart,
			&metric.PeriodEnd,
			&metric.DeviceID,
			&metric.DeviceName,
			&metric.TaskID,
			&metric.AttackMode,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan agent metric: %w", err)
		}
		metrics = append(metrics, metric)
	}

	return metrics, nil
}

// GetAgentDeviceMetrics retrieves device metrics for an agent within a time range for multiple metric types
func (r *BenchmarkRepository) GetAgentDeviceMetrics(ctx context.Context, agentID int, metricTypes []models.MetricType, start, end time.Time) ([]models.AgentPerformanceMetric, error) {
	// Build placeholders for metric types
	placeholders := make([]string, len(metricTypes))
	args := make([]interface{}, 0, len(metricTypes)+3)
	args = append(args, agentID)
	
	for i, mt := range metricTypes {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args = append(args, mt)
	}
	
	args = append(args, start, end)
	
	query := fmt.Sprintf(`
		SELECT id, agent_id, metric_type, value, timestamp, aggregation_level, period_start, period_end,
		       device_id, device_name, task_id, attack_mode
		FROM agent_performance_metrics
		WHERE agent_id = $1 
		  AND metric_type IN (%s)
		  AND timestamp BETWEEN $%d AND $%d 
		  AND aggregation_level = 'realtime'
		  AND device_id IS NOT NULL
		ORDER BY timestamp ASC, device_id ASC, metric_type ASC`,
		strings.Join(placeholders, ", "),
		len(metricTypes)+2,
		len(metricTypes)+3,
	)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent device metrics: %w", err)
	}
	defer rows.Close()

	var metrics []models.AgentPerformanceMetric
	for rows.Next() {
		var metric models.AgentPerformanceMetric
		err := rows.Scan(
			&metric.ID,
			&metric.AgentID,
			&metric.MetricType,
			&metric.Value,
			&metric.Timestamp,
			&metric.AggregationLevel,
			&metric.PeriodStart,
			&metric.PeriodEnd,
			&metric.DeviceID,
			&metric.DeviceName,
			&metric.TaskID,
			&metric.AttackMode,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan agent device metric: %w", err)
		}
		metrics = append(metrics, metric)
	}

	return metrics, nil
}

// CreateJobPerformanceMetric creates a new job performance metric
func (r *BenchmarkRepository) CreateJobPerformanceMetric(ctx context.Context, metric *models.JobPerformanceMetric) error {
	query := `
		INSERT INTO job_performance_metrics (
			job_execution_id, metric_type, value, timestamp, aggregation_level, period_start, period_end
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`

	err := r.db.QueryRowContext(ctx, query,
		metric.JobExecutionID,
		metric.MetricType,
		metric.Value,
		metric.Timestamp,
		metric.AggregationLevel,
		metric.PeriodStart,
		metric.PeriodEnd,
	).Scan(&metric.ID)

	if err != nil {
		return fmt.Errorf("failed to create job performance metric: %w", err)
	}

	return nil
}

// GetJobMetrics retrieves metrics for a job execution within a time range
func (r *BenchmarkRepository) GetJobMetrics(ctx context.Context, jobExecutionID uuid.UUID, metricType models.JobMetricType, start, end time.Time, aggregationLevel models.AggregationLevel) ([]models.JobPerformanceMetric, error) {
	query := `
		SELECT id, job_execution_id, metric_type, value, timestamp, aggregation_level, period_start, period_end
		FROM job_performance_metrics
		WHERE job_execution_id = $1 AND metric_type = $2 AND timestamp BETWEEN $3 AND $4 AND aggregation_level = $5
		ORDER BY timestamp ASC`

	rows, err := r.db.QueryContext(ctx, query, jobExecutionID, metricType, start, end, aggregationLevel)
	if err != nil {
		return nil, fmt.Errorf("failed to get job metrics: %w", err)
	}
	defer rows.Close()

	var metrics []models.JobPerformanceMetric
	for rows.Next() {
		var metric models.JobPerformanceMetric
		err := rows.Scan(
			&metric.ID,
			&metric.JobExecutionID,
			&metric.MetricType,
			&metric.Value,
			&metric.Timestamp,
			&metric.AggregationLevel,
			&metric.PeriodStart,
			&metric.PeriodEnd,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job metric: %w", err)
		}
		metrics = append(metrics, metric)
	}

	return metrics, nil
}

// AggregateMetrics aggregates realtime metrics to daily or weekly
func (r *BenchmarkRepository) AggregateMetrics(ctx context.Context, fromLevel, toLevel models.AggregationLevel, before time.Time) error {
	// This would typically be a stored procedure or complex query
	// For now, we'll implement a simple aggregation

	var interval string
	switch toLevel {
	case models.AggregationLevelDaily:
		interval = "1 day"
	case models.AggregationLevelWeekly:
		interval = "7 days"
	default:
		return fmt.Errorf("invalid target aggregation level: %s", toLevel)
	}

	// Aggregate agent metrics
	agentQuery := fmt.Sprintf(`
		INSERT INTO agent_performance_metrics (agent_id, metric_type, value, timestamp, aggregation_level, period_start, period_end)
		SELECT 
			agent_id,
			metric_type,
			AVG(value) as value,
			date_trunc('day', MIN(timestamp)) + interval '%s' as timestamp,
			$1 as aggregation_level,
			MIN(timestamp) as period_start,
			MAX(timestamp) as period_end
		FROM agent_performance_metrics
		WHERE aggregation_level = $2 AND timestamp < $3
		GROUP BY agent_id, metric_type, date_trunc('day', timestamp)
		ON CONFLICT DO NOTHING`, interval)

	_, err := r.db.ExecContext(ctx, agentQuery, toLevel, fromLevel, before)
	if err != nil {
		return fmt.Errorf("failed to aggregate agent metrics: %w", err)
	}

	// Aggregate job metrics
	jobQuery := fmt.Sprintf(`
		INSERT INTO job_performance_metrics (job_execution_id, metric_type, value, timestamp, aggregation_level, period_start, period_end)
		SELECT 
			job_execution_id,
			metric_type,
			AVG(value) as value,
			date_trunc('day', MIN(timestamp)) + interval '%s' as timestamp,
			$1 as aggregation_level,
			MIN(timestamp) as period_start,
			MAX(timestamp) as period_end
		FROM job_performance_metrics
		WHERE aggregation_level = $2 AND timestamp < $3
		GROUP BY job_execution_id, metric_type, date_trunc('day', timestamp)
		ON CONFLICT DO NOTHING`, interval)

	_, err = r.db.ExecContext(ctx, jobQuery, toLevel, fromLevel, before)
	if err != nil {
		return fmt.Errorf("failed to aggregate job metrics: %w", err)
	}

	return nil
}

// DeleteOldMetrics deletes metrics older than the retention period
func (r *BenchmarkRepository) DeleteOldMetrics(ctx context.Context, aggregationLevel models.AggregationLevel, before time.Time) error {
	// Delete old agent metrics
	agentQuery := `DELETE FROM agent_performance_metrics WHERE aggregation_level = $1 AND timestamp < $2`
	_, err := r.db.ExecContext(ctx, agentQuery, aggregationLevel, before)
	if err != nil {
		return fmt.Errorf("failed to delete old agent metrics: %w", err)
	}

	// Delete old job metrics
	jobQuery := `DELETE FROM job_performance_metrics WHERE aggregation_level = $1 AND timestamp < $2`
	_, err = r.db.ExecContext(ctx, jobQuery, aggregationLevel, before)
	if err != nil {
		return fmt.Errorf("failed to delete old job metrics: %w", err)
	}

	return nil
}
