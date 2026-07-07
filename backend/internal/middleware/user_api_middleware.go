package middleware

import (
	"context"
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// UserAPIKeyMiddleware authenticates requests using user email and API key
func UserAPIKeyMiddleware(userAPIService *services.UserAPIService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			debug.Info("Processing User API key authentication for %s %s", r.Method, r.URL.Path)

			// Get user email from header
			email := r.Header.Get("X-User-Email")
			if email == "" {
				debug.Error("No user email provided")
				sendAPIError(w, "User email required", "AUTH_MISSING_CREDENTIALS", http.StatusUnauthorized)
				return
			}

			// Get API key from header
			apiKey := r.Header.Get("X-API-Key")
			if apiKey == "" {
				debug.Error("No API key provided")
				sendAPIError(w, "API key required", "AUTH_MISSING_CREDENTIALS", http.StatusUnauthorized)
				return
			}

			// Validate API key and get user ID
			userID, err := userAPIService.ValidateAPIKey(r.Context(), email, apiKey)
			if err != nil {
				debug.Error("Invalid API key for email %s: %v", email, err)
				sendAPIError(w, "Invalid credentials", "AUTH_INVALID_CREDENTIALS", http.StatusUnauthorized)
				return
			}

			// Look up the user's role so team-aware handlers can honor the admin
			// bypass (mirrors the web auth middleware). A lookup failure is
			// non-fatal — default to an empty role, which fails closed (non-admin).
			role, roleErr := userAPIService.GetUserRole(r.Context(), userID)
			if roleErr != nil {
				debug.Warning("Failed to load role for user %s: %v", userID.String(), roleErr)
				role = ""
			}

			// Store user ID and role in context for handlers
			ctx := context.WithValue(r.Context(), "user_id", userID.String())
			ctx = context.WithValue(ctx, "user_uuid", userID)
			ctx = context.WithValue(ctx, "user_role", role)
			r = r.WithContext(ctx)

			debug.Info("User API key authentication successful for user %s", userID.String())
			next.ServeHTTP(w, r)
		})
	}
}

// sendAPIError sends a standardized JSON error response
func sendAPIError(w http.ResponseWriter, message, code string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Simple JSON encoding
	w.Write([]byte(`{"error":"` + message + `","code":"` + code + `"}`))
}
