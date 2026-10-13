package repository

import (
	"context"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashlistRepository_Create_WithExcludeFromPotfile(t *testing.T) {
	db := testutil.SetupTestDB(t)
	repo := NewHashListRepository(db)
	ctx := context.Background()

	// Create a test user for the hashlist
	user := testutil.CreateTestUser(t, db, "testuser", "test@example.com", testutil.DefaultTestPassword, "user")

	tests := []struct {
		name               string
		excludeFromPotfile bool
		description        string
	}{
		{
			name:               "create hashlist with exclusion enabled",
			excludeFromPotfile: true,
			description:        "Should persist exclude_from_potfile as true",
		},
		{
			name:               "create hashlist with exclusion disabled",
			excludeFromPotfile: false,
			description:        "Should persist exclude_from_potfile as false (default)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hashlist := &models.HashList{
				Name:               "Test Hashlist - " + tt.name,
				UserID:             user.ID,
				ClientID:           uuid.Nil,
				HashTypeID:         1000, // MD5
				Status:             models.HashListStatusUploading,
				ExcludeFromPotfile: tt.excludeFromPotfile,
				CreatedAt:          time.Now(),
				UpdatedAt:          time.Now(),
			}

			// Create the hashlist
			err := repo.Create(ctx, hashlist)
			require.NoError(t, err, "Failed to create hashlist")
			assert.NotZero(t, hashlist.ID, "Hashlist ID should be set after creation")

			// Retrieve the hashlist to verify exclusion flag was persisted
			retrieved, err := repo.GetByID(ctx, hashlist.ID)
			require.NoError(t, err, "Failed to retrieve created hashlist")
			assert.Equal(t, tt.excludeFromPotfile, retrieved.ExcludeFromPotfile,
				"ExcludeFromPotfile should match the created value")
		})
	}
}

func TestHashlistRepository_GetByID_WithExcludeFromPotfile(t *testing.T) {
	db := testutil.SetupTestDB(t)
	repo := NewHashListRepository(db)
	ctx := context.Background()

	// Create a test user
	user := testutil.CreateTestUser(t, db, "testuser", "test@example.com", testutil.DefaultTestPassword, "user")

	// Create hashlists with different exclusion settings
	hashlistExcluded := &models.HashList{
		Name:               "Excluded Hashlist",
		UserID:             user.ID,
		ClientID:           uuid.Nil,
		HashTypeID:         1000,
		Status:             models.HashListStatusReady,
		ExcludeFromPotfile: true,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	err := repo.Create(ctx, hashlistExcluded)
	require.NoError(t, err)

	hashlistIncluded := &models.HashList{
		Name:               "Included Hashlist",
		UserID:             user.ID,
		ClientID:           uuid.Nil,
		HashTypeID:         1000,
		Status:             models.HashListStatusReady,
		ExcludeFromPotfile: false,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	err = repo.Create(ctx, hashlistIncluded)
	require.NoError(t, err)

	tests := []struct {
		name             string
		hashlistID       int64
		expectedExcluded bool
		description      string
	}{
		{
			name:             "retrieve excluded hashlist",
			hashlistID:       hashlistExcluded.ID,
			expectedExcluded: true,
			description:      "Should return true for excluded hashlist",
		},
		{
			name:             "retrieve included hashlist",
			hashlistID:       hashlistIncluded.ID,
			expectedExcluded: false,
			description:      "Should return false for non-excluded hashlist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retrieved, err := repo.GetByID(ctx, tt.hashlistID)
			require.NoError(t, err, "Failed to retrieve hashlist")
			assert.Equal(t, tt.expectedExcluded, retrieved.ExcludeFromPotfile,
				"ExcludeFromPotfile should match expected value")
		})
	}
}

func TestHashlistRepository_List_WithExcludeFromPotfile(t *testing.T) {
	db := testutil.SetupTestDB(t)
	repo := NewHashListRepository(db)
	ctx := context.Background()

	// Create a test user
	user := testutil.CreateTestUser(t, db, "testuser", "test@example.com", testutil.DefaultTestPassword, "user")

	// Create multiple hashlists with different exclusion settings
	hashlists := []*models.HashList{
		{
			Name:               "Excluded List 1",
			UserID:             user.ID,
			ClientID:           uuid.Nil,
			HashTypeID:         1000,
			Status:             models.HashListStatusReady,
			ExcludeFromPotfile: true,
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
		},
		{
			Name:               "Included List 1",
			UserID:             user.ID,
			ClientID:           uuid.Nil,
			HashTypeID:         1000,
			Status:             models.HashListStatusReady,
			ExcludeFromPotfile: false,
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
		},
		{
			Name:               "Excluded List 2",
			UserID:             user.ID,
			ClientID:           uuid.Nil,
			HashTypeID:         1000,
			Status:             models.HashListStatusReady,
			ExcludeFromPotfile: true,
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
		},
	}

	for _, hashlist := range hashlists {
		err := repo.Create(ctx, hashlist)
		require.NoError(t, err, "Failed to create hashlist")
	}

	// List all hashlists for the user
	params := ListHashlistsParams{
		UserID: &user.ID,
		Limit:  10,
		Offset: 0,
	}

	retrieved, totalCount, err := repo.List(ctx, params)
	require.NoError(t, err, "Failed to list hashlists")
	assert.Equal(t, 3, totalCount, "Should have 3 hashlists")
	assert.Len(t, retrieved, 3, "Should retrieve 3 hashlists")

	// Verify each hashlist has the correct exclusion flag
	excludedCount := 0
	includedCount := 0
	for _, hashlist := range retrieved {
		assert.NotNil(t, hashlist, "Hashlist should not be nil")
		if hashlist.ExcludeFromPotfile {
			excludedCount++
		} else {
			includedCount++
		}
	}

	assert.Equal(t, 2, excludedCount, "Should have 2 excluded hashlists")
	assert.Equal(t, 1, includedCount, "Should have 1 included hashlist")
}

func TestHashlistRepository_IsExcludedFromPotfile(t *testing.T) {
	db := testutil.SetupTestDB(t)
	repo := NewHashListRepository(db)
	ctx := context.Background()

	// Create a test user
	user := testutil.CreateTestUser(t, db, "testuser", "test@example.com", testutil.DefaultTestPassword, "user")

	// Create test hashlists
	excludedHashlist := &models.HashList{
		Name:               "Excluded Hashlist",
		UserID:             user.ID,
		ClientID:           uuid.Nil,
		HashTypeID:         1000,
		Status:             models.HashListStatusReady,
		ExcludeFromPotfile: true,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	err := repo.Create(ctx, excludedHashlist)
	require.NoError(t, err)

	includedHashlist := &models.HashList{
		Name:               "Included Hashlist",
		UserID:             user.ID,
		ClientID:           uuid.Nil,
		HashTypeID:         1000,
		Status:             models.HashListStatusReady,
		ExcludeFromPotfile: false,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	err = repo.Create(ctx, includedHashlist)
	require.NoError(t, err)

	tests := []struct {
		name          string
		hashlistID    int64
		expectedValue bool
		expectError   bool
		description   string
	}{
		{
			name:          "excluded hashlist returns true",
			hashlistID:    excludedHashlist.ID,
			expectedValue: true,
			expectError:   false,
			description:   "Should return true for excluded hashlist",
		},
		{
			name:          "included hashlist returns false",
			hashlistID:    includedHashlist.ID,
			expectedValue: false,
			expectError:   false,
			description:   "Should return false for non-excluded hashlist",
		},
		{
			name:          "non-existent hashlist returns error",
			hashlistID:    999999,
			expectedValue: false,
			expectError:   true,
			description:   "Should return error for non-existent hashlist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			excluded, err := repo.IsExcludedFromPotfile(ctx, tt.hashlistID)

			if tt.expectError {
				assert.Error(t, err, "Should return error for non-existent hashlist")
			} else {
				assert.NoError(t, err, "Should not return error")
				assert.Equal(t, tt.expectedValue, excluded,
					"Exclusion status should match expected value")
			}
		})
	}
}

func TestHashlistRepository_IsExcludedFromPotfile_PerformanceCheck(t *testing.T) {
	db := testutil.SetupTestDB(t)
	repo := NewHashListRepository(db)
	ctx := context.Background()

	// Create a test user
	user := testutil.CreateTestUser(t, db, "testuser", "test@example.com", testutil.DefaultTestPassword, "user")

	// Create a test hashlist
	hashlist := &models.HashList{
		Name:               "Performance Test Hashlist",
		UserID:             user.ID,
		ClientID:           uuid.Nil,
		HashTypeID:         1000,
		Status:             models.HashListStatusReady,
		ExcludeFromPotfile: true,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	err := repo.Create(ctx, hashlist)
	require.NoError(t, err)

	// Measure performance of IsExcludedFromPotfile
	// This query should be very fast as it only fetches one boolean column
	start := time.Now()
	iterations := 100

	for i := 0; i < iterations; i++ {
		excluded, err := repo.IsExcludedFromPotfile(ctx, hashlist.ID)
		require.NoError(t, err)
		assert.True(t, excluded)
	}

	duration := time.Since(start)
	avgDuration := duration / time.Duration(iterations)

	// Each query should complete in less than 10ms on average
	assert.Less(t, avgDuration, 10*time.Millisecond,
		"IsExcludedFromPotfile should be fast (avg: %v)", avgDuration)

	t.Logf("IsExcludedFromPotfile average execution time: %v (%d iterations)", avgDuration, iterations)
}

// TestHashlistRepository_ListWithTeamFilter_EmptyReturnsNonNilSlice guards issue #114:
// a teams-enabled list for a team with no matching (or only-archived) hashlists must
// return a non-nil empty slice so the JSON response is [] and not null. A null `data`
// crashes the frontend live-query predicate and the Hashlist view errors out instead
// of rendering an empty table.
func TestHashlistRepository_ListWithTeamFilter_EmptyReturnsNonNilSlice(t *testing.T) {
	db := testutil.SetupTestDB(t)
	repo := NewHashListRepository(db)
	ctx := context.Background()

	// Teams enabled, scoped to a team that has no client_teams mappings → zero rows.
	params := ListHashlistsParams{
		Limit:        50,
		Offset:       0,
		TeamsEnabled: true,
		TeamIDs:      []uuid.UUID{uuid.New()},
	}

	hashlists, total, err := repo.ListWithTeamFilter(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, int64(0), total, "expected zero hashlists for a team with no mappings")
	assert.NotNil(t, hashlists, "must be a non-nil slice so JSON marshals to [] not null (issue #114)")
	assert.Len(t, hashlists, 0)
}
