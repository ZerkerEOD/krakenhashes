package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db/queries" // Import queries package
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/lib/pq" // Import pq for error handling
)

// ClientRepository handles database operations for clients.
type ClientRepository struct {
	db *db.DB
}

// NewClientRepository creates a new instance of ClientRepository.
func NewClientRepository(database *db.DB) *ClientRepository {
	return &ClientRepository{db: database}
}

// Create inserts a new client record into the database.
func (r *ClientRepository) Create(ctx context.Context, client *models.Client) error {
	client.CreatedAt = time.Now() // Ensure CreatedAt is set
	client.UpdatedAt = time.Now() // Ensure UpdatedAt is set
	// ExcludeFromClientPotfile defaults to false (not excluded = writes to client potfile)
	_, err := r.db.ExecContext(ctx, queries.CreateClientQuery, // Use constant
		client.ID,
		client.Name,
		client.Description,
		client.ContactInfo,
		client.DataRetentionMonths,
		client.ExcludeFromPotfile,
		client.ExcludeFromClientPotfile,
		client.RemoveFromGlobalPotfileOnHashlistDelete,
		client.RemoveFromClientPotfileOnHashlistDelete,
		client.CreatedAt,
		client.UpdatedAt,
	)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" { // unique_violation
			return fmt.Errorf("client with name '%s' already exists: %w", client.Name, ErrDuplicateRecord)
		}
		return fmt.Errorf("failed to create client: %w", err)
	}
	return nil
}

// GetByID retrieves a client by its ID.
func (r *ClientRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Client, error) {
	row := r.db.QueryRowContext(ctx, queries.GetClientByIDQuery, id) // Use constant
	var client models.Client
	err := row.Scan(
		&client.ID,
		&client.Name,
		&client.Description,
		&client.ContactInfo,
		&client.DataRetentionMonths,
		&client.ExcludeFromPotfile,
		&client.ExcludeFromClientPotfile,
		&client.RemoveFromGlobalPotfileOnHashlistDelete,
		&client.RemoveFromClientPotfileOnHashlistDelete,
		&client.CreatedAt,
		&client.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("client with ID %s not found: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get client by ID %s: %w", id, err)
	}
	return &client, nil
}

// GetByName retrieves a single client by its name.
func (r *ClientRepository) GetByName(ctx context.Context, name string) (*models.Client, error) {
	row := r.db.QueryRowContext(ctx, queries.GetClientByNameQuery, name) // Use constant
	var client models.Client
	err := row.Scan(
		&client.ID,
		&client.Name,
		&client.Description,
		&client.ContactInfo,
		&client.DataRetentionMonths,
		&client.ExcludeFromPotfile,
		&client.ExcludeFromClientPotfile,
		&client.RemoveFromGlobalPotfileOnHashlistDelete,
		&client.RemoveFromClientPotfileOnHashlistDelete,
		&client.CreatedAt,
		&client.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // Return nil, nil when not found
		}
		return nil, fmt.Errorf("failed to get client by name %s: %w", name, err)
	}
	return &client, nil
}

// List retrieves all clients from the database.
func (r *ClientRepository) List(ctx context.Context) ([]models.Client, error) {
	rows, err := r.db.QueryContext(ctx, queries.ListClientsQuery) // Use constant
	if err != nil {
		return nil, fmt.Errorf("failed to list clients: %w", err)
	}
	defer rows.Close()

	var clients []models.Client
	for rows.Next() {
		var client models.Client
		if err := rows.Scan(
			&client.ID,
			&client.Name,
			&client.Description,
			&client.ContactInfo,
			&client.DataRetentionMonths,
			&client.ExcludeFromPotfile,
			&client.ExcludeFromClientPotfile,
			&client.RemoveFromGlobalPotfileOnHashlistDelete,
			&client.RemoveFromClientPotfileOnHashlistDelete,
			&client.CreatedAt,
			&client.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan client row: %w", err)
		}
		clients = append(clients, client)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating client rows: %w", err)
	}

	return clients, nil
}

// ListWithCrackedCounts retrieves all clients with their cracked hash counts
func (r *ClientRepository) ListWithCrackedCounts(ctx context.Context) ([]models.Client, error) {
	rows, err := r.db.QueryContext(ctx, queries.ListClientsWithCrackedCountsQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to list clients with cracked counts: %w", err)
	}
	defer rows.Close()

	var clients []models.Client
	for rows.Next() {
		var client models.Client
		var crackedCount int
		var wordlistCount int
		if err := rows.Scan(
			&client.ID,
			&client.Name,
			&client.Description,
			&client.ContactInfo,
			&client.DataRetentionMonths,
			&client.ExcludeFromPotfile,
			&client.ExcludeFromClientPotfile,
			&client.RemoveFromGlobalPotfileOnHashlistDelete,
			&client.RemoveFromClientPotfileOnHashlistDelete,
			&client.CreatedAt,
			&client.UpdatedAt,
			&crackedCount,
			&wordlistCount,
		); err != nil {
			return nil, fmt.Errorf("failed to scan client row with cracked count: %w", err)
		}
		client.CrackedCount = &crackedCount
		client.WordlistCount = &wordlistCount
		clients = append(clients, client)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating client rows with cracked counts: %w", err)
	}

	return clients, nil
}

// ListWithCrackedCountsByTeamIDs retrieves clients filtered by team membership with cracked hash counts
func (r *ClientRepository) ListWithCrackedCountsByTeamIDs(ctx context.Context, teamIDs []uuid.UUID) ([]models.Client, error) {
	if len(teamIDs) == 0 {
		return []models.Client{}, nil
	}

	// Build placeholders for IN clause
	placeholders := make([]string, len(teamIDs))
	args := make([]interface{}, len(teamIDs))
	for i, id := range teamIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := fmt.Sprintf(queries.ListClientsWithCrackedCountsByTeamsQueryBase, strings.Join(placeholders, ", "))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list clients with cracked counts by team IDs: %w", err)
	}
	defer rows.Close()

	var clients []models.Client
	for rows.Next() {
		var client models.Client
		var crackedCount int
		var wordlistCount int
		if err := rows.Scan(
			&client.ID,
			&client.Name,
			&client.Description,
			&client.ContactInfo,
			&client.DataRetentionMonths,
			&client.ExcludeFromPotfile,
			&client.ExcludeFromClientPotfile,
			&client.RemoveFromGlobalPotfileOnHashlistDelete,
			&client.RemoveFromClientPotfileOnHashlistDelete,
			&client.CreatedAt,
			&client.UpdatedAt,
			&crackedCount,
			&wordlistCount,
		); err != nil {
			return nil, fmt.Errorf("failed to scan client row with cracked count: %w", err)
		}
		client.CrackedCount = &crackedCount
		client.WordlistCount = &wordlistCount
		clients = append(clients, client)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating client rows with cracked counts: %w", err)
	}

	return clients, nil
}

// Search retrieves clients matching a search query (name, description).
func (r *ClientRepository) Search(ctx context.Context, query string) ([]models.Client, error) {
	searchTerm := "%" + strings.ToLower(query) + "%"                            // Case-insensitive search
	rows, err := r.db.QueryContext(ctx, queries.SearchClientsQuery, searchTerm) // Use constant
	if err != nil {
		return nil, fmt.Errorf("failed to search clients with query '%s': %w", query, err)
	}
	defer rows.Close()

	var clients []models.Client
	for rows.Next() {
		var client models.Client
		if err := rows.Scan(
			&client.ID,
			&client.Name,
			&client.Description,
			&client.ContactInfo,
			&client.DataRetentionMonths,
			&client.ExcludeFromPotfile,
			&client.ExcludeFromClientPotfile,
			&client.RemoveFromGlobalPotfileOnHashlistDelete,
			&client.RemoveFromClientPotfileOnHashlistDelete,
			&client.CreatedAt,
			&client.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan client search result row: %w", err)
		}
		clients = append(clients, client)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating client search results: %w", err)
	}

	return clients, nil
}

// Update modifies an existing client record in the database.
func (r *ClientRepository) Update(ctx context.Context, client *models.Client) error {
	client.UpdatedAt = time.Now()                                   // Ensure UpdatedAt is set
	result, err := r.db.ExecContext(ctx, queries.UpdateClientQuery, // Use constant
		client.Name,
		client.Description,
		client.ContactInfo,
		client.DataRetentionMonths,
		client.ExcludeFromPotfile,
		client.ExcludeFromClientPotfile,
		client.RemoveFromGlobalPotfileOnHashlistDelete,
		client.RemoveFromClientPotfileOnHashlistDelete,
		client.UpdatedAt,
		client.ID,
	)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return fmt.Errorf("client with name '%s' already exists: %w", client.Name, ErrDuplicateRecord)
		}
		return fmt.Errorf("failed to update client %s: %w", client.ID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		debug.Warning("Could not get rows affected after updating client %s: %v", client.ID, err)
	} else if rowsAffected == 0 {
		return fmt.Errorf("client with ID %s not found for update: %w", client.ID, ErrNotFound)
	}

	return nil
}

// Delete removes a client record from the database by its ID.
func (r *ClientRepository) Delete(ctx context.Context, id uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, queries.DeleteClientQuery, id) // Use constant
	if err != nil {
		return fmt.Errorf("failed to delete client %s: %w", id, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		debug.Warning("Could not get rows affected after deleting client %s: %v", id, err)
	} else if rowsAffected == 0 {
		return fmt.Errorf("client with ID %s not found for deletion: %w", id, ErrNotFound)
	}

	return nil
}

// IsExcludedFromPotfile checks if a client has potfile exclusion enabled
func (r *ClientRepository) IsExcludedFromPotfile(ctx context.Context, clientID uuid.UUID) (bool, error) {
	query := `SELECT exclude_from_potfile FROM clients WHERE id = $1`
	var excluded bool
	err := r.db.QueryRowContext(ctx, query, clientID).Scan(&excluded)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, fmt.Errorf("client with ID %s not found: %w", clientID, ErrNotFound)
		}
		return false, fmt.Errorf("failed to check potfile exclusion for client %s: %w", clientID, err)
	}
	return excluded, nil
}

// GetClientPotfileSettings retrieves the potfile-related settings for a client
// Returns excludeFromClientPotfile (true = don't write to client potfile)
func (r *ClientRepository) GetClientPotfileSettings(ctx context.Context, clientID uuid.UUID) (excludeFromClientPotfile bool, excludeFromGlobalPotfile bool, err error) {
	query := `SELECT exclude_from_client_potfile, exclude_from_potfile FROM clients WHERE id = $1`
	err = r.db.QueryRowContext(ctx, query, clientID).Scan(&excludeFromClientPotfile, &excludeFromGlobalPotfile)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, false, fmt.Errorf("client with ID %s not found: %w", clientID, ErrNotFound)
		}
		return false, false, fmt.Errorf("failed to get potfile settings for client %s: %w", clientID, err)
	}
	return excludeFromClientPotfile, excludeFromGlobalPotfile, nil
}

// ClientListFilters contains optional filters for client listing
type ClientListFilters struct {
	Search string
	Limit  int
	Offset int
}

// ListForTeams returns clients accessible to the given teams
// If teamIDs is empty or nil, returns all clients (for admin or when teams disabled)
func (r *ClientRepository) ListForTeams(ctx context.Context, teamIDs []uuid.UUID, filters *ClientListFilters) ([]models.Client, error) {
	var query string
	var args []interface{}
	argIndex := 1

	if len(teamIDs) == 0 {
		// No team filter - return all (admin mode or teams disabled)
		query = `
			SELECT id, name, description, data_retention_months,
			       exclude_from_potfile, created_at, updated_at
			FROM clients
			WHERE 1=1`
	} else {
		// Build team filter
		placeholders := make([]string, len(teamIDs))
		for i, id := range teamIDs {
			placeholders[i] = fmt.Sprintf("$%d", argIndex)
			args = append(args, id)
			argIndex++
		}

		query = fmt.Sprintf(`
			SELECT DISTINCT c.id, c.name, c.description, c.data_retention_months,
			       c.exclude_from_potfile, c.created_at, c.updated_at
			FROM clients c
			INNER JOIN client_teams ct ON c.id = ct.client_id
			WHERE ct.team_id IN (%s)`, strings.Join(placeholders, ", "))
	}

	// Apply additional filters
	if filters != nil {
		if filters.Search != "" {
			query += fmt.Sprintf(` AND (LOWER(c.name) LIKE LOWER($%d) OR LOWER(c.description) LIKE LOWER($%d))`, argIndex, argIndex)
			args = append(args, "%"+filters.Search+"%")
			argIndex++
		}
	}

	query += ` ORDER BY c.name ASC`

	if filters != nil && filters.Limit > 0 {
		query += fmt.Sprintf(` LIMIT $%d`, argIndex)
		args = append(args, filters.Limit)
		argIndex++

		if filters.Offset > 0 {
			query += fmt.Sprintf(` OFFSET $%d`, argIndex)
			args = append(args, filters.Offset)
		}
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query clients: %w", err)
	}
	defer rows.Close()

	var clients []models.Client
	for rows.Next() {
		var c models.Client
		err := rows.Scan(
			&c.ID, &c.Name, &c.Description, &c.DataRetentionMonths,
			&c.ExcludeFromPotfile, &c.CreatedAt, &c.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan client: %w", err)
		}
		clients = append(clients, c)
	}

	return clients, rows.Err()
}
