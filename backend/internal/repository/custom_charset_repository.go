package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// CustomCharsetRepository defines the interface for interacting with custom_charsets.
type CustomCharsetRepository interface {
	Create(ctx context.Context, charset *models.CustomCharset) (*models.CustomCharset, error)
	GetByID(ctx context.Context, id uuid.UUID) (*models.CustomCharset, error)
	Update(ctx context.Context, id uuid.UUID, charset *models.CustomCharset) (*models.CustomCharset, error)
	Delete(ctx context.Context, id uuid.UUID) error
	ListGlobal(ctx context.Context) ([]models.CustomCharset, error)
	ListByUser(ctx context.Context, userID uuid.UUID) ([]models.CustomCharset, error)
	ListByTeam(ctx context.Context, teamID uuid.UUID) ([]models.CustomCharset, error)
	ListAccessible(ctx context.Context, userID uuid.UUID, teamIDs []uuid.UUID) ([]models.CustomCharset, error)
}

// customCharsetRepository implements CustomCharsetRepository.
type customCharsetRepository struct {
	db *sql.DB
}

// NewCustomCharsetRepository creates a new repository for custom charsets.
func NewCustomCharsetRepository(db *sql.DB) CustomCharsetRepository {
	return &customCharsetRepository{db: db}
}

const customCharsetColumns = `id, name, description, definition, scope, owner_id, created_by, created_at, updated_at`

func scanCustomCharset(row interface{ Scan(dest ...interface{}) error }) (*models.CustomCharset, error) {
	var c models.CustomCharset
	var ownerID, createdBy sql.NullString
	err := row.Scan(
		&c.ID, &c.Name, &c.Description, &c.Definition, &c.Scope,
		&ownerID, &createdBy, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if ownerID.Valid {
		id, _ := uuid.Parse(ownerID.String)
		c.OwnerID = &id
	}
	if createdBy.Valid {
		id, _ := uuid.Parse(createdBy.String)
		c.CreatedBy = &id
	}
	return &c, nil
}

// Create inserts a new custom charset into the database.
func (r *customCharsetRepository) Create(ctx context.Context, charset *models.CustomCharset) (*models.CustomCharset, error) {
	query := fmt.Sprintf(`
		INSERT INTO custom_charsets (name, description, definition, scope, owner_id, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING %s`, customCharsetColumns)

	row := r.db.QueryRowContext(ctx, query,
		charset.Name, charset.Description, charset.Definition,
		charset.Scope, charset.OwnerID, charset.CreatedBy,
	)

	created, err := scanCustomCharset(row)
	if err != nil {
		debug.Error("Error creating custom charset: %v", err)
		return nil, fmt.Errorf("error creating custom charset: %w", err)
	}
	return created, nil
}

// GetByID retrieves a custom charset by its UUID.
func (r *customCharsetRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.CustomCharset, error) {
	query := fmt.Sprintf(`SELECT %s FROM custom_charsets WHERE id = $1`, customCharsetColumns)

	row := r.db.QueryRowContext(ctx, query, id)
	charset, err := scanCustomCharset(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("custom charset not found: %w", ErrNotFound)
		}
		debug.Error("Error getting custom charset by ID %s: %v", id, err)
		return nil, fmt.Errorf("error getting custom charset: %w", err)
	}
	return charset, nil
}

// Update modifies an existing custom charset.
func (r *customCharsetRepository) Update(ctx context.Context, id uuid.UUID, charset *models.CustomCharset) (*models.CustomCharset, error) {
	query := fmt.Sprintf(`
		UPDATE custom_charsets
		SET name = $2, description = $3, definition = $4, updated_at = NOW()
		WHERE id = $1
		RETURNING %s`, customCharsetColumns)

	row := r.db.QueryRowContext(ctx, query,
		id, charset.Name, charset.Description, charset.Definition,
	)

	updated, err := scanCustomCharset(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("custom charset not found for update: %w", ErrNotFound)
		}
		debug.Error("Error updating custom charset %s: %v", id, err)
		return nil, fmt.Errorf("error updating custom charset: %w", err)
	}
	return updated, nil
}

// Delete removes a custom charset from the database.
func (r *customCharsetRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM custom_charsets WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		debug.Error("Error deleting custom charset %s: %v", id, err)
		return fmt.Errorf("error deleting custom charset: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		debug.Warning("Could not get rows affected after deleting custom charset %s: %v", id, err)
	} else if rowsAffected == 0 {
		return fmt.Errorf("custom charset not found for deletion: %w", ErrNotFound)
	}
	return nil
}

// ListGlobal retrieves all global charsets.
func (r *customCharsetRepository) ListGlobal(ctx context.Context) ([]models.CustomCharset, error) {
	query := fmt.Sprintf(`SELECT %s FROM custom_charsets WHERE scope = 'global' ORDER BY name`, customCharsetColumns)
	return r.queryCharsets(ctx, query)
}

// ListByUser retrieves all charsets owned by a specific user.
func (r *customCharsetRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]models.CustomCharset, error) {
	query := fmt.Sprintf(`SELECT %s FROM custom_charsets WHERE scope = 'user' AND owner_id = $1 ORDER BY name`, customCharsetColumns)
	return r.queryCharsetsWithArgs(ctx, query, userID)
}

// ListByTeam retrieves all charsets for a specific team.
func (r *customCharsetRepository) ListByTeam(ctx context.Context, teamID uuid.UUID) ([]models.CustomCharset, error) {
	query := fmt.Sprintf(`SELECT %s FROM custom_charsets WHERE scope = 'team' AND owner_id = $1 ORDER BY name`, customCharsetColumns)
	return r.queryCharsetsWithArgs(ctx, query, teamID)
}

// ListAccessible retrieves all charsets accessible to a user (global + user's own + teams).
func (r *customCharsetRepository) ListAccessible(ctx context.Context, userID uuid.UUID, teamIDs []uuid.UUID) ([]models.CustomCharset, error) {
	if len(teamIDs) == 0 {
		// No teams - just global + user's own
		query := fmt.Sprintf(`
			SELECT %s FROM custom_charsets
			WHERE scope = 'global'
			   OR (scope = 'user' AND owner_id = $1)
			ORDER BY scope, name`, customCharsetColumns)
		return r.queryCharsetsWithArgs(ctx, query, userID)
	}

	// Build team ID array for ANY clause
	teamIDStrings := make([]interface{}, len(teamIDs)+1)
	teamIDStrings[0] = userID
	placeholders := "$2"
	for i, tid := range teamIDs {
		teamIDStrings[i+1] = tid
		if i > 0 {
			placeholders += fmt.Sprintf(", $%d", i+2)
		}
	}

	query := fmt.Sprintf(`
		SELECT %s FROM custom_charsets
		WHERE scope = 'global'
		   OR (scope = 'user' AND owner_id = $1)
		   OR (scope = 'team' AND owner_id IN (%s))
		ORDER BY scope, name`, customCharsetColumns, placeholders)

	return r.queryCharsetsWithArgs(ctx, query, teamIDStrings...)
}

// queryCharsets runs a query and returns a list of custom charsets (no args).
func (r *customCharsetRepository) queryCharsets(ctx context.Context, query string) ([]models.CustomCharset, error) {
	return r.queryCharsetsWithArgs(ctx, query)
}

// queryCharsetsWithArgs runs a query with arguments and returns a list of custom charsets.
func (r *customCharsetRepository) queryCharsetsWithArgs(ctx context.Context, query string, args ...interface{}) ([]models.CustomCharset, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		debug.Error("Error querying custom charsets: %v", err)
		return nil, fmt.Errorf("error querying custom charsets: %w", err)
	}
	defer rows.Close()

	charsets := []models.CustomCharset{}
	for rows.Next() {
		charset, err := scanCustomCharset(rows)
		if err != nil {
			debug.Error("Error scanning custom charset row: %v", err)
			return nil, fmt.Errorf("error scanning custom charset row: %w", err)
		}
		charsets = append(charsets, *charset)
	}

	if err = rows.Err(); err != nil {
		debug.Error("Error iterating custom charset rows: %v", err)
		return nil, fmt.Errorf("error iterating custom charset rows: %w", err)
	}

	return charsets, nil
}
