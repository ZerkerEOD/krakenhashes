package repository

import (
	"context"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestGetCrackedHashes_OrderedByCrackedAt guards the /pot ordering fix: the cracked
// hashes list must come back newest-cracked first by cracked_at, not by last_updated
// (which a BEFORE UPDATE trigger rewrites on every row change). cracked_at is inserted
// out of order, and last_updated is set to the OPPOSITE order, so a query that ordered
// by last_updated would fail this test.
func TestGetCrackedHashes_OrderedByCrackedAt(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database test in short mode")
	}
	database := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, database)
	testutil.SeedDefaults(t, database)
	ctx := context.Background()
	repo := NewHashRepository(database)

	rows := []struct {
		hashVal     string
		crackedAt   string
		lastUpdated string
	}{
		{"aaaa", "2026-01-01T00:00:00Z", "2026-03-03T00:00:00Z"}, // oldest crack, newest last_updated
		{"bbbb", "2026-02-02T00:00:00Z", "2026-02-02T00:00:00Z"},
		{"cccc", "2026-03-03T00:00:00Z", "2026-01-01T00:00:00Z"}, // newest crack, oldest last_updated
	}
	for _, r := range rows {
		if _, err := database.Exec(`
			INSERT INTO hashes (hash_value, original_hash, hash_type_id, is_cracked, password, last_updated, cracked_at)
			VALUES ($1, $1, 0, true, 'pw', $2, $3)`, r.hashVal, r.lastUpdated, r.crackedAt); err != nil {
			t.Fatalf("insert %s: %v", r.hashVal, err)
		}
	}

	got, total, err := repo.GetCrackedHashes(ctx, CrackedHashParams{Limit: 10, Offset: 0})
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Len(t, got, 3)
	// Newest cracked_at first, regardless of last_updated.
	require.Equal(t, "cccc", got[0].HashValue)
	require.Equal(t, "bbbb", got[1].HashValue)
	require.Equal(t, "aaaa", got[2].HashValue)
}

// TestGetCrackedHashesByClientIDs_Executes guards the wrapped DISTINCT ON team-path
// query: both the search and no-search branches must be valid SQL that execute
// (the cracked_at ordering wrap is easy to break syntactically).
func TestGetCrackedHashesByClientIDs_Executes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database test in short mode")
	}
	database := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, database)
	testutil.SeedDefaults(t, database)
	ctx := context.Background()
	repo := NewHashRepository(database)

	clientIDs := []uuid.UUID{uuid.New()}

	got, total, err := repo.GetCrackedHashesByClientIDs(ctx, clientIDs, CrackedHashParams{Limit: 10})
	require.NoError(t, err)
	require.Equal(t, int64(0), total)
	require.Len(t, got, 0)

	got, total, err = repo.GetCrackedHashesByClientIDs(ctx, clientIDs, CrackedHashParams{Limit: 10, Search: "abc"})
	require.NoError(t, err)
	require.Equal(t, int64(0), total)
	require.Len(t, got, 0)
}
