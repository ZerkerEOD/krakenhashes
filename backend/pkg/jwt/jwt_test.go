package jwt

import (
	"encoding/base64"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateToken(t *testing.T) {
	// Set test JWT secret
	oldSecret := os.Getenv("JWT_SECRET")
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer func() {
		if oldSecret != "" {
			os.Setenv("JWT_SECRET", oldSecret)
		} else {
			os.Unsetenv("JWT_SECRET")
		}
	}()

	tests := []struct {
		name   string
		userID string
		role   string
	}{
		{
			name:   "admin user token",
			userID: uuid.New().String(),
			role:   "admin",
		},
		{
			name:   "regular user token",
			userID: uuid.New().String(),
			role:   "user",
		},
		{
			name:   "agent token",
			userID: uuid.New().String(),
			role:   "agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := GenerateToken(tt.userID, tt.role, 60)
			require.NoError(t, err)
			assert.NotEmpty(t, token)

			// Parse the token to verify claims
			parsedToken, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
				return []byte("test-secret-key"), nil
			})
			require.NoError(t, err)
			assert.True(t, parsedToken.Valid)

			claims, ok := parsedToken.Claims.(jwt.MapClaims)
			require.True(t, ok)

			assert.Equal(t, tt.userID, claims["user_id"])
			assert.Equal(t, tt.role, claims["role"])

			// Check expiration is set correctly (24 hours)
			exp, ok := claims["exp"].(float64)
			require.True(t, ok)
			expTime := time.Unix(int64(exp), 0)
			assert.True(t, expTime.After(time.Now()))
			assert.True(t, expTime.Before(time.Now().Add(25*time.Hour)))
		})
	}
}

func TestValidateJWT(t *testing.T) {
	// Set test JWT secret
	oldSecret := os.Getenv("JWT_SECRET")
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer func() {
		if oldSecret != "" {
			os.Setenv("JWT_SECRET", oldSecret)
		} else {
			os.Unsetenv("JWT_SECRET")
		}
	}()

	userID := uuid.New().String()
	role := "user"

	// Generate a valid token
	validToken, err := GenerateToken(userID, role, 60)
	require.NoError(t, err)

	// Generate an expired token
	expiredToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{})
	claims := expiredToken.Claims.(jwt.MapClaims)
	claims["user_id"] = userID
	claims["role"] = role
	claims["exp"] = time.Now().Add(-1 * time.Hour).Unix() // Expired 1 hour ago
	expiredTokenString, err := expiredToken.SignedString([]byte("test-secret-key"))
	require.NoError(t, err)

	// Generate a token with wrong signing method
	wrongMethodToken := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{}) // Using RS256 instead of HS256
	claims = wrongMethodToken.Claims.(jwt.MapClaims)
	claims["user_id"] = userID
	claims["role"] = role
	claims["exp"] = time.Now().Add(time.Hour).Unix()
	// This will fail during validation due to wrong signing method

	tests := []struct {
		name          string
		token         string
		expectedID    string
		expectedError bool
	}{
		{
			name:          "valid token",
			token:         validToken,
			expectedID:    userID,
			expectedError: false,
		},
		{
			name:          "expired token",
			token:         expiredTokenString,
			expectedID:    "",
			expectedError: true,
		},
		{
			name:          "invalid token format",
			token:         "invalid.token.format",
			expectedID:    "",
			expectedError: true,
		},
		{
			name:          "empty token",
			token:         "",
			expectedID:    "",
			expectedError: true,
		},
		{
			name:          "token signed with wrong secret",
			token:         generateTokenWithSecret(t, userID, role, "wrong-secret"),
			expectedID:    "",
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := ValidateJWT(tt.token)

			if tt.expectedError {
				assert.Error(t, err)
				assert.Empty(t, id)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedID, id)
			}
		})
	}
}

func TestGetUserRole(t *testing.T) {
	// Set test JWT secret
	oldSecret := os.Getenv("JWT_SECRET")
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer func() {
		if oldSecret != "" {
			os.Setenv("JWT_SECRET", oldSecret)
		} else {
			os.Unsetenv("JWT_SECRET")
		}
	}()

	tests := []struct {
		name         string
		userID       string
		role         string
		expectedRole string
		expectError  bool
	}{
		{
			name:         "admin role",
			userID:       uuid.New().String(),
			role:         "admin",
			expectedRole: "admin",
			expectError:  false,
		},
		{
			name:         "user role",
			userID:       uuid.New().String(),
			role:         "user",
			expectedRole: "user",
			expectError:  false,
		},
		{
			name:         "agent role",
			userID:       uuid.New().String(),
			role:         "agent",
			expectedRole: "agent",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := GenerateToken(tt.userID, tt.role, 60)
			require.NoError(t, err)

			role, err := GetUserRole(token)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedRole, role)
			}
		})
	}

	// Test with invalid tokens
	t.Run("invalid token", func(t *testing.T) {
		role, err := GetUserRole("invalid.token")
		assert.Error(t, err)
		assert.Empty(t, role)
	})

	// Test with token missing role claim
	t.Run("token without role claim", func(t *testing.T) {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{})
		claims := token.Claims.(jwt.MapClaims)
		claims["user_id"] = uuid.New().String()
		claims["exp"] = time.Now().Add(time.Hour).Unix()
		// Deliberately not setting "role" claim

		tokenString, err := token.SignedString([]byte("test-secret-key"))
		require.NoError(t, err)

		role, err := GetUserRole(tokenString)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "role claim not found")
		assert.Empty(t, role)
	})
}

func TestGenerateSecureToken(t *testing.T) {
	// Generate multiple tokens to ensure they're unique
	tokens := make(map[string]bool)

	for i := 0; i < 100; i++ {
		token := GenerateSecureToken()

		// Check token is not empty
		assert.NotEmpty(t, token)

		// Check token is base64 encoded (shouldn't error on decode)
		_, err := base64.URLEncoding.DecodeString(token)
		assert.NoError(t, err)

		// Check uniqueness
		assert.False(t, tokens[token], "Token should be unique")
		tokens[token] = true
	}
}

func TestIsAdmin(t *testing.T) {
	// Set test admin ID
	oldAdminID := os.Getenv("DEFAULT_ADMIN_ID")
	testAdminID := uuid.New().String()
	os.Setenv("DEFAULT_ADMIN_ID", testAdminID)
	defer func() {
		if oldAdminID != "" {
			os.Setenv("DEFAULT_ADMIN_ID", oldAdminID)
		} else {
			os.Unsetenv("DEFAULT_ADMIN_ID")
		}
	}()

	tests := []struct {
		name     string
		userID   string
		expected bool
	}{
		{
			name:     "admin user",
			userID:   testAdminID,
			expected: true,
		},
		{
			name:     "non-admin user",
			userID:   uuid.New().String(),
			expected: false,
		},
		{
			name:     "empty user ID",
			userID:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isAdmin, err := IsAdmin(tt.userID)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, isAdmin)
		})
	}
}

// Helper function to generate a token with a specific secret
func generateTokenWithSecret(t *testing.T, userID, role, secret string) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{})
	claims := token.Claims.(jwt.MapClaims)
	claims["user_id"] = userID
	claims["role"] = role
	claims["exp"] = time.Now().Add(time.Hour).Unix()

	tokenString, err := token.SignedString([]byte(secret))
	require.NoError(t, err)
	return tokenString
}

func TestEmptyJWTSecret(t *testing.T) {
	// Test behavior when JWT_SECRET is not set
	oldSecret := os.Getenv("JWT_SECRET")
	os.Unsetenv("JWT_SECRET")
	defer func() {
		if oldSecret != "" {
			os.Setenv("JWT_SECRET", oldSecret)
		}
	}()

	userID := uuid.New().String()
	role := "user"

	// GenerateToken should still work (uses empty secret)
	token, err := GenerateToken(userID, role, 60)
	assert.NoError(t, err)
	assert.NotEmpty(t, token)

	// ValidateJWT should work with the same empty secret
	validatedID, err := ValidateJWT(token)
	assert.NoError(t, err)
	assert.Equal(t, userID, validatedID)
}
