package auth

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
)

/*
 * The auth/MFA settings endpoints accept partial bodies: only the fields sent
 * are written. Two regressions these guard against:
 *   - a single-field save must not reset display_timezone (the UI never showed
 *     it and used to send "UTC" on every blur);
 *   - editing an MFA field while require_mfa is already on must not re-run the
 *     bulk "enable MFA for everyone" side effect.
 *
 * Shares the test database: run under `go test -p 1`, no t.Parallel().
 */
func TestAuthSettings_PartialUpdatePreservesUnsentFields(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database test in short mode")
	}
	database := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, database)
	testutil.SeedDefaults(t, database)
	if _, err := database.Exec(`UPDATE auth_settings SET display_timezone = 'Europe/Berlin', max_failed_attempts = 7`); err != nil {
		t.Fatalf("seed timezone: %v", err)
	}

	h := NewAuthSettingsHandler(database)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/auth/settings", bytes.NewBufferString(`{"min_password_length": 12}`))
	rec := httptest.NewRecorder()
	h.UpdateSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("partial update: want 200, got %d %s", rec.Code, rec.Body.String())
	}

	s, err := database.GetAuthSettings()
	if err != nil {
		t.Fatalf("get auth settings: %v", err)
	}
	if s.MinPasswordLength != 12 {
		t.Fatalf("min_password_length: want 12, got %d", s.MinPasswordLength)
	}
	if s.DisplayTimezone != "Europe/Berlin" {
		t.Fatalf("display_timezone was clobbered: got %q", s.DisplayTimezone)
	}
	if s.MaxFailedAttempts != 7 {
		t.Fatalf("max_failed_attempts was clobbered: got %d", s.MaxFailedAttempts)
	}

	// Out-of-range values are rejected and nothing is written.
	req = httptest.NewRequest(http.MethodPut, "/api/admin/auth/settings", bytes.NewBufferString(`{"jwt_expiry_minutes": 0}`))
	rec = httptest.NewRecorder()
	h.UpdateSettings(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid jwt expiry: want 400, got %d", rec.Code)
	}
}

func TestMFASettings_PartialUpdateDoesNotRerunBulkEnable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database test in short mode")
	}
	database := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, database)
	testutil.SeedDefaults(t, database)

	// MFA already required globally; a user who still has it off (e.g. created
	// later, or exempted) must stay off when an unrelated MFA field changes.
	if _, err := database.Exec(`UPDATE auth_settings SET require_mfa = true, allowed_mfa_methods = '["email"]'::jsonb, backup_codes_count = 8`); err != nil {
		t.Fatalf("seed mfa: %v", err)
	}
	user := testutil.CreateTestUser(t, database, "mfa-user", "mfa-user@test.local", testutil.DefaultTestPassword, "user")
	if _, err := database.Exec(`UPDATE users SET mfa_enabled = false WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("reset user mfa: %v", err)
	}

	h := NewAuthSettingsHandler(database)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/auth/settings/mfa", bytes.NewBufferString(`{"backupCodesCount": 10}`))
	rec := httptest.NewRecorder()
	h.UpdateMFASettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("partial MFA update: want 200, got %d %s", rec.Code, rec.Body.String())
	}

	mfa, err := database.GetMFASettings()
	if err != nil {
		t.Fatalf("get mfa settings: %v", err)
	}
	if mfa.BackupCodesCount != 10 || !mfa.RequireMFA || len(mfa.AllowedMFAMethods) != 1 || mfa.AllowedMFAMethods[0] != "email" {
		t.Fatalf("partial update changed more than it should: %+v", mfa)
	}
	var enabled bool
	if err := database.QueryRow(`SELECT mfa_enabled FROM users WHERE id = $1`, user.ID).Scan(&enabled); err != nil {
		t.Fatalf("read user: %v", err)
	}
	if enabled {
		t.Fatalf("bulk MFA enable re-ran on an unrelated field save")
	}
}
