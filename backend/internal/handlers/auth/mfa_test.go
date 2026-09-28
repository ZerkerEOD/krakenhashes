package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupMFAHandler(t *testing.T) {
	// Set up test environment
	testutil.SetTestJWTSecret(t)
	db := testutil.SetupTestDB(t)
	emailService := testutil.NewMockEmailService()
	handler := NewHandler(db, emailService)

	// Create test user
	testUser := testutil.CreateTestUser(t, db, "testuser", "test@example.com", testutil.DefaultTestPassword, "user")

	t.Run("setup authenticator MFA", func(t *testing.T) {
		req := testutil.MakeAuthenticatedRequest(t, http.MethodPost, "/auth/mfa/setup",
			map[string]string{"method": "authenticator"}, testUser.ID.String(), "user")

		rr := httptest.NewRecorder()
		handler.SetupMFAHandler(rr, req)

		var resp struct {
			Secret string `json:"secret"`
			QRCode string `json:"qrCode"`
		}
		testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)

		assert.NotEmpty(t, resp.Secret)
		assert.NotEmpty(t, resp.QRCode)

		// Verify pending setup was stored
		secret, err := db.GetPendingMFASetup(testUser.ID.String())
		assert.NoError(t, err)
		assert.Equal(t, resp.Secret, secret)
	})

	t.Run("setup email MFA", func(t *testing.T) {
		emailService.Reset()

		req := testutil.MakeAuthenticatedRequest(t, http.MethodPost, "/auth/mfa/setup",
			map[string]string{"method": "email"}, testUser.ID.String(), "user")

		rr := httptest.NewRecorder()
		handler.SetupMFAHandler(rr, req)

		var resp MFASetupResponse
		testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)

		assert.True(t, resp.CodeSent)
		assert.NotEmpty(t, resp.ExpiresAt)

		// Verify email was sent
		assert.Equal(t, 1, emailService.CallCount)
		assert.Equal(t, "test@example.com", emailService.LastRecipient)
		assert.NotEmpty(t, emailService.LastCode)
	})

	t.Run("invalid MFA method", func(t *testing.T) {
		req := testutil.MakeAuthenticatedRequest(t, http.MethodPost, "/auth/mfa/setup",
			map[string]string{"method": "invalid"}, testUser.ID.String(), "user")

		rr := httptest.NewRecorder()
		handler.SetupMFAHandler(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code)
		assert.Contains(t, rr.Body.String(), "Invalid MFA method")
	})
}

func TestVerifyMFAHandler(t *testing.T) {
	// Set up test environment
	testutil.SetTestJWTSecret(t)
	db := testutil.SetupTestDB(t)
	emailService := testutil.NewMockEmailService()
	handler := NewHandler(db, emailService)

	// Create test user
	testUser := testutil.CreateTestUser(t, db, "testuser", "test@example.com", testutil.DefaultTestPassword, "user")

	t.Run("verify authenticator setup", func(t *testing.T) {
		// Store pending MFA setup
		secret := testutil.ValidTOTPSecret
		err := db.StorePendingMFASetup(testUser.ID.String(), "authenticator", secret)
		require.NoError(t, err)

		// Generate valid TOTP code
		code, err := testutil.GenerateTOTPCode(secret)
		require.NoError(t, err)

		req := testutil.MakeAuthenticatedRequest(t, http.MethodPost, "/auth/mfa/verify",
			testutil.ValidMFAVerifyRequest("authenticator", code, ""),
			testUser.ID.String(), "user")

		rr := httptest.NewRecorder()
		handler.VerifyMFAHandler(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		// Verify MFA was enabled
		settings, err := db.GetUserMFASettings(testUser.ID.String())
		require.NoError(t, err)
		assert.True(t, settings.MFAEnabled)
		assert.Contains(t, settings.MFAType, "authenticator")
	})

	t.Run("verify email MFA code", func(t *testing.T) {
		// Store email MFA code
		emailCode := "123456"
		err := db.StoreEmailMFACode(testUser.ID.String(), emailCode)
		require.NoError(t, err)

		req := testutil.MakeAuthenticatedRequest(t, http.MethodPost, "/auth/mfa/verify",
			testutil.ValidMFAVerifyRequest("email", emailCode, ""),
			testUser.ID.String(), "user")

		rr := httptest.NewRecorder()
		handler.VerifyMFAHandler(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("verify during login flow", func(t *testing.T) {
		// Create MFA session
		sessionToken := uuid.New().String()
		_, err := db.CreateMFASession(testUser.ID.String(), sessionToken)
		require.NoError(t, err)

		// Enable authenticator MFA for user
		err = db.EnableMFA(testUser.ID.String(), "authenticator", testutil.ValidTOTPSecret)
		require.NoError(t, err)

		// Generate valid TOTP code
		code, err := testutil.GenerateTOTPCode(testutil.ValidTOTPSecret)
		require.NoError(t, err)

		req := testutil.MakeRequest(t, http.MethodPost, "/auth/mfa/verify",
			testutil.ValidMFAVerifyRequest("authenticator", code, sessionToken))

		rr := httptest.NewRecorder()
		handler.VerifyMFAHandler(rr, req)

		var resp map[string]interface{}
		testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)

		assert.True(t, testutil.GetRespBool(t, resp, "success"))
		assert.NotEmpty(t, resp["token"])

		// Check that auth cookie was set
		cookie := testutil.AssertCookieSet(t, rr, "token")
		assert.Equal(t, resp["token"], cookie.Value)
	})

	t.Run("invalid verification code", func(t *testing.T) {
		sessionToken := uuid.New().String()
		_, err := db.CreateMFASession(testUser.ID.String(), sessionToken)
		require.NoError(t, err)

		req := testutil.MakeRequest(t, http.MethodPost, "/auth/mfa/verify",
			testutil.ValidMFAVerifyRequest("authenticator", "000000", sessionToken))

		rr := httptest.NewRecorder()
		handler.VerifyMFAHandler(rr, req)

		var resp map[string]interface{}
		testutil.AssertJSONResponse(t, rr, http.StatusBadRequest, &resp)

		assert.False(t, testutil.GetRespBool(t, resp, "success"))
		assert.Contains(t, resp["message"], "Invalid verification code")
		assert.Greater(t, testutil.GetRespFloat64(t, resp, "remainingAttempts"), 0.0)
	})

	t.Run("max attempts exceeded", func(t *testing.T) {
		sessionToken := uuid.New().String()
		_, err := db.CreateMFASession(testUser.ID.String(), sessionToken)
		require.NoError(t, err)

		// Enable MFA for user
		err = db.EnableMFA(testUser.ID.String(), "authenticator", testutil.ValidTOTPSecret)
		require.NoError(t, err)

		// Exhaust attempts
		for i := 0; i < 3; i++ {
			req := testutil.MakeRequest(t, http.MethodPost, "/auth/mfa/verify",
				testutil.ValidMFAVerifyRequest("authenticator", "000000", sessionToken))

			rr := httptest.NewRecorder()
			handler.VerifyMFAHandler(rr, req)
		}

		// Next attempt should fail with too many attempts
		req := testutil.MakeRequest(t, http.MethodPost, "/auth/mfa/verify",
			testutil.ValidMFAVerifyRequest("authenticator", "000000", sessionToken))

		rr := httptest.NewRecorder()
		handler.VerifyMFAHandler(rr, req)

		var resp map[string]interface{}
		testutil.AssertJSONResponse(t, rr, http.StatusTooManyRequests, &resp)

		assert.False(t, testutil.GetRespBool(t, resp, "success"))
		assert.Contains(t, resp["message"], "Too many verification attempts")
		assert.Equal(t, 0.0, testutil.GetRespFloat64(t, resp, "remainingAttempts"))
	})
}

func TestBackupCodes(t *testing.T) {
	// Set up test environment
	testutil.SetTestJWTSecret(t)
	db := testutil.SetupTestDB(t)
	emailService := testutil.NewMockEmailService()
	handler := NewHandler(db, emailService)
	mfaHandler := NewMFAHandler(db, emailService)

	// Create test user
	testUser := testutil.CreateTestUser(t, db, "testuser", "test@example.com", testutil.DefaultTestPassword, "user")

	t.Run("generate backup codes", func(t *testing.T) {
		req := testutil.MakeAuthenticatedRequest(t, http.MethodPost, "/auth/mfa/backup-codes",
			nil, testUser.ID.String(), "user")

		rr := httptest.NewRecorder()
		mfaHandler.GenerateBackupCodes(rr, req)

		var resp struct {
			BackupCodes []string `json:"backupCodes"`
		}
		testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)

		assert.Len(t, resp.BackupCodes, 8) // Default number of backup codes

		// Check all codes are unique
		codeMap := make(map[string]bool)
		for _, code := range resp.BackupCodes {
			assert.Len(t, code, 8) // Each code should be 8 characters
			assert.False(t, codeMap[code], "Backup codes should be unique")
			codeMap[code] = true
		}
	})

	t.Run("verify backup code during login", func(t *testing.T) {
		// Generate and store backup codes
		req := testutil.MakeAuthenticatedRequest(t, http.MethodPost, "/auth/mfa/backup-codes",
			nil, testUser.ID.String(), "user")

		rr := httptest.NewRecorder()
		mfaHandler.GenerateBackupCodes(rr, req)

		var backupResp struct {
			BackupCodes []string `json:"backupCodes"`
		}
		json.NewDecoder(rr.Body).Decode(&backupResp)

		// Create MFA session
		sessionToken := uuid.New().String()
		_, err := db.CreateMFASession(testUser.ID.String(), sessionToken)
		require.NoError(t, err)

		// Use one of the backup codes
		req = testutil.MakeRequest(t, http.MethodPost, "/auth/mfa/verify",
			testutil.ValidMFAVerifyRequest("backup", backupResp.BackupCodes[0], sessionToken))

		rr = httptest.NewRecorder()
		handler.VerifyMFAHandler(rr, req)

		var resp map[string]interface{}
		testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)

		assert.True(t, testutil.GetRespBool(t, resp, "success"))
		assert.NotEmpty(t, resp["token"])

		// Try to use the same backup code again - should fail
		_, err = db.CreateMFASession(testUser.ID.String(), sessionToken)
		require.NoError(t, err)

		req = testutil.MakeRequest(t, http.MethodPost, "/auth/mfa/verify",
			testutil.ValidMFAVerifyRequest("backup", backupResp.BackupCodes[0], sessionToken))

		rr = httptest.NewRecorder()
		handler.VerifyMFAHandler(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})
}

func TestMFASettings(t *testing.T) {
	// Set up test environment
	testutil.SetTestJWTSecret(t)
	db := testutil.SetupTestDB(t)
	emailService := testutil.NewMockEmailService()
	mfaHandler := NewMFAHandler(db, emailService)

	// Create test user
	testUser := testutil.CreateTestUser(t, db, "testuser", "test@example.com", testutil.DefaultTestPassword, "user")

	t.Run("get user MFA settings", func(t *testing.T) {
		// Enable MFA for user
		err := db.EnableMFA(testUser.ID.String(), "email", "")
		require.NoError(t, err)

		req := testutil.MakeAuthenticatedRequest(t, http.MethodGet, "/auth/mfa/settings",
			nil, testUser.ID.String(), "user")

		rr := httptest.NewRecorder()
		mfaHandler.GetUserMFASettings(rr, req)

		var resp struct {
			RequireMFA           bool     `json:"requireMfa"`
			AllowedMFAMethods    []string `json:"allowedMfaMethods"`
			MfaEnabled           bool     `json:"mfaEnabled"`
			MfaType              []string `json:"mfaType"`
			PreferredMethod      string   `json:"preferredMethod"`
			RemainingBackupCodes int      `json:"remainingBackupCodes"`
		}
		testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)

		assert.True(t, resp.MfaEnabled)
		assert.Contains(t, resp.MfaType, "email")
	})

	t.Run("update preferred MFA method", func(t *testing.T) {
		// Enable both email and authenticator
		err := db.EnableMFA(testUser.ID.String(), "email", "")
		require.NoError(t, err)
		err = db.EnableMFA(testUser.ID.String(), "authenticator", testutil.ValidTOTPSecret)
		require.NoError(t, err)

		req := testutil.MakeAuthenticatedRequest(t, http.MethodPost, "/auth/mfa/preferred",
			map[string]string{"method": "authenticator"}, testUser.ID.String(), "user")

		rr := httptest.NewRecorder()
		mfaHandler.UpdatePreferredMFAMethod(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		// Verify preferred method was updated
		settings, err := db.GetUserMFASettings(testUser.ID.String())
		require.NoError(t, err)
		assert.Equal(t, "authenticator", settings.PreferredMFAMethod)
	})

	t.Run("disable MFA when not required", func(t *testing.T) {
		req := testutil.MakeAuthenticatedRequest(t, http.MethodPost, "/auth/mfa/disable",
			nil, testUser.ID.String(), "user")

		rr := httptest.NewRecorder()
		mfaHandler.DisableMFA(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		// Verify MFA was disabled
		settings, err := db.GetUserMFASettings(testUser.ID.String())
		require.NoError(t, err)
		assert.False(t, settings.MFAEnabled)
	})

	t.Run("cannot disable MFA when required", func(t *testing.T) {
		// Enable MFA requirement. require_mfa lives on auth_settings (there is no
		// mfa_settings table); the seeded singleton row is what IsMFARequired reads.
		_, err := db.Exec("UPDATE auth_settings SET require_mfa = true")
		require.NoError(t, err)

		req := testutil.MakeAuthenticatedRequest(t, http.MethodPost, "/auth/mfa/disable",
			nil, testUser.ID.String(), "user")

		rr := httptest.NewRecorder()
		mfaHandler.DisableMFA(rr, req)

		assert.Equal(t, http.StatusForbidden, rr.Code)
		assert.Contains(t, rr.Body.String(), "MFA is required by organization policy")
	})
}

func TestEmailMFAFlow(t *testing.T) {
	// Set up test environment
	testutil.SetTestJWTSecret(t)
	db := testutil.SetupTestDB(t)
	emailService := testutil.NewMockEmailService()
	mfaHandler := NewMFAHandler(db, emailService)

	// Create test user
	testUser := testutil.CreateTestUser(t, db, "testuser", "test@example.com", testutil.DefaultTestPassword, "user")

	t.Run("send email MFA code", func(t *testing.T) {
		// Create MFA session
		sessionToken := uuid.New().String()
		_, err := db.CreateMFASession(testUser.ID.String(), sessionToken)
		require.NoError(t, err)

		// Enable email MFA
		err = db.EnableMFA(testUser.ID.String(), "email", "")
		require.NoError(t, err)

		emailService.Reset()

		req := testutil.MakeRequest(t, http.MethodPost, "/auth/mfa/send-code",
			map[string]string{"sessionToken": sessionToken})

		rr := httptest.NewRecorder()
		mfaHandler.SendEmailMFACode(rr, req)

		var resp map[string]interface{}
		testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)

		assert.True(t, testutil.GetRespBool(t, resp, "success"))
		assert.Greater(t, testutil.GetRespFloat64(t, resp, "remainingAttempts"), 0.0)

		// Verify email was sent
		assert.Equal(t, 1, emailService.CallCount)
		assert.Equal(t, "test@example.com", emailService.LastRecipient)
		assert.NotEmpty(t, emailService.LastCode)
	})

	t.Run("email code cooldown", func(t *testing.T) {
		// Use a dedicated user: the resend cooldown is per user, and the previous
		// subtest already sent testuser a code, so reusing testUser would put even
		// this subtest's first send on cooldown.
		cooldownUser := testutil.CreateTestUser(t, db, "cooldownuser", "cooldown@example.com", testutil.DefaultTestPassword, "user")

		// Create MFA session
		sessionToken := uuid.New().String()
		_, err := db.CreateMFASession(cooldownUser.ID.String(), sessionToken)
		require.NoError(t, err)

		// Send first code
		req := testutil.MakeRequest(t, http.MethodPost, "/auth/mfa/send-code",
			map[string]string{"sessionToken": sessionToken})

		rr := httptest.NewRecorder()
		mfaHandler.SendEmailMFACode(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)

		// Try to send another code immediately - should fail due to cooldown
		req = testutil.MakeRequest(t, http.MethodPost, "/auth/mfa/send-code",
			map[string]string{"sessionToken": sessionToken})

		rr = httptest.NewRecorder()
		mfaHandler.SendEmailMFACode(rr, req)

		assert.Equal(t, http.StatusTooManyRequests, rr.Code)
		assert.Contains(t, rr.Body.String(), "Please wait before requesting a new code")
	})
}
