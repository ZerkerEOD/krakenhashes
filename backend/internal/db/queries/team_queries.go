package queries

// Team queries - User membership
const (
	// Get all teams for a specific user with counts
	GetTeamsForUser = `
		SELECT t.id, t.name, t.description, t.created_at, t.updated_at, ut.role,
			(SELECT COUNT(*) FROM user_teams WHERE team_id = t.id) AS member_count,
			(SELECT COUNT(*) FROM client_teams WHERE team_id = t.id) AS client_count,
			(SELECT COUNT(*) FROM hashlists h INNER JOIN client_teams ct ON h.client_id = ct.client_id WHERE ct.team_id = t.id) AS hashlist_count
		FROM teams t
		INNER JOIN user_teams ut ON t.id = ut.team_id
		WHERE ut.user_id = $1
		ORDER BY t.name ASC`

	// Get all teams with counts (admin view)
	GetAllTeamsWithCounts = `
		SELECT t.id, t.name, t.description, t.created_at, t.updated_at,
			(SELECT COUNT(*) FROM user_teams WHERE team_id = t.id) AS member_count,
			(SELECT COUNT(*) FROM client_teams WHERE team_id = t.id) AS client_count,
			(SELECT COUNT(*) FROM hashlists h INNER JOIN client_teams ct ON h.client_id = ct.client_id WHERE ct.team_id = t.id) AS hashlist_count
		FROM teams t
		ORDER BY t.name ASC`

	// Get team IDs only for a user (faster for access checks)
	GetUserTeamIDs = `
		SELECT team_id
		FROM user_teams
		WHERE user_id = $1`

	// Check if user is member of a specific team
	IsUserInTeam = `
		SELECT EXISTS(
			SELECT 1 FROM user_teams
			WHERE user_id = $1 AND team_id = $2
		)`

	// Get user's role in a specific team
	GetTeamRoleForUser = `
		SELECT role
		FROM user_teams
		WHERE user_id = $1 AND team_id = $2`

	// Update user's role in a team
	SetUserTeamRole = `
		UPDATE user_teams
		SET role = $3
		WHERE user_id = $1 AND team_id = $2`

	// Search users not in a specific team (for adding members) — hardcoded limit
	SearchUsersNotInTeam = `
		SELECT u.id, u.username, u.email, u.created_at
		FROM users u
		WHERE u.id NOT IN (
			SELECT user_id FROM user_teams WHERE team_id = $1
		)
		AND u.deleted_at IS NULL
		AND (
			LOWER(u.username) LIKE LOWER($2)
			OR LOWER(u.email) LIKE LOWER($2)
		)
		ORDER BY u.username ASC
		LIMIT 20`

	// Search users not in a specific team with pagination support
	SearchUsersNotInTeamPaginated = `
		SELECT u.id, u.username, u.email, u.created_at
		FROM users u
		WHERE u.id NOT IN (
			SELECT user_id FROM user_teams WHERE team_id = $1
		)
		AND u.deleted_at IS NULL
		AND (
			LOWER(u.username) LIKE LOWER($2)
			OR LOWER(u.email) LIKE LOWER($2)
		)
		ORDER BY u.username ASC
		LIMIT $3 OFFSET $4`

	// Add a user to a team with a role
	AddUserToTeamWithRole = `
		INSERT INTO user_teams (user_id, team_id, role, joined_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (user_id, team_id) DO NOTHING`

	// Count admins in a team (for last admin protection)
	CountTeamAdmins = `
		SELECT COUNT(*)
		FROM user_teams
		WHERE team_id = $1 AND role = 'admin'`

	// Get all users in a team with their roles
	GetTeamMembers = `
		SELECT u.id, u.username, u.email, ut.role, ut.joined_at
		FROM users u
		INNER JOIN user_teams ut ON u.id = ut.user_id
		WHERE ut.team_id = $1 AND u.deleted_at IS NULL
		ORDER BY ut.role DESC, u.username ASC`

	// Get team IDs for an agent (explicit assignments)
	GetTeamIDsForAgent = `
		SELECT team_id
		FROM agent_teams
		WHERE agent_id = $1`
)

// Client-Team association queries
const (
	// Assign a client to a team
	AssignClientToTeam = `
		INSERT INTO client_teams (client_id, team_id, assigned_at, assigned_by)
		VALUES ($1, $2, NOW(), $3)
		ON CONFLICT (client_id, team_id) DO NOTHING`

	// Remove a client from a team
	RemoveClientFromTeam = `
		DELETE FROM client_teams WHERE client_id = $1 AND team_id = $2`

	// Get all teams for a client
	GetTeamsForClient = `
		SELECT t.id, t.name, t.description, t.created_at, t.updated_at
		FROM teams t
		INNER JOIN client_teams ct ON t.id = ct.team_id
		WHERE ct.client_id = $1
		ORDER BY t.name ASC`

	// Get team IDs for a client (efficient for access checks)
	GetTeamIDsForClient = `
		SELECT team_id FROM client_teams WHERE client_id = $1`

	// Get all clients accessible to given teams
	// Note: For dynamic IN clauses, build placeholders at runtime
	// This constant serves as documentation; the actual query is built dynamically
	GetClientsForTeamsBase = `
		SELECT DISTINCT c.id, c.name, c.description, c.data_retention_months,
		       c.exclude_from_potfile, c.created_at, c.updated_at
		FROM clients c
		INNER JOIN client_teams ct ON c.id = ct.client_id
		WHERE ct.team_id IN (%s)
		ORDER BY c.name ASC`

	// Get client IDs for given teams (dynamic IN clause)
	GetClientIDsForTeamsBase = `
		SELECT DISTINCT client_id
		FROM client_teams
		WHERE team_id IN (%s)`

	// Check if a client is accessible by a user via team membership
	IsClientAccessibleByUser = `
		SELECT EXISTS(
			SELECT 1
			FROM client_teams ct
			INNER JOIN user_teams ut ON ct.team_id = ut.team_id
			WHERE ct.client_id = $1 AND ut.user_id = $2
		)`

	// Check if a client is in any of the given teams (dynamic IN clause)
	IsClientInTeamsBase = `
		SELECT EXISTS(
			SELECT 1 FROM client_teams
			WHERE client_id = $1 AND team_id IN (%s)
		)`

	// Count teams for a client
	CountTeamsForClient = `
		SELECT COUNT(*) FROM client_teams WHERE client_id = $1`

	// Get detailed assignment info for a client
	GetAssignmentsForClient = `
		SELECT client_id, team_id, assigned_at, assigned_by
		FROM client_teams
		WHERE client_id = $1
		ORDER BY assigned_at DESC`

	// Get all clients not assigned to any team
	// Used during initial teams_enabled toggle to find orphaned clients
	GetClientsWithoutTeam = `
		SELECT c.id, c.name, c.description, c.data_retention_months,
		       c.exclude_from_potfile, c.created_at, c.updated_at
		FROM clients c
		LEFT JOIN client_teams ct ON c.id = ct.client_id
		WHERE ct.client_id IS NULL
		ORDER BY c.name ASC`
)
