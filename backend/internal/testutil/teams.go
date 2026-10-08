package testutil

import (
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/google/uuid"
)

/*
 * Multi-Team fixtures shared by the team-isolation suites (GH #100).
 *
 * These use raw SQL rather than the repository layer so that testutil stays
 * import-free of internal/repository (whose own tests import testutil).
 */

// SetTeamsEnabled upserts the teams_enabled system setting.
//
// TruncateAll removes the row that migration 000123 inserts and SeedDefaults
// does not restore it, so this is an INSERT ... ON CONFLICT rather than an
// UPDATE. Callers that hold a *services.TeamService must call its
// InvalidateTeamsEnabledCache() afterwards: the service caches the flag for
// several seconds and would otherwise keep serving the previous value.
func SetTeamsEnabled(t *testing.T, database *db.DB, enabled bool) {
	t.Helper()
	value := "false"
	if enabled {
		value = "true"
	}
	_, err := database.Exec(`
		INSERT INTO system_settings (key, value, description, data_type, updated_at)
		VALUES ('teams_enabled', $1, 'test fixture', 'boolean', NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`, value)
	if err != nil {
		t.Fatalf("Failed to set teams_enabled=%s: %v", value, err)
	}
}

// CreateTestTeam inserts a team and returns its ID.
func CreateTestTeam(t *testing.T, database *db.DB, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now()
	_, err := database.Exec(`
		INSERT INTO teams (id, name, description, created_at, updated_at)
		VALUES ($1, $2, '', $3, $3)`, id, name, now)
	if err != nil {
		t.Fatalf("Failed to create test team %q: %v", name, err)
	}
	return id
}

// AddUserToTeam makes userID a regular member of teamID.
func AddUserToTeam(t *testing.T, database *db.DB, teamID, userID uuid.UUID) {
	t.Helper()
	_, err := database.Exec(`
		INSERT INTO user_teams (user_id, team_id, role, joined_at)
		VALUES ($1, $2, 'member', NOW())
		ON CONFLICT (user_id, team_id) DO NOTHING`, userID, teamID)
	if err != nil {
		t.Fatalf("Failed to add user %s to team %s: %v", userID, teamID, err)
	}
}

// AssignClientToTeam links clientID to teamID (the Team -> Client access edge).
func AssignClientToTeam(t *testing.T, database *db.DB, clientID, teamID uuid.UUID) {
	t.Helper()
	_, err := database.Exec(`
		INSERT INTO client_teams (client_id, team_id, assigned_at, assigned_by)
		VALUES ($1, $2, NOW(), NULL)
		ON CONFLICT (client_id, team_id) DO NOTHING`, clientID, teamID)
	if err != nil {
		t.Fatalf("Failed to assign client %s to team %s: %v", clientID, teamID, err)
	}
}

// CreateTestHashlist inserts a ready, empty hashlist owned by userID under clientID
// and returns its ID. Hash type 0 (MD5) is preserved across truncation.
func CreateTestHashlist(t *testing.T, database *db.DB, userID, clientID uuid.UUID, name string) int64 {
	t.Helper()
	var id int64
	err := database.QueryRow(`
		INSERT INTO hashlists (name, user_id, client_id, hash_type_id,
		                       total_hashes, cracked_hashes, status)
		VALUES ($1, $2, $3, 0, 0, 0, 'ready')
		RETURNING id`, name, userID, clientID).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to create test hashlist %q: %v", name, err)
	}
	return id
}

// ArchiveTestHashlist marks a hashlist archived so /unarchive has something to act on.
func ArchiveTestHashlist(t *testing.T, database *db.DB, hashlistID int64) {
	t.Helper()
	if _, err := database.Exec(`UPDATE hashlists SET archived_at = NOW() WHERE id = $1`, hashlistID); err != nil {
		t.Fatalf("Failed to archive test hashlist %d: %v", hashlistID, err)
	}
}

// CreateTestJobExecution inserts a minimal job execution for hashlistID in the
// given status and returns its ID. The column set mirrors CreateCloudJob plus the
// non-nullable string columns JobExecutionRepository.GetByID scans (mask, binary_version).
func CreateTestJobExecution(t *testing.T, database *db.DB, hashlistID int64, createdBy uuid.UUID, status string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := database.QueryRow(`
		INSERT INTO job_executions (
			hashlist_id, status, priority, max_agents, attack_mode, created_by,
			name, wordlist_ids, rule_ids, hash_type, chunk_size_seconds,
			status_updates_enabled, allow_high_priority_override,
			increment_mode, multiplication_factor, is_accurate_keyspace,
			cloud_burst_enabled, mask, binary_version
		) VALUES ($1,$2,5,1,0,$3,$4,'[]'::jsonb,'[]'::jsonb,0,1200,
		          true,false,'off',1,false,false,'','default')
		RETURNING id`,
		hashlistID, status, createdBy, "iso-job-"+uuid.New().String()[:8],
	).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to create test job execution: %v", err)
	}
	return id
}

// CountJobExecutions returns the number of job_executions rows for hashlistID.
func CountJobExecutions(t *testing.T, database *db.DB, hashlistID int64) int {
	t.Helper()
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM job_executions WHERE hashlist_id = $1`, hashlistID).Scan(&n); err != nil {
		t.Fatalf("Failed to count job executions for hashlist %d: %v", hashlistID, err)
	}
	return n
}

// CreateBlocklistEntry inserts an active agent_benchmark_blocklist row. A nil
// jobExecutionID creates a global entry (job_execution_id IS NULL).
func CreateBlocklistEntry(t *testing.T, database *db.DB, agentID int, jobExecutionID *uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := database.QueryRow(`
		INSERT INTO agent_benchmark_blocklist
			(agent_id, job_execution_id, attack_mode, hash_type, reason, expires_at)
		VALUES ($1, $2, 0, 0, 'test fixture', NOW() + INTERVAL '1 day')
		RETURNING id`, agentID, jobExecutionID).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to create blocklist entry: %v", err)
	}
	return id
}
