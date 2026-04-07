package jwt

import (
	"fmt"
	"math/rand"
	"os"
	"time"

	"encoding/base64"

	"github.com/golang-jwt/jwt/v5"
)

var jwtKey = []byte(os.Getenv("JWT_SECRET"))

type Claims struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

func GenerateToken(userID string, role string, expiryMinutes int) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{})

	// Set claims
	claims := token.Claims.(jwt.MapClaims)
	claims["user_id"] = userID
	claims["role"] = role
	claims["exp"] = time.Now().Add(time.Duration(expiryMinutes) * time.Minute).Unix()

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
