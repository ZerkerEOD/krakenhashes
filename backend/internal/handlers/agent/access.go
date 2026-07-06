package agent

import (
	"context"
	"errors"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/middleware"
	"github.com/google/uuid"
)

// ErrAgentForbidden is returned when a user tries to access an agent they don't own.
var ErrAgentForbidden = errors.New("forbidden: you do not own this agent")

// checkAgentOwnership verifies the current user owns the agent.
// Returns nil if:
//   - teams are not enabled (no restriction)
//   - user is admin (admins bypass)
//   - user's ID matches the agent's owner_id
//
// Returns ErrAgentForbidden if the user doesn't own the agent.
func checkAgentOwnership(ctx context.Context, agentOwnerID *uuid.UUID) error {
	if !middleware.IsTeamsEnabledFromContext(ctx) || middleware.IsAdminFromContext(ctx) {
		return nil
	}

	userIDStr, ok := ctx.Value("user_id").(string)
	if !ok || userIDStr == "" {
		return errors.New("unauthorized")
	}

	userUUID, err := uuid.Parse(userIDStr)
	if err != nil {
		return errors.New("unauthorized")
	}

	if agentOwnerID == nil || *agentOwnerID != userUUID {
		return ErrAgentForbidden
	}

	return nil
}
