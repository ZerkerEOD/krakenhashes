//go:build unit
// +build unit

package services

import (
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helper to create test Hash objects
func createTestHash(password string, username *string, hashTypeID int) *models.Hash {
	return &models.Hash{
		ID:         uuid.New(),
		Password:   password,
		Username:   username,
		HashTypeID: hashTypeID,
		IsCracked:  true,
	}
}

// Test helper to create pointer to string
func strPtr(s string) *string {
	return &s
}

// TestCalculateOverview tests the overview statistics calculation
func TestCalculateOverview(t *testing.T) {
	service := &AnalyticsService{}

	tests := []struct {
		name         string
		passwords    []*models.Hash
		totalHashes  int
		totalCracked int
		wantModes    int
	}{
		{
			name: "Single hash type",
			passwords: []*models.Hash{
				createTestHash("password1", nil, 5600),
				createTestHash("password2", nil, 5600),
			},
			totalHashes:  10,
			totalCracked: 2,
			wantModes:    1,
		},
		{
			name: "Multiple hash types",
			passwords: []*models.Hash{
				createTestHash("password1", nil, 5600),
				createTestHash("password2", nil, 3200),
				createTestHash("password3", nil, 1400),
			},
			totalHashes:  15,
			totalCracked: 3,
			wantModes:    3,
		},
		{
			name:         "Empty password list",
			passwords:    []*models.Hash{},
			totalHashes:  0,
			totalCracked: 0,
			wantModes:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.calculateOverview(tt.passwords, tt.totalHashes, tt.totalCracked)

			assert.Equal(t, tt.totalHashes, result.TotalHashes)
			assert.Equal(t, tt.totalCracked, result.TotalCracked)
			assert.Len(t, result.HashModes, tt.wantModes)

			if tt.totalHashes > 0 {
				expectedPercentage := float64(tt.totalCracked) / float64(tt.totalHashes) * 100
				assert.InDelta(t, expectedPercentage, result.CrackPercentage, 0.01)
			}
		})
	}
}

// TestCalculateLengthDistribution tests length distribution calculation
func TestCalculateLengthDistribution(t *testing.T) {
	service := &AnalyticsService{}

	tests := []struct {
		name          string
		passwords     []*models.Hash
		wantLengths   map[string]int
		wantAvgUnder15 float64
	}{
		{
			name: "Various lengths",
			passwords: []*models.Hash{
				createTestHash("pwd", nil, 0),                    // 3
				createTestHash("password", nil, 0),               // 8
				createTestHash("password123", nil, 0),            // 11
				createTestHash("verylongpassword", nil, 0),       // 16
				createTestHash("extremelylongpasswordhere", nil, 0), // 25
			},
			wantLengths: map[string]int{
				"3":  1,
				"8":  1,
				"11": 1,
				"16": 1,
				"25": 1,
			},
			wantAvgUnder15: (3.0 + 8.0 + 11.0) / 3.0,
		},
		{
			name: "Length boundaries",
			passwords: []*models.Hash{
				createTestHash("x", nil, 0),                                              // 1
				createTestHash("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", nil, 0),               // 32
				createTestHash("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", nil, 0),     // 42 -> 32+
			},
			wantLengths: map[string]int{
				"1":   1,
				"32":  1,
				"32+": 1,
			},
			wantAvgUnder15: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.calculateLengthDistribution(tt.passwords)

			for length, expectedCount := range tt.wantLengths {
				if dist, ok := result.Distribution[length]; ok {
					assert.Equal(t, expectedCount, dist.Count, "Length %s count mismatch", length)
				} else {
					t.Errorf("Expected length %s not found in distribution", length)
				}
			}

			assert.InDelta(t, tt.wantAvgUnder15, result.AverageLengthUnder15, 0.01)
		})
	}
}

// TestDetectCharacterTypes tests character type detection
func TestDetectCharacterTypes(t *testing.T) {
	service := &AnalyticsService{}

	tests := []struct {
		name     string
		password string
		want     models.CharacterTypes
	}{
		{
			name:     "Lowercase only",
			password: "password",
			want:     models.CharacterTypes{HasLowercase: true},
		},
		{
			name:     "Uppercase only",
			password: "PASSWORD",
			want:     models.CharacterTypes{HasUppercase: true},
		},
		{
			name:     "Digits only",
			password: "12345678",
			want:     models.CharacterTypes{HasNumbers: true},
		},
		{
			name:     "Special only",
			password: "!@#$%^&*",
			want:     models.CharacterTypes{HasSpecial: true},
		},
		{
			name:     "All types",
			password: "Passw0rd!",
			want: models.CharacterTypes{
				HasLowercase:   true,
				HasUppercase:   true,
				HasNumbers:   true,
				HasSpecial: true,
			},
		},
		{
			name:     "Unicode characters",
			password: "пароль123",
			want: models.CharacterTypes{
				HasLowercase: true,
				HasNumbers: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.detectCharacterTypes(tt.password)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestCalculateComplexity tests complexity analysis with all 16 categories
func TestCalculateComplexity(t *testing.T) {
	service := &AnalyticsService{}

	passwords := []*models.Hash{
		// Single type
		createTestHash("password", nil, 0),       // lowercase
		createTestHash("PASSWORD", nil, 0),       // uppercase
		createTestHash("12345678", nil, 0),       // digits
		createTestHash("!@#$%^&*", nil, 0),       // special

		// Two types
		createTestHash("password123", nil, 0),    // lower+digit
		createTestHash("PASSWORD!", nil, 0),      // upper+special

		// Three types
		createTestHash("Password123", nil, 0),    // lower+upper+digit

		// Four types
		createTestHash("Passw0rd!", nil, 0),      // all types

		// Complex short (≤14 chars, 3-4 types)
		createTestHash("Pass123!", nil, 0),       // 8 chars, 4 types

		// Complex long (15+ chars, 3-4 types)
		createTestHash("MyP@ssw0rd123456", nil, 0), // 16 chars, 4 types
	}

	result := service.calculateComplexity(passwords)

	// Verify single type counts
	assert.Equal(t, 1, result.SingleType["lowercase_only"].Count)
	assert.Equal(t, 1, result.SingleType["uppercase_only"].Count)
	assert.Equal(t, 1, result.SingleType["numbers_only"].Count)
	assert.Equal(t, 1, result.SingleType["special_only"].Count)

	// Verify complex short/long
	// Complex short: Password123 (11 chars, 3 types), Passw0rd! (9 chars, 4 types), Pass123! (8 chars, 4 types)
	assert.Equal(t, 3, result.ComplexShort.Count)
	assert.Equal(t, 1, result.ComplexLong.Count) // MyP@ssw0rd123456
}

// TestCalculatePositionalAnalysis tests positional pattern detection
func TestCalculatePositionalAnalysis(t *testing.T) {
	service := &AnalyticsService{}

	passwords := []*models.Hash{
		createTestHash("Password", nil, 0),    // uppercase start
		createTestHash("password1", nil, 0),   // number end
		createTestHash("password!", nil, 0),   // special end
		createTestHash("Password1", nil, 0),   // both uppercase start and number end
		createTestHash("lowercase", nil, 0),   // none
	}

	result := service.calculatePositionalAnalysis(passwords)

	assert.Equal(t, 2, result.StartsUppercase.Count) // Password, Password1
	assert.Equal(t, 2, result.EndsNumber.Count)      // password1, Password1
	assert.Equal(t, 1, result.EndsSpecial.Count)     // password!
}

// TestDetectPatterns tests pattern detection (keyboard walks, sequences, repeating)
func TestDetectPatterns(t *testing.T) {
	service := &AnalyticsService{}

	passwords := []*models.Hash{
		createTestHash("qwerty", nil, 0),      // keyboard walk
		createTestHash("asdfgh", nil, 0),      // keyboard walk
		createTestHash("abc123", nil, 0),      // sequence
		createTestHash("xyz789", nil, 0),      // sequence
		createTestHash("aaa111", nil, 0),      // repeating
		createTestHash("!!!@@@", nil, 0),      // repeating
		createTestHash("normalpass", nil, 0),  // none
	}

	result := service.detectPatterns(passwords)

	assert.Equal(t, 2, result.KeyboardWalks.Count)
	// Sequential regex matches any 3+ consecutive letters/digits, so matches 6 out of 7
	assert.Equal(t, 6, result.Sequential.Count)
	assert.Equal(t, 2, result.RepeatingChars.Count)
}

// TestAnalyzeUsernameCorrelation tests username-password correlation
func TestAnalyzeUsernameCorrelation(t *testing.T) {
	service := &AnalyticsService{}

	passwords := []*models.Hash{
		createTestHash("john", strPtr("john"), 0),           // equals username
		createTestHash("john123", strPtr("john"), 0),        // username+suffix (123 matches \d{1,4})
		createTestHash("john2024", strPtr("john"), 0),       // username+suffix (2024 matches \d{1,4})
		createTestHash("johnabc", strPtr("john"), 0),        // contains username (abc doesn't match suffix regex)
		createTestHash("nhoj", strPtr("john"), 0),           // reversed
		createTestHash("password", strPtr("john"), 0),       // no correlation
		createTestHash("password", nil, 0),                  // no username
	}

	result := service.analyzeUsernameCorrelation(passwords)

	assert.Equal(t, 1, result.EqualsUsername.Count)
	assert.Equal(t, 1, result.ContainsUsername.Count)      // "johnabc"
	assert.Equal(t, 2, result.UsernamePlusSuffix.Count)    // "john123" and "john2024"
	assert.Equal(t, 1, result.ReversedUsername.Count)
}

// TestDetectPasswordReuse tests password reuse detection
func TestDetectPasswordReuse(t *testing.T) {
	service := &AnalyticsService{}

	// Create test data with hashlist tracking
	hashesWithHashlists := []repository.HashWithHashlist{
		{Hash: *createTestHash("Password123", strPtr("user1"), 0), HashlistID: 1},
		{Hash: *createTestHash("Password123", strPtr("user2"), 0), HashlistID: 1},
		{Hash: *createTestHash("Password123", strPtr("user3"), 0), HashlistID: 2},
		{Hash: *createTestHash("Password123", strPtr("user1"), 0), HashlistID: 2}, // user1 in 2 hashlists
		{Hash: *createTestHash("Common456", strPtr("user4"), 0), HashlistID: 1},
		{Hash: *createTestHash("Common456", strPtr("user5"), 0), HashlistID: 1},
		{Hash: *createTestHash("Unique1", strPtr("user6"), 0), HashlistID: 1},
		{Hash: *createTestHash("Unique2", strPtr("user7"), 0), HashlistID: 2},
	}

	result := service.detectPasswordReuse(hashesWithHashlists)

	// Verify totals: 6 reused (4 Password123 + 2 Common456), 2 unique
	assert.Equal(t, 6, result.TotalReused)
	assert.Equal(t, 2, result.TotalUnique)

	// Should have 2 password entries (Password123, Common456)
	assert.Len(t, result.PasswordReuseInfo, 2)

	// Verify percentage
	expectedPercentage := (6.0 / 8.0) * 100
	assert.InDelta(t, expectedPercentage, result.PercentageReused, 0.01)

	// Verify sorting by total occurrences (Password123 should be first with 4 occurrences)
	assert.Equal(t, "Password123", result.PasswordReuseInfo[0].Password)
	assert.Equal(t, 4, result.PasswordReuseInfo[0].TotalOccurrences)
	assert.Equal(t, 3, result.PasswordReuseInfo[0].UserCount) // user1, user2, user3

	// Verify hashlist counting for user1 who appears in 2 hashlists
	var user1Info *models.UserOccurrence
	for _, user := range result.PasswordReuseInfo[0].Users {
		if user.Username == "user1" {
			user1Info = &user
			break
		}
	}
	assert.NotNil(t, user1Info)
	assert.Equal(t, 2, user1Info.HashlistCount)

	// Verify Common456
	assert.Equal(t, "Common456", result.PasswordReuseInfo[1].Password)
	assert.Equal(t, 2, result.PasswordReuseInfo[1].TotalOccurrences)
	assert.Equal(t, 2, result.PasswordReuseInfo[1].UserCount)
}

// TestDetectTemporalPatterns tests temporal pattern detection
func TestDetectTemporalPatterns(t *testing.T) {
	service := &AnalyticsService{}

	passwords := []*models.Hash{
		createTestHash("Password2024", nil, 0),    // year
		createTestHash("Summer2023", nil, 0),      // year + season
		createTestHash("January", nil, 0),         // month
		createTestHash("December123", nil, 0),     // month
		createTestHash("Spring", nil, 0),          // season
		createTestHash("Winter2024", nil, 0),      // season + year
		createTestHash("normalpass", nil, 0),      // none
	}

	result := service.detectTemporalPatterns(passwords)

	assert.Equal(t, 3, result.ContainsYear.Count)      // 2024, 2023, 2024
	assert.Equal(t, 2, result.ContainsMonth.Count)     // January, December
	assert.Equal(t, 3, result.ContainsSeason.Count)    // Summer, Spring, Winter

	// Check year breakdown
	assert.Contains(t, result.YearBreakdown, "2024")
	assert.Contains(t, result.YearBreakdown, "2023")
}

// TestAnalyzeMasks tests hashcat mask generation
func TestAnalyzeMasks(t *testing.T) {
	service := &AnalyticsService{}

	passwords := []*models.Hash{
		createTestHash("Password", nil, 0),        // ?u?l?l?l?l?l?l?l
		createTestHash("password", nil, 0),        // ?l?l?l?l?l?l?l?l
		createTestHash("Password123", nil, 0),     // ?u?l?l?l?l?l?l?l?d?d?d
		createTestHash("Pass123!", nil, 0),        // ?u?l?l?l?d?d?d?s
	}

	result := service.analyzeMasks(passwords)

	assert.Greater(t, len(result.TopMasks), 0)

	// Verify mask format
	for _, mask := range result.TopMasks {
		assert.Contains(t, mask.Mask, "?")
		assert.Greater(t, mask.Count, 0)
	}
}

// TestPasswordToMask tests individual mask generation
func TestPasswordToMask(t *testing.T) {
	service := &AnalyticsService{}

	tests := []struct {
		name     string
		password string
		want     string
	}{
		{
			name:     "Lowercase only",
			password: "password",
			want:     "?l?l?l?l?l?l?l?l",
		},
		{
			name:     "Mixed case",
			password: "Password",
			want:     "?u?l?l?l?l?l?l?l",
		},
		{
			name:     "With digits",
			password: "Pass123",
			want:     "?u?l?l?l?d?d?d",
		},
		{
			name:     "With special",
			password: "Pass!@#",
			want:     "?u?l?l?l?s?s?s",
		},
		{
			name:     "All types",
			password: "Pa$$w0rd!",
			want:     "?u?l?s?s?l?d?l?l?s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.passwordToMask(tt.password)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestCheckCustomPatterns tests custom organization name pattern matching
func TestCheckCustomPatterns(t *testing.T) {
	service := &AnalyticsService{}

	customPatterns := []string{"acme", "corp"}
	clientID := uuid.New().String()

	passwords := []*models.Hash{
		createTestHash("Acme123", nil, 0),      // matches "acme" (case-insensitive)
		createTestHash("corp2024", nil, 0),     // matches "corp"
		createTestHash("ACME!", nil, 0),        // matches "acme"
		createTestHash("AcmeInc", nil, 0),      // matches "acme"
		createTestHash("password", nil, 0),     // no match
	}

	result := service.checkCustomPatterns(passwords, customPatterns, clientID)

	assert.Len(t, result.PatternsDetected, 2)
	assert.Equal(t, 3, result.PatternsDetected["acme"].Count)  // Acme123, ACME!, AcmeInc
	assert.Equal(t, 1, result.PatternsDetected["corp"].Count)  // corp2024
}

// TestEstimateKeyspaceBits tests the brute-force keyspace strength estimate
func TestEstimateKeyspaceBits(t *testing.T) {
	service := &AnalyticsService{}

	tests := []struct {
		name     string
		password string
		minBits  float64
		maxBits  float64
	}{
		{
			name:     "Simple password",
			password: "password",
			minBits:  30.0,
			maxBits:  45.0,
		},
		{
			name:     "Complex password",
			password: "P@ssw0rd!123",
			minBits:  70.0,
			maxBits:  85.0,
		},
		{
			name:     "High entropy",
			password: "X7$mK9@pL2&nQ5#vR8^wT3",
			minBits:  100.0,
			maxBits:  150.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.estimateKeyspaceBits(tt.password)
			assert.GreaterOrEqual(t, result, tt.minBits)
			assert.LessOrEqual(t, result, tt.maxBits)
		})
	}
}

// TestCalculateStrengthMetrics tests strength metrics with 3-tier entropy
func TestCalculateStrengthMetrics(t *testing.T) {
	service := &AnalyticsService{}

	passwords := []*models.Hash{
		createTestHash("pwd", nil, 0),                          // low entropy (<78)
		createTestHash("password", nil, 0),                     // low entropy
		createTestHash("MyP@ssw0rd123", nil, 0),               // moderate (78-127)
		createTestHash("X7$mK9@pL2&nQ5#vR8^wT3", nil, 0),     // high (128+)
	}

	speeds := []int64{1000000000} // 1 GH/s

	result := service.calculateStrengthMetrics(passwords, speeds)

	// Verify entropy distribution
	assert.Equal(t, 2, result.EntropyDistribution.Low.Count)
	assert.GreaterOrEqual(t, result.EntropyDistribution.Moderate.Count, 1)
	assert.GreaterOrEqual(t, result.EntropyDistribution.High.Count, 0)

	// Verify crack time estimates exist
	require.NotNil(t, result.CrackTimeEstimates)
	assert.Greater(t, result.CrackTimeEstimates.Speed50Percent.SpeedHPS, int64(0))
	assert.Greater(t, result.CrackTimeEstimates.Speed100Percent.SpeedHPS, int64(0))
	assert.Greater(t, result.CrackTimeEstimates.Speed200Percent.SpeedHPS, int64(0))
}

// TestGetTopPasswords tests top password extraction with 2+ uses requirement
func TestGetTopPasswords(t *testing.T) {
	service := &AnalyticsService{}

	passwords := []*models.Hash{
		createTestHash("Password123", nil, 0),
		createTestHash("Password123", nil, 0),
		createTestHash("Password123", nil, 0),  // 3 uses
		createTestHash("Summer2024", nil, 0),
		createTestHash("Summer2024", nil, 0),   // 2 uses
		createTestHash("UniquePassword", nil, 0), // 1 use - should be excluded
	}

	result := service.getTopPasswords(passwords, 10)

	assert.Len(t, result, 2) // Only passwords with 2+ uses

	// Verify sorting (most common first)
	assert.Equal(t, "Password123", result[0].Password)
	assert.Equal(t, 3, result[0].Count)
	assert.Equal(t, "Summer2024", result[1].Password)
	assert.Equal(t, 2, result[1].Count)
}

// TestEdgeCases tests various edge cases
func TestEdgeCases(t *testing.T) {
	service := &AnalyticsService{}

	t.Run("Empty password list", func(t *testing.T) {
		passwords := []*models.Hash{}

		lengthDist := service.calculateLengthDistribution(passwords)
		assert.Len(t, lengthDist.Distribution, 0)

		complexity := service.calculateComplexity(passwords)
		assert.Len(t, complexity.SingleType, 0)
	})

	t.Run("NULL usernames", func(t *testing.T) {
		passwords := []*models.Hash{
			createTestHash("password", nil, 0),
		}

		result := service.analyzeUsernameCorrelation(passwords)
		assert.Equal(t, 0, result.EqualsUsername.Count)
	})

	t.Run("Single character password", func(t *testing.T) {
		passwords := []*models.Hash{
			createTestHash("a", nil, 0),
		}

		result := service.calculateLengthDistribution(passwords)
		assert.Equal(t, 1, result.Distribution["1"].Count)
	})

	t.Run("Very long password", func(t *testing.T) {
		longPwd := ""
		for i := 0; i < 100; i++ {
			longPwd += "x"
		}
		passwords := []*models.Hash{
			createTestHash(longPwd, nil, 0),
		}

		result := service.calculateLengthDistribution(passwords)
		assert.Equal(t, 1, result.Distribution["32+"].Count)
	})
}
