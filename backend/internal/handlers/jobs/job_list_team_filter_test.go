package jobs

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/middleware"
	"github.com/google/uuid"
)

// jobListTeamFilter must never widen a non-admin's view: a foreign or
// malformed team_id falls back to their own teams instead of "no filter".
func TestJobListTeamFilter(t *testing.T) {
	mine := uuid.New()
	foreign := uuid.New()

	ctxFor := func(teamsEnabled bool, role string, teams []uuid.UUID) context.Context {
		ctx := context.WithValue(context.Background(), middleware.ContextKeyTeamsEnabled, teamsEnabled)
		ctx = context.WithValue(ctx, middleware.ContextKeyUserRole, role)
		if teams != nil {
			ctx = context.WithValue(ctx, middleware.ContextKeyUserTeamIDs, teams)
		}
		return ctx
	}

	cases := []struct {
		name        string
		teamsOn     bool
		role        string
		teams       []uuid.UUID
		query       string
		wantEnabled bool
		wantIDs     []uuid.UUID
	}{
		{"teams disabled", false, "user", nil, "?team_id=" + foreign.String(), false, nil},
		{"user default: own teams", true, "user", []uuid.UUID{mine}, "", true, []uuid.UUID{mine}},
		{"user own team_id", true, "user", []uuid.UUID{mine}, "?team_id=" + mine.String(), true, []uuid.UUID{mine}},
		{"user foreign team_id falls back", true, "user", []uuid.UUID{mine}, "?team_id=" + foreign.String(), true, []uuid.UUID{mine}},
		{"user malformed team_id falls back", true, "user", []uuid.UUID{mine}, "?team_id=nope", true, []uuid.UUID{mine}},
		{"user without teams fails closed", true, "user", nil, "?team_id=" + foreign.String(), true, []uuid.UUID{}},
		{"admin default: unrestricted", true, "admin", nil, "", false, nil},
		{"admin any team_id", true, "admin", nil, "?team_id=" + foreign.String(), true, []uuid.UUID{foreign}},
		{"admin scope=teams: own memberships", true, "admin", []uuid.UUID{mine}, "?scope=teams", true, []uuid.UUID{mine}},
		{"admin scope=teams without memberships", true, "admin", nil, "?scope=teams", true, []uuid.UUID{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/jobs"+tc.query, nil)
			enabled, ids := jobListTeamFilter(ctxFor(tc.teamsOn, tc.role, tc.teams), r)
			if enabled != tc.wantEnabled {
				t.Fatalf("enabled: want %v, got %v", tc.wantEnabled, enabled)
			}
			if tc.wantIDs == nil {
				if ids != nil {
					t.Fatalf("ids: want nil, got %v", ids)
				}
				return
			}
			if ids == nil || len(ids) != len(tc.wantIDs) {
				t.Fatalf("ids: want %v, got %v", tc.wantIDs, ids)
			}
			for i := range ids {
				if ids[i] != tc.wantIDs[i] {
					t.Fatalf("ids: want %v, got %v", tc.wantIDs, ids)
				}
			}
		})
	}
}
