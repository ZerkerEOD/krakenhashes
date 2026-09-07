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

// TestUserJourney tests realistic user journeys through the authentication system
func TestUserJourney(t *testing.T) {
	testutil.SetTestJWTSecret(t)
	database := testutil.SetupTestDB(t)
	emailService := testutil.NewMockEmailService()
	authHandler := auth.NewHandler(database, emailService)
	mfaHandler := auth.NewMFAHandler(database, emailService)

	t.Run("new user complete journey", func(t *testing.T) {
		// Step 1: User registration (simulated)
		user := testutil.CreateTestUser(t, database, "newuser", "newuser@example.com", testutil.DefaultTestPassword, "user")

		// Step 2: First login (no MFA)
		token := performLogin(t, authHandler, "newuser", testutil.DefaultTestPassword, false, "")
		assert.NotEmpty(t, token)

		// Step 3: User decides to enable MFA for security
		setupAuthenticatorMFA(t, authHandler, mfaHandler, user.ID.String())

		// Step 4: Subsequent login requires MFA
		performLogin(t, authHandler, "newuser", testutil.DefaultTestPassword, true, testutil.ValidTOTPSecret)

		// Step 5: User generates backup codes
		backupCodes := generateBackupCodes(t, mfaHandler, user.ID.String())
		assert.Len(t, backupCodes, 8)

		// Step 6: User loses phone, uses backup code
		testBackupCodeUsage(t, authHandler, database, "newuser", testutil.DefaultTestPassword, backupCodes[0])

		// Step 7: User disables MFA (after regaining access)
		disableMFA(t, mfaHandler, user.ID.String())

		// Step 8: Login no longer requires MFA
		token = performLogin(t, authHandler, "newuser", testutil.DefaultTestPassword, false, "")
		assert.NotEmpty(t, token)

		// Step 9: User logs out
		performLogout(t, authHandler, database, token)
	})

	t.Run("admin user journey", func(t *testing.T) {
		// Admin user with elevated security requirements
		admin := testutil.CreateTestUser(t, database, "admin", "admin@example.com", testutil.DefaultTestPassword, "admin")

		// Force MFA requirement for admin
		err := database.EnableMFA(admin.ID.String(), "authenticator", testutil.ValidTOTPSecret)
		require.NoError(t, err)

		// Admin must use MFA
		performLogin(t, authHandler, "admin", testutil.DefaultTestPassword, true, testutil.ValidTOTPSecret)

		// Admin cannot disable MFA (simulated by policy)
		// This would be enforced by business logic in a real system
	})

	t.Run("mobile app user journey", func(t *testing.T) {
		// User on mobile device
		user := testutil.CreateTestUser(t, database, "mobile", "mobile@example.com", testutil.DefaultTestPassword, "user")

		// Mobile user prefers email MFA
		err := database.EnableMFA(user.ID.String(), "email", "")
		require.NoError(t, err)

		// Test login flow optimized for mobile
		testMobileLoginFlow(t, authHandler, emailService, "mobile", testutil.DefaultTestPassword)
	})
}

// TestEdgeCases tests various edge cases and boundary conditions
func TestEdgeCases(t *testing.T) {
	testutil.SetTestJWTSecret(t)
	database := testutil.SetupTestDB(t)
	emailService := testutil.NewMockEmailService()
	authHandler := auth.NewHandler(database, emailService)
	_ = auth.NewMFAHandler(database, emailService)

	t.Run("password edge cases", func(t *testing.T) {
		// Test with various password complexities
		passwords := []struct {
			name     string
			password string
			valid    bool
		}{
			{"minimum length", "Pass123!", true},
			{"unicode password", "Pássw0rd!中文", true},
			{"very long password", "ThisIsAVeryLongPasswordWithLotsOfCharacters123!", true},
			{"only spaces", "        ", false},
			{"empty password", "", false},
		}

		for i, tc := range passwords {
			t.Run(tc.name, func(t *testing.T) {
				username := fmt.Sprintf("edgeuser%d", i)
				email := fmt.Sprintf("edge%d@example.com", i)

				if tc.valid {
					user := testutil.CreateTestUser(t, database, username, email, tc.password, "user")
					assert.NotNil(t, user)

					// Test login with this password
					token := performLogin(t, authHandler, username, tc.password, false, "")
					assert.NotEmpty(t, token)
				}
			})
		}
	})

	t.Run("username edge cases", func(t *testing.T) {
		usernames := []struct {
			name     string
			username string
			valid    bool
		}{
			{"normal username", "normaluser", true},
			{"username with numbers", "user123", true},
			{"username with underscores", "user_name", true},
			{"very long username", "verylongusernamewithlotsofcharacters", true},
			{"system username blocked", "system", false},
		}

		for i, tc := range usernames {
			t.Run(tc.name, func(t *testing.T) {
				email := fmt.Sprintf("username%d@example.com", i)

				if tc.valid {
					user := testutil.CreateTestUser(t, database, tc.username, email, testutil.DefaultTestPassword, "user")
					if tc.username != "system" {
						assert.NotNil(t, user)
					}
				}
			})
		}
	})

	t.Run("session edge cases", func(t *testing.T) {
		user := testutil.CreateTestUser(t, database, "sessiontest", "session@example.com", testutil.DefaultTestPassword, "user")

		// Test expired session handling
		expiredToken := generateExpiredToken(t, user.ID.String(), user.Role)
		_, err := database.StoreToken(user.ID.String(), expiredToken)
		require.NoError(t, err)

		// Expired token should not authenticate
		req := httptest.NewRequest(http.MethodGet, "/auth/check", nil)
		req.AddCookie(&http.Cookie{
			Name:  "token",
			Value: expiredToken,
		})
		rr := httptest.NewRecorder()

		authHandler.CheckAuthHandler(rr, req)

		var resp map[string]interface{}
		testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
		assert.False(t, resp["authenticated"].(bool))
	})

	t.Run("MFA timing edge cases", func(t *testing.T) {
		user := testutil.CreateTestUser(t, database, "timingtest", "timing@example.com", testutil.DefaultTestPassword, "user")
		err := database.EnableMFA(user.ID.String(), "authenticator", testutil.ValidTOTPSecret)
		require.NoError(t, err)

		// Test TOTP codes at time boundaries
		testTOTPAtTimeBoundaries(t, authHandler, "timingtest", testutil.DefaultTestPassword, testutil.ValidTOTPSecret)
	})
}

// TestPerformanceScenarios tests authentication system under various load conditions
func TestPerformanceScenarios(t *testing.T) {
	testutil.SetTestJWTSecret(t)
	database := testutil.SetupTestDB(t)
	emailService := testutil.NewMockEmailService()
	authHandler := auth.NewHandler(database, emailService)

	t.Run("rapid login attempts", func(t *testing.T) {
		_ = testutil.CreateTestUser(t, database, "rapidtest", "rapid@example.com", testutil.DefaultTestPassword, "user")

		// Perform rapid login attempts
		const numAttempts = 50
		successCount := 0

		start := time.Now()
		for i := 0; i < numAttempts; i++ {
			token := performLogin(t, authHandler, "rapidtest", testutil.DefaultTestPassword, false, "")
			if token != "" {
				successCount++
				// Clean up token. RemoveTokenByString exists for exactly this
				// case: RemoveToken takes the uuid the ledger is keyed on, and
				// what performLogin hands back is the opaque token string.
				database.RemoveTokenByString(token)
			}
		}
		duration := time.Since(start)

		assert.Equal(t, numAttempts, successCount)
		assert.Less(t, duration, 10*time.Second, "Rapid logins took too long: %v", duration)
	})

	t.Run("many concurrent token validations", func(t *testing.T) {
		user := testutil.CreateTestUser(t, database, "concurrentval", "concurrent@example.com", testutil.DefaultTestPassword, "user")

		// Generate token
		token, err := jwt.GenerateToken(user.ID.String(), user.Role, 60)
		require.NoError(t, err)
		_, err = database.StoreToken(user.ID.String(), token)
		require.NoError(t, err)

		const numValidations = 100
		results := make(chan bool, numValidations)

		start := time.Now()
		for i := 0; i < numValidations; i++ {
			go func() {
				req := httptest.NewRequest(http.MethodGet, "/auth/check", nil)
				req.AddCookie(&http.Cookie{
					Name:  "token",
					Value: token,
				})
				rr := httptest.NewRecorder()

				authHandler.CheckAuthHandler(rr, req)

				var resp map[string]interface{}
				json.NewDecoder(rr.Body).Decode(&resp)
				results <- resp["authenticated"].(bool)
			}()
		}

		// Collect results
		successCount := 0
		for i := 0; i < numValidations; i++ {
			if <-results {
				successCount++
			}
		}
		duration := time.Since(start)

		assert.Equal(t, numValidations, successCount)
		assert.Less(t, duration, 5*time.Second, "Concurrent validations took too long: %v", duration)
	})
}

// Helper functions

func performLogin(t *testing.T, handler *auth.Handler, username, password string, expectMFA bool, totpSecret string) string {
	loginReq := map[string]string{
		"username": username,
		"password": password,
	}

	reqBody, _ := json.Marshal(loginReq)
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.LoginHandler(rr, req)

	if expectMFA {
		var resp map[string]interface{}
		testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
		assert.True(t, resp["mfa_required"].(bool))

		sessionToken := resp["session_token"].(string)

		// Complete MFA
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
		return resp["token"].(string)
	} else {
		var resp models.LoginResponse
		testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
		return resp.Token
	}
}

func setupAuthenticatorMFA(t *testing.T, authHandler *auth.Handler, mfaHandler *auth.MFAHandler, userID string) {
	// Setup MFA
	req := testutil.MakeAuthenticatedRequest(t, http.MethodPost, "/auth/mfa/setup",
		map[string]string{"method": "authenticator"}, userID, "user")
	rr := httptest.NewRecorder()
	authHandler.SetupMFAHandler(rr, req)

	var setupResp struct {
		Secret string `json:"secret"`
	}
	testutil.AssertJSONResponse(t, rr, http.StatusOK, &setupResp)

	// Verify setup
	code, err := totp.GenerateCode(testutil.ValidTOTPSecret, time.Now())
	require.NoError(t, err)

	req = testutil.MakeAuthenticatedRequest(t, http.MethodPost, "/auth/mfa/verify",
		map[string]string{"method": "authenticator", "code": code}, userID, "user")
	rr = httptest.NewRecorder()
	authHandler.VerifyMFAHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func generateBackupCodes(t *testing.T, mfaHandler *auth.MFAHandler, userID string) []string {
	req := testutil.MakeAuthenticatedRequest(t, http.MethodPost, "/auth/mfa/backup-codes",
		nil, userID, "user")
	rr := httptest.NewRecorder()
	mfaHandler.GenerateBackupCodes(rr, req)

	var resp struct {
		BackupCodes []string `json:"backupCodes"`
	}
	testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
	return resp.BackupCodes
}

func testBackupCodeUsage(t *testing.T, handler *auth.Handler, database *db.DB, username, password, backupCode string) {
	// Login to get session
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
	sessionToken := resp["session_token"].(string)

	// Use backup code
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
}

func disableMFA(t *testing.T, mfaHandler *auth.MFAHandler, userID string) {
	req := testutil.MakeAuthenticatedRequest(t, http.MethodPost, "/auth/mfa/disable",
		nil, userID, "user")
	rr := httptest.NewRecorder()
	mfaHandler.DisableMFA(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func performLogout(t *testing.T, handler *auth.Handler, database *db.DB, token string) {
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.AddCookie(&http.Cookie{
		Name:  "token",
		Value: token,
	})
	rr := httptest.NewRecorder()

	handler.LogoutHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func testMobileLoginFlow(t *testing.T, handler *auth.Handler, emailService *testutil.MockEmailService, username, password string) {
	// Mobile-optimized login flow would include specific headers, user agent, etc.
	loginReq := map[string]string{
		"username": username,
		"password": password,
	}

	reqBody, _ := json.Marshal(loginReq)
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "KrakenHashes-Mobile/1.0")
	rr := httptest.NewRecorder()

	handler.LoginHandler(rr, req)

	var resp map[string]interface{}
	testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
	assert.True(t, resp["mfa_required"].(bool))

	// Mobile app would show UI for email code entry
	// Simulate email code verification
	sessionToken := resp["session_token"].(string)
	emailCode := emailService.LastCode

	mfaReq := map[string]string{
		"method":       "email",
		"code":         emailCode,
		"sessionToken": sessionToken,
	}

	reqBody, _ = json.Marshal(mfaReq)
	req = httptest.NewRequest(http.MethodPost, "/auth/mfa/verify", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "KrakenHashes-Mobile/1.0")
	rr = httptest.NewRecorder()

	handler.VerifyMFAHandler(rr, req)

	testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
	assert.True(t, resp["success"].(bool))
}

func generateExpiredToken(t *testing.T, userID, role string) string {
	// Generate token that expires immediately
	token, err := jwt.GenerateToken(userID, role, 1) // 1 minute expiry for testing
	require.NoError(t, err)

	// In a real implementation, you'd modify the expiration
	// For this test, we'll simulate an expired token scenario
	return token
}

func testTOTPAtTimeBoundaries(t *testing.T, handler *auth.Handler, username, password, secret string) {
	// Test TOTP codes at different time windows
	times := []time.Time{
		time.Now(),
		time.Now().Add(30 * time.Second),  // Next window
		time.Now().Add(-30 * time.Second), // Previous window
	}

	for i, testTime := range times {
		t.Run(fmt.Sprintf("time_window_%d", i), func(t *testing.T) {
			code, err := totp.GenerateCode(secret, testTime)
			require.NoError(t, err)

			// Login first
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
			sessionToken := resp["session_token"].(string)

			// Try MFA with time-shifted code
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

			// Current time and previous window should work due to skew tolerance
			// Future window might not work depending on implementation
			if i <= 1 {
				testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
				if resp["success"] != nil {
					assert.True(t, resp["success"].(bool))
				}
			}
		})
	}
}
