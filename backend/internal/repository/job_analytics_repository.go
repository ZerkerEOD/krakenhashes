package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/google/uuid"
)

// JobAnalyticsRepository handles database queries for job performance analytics
type JobAnalyticsRepository struct {
	db *db.DB
}

// NewJobAnalyticsRepository creates a new job analytics repository
func NewJobAnalyticsRepository(db *db.DB) *JobAnalyticsRepository {
	return &JobAnalyticsRepository{db: db}
}

// JobAnalyticsFilter contains filter parameters for analytics queries
type JobAnalyticsFilter struct {
	DateStart   *time.Time
	DateEnd     *time.Time
	AttackMode  *int
	HashType    *int
	AgentID     *int
	HashlistID  *int64
	Status      []string
	MinKeyspace *int64
	MaxKeyspace *int64
}

// FilterOptions contains available values for filter dropdowns
type FilterOptions struct {
	AttackModes []AttackModeOption `json:"attack_modes"`
	HashTypes   []HashTypeOption   `json:"hash_types"`
	Agents      []AgentOption      `json:"agents"`
	Hashlists   []HashlistOption   `json:"hashlists"`
}

// AttackModeOption is an attack mode filter value
type AttackModeOption struct {
	Value int    `json:"value"`
	Label string `json:"label"`
}

// HashTypeOption is a hash type filter value
type HashTypeOption struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// AgentOption is an agent filter value
type AgentOption struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// HashlistOption is a hashlist filter value
type HashlistOption struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// JobAnalyticsSummary contains aggregate statistics
type JobAnalyticsSummary struct {
	TotalJobs        int     `json:"total_jobs"`
	CompletedJobs    int     `json:"completed_jobs"`
	CancelledJobs    int     `json:"cancelled_jobs"`
	FailedJobs       int     `json:"failed_jobs"`
	TotalCracks      int64   `json:"total_cracks"`
	AverageSpeed     float64 `json:"average_speed"`
	TotalKeyspace    int64   `json:"total_keyspace_processed"`
	AverageDuration  float64 `json:"average_duration_seconds"`
}

// JobAnalyticsEntry represents a single job with computed metrics
type JobAnalyticsEntry struct {
	ID                    uuid.UUID  `json:"id"`
	Name                  string     `json:"name"`
	AttackMode            int        `json:"attack_mode"`
	HashType              int        `json:"hash_type"`
	HashTypeName          string     `json:"hash_type_name"`
	EffectiveKeyspace     int64      `json:"effective_keyspace"`
	Status                string     `json:"status"`
	Priority              int        `json:"priority"`
	StartedAt             *time.Time `json:"started_at"`
	CompletedAt           *time.Time `json:"completed_at"`
	DurationSeconds       *float64   `json:"duration_seconds"`
	TaskCount             int        `json:"task_count"`
	TotalCracks           int64      `json:"total_cracks"`
	AvgSpeed              float64    `json:"avg_speed"`
	MaxSpeed              int64      `json:"max_speed"`
	UniqueAgents          int        `json:"unique_agents"`
	HashlistID            int64      `json:"hashlist_id"`
	HashlistName          string     `json:"hashlist_name"`
	OverallProgressPercent float64   `json:"overall_progress_percent"`
}

// TimelinePoint represents a single data point in a time series
type TimelinePoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
	JobCount  int       `json:"job_count,omitempty"`
}

// TaskSegment represents a task within a job for the detail view
type TaskSegment struct {
	TaskID         uuid.UUID  `json:"task_id"`
	AgentID        int        `json:"agent_id"`
	AgentName      string     `json:"agent_name"`
	StartedAt      *time.Time `json:"started_at"`
	CompletedAt    *time.Time `json:"completed_at"`
	AverageSpeed   int64      `json:"average_speed"`
	BenchmarkSpeed int64      `json:"benchmark_speed"`
	CrackCount     int        `json:"crack_count"`
	Status         string     `json:"status"`
}

// buildFilterClauses builds WHERE clause fragments and args from a filter
func (f *JobAnalyticsFilter) buildFilterClauses(startArgIdx int) ([]string, []interface{}) {
	where := []string{}
	args := []interface{}{}
	idx := startArgIdx

	if f.DateStart != nil {
		where = append(where, fmt.Sprintf("je.started_at >= $%d", idx))
		args = append(args, *f.DateStart)
		idx++
	}
	if f.DateEnd != nil {
		where = append(where, fmt.Sprintf("COALESCE(je.completed_at, je.started_at) <= $%d", idx))
		args = append(args, *f.DateEnd)
		idx++
	}
	if f.AttackMode != nil {
		where = append(where, fmt.Sprintf("je.attack_mode = $%d", idx))
		args = append(args, *f.AttackMode)
		idx++
	}
	if f.HashType != nil {
		where = append(where, fmt.Sprintf("je.hash_type = $%d", idx))
		args = append(args, *f.HashType)
		idx++
	}
	if f.HashlistID != nil {
		where = append(where, fmt.Sprintf("je.hashlist_id = $%d", idx))
		args = append(args, *f.HashlistID)
		idx++
	}
	if f.MinKeyspace != nil {
		where = append(where, fmt.Sprintf("je.effective_keyspace >= $%d", idx))
		args = append(args, *f.MinKeyspace)
		idx++
	}
	if f.MaxKeyspace != nil {
		where = append(where, fmt.Sprintf("je.effective_keyspace <= $%d", idx))
		args = append(args, *f.MaxKeyspace)
		idx++
	}
	if len(f.Status) > 0 {
		placeholders := make([]string, len(f.Status))
		for i, s := range f.Status {
			placeholders[i] = fmt.Sprintf("$%d", idx)
			args = append(args, s)
			idx++
		}
		where = append(where, fmt.Sprintf("je.status IN (%s)", strings.Join(placeholders, ", ")))
	}
	if f.AgentID != nil {
		where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM job_tasks jt_f WHERE jt_f.job_execution_id = je.id AND jt_f.agent_id = $%d)", idx))
		args = append(args, *f.AgentID)
		idx++
	}

	return where, args
}

// GetFilterOptions returns available filter values from existing job data
func (r *JobAnalyticsRepository) GetFilterOptions(ctx context.Context) (*FilterOptions, error) {
	opts := &FilterOptions{}

	// Attack modes used in jobs
	amRows, err := r.db.QueryContext(ctx, `SELECT DISTINCT attack_mode FROM job_executions WHERE started_at IS NOT NULL ORDER BY attack_mode`)
	if err != nil {
		return nil, fmt.Errorf("failed to get attack modes: %w", err)
	}
	defer amRows.Close()
	for amRows.Next() {
		var am int
		if err := amRows.Scan(&am); err != nil {
			return nil, err
		}
		opts.AttackModes = append(opts.AttackModes, AttackModeOption{Value: am})
	}

	// Hash types used in jobs
	htRows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT je.hash_type, COALESCE(ht.name, 'Unknown')
		FROM job_executions je
		LEFT JOIN hash_types ht ON ht.id = je.hash_type
		WHERE je.started_at IS NOT NULL
		ORDER BY je.hash_type`)
	if err != nil {
		return nil, fmt.Errorf("failed to get hash types: %w", err)
	}
	defer htRows.Close()
	for htRows.Next() {
		var opt HashTypeOption
		if err := htRows.Scan(&opt.ID, &opt.Name); err != nil {
			return nil, err
		}
		opts.HashTypes = append(opts.HashTypes, opt)
	}

	// Agents that have run tasks
	agRows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT jt.agent_id, COALESCE(a.name, 'Agent ' || jt.agent_id::text)
		FROM job_tasks jt
		JOIN agents a ON a.id = jt.agent_id
		ORDER BY jt.agent_id`)
	if err != nil {
		return nil, fmt.Errorf("failed to get agents: %w", err)
	}
	defer agRows.Close()
	for agRows.Next() {
		var opt AgentOption
		if err := agRows.Scan(&opt.ID, &opt.Name); err != nil {
			return nil, err
		}
		opts.Agents = append(opts.Agents, opt)
	}

	// Hashlists that have had jobs
	hlRows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT je.hashlist_id, COALESCE(hl.name, 'Hashlist ' || je.hashlist_id::text)
		FROM job_executions je
		JOIN hashlists hl ON hl.id = je.hashlist_id
		WHERE je.started_at IS NOT NULL
		ORDER BY je.hashlist_id`)
	if err != nil {
		return nil, fmt.Errorf("failed to get hashlists: %w", err)
	}
	defer hlRows.Close()
	for hlRows.Next() {
		var opt HashlistOption
		if err := hlRows.Scan(&opt.ID, &opt.Name); err != nil {
			return nil, err
		}
		opts.Hashlists = append(opts.Hashlists, opt)
	}

	return opts, nil
}

// GetSummary returns aggregate statistics for filtered jobs
func (r *JobAnalyticsRepository) GetSummary(ctx context.Context, filter *JobAnalyticsFilter) (*JobAnalyticsSummary, error) {
	filterClauses, filterArgs := filter.buildFilterClauses(1)

	whereClause := "je.started_at IS NOT NULL"
	if len(filterClauses) > 0 {
		whereClause += " AND " + strings.Join(filterClauses, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT
			COUNT(*),
			COUNT(CASE WHEN je.status = 'completed' THEN 1 END),
			COUNT(CASE WHEN je.status = 'cancelled' THEN 1 END),
			COUNT(CASE WHEN je.status = 'failed' THEN 1 END),
			COALESCE(SUM(ts.total_cracks), 0),
			COALESCE(AVG(ts.avg_speed) FILTER (WHERE ts.avg_speed > 0), 0),
			COALESCE(SUM(je.effective_keyspace), 0),
			COALESCE(AVG(EXTRACT(EPOCH FROM (COALESCE(je.cracking_completed_at, je.completed_at) - je.started_at)))
				FILTER (WHERE je.started_at IS NOT NULL AND (je.cracking_completed_at IS NOT NULL OR je.completed_at IS NOT NULL)), 0)
		FROM job_executions je
		LEFT JOIN LATERAL (
			SELECT COALESCE(SUM(crack_count), 0) as total_cracks,
			       AVG(COALESCE(average_speed, benchmark_speed)) FILTER (WHERE COALESCE(average_speed, benchmark_speed) > 0) as avg_speed
			FROM job_tasks WHERE job_execution_id = je.id
		) ts ON true
		WHERE %s`, whereClause)

	summary := &JobAnalyticsSummary{}
	err := r.db.QueryRowContext(ctx, query, filterArgs...).Scan(
		&summary.TotalJobs,
		&summary.CompletedJobs,
		&summary.CancelledJobs,
		&summary.FailedJobs,
		&summary.TotalCracks,
		&summary.AverageSpeed,
		&summary.TotalKeyspace,
		&summary.AverageDuration,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get job analytics summary: %w", err)
	}

	return summary, nil
}

// GetJobsList returns paginated job entries with computed metrics
func (r *JobAnalyticsRepository) GetJobsList(ctx context.Context, filter *JobAnalyticsFilter, page, pageSize int, sortBy, sortOrder string) ([]JobAnalyticsEntry, int, error) {
	filterClauses, filterArgs := filter.buildFilterClauses(1)

	whereClause := "je.started_at IS NOT NULL"
	if len(filterClauses) > 0 {
		whereClause += " AND " + strings.Join(filterClauses, " AND ")
	}

	// Validate sort
	allowedSorts := map[string]string{
		"name":             "je.name",
		"started_at":       "je.started_at",
		"duration":         "duration_seconds",
		"avg_speed":        "avg_speed",
		"total_cracks":     "total_cracks",
		"effective_keyspace": "je.effective_keyspace",
		"status":           "je.status",
		"attack_mode":      "je.attack_mode",
		"hash_type":        "je.hash_type",
	}
	sortColumn, ok := allowedSorts[sortBy]
	if !ok {
		sortColumn = "je.started_at"
	}
	if sortOrder != "asc" {
		sortOrder = "desc"
	}

	// Count total
	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM job_executions je WHERE %s`, whereClause)
	var total int
	err := r.db.QueryRowContext(ctx, countQuery, filterArgs...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count jobs: %w", err)
	}

	// Build data query
	nextIdx := len(filterArgs) + 1
	dataQuery := fmt.Sprintf(`
		SELECT je.id, je.name, je.attack_mode, je.hash_type, je.effective_keyspace,
			je.status, je.priority, je.started_at, je.completed_at,
			EXTRACT(EPOCH FROM (COALESCE(je.cracking_completed_at, je.completed_at) - je.started_at)) as duration_seconds,
			je.overall_progress_percent, je.hashlist_id,
			COALESCE(ts.task_count, 0), COALESCE(ts.total_cracks, 0),
			COALESCE(ts.avg_speed, 0), COALESCE(ts.max_speed, 0),
			COALESCE(ts.unique_agents, 0),
			COALESCE(ht.name, 'Hash Type ' || je.hash_type::text),
			COALESCE(hl.name, 'Hashlist ' || je.hashlist_id::text)
		FROM job_executions je
		LEFT JOIN LATERAL (
			SELECT COUNT(*) as task_count,
				COALESCE(SUM(crack_count), 0) as total_cracks,
				AVG(COALESCE(average_speed, benchmark_speed)) FILTER (WHERE COALESCE(average_speed, benchmark_speed) > 0) as avg_speed,
				COALESCE(MAX(COALESCE(average_speed, benchmark_speed)), 0) as max_speed,
				COUNT(DISTINCT agent_id) as unique_agents
			FROM job_tasks WHERE job_execution_id = je.id
		) ts ON true
		LEFT JOIN hash_types ht ON ht.id = je.hash_type
		LEFT JOIN hashlists hl ON hl.id = je.hashlist_id
		WHERE %s
		ORDER BY %s %s NULLS LAST
		LIMIT $%d OFFSET $%d`,
		whereClause, sortColumn, sortOrder, nextIdx, nextIdx+1)

	offset := (page - 1) * pageSize
	allArgs := append(filterArgs, pageSize, offset)

	rows, err := r.db.QueryContext(ctx, dataQuery, allArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query jobs: %w", err)
	}
	defer rows.Close()

	var jobs []JobAnalyticsEntry
	for rows.Next() {
		var j JobAnalyticsEntry
		var progressPercent sql.NullFloat64
		err := rows.Scan(
			&j.ID, &j.Name, &j.AttackMode, &j.HashType, &j.EffectiveKeyspace,
			&j.Status, &j.Priority, &j.StartedAt, &j.CompletedAt,
			&j.DurationSeconds, &progressPercent, &j.HashlistID,
			&j.TaskCount, &j.TotalCracks,
			&j.AvgSpeed, &j.MaxSpeed,
			&j.UniqueAgents,
			&j.HashTypeName, &j.HashlistName,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan job entry: %w", err)
		}
		if progressPercent.Valid {
			j.OverallProgressPercent = progressPercent.Float64
		}
		jobs = append(jobs, j)
	}

	return jobs, total, nil
}

// GetTimeline returns aggregated hash rate data over time for charting
func (r *JobAnalyticsRepository) GetTimeline(ctx context.Context, filter *JobAnalyticsFilter, resolution string) ([]TimelinePoint, error) {
	if resolution != "daily" && resolution != "weekly" {
		resolution = "daily"
	}

	filterClauses, filterArgs := filter.buildFilterClauses(1)

	whereClause := "je.started_at IS NOT NULL"
	if len(filterClauses) > 0 {
		whereClause += " AND " + strings.Join(filterClauses, " AND ")
	}

	// Determine date_trunc interval and which aggregation levels to include.
	// Include realtime metrics alongside aggregated ones so recent/fast-completing jobs
	// that haven't been aggregated yet still appear on the timeline.
	var truncInterval, levels string
	if resolution == "weekly" {
		truncInterval = "week"
		levels = "'realtime', 'daily', 'weekly'"
	} else {
		truncInterval = "day"
		levels = "'realtime', 'daily'"
	}

	query := fmt.Sprintf(`
		WITH per_job AS (
			SELECT jpm.job_execution_id,
			       date_trunc('%s', jpm.timestamp) as bucket,
			       AVG(jpm.value) as avg_rate
			FROM job_performance_metrics jpm
			JOIN job_executions je ON je.id = jpm.job_execution_id
			WHERE jpm.metric_type = 'hash_rate'
			  AND jpm.aggregation_level IN (%s)
			  AND %s
			GROUP BY jpm.job_execution_id, date_trunc('%s', jpm.timestamp)
		)
		SELECT bucket as timestamp, SUM(avg_rate) as total_rate, COUNT(DISTINCT job_execution_id) as job_count
		FROM per_job
		GROUP BY bucket
		ORDER BY bucket ASC`, truncInterval, levels, whereClause, truncInterval)

	allArgs := filterArgs

	rows, err := r.db.QueryContext(ctx, query, allArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to query timeline: %w", err)
	}
	defer rows.Close()

	var points []TimelinePoint
	for rows.Next() {
		var p TimelinePoint
		if err := rows.Scan(&p.Timestamp, &p.Value, &p.JobCount); err != nil {
			return nil, fmt.Errorf("failed to scan timeline point: %w", err)
		}
		points = append(points, p)
	}

	return points, nil
}

// GetJobTimeline returns detailed timeline and task segments for a single job
func (r *JobAnalyticsRepository) GetJobTimeline(ctx context.Context, jobID uuid.UUID) ([]TimelinePoint, []TaskSegment, error) {
	// Get metric timeline
	metricQuery := `
		SELECT timestamp, value
		FROM job_performance_metrics
		WHERE job_execution_id = $1 AND metric_type = 'hash_rate'
		ORDER BY timestamp ASC`

	metricRows, err := r.db.QueryContext(ctx, metricQuery, jobID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query job timeline: %w", err)
	}
	defer metricRows.Close()

	var points []TimelinePoint
	for metricRows.Next() {
		var p TimelinePoint
		if err := metricRows.Scan(&p.Timestamp, &p.Value); err != nil {
			return nil, nil, fmt.Errorf("failed to scan timeline point: %w", err)
		}
		points = append(points, p)
	}

	// Get task segments
	taskQuery := `
		SELECT jt.id, jt.agent_id, COALESCE(a.name, 'Agent ' || jt.agent_id::text),
		       jt.started_at, jt.completed_at, COALESCE(jt.average_speed, 0),
		       COALESCE(jt.benchmark_speed, 0), COALESCE(jt.crack_count, 0), jt.status
		FROM job_tasks jt
		LEFT JOIN agents a ON a.id = jt.agent_id
		WHERE jt.job_execution_id = $1
		ORDER BY jt.started_at ASC NULLS LAST`

	taskRows, err := r.db.QueryContext(ctx, taskQuery, jobID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query job tasks: %w", err)
	}
	defer taskRows.Close()

	var tasks []TaskSegment
	for taskRows.Next() {
		var t TaskSegment
		if err := taskRows.Scan(&t.TaskID, &t.AgentID, &t.AgentName,
			&t.StartedAt, &t.CompletedAt, &t.AverageSpeed,
			&t.BenchmarkSpeed, &t.CrackCount, &t.Status); err != nil {
			return nil, nil, fmt.Errorf("failed to scan task segment: %w", err)
		}
		tasks = append(tasks, t)
	}

	return points, tasks, nil
}
