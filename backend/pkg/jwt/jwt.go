package jwt

import (
	"fmt"
	"math/rand"
	"os"
	"time"

	"encoding/base64"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var jwtKey = []byte(os.Getenv("JWT_SECRET"))

type Claims struct {
	UserID   string `json:"user_id"`
	Role     string `json:"role"`
	Remember bool   `json:"remember"`
	jwt.RegisteredClaims
}

// GenerateToken mints a standard (non-remember-me) auth token. It is a thin
// wrapper around GenerateTokenWithRemember so existing callers (MFA, passkey,
// SSO) keep their current behavior.
func GenerateToken(userID string, role string, expiryMinutes int) (string, error) {
	return GenerateTokenWithRemember(userID, role, expiryMinutes, false)
}

// GenerateTokenWithRemember mints an auth token carrying the "remember me"
// choice. The remember claim is read back on refresh (via GetRemember) so the
// sliding-window refresh keeps re-minting a persistent token and setting a
// persistent cookie, rather than silently downgrading the session.
func GenerateTokenWithRemember(userID string, role string, expiryMinutes int, remember bool) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{})

	// Set claims
	claims := token.Claims.(jwt.MapClaims)
	claims["user_id"] = userID
	claims["role"] = role
	claims["remember"] = remember
	claims["exp"] = time.Now().Add(time.Duration(expiryMinutes) * time.Minute).Unix()
	// A random jti makes every token string unique. Without it, exp has only
	// second granularity, so two tokens minted for the same user+role within one
	// second are byte-identical and the second insert violates the
	// tokens.token UNIQUE constraint (HTTP 500). Reachable on password login,
	// SSO (a retried OAuth callback / double-submitted SAMLResponse), passkey,
	// and token refresh. iat is complementary; jti carries the uniqueness.
	// Only user_id, role and remember are ever read back (ValidateJWT /
	// GetUserRole / GetRemember), so the extra claims are ignored everywhere else.
	claims["jti"] = uuid.NewString()
	claims["iat"] = time.Now().Unix()

	// Generate encoded token
	tokenString, err := token.SignedString([]byte(os.Getenv("JWT_SECRET")))
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

func ValidateJWT(tokenString string) (string, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(os.Getenv("JWT_SECRET")), nil
	})

	if err != nil {
		return "", err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		userID := claims["user_id"].(string)
		return userID, nil
	}

	return "", fmt.Errorf("invalid token")
}

// GetUserRole extracts the user's role from the JWT token
func GetUserRole(tokenString string) (string, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(os.Getenv("JWT_SECRET")), nil
	})

	if err != nil {
		return "", err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		if role, ok := claims["role"].(string); ok {
			return role, nil
		}
		return "", fmt.Errorf("role claim not found in token")
	}

	return "", fmt.Errorf("invalid token")
}

// GetRemember extracts the "remember me" flag from the JWT token. A token minted
// before this claim existed (or any token without it) reports false, so the
// session degrades to a normal browser-close session rather than failing.
func GetRemember(tokenString string) (bool, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(os.Getenv("JWT_SECRET")), nil
	})

	if err != nil {
		return false, err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		if remember, ok := claims["remember"].(bool); ok {
			return remember, nil
		}
		return false, nil
	}

	return false, fmt.Errorf("invalid token")
}

// IsAdmin checks if the user has admin privileges
func IsAdmin(userID string) (bool, error) {
	// TODO: Implement proper role checking from database
	// For now, just check if the user is the default admin
	defaultAdminID := os.Getenv("DEFAULT_ADMIN_ID")
	return userID == defaultAdminID, nil
}

// GenerateSecureToken generates a secure random token for MFA sessions
func GenerateSecureToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return base64.URLEncoding.EncodeToString(b)
}
