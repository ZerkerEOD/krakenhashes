package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db/queries"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

// ClientTeamRepository handles client-team association operations
type ClientTeamRepository struct {
	db *db.DB
}

// NewClientTeamRepository creates a new ClientTeamRepository
func NewClientTeamRepository(database *db.DB) *ClientTeamRepository {
	return &ClientTeamRepository{db: database}
}

// ClientTeamAssignment represents a client-team relationship
type ClientTeamAssignment struct {
	ClientID   uuid.UUID  `json:"client_id"`
	TeamID     uuid.UUID  `json:"team_id"`
	AssignedAt time.Time  `json:"assigned_at"`
	AssignedBy *uuid.UUID `json:"assigned_by,omitempty"`
}

// AssignClientToTeam creates an association between a client and a team
func (r *ClientTeamRepository) AssignClientToTeam(ctx context.Context, clientID, teamID uuid.UUID, assignedBy *uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, queries.AssignClientToTeam, clientID, teamID, assignedBy)
	if err != nil {
		return fmt.Errorf("failed to assign client to team: %w", err)
	}

	return nil
}

// AssignClientToTeamWithResult assigns a client to a team and reports if a new row was inserted.
// Returns true if newly assigned, false if already existed (ON CONFLICT DO NOTHING).
func (r *ClientTeamRepository) AssignClientToTeamWithResult(ctx context.Context, clientID, teamID uuid.UUID, assignedBy *uuid.UUID) (bool, error) {
	result, err := r.db.ExecContext(ctx, queries.AssignClientToTeam, clientID, teamID, assignedBy)
	if err != nil {
		return false, fmt.Errorf("failed to assign client to team: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to get rows affected: %w", err)
	}
	return rowsAffected > 0, nil
}

// RemoveClientFromTeam removes an association between a client and a team
func (r *ClientTeamRepository) RemoveClientFromTeam(ctx context.Context, clientID, teamID uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, queries.RemoveClientFromTeam, clientID, teamID)
	if err != nil {
		return fmt.Errorf("failed to remove client from team: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return models.ErrClientNotInTeam
	}

	return nil
}

// GetTeamsForClient returns all teams a client is assigned to
func (r *ClientTeamRepository) GetTeamsForClient(ctx context.Context, clientID uuid.UUID) ([]models.Team, error) {
	rows, err := r.db.QueryContext(ctx, queries.GetTeamsForClient, clientID)
	if err != nil {
		return nil, fmt.Errorf("failed to query teams for client: %w", err)
	}
	defer rows.Close()

	var teams []models.Team
	for rows.Next() {
		var t models.Team
		err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.CreatedAt, &t.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan team row: %w", err)
		}
		teams = append(teams, t)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating team rows: %w", err)
	}

	return teams, nil
}

// GetTeamIDsForClient returns just the team IDs for a client (efficient for access checks)
func (r *ClientTeamRepository) GetTeamIDsForClient(ctx context.Context, clientID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.QueryContext(ctx, queries.GetTeamIDsForClient, clientID)
	if err != nil {
		return nil, fmt.Errorf("failed to query team IDs for client: %w", err)
	}
	defer rows.Close()

	var teamIDs []uuid.UUID
	for rows.Next() {
		var teamID uuid.UUID
		if err := rows.Scan(&teamID); err != nil {
			return nil, fmt.Errorf("failed to scan team ID: %w", err)
		}
		teamIDs = append(teamIDs, teamID)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating team ID rows: %w", err)
	}

	return teamIDs, nil
}

// GetClientsForTeams returns all clients accessible to the given teams
func (r *ClientTeamRepository) GetClientsForTeams(ctx context.Context, teamIDs []uuid.UUID) ([]models.Client, error) {
	if len(teamIDs) == 0 {
		return []models.Client{}, nil
	}

	// Build placeholder string for IN clause
	placeholders := make([]string, len(teamIDs))
	args := make([]interface{}, len(teamIDs))
	for i, id := range teamIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := fmt.Sprintf(queries.GetClientsForTeamsBase, strings.Join(placeholders, ", "))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query clients for teams: %w", err)
	}
	defer rows.Close()

	clients := []models.Client{}
	for rows.Next() {
		var c models.Client
		err := rows.Scan(
			&c.ID, &c.Name, &c.Description, &c.DataRetentionMonths,
			&c.ExcludeFromPotfile, &c.CreatedAt, &c.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan client row: %w", err)
		}
		clients = append(clients, c)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating client rows: %w", err)
	}

	return clients, nil
}

// GetClientIDsForTeams returns just the client IDs accessible to the given teams
func (r *ClientTeamRepository) GetClientIDsForTeams(ctx context.Context, teamIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(teamIDs) == 0 {
		return []uuid.UUID{}, nil
	}

	placeholders := make([]string, len(teamIDs))
	args := make([]interface{}, len(teamIDs))
	for i, id := range teamIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := fmt.Sprintf(queries.GetClientIDsForTeamsBase, strings.Join(placeholders, ", "))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query client IDs for teams: %w", err)
	}
	defer rows.Close()

	var clientIDs []uuid.UUID
	for rows.Next() {
		var clientID uuid.UUID
		if err := rows.Scan(&clientID); err != nil {
			return nil, fmt.Errorf("failed to scan client ID: %w", err)
		}
		clientIDs = append(clientIDs, clientID)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating client ID rows: %w", err)
	}

	return clientIDs, nil
}

// IsClientAccessibleByUser checks if a user can access a client via team membership
func (r *ClientTeamRepository) IsClientAccessibleByUser(ctx context.Context, clientID, userID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, queries.IsClientAccessibleByUser, clientID, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check client access: %w", err)
	}

	return exists, nil
}

// IsClientInTeams checks if a client is in any of the given teams
func (r *ClientTeamRepository) IsClientInTeams(ctx context.Context, clientID uuid.UUID, teamIDs []uuid.UUID) (bool, error) {
	if len(teamIDs) == 0 {
		return false, nil
	}

	placeholders := make([]string, len(teamIDs))
	args := make([]interface{}, len(teamIDs)+1)
	args[0] = clientID
	for i, id := range teamIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args[i+1] = id
	}

	query := fmt.Sprintf(queries.IsClientInTeamsBase, strings.Join(placeholders, ", "))

	var exists bool
	err := r.db.QueryRowContext(ctx, query, args...).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check if client is in teams: %w", err)
	}

	return exists, nil
}

// CountTeamsForClient returns the number of teams a client is assigned to
func (r *ClientTeamRepository) CountTeamsForClient(ctx context.Context, clientID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, queries.CountTeamsForClient, clientID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count teams for client: %w", err)
	}

	return count, nil
}

// ReassignClientToDefaultTeam moves a client to the Default Team
// Used when a client's only team is deleted
func (r *ClientTeamRepository) ReassignClientToDefaultTeam(ctx context.Context, clientID uuid.UUID) error {
	return r.AssignClientToTeam(ctx, clientID, models.DefaultTeamID, nil)
}

// GetAssignmentsForClient returns detailed assignment information
func (r *ClientTeamRepository) GetAssignmentsForClient(ctx context.Context, clientID uuid.UUID) ([]ClientTeamAssignment, error) {
	rows, err := r.db.QueryContext(ctx, queries.GetAssignmentsForClient, clientID)
	if err != nil {
		return nil, fmt.Errorf("failed to query assignments: %w", err)
	}
	defer rows.Close()

	var assignments []ClientTeamAssignment
	for rows.Next() {
		var a ClientTeamAssignment
		err := rows.Scan(&a.ClientID, &a.TeamID, &a.AssignedAt, &a.AssignedBy)
		if err != nil {
			return nil, fmt.Errorf("failed to scan assignment row: %w", err)
		}
		assignments = append(assignments, a)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating assignment rows: %w", err)
	}

	return assignments, nil
}

// ============================================================
// Transaction-aware (Tx) methods
// ============================================================

// GetClientsOnlyInTeamTx returns client IDs that are ONLY in the given team
// (not in any other team). Used during team deletion to find clients that
// would become orphaned and need reassignment to Default Team.
func (r *ClientTeamRepository) GetClientsOnlyInTeamTx(ctx context.Context, tx *sql.Tx, teamID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT ct.client_id
		FROM client_teams ct
		WHERE ct.team_id = $1
		AND NOT EXISTS (
			SELECT 1 FROM client_teams ct2
			WHERE ct2.client_id = ct.client_id
			AND ct2.team_id != $1
		)`, teamID)
	if err != nil {
		return nil, fmt.Errorf("failed to query clients only in team: %w", err)
	}
	defer rows.Close()

	var clientIDs []uuid.UUID
	for rows.Next() {
		var clientID uuid.UUID
		if err := rows.Scan(&clientID); err != nil {
			return nil, fmt.Errorf("failed to scan client ID: %w", err)
		}
		clientIDs = append(clientIDs, clientID)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating client ID rows: %w", err)
	}

	return clientIDs, nil
}

// AssignClientToTeamTx creates an association between a client and a team within a transaction
func (r *ClientTeamRepository) AssignClientToTeamTx(ctx context.Context, tx *sql.Tx, clientID, teamID uuid.UUID, assignedBy *uuid.UUID) error {
	_, err := tx.ExecContext(ctx, queries.AssignClientToTeam, clientID, teamID, assignedBy)
	if err != nil {
		return fmt.Errorf("failed to assign client to team (tx): %w", err)
	}
	return nil
}

// RemoveAllForTeamTx removes all client_teams entries for a specific team within a transaction.
// Used during team deletion — must be called before deleting the team due to ON DELETE RESTRICT.
func (r *ClientTeamRepository) RemoveAllForTeamTx(ctx context.Context, tx *sql.Tx, teamID uuid.UUID) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM client_teams WHERE team_id = $1`, teamID)
	if err != nil {
		return fmt.Errorf("failed to remove all client_teams for team (tx): %w", err)
	}
	return nil
}

// GetClientsWithoutTeam returns all clients not assigned to any team.
// Used during initial teams_enabled toggle to find orphaned clients
// that need to be assigned to the Default Team.
func (r *ClientTeamRepository) GetClientsWithoutTeam(ctx context.Context) ([]models.Client, error) {
	rows, err := r.db.QueryContext(ctx, queries.GetClientsWithoutTeam)
	if err != nil {
		return nil, fmt.Errorf("failed to query clients without team: %w", err)
	}
	defer rows.Close()

	clients := []models.Client{}
	for rows.Next() {
		var c models.Client
		err := rows.Scan(
			&c.ID, &c.Name, &c.Description, &c.DataRetentionMonths,
			&c.ExcludeFromPotfile, &c.CreatedAt, &c.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan client row: %w", err)
		}
		clients = append(clients, c)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating client rows: %w", err)
	}

	return clients, nil
}
