package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db/queries"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

// TeamRepository handles database operations for teams
type TeamRepository struct {
	db *db.DB
}

// NewTeamRepository creates a new team repository
func NewTeamRepository(db *db.DB) *TeamRepository {
	return &TeamRepository{db: db}
}

// Create creates a new team
func (r *TeamRepository) Create(ctx context.Context, team *models.Team) error {
	err := r.db.QueryRowContext(ctx, queries.CreateTeam,
		team.ID,
		team.Name,
		team.Description,
		team.CreatedAt,
		team.UpdatedAt,
	).Scan(&team.ID)

	if err != nil {
		return fmt.Errorf("failed to create team: %w", err)
	}

	return nil
}

// GetByID retrieves a team by ID
func (r *TeamRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Team, error) {
	team := &models.Team{}

	err := r.db.QueryRowContext(ctx, queries.GetTeamByID, id).Scan(
		&team.ID,
		&team.Name,
		&team.Description,
		&team.CreatedAt,
		&team.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("team not found: %s", id)
	} else if err != nil {
		return nil, fmt.Errorf("failed to get team: %w", err)
	}

	// Get team's users
	users, err := r.getTeamUsers(ctx, team.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get team users: %w", err)
	}
	team.Users = users

	// Get team's agents
	agents, err := r.getTeamAgents(ctx, team.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get team agents: %w", err)
	}
	team.Agents = agents

	return team, nil
}

// Update updates a team
func (r *TeamRepository) Update(ctx context.Context, team *models.Team) error {
	result, err := r.db.ExecContext(ctx, queries.UpdateTeam,
		team.ID,
		team.Name,
		team.Description,
		team.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to update team: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("team not found: %s", team.ID)
	}

	return nil
}

// Delete deletes a team
func (r *TeamRepository) Delete(ctx context.Context, id uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, queries.DeleteTeam, id)
	if err != nil {
		return fmt.Errorf("failed to delete team: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("team not found: %s", id)
	}

	return nil
}

// AddUser adds a user to a team with a specified role
// This replaces the existing AddUser(ctx, teamID, userID string) which had no role parameter.
// The role parameter must be "member" or "admin".
func (r *TeamRepository) AddUser(ctx context.Context, teamID, userID uuid.UUID, role string) error {
	if role != models.TeamRoleMember && role != models.TeamRoleAdmin {
		return fmt.Errorf("invalid role: %s (must be 'member' or 'admin')", role)
	}

	_, err := r.db.ExecContext(ctx, queries.AddUserToTeam, userID, teamID, role)
	if err != nil {
		return fmt.Errorf("failed to add user to team: %w", err)
	}

	return nil
}

// RemoveUser removes a user from the team
func (r *TeamRepository) RemoveUser(ctx context.Context, teamID, userID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, queries.RemoveUserFromTeam, userID, teamID)
	if err != nil {
		return fmt.Errorf("failed to remove user from team: %w", err)
	}
	return nil
}

// AddAgent adds an agent to the team
func (r *TeamRepository) AddAgent(ctx context.Context, teamID uuid.UUID, agentID int) error {
	_, err := r.db.ExecContext(ctx, queries.AddAgentToTeam, agentID, teamID)
	if err != nil {
		return fmt.Errorf("failed to add agent to team: %w", err)
	}
	return nil
}

// RemoveAgent removes an agent from the team
func (r *TeamRepository) RemoveAgent(ctx context.Context, teamID uuid.UUID, agentID int) error {
	_, err := r.db.ExecContext(ctx, queries.RemoveAgentFromTeam, agentID, teamID)
	if err != nil {
		return fmt.Errorf("failed to remove agent from team: %w", err)
	}
	return nil
}

// getTeamUsers retrieves all users in a team
func (r *TeamRepository) getTeamUsers(ctx context.Context, teamID uuid.UUID) ([]models.User, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT u.id, u.username, u.email, u.role, u.created_at, u.updated_at
		FROM users u
		JOIN user_teams ut ON u.id = ut.user_id
		WHERE ut.team_id = $1
	`, teamID)
	if err != nil {
		return nil, fmt.Errorf("failed to get team users: %w", err)
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var user models.User
		err := rows.Scan(
			&user.ID,
			&user.Username,
			&user.Email,
			&user.Role,
			&user.CreatedAt,
			&user.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, user)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating users: %w", err)
	}

	return users, nil
}

// getTeamAgents retrieves all agents in a team
func (r *TeamRepository) getTeamAgents(ctx context.Context, teamID uuid.UUID) ([]models.Agent, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.id, a.name, a.status, a.version, a.created_at, a.updated_at
		FROM agents a
		JOIN agent_teams at ON a.id = at.agent_id
		WHERE at.team_id = $1
	`, teamID)
	if err != nil {
		return nil, fmt.Errorf("failed to get team agents: %w", err)
	}
	defer rows.Close()

	var agents []models.Agent
	for rows.Next() {
		var agent models.Agent
		err := rows.Scan(
			&agent.ID,
			&agent.Name,
			&agent.Status,
			&agent.Version,
			&agent.CreatedAt,
			&agent.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan agent: %w", err)
		}
		agents = append(agents, agent)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating agents: %w", err)
	}

	return agents, nil
}

// List retrieves all teams with optional filters
func (r *TeamRepository) List(ctx context.Context, filters map[string]interface{}) ([]models.Team, error) {
	query := `
		SELECT id, name, description, created_at, updated_at
		FROM teams
		WHERE 1=1
	`
	args := make([]interface{}, 0)
	argPos := 1

	// Add filters if needed
	if name, ok := filters["name"].(string); ok {
		query += fmt.Sprintf(" AND name ILIKE $%d", argPos)
		args = append(args, "%"+name+"%")
		argPos++
	}

	query += " ORDER BY created_at DESC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list teams: %w", err)
	}
	defer rows.Close()

	var teams []models.Team
	for rows.Next() {
		var team models.Team
		err := rows.Scan(
			&team.ID,
			&team.Name,
			&team.Description,
			&team.CreatedAt,
			&team.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan team: %w", err)
		}

		// Get team's users
		users, err := r.getTeamUsers(ctx, team.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get team users: %w", err)
		}
		team.Users = users

		// Get team's agents
		agents, err := r.getTeamAgents(ctx, team.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get team agents: %w", err)
		}
		team.Agents = agents

		teams = append(teams, team)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating teams: %w", err)
	}

	return teams, nil
}

// ============================================================
// New methods for multi-team dynamics
// ============================================================

// TeamWithRole extends Team with the user's role and aggregate counts
type TeamWithRole struct {
	models.Team
	UserRole      string `json:"user_role"`
	MemberCount   int    `json:"member_count"`
	ClientCount   int    `json:"client_count"`
	HashlistCount int    `json:"hashlist_count"`
}

// TeamMember represents a user's membership in a team
type TeamMember struct {
	UserID   uuid.UUID `json:"user_id"`
	Username string    `json:"username"`
	Email    string    `json:"email"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

// GetTeamsForUser returns all teams a user belongs to, including their role
func (r *TeamRepository) GetTeamsForUser(ctx context.Context, userID uuid.UUID) ([]TeamWithRole, error) {
	rows, err := r.db.QueryContext(ctx, queries.GetTeamsForUser, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query user teams: %w", err)
	}
	defer rows.Close()

	var teams []TeamWithRole
	for rows.Next() {
		var t TeamWithRole
		err := rows.Scan(
			&t.ID,
			&t.Name,
			&t.Description,
			&t.CreatedAt,
			&t.UpdatedAt,
			&t.UserRole,
			&t.MemberCount,
			&t.ClientCount,
			&t.HashlistCount,
		)
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

// ListAllWithCounts returns all teams with member/client/hashlist counts (admin view)
func (r *TeamRepository) ListAllWithCounts(ctx context.Context) ([]TeamWithRole, error) {
	rows, err := r.db.QueryContext(ctx, queries.GetAllTeamsWithCounts)
	if err != nil {
		return nil, fmt.Errorf("failed to query all teams with counts: %w", err)
	}
	defer rows.Close()

	var teams []TeamWithRole
	for rows.Next() {
		var t TeamWithRole
		err := rows.Scan(
			&t.ID,
			&t.Name,
			&t.Description,
			&t.CreatedAt,
			&t.UpdatedAt,
			&t.MemberCount,
			&t.ClientCount,
			&t.HashlistCount,
		)
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

// GetUserTeamIDs returns just the team IDs for a user (efficient for access checks)
func (r *TeamRepository) GetUserTeamIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.QueryContext(ctx, queries.GetUserTeamIDs, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query user team IDs: %w", err)
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

// IsUserInTeam checks if a user is a member of a specific team
func (r *TeamRepository) IsUserInTeam(ctx context.Context, userID, teamID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, queries.IsUserInTeam, userID, teamID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check team membership: %w", err)
	}
	return exists, nil
}

// GetTeamRole returns the user's role in a specific team
// Returns empty string if user is not in the team
func (r *TeamRepository) GetTeamRole(ctx context.Context, userID, teamID uuid.UUID) (string, error) {
	var role string
	err := r.db.QueryRowContext(ctx, queries.GetTeamRoleForUser, userID, teamID).Scan(&role)
	if err == sql.ErrNoRows {
		return "", nil // User not in team
	}
	if err != nil {
		return "", fmt.Errorf("failed to get team role: %w", err)
	}
	return role, nil
}

// SetUserTeamRole updates a user's role in a team
func (r *TeamRepository) SetUserTeamRole(ctx context.Context, userID, teamID uuid.UUID, role string) error {
	// Validate role
	if role != models.TeamRoleMember && role != models.TeamRoleAdmin {
		return fmt.Errorf("invalid role: %s (must be 'member' or 'admin')", role)
	}

	result, err := r.db.ExecContext(ctx, queries.SetUserTeamRole, userID, teamID, role)
	if err != nil {
		return fmt.Errorf("failed to update team role: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return models.ErrNotTeamMember
	}

	return nil
}

// SearchUsersNotInTeam searches for users who are not members of a team
// Supports pagination via limit and offset parameters
func (r *TeamRepository) SearchUsersNotInTeam(ctx context.Context, teamID uuid.UUID, query string, limit, offset int) ([]models.User, error) {
	// Add wildcards for LIKE search
	searchPattern := "%" + query + "%"

	rows, err := r.db.QueryContext(ctx, queries.SearchUsersNotInTeamPaginated, teamID, searchPattern, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to search users: %w", err)
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user row: %w", err)
		}
		users = append(users, u)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating user rows: %w", err)
	}

	return users, nil
}

// CountTeamAdmins returns the number of admins in a team
// Used to prevent removing the last admin
func (r *TeamRepository) CountTeamAdmins(ctx context.Context, teamID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, queries.CountTeamAdmins, teamID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count team admins: %w", err)
	}
	return count, nil
}

// GetTeamMembers returns all members of a team with their roles
func (r *TeamRepository) GetTeamMembers(ctx context.Context, teamID uuid.UUID) ([]TeamMember, error) {
	rows, err := r.db.QueryContext(ctx, queries.GetTeamMembers, teamID)
	if err != nil {
		return nil, fmt.Errorf("failed to query team members: %w", err)
	}
	defer rows.Close()

	var members []TeamMember
	for rows.Next() {
		var m TeamMember
		err := rows.Scan(&m.UserID, &m.Username, &m.Email, &m.Role, &m.JoinedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan member row: %w", err)
		}
		members = append(members, m)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating member rows: %w", err)
	}

	return members, nil
}

// GetTeamIDsForAgent returns team IDs explicitly assigned to an agent
// Used when agent.AdminOverrideTeams is true
// Note: Agent.ID is int, not uuid.UUID
func (r *TeamRepository) GetTeamIDsForAgent(ctx context.Context, agentID int) ([]uuid.UUID, error) {
	rows, err := r.db.QueryContext(ctx, queries.GetTeamIDsForAgent, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to query agent team IDs: %w", err)
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

// IsDefaultTeam checks if the given team ID is the Default Team
func (r *TeamRepository) IsDefaultTeam(teamID uuid.UUID) bool {
	return teamID == models.DefaultTeamID
}

// ============================================================
// Transaction-aware (Tx) methods
// ============================================================
// These methods mirror their non-Tx counterparts but execute
// against an explicit *sql.Tx instead of r.db. Use them inside
// service-layer transactions to ensure atomic, serializable
// operations (e.g., team creation + admin seeding, or last-admin
// checks before role demotion/removal).

// CreateTx creates a new team within a transaction
func (r *TeamRepository) CreateTx(ctx context.Context, tx *sql.Tx, team *models.Team) error {
	err := tx.QueryRowContext(ctx,
		`INSERT INTO teams (name, description, created_at, updated_at)
		 VALUES ($1, $2, NOW(), NOW())
		 RETURNING id, created_at, updated_at`,
		team.Name, team.Description,
	).Scan(&team.ID, &team.CreatedAt, &team.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create team (tx): %w", err)
	}
	return nil
}

// AddUserTx adds a user to a team with a specified role within a transaction
func (r *TeamRepository) AddUserTx(ctx context.Context, tx *sql.Tx, teamID, userID uuid.UUID, role string) error {
	if role != models.TeamRoleMember && role != models.TeamRoleAdmin {
		return fmt.Errorf("invalid role: %s (must be 'member' or 'admin')", role)
	}

	_, err := tx.ExecContext(ctx, queries.AddUserToTeam, userID, teamID, role)
	if err != nil {
		return fmt.Errorf("failed to add user to team (tx): %w", err)
	}
	return nil
}

// RemoveUserTx removes a user from a team within a transaction
func (r *TeamRepository) RemoveUserTx(ctx context.Context, tx *sql.Tx, teamID, userID uuid.UUID) error {
	result, err := tx.ExecContext(ctx,
		`DELETE FROM user_teams WHERE user_id = $1 AND team_id = $2`,
		userID, teamID,
	)
	if err != nil {
		return fmt.Errorf("failed to remove user from team (tx): %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected (tx): %w", err)
	}
	if rowsAffected == 0 {
		return models.ErrNotTeamMember
	}
	return nil
}

// GetTeamRoleTx returns a user's role in a specific team within a transaction
// Returns empty string if the user is not in the team
func (r *TeamRepository) GetTeamRoleTx(ctx context.Context, tx *sql.Tx, userID, teamID uuid.UUID) (string, error) {
	var role string
	err := tx.QueryRowContext(ctx, queries.GetTeamRoleForUser, userID, teamID).Scan(&role)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get team role (tx): %w", err)
	}
	return role, nil
}

// CountTeamAdminsTx returns the number of admins in a team within a transaction.
// Uses FOR UPDATE locking to prevent concurrent modifications from violating the
// last-admin invariant. Two simultaneous demote/remove requests will be serialized
// by the row-level lock, ensuring that at least one admin always remains.
func (r *TeamRepository) CountTeamAdminsTx(ctx context.Context, tx *sql.Tx, teamID uuid.UUID) (int, error) {
	var count int
	err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM user_teams WHERE team_id = $1 AND role = 'admin' FOR UPDATE`,
		teamID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count team admins (tx): %w", err)
	}
	return count, nil
}

// DeleteTx deletes a team within a transaction
// Must be called after removing all client_teams and user_teams entries
// due to ON DELETE RESTRICT foreign keys
func (r *TeamRepository) DeleteTx(ctx context.Context, tx *sql.Tx, id uuid.UUID) error {
	// Also remove user_teams entries for the team
	_, err := tx.ExecContext(ctx, `DELETE FROM user_teams WHERE team_id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to remove user_teams for team (tx): %w", err)
	}

	result, err := tx.ExecContext(ctx, queries.DeleteTeam, id)
	if err != nil {
		return fmt.Errorf("failed to delete team (tx): %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected (tx): %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("team not found: %s", id)
	}

	return nil
}

// SetUserTeamRoleTx updates a user's role in a team within a transaction
func (r *TeamRepository) SetUserTeamRoleTx(ctx context.Context, tx *sql.Tx, userID, teamID uuid.UUID, role string) error {
	if role != models.TeamRoleMember && role != models.TeamRoleAdmin {
		return fmt.Errorf("invalid role: %s (must be 'member' or 'admin')", role)
	}

	result, err := tx.ExecContext(ctx, queries.SetUserTeamRole, userID, teamID, role)
	if err != nil {
		return fmt.Errorf("failed to update team role (tx): %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected (tx): %w", err)
	}
	if rowsAffected == 0 {
		return models.ErrNotTeamMember
	}
	return nil
}
