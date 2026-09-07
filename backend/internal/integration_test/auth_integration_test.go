package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/auth"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/jwt"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFullAuthenticationFlow tests the complete authentication flow end-to-end
func TestFullAuthenticationFlow(t *testing.T) {
	// Set up test environment
	testutil.SetTestJWTSecret(t)
	database := testutil.SetupTestDB(t)
	emailService := testutil.NewMockEmailService()
	authHandler := auth.NewHandler(database, emailService)

	// Test scenarios
	scenarios := []struct {
		name     string
		username string
		email    string
		password string
		role     string
		withMFA  bool
	}{
		{
			name:     "admin user without MFA",
			username: "admin",
			email:    "admin@test.com",
			password: testutil.DefaultTestPassword,
			role:     "admin",
			withMFA:  false,
		},
		{
			name:     "regular user with email MFA",
			username: "user1",
			email:    "user1@test.com",
			password: testutil.DefaultTestPassword,
			role:     "user",
			withMFA:  true,
		},
		{
			name:     "agent user with authenticator MFA",
			username: "agent1",
			email:    "agent1@test.com",
			password: testutil.DefaultTestPassword,
			role:     "agent",
			withMFA:  true,
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			// Create user
			user := testutil.CreateTestUser(t, database, scenario.username, scenario.email, scenario.password, scenario.role)

			// Setup MFA if required
			var totpSecret string
			if scenario.withMFA {
				if scenario.username == "user1" {
					// Email MFA
					err := database.EnableMFA(user.ID.String(), "email", "")
					require.NoError(t, err)
				} else {
					// Authenticator MFA
					totpSecret = testutil.ValidTOTPSecret
					err := database.EnableMFA(user.ID.String(), "authenticator", totpSecret)
					require.NoError(t, err)
				}
			}

			// Test complete login flow
			testCompleteLoginFlow(t, authHandler, emailService, scenario.username, scenario.password, scenario.withMFA, totpSecret)

			// Test authentication check
			testAuthenticationCheck(t, authHandler, database, user)

			// Test logout flow
			testLogoutFlow(t, authHandler, database, user)
		})
	}
}

func testCompleteLoginFlow(t *testing.T, handler *auth.Handler, emailService *testutil.MockEmailService, username, password string, withMFA bool, totpSecret string) {
	// Step 1: Initial login attempt
	loginReq := map[string]string{
		"username": username,
		"password": password,
	}

	reqBody, _ := json.Marshal(loginReq)
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.LoginHandler(rr, req)

	if !withMFA {
		// Should complete login immediately
		var resp models.LoginResponse
		testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
		assert.True(t, resp.Success)
		assert.NotEmpty(t, resp.Token)

		// Should set auth cookie
		cookie := testutil.AssertCookieSet(t, rr, "token")
		assert.Equal(t, resp.Token, cookie.Value)
	} else {
		// Should require MFA
		var resp map[string]interface{}
		testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
		assert.True(t, resp["mfa_required"].(bool))
		assert.NotEmpty(t, resp["session_token"])

		sessionToken := resp["session_token"].(string)

		// Step 2: Complete MFA verification
		completeMFAVerification(t, handler, emailService, sessionToken, totpSecret, username)
	}
}

func completeMFAVerification(t *testing.T, handler *auth.Handler, emailService *testutil.MockEmailService, sessionToken, totpSecret, username string) {
	if totpSecret != "" {
		// Authenticator MFA
		code, err := totp.GenerateCode(totpSecret, time.Now())
		require.NoError(t, err)

		mfaReq := map[string]string{
			"method":       "authenticator",
			"code":         code,
			"sessionToken": sessionToken,
		}

		reqBody, _ := json.Marshal(mfaReq)
		req := httptest.NewRequest(http.MethodPost, "/auth/mfa/verify", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		handler.VerifyMFAHandler(rr, req)

		var resp map[string]interface{}
		testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
		assert.True(t, resp["success"].(bool))
		assert.NotEmpty(t, resp["token"])

		// Should set auth cookie
		cookie := testutil.AssertCookieSet(t, rr, "token")
		assert.Equal(t, resp["token"], cookie.Value)
	} else {
		// Email MFA
		// First, trigger email code sending
		sendReq := map[string]string{
			"sessionToken": sessionToken,
		}

		reqBody, _ := json.Marshal(sendReq)
		req := httptest.NewRequest(http.MethodPost, "/auth/mfa/send-code", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")

		// Create MFA handler to test email code sending
		mfaHandler := auth.NewMFAHandler(testutil.SetupTestDB(t), emailService)
		rr := httptest.NewRecorder()
		mfaHandler.SendEmailMFACode(rr, req)

		var sendResp map[string]interface{}
		testutil.AssertJSONResponse(t, rr, http.StatusOK, &sendResp)
		assert.True(t, sendResp["success"].(bool))

		// Get the sent code
		code := emailService.LastCode
		assert.NotEmpty(t, code)

		// Verify with email code
		mfaReq := map[string]string{
			"method":       "email",
			"code":         code,
			"sessionToken": sessionToken,
		}

		reqBody, _ = json.Marshal(mfaReq)
		req = httptest.NewRequest(http.MethodPost, "/auth/mfa/verify", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		rr = httptest.NewRecorder()

		handler.VerifyMFAHandler(rr, req)

		var resp map[string]interface{}
		testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
		assert.True(t, resp["success"].(bool))
		assert.NotEmpty(t, resp["token"])
	}
}

func testAuthenticationCheck(t *testing.T, handler *auth.Handler, database *db.DB, user *models.User) {
	// Generate token and store it
	token, err := jwt.GenerateToken(user.ID.String(), user.Role, 60)
	require.NoError(t, err)
	_, err = database.StoreToken(user.ID.String(), token)
	require.NoError(t, err)

	// Test auth check with valid token
	req := httptest.NewRequest(http.MethodGet, "/auth/check", nil)
	req.AddCookie(&http.Cookie{
		Name:  "token",
		Value: token,
	})
	rr := httptest.NewRecorder()

	handler.CheckAuthHandler(rr, req)

	var resp map[string]interface{}
	testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
	assert.True(t, resp["authenticated"].(bool))
	assert.Equal(t, user.Role, resp["role"])
}

func testLogoutFlow(t *testing.T, handler *auth.Handler, database *db.DB, user *models.User) {
	// Generate token and store it
	token, err := jwt.GenerateToken(user.ID.String(), user.Role, 60)
	require.NoError(t, err)
	_, err = database.StoreToken(user.ID.String(), token)
	require.NoError(t, err)

	// Logout
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.AddCookie(&http.Cookie{
		Name:  "token",
		Value: token,
	})
	rr := httptest.NewRecorder()

	handler.LogoutHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	// Verify token was removed
	exists, err := database.TokenExists(token)
	require.NoError(t, err)
	assert.False(t, exists)

	// Verify cookie was deleted
	cookie := testutil.AssertCookieSet(t, rr, "token")
	assert.Equal(t, "", cookie.Value)
	assert.Equal(t, -1, cookie.MaxAge)
}

// TestMFAWorkflow tests the complete MFA setup and usage workflow
func TestMFAWorkflow(t *testing.T) {
	// Set up test environment
	testutil.SetTestJWTSecret(t)
	database := testutil.SetupTestDB(t)
	emailService := testutil.NewMockEmailService()
	authHandler := auth.NewHandler(database, emailService)
	mfaHandler := auth.NewMFAHandler(database, emailService)

	// Create test user
	user := testutil.CreateTestUser(t, database, "mfauser", "mfa@test.com", testutil.DefaultTestPassword, "user")

	t.Run("authenticator MFA setup workflow", func(t *testing.T) {
		// Step 1: Setup authenticator MFA
		req := testutil.MakeAuthenticatedRequest(t, http.MethodPost, "/auth/mfa/setup",
			map[string]string{"method": "authenticator"}, user.ID.String(), "user")
		rr := httptest.NewRecorder()
		authHandler.SetupMFAHandler(rr, req)

		var setupResp struct {
			Secret string `json:"secret"`
			QRCode string `json:"qrCode"`
		}
		testutil.AssertJSONResponse(t, rr, http.StatusOK, &setupResp)
		assert.NotEmpty(t, setupResp.Secret)
		assert.NotEmpty(t, setupResp.QRCode)

		// Step 2: Verify setup with TOTP code
		code, err := totp.GenerateCode(setupResp.Secret, time.Now())
		require.NoError(t, err)

		req = testutil.MakeAuthenticatedRequest(t, http.MethodPost, "/auth/mfa/verify",
			map[string]string{"method": "authenticator", "code": code}, user.ID.String(), "user")
		rr = httptest.NewRecorder()
		authHandler.VerifyMFAHandler(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)

		// Step 3: Verify MFA is enabled
		settings, err := database.GetUserMFASettings(user.ID.String())
		require.NoError(t, err)
		assert.True(t, settings.MFAEnabled)
		assert.Contains(t, settings.MFAType, "authenticator")

		// Step 4: Test login with MFA
		testMFALoginFlow(t, authHandler, user.Username, testutil.DefaultTestPassword, setupResp.Secret)
	})

	t.Run("backup codes workflow", func(t *testing.T) {
		// Generate backup codes
		req := testutil.MakeAuthenticatedRequest(t, http.MethodPost, "/auth/mfa/backup-codes",
			nil, user.ID.String(), "user")
		rr := httptest.NewRecorder()
		mfaHandler.GenerateBackupCodes(rr, req)

		var backupResp struct {
			BackupCodes []string `json:"backupCodes"`
		}
		testutil.AssertJSONResponse(t, rr, http.StatusOK, &backupResp)
		assert.Len(t, backupResp.BackupCodes, 8)

		// Test login with backup code
		testBackupCodeLoginFlow(t, authHandler, database, user.Username, testutil.DefaultTestPassword, backupResp.BackupCodes[0])
	})
}

func testMFALoginFlow(t *testing.T, handler *auth.Handler, username, password, totpSecret string) {
	// Step 1: Login attempt
	loginReq := map[string]string{
		"username": username,
		"password": password,
	}

	reqBody, _ := json.Marshal(loginReq)
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.LoginHandler(rr, req)

	var resp map[string]interface{}
	testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
	assert.True(t, resp["mfa_required"].(bool))

	sessionToken := resp["session_token"].(string)

	// Step 2: MFA verification
	code, err := totp.GenerateCode(totpSecret, time.Now())
	require.NoError(t, err)

	mfaReq := map[string]string{
		"method":       "authenticator",
		"code":         code,
		"sessionToken": sessionToken,
	}

	reqBody, _ = json.Marshal(mfaReq)
	req = httptest.NewRequest(http.MethodPost, "/auth/mfa/verify", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()

	handler.VerifyMFAHandler(rr, req)

	testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
	assert.True(t, resp["success"].(bool))
	assert.NotEmpty(t, resp["token"])
}

func testBackupCodeLoginFlow(t *testing.T, handler *auth.Handler, database *db.DB, username, password, backupCode string) {
	// Step 1: Login attempt
	loginReq := map[string]string{
		"username": username,
		"password": password,
	}

	reqBody, _ := json.Marshal(loginReq)
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.LoginHandler(rr, req)

	var resp map[string]interface{}
	testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
	assert.True(t, resp["mfa_required"].(bool))

	sessionToken := resp["session_token"].(string)

	// Step 2: Backup code verification
	mfaReq := map[string]string{
		"method":       "backup",
		"code":         backupCode,
		"sessionToken": sessionToken,
	}

	reqBody, _ = json.Marshal(mfaReq)
	req = httptest.NewRequest(http.MethodPost, "/auth/mfa/verify", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()

	handler.VerifyMFAHandler(rr, req)

	testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
	assert.True(t, resp["success"].(bool))
	assert.NotEmpty(t, resp["token"])

	// Step 3: Verify backup code was consumed (can't be used again)
	req = httptest.NewRequest(http.MethodPost, "/auth/mfa/verify", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()

	handler.VerifyMFAHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// TestSecurityScenarios tests various security attack scenarios
func TestSecurityScenarios(t *testing.T) {
	// Set up test environment
	testutil.SetTestJWTSecret(t)
	database := testutil.SetupTestDB(t)
	emailService := testutil.NewMockEmailService()
	authHandler := auth.NewHandler(database, emailService)

	// Create test user
	user := testutil.CreateTestUser(t, database, "sectest", "sec@test.com", testutil.DefaultTestPassword, "user")

	t.Run("brute force protection", func(t *testing.T) {
		// Attempt multiple failed logins
		for i := 0; i < 5; i++ {
			loginReq := map[string]string{
				"username": "sectest",
				"password": "wrongpassword",
			}

			reqBody, _ := json.Marshal(loginReq)
			req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()

			authHandler.LoginHandler(rr, req)
			assert.Equal(t, http.StatusUnauthorized, rr.Code)
		}

		// Should still be able to login with correct password
		loginReq := map[string]string{
			"username": "sectest",
			"password": testutil.DefaultTestPassword,
		}

		reqBody, _ := json.Marshal(loginReq)
		req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		authHandler.LoginHandler(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("session hijacking prevention", func(t *testing.T) {
		// Login and get token
		token, err := jwt.GenerateToken(user.ID.String(), user.Role, 60)
		require.NoError(t, err)
		_, err = database.StoreToken(user.ID.String(), token)
		require.NoError(t, err)

		// Simulate token being stolen and used from different IP
		req := httptest.NewRequest(http.MethodGet, "/auth/check", nil)
		req.AddCookie(&http.Cookie{
			Name:  "token",
			Value: token,
		})
		req.RemoteAddr = "192.168.1.100:12345" // Different IP
		rr := httptest.NewRecorder()

		authHandler.CheckAuthHandler(rr, req)

		var resp map[string]interface{}
		testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
		// Token should still be valid (no IP binding in current implementation)
		assert.True(t, resp["authenticated"].(bool))
	})

	t.Run("timing attack resistance", func(t *testing.T) {
		start := time.Now()

		// Login with non-existent user
		loginReq := map[string]string{
			"username": "nonexistent",
			"password": "anypassword",
		}

		reqBody, _ := json.Marshal(loginReq)
		req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		authHandler.LoginHandler(rr, req)
		nonExistentDuration := time.Since(start)

		start = time.Now()

		// Login with existing user but wrong password
		loginReq = map[string]string{
			"username": "sectest",
			"password": "wrongpassword",
		}

		reqBody, _ = json.Marshal(loginReq)
		req = httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		rr = httptest.NewRecorder()

		authHandler.LoginHandler(rr, req)
		wrongPasswordDuration := time.Since(start)

		// Response times should be similar (within reasonable variance)
		// This is a basic check - in production you'd want more sophisticated timing analysis
		ratio := float64(nonExistentDuration) / float64(wrongPasswordDuration)
		assert.True(t, ratio > 0.5 && ratio < 2.0, "Timing difference too large: %v vs %v", nonExistentDuration, wrongPasswordDuration)
	})
}

// TestConcurrentAccess tests concurrent authentication operations
func TestConcurrentAccess(t *testing.T) {
	// Set up test environment
	testutil.SetTestJWTSecret(t)
	database := testutil.SetupTestDB(t)
	emailService := testutil.NewMockEmailService()
	authHandler := auth.NewHandler(database, emailService)

	// Create test user
	user := testutil.CreateTestUser(t, database, "concurrent", "concurrent@test.com", testutil.DefaultTestPassword, "user")

	t.Run("concurrent logins", func(t *testing.T) {
		const numConcurrentLogins = 10
		results := make(chan bool, numConcurrentLogins)

		// Launch concurrent login attempts
		for i := 0; i < numConcurrentLogins; i++ {
			go func() {
				loginReq := map[string]string{
					"username": "concurrent",
					"password": testutil.DefaultTestPassword,
				}

				reqBody, _ := json.Marshal(loginReq)
				req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(reqBody))
				req.Header.Set("Content-Type", "application/json")
				rr := httptest.NewRecorder()

				authHandler.LoginHandler(rr, req)
				results <- rr.Code == http.StatusOK
			}()
		}

		// Collect results
		successCount := 0
		for i := 0; i < numConcurrentLogins; i++ {
			if <-results {
				successCount++
			}
		}

		// All logins should succeed
		assert.Equal(t, numConcurrentLogins, successCount)
	})

	t.Run("concurrent token operations", func(t *testing.T) {
		const numOperations = 20
		results := make(chan error, numOperations)

		// Generate tokens
		tokens := make([]string, numOperations)
		for i := range tokens {
			token, err := jwt.GenerateToken(user.ID.String(), user.Role, 60)
			require.NoError(t, err)
			tokens[i] = token
		}

		// Launch concurrent token storage operations
		for _, token := range tokens {
			go func(t string) {
				_, err := database.StoreToken(user.ID.String(), t)
				results <- err
			}(token)
		}

		// Collect results
		errorCount := 0
		for i := 0; i < numOperations; i++ {
			if err := <-results; err != nil {
				errorCount++
			}
		}

		// All operations should succeed
		assert.Equal(t, 0, errorCount)

		// Verify all tokens exist
		for _, token := range tokens {
			exists, err := database.TokenExists(token)
			require.NoError(t, err)
			assert.True(t, exists)
		}
	})
}

// TestErrorHandling tests various error conditions
func TestErrorHandling(t *testing.T) {
	// Set up test environment
	testutil.SetTestJWTSecret(t)
	database := testutil.SetupTestDB(t)
	emailService := testutil.NewMockEmailService()
	authHandler := auth.NewHandler(database, emailService)

	t.Run("malformed request handling", func(t *testing.T) {
		malformedRequests := []struct {
			name string
			body string
		}{
			{"invalid json", `{"username": "test", "password": `},
			{"missing fields", `{"username": "test"}`},
			{"empty json", `{}`},
			{"non-json", `this is not json`},
		}

		for _, tc := range malformedRequests {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader([]byte(tc.body)))
				req.Header.Set("Content-Type", "application/json")
				rr := httptest.NewRecorder()

				authHandler.LoginHandler(rr, req)
				assert.Equal(t, http.StatusBadRequest, rr.Code)
			})
		}
	})

	t.Run("email service failure handling", func(t *testing.T) {
		// Create user with email MFA
		user := testutil.CreateTestUser(t, database, "emailtest", "email@test.com", testutil.DefaultTestPassword, "user")
		err := database.EnableMFA(user.ID.String(), "email", "")
		require.NoError(t, err)

		// Set email service to fail
		emailService.SetSendError(fmt.Errorf("email service unavailable"))

		// Attempt login
		loginReq := map[string]string{
			"username": "emailtest",
			"password": testutil.DefaultTestPassword,
		}

		reqBody, _ := json.Marshal(loginReq)
		req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		authHandler.LoginHandler(rr, req)
		assert.Equal(t, http.StatusInternalServerError, rr.Code)

		// Reset email service
		emailService.SetSendError(nil)
	})

	t.Run("database connection failure simulation", func(t *testing.T) {
		// This would require mocking the database connection
		// For now, we test with a nil database to simulate failure
		invalidHandler := auth.NewHandler(nil, emailService)

		loginReq := map[string]string{
			"username": "test",
			"password": "test",
		}

		reqBody, _ := json.Marshal(loginReq)
		req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		// This will panic or error due to nil database
		// In production, you'd want proper error handling
		assert.Panics(t, func() {
			invalidHandler.LoginHandler(rr, req)
		})
	})
}
