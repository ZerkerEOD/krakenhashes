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

// PresetIncrementLayerRepository handles database operations for preset increment layers
type PresetIncrementLayerRepository struct {
	db *db.DB
}

// NewPresetIncrementLayerRepository creates a new preset increment layer repository
func NewPresetIncrementLayerRepository(db *db.DB) *PresetIncrementLayerRepository {
	return &PresetIncrementLayerRepository{db: db}
}

// Create creates a new preset increment layer
func (r *PresetIncrementLayerRepository) Create(ctx context.Context, layer *models.PresetIncrementLayer) error {
	query := `
		INSERT INTO preset_increment_layers (
			preset_job_id, layer_index, mask, base_keyspace, effective_keyspace
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at`

	err := r.db.QueryRowContext(ctx, query,
		layer.PresetJobID,
		layer.LayerIndex,
		layer.Mask,
		layer.BaseKeyspace,
		layer.EffectiveKeyspace,
	).Scan(&layer.ID, &layer.CreatedAt, &layer.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create preset increment layer: %w", err)
	}

	debug.Log("Created preset increment layer", map[string]interface{}{
		"id":            layer.ID,
		"preset_job_id": layer.PresetJobID,
		"layer_index":   layer.LayerIndex,
		"mask":          layer.Mask,
		"base_keyspace": layer.BaseKeyspace,
	})

	return nil
}

// GetByPresetJobID retrieves all increment layers for a preset job
func (r *PresetIncrementLayerRepository) GetByPresetJobID(ctx context.Context, presetJobID uuid.UUID) ([]models.PresetIncrementLayer, error) {
	query := `
		SELECT id, preset_job_id, layer_index, mask, base_keyspace, effective_keyspace,
		       created_at, updated_at
		FROM preset_increment_layers
		WHERE preset_job_id = $1
		ORDER BY layer_index ASC`

	rows, err := r.db.QueryContext(ctx, query, presetJobID)
	if err != nil {
		return nil, fmt.Errorf("failed to query preset increment layers: %w", err)
	}
	defer rows.Close()

	var layers []models.PresetIncrementLayer
	for rows.Next() {
		var layer models.PresetIncrementLayer
		err := rows.Scan(
			&layer.ID,
			&layer.PresetJobID,
			&layer.LayerIndex,
			&layer.Mask,
			&layer.BaseKeyspace,
			&layer.EffectiveKeyspace,
			&layer.CreatedAt,
			&layer.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan preset increment layer: %w", err)
		}
		layers = append(layers, layer)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating preset increment layers: %w", err)
	}

	return layers, nil
}

// DeleteByPresetJobID deletes all increment layers for a preset job
func (r *PresetIncrementLayerRepository) DeleteByPresetJobID(ctx context.Context, presetJobID uuid.UUID) error {
	query := `DELETE FROM preset_increment_layers WHERE preset_job_id = $1`

	result, err := r.db.ExecContext(ctx, query, presetJobID)
	if err != nil {
		return fmt.Errorf("failed to delete preset increment layers: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	debug.Log("Deleted preset increment layers", map[string]interface{}{
		"preset_job_id": presetJobID,
		"rows_deleted":  rowsAffected,
	})

	return nil
}

// GetByID retrieves a preset increment layer by ID
func (r *PresetIncrementLayerRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.PresetIncrementLayer, error) {
	query := `
		SELECT id, preset_job_id, layer_index, mask, base_keyspace, effective_keyspace,
		       created_at, updated_at
		FROM preset_increment_layers
		WHERE id = $1`

	var layer models.PresetIncrementLayer
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&layer.ID,
		&layer.PresetJobID,
		&layer.LayerIndex,
		&layer.Mask,
		&layer.BaseKeyspace,
		&layer.EffectiveKeyspace,
		&layer.CreatedAt,
		&layer.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("preset increment layer not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get preset increment layer: %w", err)
	}

	return &layer, nil
}

// GetTotalEffectiveKeyspace returns the sum of all layers' effective keyspaces for a preset
func (r *PresetIncrementLayerRepository) GetTotalEffectiveKeyspace(ctx context.Context, presetJobID uuid.UUID) (int64, error) {
	query := `
		SELECT COALESCE(SUM(COALESCE(effective_keyspace, base_keyspace)), 0)
		FROM preset_increment_layers
		WHERE preset_job_id = $1`

	var total int64
	err := r.db.QueryRowContext(ctx, query, presetJobID).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("failed to get total effective keyspace: %w", err)
	}

	return total, nil
}

// CountByPresetJobID returns the number of increment layers for a preset job
func (r *PresetIncrementLayerRepository) CountByPresetJobID(ctx context.Context, presetJobID uuid.UUID) (int, error) {
	query := `SELECT COUNT(*) FROM preset_increment_layers WHERE preset_job_id = $1`

	var count int
	err := r.db.QueryRowContext(ctx, query, presetJobID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count preset increment layers: %w", err)
	}

	return count, nil
}
