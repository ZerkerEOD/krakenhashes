package repository

import "testing"

// TestApplyMaxAgentsLimit exercises the pure max_agents policy (GH #126) without
// a database. ResolveMaxAgents layers only the limit lookup on top of this.
func TestApplyMaxAgentsLimit(t *testing.T) {
	tests := []struct {
		name      string
		requested int
		limit     int
		want      int
		wantErr   bool
	}{
		{name: "no limit passes value through", requested: 50, limit: 0, want: 50},
		{name: "no limit keeps unlimited", requested: 0, limit: 0, want: 0},
		{name: "unlimited clamped to limit", requested: 0, limit: 3, want: 3},
		{name: "within limit unchanged", requested: 2, limit: 3, want: 2},
		{name: "at limit unchanged", requested: 3, limit: 3, want: 3},
		{name: "over limit rejected", requested: 4, limit: 3, wantErr: true},
		{name: "negative rejected regardless of limit", requested: -1, limit: 0, wantErr: true},
		{name: "negative rejected with limit", requested: -1, limit: 5, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplyMaxAgentsLimit(tt.requested, tt.limit)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ApplyMaxAgentsLimit(%d, %d) = %d, want error", tt.requested, tt.limit, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ApplyMaxAgentsLimit(%d, %d) unexpected error: %v", tt.requested, tt.limit, err)
			}
			if got != tt.want {
				t.Errorf("ApplyMaxAgentsLimit(%d, %d) = %d, want %d", tt.requested, tt.limit, got, tt.want)
			}
		})
	}
}
