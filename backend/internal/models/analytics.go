package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// AnalyticsReport represents a password analytics report for a client engagement
type AnalyticsReport struct {
	ID             uuid.UUID      `json:"id"`
	ClientID       uuid.UUID      `json:"client_id"`
	UserID         uuid.UUID      `json:"user_id"`
	StartDate      time.Time      `json:"start_date"`
	EndDate        time.Time      `json:"end_date"`
	Status         string         `json:"status"` // queued, processing, completed, failed
	AnalyticsData  *AnalyticsData `json:"analytics_data"`
	TotalHashlists int            `json:"total_hashlists"`
	TotalHashes    int            `json:"total_hashes"`
	TotalCracked   int            `json:"total_cracked"`
	QueuePosition  *int           `json:"queue_position"`
	CustomPatterns pq.StringArray `json:"custom_patterns" db:"custom_patterns"`
	CreatedAt      time.Time      `json:"created_at"`
	StartedAt      *time.Time     `json:"started_at"`
	CompletedAt    *time.Time     `json:"completed_at"`
	ErrorMessage   *string        `json:"error_message"`
}

// AnalyticsData contains all calculated analytics metrics
type AnalyticsData struct {
	Overview            OverviewStats       `json:"overview"`
	WindowsHashes       *WindowsHashStats   `json:"windows_hashes,omitempty"`
	LengthDistribution  LengthStats         `json:"length_distribution"`
	ComplexityAnalysis  ComplexityStats     `json:"complexity_analysis"`
	PositionalAnalysis  PositionalStats     `json:"positional_analysis"`
	PatternDetection    PatternStats        `json:"pattern_detection"`
	UsernameCorrelation UsernameStats       `json:"username_correlation"`
	PasswordReuse       ReuseStats          `json:"password_reuse"`
	HashReuse           HashReuseStats      `json:"hash_reuse"`
	TemporalPatterns    TemporalStats       `json:"temporal_patterns"`
	MaskAnalysis        MaskStats           `json:"mask_analysis"`
	CustomPatterns      CustomPatternStats  `json:"custom_patterns"`
	StrengthMetrics     StrengthStats       `json:"strength_metrics"`
	TopPasswords        []TopPassword       `json:"top_passwords"`
	LMPartialCracks     *LMPartialCrackStats `json:"lm_partial_cracks,omitempty"`
	LMToNTLMMasks       *LMToNTLMMaskStats   `json:"lm_to_ntlm_masks,omitempty"`
	Recommendations     []Recommendation    `json:"recommendations"`
	DomainAnalytics     []DomainAnalytics   `json:"domain_analytics"`
}

// DomainAnalytics contains complete analytics for a specific domain
type DomainAnalytics struct {
	Domain              string                `json:"domain"`
	Overview            OverviewStats         `json:"overview"`
	WindowsHashes       *WindowsHashStats     `json:"windows_hashes,omitempty"`
	LengthDistribution  LengthStats           `json:"length_distribution"`
	ComplexityAnalysis  ComplexityStats       `json:"complexity_analysis"`
	PositionalAnalysis  PositionalStats       `json:"positional_analysis"`
	PatternDetection    PatternStats          `json:"pattern_detection"`
	UsernameCorrelation UsernameStats         `json:"username_correlation"`
	PasswordReuse       ReuseStats            `json:"password_reuse"`
	HashReuse           *HashReuseStats       `json:"hash_reuse,omitempty"`
	TemporalPatterns    TemporalStats         `json:"temporal_patterns"`
	MaskAnalysis        MaskStats             `json:"mask_analysis"`
	CustomPatterns      CustomPatternStats    `json:"custom_patterns"`
	StrengthMetrics     StrengthStats         `json:"strength_metrics"`
	TopPasswords        []TopPassword         `json:"top_passwords"`
	LMPartialCracks     *LMPartialCrackStats  `json:"lm_partial_cracks,omitempty"`
	LMToNTLMMasks       *LMToNTLMMaskStats    `json:"lm_to_ntlm_masks,omitempty"`
}

// Scan implements sql.Scanner for AnalyticsData
func (a *AnalyticsData) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return json.Unmarshal(value.([]byte), a)
	}
	return json.Unmarshal(bytes, a)
}

// Value implements driver.Valuer for AnalyticsData
func (a AnalyticsData) Value() (driver.Value, error) {
	return json.Marshal(a)
}

// OverviewStats contains high-level statistics
type OverviewStats struct {
	TotalHashes     int              `json:"total_hashes"`
	TotalCracked    int              `json:"total_cracked"`
	CrackPercentage float64          `json:"crack_percentage"`
	HashModes       []HashModeStats  `json:"hash_modes"`
	DomainBreakdown []DomainStats    `json:"domain_breakdown"`
}

// HashModeStats contains statistics for a specific hash type
type HashModeStats struct {
	ModeID     int     `json:"mode_id"`
	ModeName   string  `json:"mode_name"`
	Total      int     `json:"total"`
	Cracked    int     `json:"cracked"`
	Percentage float64 `json:"percentage"`
}

// DomainStats contains statistics for a specific domain
type DomainStats struct {
	Domain          string  `json:"domain"`
	TotalHashes     int     `json:"total_hashes"`
	CrackedHashes   int     `json:"cracked_hashes"`
	CrackPercentage float64 `json:"crack_percentage"`
}

// LengthStats contains password length distribution
type LengthStats struct {
	Distribution       map[string]CategoryCount `json:"distribution"` // "8": {count, percentage}
	AverageLength      float64                  `json:"average_length"`
	AverageLengthUnder15 float64                `json:"average_length_under_15"` // Average for passwords <15 chars
	MostCommonLengths  []int                    `json:"most_common_lengths"`
	CountUnder8        int                      `json:"count_under_8"`
	Count8to11         int                      `json:"count_8_to_11"`
	CountUnder15       int                      `json:"count_under_15"`
}

// ComplexityStats contains character complexity analysis
type ComplexityStats struct {
	SingleType   map[string]CategoryCount `json:"single_type"`   // "lowercase_only": {count, percentage}
	TwoTypes     map[string]CategoryCount `json:"two_types"`     // "lowercase_uppercase": {count, percentage}
	ThreeTypes   map[string]CategoryCount `json:"three_types"`   // "lowercase_uppercase_numbers": {count, percentage}
	FourTypes    CategoryCount            `json:"four_types"`    // All 4 types
	ComplexShort CategoryCount            `json:"complex_short"` // 3-4 types, <=14 chars
	ComplexLong  CategoryCount            `json:"complex_long"`  // 3-4 types, 15+ chars
}

// CategoryCount represents a count and percentage for a category
type CategoryCount struct {
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
}

// PositionalStats contains positional analysis of password characteristics
type PositionalStats struct {
	StartsUppercase CategoryCount `json:"starts_uppercase"`
	EndsNumber      CategoryCount `json:"ends_number"`
	EndsSpecial     CategoryCount `json:"ends_special"`
}

// PatternStats contains detected patterns in passwords
type PatternStats struct {
	KeyboardWalks   CategoryCount            `json:"keyboard_walks"`
	Sequential      CategoryCount            `json:"sequential"`
	RepeatingChars  CategoryCount            `json:"repeating_chars"`
	CommonBaseWords map[string]CategoryCount `json:"common_base_words"` // "password": {count, percentage}
}

// UsernameStats contains username correlation analysis
type UsernameStats struct {
	EqualsUsername       CategoryCount `json:"equals_username"`
	ContainsUsername     CategoryCount `json:"contains_username"`
	UsernamePlusSuffix   CategoryCount `json:"username_plus_suffix"`
	ReversedUsername     CategoryCount `json:"reversed_username"`
}

// ReuseStats contains password reuse analysis
type ReuseStats struct {
	TotalReused       int                  `json:"total_reused"`
	PercentageReused  float64              `json:"percentage_reused"`
	TotalUnique       int                  `json:"total_unique"`
	PasswordReuseInfo []PasswordReuseInfo  `json:"password_reuse_info"` // List of reused passwords with user info
}

// PasswordReuseInfo contains information about a reused password and its users
type PasswordReuseInfo struct {
	Password         string           `json:"password"`          // The reused password
	Users            []UserOccurrence `json:"users"`             // List of users with occurrence counts
	TotalOccurrences int              `json:"total_occurrences"` // Sum of all occurrences across hashlists
	UserCount        int              `json:"user_count"`        // Total unique users using this password
}

// UserOccurrence tracks a user and how many hashlists they appear in with a specific password
type UserOccurrence struct {
	Username      string `json:"username"`        // Username
	HashlistCount int    `json:"hashlist_count"`  // How many different hashlists this user-password combo appears in
}

// TemporalStats contains temporal pattern analysis
type TemporalStats struct {
	ContainsYear   CategoryCount            `json:"contains_year"`
	ContainsMonth  CategoryCount            `json:"contains_month"`
	ContainsSeason CategoryCount            `json:"contains_season"`
	YearBreakdown  map[string]CategoryCount `json:"year_breakdown"` // "2024": {count, percentage}
}

// MaskStats contains hashcat-style mask analysis
type MaskStats struct {
	TopMasks []MaskInfo `json:"top_masks"`
}

// MaskInfo contains information about a password mask pattern
type MaskInfo struct {
	Mask       string  `json:"mask"`       // e.g., "?u?l?l?l?l?l?l?d?d"
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
	Example    string  `json:"example"` // Example password matching this mask
}

// CustomPatternStats contains custom organization pattern matching
type CustomPatternStats struct {
	PatternsDetected map[string]CategoryCount `json:"patterns_detected"` // "google": {count, percentage}
}

// StrengthStats contains password strength metrics
type StrengthStats struct {
	AverageSpeedHPS     int64               `json:"average_speed_hps"`
	EntropyDistribution EntropyDistribution `json:"entropy_distribution"`
	CrackTimeEstimates  CrackTimeEstimates  `json:"crack_time_estimates"`
}

// EntropyDistribution contains entropy classification (3-tier)
type EntropyDistribution struct {
	Low      CategoryCount `json:"low"`      // <78 bits
	Moderate CategoryCount `json:"moderate"` // 78-127 bits
	High     CategoryCount `json:"high"`     // 128+ bits
}

// CrackTimeEstimates contains crack time estimates at different speed levels
type CrackTimeEstimates struct {
	Speed50Percent  SpeedLevelEstimate `json:"speed_50_percent"`  // 50% of average
	Speed75Percent  SpeedLevelEstimate `json:"speed_75_percent"`  // 75% of average
	Speed100Percent SpeedLevelEstimate `json:"speed_100_percent"` // 100% of average (baseline)
	Speed150Percent SpeedLevelEstimate `json:"speed_150_percent"` // 150% of average
	Speed200Percent SpeedLevelEstimate `json:"speed_200_percent"` // 200% of average
}

// SpeedLevelEstimate contains crack time statistics for a specific speed level
type SpeedLevelEstimate struct {
	SpeedHPS            int64   `json:"speed_hps"`
	PercentUnder1Hour   float64 `json:"percent_under_1_hour"`
	PercentUnder1Day    float64 `json:"percent_under_1_day"`
	PercentUnder1Week   float64 `json:"percent_under_1_week"`
	PercentUnder1Month  float64 `json:"percent_under_1_month"`
	PercentUnder6Months float64 `json:"percent_under_6_months"`
	PercentUnder1Year   float64 `json:"percent_under_1_year"`
	PercentOver1Year    float64 `json:"percent_over_1_year"`
}

// TopPassword represents a commonly used password
type TopPassword struct {
	Password   string  `json:"password"` // Plaintext for internal use
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
}

// Recommendation represents an auto-generated recommendation
type Recommendation struct {
	Severity   string  `json:"severity"`   // CRITICAL, HIGH, MEDIUM, INFO
	Count      int     `json:"count"`      // Number of passwords affected
	Percentage float64 `json:"percentage"` // Percentage of total
	Message    string  `json:"message"`    // Full recommendation text
}

// CharacterTypes represents which character types are present in a password
type CharacterTypes struct {
	HasLowercase bool
	HasUppercase bool
	HasNumbers   bool
	HasSpecial   bool
}

// CountTypes returns the number of character types present
func (ct CharacterTypes) CountTypes() int {
	count := 0
	if ct.HasLowercase {
		count++
	}
	if ct.HasUppercase {
		count++
	}
	if ct.HasNumbers {
		count++
	}
	if ct.HasSpecial {
		count++
	}
	return count
}

// IsComplex returns true if password has 3 or 4 character types
func (ct CharacterTypes) IsComplex() bool {
	return ct.CountTypes() >= 3
}

// GetCharsetSize returns the character set size based on types present
func (ct CharacterTypes) GetCharsetSize() int {
	size := 0
	if ct.HasLowercase {
		size += 26
	}
	if ct.HasUppercase {
		size += 26
	}
	if ct.HasNumbers {
		size += 10
	}
	if ct.HasSpecial {
		size += 32 // Common special characters
	}
	return size
}

// WindowsHashStats contains statistics for all Windows-related hash types
type WindowsHashStats struct {
	Overview          WindowsOverviewStats        `json:"overview"`
	NTLM              WindowsHashTypeStats        `json:"ntlm"`
	LM                LMHashStats                 `json:"lm"`
	NetNTLMv1         WindowsHashTypeStats        `json:"netntlmv1"`
	NetNTLMv2         WindowsHashTypeStats        `json:"netntlmv2"`
	DCC               WindowsHashTypeStats        `json:"dcc"`        // MS Cache
	DCC2              WindowsHashTypeStats        `json:"dcc2"`       // MS Cache 2
	Kerberos          KerberosStats               `json:"kerberos"`
	LinkedCorrelation LinkedHashCorrelationStats  `json:"linked_correlation"`
}

// WindowsOverviewStats contains high-level Windows hash statistics
type WindowsOverviewStats struct {
	TotalWindows      int     `json:"total_windows"`      // Total Windows hash records (NTLM + LM + others)
	CrackedWindows    int     `json:"cracked_windows"`
	PercentageWindows float64 `json:"percentage_windows"`
	UniqueUsers       int     `json:"unique_users"`       // Distinct usernames across all Windows hashes
	LinkedPairs       int     `json:"linked_pairs"`       // Number of LM/NTLM linked pairs
}

// WindowsHashTypeStats contains statistics for a specific Windows hash type
type WindowsHashTypeStats struct {
	Total      int     `json:"total"`
	Cracked    int     `json:"cracked"`
	Percentage float64 `json:"percentage"`
}

// LMHashStats contains LM-specific statistics with length breakdown
type LMHashStats struct {
	WindowsHashTypeStats
	UnderEight       int `json:"under_8"`           // Passwords <= 7 chars
	EightToFourteen  int `json:"8_to_14"`           // Passwords 8-14 chars
	PartiallyCracked int `json:"partially_cracked"` // Partial cracks
}

// KerberosStats contains Kerberos statistics with type breakdown
type KerberosStats struct {
	Total      int                             `json:"total"`   // All Kerberos types combined
	Cracked    int                             `json:"cracked"`
	Percentage float64                         `json:"percentage"`
	ByType     map[string]WindowsHashTypeStats `json:"by_type"` // Breakdown by etype
}

// LinkedHashCorrelationStats shows correlation between LM and NTLM hashes
type LinkedHashCorrelationStats struct {
	TotalLinkedPairs int     `json:"total_linked_pairs"` // How many LM-NTLM pairs exist
	BothCracked      int     `json:"both_cracked"`       // Both LM and NTLM cracked
	OnlyNTLMCracked  int     `json:"only_ntlm_cracked"`  // NTLM cracked (implies LM known)
	OnlyLMCracked    int     `json:"only_lm_cracked"`    // LM cracked (NTLM still unknown)
	NeitherCracked   int     `json:"neither_cracked"`    // Neither cracked yet
	PercentageBoth   float64 `json:"percentage_both"`
}

// HashReuseStats contains hash-based password reuse analysis
type HashReuseStats struct {
	TotalReused      int             `json:"total_reused"`       // Hashes appearing 2+ times
	PercentageReused float64         `json:"percentage_reused"`
	TotalUnique      int             `json:"total_unique"`
	HashReuseInfo    []HashReuseInfo `json:"hash_reuse_info"`    // Top 50, paginated
}

// HashReuseInfo contains information about a reused hash value
type HashReuseInfo struct {
	HashValue        string            `json:"hash_value"`         // The actual hash
	HashType         string            `json:"hash_type"`          // e.g., "NTLM", "LM"
	Password         *string           `json:"password,omitempty"` // Cracked password if available
	Users            []UserOccurrence  `json:"users"`              // All users with this hash
	TotalOccurrences int               `json:"total_occurrences"`  // Total count across hashlists
	UserCount        int               `json:"user_count"`         // Unique user count
}

// LMPartialCrackStats contains LM partial crack statistics
type LMPartialCrackStats struct {
	TotalPartial        int                     `json:"total_partial"`
	FirstHalfOnly       int                     `json:"first_half_only"`
	SecondHalfOnly      int                     `json:"second_half_only"`
	PercentagePartial   float64                 `json:"percentage_partial"`     // Of total LM hashes
	PartialCrackDetails []LMPartialCrackDetail  `json:"partial_crack_details"`  // Top 50, paginated
}

// LMPartialCrackDetail contains details about a partially cracked LM hash
type LMPartialCrackDetail struct {
	Username          *string `json:"username,omitempty"`
	Domain            *string `json:"domain,omitempty"`
	FirstHalfCracked  bool    `json:"first_half_cracked"`
	FirstHalfPwd      *string `json:"first_half_pwd,omitempty"`
	SecondHalfCracked bool    `json:"second_half_cracked"`
	SecondHalfPwd     *string `json:"second_half_pwd,omitempty"`
	HashlistName      string  `json:"hashlist_name"`
}

// LMToNTLMMaskStats contains LM-to-NTLM mask generation statistics
type LMToNTLMMaskStats struct {
	TotalLMCracked        int              `json:"total_lm_cracked"`
	TotalMasksGenerated   int              `json:"total_masks_generated"`
	Masks                 []LMNTLMMaskInfo `json:"masks"`                      // Top 50
	TotalEstimatedKeyspace int64           `json:"total_estimated_keyspace"`   // Total combinations
}

// LMNTLMMaskInfo contains information about a generated mask for NTLM cracking
type LMNTLMMaskInfo struct {
	Mask              string  `json:"mask"`               // Hashcat mask (e.g., "?u?l?l?l?l?l?l?l?d?d?d")
	LMPattern         string  `json:"lm_pattern"`         // Original LM pattern (e.g., "AAAAAADDD")
	Count             int     `json:"count"`              // How many LM hashes match this pattern
	Percentage        float64 `json:"percentage"`         // Percentage of total LM hashes
	MatchPercentage   float64 `json:"match_percentage"`   // % of LM hashes matching this mask
	EstimatedKeyspace int64   `json:"estimated_keyspace"` // Keyspace for this mask
	ExampleLM         string  `json:"example_lm"`         // Example LM password
}

// CreateAnalyticsReportRequest represents the request to create a new analytics report
type CreateAnalyticsReportRequest struct {
	ClientID       uuid.UUID `json:"client_id" binding:"required"`
	StartDate      time.Time `json:"start_date" binding:"required"`
	EndDate        time.Time `json:"end_date" binding:"required"`
	CustomPatterns []string  `json:"custom_patterns"`
}
