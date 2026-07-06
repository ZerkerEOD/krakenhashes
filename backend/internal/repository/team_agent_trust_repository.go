package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

// TeamAgentTrustRepository handles team agent trust operations
type TeamAgentTrustRepository struct {
	db *db.DB
}

// NewTeamAgentTrustRepository creates a new TeamAgentTrustRepository
func NewTeamAgentTrustRepository(database *db.DB) *TeamAgentTrustRepository {
	return &TeamAgentTrustRepository{db: database}
}

// AddTrust creates a trust relationship: teamID trusts trustedTeamID's agents
func (r *TeamAgentTrustRepository) AddTrust(ctx context.Context, teamID, trustedTeamID, createdBy uuid.UUID) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO team_agent_trust (team_id, trusted_team_id, created_by)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (team_id, trusted_team_id) DO NOTHING`,
		teamID, trustedTeamID, createdBy)
	if err != nil {
		return fmt.Errorf("failed to add trust relationship: %w", err)
	}
	return nil
}

// RemoveTrust removes a trust relationship
func (r *TeamAgentTrustRepository) RemoveTrust(ctx context.Context, teamID, trustedTeamID uuid.UUID) error {
	result, err := r.db.ExecContext(ctx,
		`DELETE FROM team_agent_trust WHERE team_id = $1 AND trusted_team_id = $2`,
		teamID, trustedTeamID)
	if err != nil {
		return fmt.Errorf("failed to remove trust relationship: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("trust relationship not found")
	}

	return nil
}

// GetTrustedTeamIDs returns the IDs of teams that teamID trusts
func (r *TeamAgentTrustRepository) GetTrustedTeamIDs(ctx context.Context, teamID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT trusted_team_id FROM team_agent_trust WHERE team_id = $1`,
		teamID)
	if err != nil {
		return nil, fmt.Errorf("failed to query trusted team IDs: %w", err)
	}
	defer rows.Close()

	var teamIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan trusted team ID: %w", err)
		}
		teamIDs = append(teamIDs, id)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating trusted team ID rows: %w", err)
	}

	return teamIDs, nil
}

// GetAllTrustRelationships bulk-loads all trust relationships for scheduler efficiency.
// Returns a map of teamID → []trustedTeamIDs
func (r *TeamAgentTrustRepository) GetAllTrustRelationships(ctx context.Context) (map[uuid.UUID][]uuid.UUID, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT team_id, trusted_team_id FROM team_agent_trust`)
	if err != nil {
		return nil, fmt.Errorf("failed to query all trust relationships: %w", err)
	}
	defer rows.Close()

	trustMap := make(map[uuid.UUID][]uuid.UUID)
	for rows.Next() {
		var teamID, trustedTeamID uuid.UUID
		if err := rows.Scan(&teamID, &trustedTeamID); err != nil {
			return nil, fmt.Errorf("failed to scan trust relationship: %w", err)
		}
		trustMap[teamID] = append(trustMap[teamID], trustedTeamID)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating trust relationship rows: %w", err)
	}

	return trustMap, nil
}

// IsTrusted checks if teamID trusts trustedTeamID
func (r *TeamAgentTrustRepository) IsTrusted(ctx context.Context, teamID, trustedTeamID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM team_agent_trust WHERE team_id = $1 AND trusted_team_id = $2)`,
		teamID, trustedTeamID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check trust: %w", err)
	}
	return exists, nil
}

// GetTrustForTeam returns detailed trust relationships for a team (for UI display)
func (r *TeamAgentTrustRepository) GetTrustForTeam(ctx context.Context, teamID uuid.UUID) ([]models.TeamAgentTrust, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT team_id, trusted_team_id, created_at, created_by
		 FROM team_agent_trust
		 WHERE team_id = $1
		 ORDER BY created_at`,
		teamID)
	if err != nil {
		return nil, fmt.Errorf("failed to query trust for team: %w", err)
	}
	defer rows.Close()

	var trusts []models.TeamAgentTrust
	for rows.Next() {
		var t models.TeamAgentTrust
		var createdAt time.Time
		var createdBy *uuid.UUID
		if err := rows.Scan(&t.TeamID, &t.TrustedTeamID, &createdAt, &createdBy); err != nil {
			return nil, fmt.Errorf("failed to scan trust row: %w", err)
		}
		t.CreatedAt = createdAt
		t.CreatedBy = createdBy
		trusts = append(trusts, t)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating trust rows: %w", err)
	}

	return trusts, nil
}
