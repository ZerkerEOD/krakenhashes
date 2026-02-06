// File: backend/internal/services/team_service.go

package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/google/uuid"
)

// TeamService handles team-related business logic and access control
type TeamService struct {
	db                 *db.DB // Direct DB access for transaction support
	teamRepo           *repository.TeamRepository
	clientTeamRepo     *repository.ClientTeamRepository
	hashlistRepo       *repository.HashListRepository
	jobRepo            *repository.JobExecutionRepository
	agentRepo          *repository.AgentRepository
	systemSettingsRepo *repository.SystemSettingsRepository

	// Cache for teams_enabled setting (refreshed periodically)
	teamsEnabledCache     bool
	teamsEnabledCacheMu   sync.RWMutex
	teamsEnabledCacheTime time.Time
	cacheTTL              time.Duration
}

// NewTeamService creates a new TeamService
func NewTeamService(
	database *db.DB,
	teamRepo *repository.TeamRepository,
	clientTeamRepo *repository.ClientTeamRepository,
	hashlistRepo *repository.HashListRepository,
	jobRepo *repository.JobExecutionRepository,
	agentRepo *repository.AgentRepository,
	systemSettingsRepo *repository.SystemSettingsRepository,
) *TeamService {
	return &TeamService{
		db:                 database,
		teamRepo:           teamRepo,
		clientTeamRepo:     clientTeamRepo,
		hashlistRepo:       hashlistRepo,
		jobRepo:            jobRepo,
		agentRepo:          agentRepo,
		systemSettingsRepo: systemSettingsRepo,
		cacheTTL:           5 * time.Second, // Cache teams_enabled for 5 seconds
	}
}

// =============================================================================
// Feature Toggle
// =============================================================================

// IsTeamsEnabled checks if multi-team mode is enabled
// Uses a short-lived cache to avoid repeated database queries
func (s *TeamService) IsTeamsEnabled(ctx context.Context) bool {
	s.teamsEnabledCacheMu.RLock()
	if time.Since(s.teamsEnabledCacheTime) < s.cacheTTL {
		enabled := s.teamsEnabledCache
		s.teamsEnabledCacheMu.RUnlock()
		return enabled
	}
	s.teamsEnabledCacheMu.RUnlock()

	// Cache expired, refresh
	s.teamsEnabledCacheMu.Lock()
	defer s.teamsEnabledCacheMu.Unlock()

	// Double-check after acquiring write lock
	if time.Since(s.teamsEnabledCacheTime) < s.cacheTTL {
		return s.teamsEnabledCache
	}

	// Fetch from database
	setting, err := s.systemSettingsRepo.GetSetting(ctx, models.SystemSettingTeamsEnabled)
	if err != nil {
		// Default to false on error
		s.teamsEnabledCache = false
	} else {
		s.teamsEnabledCache = setting.Value != nil && *setting.Value == "true"
	}
	s.teamsEnabledCacheTime = time.Now()

	return s.teamsEnabledCache
}

// InvalidateTeamsEnabledCache forces a refresh of the teams_enabled setting
// Call this after changing the setting via the admin API
func (s *TeamService) InvalidateTeamsEnabledCache() {
	s.teamsEnabledCacheMu.Lock()
	defer s.teamsEnabledCacheMu.Unlock()
	s.teamsEnabledCacheTime = time.Time{} // Zero time forces refresh
}

// OnTeamsEnabledChanged handles the side effects of toggling the teams_enabled setting.
// When teams are enabled, any clients not currently assigned to any team are
// automatically assigned to the Default Team so they remain accessible.
// This should be called from the admin settings handler when teams_enabled changes.
func (s *TeamService) OnTeamsEnabledChanged(ctx context.Context, enabled bool) error {
	// Update the system setting in the database
	value := "false"
	if enabled {
		value = "true"
	}
	if err := s.systemSettingsRepo.UpdateSetting(ctx, models.SystemSettingTeamsEnabled, value); err != nil {
		return fmt.Errorf("failed to update teams_enabled setting: %w", err)
	}

	if enabled {
		// Find all clients not in any team and assign them to the Default Team
		// This ensures no clients become "orphaned" (invisible) when teams mode is turned on.
		//
		// Query: SELECT c.id FROM clients c
		//        WHERE NOT EXISTS (SELECT 1 FROM client_teams ct WHERE ct.client_id = c.id)
		unassignedClients, err := s.clientTeamRepo.GetClientsWithoutTeam(ctx)
		if err != nil {
			return fmt.Errorf("failed to find unassigned clients: %w", err)
		}

		for _, client := range unassignedClients {
			if err := s.clientTeamRepo.AssignClientToTeam(ctx, client.ID, models.DefaultTeamID, nil); err != nil {
				return fmt.Errorf("failed to assign client %s to Default Team: %w", client.ID, err)
			}
		}

		if len(unassignedClients) > 0 {
			// Log the count of auto-assigned clients
			fmt.Printf("[TeamService] Auto-assigned %d unassigned clients to Default Team\n", len(unassignedClients))
		}
	}

	// Invalidate cache so the new setting takes effect immediately
	s.InvalidateTeamsEnabledCache()
	return nil
}

// =============================================================================
// User Team Queries
// =============================================================================

// GetUserTeams returns all teams a user belongs to
func (s *TeamService) GetUserTeams(ctx context.Context, userID uuid.UUID) ([]repository.TeamWithRole, error) {
	return s.teamRepo.GetTeamsForUser(ctx, userID)
}

// GetUserTeamIDs returns just the team IDs for a user (efficient for access checks)
func (s *TeamService) GetUserTeamIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	return s.teamRepo.GetUserTeamIDs(ctx, userID)
}

// IsUserInTeam checks if a user is a member of a specific team
func (s *TeamService) IsUserInTeam(ctx context.Context, userID, teamID uuid.UUID) (bool, error) {
	return s.teamRepo.IsUserInTeam(ctx, userID, teamID)
}

// IsUserTeamAdmin checks if a user is an admin (manager) of a specific team
func (s *TeamService) IsUserTeamAdmin(ctx context.Context, userID, teamID uuid.UUID) (bool, error) {
	role, err := s.teamRepo.GetTeamRole(ctx, userID, teamID)
	if err != nil {
		return false, err
	}
	return role == models.TeamRoleAdmin, nil
}

// =============================================================================
// Access Control - Primary Methods
// =============================================================================

// CanUserAccessClient checks if a user can access a client
// Returns true if:
//   - Teams are disabled (everyone can access everything)
//   - User is a system admin
//   - User is in a team that the client is assigned to
func (s *TeamService) CanUserAccessClient(ctx context.Context, userID, clientID uuid.UUID, isAdmin bool) (bool, error) {
	// If teams not enabled, all users can access all clients
	if !s.IsTeamsEnabled(ctx) {
		return true, nil
	}

	// Admins can always access everything
	if isAdmin {
		return true, nil
	}

	// Check if user is in any team that has access to this client
	return s.clientTeamRepo.IsClientAccessibleByUser(ctx, clientID, userID)
}

// CanUserAccessHashlist checks if a user can access a hashlist
// Access is determined by:
//   - Teams disabled: user must own the hashlist OR be admin
//   - Teams enabled: user must be in team that has access to hashlist's client
//   - Legacy hashlists (no client): only owner can access
func (s *TeamService) CanUserAccessHashlist(ctx context.Context, userID uuid.UUID, hashlistID int64, isAdmin bool) (bool, error) {
	// Admins can always access
	if isAdmin {
		return true, nil
	}

	// Get the hashlist
	hashlist, err := s.hashlistRepo.GetByID(ctx, hashlistID)
	if err != nil {
		return false, fmt.Errorf("failed to get hashlist: %w", err)
	}

	// If teams not enabled, use ownership check
	if !s.IsTeamsEnabled(ctx) {
		return hashlist.UserID == userID, nil
	}

	// Teams enabled - check via client
	// Legacy hashlist without client — ClientID is uuid.Nil (zero value), not nil pointer
	if hashlist.ClientID == uuid.Nil {
		// Legacy hashlist without client - only owner can access
		return hashlist.UserID == userID, nil
	}

	// Check if user can access the client
	return s.clientTeamRepo.IsClientAccessibleByUser(ctx, hashlist.ClientID, userID)
}

// CanUserAccessJob checks if a user can access a job
// Access flows through: job → hashlist → client → teams
func (s *TeamService) CanUserAccessJob(ctx context.Context, userID, jobID uuid.UUID, isAdmin bool) (bool, error) {
	// Admins can always access
	if isAdmin {
		return true, nil
	}

	// Get the job
	job, err := s.jobRepo.GetByID(ctx, jobID)
	if err != nil {
		return false, fmt.Errorf("failed to get job: %w", err)
	}

	// Check access via hashlist
	return s.CanUserAccessHashlist(ctx, userID, job.HashlistID, false)
}

// GetAccessibleClientIDs returns all client IDs the user can access
func (s *TeamService) GetAccessibleClientIDs(ctx context.Context, userID uuid.UUID, isAdmin bool) ([]uuid.UUID, error) {
	// If teams not enabled or user is admin, return nil (meaning all clients)
	if !s.IsTeamsEnabled(ctx) || isAdmin {
		return nil, nil
	}

	// Get user's team IDs
	teamIDs, err := s.GetUserTeamIDs(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user team IDs: %w", err)
	}

	// Get client IDs for those teams
	return s.clientTeamRepo.GetClientIDsForTeams(ctx, teamIDs)
}

// =============================================================================
// Team CRUD Operations
// =============================================================================

// CreateTeam creates a new team and adds the creator as admin
// Uses a database transaction to ensure atomicity
func (s *TeamService) CreateTeam(ctx context.Context, name, description string, creatorID uuid.UUID) (*models.Team, error) {
	// Validate name
	if name == "" {
		return nil, fmt.Errorf("team name is required")
	}

	// Use a transaction for atomicity: create team + add creator as admin
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Create the team
	team := &models.Team{
		Name:        name,
		Description: description,
	}

	if err := s.teamRepo.CreateTx(ctx, tx, team); err != nil {
		return nil, fmt.Errorf("failed to create team: %w", err)
	}

	// Add creator as admin
	// Note: team.ID is now uuid.UUID, no uuid.Parse() needed
	if err := s.teamRepo.AddUserTx(ctx, tx, team.ID, creatorID, models.TeamRoleAdmin); err != nil {
		return nil, fmt.Errorf("failed to add creator to team: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return team, nil
}

// UpdateTeam updates a team's details
// Only team admins or system admins can update
func (s *TeamService) UpdateTeam(ctx context.Context, teamID uuid.UUID, name, description string, userID uuid.UUID, isSystemAdmin bool) error {
	// Check if user can update this team
	if !isSystemAdmin {
		isTeamAdmin, err := s.IsUserTeamAdmin(ctx, userID, teamID)
		if err != nil {
			return fmt.Errorf("failed to check team admin status: %w", err)
		}
		if !isTeamAdmin {
			return fmt.Errorf("user is not authorized to update this team")
		}
	}

	// Validate name
	if name == "" {
		return fmt.Errorf("team name is required")
	}

	team := &models.Team{ID: teamID, Name: name, Description: description, UpdatedAt: time.Now()}
	return s.teamRepo.Update(ctx, team)
}

// DeleteTeam deletes a team
// Only system admins can delete teams
// Cannot delete the Default Team
//
// Uses a transaction to match Step 1's documented "Team Deletion Flow":
//  1. BEGIN transaction
//  2. Find all clients ONLY in this team (not in other teams)
//  3. Reassign those clients to Default Team
//  4. Remove all client_teams entries for the team being deleted
//  5. Delete the team
//  6. COMMIT transaction
func (s *TeamService) DeleteTeam(ctx context.Context, teamID uuid.UUID) error {
	// Check if this is the Default Team
	if s.teamRepo.IsDefaultTeam(teamID) {
		return models.ErrDefaultTeamProtected
	}

	// Use a transaction for atomicity (required because client_teams.team_id has ON DELETE RESTRICT)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Step 2: Find clients ONLY in this team (not in any other team)
	// These clients would become orphaned if we just deleted the team's client_teams entries,
	// so we must reassign them to Default Team first.
	clients, err := s.clientTeamRepo.GetClientsOnlyInTeamTx(ctx, tx, teamID)
	if err != nil {
		return fmt.Errorf("failed to find clients only in this team: %w", err)
	}

	// Step 3: Reassign orphaned clients to Default Team
	for _, clientID := range clients {
		if err := s.clientTeamRepo.AssignClientToTeamTx(ctx, tx, clientID, models.DefaultTeamID, nil); err != nil {
			return fmt.Errorf("failed to reassign client %s to Default Team: %w", clientID, err)
		}
	}

	// Step 4: Remove ALL client_teams entries for the team being deleted
	// This must happen before deleting the team due to ON DELETE RESTRICT.
	// NOTE: This requires a Tx-variant method in ClientTeamRepository (Step 2):
	//   func (r *ClientTeamRepository) RemoveAllForTeamTx(ctx, tx, teamID) error
	if err := s.clientTeamRepo.RemoveAllForTeamTx(ctx, tx, teamID); err != nil {
		return fmt.Errorf("failed to remove client_teams entries for team: %w", err)
	}

	// Step 5: Delete the team
	if err := s.teamRepo.DeleteTx(ctx, tx, teamID); err != nil {
		return fmt.Errorf("failed to delete team: %w", err)
	}

	// Step 6: Commit
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetTeam returns a team by ID
func (s *TeamService) GetTeam(ctx context.Context, teamID uuid.UUID) (*models.Team, error) {
	return s.teamRepo.GetByID(ctx, teamID)
}

// ListTeams returns all teams (for admins) or user's teams
func (s *TeamService) ListTeams(ctx context.Context, userID uuid.UUID, isAdmin bool) ([]repository.TeamWithRole, error) {
	if isAdmin {
		// Admin sees all teams
		teams, err := s.teamRepo.List(ctx, nil)
		if err != nil {
			return nil, err
		}

		// Convert to TeamWithRole
		result := make([]repository.TeamWithRole, len(teams))
		for i, t := range teams {
			result[i] = repository.TeamWithRole{
				Team:     t,
				UserRole: "admin", // Admin has admin access to all
			}
		}
		return result, nil
	}

	// Regular user sees their teams
	return s.teamRepo.GetTeamsForUser(ctx, userID)
}

// =============================================================================
// Member Management
// =============================================================================

// AddUserToTeam adds a user to a team
// Only team admins or system admins can add members
func (s *TeamService) AddUserToTeam(ctx context.Context, teamID, targetUserID, actingUserID uuid.UUID, role string, isSystemAdmin bool) error {
	// Validate role
	if role != models.TeamRoleMember && role != models.TeamRoleAdmin {
		return fmt.Errorf("invalid role: %s (must be 'member' or 'admin')", role)
	}

	// Check if acting user can manage this team
	if !isSystemAdmin {
		isTeamAdmin, err := s.IsUserTeamAdmin(ctx, actingUserID, teamID)
		if err != nil {
			return fmt.Errorf("failed to check team admin status: %w", err)
		}
		if !isTeamAdmin {
			return fmt.Errorf("user is not authorized to add members to this team")
		}
	}

	// Check if target user is already in the team
	inTeam, err := s.IsUserInTeam(ctx, targetUserID, teamID)
	if err != nil {
		return fmt.Errorf("failed to check if user is in team: %w", err)
	}
	if inTeam {
		return fmt.Errorf("user is already a member of this team")
	}

	return s.teamRepo.AddUser(ctx, teamID, targetUserID, role)
}

// RemoveUserFromTeam removes a user from a team
// Only team admins or system admins can remove members
// Cannot remove the last admin from a team
// Uses a transaction with SELECT ... FOR UPDATE to prevent race conditions on last-admin check
func (s *TeamService) RemoveUserFromTeam(ctx context.Context, teamID, targetUserID, actingUserID uuid.UUID, isSystemAdmin bool) error {
	// Check if acting user can manage this team
	if !isSystemAdmin {
		isTeamAdmin, err := s.IsUserTeamAdmin(ctx, actingUserID, teamID)
		if err != nil {
			return fmt.Errorf("failed to check team admin status: %w", err)
		}
		if !isTeamAdmin {
			return fmt.Errorf("user is not authorized to remove members from this team")
		}
	}

	// Check if target user is in the team
	inTeam, err := s.IsUserInTeam(ctx, targetUserID, teamID)
	if err != nil {
		return fmt.Errorf("failed to check if user is in team: %w", err)
	}
	if !inTeam {
		return models.ErrNotTeamMember
	}

	// Use a transaction with FOR UPDATE to prevent race conditions
	// on the last-admin check + removal
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Check if removing an admin — lock the admin rows to prevent concurrent removal
	targetRole, err := s.teamRepo.GetTeamRoleTx(ctx, tx, targetUserID, teamID)
	if err != nil {
		return fmt.Errorf("failed to get user's team role: %w", err)
	}

	if targetRole == models.TeamRoleAdmin {
		// Count admins with FOR UPDATE lock to prevent race condition
		// Query: SELECT COUNT(*) FROM user_teams WHERE team_id = $1 AND role = 'admin' FOR UPDATE
		adminCount, err := s.teamRepo.CountTeamAdminsTx(ctx, tx, teamID)
		if err != nil {
			return fmt.Errorf("failed to count team admins: %w", err)
		}
		if adminCount <= 1 {
			return models.ErrLastTeamAdmin
		}
	}

	// Remove user within the same transaction
	if err := s.teamRepo.RemoveUserTx(ctx, tx, teamID, targetUserID); err != nil {
		return fmt.Errorf("failed to remove user from team: %w", err)
	}

	return tx.Commit()
}

// UpdateUserTeamRole updates a user's role in a team
// Only team admins or system admins can update roles
// Cannot demote the last admin
// Uses a transaction with SELECT ... FOR UPDATE to prevent race conditions on last-admin check
func (s *TeamService) UpdateUserTeamRole(ctx context.Context, teamID, targetUserID, actingUserID uuid.UUID, newRole string, isSystemAdmin bool) error {
	// Validate role
	if newRole != models.TeamRoleMember && newRole != models.TeamRoleAdmin {
		return fmt.Errorf("invalid role: %s (must be 'member' or 'admin')", newRole)
	}

	// Check if acting user can manage this team
	if !isSystemAdmin {
		isTeamAdmin, err := s.IsUserTeamAdmin(ctx, actingUserID, teamID)
		if err != nil {
			return fmt.Errorf("failed to check team admin status: %w", err)
		}
		if !isTeamAdmin {
			return fmt.Errorf("user is not authorized to update roles in this team")
		}
	}

	// Use a transaction with FOR UPDATE to prevent race conditions
	// on the last-admin check + demotion
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get current role within transaction
	currentRole, err := s.teamRepo.GetTeamRoleTx(ctx, tx, targetUserID, teamID)
	if err != nil {
		return fmt.Errorf("failed to get user's current role: %w", err)
	}
	if currentRole == "" {
		return models.ErrNotTeamMember
	}

	// If demoting from admin, check if this is the last admin with FOR UPDATE lock
	// Query: SELECT COUNT(*) FROM user_teams WHERE team_id = $1 AND role = 'admin' FOR UPDATE
	if currentRole == models.TeamRoleAdmin && newRole == models.TeamRoleMember {
		adminCount, err := s.teamRepo.CountTeamAdminsTx(ctx, tx, teamID)
		if err != nil {
			return fmt.Errorf("failed to count team admins: %w", err)
		}
		if adminCount <= 1 {
			return models.ErrLastTeamAdmin
		}
	}

	// Update role within the same transaction
	if err := s.teamRepo.SetUserTeamRoleTx(ctx, tx, targetUserID, teamID, newRole); err != nil {
		return fmt.Errorf("failed to update team role: %w", err)
	}

	return tx.Commit()
}

// GetTeamMembers returns all members of a team
func (s *TeamService) GetTeamMembers(ctx context.Context, teamID uuid.UUID) ([]repository.TeamMember, error) {
	return s.teamRepo.GetTeamMembers(ctx, teamID)
}

// SearchUsersForTeam searches for users who can be added to a team
// Supports pagination via limit and offset parameters.
// NOTE: Step 2's SearchUsersNotInTeam repo method must also accept limit/offset int params
// and use $3/$4 placeholders instead of a hardcoded LIMIT 20 in the SQL query.
func (s *TeamService) SearchUsersForTeam(ctx context.Context, teamID uuid.UUID, query string, limit, offset int) ([]models.User, error) {
	if len(query) < 2 {
		return nil, fmt.Errorf("search query must be at least 2 characters")
	}
	return s.teamRepo.SearchUsersNotInTeam(ctx, teamID, query, limit, offset)
}

// =============================================================================
// Client-Team Assignment
// =============================================================================

// AssignClientToTeam assigns a client to a team
// Only team admins or system admins can assign clients
func (s *TeamService) AssignClientToTeam(ctx context.Context, clientID, teamID, actingUserID uuid.UUID, isSystemAdmin bool) error {
	// Check if acting user can manage this team
	if !isSystemAdmin {
		isTeamAdmin, err := s.IsUserTeamAdmin(ctx, actingUserID, teamID)
		if err != nil {
			return fmt.Errorf("failed to check team admin status: %w", err)
		}
		if !isTeamAdmin {
			return fmt.Errorf("user is not authorized to assign clients to this team")
		}
	}

	return s.clientTeamRepo.AssignClientToTeam(ctx, clientID, teamID, &actingUserID)
}

// RemoveClientFromTeam removes a client from a team
// Only team admins or system admins can remove clients
// If this is the client's only team, it will be reassigned to Default Team
func (s *TeamService) RemoveClientFromTeam(ctx context.Context, clientID, teamID, actingUserID uuid.UUID, isSystemAdmin bool) error {
	// Check if acting user can manage this team
	if !isSystemAdmin {
		isTeamAdmin, err := s.IsUserTeamAdmin(ctx, actingUserID, teamID)
		if err != nil {
			return fmt.Errorf("failed to check team admin status: %w", err)
		}
		if !isTeamAdmin {
			return fmt.Errorf("user is not authorized to remove clients from this team")
		}
	}

	// Check if client has other teams
	teamCount, err := s.clientTeamRepo.CountTeamsForClient(ctx, clientID)
	if err != nil {
		return fmt.Errorf("failed to count teams for client: %w", err)
	}

	// Remove from current team
	if err := s.clientTeamRepo.RemoveClientFromTeam(ctx, clientID, teamID); err != nil {
		return err
	}

	// If this was the only team, reassign to Default Team
	if teamCount == 1 {
		if err := s.clientTeamRepo.ReassignClientToDefaultTeam(ctx, clientID); err != nil {
			return fmt.Errorf("failed to reassign client to Default Team: %w", err)
		}
	}

	return nil
}

// GetTeamsForClient returns all teams a client is assigned to
func (s *TeamService) GetTeamsForClient(ctx context.Context, clientID uuid.UUID) ([]models.Team, error) {
	return s.clientTeamRepo.GetTeamsForClient(ctx, clientID)
}

// GetClientsForTeam returns all clients assigned to a team
func (s *TeamService) GetClientsForTeam(ctx context.Context, teamID uuid.UUID) ([]models.Client, error) {
	return s.clientTeamRepo.GetClientsForTeams(ctx, []uuid.UUID{teamID})
}

// =============================================================================
// Agent Team Resolution
// =============================================================================

// GetAgentEffectiveTeams returns the teams an agent can work with
// If admin_override_teams is true, uses explicit agent_teams entries
// Otherwise, inherits from agent owner's teams
// Note: Agent.ID is int, not uuid.UUID
func (s *TeamService) GetAgentEffectiveTeams(ctx context.Context, agentID int) ([]uuid.UUID, error) {
	// Get the agent
	agent, err := s.agentRepo.GetByID(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent: %w", err)
	}

	// Check if using explicit team assignments
	if agent.AdminOverrideTeams {
		return s.teamRepo.GetTeamIDsForAgent(ctx, agentID)
	}

	// Inherit from owner
	if agent.OwnerID != nil {
		return s.teamRepo.GetUserTeamIDs(ctx, *agent.OwnerID)
	}

	// No owner and no override - agent has no team access
	// This means it goes to Default Team
	return []uuid.UUID{models.DefaultTeamID}, nil
}

// CanAgentAccessJob checks if an agent can work on a job based on team access
// Note: Agent.ID is int, not uuid.UUID
func (s *TeamService) CanAgentAccessJob(ctx context.Context, agentID int, jobID uuid.UUID) (bool, error) {
	// If teams not enabled, any agent can work on any job
	if !s.IsTeamsEnabled(ctx) {
		return true, nil
	}

	// Get job's team IDs
	jobTeamIDs, err := s.GetJobTeamIDs(ctx, jobID)
	if err != nil {
		return false, err
	}

	// Get agent's effective teams
	agentTeamIDs, err := s.GetAgentEffectiveTeams(ctx, agentID)
	if err != nil {
		return false, err
	}

	// Check for intersection
	return hasIntersection(jobTeamIDs, agentTeamIDs), nil
}

// GetJobTeamIDs returns the team IDs associated with a job
// Derived from: job → hashlist → client → client_teams
func (s *TeamService) GetJobTeamIDs(ctx context.Context, jobID uuid.UUID) ([]uuid.UUID, error) {
	// Get the job
	job, err := s.jobRepo.GetByID(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to get job: %w", err)
	}

	// Get the hashlist
	hashlist, err := s.hashlistRepo.GetByID(ctx, job.HashlistID)
	if err != nil {
		return nil, fmt.Errorf("failed to get hashlist: %w", err)
	}

	// If no client, return Default Team (legacy hashlist)
	// ClientID is uuid.UUID (not pointer), so check against uuid.Nil (zero value)
	if hashlist.ClientID == uuid.Nil {
		return []uuid.UUID{models.DefaultTeamID}, nil
	}

	// Get client's team IDs
	return s.clientTeamRepo.GetTeamIDsForClient(ctx, hashlist.ClientID)
}

// hasIntersection checks if two slices of UUIDs have any common elements
func hasIntersection(a, b []uuid.UUID) bool {
	set := make(map[uuid.UUID]struct{}, len(a))
	for _, id := range a {
		set[id] = struct{}{}
	}
	for _, id := range b {
		if _, exists := set[id]; exists {
			return true
		}
	}
	return false
}
