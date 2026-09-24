package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// longKerberoastHash builds a mode-13100 style hash whose edata alone is well
// past the 2704-byte btree row limit that broke GH #88 (~4316 chars there).
func longKerberoastHash(seed string) string {
	edata := strings.Repeat(seed, 4400/len(seed)+1)[:4400]
	return "$krb5tgs$23$*svc_sql$CORP.LOCAL$MSSQLSvc/db01.corp.local*$" +
		"0123456789abcdef0123456789abcdef$" + edata
}

func TestBulkImportHashes_LongHash(t *testing.T) {
	database := testutil.SetupTestDB(t)
	hashRepo := NewHashRepository(database)
	hashlistRepo := NewHashListRepository(database)
	ctx := context.Background()

	user := testutil.CreateTestUser(t, database, "longhash", "longhash@example.com", testutil.DefaultTestPassword, "user")

	newHashlist := func(name string) *models.HashList {
		hl := &models.HashList{
			Name:       name,
			UserID:     user.ID,
			ClientID:   uuid.Nil,
			HashTypeID: 13100,
			Status:     models.HashListStatusUploading,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		require.NoError(t, hashlistRepo.Create(ctx, hl))
		return hl
	}

	long := longKerberoastHash("a1b2c3d4e5f6")
	other := longKerberoastHash("f6e5d4c3b2a1")
	require.Greater(t, len(long), 2704)

	mk := func(v string) *models.Hash {
		return &models.Hash{HashValue: v, OriginalHash: v, HashTypeID: 13100}
	}

	first := newHashlist("long hashes")
	res, err := hashRepo.BulkImportHashes(ctx, []*models.Hash{mk(long), mk(other), mk(long)}, first.ID)
	require.NoError(t, err, "long hashes must import (GH #88)")
	assert.EqualValues(t, 2, res.NewHashes, "in-batch duplicate is collapsed")
	assert.EqualValues(t, 2, res.Associations)

	// Re-importing the same hash into another hashlist dedups on the md5 unique
	// index and still associates the existing row.
	second := newHashlist("long hashes again")
	res, err = hashRepo.BulkImportHashes(ctx, []*models.Hash{mk(long)}, second.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 0, res.NewHashes)
	assert.EqualValues(t, 1, res.Associations)

	got, err := hashRepo.GetByHashValues(ctx, []string{long, "not-present"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, long, got[0].HashValue, "lookup round-trips the exact text")

	search, err := hashRepo.SearchHashes(ctx, []string{long}, user.ID)
	require.NoError(t, err)
	require.Len(t, search, 1)
	assert.Len(t, search[0].Hashlists, 2)

	containing, err := hashlistRepo.GetHashlistsContainingHashes(ctx, []string{other})
	require.NoError(t, err)
	require.Len(t, containing, 1)
	assert.Equal(t, first.ID, containing[0].ID)

	uncracked, err := hashRepo.GetUncrackedHashValuesByHashlistID(ctx, first.ID)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{long, other}, uncracked)

	// Crack one inside a transaction via the row-locking lookup.
	tx, err := database.Begin()
	require.NoError(t, err)
	locked, err := hashRepo.GetByHashValueForUpdate(tx, long)
	require.NoError(t, err)
	require.NotNil(t, locked)
	require.NoError(t, hashRepo.UpdateCrackStatus(tx, locked.ID, "Summer2026!", time.Now(), nil))
	require.NoError(t, tx.Commit())

	uncracked, err = hashRepo.GetUncrackedHashValuesByHashlistID(ctx, first.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{other}, uncracked)

	// A cracked re-import of the other long hash updates the existing row.
	cracked := mk(other)
	pw := "Winter2026!"
	cracked.IsCracked = true
	cracked.Password = &pw
	res, err = hashRepo.BulkImportHashes(ctx, []*models.Hash{cracked}, second.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 0, res.NewHashes)
	assert.EqualValues(t, 1, res.UpdatedHashes)
}
