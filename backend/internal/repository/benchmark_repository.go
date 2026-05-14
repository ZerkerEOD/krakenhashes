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

// CreateOrUpdateAgentBenchmark stores the latest benchmark for an agent.
//
// salt_count is non-nil only for salted hash types where benchmark speed
// varies with salt count. For non-salted hashes the value is NULL.
//
// Implementation note: the unique constraint on (agent_id, attack_mode,
// hash_type, salt_count) was created NULLS DISTINCT (Postgres default), so
// an `INSERT ... ON CONFLICT (..., salt_count) DO UPDATE` never fires when
// salt_count is NULL — every call would otherwise leak a fresh duplicate
// row. The EMA-update path at UpdateAgentBenchmarkEMA discovered the same
// pitfall and switched to a transactional SELECT-FOR-UPDATE + UPDATE-by-id;
// we mirror that here so the public store path is also a true upsert.
//
// Migration 000144 tightens the constraint to NULLS NOT DISTINCT going
// forward, but keeping this SELECT-then-modify pattern is still cheaper
// than relying on the constraint alone (it skips a wasted INSERT attempt
// when the row already exists) and survives a future schema change that
// might drop NULLS NOT DISTINCT.
func (r *BenchmarkRepository) CreateOrUpdateAgentBenchmark(ctx context.Context, benchmark *models.AgentBenchmark) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Lock the freshest matching row (if any). IS NOT DISTINCT FROM is
	// NULL-safe; the unique constraint is not, so concurrent callers may
	// briefly see no row even when one exists — FOR UPDATE serialises them.
	var existingID sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM agent_benchmarks
		WHERE agent_id = $1 AND attack_mode = $2 AND hash_type = $3
		  AND salt_count IS NOT DISTINCT FROM $4
		ORDER BY updated_at DESC
		LIMIT 1
		FOR UPDATE`,
		benchmark.AgentID, benchmark.AttackMode, benchmark.HashType, benchmark.SaltCount,
	).Scan(&existingID)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("lock existing benchmark: %w", err)
	}

	if existingID.Valid {
		err = tx.QueryRowContext(ctx, `
			UPDATE agent_benchmarks
			SET speed = $1, updated_at = CURRENT_TIMESTAMP
			WHERE id = $2
			RETURNING id, created_at, updated_at`,
			benchmark.Speed, existingID.String,
		).Scan(&benchmark.ID, &benchmark.CreatedAt, &benchmark.UpdatedAt)
		if err != nil {
			return fmt.Errorf("update agent benchmark: %w", err)
		}
	} else {
		err = tx.QueryRowContext(ctx, `
			INSERT INTO agent_benchmarks (agent_id, attack_mode, hash_type, salt_count, speed)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id, created_at, updated_at`,
			benchmark.AgentID, benchmark.AttackMode, benchmark.HashType,
			benchmark.SaltCount, benchmark.Speed,
		).Scan(&benchmark.ID, &benchmark.CreatedAt, &benchmark.UpdatedAt)
		if err != nil {
			return fmt.Errorf("insert agent benchmark: %w", err)
		}
	}

	// History is append-only and supplementary — failure here doesn't
	// invalidate the current-best estimate above.
	if _, histErr := tx.ExecContext(ctx, `
		INSERT INTO agent_benchmark_history (agent_id, attack_mode, hash_type, salt_count, speed, source)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		benchmark.AgentID, benchmark.AttackMode, benchmark.HashType,
		benchmark.SaltCount, benchmark.Speed, models.BenchmarkHistorySourceSpeedtest,
	); histErr != nil {
		fmt.Printf("[WARNING] Failed to record benchmark history: %v\n", histErr)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// GetAgentBenchmark retrieves the freshest benchmark for an agent + combo.
// saltCount is used for salted hash types - use nil for non-salted hash types.
// Uses IS NOT DISTINCT FROM for NULL-safe comparison of salt_count.
// ORDER BY ... LIMIT 1 mirrors IsRecentBenchmark — defensive against any
// pre-migration-000144 duplicate rows still present in the table.
func (r *BenchmarkRepository) GetAgentBenchmark(ctx context.Context, agentID int, attackMode models.AttackMode, hashType int, saltCount *int) (*models.AgentBenchmark, error) {
	query := `
		SELECT id, agent_id, attack_mode, hash_type, salt_count, speed, created_at, updated_at
		FROM agent_benchmarks
		WHERE agent_id = $1 AND attack_mode = $2 AND hash_type = $3
		  AND salt_count IS NOT DISTINCT FROM $4
		ORDER BY updated_at DESC
		LIMIT 1`

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

// IsRecentBenchmark checks if a benchmark is recent based on cache duration.
// saltCount is used for salted hash types - use nil for non-salted hash types.
// Uses IS NOT DISTINCT FROM for NULL-safe comparison of salt_count.
//
// ORDER BY updated_at DESC LIMIT 1 is intentional: pre-migration-000144 data
// can contain multiple duplicate rows for the same (agent, mode, type) when
// salt_count is NULL (see CreateOrUpdateAgentBenchmark for the history). Even
// after the dedupe migration, this is cheap and prevents a regression if any
// future code path forgets the upsert pattern.
func (r *BenchmarkRepository) IsRecentBenchmark(ctx context.Context, agentID int, attackMode models.AttackMode, hashType int, saltCount *int, cacheDuration time.Duration) (bool, error) {
	query := `
		SELECT updated_at
		FROM agent_benchmarks
		WHERE agent_id = $1 AND attack_mode = $2 AND hash_type = $3
		  AND salt_count IS NOT DISTINCT FROM $4
		ORDER BY updated_at DESC
		LIMIT 1`

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

// GetBenchmarkHistory retrieves paginated benchmark history with filters
func (r *BenchmarkRepository) GetBenchmarkHistory(ctx context.Context, agentID *int, hashType *int, attackMode *int, limit, offset int) ([]models.AgentBenchmarkHistory, int, error) {
	where := []string{"1=1"}
	args := []interface{}{}
	argIdx := 1

	if agentID != nil {
		where = append(where, fmt.Sprintf("abh.agent_id = $%d", argIdx))
		args = append(args, *agentID)
		argIdx++
	}
	if hashType != nil {
		where = append(where, fmt.Sprintf("abh.hash_type = $%d", argIdx))
		args = append(args, *hashType)
		argIdx++
	}
	if attackMode != nil {
		where = append(where, fmt.Sprintf("abh.attack_mode = $%d", argIdx))
		args = append(args, *attackMode)
		argIdx++
	}

	whereClause := strings.Join(where, " AND ")

	// Count total
	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM agent_benchmark_history abh WHERE %s`, whereClause)
	var total int
	err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count benchmark history: %w", err)
	}

	// Fetch page
	dataQuery := fmt.Sprintf(`
		SELECT abh.id, abh.agent_id, abh.attack_mode, abh.hash_type, abh.salt_count,
		       abh.speed, abh.success, abh.error_message, abh.recorded_at, abh.source
		FROM agent_benchmark_history abh
		WHERE %s
		ORDER BY abh.recorded_at DESC
		LIMIT $%d OFFSET $%d`, whereClause, argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, dataQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query benchmark history: %w", err)
	}
	defer rows.Close()

	var entries []models.AgentBenchmarkHistory
	for rows.Next() {
		var e models.AgentBenchmarkHistory
		if err := rows.Scan(&e.ID, &e.AgentID, &e.AttackMode, &e.HashType, &e.SaltCount,
			&e.Speed, &e.Success, &e.ErrorMessage, &e.RecordedAt, &e.Source); err != nil {
			return nil, 0, fmt.Errorf("failed to scan benchmark history: %w", err)
		}
		entries = append(entries, e)
	}

	return entries, total, nil
}

// GetBenchmarkTrends retrieves benchmark speed data over time for charting
func (r *BenchmarkRepository) GetBenchmarkTrends(ctx context.Context, agentID int, hashType *int, attackMode *int, since time.Time) ([]models.AgentBenchmarkHistory, error) {
	where := []string{"agent_id = $1", "success = true", "recorded_at >= $2"}
	args := []interface{}{agentID, since}
	argIdx := 3

	if hashType != nil {
		where = append(where, fmt.Sprintf("hash_type = $%d", argIdx))
		args = append(args, *hashType)
		argIdx++
	}
	if attackMode != nil {
		where = append(where, fmt.Sprintf("attack_mode = $%d", argIdx))
		args = append(args, *attackMode)
		argIdx++
	}

	query := fmt.Sprintf(`
		SELECT id, agent_id, attack_mode, hash_type, salt_count, speed, success, error_message, recorded_at, source
		FROM agent_benchmark_history
		WHERE %s
		ORDER BY recorded_at ASC`, strings.Join(where, " AND "))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query benchmark trends: %w", err)
	}
	defer rows.Close()

	var entries []models.AgentBenchmarkHistory
	for rows.Next() {
		var e models.AgentBenchmarkHistory
		if err := rows.Scan(&e.ID, &e.AgentID, &e.AttackMode, &e.HashType, &e.SaltCount,
			&e.Speed, &e.Success, &e.ErrorMessage, &e.RecordedAt, &e.Source); err != nil {
			return nil, fmt.Errorf("failed to scan benchmark trend: %w", err)
		}
		entries = append(entries, e)
	}

	return entries, nil
}

// DeleteOldBenchmarkHistory deletes benchmark history records older than the given time
func (r *BenchmarkRepository) DeleteOldBenchmarkHistory(ctx context.Context, before time.Time) error {
	query := `DELETE FROM agent_benchmark_history WHERE recorded_at < $1`
	_, err := r.db.ExecContext(ctx, query, before)
	if err != nil {
		return fmt.Errorf("failed to delete old benchmark history: %w", err)
	}
	return nil
}

// UpdateSpeedEMA applies an exponential moving average to the cached benchmark
// speed based on an observation from a completed task. Returns the previous and
// new speeds so callers can log the transition. If no benchmark row exists yet
// (cold cache) the observation is inserted as-is. Also appends an
// `observed_task` row to agent_benchmark_history for audit.
//
// alpha is the weight given to the new observation; typical value 0.3.
func (r *BenchmarkRepository) UpdateSpeedEMA(
	ctx context.Context,
	agentID int,
	attackMode models.AttackMode,
	hashType int,
	saltCount *int,
	observedSpeed int64,
	alpha float64,
) (oldSpeed int64, newSpeed int64, err error) {
	if alpha <= 0 || alpha > 1 {
		return 0, 0, fmt.Errorf("invalid EMA alpha %f (must be in (0, 1])", alpha)
	}
	if observedSpeed <= 0 {
		return 0, 0, fmt.Errorf("invalid observed speed %d", observedSpeed)
	}

	tx, err := r.db.Begin()
	if err != nil {
		return 0, 0, fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Lock the newest matching row (if any) so concurrent updates serialize
	// cleanly. `IS NOT DISTINCT FROM` treats NULL salt_count as equal, which
	// the unique constraint does NOT — see note below on ON CONFLICT.
	var existingID sql.NullString
	var existingSpeed sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT id, speed FROM agent_benchmarks
		WHERE agent_id = $1 AND attack_mode = $2 AND hash_type = $3
		  AND salt_count IS NOT DISTINCT FROM $4
		ORDER BY updated_at DESC
		LIMIT 1
		FOR UPDATE`,
		agentID, attackMode, hashType, saltCount,
	).Scan(&existingID, &existingSpeed)
	if err != nil && err != sql.ErrNoRows {
		return 0, 0, fmt.Errorf("lock existing benchmark: %w", err)
	}

	if existingSpeed.Valid && existingSpeed.Int64 > 0 {
		oldSpeed = existingSpeed.Int64
		blended := float64(oldSpeed)*(1.0-alpha) + float64(observedSpeed)*alpha
		newSpeed = int64(blended + 0.5)
	} else {
		oldSpeed = 0
		newSpeed = observedSpeed
	}

	// Prefer UPDATE-by-id over INSERT ... ON CONFLICT because the unique
	// constraint on (agent_id, attack_mode, hash_type, salt_count) does NOT
	// treat NULL salt_count as equal under PostgreSQL's default UNIQUE
	// semantics. An ON CONFLICT path would therefore insert duplicate rows
	// for non-salted hash types and the EMA "drift" wouldn't be visible on
	// an existing row. UPDATE-by-id is simple and correct for both cases.
	if existingID.Valid {
		if _, err = tx.ExecContext(ctx, `
			UPDATE agent_benchmarks
			SET speed = $1, updated_at = CURRENT_TIMESTAMP
			WHERE id = $2`,
			newSpeed, existingID.String,
		); err != nil {
			return 0, 0, fmt.Errorf("update benchmark by id: %w", err)
		}
	} else {
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO agent_benchmarks (agent_id, attack_mode, hash_type, salt_count, speed)
			VALUES ($1, $2, $3, $4, $5)`,
			agentID, attackMode, hashType, saltCount, newSpeed,
		); err != nil {
			return 0, 0, fmt.Errorf("insert benchmark: %w", err)
		}
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO agent_benchmark_history (agent_id, attack_mode, hash_type, salt_count, speed, source)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		agentID, attackMode, hashType, saltCount, observedSpeed, models.BenchmarkHistorySourceObservedTask,
	)
	if err != nil {
		return 0, 0, fmt.Errorf("append observed history: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit EMA update: %w", err)
	}
	return oldSpeed, newSpeed, nil
}

// RecordFailureAttempt upserts the per-(agent, job, attack_mode, hash_type)
// failure row and returns the post-increment state so callers can decide
// whether the failure threshold has been crossed.
func (r *BenchmarkRepository) RecordFailureAttempt(
	ctx context.Context,
	agentID int,
	jobExecutionID uuid.UUID,
	attackMode models.AttackMode,
	hashType int,
	errMsg string,
) (*models.BenchmarkFailureAttempt, error) {
	query := `
		INSERT INTO benchmark_failure_attempts (
			agent_id, job_execution_id, attack_mode, hash_type,
			failure_count, first_failure_at, last_failure_at, last_error
		)
		VALUES ($1, $2, $3, $4, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, $5)
		ON CONFLICT (agent_id, job_execution_id, attack_mode, hash_type)
		DO UPDATE SET
			failure_count = benchmark_failure_attempts.failure_count + 1,
			last_failure_at = CURRENT_TIMESTAMP,
			last_error = EXCLUDED.last_error
		RETURNING id, agent_id, job_execution_id, attack_mode, hash_type,
		          failure_count, first_failure_at, last_failure_at, last_error`

	var errMsgArg sql.NullString
	if errMsg != "" {
		errMsgArg = sql.NullString{String: errMsg, Valid: true}
	}

	var a models.BenchmarkFailureAttempt
	var lastErr sql.NullString
	err := r.db.QueryRowContext(ctx, query,
		agentID, jobExecutionID, attackMode, hashType, errMsgArg,
	).Scan(
		&a.ID, &a.AgentID, &a.JobExecutionID, &a.AttackMode, &a.HashType,
		&a.FailureCount, &a.FirstFailureAt, &a.LastFailureAt, &lastErr,
	)
	if err != nil {
		return nil, fmt.Errorf("record failure attempt: %w", err)
	}
	if lastErr.Valid {
		s := lastErr.String
		a.LastError = &s
	}
	return &a, nil
}

// ResetFailureAttempts clears the failure counter for a combo. Called after a
// successful benchmark so prior failures don't keep influencing future policy.
func (r *BenchmarkRepository) ResetFailureAttempts(
	ctx context.Context,
	agentID int,
	jobExecutionID uuid.UUID,
	attackMode models.AttackMode,
	hashType int,
) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM benchmark_failure_attempts
		WHERE agent_id = $1 AND job_execution_id = $2
		  AND attack_mode = $3 AND hash_type = $4`,
		agentID, jobExecutionID, attackMode, hashType,
	)
	if err != nil {
		return fmt.Errorf("reset failure attempts: %w", err)
	}
	return nil
}

// AddBlocklistEntry inserts (or refreshes) a cooldown entry. jobExecutionID
// may be nil for a global "any job with this combo" entry.
//
// Postgres supports ON CONFLICT against a partial unique index when the
// WHERE matches the index predicate, so we target the two partial indexes
// directly (idx_bench_blocklist_active_job_scoped and
// idx_bench_blocklist_active_global). This is fully idempotent and self-
// healing: if a row exists with cleared_at IS NULL but expires_at in the
// past (cooldown elapsed without being refreshed), DO UPDATE resurrects it
// with the new expiry instead of hitting a duplicate-key error.
func (r *BenchmarkRepository) AddBlocklistEntry(
	ctx context.Context,
	agentID int,
	jobExecutionID *uuid.UUID,
	attackMode models.AttackMode,
	hashType int,
	reason string,
	expiresAt time.Time,
) (*models.AgentBenchmarkBlocklist, error) {
	var (
		query string
		args  []interface{}
	)
	if jobExecutionID != nil {
		query = `
			INSERT INTO agent_benchmark_blocklist (
				agent_id, job_execution_id, attack_mode, hash_type, reason, expires_at
			)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (agent_id, job_execution_id, attack_mode, hash_type)
				WHERE cleared_at IS NULL AND job_execution_id IS NOT NULL
			DO UPDATE SET
				expires_at = GREATEST(agent_benchmark_blocklist.expires_at, EXCLUDED.expires_at),
				reason     = EXCLUDED.reason
			RETURNING id, created_at, expires_at`
		args = []interface{}{agentID, *jobExecutionID, attackMode, hashType, reason, expiresAt}
	} else {
		query = `
			INSERT INTO agent_benchmark_blocklist (
				agent_id, job_execution_id, attack_mode, hash_type, reason, expires_at
			)
			VALUES ($1, NULL, $2, $3, $4, $5)
			ON CONFLICT (agent_id, attack_mode, hash_type)
				WHERE cleared_at IS NULL AND job_execution_id IS NULL
			DO UPDATE SET
				expires_at = GREATEST(agent_benchmark_blocklist.expires_at, EXCLUDED.expires_at),
				reason     = EXCLUDED.reason
			RETURNING id, created_at, expires_at`
		args = []interface{}{agentID, attackMode, hashType, reason, expiresAt}
	}

	var entry models.AgentBenchmarkBlocklist
	entry.AgentID = agentID
	entry.JobExecutionID = jobExecutionID
	entry.AttackMode = attackMode
	entry.HashType = hashType
	entry.Reason = reason
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&entry.ID, &entry.CreatedAt, &entry.ExpiresAt,
	); err != nil {
		return nil, fmt.Errorf("add blocklist entry: %w", err)
	}
	return &entry, nil
}

// GetActiveBlocklistEntry returns an active (cleared_at IS NULL AND
// expires_at > NOW()) entry for the exact key if one exists, else nil.
func (r *BenchmarkRepository) GetActiveBlocklistEntry(
	ctx context.Context,
	agentID int,
	jobExecutionID *uuid.UUID,
	attackMode models.AttackMode,
	hashType int,
) (*models.AgentBenchmarkBlocklist, error) {
	query := `
		SELECT id, agent_id, job_execution_id, attack_mode, hash_type,
		       reason, expires_at, created_at, cleared_at, cleared_by
		FROM agent_benchmark_blocklist
		WHERE agent_id = $1
		  AND job_execution_id IS NOT DISTINCT FROM $2
		  AND attack_mode = $3
		  AND hash_type = $4
		  AND cleared_at IS NULL
		  AND expires_at > CURRENT_TIMESTAMP
		ORDER BY created_at DESC
		LIMIT 1`
	var e models.AgentBenchmarkBlocklist
	err := r.db.QueryRowContext(ctx, query, agentID, jobExecutionID, attackMode, hashType).Scan(
		&e.ID, &e.AgentID, &e.JobExecutionID, &e.AttackMode, &e.HashType,
		&e.Reason, &e.ExpiresAt, &e.CreatedAt, &e.ClearedAt, &e.ClearedBy,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get active blocklist entry: %w", err)
	}
	return &e, nil
}

// IsBlocklisted returns true if this (agent, job?, combo) has an active entry.
// Considers both job-scoped and global entries (job_execution_id IS NULL).
func (r *BenchmarkRepository) IsBlocklisted(
	ctx context.Context,
	agentID int,
	jobExecutionID uuid.UUID,
	attackMode models.AttackMode,
	hashType int,
) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1 FROM agent_benchmark_blocklist
			WHERE agent_id = $1
			  AND (job_execution_id = $2 OR job_execution_id IS NULL)
			  AND attack_mode = $3
			  AND hash_type = $4
			  AND cleared_at IS NULL
			  AND expires_at > CURRENT_TIMESTAMP
		)`
	var exists bool
	if err := r.db.QueryRowContext(ctx, query, agentID, jobExecutionID, attackMode, hashType).Scan(&exists); err != nil {
		return false, fmt.Errorf("check blocklist: %w", err)
	}
	return exists, nil
}

// ClearBlocklistEntry marks a specific entry as cleared by a user. Returns
// sql.ErrNoRows if the entry is already cleared or doesn't exist.
func (r *BenchmarkRepository) ClearBlocklistEntry(
	ctx context.Context,
	entryID uuid.UUID,
	clearedBy uuid.UUID,
) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE agent_benchmark_blocklist
		SET cleared_at = CURRENT_TIMESTAMP, cleared_by = $2
		WHERE id = $1 AND cleared_at IS NULL`, entryID, clearedBy)
	if err != nil {
		return fmt.Errorf("clear blocklist entry: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListBlocklistForJob returns all active blocklist entries whose
// job_execution_id matches or is NULL (global). Joins agent name and the
// matching failure_attempts row for UI display. Global entries have
// FailureCount/LastError left nil.
func (r *BenchmarkRepository) ListBlocklistForJob(
	ctx context.Context,
	jobExecutionID uuid.UUID,
) ([]models.AgentBenchmarkBlocklist, error) {
	query := `
		SELECT b.id, b.agent_id, b.job_execution_id, b.attack_mode, b.hash_type,
		       b.reason, b.expires_at, b.created_at, b.cleared_at, b.cleared_by,
		       a.name AS agent_name,
		       f.failure_count, f.last_error
		FROM agent_benchmark_blocklist b
		LEFT JOIN agents a ON a.id = b.agent_id
		LEFT JOIN benchmark_failure_attempts f
		  ON f.agent_id = b.agent_id
		 AND f.job_execution_id = $1
		 AND f.attack_mode = b.attack_mode
		 AND f.hash_type = b.hash_type
		WHERE (b.job_execution_id = $1 OR b.job_execution_id IS NULL)
		  AND b.cleared_at IS NULL
		  AND b.expires_at > CURRENT_TIMESTAMP
		ORDER BY b.created_at DESC`

	rows, err := r.db.QueryContext(ctx, query, jobExecutionID)
	if err != nil {
		return nil, fmt.Errorf("list blocklist for job: %w", err)
	}
	defer rows.Close()

	var entries []models.AgentBenchmarkBlocklist
	for rows.Next() {
		var e models.AgentBenchmarkBlocklist
		if err := rows.Scan(
			&e.ID, &e.AgentID, &e.JobExecutionID, &e.AttackMode, &e.HashType,
			&e.Reason, &e.ExpiresAt, &e.CreatedAt, &e.ClearedAt, &e.ClearedBy,
			&e.AgentName, &e.FailureCount, &e.LastError,
		); err != nil {
			return nil, fmt.Errorf("scan blocklist entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// CountAgentsWithRecentBenchmark returns how many *other* agents have a
// successful benchmark for this (attack_mode, hash_type) combination that
// is newer than `cacheDuration`. Used to decide whether a failure is likely
// agent-specific (many others succeeded → probably this GPU) vs job-specific
// (nobody has succeeded → probably the job is broken).
func (r *BenchmarkRepository) CountAgentsWithRecentBenchmark(
	ctx context.Context,
	excludeAgentID int,
	attackMode models.AttackMode,
	hashType int,
	cacheDuration time.Duration,
) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM agent_benchmarks
		WHERE agent_id <> $1
		  AND attack_mode = $2
		  AND hash_type = $3
		  AND speed > 0
		  AND updated_at > $4`
	var n int
	err := r.db.QueryRowContext(ctx, query,
		excludeAgentID, attackMode, hashType, time.Now().Add(-cacheDuration),
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count agents with recent benchmark: %w", err)
	}
	return n, nil
}

// AgentBenchmarkHealth is the bookkeeping snapshot used by quarantine logic.
type AgentBenchmarkHealth struct {
	Streak               int
	DistinctCombosFailed int
}

// RecordAgentBenchmarkFailure atomically updates the per-agent benchmark
// health counters and returns the post-update values.
//
//   - benchmark_failure_streak: incremented every call, OR reset to 1 if the
//     agent has been idle for longer than resetWindow (avoids false positives
//     after a long quiet period).
//   - benchmark_distinct_combos_failed: recomputed from
//     benchmark_failure_attempts within resetWindow.
//   - benchmark_last_failure_at: bumped to NOW().
func (r *BenchmarkRepository) RecordAgentBenchmarkFailure(
	ctx context.Context,
	agentID int,
	resetWindow time.Duration,
) (*AgentBenchmarkHealth, error) {
	resetSeconds := int64(resetWindow.Seconds())
	if resetSeconds < 1 {
		resetSeconds = 1
	}
	query := `
		WITH window_distinct AS (
			SELECT COUNT(DISTINCT (attack_mode, hash_type)) AS n
			FROM benchmark_failure_attempts
			WHERE agent_id = $1
			  AND last_failure_at > NOW() - make_interval(secs => $2::bigint)
		)
		UPDATE agents
		SET benchmark_failure_streak =
		        CASE
		            WHEN benchmark_last_failure_at IS NULL THEN 1
		            WHEN benchmark_last_failure_at < NOW() - make_interval(secs => $2::bigint) THEN 1
		            ELSE benchmark_failure_streak + 1
		        END,
		    benchmark_distinct_combos_failed = COALESCE((SELECT n FROM window_distinct), 0),
		    benchmark_last_failure_at = NOW()
		WHERE id = $1
		RETURNING benchmark_failure_streak, benchmark_distinct_combos_failed`
	var h AgentBenchmarkHealth
	if err := r.db.QueryRowContext(ctx, query, agentID, resetSeconds).Scan(
		&h.Streak, &h.DistinctCombosFailed,
	); err != nil {
		return nil, fmt.Errorf("record agent benchmark failure: %w", err)
	}
	return &h, nil
}

// ResetAgentBenchmarkHealth clears the per-agent benchmark health counters
// after a successful benchmark. Idempotent.
func (r *BenchmarkRepository) ResetAgentBenchmarkHealth(
	ctx context.Context,
	agentID int,
) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE agents
		SET benchmark_failure_streak = 0,
		    benchmark_distinct_combos_failed = 0
		WHERE id = $1
		  AND (benchmark_failure_streak <> 0 OR benchmark_distinct_combos_failed <> 0)`,
		agentID)
	if err != nil {
		return fmt.Errorf("reset agent benchmark health: %w", err)
	}
	return nil
}

// QuarantineAgent disables an agent (`is_enabled = false`) and stamps a reason
// into `last_error` so the operator UI surfaces why it stopped getting work.
// Idempotent: if the agent is already disabled, the row is still updated so the
// reason reflects the most recent diagnosis.
func (r *BenchmarkRepository) QuarantineAgent(
	ctx context.Context,
	agentID int,
	reason string,
) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE agents
		SET is_enabled = false,
		    last_error = $2,
		    updated_at = NOW()
		WHERE id = $1`,
		agentID, reason)
	if err != nil {
		return fmt.Errorf("quarantine agent %d: %w", agentID, err)
	}
	return nil
}

// CountRecentBenchmarkFailuresForJob returns the number of benchmark failure
// rows recorded against a job within `window`. Used to trigger
// `benchmark_storm` informational notifications before the per-tuple hard cap
// fires.
func (r *BenchmarkRepository) CountRecentBenchmarkFailuresForJob(
	ctx context.Context,
	jobExecutionID uuid.UUID,
	window time.Duration,
) (int, error) {
	windowSeconds := int64(window.Seconds())
	if windowSeconds < 1 {
		windowSeconds = 1
	}
	var n int
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(failure_count), 0)
		FROM benchmark_failure_attempts
		WHERE job_execution_id = $1
		  AND last_failure_at > NOW() - make_interval(secs => $2::bigint)`,
		jobExecutionID, windowSeconds,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count recent benchmark failures: %w", err)
	}
	return n, nil
}

// GetOverCapTuples returns the set of (agent_id, job_execution_id, attack_mode,
// hash_type) tuples whose per-tuple failure_count has reached the supplied cap.
// Used by the forced-benchmark planner to skip already-doomed targets without
// requiring an active blocklist row.
type OverCapTuple struct {
	AgentID        int
	JobExecutionID uuid.UUID
	AttackMode     models.AttackMode
	HashType       int
	FailureCount   int
}

func (r *BenchmarkRepository) GetOverCapTuples(
	ctx context.Context,
	cap int,
) ([]OverCapTuple, error) {
	if cap < 1 {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT agent_id, job_execution_id, attack_mode, hash_type, failure_count
		FROM benchmark_failure_attempts
		WHERE failure_count >= $1`,
		cap)
	if err != nil {
		return nil, fmt.Errorf("query over-cap tuples: %w", err)
	}
	defer rows.Close()
	var out []OverCapTuple
	for rows.Next() {
		var t OverCapTuple
		var mode int
		if err := rows.Scan(&t.AgentID, &t.JobExecutionID, &mode, &t.HashType, &t.FailureCount); err != nil {
			return nil, fmt.Errorf("scan over-cap tuple: %w", err)
		}
		t.AttackMode = models.AttackMode(mode)
		out = append(out, t)
	}
	return out, nil
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
