package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenManagement(t *testing.T) {
	// Set up test environment
	testutil.SetTestJWTSecret(t)
	db := testutil.SetupTestDB(t)
	emailService := testutil.NewMockEmailService()
	handler := NewHandler(db, emailService)

	// Create test user
	testUser := testutil.CreateTestUser(t, db, "testuser", "test@example.com", testutil.DefaultTestPassword, "user")

	t.Run("token storage and retrieval", func(t *testing.T) {
		// Generate token
		token, err := handler.generateAuthToken(testUser, 60)
		require.NoError(t, err)
		assert.NotEmpty(t, token)

		// Store token
		_, err = db.StoreToken(testUser.ID.String(), token)
		require.NoError(t, err)

		// Verify token exists
		exists, err := db.TokenExists(token)
		require.NoError(t, err)
		assert.True(t, exists)

		// Remove token
		err = db.RemoveTokenByString(token)
		require.NoError(t, err)

		// Verify token no longer exists
		exists, err = db.TokenExists(token)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("token expiration", func(t *testing.T) {
		// Generate token
		token, err := jwt.GenerateToken(testUser.ID.String(), testUser.Role, 60)
		require.NoError(t, err)

		// Parse token to check expiration
		userID, err := jwt.ValidateJWT(token)
		require.NoError(t, err)
		assert.Equal(t, testUser.ID.String(), userID)

		// Token should be valid for 24 hours
		// This is verified in the JWT tests, but we ensure it works in auth context
	})

	t.Run("concurrent token operations", func(t *testing.T) {
		// Test that multiple tokens can exist for the same user
		tokens := make([]string, 3)

		// Create multiple tokens
		for i := range tokens {
			token, err := handler.generateAuthToken(testUser, 60)
			require.NoError(t, err)
			tokens[i] = token

			_, err = db.StoreToken(testUser.ID.String(), token)
			require.NoError(t, err)
		}

		// Verify all tokens exist
		for _, token := range tokens {
			exists, err := db.TokenExists(token)
			require.NoError(t, err)
			assert.True(t, exists)
		}

		// Remove one token shouldn't affect others
		err := db.RemoveTokenByString(tokens[0])
		require.NoError(t, err)

		// First token should not exist
		exists, err := db.TokenExists(tokens[0])
		require.NoError(t, err)
		assert.False(t, exists)

		// Other tokens should still exist
		for i := 1; i < len(tokens); i++ {
			exists, err := db.TokenExists(tokens[i])
			require.NoError(t, err)
			assert.True(t, exists)
		}

		// Clean up
		for i := 1; i < len(tokens); i++ {
			db.RemoveTokenByString(tokens[i])
		}
	})

	t.Run("token validation in auth check", func(t *testing.T) {
		// Generate and store token
		token, err := handler.generateAuthToken(testUser, 60)
		require.NoError(t, err)
		_, err = db.StoreToken(testUser.ID.String(), token)
		require.NoError(t, err)

		// Make authenticated request
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
		assert.Equal(t, "user", resp["role"])

		// Remove token and check again
		err = db.RemoveTokenByString(token)
		require.NoError(t, err)

		req = httptest.NewRequest(http.MethodGet, "/auth/check", nil)
		req.AddCookie(&http.Cookie{
			Name:  "token",
			Value: token,
		})
		rr = httptest.NewRecorder()

		handler.CheckAuthHandler(rr, req)

		testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
		assert.False(t, resp["authenticated"].(bool))
	})
}

func TestTokenSecurityFeatures(t *testing.T) {
	// Set up test environment
	testutil.SetTestJWTSecret(t)
	db := testutil.SetupTestDB(t)
	emailService := testutil.NewMockEmailService()
	handler := NewHandler(db, emailService)

	// Create test users
	user1 := testutil.CreateTestUser(t, db, "user1", "user1@example.com", testutil.DefaultTestPassword, "user")
	user2 := testutil.CreateTestUser(t, db, "user2", "user2@example.com", testutil.DefaultTestPassword, "user")

	t.Run("token isolation between users", func(t *testing.T) {
		// Generate tokens for both users
		token1, err := handler.generateAuthToken(user1, 60)
		require.NoError(t, err)
		token2, err := handler.generateAuthToken(user2, 60)
		require.NoError(t, err)

		// Store both tokens
		_, err = db.StoreToken(user1.ID.String(), token1)
		require.NoError(t, err)
		_, err = db.StoreToken(user2.ID.String(), token2)
		require.NoError(t, err)

		// Verify tokens are different
		assert.NotEqual(t, token1, token2)

		// Each token should authenticate the correct user
		userID1, err := jwt.ValidateJWT(token1)
		require.NoError(t, err)
		assert.Equal(t, user1.ID.String(), userID1)

		userID2, err := jwt.ValidateJWT(token2)
		require.NoError(t, err)
		assert.Equal(t, user2.ID.String(), userID2)
	})

	t.Run("token replay prevention", func(t *testing.T) {
		// Login and get token
		req := testutil.MakeRequest(t, http.MethodPost, "/auth/login", map[string]string{
			"username": "user1",
			"password": testutil.DefaultTestPassword,
		})
		rr := httptest.NewRecorder()
		handler.LoginHandler(rr, req)

		var loginResp models.LoginResponse
		testutil.AssertJSONResponse(t, rr, http.StatusOK, &loginResp)
		token := loginResp.Token

		// Logout (removes token)
		req = httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
		req.AddCookie(&http.Cookie{
			Name:  "token",
			Value: token,
		})
		rr = httptest.NewRecorder()
		handler.LogoutHandler(rr, req)

		// Try to use the same token again
		req = httptest.NewRequest(http.MethodGet, "/auth/check", nil)
		req.AddCookie(&http.Cookie{
			Name:  "token",
			Value: token,
		})
		rr = httptest.NewRecorder()
		handler.CheckAuthHandler(rr, req)

		var resp map[string]interface{}
		testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
		assert.False(t, resp["authenticated"].(bool))
	})

	t.Run("invalid token handling", func(t *testing.T) {
		tests := []struct {
			name  string
			token string
		}{
			{"malformed token", "invalid.token.format"},
			{"empty token", ""},
			{"random string", "randomstring123"},
			{"expired token format", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjB9.invalid"},
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
				assert.False(t, resp["authenticated"].(bool))
			})
		}
	})
}

func TestTokenCookieHandling(t *testing.T) {
	// Set up test environment
	testutil.SetTestJWTSecret(t)
	db := testutil.SetupTestDB(t)
	emailService := testutil.NewMockEmailService()
	handler := NewHandler(db, emailService)

	// Create test user (needed for database, not directly used in test assertions)
	_ = testutil.CreateTestUser(t, db, "testuser", "test@example.com", testutil.DefaultTestPassword, "user")

	t.Run("cookie attributes on login", func(t *testing.T) {
		req := testutil.MakeRequest(t, http.MethodPost, "/auth/login", map[string]string{
			"username": "testuser",
			"password": testutil.DefaultTestPassword,
		})
		req.Host = "example.com"
		rr := httptest.NewRecorder()

		handler.LoginHandler(rr, req)

		cookie := testutil.AssertCookieSet(t, rr, "token")
		assert.True(t, cookie.HttpOnly)
		assert.True(t, cookie.Secure)
		assert.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
		assert.Equal(t, "/", cookie.Path)
		assert.Equal(t, "example.com", cookie.Domain)
		assert.Equal(t, int(time.Hour*24*7/time.Second), cookie.MaxAge)
	})

	t.Run("cookie removal on logout", func(t *testing.T) {
		// First login
		req := testutil.MakeRequest(t, http.MethodPost, "/auth/login", map[string]string{
			"username": "testuser",
			"password": testutil.DefaultTestPassword,
		})
		rr := httptest.NewRecorder()
		handler.LoginHandler(rr, req)

		var loginResp models.LoginResponse
		json.NewDecoder(rr.Body).Decode(&loginResp)

		// Then logout
		req = httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
		req.AddCookie(&http.Cookie{
			Name:  "token",
			Value: loginResp.Token,
		})
		rr = httptest.NewRecorder()

		handler.LogoutHandler(rr, req)

		cookie := testutil.AssertCookieSet(t, rr, "token")
		assert.Equal(t, "", cookie.Value)
		assert.Equal(t, -1, cookie.MaxAge)
	})

	t.Run("cookie domain handling for different hosts", func(t *testing.T) {
		hosts := []struct {
			host           string
			expectedDomain string
		}{
			{"localhost", ""},
			{"localhost:3000", ""},
			{"127.0.0.1", ""},
			{"127.0.0.1:8080", ""},
			{"app.example.com", "app.example.com"},
			{"app.example.com:443", "app.example.com"},
		}

		for _, tt := range hosts {
			t.Run(tt.host, func(t *testing.T) {
				req := testutil.MakeRequest(t, http.MethodPost, "/auth/login", map[string]string{
					"username": "testuser",
					"password": testutil.DefaultTestPassword,
				})
				req.Host = tt.host
				rr := httptest.NewRecorder()

				handler.LoginHandler(rr, req)

				cookie := testutil.AssertCookieSet(t, rr, "token")
				assert.Equal(t, tt.expectedDomain, cookie.Domain)
			})
		}
	})
}

func TestMultiDeviceTokenSupport(t *testing.T) {
	// Set up test environment
	testutil.SetTestJWTSecret(t)
	db := testutil.SetupTestDB(t)
	emailService := testutil.NewMockEmailService()
	handler := NewHandler(db, emailService)

	// Create test user
	testUser := testutil.CreateTestUser(t, db, "testuser", "test@example.com", testutil.DefaultTestPassword, "user")

	t.Run("multiple active sessions", func(t *testing.T) {
		devices := []string{"desktop", "mobile", "tablet"}
		tokens := make(map[string]string)

		// Login from multiple devices
		for _, device := range devices {
			req := testutil.MakeRequest(t, http.MethodPost, "/auth/login", map[string]string{
				"username": "testuser",
				"password": testutil.DefaultTestPassword,
			})
			req.Header.Set("User-Agent", device)
			rr := httptest.NewRecorder()

			handler.LoginHandler(rr, req)

			var resp models.LoginResponse
			testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
			tokens[device] = resp.Token

			// Store token
			_, err := db.StoreToken(testUser.ID.String(), resp.Token)
			require.NoError(t, err)
		}

		// Verify all tokens are valid
		for device, token := range tokens {
			req := httptest.NewRequest(http.MethodGet, "/auth/check", nil)
			req.AddCookie(&http.Cookie{
				Name:  "token",
				Value: token,
			})
			rr := httptest.NewRecorder()

			handler.CheckAuthHandler(rr, req)

			var resp map[string]interface{}
			testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
			assert.True(t, resp["authenticated"].(bool), "Token for %s should be valid", device)
		}

		// Logout from one device shouldn't affect others
		req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
		req.AddCookie(&http.Cookie{
			Name:  "token",
			Value: tokens["mobile"],
		})
		rr := httptest.NewRecorder()
		handler.LogoutHandler(rr, req)

		// Mobile token should be invalid
		req = httptest.NewRequest(http.MethodGet, "/auth/check", nil)
		req.AddCookie(&http.Cookie{
			Name:  "token",
			Value: tokens["mobile"],
		})
		rr = httptest.NewRecorder()
		handler.CheckAuthHandler(rr, req)

		var resp map[string]interface{}
		testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
		assert.False(t, resp["authenticated"].(bool))

		// Other tokens should still be valid
		for device, token := range tokens {
			if device == "mobile" {
				continue
			}

			req := httptest.NewRequest(http.MethodGet, "/auth/check", nil)
			req.AddCookie(&http.Cookie{
				Name:  "token",
				Value: token,
			})
			rr := httptest.NewRecorder()

			handler.CheckAuthHandler(rr, req)

			testutil.AssertJSONResponse(t, rr, http.StatusOK, &resp)
			assert.True(t, resp["authenticated"].(bool), "Token for %s should still be valid", device)
		}
	})
}
