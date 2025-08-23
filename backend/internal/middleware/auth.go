package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/jwt"
)

// RequireAuth middleware ensures that only authenticated users can access the route
func RequireAuth(database *db.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			isSSERequest := r.URL.Path == "/api/jobs/stream"

			// Skip auth for agent routes - they use API key authentication
			if strings.HasPrefix(r.URL.Path, "/api/agent/") {
				debug.Debug("[AUTH] Skipping JWT auth for agent route: %s", r.URL.Path)
				next.ServeHTTP(w, r)
				return
			}

			if isSSERequest {
				debug.Info("[AUTH] SSE request authentication check for %s %s", r.Method, r.URL.Path)
				debug.Debug("[AUTH] SSE Request headers: %+v", r.Header)
			} else {
				debug.Debug("[AUTH] Checking authentication for %s %s", r.Method, r.URL.Path)
			}

			// Skip middleware for OPTIONS requests
			if r.Method == "OPTIONS" {
				debug.Debug("[AUTH] Skipping auth check for OPTIONS request")
				next.ServeHTTP(w, r)
				return
			}

			// Log all cookies for debugging
			if isSSERequest {
				debug.Info("[AUTH] SSE Request cookies count: %d", len(r.Cookies()))
				for _, cookie := range r.Cookies() {
					debug.Debug("[AUTH] SSE Cookie: %s (length: %d, secure: %v, httpOnly: %v)",
						cookie.Name, len(cookie.Value), cookie.Secure, cookie.HttpOnly)
				}
			} else {
				debug.Debug("[AUTH] Request cookies: %v", r.Cookies())
			}

			// Get token from cookie
			cookie, err := r.Cookie("token")
			if err != nil {
				if isSSERequest {
					debug.Error("[AUTH] SSE: No auth token found in cookies - %v", err)
				} else {
					debug.Warning("[AUTH] No auth token found in cookies for %s %s", r.Method, r.URL.Path)
				}
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			if isSSERequest {
				debug.Info("[AUTH] SSE: Found auth token cookie: %s (length: %d)",
					cookie.Name, len(cookie.Value))
			} else {
				debug.Debug("[AUTH] Found auth token cookie: %s", cookie.Name)
			}

			// Validate token and get user ID
			userID, err := jwt.ValidateJWT(cookie.Value)
			if err != nil {
				if isSSERequest {
					debug.Error("[AUTH] SSE: Invalid token - %v", err)
				} else {
					debug.Warning("[AUTH] Invalid token: %v", err)
				}
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			if isSSERequest {
				debug.Debug("[AUTH] SSE: Token validation successful for user: %s", userID)
			}

			// Verify token exists in database
			exists, err := database.TokenExists(cookie.Value)
			if err != nil {
				if isSSERequest {
					debug.Error("[AUTH] SSE: Error checking token in database - %v", err)
				} else {
					debug.Error("[AUTH] Error checking token in database: %v", err)
				}
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}
			if !exists {
				if isSSERequest {
					debug.Error("[AUTH] SSE: Token not found in database for user ID: %s", userID)
				} else {
					debug.Warning("[AUTH] Token not found in database for user ID: %s", userID)
				}
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Check if token has exceeded idle timeout
			expired, err := database.IsTokenExpiredByIdleTimeout(cookie.Value)
			if err != nil {
				debug.Error("[AUTH] Error checking token idle timeout: %v", err)
				// Don't block on error, let request continue
			} else if expired {
				debug.Warning("[AUTH] Token expired due to idle timeout for user ID: %s", userID)
				http.Error(w, "Session expired due to inactivity", http.StatusUnauthorized)
				return
			}

			// Update last activity for non-auto-refresh requests
			isAutoRefresh := r.Header.Get("X-Auto-Refresh") == "true" ||
				r.URL.Path == "/api/dashboard/stats" ||
				r.URL.Path == "/api/jobs" ||
				r.URL.Path == "/api/jobs/stream"
			
			if !isAutoRefresh {
				if err := database.UpdateTokenActivity(cookie.Value); err != nil {
					debug.Error("[AUTH] Failed to update token activity: %v", err)
					// Don't block on error, let request continue
				}
			}

			if isSSERequest {
				debug.Debug("[AUTH] SSE: Token found in database for user: %s", userID)
			}

			// Get user role from token
			role, err := jwt.GetUserRole(cookie.Value)
			if err != nil {
				// This shouldn't happen if ValidateJWT passed, but handle defensively
				if isSSERequest {
					debug.Error("[AUTH] SSE: Failed to get role from valid token - %v", err)
				} else {
					debug.Warning("[AUTH] Failed to get role from valid token: %v", err)
				}
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Add user ID and role to request context
			ctx := context.WithValue(r.Context(), "user_id", userID)
			ctx = context.WithValue(ctx, "user_role", role) // Add role to context
			r = r.WithContext(ctx)

			if isSSERequest {
				debug.Info("[AUTH] SSE: Authentication successful for user: %s with role: %s", userID, role)
				debug.Debug("[AUTH] SSE: Proceeding to SSE handler")
			} else {
				debug.Debug("[AUTH] Authentication successful for user: %s with role: %s", userID, role)
			}

			next.ServeHTTP(w, r)
		})
	}
}
