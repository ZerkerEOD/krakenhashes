package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// JobIncrementLayerRepository handles database operations for job increment layers
type JobIncrementLayerRepository struct {
	db *db.DB
}

// NewJobIncrementLayerRepository creates a new job increment layer repository
func NewJobIncrementLayerRepository(db *db.DB) *JobIncrementLayerRepository {
	return &JobIncrementLayerRepository{db: db}
}

// Create creates a new job increment layer
func (r *JobIncrementLayerRepository) Create(ctx context.Context, layer *models.JobIncrementLayer) error {
	query := `
		INSERT INTO job_increment_layers (
			job_execution_id, layer_index, mask, status,
			base_keyspace, effective_keyspace, processed_keyspace, dispatched_keyspace,
			is_accurate_keyspace, overall_progress_percent
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at, updated_at`

	err := r.db.QueryRowContext(ctx, query,
		layer.JobExecutionID,
		layer.LayerIndex,
		layer.Mask,
		layer.Status,
		layer.BaseKeyspace,
		layer.EffectiveKeyspace,
		layer.ProcessedKeyspace,
		layer.DispatchedKeyspace,
		layer.IsAccurateKeyspace,
		layer.OverallProgressPercent,
	).Scan(&layer.ID, &layer.CreatedAt, &layer.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create job increment layer: %w", err)
	}

	debug.Log("Created job increment layer", map[string]interface{}{
		"id":                layer.ID,
		"job_execution_id":  layer.JobExecutionID,
		"layer_index":       layer.LayerIndex,
		"mask":              layer.Mask,
		"base_keyspace":     layer.BaseKeyspace,
	})

	return nil
}

// GetByID retrieves a job increment layer by ID
func (r *JobIncrementLayerRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.JobIncrementLayer, error) {
	query := `
		SELECT id, job_execution_id, layer_index, mask, status,
		       base_keyspace, effective_keyspace, processed_keyspace, dispatched_keyspace,
		       is_accurate_keyspace, overall_progress_percent, last_progress_update,
		       created_at, started_at, completed_at, updated_at, error_message
		FROM job_increment_layers
		WHERE id = $1`

	var layer models.JobIncrementLayer
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&layer.ID,
		&layer.JobExecutionID,
		&layer.LayerIndex,
		&layer.Mask,
		&layer.Status,
		&layer.BaseKeyspace,
		&layer.EffectiveKeyspace,
		&layer.ProcessedKeyspace,
		&layer.DispatchedKeyspace,
		&layer.IsAccurateKeyspace,
		&layer.OverallProgressPercent,
		&layer.LastProgressUpdate,
		&layer.CreatedAt,
		&layer.StartedAt,
		&layer.CompletedAt,
		&layer.UpdatedAt,
		&layer.ErrorMessage,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("job increment layer not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get job increment layer: %w", err)
	}

	return &layer, nil
}

// GetByJobExecutionID retrieves all increment layers for a job execution
func (r *JobIncrementLayerRepository) GetByJobExecutionID(ctx context.Context, jobExecutionID uuid.UUID) ([]models.JobIncrementLayer, error) {
	query := `
		SELECT id, job_execution_id, layer_index, mask, status,
		       base_keyspace, effective_keyspace, processed_keyspace, dispatched_keyspace,
		       is_accurate_keyspace, overall_progress_percent, last_progress_update,
		       created_at, started_at, completed_at, updated_at, error_message
		FROM job_increment_layers
		WHERE job_execution_id = $1
		ORDER BY layer_index ASC`

	rows, err := r.db.QueryContext(ctx, query, jobExecutionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query job increment layers: %w", err)
	}
	defer rows.Close()

	var layers []models.JobIncrementLayer
	for rows.Next() {
		var layer models.JobIncrementLayer
		err := rows.Scan(
			&layer.ID,
			&layer.JobExecutionID,
			&layer.LayerIndex,
			&layer.Mask,
			&layer.Status,
			&layer.BaseKeyspace,
			&layer.EffectiveKeyspace,
			&layer.ProcessedKeyspace,
			&layer.DispatchedKeyspace,
			&layer.IsAccurateKeyspace,
			&layer.OverallProgressPercent,
			&layer.LastProgressUpdate,
			&layer.CreatedAt,
			&layer.StartedAt,
			&layer.CompletedAt,
			&layer.UpdatedAt,
			&layer.ErrorMessage,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job increment layer: %w", err)
		}
		layers = append(layers, layer)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating job increment layers: %w", err)
	}

	return layers, nil
}

// GetLayersWithStats retrieves all increment layers for a job with aggregated stats
func (r *JobIncrementLayerRepository) GetLayersWithStats(ctx context.Context, jobExecutionID uuid.UUID) ([]models.JobIncrementLayerWithStats, error) {
	query := `
		SELECT
			l.id, l.job_execution_id, l.layer_index, l.mask, l.status,
			l.base_keyspace, l.effective_keyspace, l.processed_keyspace, l.dispatched_keyspace,
			l.is_accurate_keyspace, l.overall_progress_percent, l.last_progress_update,
			l.created_at, l.started_at, l.completed_at, l.updated_at, l.error_message,
			COALESCE(COUNT(t.id), 0) as total_tasks,
			COALESCE(COUNT(CASE WHEN t.status IN ('running', 'assigned', 'processing') THEN 1 END), 0) as running_tasks,
			COALESCE(COUNT(CASE WHEN t.status = 'completed' THEN 1 END), 0) as completed_tasks,
			COALESCE(COUNT(CASE WHEN t.status = 'failed' THEN 1 END), 0) as failed_tasks,
			COALESCE(SUM(COALESCE(t.crack_count, 0)), 0) as crack_count
		FROM job_increment_layers l
		LEFT JOIN job_tasks t ON t.increment_layer_id = l.id
		WHERE l.job_execution_id = $1
		GROUP BY l.id, l.job_execution_id, l.layer_index, l.mask, l.status,
		         l.base_keyspace, l.effective_keyspace, l.processed_keyspace, l.dispatched_keyspace,
		         l.is_accurate_keyspace, l.overall_progress_percent, l.last_progress_update,
		         l.created_at, l.started_at, l.completed_at, l.updated_at, l.error_message
		ORDER BY l.layer_index ASC`

	rows, err := r.db.QueryContext(ctx, query, jobExecutionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query job increment layers with stats: %w", err)
	}
	defer rows.Close()

	// Initialize as empty slice so JSON encodes as [] instead of null
	layers := make([]models.JobIncrementLayerWithStats, 0)
	for rows.Next() {
		var layer models.JobIncrementLayerWithStats
		err := rows.Scan(
			&layer.ID,
			&layer.JobExecutionID,
			&layer.LayerIndex,
			&layer.Mask,
			&layer.Status,
			&layer.BaseKeyspace,
			&layer.EffectiveKeyspace,
			&layer.ProcessedKeyspace,
			&layer.DispatchedKeyspace,
			&layer.IsAccurateKeyspace,
			&layer.OverallProgressPercent,
			&layer.LastProgressUpdate,
			&layer.CreatedAt,
			&layer.StartedAt,
			&layer.CompletedAt,
			&layer.UpdatedAt,
			&layer.ErrorMessage,
			&layer.TotalTasks,
			&layer.RunningTasks,
			&layer.CompletedTasks,
			&layer.FailedTasks,
			&layer.CrackCount,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job increment layer with stats: %w", err)
		}
		layers = append(layers, layer)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating job increment layers with stats: %w", err)
	}

	return layers, nil
}

// GetLayersNeedingBenchmark retrieves layers that need benchmarking, ordered by priority
func (r *JobIncrementLayerRepository) GetLayersNeedingBenchmark(ctx context.Context) ([]models.JobIncrementLayer, error) {
	query := `
		SELECT
			l.id, l.job_execution_id, l.layer_index, l.mask, l.status,
			l.base_keyspace, l.effective_keyspace, l.processed_keyspace, l.dispatched_keyspace,
			l.is_accurate_keyspace, l.overall_progress_percent, l.last_progress_update,
			l.created_at, l.started_at, l.completed_at, l.updated_at, l.error_message
		FROM job_increment_layers l
		JOIN job_executions j ON j.id = l.job_execution_id
		WHERE l.is_accurate_keyspace = FALSE
		  AND j.status IN ('pending', 'running')
		ORDER BY j.priority DESC, j.created_at ASC, l.layer_index ASC`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query layers needing benchmark: %w", err)
	}
	defer rows.Close()

	var layers []models.JobIncrementLayer
	for rows.Next() {
		var layer models.JobIncrementLayer
		err := rows.Scan(
			&layer.ID,
			&layer.JobExecutionID,
			&layer.LayerIndex,
			&layer.Mask,
			&layer.Status,
			&layer.BaseKeyspace,
			&layer.EffectiveKeyspace,
			&layer.ProcessedKeyspace,
			&layer.DispatchedKeyspace,
			&layer.IsAccurateKeyspace,
			&layer.OverallProgressPercent,
			&layer.LastProgressUpdate,
			&layer.CreatedAt,
			&layer.StartedAt,
			&layer.CompletedAt,
			&layer.UpdatedAt,
			&layer.ErrorMessage,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan layer needing benchmark: %w", err)
		}
		layers = append(layers, layer)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating layers needing benchmark: %w", err)
	}

	return layers, nil
}

// GetLayersWithPendingWork retrieves layers that have undispatched keyspace or need benchmarking
func (r *JobIncrementLayerRepository) GetLayersWithPendingWork(ctx context.Context, jobExecutionID uuid.UUID) ([]models.JobIncrementLayer, error) {
	query := `
		SELECT id, job_execution_id, layer_index, mask, status,
		       base_keyspace, effective_keyspace, processed_keyspace, dispatched_keyspace,
		       is_accurate_keyspace, overall_progress_percent, last_progress_update,
		       created_at, started_at, completed_at, updated_at, error_message
		FROM job_increment_layers
		WHERE job_execution_id = $1
		  AND (
		    -- Case A: Needs benchmark (return for benchmarking)
		    (is_accurate_keyspace = FALSE OR effective_keyspace IS NULL)
		    -- Case B: Has undispatched work
		    OR (is_accurate_keyspace = TRUE AND dispatched_keyspace < effective_keyspace)
		  )
		  AND status NOT IN ('completed', 'failed')
		ORDER BY layer_index ASC`

	rows, err := r.db.QueryContext(ctx, query, jobExecutionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query layers with pending work: %w", err)
	}
	defer rows.Close()

	var layers []models.JobIncrementLayer
	for rows.Next() {
		var layer models.JobIncrementLayer
		err := rows.Scan(
			&layer.ID,
			&layer.JobExecutionID,
			&layer.LayerIndex,
			&layer.Mask,
			&layer.Status,
			&layer.BaseKeyspace,
			&layer.EffectiveKeyspace,
			&layer.ProcessedKeyspace,
			&layer.DispatchedKeyspace,
			&layer.IsAccurateKeyspace,
			&layer.OverallProgressPercent,
			&layer.LastProgressUpdate,
			&layer.CreatedAt,
			&layer.StartedAt,
			&layer.CompletedAt,
			&layer.UpdatedAt,
			&layer.ErrorMessage,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan layer with pending work: %w", err)
		}
		layers = append(layers, layer)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating layers with pending work: %w", err)
	}

	return layers, nil
}

// UpdateKeyspace updates the keyspace fields for a layer
func (r *JobIncrementLayerRepository) UpdateKeyspace(ctx context.Context, layerID uuid.UUID, effectiveKeyspace int64, isAccurate bool) error {
	query := `
		UPDATE job_increment_layers
		SET effective_keyspace = $2,
		    is_accurate_keyspace = $3,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, layerID, effectiveKeyspace, isAccurate)
	if err != nil {
		return fmt.Errorf("failed to update layer keyspace: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("layer not found: %s", layerID)
	}

	debug.Log("Updated layer keyspace", map[string]interface{}{
		"layer_id":            layerID,
		"effective_keyspace":  effectiveKeyspace,
		"is_accurate_keyspace": isAccurate,
	})

	return nil
}

// UpdateStatus updates the status of a layer
func (r *JobIncrementLayerRepository) UpdateStatus(ctx context.Context, layerID uuid.UUID, status models.JobIncrementLayerStatus) error {
	query := `
		UPDATE job_increment_layers
		SET status = $2,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, layerID, status)
	if err != nil {
		return fmt.Errorf("failed to update layer status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("layer not found: %s", layerID)
	}

	debug.Log("Updated layer status", map[string]interface{}{
		"layer_id": layerID,
		"status":   status,
	})

	return nil
}

// UpdateProgress updates progress fields for a layer
func (r *JobIncrementLayerRepository) UpdateProgress(ctx context.Context, layerID uuid.UUID, processedKeyspace int64, progressPercent float64) error {
	query := `
		UPDATE job_increment_layers
		SET processed_keyspace = $2,
		    overall_progress_percent = $3,
		    last_progress_update = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, layerID, processedKeyspace, progressPercent)
	if err != nil {
		return fmt.Errorf("failed to update layer progress: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("layer not found: %s", layerID)
	}

	return nil
}

// UpdateDispatchedKeyspace updates the dispatched_keyspace field for a layer
func (r *JobIncrementLayerRepository) UpdateDispatchedKeyspace(ctx context.Context, layerID uuid.UUID, dispatchedKeyspace int64) error {
	query := `
		UPDATE job_increment_layers
		SET dispatched_keyspace = $2,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, layerID, dispatchedKeyspace)
	if err != nil {
		return fmt.Errorf("failed to update layer dispatched keyspace: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("layer not found: %s", layerID)
	}

	return nil
}

// IncrementDispatchedKeyspace increments the dispatched_keyspace field for a layer
func (r *JobIncrementLayerRepository) IncrementDispatchedKeyspace(ctx context.Context, layerID uuid.UUID, amount int64) error {
	query := `
		UPDATE job_increment_layers
		SET dispatched_keyspace = dispatched_keyspace + $2,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, layerID, amount)
	if err != nil {
		return fmt.Errorf("failed to increment layer dispatched keyspace: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("layer not found: %s", layerID)
	}

	return nil
}

// HasPendingLayers checks if a job has any layers not yet completed
func (r *JobIncrementLayerRepository) HasPendingLayers(ctx context.Context, jobExecutionID uuid.UUID) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM job_increment_layers
			WHERE job_execution_id = $1
			AND status IN ('pending', 'running', 'paused')
		)`

	var hasPending bool
	err := r.db.QueryRowContext(ctx, query, jobExecutionID).Scan(&hasPending)
	if err != nil {
		return false, fmt.Errorf("failed to check for pending layers: %w", err)
	}

	return hasPending, nil
}

// UpdateEffectiveKeyspace updates a layer's effective keyspace and marks it as accurate
func (r *JobIncrementLayerRepository) UpdateEffectiveKeyspace(ctx context.Context, layerID uuid.UUID, effectiveKeyspace int64) error {
	query := `
		UPDATE job_increment_layers
		SET effective_keyspace = $2,
		    is_accurate_keyspace = true,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, layerID, effectiveKeyspace)
	if err != nil {
		return fmt.Errorf("failed to update layer effective keyspace: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("layer not found: %s", layerID)
	}

	debug.Log("Updated layer effective keyspace", map[string]interface{}{
		"layer_id":           layerID,
		"effective_keyspace": effectiveKeyspace,
	})

	return nil
}

// GetTotalEffectiveKeyspace returns the sum of all layers' effective keyspaces for a job
func (r *JobIncrementLayerRepository) GetTotalEffectiveKeyspace(ctx context.Context, jobExecutionID uuid.UUID) (int64, error) {
	query := `
		SELECT COALESCE(SUM(COALESCE(effective_keyspace, base_keyspace)), 0)
		FROM job_increment_layers
		WHERE job_execution_id = $1`

	var total int64
	err := r.db.QueryRowContext(ctx, query, jobExecutionID).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("failed to get total effective keyspace: %w", err)
	}

	return total, nil
}

// GetCumulativeEffectiveKeyspace returns the sum of effective_keyspace for all layers
// with layer_index less than the specified index. This is used to calculate the
// global effective keyspace offset for keysplit tasks within a layer.
func (r *JobIncrementLayerRepository) GetCumulativeEffectiveKeyspace(ctx context.Context, jobExecutionID uuid.UUID, layerIndex int) (int64, error) {
	query := `
		SELECT COALESCE(SUM(COALESCE(effective_keyspace, 0)), 0)
		FROM job_increment_layers
		WHERE job_execution_id = $1 AND layer_index < $2`

	var cumulative int64
	err := r.db.QueryRowContext(ctx, query, jobExecutionID, layerIndex).Scan(&cumulative)
	if err != nil {
		return 0, fmt.Errorf("failed to get cumulative effective keyspace: %w", err)
	}

	return cumulative, nil
}

// Delete deletes a job increment layer by ID
func (r *JobIncrementLayerRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM job_increment_layers WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete job increment layer: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("layer not found: %s", id)
	}

	debug.Log("Deleted job increment layer", map[string]interface{}{
		"id": id,
	})

	return nil
}
