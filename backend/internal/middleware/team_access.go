package middleware

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/google/uuid"
)

// Context key constants for team-related data.
//
// These are plain string constants (not a custom type) because the existing
// auth middleware (auth.go, user_api_middleware.go) stores values using raw
// string keys like context.WithValue(ctx, "user_id", userID). Using a typed
// key (e.g., type contextKey string) would create a different key type that
// would never match when doing ctx.Value() lookups.
const (
	// Existing keys (used by auth.go and user_api_middleware.go)
	ContextKeyUserID   = "user_id"
	ContextKeyUserUUID = "user_uuid"
	ContextKeyUserRole = "user_role"

	// New team-related keys
	ContextKeyTeamsEnabled = "teams_enabled"
	ContextKeyUserTeamIDs  = "user_team_ids"
)

// TeamAccessMiddleware enriches the request context with team information
// This middleware should be applied AFTER authentication middleware
func TeamAccessMiddleware(teamService *services.TeamService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Check if teams mode is enabled
			teamsEnabled := teamService.IsTeamsEnabled(ctx)

			// Always add teams_enabled to context
			ctx = context.WithValue(ctx, ContextKeyTeamsEnabled, teamsEnabled)

			// If teams not enabled, skip loading team IDs
			if !teamsEnabled {
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Get user ID from context (set by auth middleware)
			// Handles both middleware paths:
			//   - user_api_middleware sets "user_uuid" as uuid.UUID
			//   - auth.go sets "user_id" as string
			userID, err := getUserIDFromContext(ctx)
			if err != nil {
				// No valid user ID - likely unauthenticated request
				// Let the auth middleware handle this
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Load user's team IDs
			teamIDs, err := teamService.GetUserTeamIDs(ctx, userID)
			if err != nil {
				// Log error but don't fail the request
				// The handler can check for missing team IDs
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Add team IDs to context
			ctx = context.WithValue(ctx, ContextKeyUserTeamIDs, teamIDs)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// =============================================================================
// Context Helper Functions
// =============================================================================

// IsTeamsEnabledFromContext returns whether teams mode is enabled
func IsTeamsEnabledFromContext(ctx context.Context) bool {
	enabled, ok := ctx.Value(ContextKeyTeamsEnabled).(bool)
	if !ok {
		return false
	}
	return enabled
}

// GetUserTeamIDsFromContext returns the user's team IDs from context
// Returns nil if teams are disabled or team IDs not loaded
func GetUserTeamIDsFromContext(ctx context.Context) []uuid.UUID {
	teamIDs, ok := ctx.Value(ContextKeyUserTeamIDs).([]uuid.UUID)
	if !ok {
		return nil
	}
	return teamIDs
}

// IsUserInTeamFromContext checks if the user is a member of a specific team
// Returns true if teams are disabled (everyone has access)
func IsUserInTeamFromContext(ctx context.Context, teamID uuid.UUID) bool {
	if !IsTeamsEnabledFromContext(ctx) {
		return true
	}

	teamIDs := GetUserTeamIDsFromContext(ctx)
	if teamIDs == nil {
		return false
	}

	for _, id := range teamIDs {
		if id == teamID {
			return true
		}
	}
	return false
}

// HasAnyTeamFromContext checks if the user is in any of the given teams
// Returns true if teams are disabled (everyone has access)
func HasAnyTeamFromContext(ctx context.Context, teamIDs []uuid.UUID) bool {
	if !IsTeamsEnabledFromContext(ctx) {
		return true
	}

	userTeamIDs := GetUserTeamIDsFromContext(ctx)
	if userTeamIDs == nil {
		return false
	}

	// Create a set of user's team IDs for O(1) lookup
	userTeamSet := make(map[uuid.UUID]struct{}, len(userTeamIDs))
	for _, id := range userTeamIDs {
		userTeamSet[id] = struct{}{}
	}

	// Check if any of the given teams match
	for _, id := range teamIDs {
		if _, exists := userTeamSet[id]; exists {
			return true
		}
	}
	return false
}

// getUserIDFromContext extracts user UUID from context (unexported helper)
// Handles both middleware paths:
//   - user_api_middleware sets "user_uuid" as uuid.UUID
//   - auth.go sets "user_id" as string
//
// Note: Uses raw string keys for context lookups because existing middleware
// (auth.go, user_api_middleware.go) stores values with raw string keys.
// The ContextKey* constants are plain strings, so they match correctly.
func getUserIDFromContext(ctx context.Context) (uuid.UUID, error) {
	// Try "user_uuid" first (set by user_api_middleware.go as uuid.UUID)
	if uid, ok := ctx.Value("user_uuid").(uuid.UUID); ok {
		return uid, nil
	}

	// Fall back to "user_id" (set by auth.go as string)
	if uidStr, ok := ctx.Value("user_id").(string); ok && uidStr != "" {
		parsed, err := uuid.Parse(uidStr)
		if err != nil {
			return uuid.Nil, fmt.Errorf("invalid user_id in context: %w", err)
		}
		return parsed, nil
	}

	return uuid.Nil, fmt.Errorf("user ID not found in context")
}

// GetUserIDFromContext returns the user UUID from context (exported wrapper)
// Handles both middleware paths:
//   - user_api_middleware sets "user_uuid" as uuid.UUID
//   - auth.go sets "user_id" as string
func GetUserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, err := getUserIDFromContext(ctx)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// getUserRoleFromContext extracts user role from context
// Uses raw string key to match what auth.go sets: context.WithValue(ctx, "user_role", role)
func getUserRoleFromContext(ctx context.Context) string {
	if role, ok := ctx.Value("user_role").(string); ok {
		return role
	}
	return ""
}

// IsAdminFromContext checks if the user is a system admin
// Checks the "user_role" context key set by auth middleware
func IsAdminFromContext(ctx context.Context) bool {
	return getUserRoleFromContext(ctx) == "admin"
}
