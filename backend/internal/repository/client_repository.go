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
	// Set defaults for new potfile fields
	if !client.ContributeToGlobalPotfile {
		client.ContributeToGlobalPotfile = true // Default to true for cascading mode
	}
	_, err := r.db.ExecContext(ctx, queries.CreateClientQuery, // Use constant
		client.ID,
		client.Name,
		client.Description,
		client.ContactInfo,
		client.DataRetentionMonths,
		client.ExcludeFromPotfile,
		client.EnableClientPotfile,
		client.ContributeToGlobalPotfile,
		client.RemovePasswordsOnHashlistDelete,
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
		&client.EnableClientPotfile,
		&client.ContributeToGlobalPotfile,
		&client.RemovePasswordsOnHashlistDelete,
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
		&client.EnableClientPotfile,
		&client.ContributeToGlobalPotfile,
		&client.RemovePasswordsOnHashlistDelete,
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
			&client.EnableClientPotfile,
			&client.ContributeToGlobalPotfile,
			&client.RemovePasswordsOnHashlistDelete,
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
		if err := rows.Scan(
			&client.ID,
			&client.Name,
			&client.Description,
			&client.ContactInfo,
			&client.DataRetentionMonths,
			&client.ExcludeFromPotfile,
			&client.EnableClientPotfile,
			&client.ContributeToGlobalPotfile,
			&client.RemovePasswordsOnHashlistDelete,
			&client.CreatedAt,
			&client.UpdatedAt,
			&crackedCount,
		); err != nil {
			return nil, fmt.Errorf("failed to scan client row with cracked count: %w", err)
		}
		client.CrackedCount = &crackedCount
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
			&client.EnableClientPotfile,
			&client.ContributeToGlobalPotfile,
			&client.RemovePasswordsOnHashlistDelete,
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
		client.EnableClientPotfile,
		client.ContributeToGlobalPotfile,
		client.RemovePasswordsOnHashlistDelete,
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
func (r *ClientRepository) GetClientPotfileSettings(ctx context.Context, clientID uuid.UUID) (enableClientPotfile bool, contributeToGlobal bool, err error) {
	query := `SELECT enable_client_potfile, contribute_to_global_potfile FROM clients WHERE id = $1`
	err = r.db.QueryRowContext(ctx, query, clientID).Scan(&enableClientPotfile, &contributeToGlobal)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, true, fmt.Errorf("client with ID %s not found: %w", clientID, ErrNotFound)
		}
		return false, true, fmt.Errorf("failed to get potfile settings for client %s: %w", clientID, err)
	}
	return enableClientPotfile, contributeToGlobal, nil
}
