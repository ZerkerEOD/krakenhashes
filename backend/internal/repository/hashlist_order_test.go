package repository

import "testing"

// TestHashlistOrderByClause verifies the client-facing sort field is validated
// against the allowlist (so it cannot be injected into the ORDER BY clause),
// that the direction is normalized, and that unknown input falls back to the
// historical default ordering.
func TestHashlistOrderByClause(t *testing.T) {
	cases := []struct {
		name   string
		sortBy string
		order  string
		want   string
	}{
		{"name asc", "name", "asc", " ORDER BY LOWER(h.name) ASC, h.id ASC"},
		{"name desc", "name", "desc", " ORDER BY LOWER(h.name) DESC, h.id DESC"},
		{"client name case-insensitive order", "clientName", "DESC", " ORDER BY LOWER(c.name) DESC, h.id DESC"},
		{"status asc", "status", "asc", " ORDER BY h.status ASC, h.id ASC"},
		{"created asc", "createdAt", "asc", " ORDER BY h.created_at ASC, h.id ASC"},
		{"empty falls back to default", "", "", " ORDER BY h.created_at DESC, h.id DESC"},
		{"unknown field falls back to default", "bogus", "asc", " ORDER BY h.created_at DESC, h.id DESC"},
		{"injection attempt falls back to default", "h.name; DROP TABLE hashlists", "asc", " ORDER BY h.created_at DESC, h.id DESC"},
		{"unknown order defaults to asc", "name", "garbage", " ORDER BY LOWER(h.name) ASC, h.id ASC"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hashlistOrderByClause(tc.sortBy, tc.order); got != tc.want {
				t.Fatalf("hashlistOrderByClause(%q, %q) = %q, want %q", tc.sortBy, tc.order, got, tc.want)
			}
		})
	}
}
