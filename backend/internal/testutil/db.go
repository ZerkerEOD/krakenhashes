// Package testutil provides shared helpers for database-backed tests.
//
// CONCURRENCY: SetupTestDB truncates every table in the public schema, so all
// tests sharing the database must run serially. Never call t.Parallel() in a
// test that calls SetupTestDB, and run DB-backed packages with `go test -p 1`.
//
// Per-test transactions were considered instead and rejected: cloud budget
// reservation opens its own transaction (CloudBudgetRepository.Reserve), and
// the SELECT ... FOR UPDATE concurrency test needs two genuinely concurrent
// connections, neither of which works inside a shared outer transaction.
package testutil

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/database"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

// defaultTestDSN is used when TEST_DATABASE_URL is unset.
const defaultTestDSN = "postgres://krakenhashes:krakenhashes@localhost:5432/krakenhashes_test?sslmode=disable"

// migrateOnce guards the migration run. Migrations are process-global (they
// chdir and read DB_* from the environment) and there are 300+ of them, so
// running them per-test was both slow and a data race on the working directory.
var migrateOnce sync.Once

// migrateErr carries the outcome of migrateOnce to every later caller.
var migrateErr error

// TestDSN returns the DSN under test.
func TestDSN() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return defaultTestDSN
}

/*
 * applyDSNToEnv derives the DB_* variables that database.RunMigrations reads
 * from the DSN.
 *
 * This used to hardcode localhost/5432/krakenhashes, which silently ignored
 * TEST_DATABASE_URL: a CI run pointed at a different host or port would
 * connect its queries to one database and migrate a different one.
 */
func applyDSNToEnv(dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("invalid TEST_DATABASE_URL: %w", err)
	}

	host := u.Hostname()
	if host == "" {
		host = "localhost"
	}
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	password, _ := u.User.Password()

	os.Setenv("DB_HOST", host)
	os.Setenv("DB_PORT", port)
	os.Setenv("DB_USER", u.User.Username())
	os.Setenv("DB_PASSWORD", password)
	os.Setenv("DB_NAME", strings.TrimPrefix(u.Path, "/"))
	if q := u.RawQuery; q != "" {
		os.Setenv("DB_ARGUMENTS", q)
	}
	return nil
}

/*
 * ensureTestDatabase creates the test database if it does not exist.
 *
 * Nothing in the repository created krakenhashes_test, so DB-backed tests
 * failed on a fresh checkout with a bare "database does not exist". CI does not
 * need this (the service container's POSTGRES_DB covers it) but every developer
 * does.
 */
func ensureTestDatabase(dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("invalid TEST_DATABASE_URL: %w", err)
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		return fmt.Errorf("TEST_DATABASE_URL has no database name")
	}

	// Connect to the maintenance database to issue CREATE DATABASE.
	admin := *u
	admin.Path = "/postgres"
	conn, err := sql.Open("postgres", admin.String())
	if err != nil {
		return fmt.Errorf("connect to maintenance database: %w", err)
	}
	defer conn.Close()

	if err := conn.Ping(); err != nil {
		return fmt.Errorf("ping maintenance database: %w", err)
	}

	// CREATE DATABASE cannot be parameterized and has no IF NOT EXISTS, so the
	// identifier is quoted and the already-exists error is tolerated.
	_, err = conn.Exec(fmt.Sprintf(`CREATE DATABASE %q`, dbName))
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return fmt.Errorf("create database %s: %w", dbName, err)
	}
	return nil
}

// runMigrationsOnce locates db/migrations and applies them, at most once per
// process.
func runMigrationsOnce() error {
	migrateOnce.Do(func() {
		originalDir, err := os.Getwd()
		if err != nil {
			migrateErr = err
			return
		}

		// Walk up until db/migrations is found.
		testDir := originalDir
		for {
			if _, statErr := os.Stat(filepath.Join(testDir, "db", "migrations")); statErr == nil {
				break
			}
			parent := filepath.Dir(testDir)
			if parent == testDir {
				migrateErr = fmt.Errorf("could not find db/migrations from %s", originalDir)
				return
			}
			testDir = parent
		}

		if err := os.Chdir(testDir); err != nil {
			migrateErr = fmt.Errorf("chdir to %s: %w", testDir, err)
			return
		}
		defer func() {
			if err := os.Chdir(originalDir); err != nil && migrateErr == nil {
				migrateErr = fmt.Errorf("restore working directory: %w", err)
			}
		}()

		if err := database.RunMigrations(); err != nil {
			migrateErr = fmt.Errorf("run migrations: %w", err)
		}
	})
	return migrateErr
}

// SetupTestDB creates a test database connection, runs migrations once per
// process, and truncates every table before returning.
func SetupTestDB(t *testing.T) *db.DB {
	t.Helper()

	dsn := TestDSN()
	if err := applyDSNToEnv(dsn); err != nil {
		t.Fatalf("Failed to parse test database URL: %v", err)
	}
	if err := ensureTestDatabase(dsn); err != nil {
		t.Fatalf("Failed to ensure test database exists: %v", err)
	}

	rawDB, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	testDB := &db.DB{DB: rawDB}

	if err := runMigrationsOnce(); err != nil {
		testDB.Close()
		t.Fatalf("Failed to run migrations: %v", err)
	}

	// Start from a clean slate rather than trusting the previous test's
	// cleanup, so a panicking test cannot poison the rest of the run.
	TruncateAll(t, testDB)
	SeedDefaults(t, testDB)

	t.Cleanup(func() {
		TruncateAll(t, testDB)
		// Re-seed so the next SetupTestDB — and any code reading defaults in
		// between — still finds the rows the migrations planted.
		SeedDefaults(t, testDB)
		testDB.Close()
	})

	return testDB
}

/*
 * assertTestDatabase refuses to touch anything that is not obviously a test
 * database.
 *
 * TruncateAll destroys every row in the public schema. The default DSN differs
 * from the development database by five characters ("krakenhashes" vs
 * "krakenhashes_test"), and TEST_DATABASE_URL is an environment variable that
 * a stray export could point anywhere. The cost of this check is one query per
 * setup; the cost of not having it is someone's development data, or worse.
 */
func assertTestDatabase(t *testing.T, database *db.DB) {
	t.Helper()

	var name string
	if err := database.QueryRow("SELECT current_database()").Scan(&name); err != nil {
		t.Fatalf("Failed to identify current database: %v", err)
	}
	if !strings.Contains(name, "test") {
		t.Fatalf("refusing to truncate database %q: the name does not contain \"test\". "+
			"TruncateAll destroys every row in the public schema; point TEST_DATABASE_URL "+
			"at a dedicated test database.", name)
	}
}

/*
 * preservedTables are populated by migrations and must survive truncation.
 *
 * hash_types holds 589 rows of hashcat mode definitions and is an FK target
 * for hashlists — truncating it makes every hashlist fixture fail on
 * hashlists_hash_type_id_fkey. It is immutable lookup data that no test should
 * be mutating, so preserving is correct and far cheaper than re-inserting it
 * per test.
 *
 * schema_migrations obviously must survive or the once-per-process migration
 * would re-run against a schema that already exists.
 */
var preservedTables = map[string]bool{
	"schema_migrations": true,
	"hash_types":        true,
}

/*
 * TruncateAll empties every table in the public schema in one statement,
 * except those in preservedTables.
 *
 * A single TRUNCATE ... CASCADE is required rather than a per-table loop:
 * agents.cloud_instance_id and cloud_instances.agent_id reference each other,
 * so no ordering of individual truncates satisfies both constraints.
 *
 * Discovering tables from pg_tables rather than maintaining a list is
 * deliberate — the previous hardcoded list of 11 tables silently missed every
 * table added since it was written, including all five cloud tables, leaving
 * rows behind that broke later tests in the same package.
 */
func TruncateAll(t *testing.T, database *db.DB) {
	t.Helper()
	assertTestDatabase(t, database)

	rows, err := database.Query(`
		SELECT tablename FROM pg_tables WHERE schemaname = 'public'`)
	if err != nil {
		t.Fatalf("Failed to list tables for truncation: %v", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("Failed to scan table name: %v", err)
		}
		if preservedTables[name] {
			continue
		}
		tables = append(tables, fmt.Sprintf("%q", name))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("Failed to iterate table names: %v", err)
	}
	if len(tables) == 0 {
		return
	}

	stmt := "TRUNCATE TABLE " + strings.Join(tables, ", ") + " RESTART IDENTITY CASCADE"
	if _, err := database.Exec(stmt); err != nil {
		t.Fatalf("Failed to truncate tables: %v", err)
	}
}

/*
 * SeedDefaults restores rows that migrations insert and TruncateAll removes.
 *
 * This is not optional. Truncation deletes the system-default row in
 * cloud_budget_policies seeded by 20260813120200_add_cloud_provisioning.up.sql;
 * without it CloudBudgetRepository.GetPolicy has no fallback and fails with
 * "no cloud budget policy found", so the first budget test in a package would
 * pass and every one after it would fail.
 */
func SeedDefaults(t *testing.T, database *db.DB) {
	t.Helper()

	// auth_settings must have exactly one row.
	_, err := database.Exec(`
		INSERT INTO auth_settings (
			min_password_length,
			require_uppercase,
			require_lowercase,
			require_numbers,
			require_special_chars,
			max_failed_attempts,
			lockout_duration_minutes,
			require_mfa,
			jwt_expiry_minutes,
			display_timezone,
			notification_aggregation_minutes
		)
		VALUES (15, true, true, true, true, 5, 60, false, 60, 'UTC', 60)
	`)
	if err != nil && !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("Failed to seed auth_settings: %v", err)
	}

	// The system-default cloud budget policy (client_id IS NULL). Values match
	// the migration so tests see production defaults.
	_, err = database.Exec(`
		INSERT INTO cloud_budget_policies (
			client_id, notify_pct, stop_provision_pct, drain_pct,
			hard_stop_pct, allow_overage, drain_timeout_seconds
		)
		VALUES (NULL, 80, 95, 99, 100, false, 300)
		ON CONFLICT DO NOTHING
	`)
	if err != nil && !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("Failed to seed cloud_budget_policies default: %v", err)
	}

	/*
	 * The system-default provisioning rules (client_id IS NULL), values matching
	 * 20260821090000_add_cloud_provisioning_rules.up.sql.
	 *
	 * Required for the same reason as the budget policy above, and the failure
	 * is louder: GetRules fails closed when this row is missing, so every
	 * provisioning path refuses rather than degrading. Without this seed the
	 * first test in a package passes and every later one fails with "the system
	 * default row is missing", which reads like a rules bug rather than a
	 * truncation artefact.
	 */
	_, err = database.Exec(`
		INSERT INTO cloud_provisioning_rules (
			client_id, min_job_priority, min_starvation_seconds,
			skip_if_finishing_within_seconds, max_spend_per_job_cents,
			provisioning_window_start, provisioning_window_end, provisioning_window_tz
		)
		VALUES (NULL, 0, 180, 900, 0, NULL, NULL, 'UTC')
		ON CONFLICT DO NOTHING
	`)
	if err != nil && !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("Failed to seed cloud_provisioning_rules default: %v", err)
	}

	// The system user (000001_initial_schema). Truncation removes it along with
	// every other user, but code references its fixed UUID directly — cloud
	// vouchers are minted under it, and Agent.OwnerID == SystemUserID is how a
	// universal agent is recognised. Without this row those inserts fail on the
	// created_by_id foreign key.
	_, err = database.Exec(`
		INSERT INTO users (id, username, email, password_hash, role)
		VALUES ('00000000-0000-0000-0000-000000000000', 'system',
		        'system@krakenhashes.local', 'SYSTEM_USER_NO_LOGIN', 'system')
		ON CONFLICT (id) DO NOTHING
	`)
	if err != nil && !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("Failed to seed system user: %v", err)
	}

	// The default team (000125_create_default_team). It is an FK target for
	// team membership, and its fixed UUID is referenced directly by code.
	_, err = database.Exec(`
		INSERT INTO teams (id, name, description)
		VALUES ('00000000-0000-0000-0000-000000000001', 'Default Team',
		        'Default team for all users')
		ON CONFLICT (id) DO NOTHING
	`)
	if err != nil && !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("Failed to seed default team: %v", err)
	}
}

// CreateTestUser creates a test user with the given attributes
func CreateTestUser(t *testing.T, database *db.DB, username, email, pass string, role string) *models.User {
	t.Helper()

	// Hash the password using bcrypt (for user authentication, not hashcat)
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	// Create user directly with SQL - include ALL fields that GetUserByID expects
	// Note: mfa_type must include 'email' per database constraint
	query := `
		INSERT INTO users (
			username, email, password_hash, role,
			account_enabled, account_locked, failed_login_attempts,
			mfa_enabled, mfa_type, preferred_mfa_method,
			last_password_change, notify_on_job_completion
		)
		VALUES ($1, $2, $3, $4, true, false, 0, false, ARRAY['email']::text[], NULL, NOW(), false)
		RETURNING id, username, email, role, created_at, updated_at,
		          account_enabled, account_locked, failed_login_attempts,
		          mfa_enabled, last_password_change, notify_on_job_completion
	`

	user := &models.User{}
	var notifyOnJobCompletion bool
	err = database.QueryRow(query, username, email, hashedPassword, role).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.Role,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.AccountEnabled,
		&user.AccountLocked,
		&user.FailedLoginAttempts,
		&user.MFAEnabled,
		&user.LastPasswordChange,
		&notifyOnJobCompletion,
	)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	return user
}
