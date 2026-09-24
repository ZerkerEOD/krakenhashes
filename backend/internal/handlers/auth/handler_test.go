package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	khauth "github.com/ZerkerEOD/krakenhashes/backend/internal/auth"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoginHandler(t *testing.T) {
	// Set up test environment
	testutil.SetTestJWTSecret(t)
	db := testutil.SetupTestDB(t)
	emailService := testutil.NewMockEmailService()
	handler := NewHandler(db, emailService)

	// Create test user (needed for database, not directly used in test assertions)
	_ = testutil.CreateTestUser(t, db, "testuser", "test@example.com", testutil.DefaultTestPassword, "user")

	tests := []struct {
		name           string
		request        interface{}
		expectedStatus int
		expectedError  string
		checkResponse  func(t *testing.T, rr *httptest.ResponseRecorder)
	}{
		{
			name: "successful login without MFA",
			request: map[string]string{
				"username": "testuser",
				"password": testutil.DefaultTestPassword,
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, rr *httptest.ResponseRecorder) {
				var resp models.LoginResponse
				testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
				assert.True(t, resp.Success)
				assert.NotEmpty(t, resp.Token)

				// Check that auth cookie was set
				cookie := testutil.AssertCookieSet(t, rr, "token")
				assert.Equal(t, resp.Token, cookie.Value)
				assert.True(t, cookie.HttpOnly)
				assert.True(t, cookie.Secure)
			},
		},
		{
			name: "invalid credentials - wrong password",
			request: map[string]string{
				"username": "testuser",
				"password": "wrongpassword",
			},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "Invalid credentials",
		},
		{
			name: "invalid credentials - non-existent user",
			request: map[string]string{
				"username": "nonexistent",
				"password": "anypassword",
			},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "Invalid credentials",
		},
		{
			name: "system user login prevented",
			request: map[string]string{
				"username": "system",
				"password": "anypassword",
			},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "Invalid credentials",
		},
		{
			name:           "invalid request format",
			request:        "not a json object",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Invalid request",
		},
		{
			name:           "empty request body",
			request:        nil,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Invalid request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			if tt.request != nil {
				if str, ok := tt.request.(string); ok {
					body = []byte(str)
				} else {
					var err error
					body, err = json.Marshal(tt.request)
					require.NoError(t, err)
				}
			}

			req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()

			handler.LoginHandler(rr, req)

			assert.Equal(t, tt.expectedStatus, rr.Code)

			if tt.expectedError != "" {
				assert.Contains(t, rr.Body.String(), tt.expectedError)
			}

			if tt.checkResponse != nil {
				tt.checkResponse(t, rr)
			}
		})
	}
}

func TestLoginHandlerWithMFA(t *testing.T) {
	// Set up test environment
	testutil.SetTestJWTSecret(t)
	db := testutil.SetupTestDB(t)
	emailService := testutil.NewMockEmailService()
	handler := NewHandler(db, emailService)

	// Create test user with MFA enabled
	testUser := testutil.CreateTestUser(t, db, "mfauser", "mfa@example.com", testutil.DefaultTestPassword, "user")

	// Enable email MFA for the user
	err := db.EnableMFA(testUser.ID.String(), "email", "")
	require.NoError(t, err)

	t.Run("login with MFA required - email method", func(t *testing.T) {
		req := testutil.MakeRequest(t, http.MethodPost, "/auth/login", map[string]string{
			"username": "mfauser",
			"password": testutil.DefaultTestPassword,
		})
		rr := httptest.NewRecorder()

		handler.LoginHandler(rr, req)

		var resp map[string]interface{}
		testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)

		assert.True(t, resp["mfa_required"].(bool))
		assert.NotEmpty(t, resp["session_token"])
		assert.Contains(t, resp["mfa_type"], "email")
		assert.Equal(t, "email", resp["preferred_method"])

		// Check that email was sent
		assert.Equal(t, 1, emailService.CallCount)
		assert.Equal(t, "mfa@example.com", emailService.LastRecipient)
		assert.NotEmpty(t, emailService.LastCode)
	})

	t.Run("login with MFA required - authenticator method", func(t *testing.T) {
		// Enable authenticator MFA
		err := db.EnableMFA(testUser.ID.String(), "authenticator", testutil.ValidTOTPSecret)
		require.NoError(t, err)

		// Set authenticator as preferred
		err = db.SetPreferredMFAMethod(testUser.ID.String(), "authenticator")
		require.NoError(t, err)

		emailService.Reset()

		req := testutil.MakeRequest(t, http.MethodPost, "/auth/login", map[string]string{
			"username": "mfauser",
			"password": testutil.DefaultTestPassword,
		})
		rr := httptest.NewRecorder()

		handler.LoginHandler(rr, req)

		var resp map[string]interface{}
		testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)

		assert.True(t, resp["mfa_required"].(bool))
		assert.NotEmpty(t, resp["session_token"])
		assert.Contains(t, resp["mfa_type"], "authenticator")
		assert.Equal(t, "authenticator", resp["preferred_method"])

		// Check that no email was sent for authenticator method
		assert.Equal(t, 0, emailService.CallCount)
	})
}

func TestLogoutHandler(t *testing.T) {
	// Set up test environment
	testutil.SetTestJWTSecret(t)
	db := testutil.SetupTestDB(t)
	emailService := testutil.NewMockEmailService()
	handler := NewHandler(db, emailService)

	// Create test user and generate token
	testUser := testutil.CreateTestUser(t, db, "testuser", "test@example.com", testutil.DefaultTestPassword, "user")
	token, err := handler.generateAuthToken(testUser, 60) // 60 minutes expiry
	require.NoError(t, err)

	// Store token in database
	_, err = db.StoreToken(testUser.ID.String(), token)
	require.NoError(t, err)

	t.Run("successful logout with token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
		req.AddCookie(&http.Cookie{
			Name:  "token",
			Value: token,
		})
		rr := httptest.NewRecorder()

		handler.LogoutHandler(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		// Check that cookie was deleted
		testutil.AssertCookieDeleted(t, rr, "token")

		// Verify token was removed from database
		exists, err := db.TokenExists(token)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("logout without token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
		rr := httptest.NewRecorder()

		handler.LogoutHandler(rr, req)

		// Should still succeed
		assert.Equal(t, http.StatusOK, rr.Code)

		// Check that cookie was deleted
		testutil.AssertCookieDeleted(t, rr, "token")
	})
}

func TestCheckAuthHandler(t *testing.T) {
	// Set up test environment
	testutil.SetTestJWTSecret(t)
	db := testutil.SetupTestDB(t)
	emailService := testutil.NewMockEmailService()
	handler := NewHandler(db, emailService)

	// Create test users
	adminUser := testutil.CreateTestUser(t, db, "admin", "admin@example.com", testutil.DefaultTestPassword, "admin")
	regularUser := testutil.CreateTestUser(t, db, "user", "user@example.com", testutil.DefaultTestPassword, "user")

	// Generate tokens
	adminToken, err := handler.generateAuthToken(adminUser, 60) // 60 minutes expiry
	require.NoError(t, err)
	userToken, err := handler.generateAuthToken(regularUser, 60) // 60 minutes expiry
	require.NoError(t, err)

	// Store tokens
	_, err = db.StoreToken(adminUser.ID.String(), adminToken)
	require.NoError(t, err)
	_, err = db.StoreToken(regularUser.ID.String(), userToken)
	require.NoError(t, err)

	tests := []struct {
		name         string
		token        string
		expectedAuth bool
		expectedRole string
	}{
		{
			name:         "valid admin token",
			token:        adminToken,
			expectedAuth: true,
			expectedRole: "admin",
		},
		{
			name:         "valid user token",
			token:        userToken,
			expectedAuth: true,
			expectedRole: "user",
		},
		{
			name:         "invalid token",
			token:        "invalid-token",
			expectedAuth: false,
			expectedRole: "",
		},
		{
			name:         "no token",
			token:        "",
			expectedAuth: false,
			expectedRole: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/auth/check", nil)
			if tt.token != "" {
				req.AddCookie(&http.Cookie{
					Name:  "token",
					Value: tt.token,
				})
			}
			rr := httptest.NewRecorder()

			handler.CheckAuthHandler(rr, req)

			var resp map[string]interface{}
			testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)

			assert.Equal(t, tt.expectedAuth, resp["authenticated"])

			if tt.expectedRole != "" {
				assert.Equal(t, tt.expectedRole, resp["role"])
			} else {
				assert.Nil(t, resp["role"])
			}
		})
	}
}

func TestCookieDomain(t *testing.T) {
	tests := []struct {
		host           string
		expectedDomain string
	}{
		{"localhost", ""},
		{"localhost:3000", ""},
		{"127.0.0.1", ""},
		{"127.0.0.1:8080", ""},
		{"example.com", "example.com"},
		{"example.com:443", "example.com"},
		{"sub.example.com", "sub.example.com"},
		{"sub.example.com:8080", "sub.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			domain := khauth.GetCookieDomain(tt.host)
			assert.Equal(t, tt.expectedDomain, domain)
		})
	}
}
