package services

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// safePercentage calculates a percentage safely, returning 0.0 if denominator is 0
// This prevents NaN values which cannot be serialized to JSON
func safePercentage(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0.0
	}
	return float64(numerator) / float64(denominator) * 100
}

// safeFloat64 returns 0.0 if the input is NaN or Inf, otherwise returns the input
func safeFloat64(val float64) float64 {
	if math.IsNaN(val) || math.IsInf(val, 0) {
		return 0.0
	}
	return val
}

// AnalyticsService handles password analytics generation
type AnalyticsService struct {
	repo *repository.AnalyticsRepository
}

// NewAnalyticsService creates a new AnalyticsService
func NewAnalyticsService(repo *repository.AnalyticsRepository) *AnalyticsService {
	return &AnalyticsService{
		repo: repo,
	}
}

// GenerateAnalytics generates complete analytics for a report
func (s *AnalyticsService) GenerateAnalytics(ctx context.Context, reportID uuid.UUID) error {
	// Get the report
	report, err := s.repo.GetByID(ctx, reportID)
	if err != nil {
		return fmt.Errorf("failed to get report: %w", err)
	}

	// Get hashlists for the client and date range
	hashlistIDs, err := s.repo.GetHashlistsByClientAndDateRange(ctx, report.ClientID, report.StartDate, report.EndDate)
	if err != nil {
		return fmt.Errorf("failed to get hashlists: %w", err)
	}

	if len(hashlistIDs) == 0 {
		return fmt.Errorf("no hashlists found for the specified date range")
	}

	// Get cracked passwords
	passwords, err := s.repo.GetCrackedPasswordsByHashlists(ctx, hashlistIDs)
	if err != nil {
		return fmt.Errorf("failed to get cracked passwords: %w", err)
	}

	if len(passwords) == 0 {
		return fmt.Errorf("no cracked passwords found in the specified hashlists")
	}

	// Get cracked passwords with hashlist tracking for reuse analysis
	passwordsWithHashlists, err := s.repo.GetCrackedPasswordsWithHashlists(ctx, hashlistIDs)
	if err != nil {
		return fmt.Errorf("failed to get cracked passwords with hashlists: %w", err)
	}

	// Get job task speeds
	speeds, err := s.repo.GetJobTaskSpeedsByHashlists(ctx, hashlistIDs)
	if err != nil {
		return fmt.Errorf("failed to get job task speeds: %w", err)
	}

	// Get hashlist info
	totalHashes, totalCracked, err := s.repo.GetHashlistsInfo(ctx, hashlistIDs)
	if err != nil {
		return fmt.Errorf("failed to get hashlist info: %w", err)
	}

	// Get hash counts by type
	hashCounts, err := s.repo.GetHashCountsByType(ctx, hashlistIDs)
	if err != nil {
		return fmt.Errorf("failed to get hash counts by type: %w", err)
	}

	// Get hash type IDs to fetch names
	hashTypeIDs := make([]int, 0, len(hashCounts))
	for hashTypeID := range hashCounts {
		hashTypeIDs = append(hashTypeIDs, hashTypeID)
	}

	// Get hash type names
	hashTypes, err := s.repo.GetHashTypesByIDs(ctx, hashTypeIDs)
	if err != nil {
		return fmt.Errorf("failed to get hash types: %w", err)
	}

	// Get domain breakdown
	domains, err := s.repo.GetDomainsByHashlists(ctx, hashlistIDs)
	if err != nil {
		return fmt.Errorf("failed to get domains: %w", err)
	}

	domainBreakdown, err := s.calculateDomainBreakdown(ctx, hashlistIDs, domains)
	if err != nil {
		return fmt.Errorf("failed to calculate domain breakdown: %w", err)
	}

	// Calculate Windows hash statistics
	windowsHashStats, err := s.calculateWindowsHashStats(ctx, hashlistIDs)
	if err != nil {
		return fmt.Errorf("failed to calculate Windows hash stats: %w", err)
	}

	// Calculate hash reuse (NTLM/LM)
	hashReuse := s.detectHashReuse(ctx, hashlistIDs, hashTypes)

	// Calculate LM partial cracks if LM hashes exist
	var lmPartialCracks *models.LMPartialCrackStats
	totalLMHashes := 0
	if windowsHashStats != nil && windowsHashStats.LM.Total > 0 {
		totalLMHashes = windowsHashStats.LM.Total
		lmPartialCracks, err = s.calculateLMPartialCracks(ctx, hashlistIDs, totalLMHashes)
		if err != nil {
			return fmt.Errorf("failed to calculate LM partial cracks: %w", err)
		}
	}

	// Generate LM-to-NTLM masks if LM passwords exist
	var lmToNTLMMasks *models.LMToNTLMMaskStats
	if windowsHashStats != nil && windowsHashStats.LM.Cracked > 0 {
		lmPasswords, err := s.repo.GetCrackedLMPasswords(ctx, hashlistIDs)
		if err == nil && len(lmPasswords) > 0 {
			lmToNTLMMasks = s.generateLMToNTLMMasks(lmPasswords)
		}
	}

	// Generate all analytics for "All" view
	analyticsData := &models.AnalyticsData{
		Overview:            s.calculateOverview(totalHashes, totalCracked, hashCounts, hashTypes, domainBreakdown),
		WindowsHashes:       windowsHashStats,
		LengthDistribution:  s.calculateLengthDistribution(passwords),
		ComplexityAnalysis:  s.calculateComplexity(passwords),
		PositionalAnalysis:  s.calculatePositionalAnalysis(passwords),
		PatternDetection:    s.detectPatterns(passwords),
		UsernameCorrelation: s.analyzeUsernameCorrelation(passwords),
		PasswordReuse:       s.detectPasswordReuse(passwordsWithHashlists),
		HashReuse:           hashReuse,
		TemporalPatterns:    s.detectTemporalPatterns(passwords),
		MaskAnalysis:        s.analyzeMasks(passwords),
		CustomPatterns:      s.checkCustomPatterns(passwords, report.CustomPatterns, report.ClientID.String()),
		StrengthMetrics:     s.calculateStrengthMetrics(passwords, speeds),
		TopPasswords:        s.getTopPasswords(passwords, 50),
		LMPartialCracks:     lmPartialCracks,
		LMToNTLMMasks:       lmToNTLMMasks,
	}

	// Calculate per-domain analytics if domains exist
	if len(domains) > 0 {
		domainAnalytics := make([]models.DomainAnalytics, 0, len(domains))

		for _, domain := range domains {
			// Get domain-filtered passwords
			domainPasswords, err := s.repo.GetCrackedPasswordsByHashlistsAndDomain(ctx, hashlistIDs, domain)
			if err != nil {
				return fmt.Errorf("failed to get cracked passwords for domain %s: %w", domain, err)
			}

			// Get domain-filtered passwords with hashlist tracking (for reuse analysis)
			domainPasswordsWithHashlists, err := s.repo.GetCrackedPasswordsWithHashlistsAndDomain(ctx, hashlistIDs, domain)
			if err != nil {
				return fmt.Errorf("failed to get cracked passwords with hashlists for domain %s: %w", domain, err)
			}

			// Get domain stats
			domainTotal, domainCracked, err := s.repo.GetDomainStats(ctx, hashlistIDs, domain)
			if err != nil {
				return fmt.Errorf("failed to get stats for domain %s: %w", domain, err)
			}

			// Get hash counts by type for this domain
			domainHashCounts, err := s.repo.GetHashCountsByTypeDomain(ctx, hashlistIDs, domain)
			if err != nil {
				return fmt.Errorf("failed to get hash counts for domain %s: %w", domain, err)
			}

			// Calculate Windows hash statistics for this domain
			domainWindowsHashStats, err := s.calculateWindowsHashStatsDomain(ctx, hashlistIDs, domain)
			if err != nil {
				return fmt.Errorf("failed to calculate Windows hash stats for domain %s: %w", domain, err)
			}

			// Calculate hash reuse for this domain
			domainHashReuse := s.detectHashReuseDomain(ctx, hashlistIDs, hashTypes, domain)

			// Calculate LM partial cracks for this domain (if LM hashes exist)
			var domainLMPartialCracks *models.LMPartialCrackStats
			var domainLMToNTLMMasks *models.LMToNTLMMaskStats
			if domainWindowsHashStats != nil && domainWindowsHashStats.LM.Total > 0 {
				totalLMHashes := domainWindowsHashStats.LM.Total

				// Calculate LM partial cracks
				domainLMPartialCracks, err = s.calculateLMPartialCracksDomain(ctx, hashlistIDs, domain, totalLMHashes)
				if err != nil {
					return fmt.Errorf("failed to calculate LM partial cracks for domain %s: %w", domain, err)
				}

				// Generate LM-to-NTLM masks if we have cracked LM passwords
				if domainWindowsHashStats.LM.Cracked > 0 {
					lmPasswords, err := s.repo.GetCrackedLMPasswordsDomain(ctx, hashlistIDs, domain)
					if err != nil {
						return fmt.Errorf("failed to get cracked LM passwords for domain %s: %w", domain, err)
					}
					domainLMToNTLMMasks = s.generateLMToNTLMMasks(lmPasswords)
				}
			}

			// Calculate all analytics for this domain
			domainAnalytic := models.DomainAnalytics{
				Domain:              domain,
				Overview:            s.calculateOverview(domainTotal, domainCracked, domainHashCounts, hashTypes, nil),
				WindowsHashes:       domainWindowsHashStats,
				LengthDistribution:  s.calculateLengthDistribution(domainPasswords),
				ComplexityAnalysis:  s.calculateComplexity(domainPasswords),
				PositionalAnalysis:  s.calculatePositionalAnalysis(domainPasswords),
				PatternDetection:    s.detectPatterns(domainPasswords),
				UsernameCorrelation: s.analyzeUsernameCorrelation(domainPasswords),
				PasswordReuse:       s.detectPasswordReuse(domainPasswordsWithHashlists),
				HashReuse:           &domainHashReuse,
				TemporalPatterns:    s.detectTemporalPatterns(domainPasswords),
				MaskAnalysis:        s.analyzeMasks(domainPasswords),
				CustomPatterns:      s.checkCustomPatterns(domainPasswords, report.CustomPatterns, report.ClientID.String()),
				StrengthMetrics:     s.calculateStrengthMetrics(domainPasswords, speeds),
				TopPasswords:        s.getTopPasswords(domainPasswords, 50),
				LMPartialCracks:     domainLMPartialCracks,
				LMToNTLMMasks:       domainLMToNTLMMasks,
			}

			domainAnalytics = append(domainAnalytics, domainAnalytic)
		}

		analyticsData.DomainAnalytics = domainAnalytics
	}

	// Generate recommendations based on all analytics
	analyticsData.Recommendations = s.generateRecommendations(analyticsData)

	// Update the report with analytics data
	if err := s.repo.UpdateAnalyticsData(ctx, reportID, analyticsData); err != nil {
		return fmt.Errorf("failed to update analytics data: %w", err)
	}

	// Calculate effective hashlist count (treating linked pairs as ONE hashlist)
	linkedPairCount, err := s.repo.GetLinkedHashlistCount(ctx, hashlistIDs)
	if err != nil {
		return fmt.Errorf("failed to get linked hashlist count: %w", err)
	}

	// Update summary fields (total_hashlists, total_hashes, total_cracked)
	// Linked hashlists count as ONE, so subtract the number of pairs
	effectiveHashlistCount := len(hashlistIDs) - linkedPairCount
	if err := s.repo.UpdateSummaryFields(ctx, reportID, effectiveHashlistCount, totalHashes, totalCracked); err != nil {
		return fmt.Errorf("failed to update summary fields: %w", err)
	}

	return nil
}

// calculateOverview generates overview statistics
func (s *AnalyticsService) calculateOverview(totalHashes, totalCracked int, hashCounts map[int]struct{ Total, Cracked int }, hashTypes map[int]string, domainBreakdown []models.DomainStats) models.OverviewStats {
	// Build hash mode stats
	hashModes := []models.HashModeStats{}
	for modeID, counts := range hashCounts {
		percentage := 0.0
		if counts.Total > 0 {
			percentage = float64(counts.Cracked) / float64(counts.Total) * 100
		}

		// Get hash type name, default to "Mode <ID>" if not found
		modeName := fmt.Sprintf("Mode %d", modeID)
		if name, exists := hashTypes[modeID]; exists {
			modeName = fmt.Sprintf("%s (%d)", name, modeID)
		}

		hashModes = append(hashModes, models.HashModeStats{
			ModeID:     modeID,
			ModeName:   modeName,
			Total:      counts.Total,
			Cracked:    counts.Cracked,
			Percentage: percentage,
		})
	}

	crackPercentage := 0.0
	if totalHashes > 0 {
		crackPercentage = float64(totalCracked) / float64(totalHashes) * 100
	}

	return models.OverviewStats{
		TotalHashes:     totalHashes,
		TotalCracked:    totalCracked,
		CrackPercentage: crackPercentage,
		HashModes:       hashModes,
		DomainBreakdown: domainBreakdown,
	}
}

// calculateDomainBreakdown calculates statistics for each domain
func (s *AnalyticsService) calculateDomainBreakdown(ctx context.Context, hashlistIDs []int64, domains []string) ([]models.DomainStats, error) {
	domainStats := make([]models.DomainStats, 0, len(domains))

	for _, domain := range domains {
		total, cracked, err := s.repo.GetDomainStats(ctx, hashlistIDs, domain)
		if err != nil {
			return nil, fmt.Errorf("failed to get stats for domain %s: %w", domain, err)
		}

		percentage := 0.0
		if total > 0 {
			percentage = float64(cracked) / float64(total) * 100
		}

		domainStats = append(domainStats, models.DomainStats{
			Domain:          domain,
			TotalHashes:     total,
			CrackedHashes:   cracked,
			CrackPercentage: percentage,
		})
	}

	return domainStats, nil
}

// calculateLengthDistribution analyzes password length distribution
func (s *AnalyticsService) calculateLengthDistribution(passwords []*models.Hash) models.LengthStats {
	lengthMap := make(map[int]int)
	var totalLength int64
	var totalLengthUnder15 int64
	var countUnder15 int
	var countUnder8 int
	var count8to11 int

	for _, pwd := range passwords {
		if pwd.Password == nil {
			continue // Skip entries without passwords
		}
		length := len([]rune(*pwd.Password))
		lengthMap[length]++
		totalLength += int64(length)

		if length < 15 {
			totalLengthUnder15 += int64(length)
			countUnder15++
		}
		if length < 8 {
			countUnder8++
		}
		if length >= 8 && length <= 11 {
			count8to11++
		}
	}

	// Build distribution map
	distribution := make(map[string]models.CategoryCount)
	for length, count := range lengthMap {
		key := fmt.Sprintf("%d", length)
		if length > 32 {
			key = "32+"
		}

		existing := distribution[key]
		existing.Count += count
		distribution[key] = existing
	}

	// Calculate percentages
	total := len(passwords)
	for key, cat := range distribution {
		cat.Percentage = safePercentage(cat.Count, total)
		distribution[key] = cat
	}

	// Find most common lengths
	type lengthCount struct {
		length int
		count  int
	}
	var lengths []lengthCount
	for length, count := range lengthMap {
		lengths = append(lengths, lengthCount{length, count})
	}
	sort.Slice(lengths, func(i, j int) bool {
		return lengths[i].count > lengths[j].count
	})

	mostCommon := []int{}
	for i := 0; i < len(lengths) && i < 3; i++ {
		mostCommon = append(mostCommon, lengths[i].length)
	}

	avgLength := 0.0
	if total > 0 {
		avgLength = float64(totalLength) / float64(total)
	}
	avgLengthUnder15 := 0.0
	if countUnder15 > 0 {
		avgLengthUnder15 = float64(totalLengthUnder15) / float64(countUnder15)
	}

	return models.LengthStats{
		Distribution:         distribution,
		AverageLength:        avgLength,
		AverageLengthUnder15: avgLengthUnder15,
		MostCommonLengths:    mostCommon,
		CountUnder8:          countUnder8,
		Count8to11:           count8to11,
		CountUnder15:         countUnder15,
	}
}

// detectCharacterTypes identifies which character types are present in a password
func (s *AnalyticsService) detectCharacterTypes(password string) models.CharacterTypes {
	types := models.CharacterTypes{}

	for _, r := range password {
		if unicode.IsLower(r) {
			types.HasLowercase = true
		} else if unicode.IsUpper(r) {
			types.HasUppercase = true
		} else if unicode.IsDigit(r) {
			types.HasNumbers = true
		} else {
			types.HasSpecial = true
		}
	}

	return types
}

// calculateComplexity analyzes password complexity
func (s *AnalyticsService) calculateComplexity(passwords []*models.Hash) models.ComplexityStats {
	singleType := make(map[string]int)
	twoTypes := make(map[string]int)
	threeTypes := make(map[string]int)
	fourTypesCount := 0
	complexShortCount := 0
	complexLongCount := 0

	for _, pwd := range passwords {
		if pwd.Password == nil {
			continue // Skip entries without passwords
		}
		charTypes := s.detectCharacterTypes(*pwd.Password)
		typeCount := charTypes.CountTypes()
		length := len([]rune(*pwd.Password))

		switch typeCount {
		case 1:
			if charTypes.HasLowercase {
				singleType["lowercase_only"]++
			} else if charTypes.HasUppercase {
				singleType["uppercase_only"]++
			} else if charTypes.HasNumbers {
				singleType["numbers_only"]++
			} else if charTypes.HasSpecial {
				singleType["special_only"]++
			}
		case 2:
			key := s.getTwoTypeKey(charTypes)
			twoTypes[key]++
		case 3:
			key := s.getThreeTypeKey(charTypes)
			threeTypes[key]++
		case 4:
			fourTypesCount++
		}

		// Check for complex short vs long
		if charTypes.IsComplex() {
			if length <= 14 {
				complexShortCount++
			} else {
				complexLongCount++
			}
		}
	}

	total := len(passwords)

	return models.ComplexityStats{
		SingleType:   s.mapToCategories(singleType, total),
		TwoTypes:     s.mapToCategories(twoTypes, total),
		ThreeTypes:   s.mapToCategories(threeTypes, total),
		FourTypes:    models.CategoryCount{Count: fourTypesCount, Percentage: safePercentage(fourTypesCount, total)},
		ComplexShort: models.CategoryCount{Count: complexShortCount, Percentage: safePercentage(complexShortCount, total)},
		ComplexLong:  models.CategoryCount{Count: complexLongCount, Percentage: safePercentage(complexLongCount, total)},
	}
}

// getTwoTypeKey returns a key for two character type combinations
func (s *AnalyticsService) getTwoTypeKey(types models.CharacterTypes) string {
	if types.HasLowercase && types.HasUppercase {
		return "lowercase_uppercase"
	}
	if types.HasLowercase && types.HasNumbers {
		return "lowercase_numbers"
	}
	if types.HasLowercase && types.HasSpecial {
		return "lowercase_special"
	}
	if types.HasUppercase && types.HasNumbers {
		return "uppercase_numbers"
	}
	if types.HasUppercase && types.HasSpecial {
		return "uppercase_special"
	}
	if types.HasNumbers && types.HasSpecial {
		return "numbers_special"
	}
	return "unknown"
}

// getThreeTypeKey returns a key for three character type combinations
func (s *AnalyticsService) getThreeTypeKey(types models.CharacterTypes) string {
	if types.HasLowercase && types.HasUppercase && types.HasNumbers {
		return "lowercase_uppercase_numbers"
	}
	if types.HasLowercase && types.HasUppercase && types.HasSpecial {
		return "lowercase_uppercase_special"
	}
	if types.HasLowercase && types.HasNumbers && types.HasSpecial {
		return "lowercase_numbers_special"
	}
	if types.HasUppercase && types.HasNumbers && types.HasSpecial {
		return "uppercase_numbers_special"
	}
	return "unknown"
}

// mapToCategories converts a map of counts to CategoryCount map
func (s *AnalyticsService) mapToCategories(counts map[string]int, total int) map[string]models.CategoryCount {
	result := make(map[string]models.CategoryCount)
	for key, count := range counts {
		result[key] = models.CategoryCount{
			Count:      count,
			Percentage: safePercentage(count, total),
		}
	}
	return result
}

// calculatePositionalAnalysis analyzes where complexity elements appear
func (s *AnalyticsService) calculatePositionalAnalysis(passwords []*models.Hash) models.PositionalStats {
	startsUpper := 0
	endsNumber := 0
	endsSpecial := 0

	for _, pwd := range passwords {
		if pwd.Password == nil {
			continue // Skip entries without passwords
		}
		runes := []rune(*pwd.Password)
		if len(runes) == 0 {
			continue
		}

		if unicode.IsUpper(runes[0]) {
			startsUpper++
		}

		lastRune := runes[len(runes)-1]
		if unicode.IsDigit(lastRune) {
			endsNumber++
		} else if !unicode.IsLetter(lastRune) && !unicode.IsDigit(lastRune) {
			endsSpecial++
		}
	}

	total := len(passwords)

	return models.PositionalStats{
		StartsUppercase: models.CategoryCount{Count: startsUpper, Percentage: safePercentage(startsUpper, total)},
		EndsNumber:      models.CategoryCount{Count: endsNumber, Percentage: safePercentage(endsNumber, total)},
		EndsSpecial:     models.CategoryCount{Count: endsSpecial, Percentage: safePercentage(endsSpecial, total)},
	}
}

// detectPatterns detects common password patterns
func (s *AnalyticsService) detectPatterns(passwords []*models.Hash) models.PatternStats {
	keyboardWalks := 0
	sequential := 0
	repeating := 0
	baseWords := make(map[string]int)

	// Common keyboard walk patterns
	keyboards := []string{"qwerty", "asdf", "zxcv", "qazwsx", "12345", "67890"}
	keyboardRegexes := make([]*regexp.Regexp, len(keyboards))
	for i, kb := range keyboards {
		keyboardRegexes[i] = regexp.MustCompile("(?i)" + kb)
	}

	// Sequential number pattern
	sequentialRegex := regexp.MustCompile(`\d{3,}|[a-z]{3,}|[A-Z]{3,}`)

	// Helper function to detect repeating characters (3+ of the same char)
	hasRepeatingChars := func(s string) bool {
		runes := []rune(s)
		for i := 0; i < len(runes)-2; i++ {
			if runes[i] == runes[i+1] && runes[i+1] == runes[i+2] {
				return true
			}
		}
		return false
	}

	// Common base words
	commonWords := []string{"password", "welcome", "admin", "user", "login", "spring", "summer", "fall", "winter", "autumn"}

	for _, pwd := range passwords {
		if pwd.Password == nil {
			continue // Skip entries without passwords
		}
		lower := strings.ToLower(*pwd.Password)

		// Check keyboard walks
		for _, re := range keyboardRegexes {
			if re.MatchString(lower) {
				keyboardWalks++
				break
			}
		}

		// Check sequential
		if sequentialRegex.MatchString(*pwd.Password) {
			sequential++
		}

		// Check repeating
		if hasRepeatingChars(*pwd.Password) {
			repeating++
		}

		// Check base words
		for _, word := range commonWords {
			if strings.Contains(lower, word) {
				baseWords[word]++
				break
			}
		}
	}

	total := len(passwords)

	return models.PatternStats{
		KeyboardWalks:   models.CategoryCount{Count: keyboardWalks, Percentage: safePercentage(keyboardWalks, total)},
		Sequential:      models.CategoryCount{Count: sequential, Percentage: safePercentage(sequential, total)},
		RepeatingChars:  models.CategoryCount{Count: repeating, Percentage: safePercentage(repeating, total)},
		CommonBaseWords: s.mapToCategories(baseWords, total),
	}
}

// analyzeUsernameCorrelation checks for username-related patterns
func (s *AnalyticsService) analyzeUsernameCorrelation(passwords []*models.Hash) models.UsernameStats {
	equals := 0
	contains := 0
	suffix := 0
	reversed := 0

	// Regex for common suffixes
	suffixRegex := regexp.MustCompile(`\d{1,4}|!+|@+`)

	for _, pwd := range passwords {
		if pwd.Password == nil || pwd.Username == nil || *pwd.Username == "" {
			continue
		}

		username := strings.ToLower(*pwd.Username)
		password := strings.ToLower(*pwd.Password)

		if username == password {
			equals++
		} else if strings.HasPrefix(password, username) {
			// Check if password is username + suffix
			suffixStr := password[len(username):]
			if suffixRegex.MatchString(suffixStr) {
				suffix++
			} else {
				// Username is prefix but not a clean suffix pattern
				contains++
			}
		} else if strings.Contains(password, username) {
			contains++
		}

		// Check reversed
		reversedUsername := reverse(username)
		if reversedUsername == password {
			reversed++
		}
	}

	total := len(passwords)

	return models.UsernameStats{
		EqualsUsername:     models.CategoryCount{Count: equals, Percentage: safePercentage(equals, total)},
		ContainsUsername:   models.CategoryCount{Count: contains, Percentage: safePercentage(contains, total)},
		UsernamePlusSuffix: models.CategoryCount{Count: suffix, Percentage: safePercentage(suffix, total)},
		ReversedUsername:   models.CategoryCount{Count: reversed, Percentage: safePercentage(reversed, total)},
	}
}

// reverse reverses a string
func reverse(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

// detectPasswordReuse analyzes password reuse with hashlist tracking
func (s *AnalyticsService) detectPasswordReuse(hashesWithHashlists []repository.HashWithHashlist) models.ReuseStats {
	// Build map: password -> username -> set of hashlist IDs
	passwordUserHashlists := make(map[string]map[string]map[int64]bool)

	for _, hwh := range hashesWithHashlists {
		if hwh.Hash.Password == nil {
			continue // Skip entries without passwords
		}
		password := *hwh.Hash.Password
		username := "NULL"
		if hwh.Hash.Username != nil {
			username = *hwh.Hash.Username
		}

		// Initialize nested maps if needed
		if passwordUserHashlists[password] == nil {
			passwordUserHashlists[password] = make(map[string]map[int64]bool)
		}
		if passwordUserHashlists[password][username] == nil {
			passwordUserHashlists[password][username] = make(map[int64]bool)
		}

		// Track this hashlist for this user-password combo
		passwordUserHashlists[password][username][hwh.HashlistID] = true
	}

	// Build PasswordReuseInfo entries for passwords used across 2+ hashlists
	passwordReuseList := []models.PasswordReuseInfo{}
	totalReused := 0
	totalUnique := 0

	for password, userHashlists := range passwordUserHashlists {
		// Calculate total occurrences across all users first
		users := []models.UserOccurrence{}
		totalOccurrences := 0

		for username, hashlists := range userHashlists {
			hashlistCount := len(hashlists)
			users = append(users, models.UserOccurrence{
				Username:      username,
				HashlistCount: hashlistCount,
			})
			totalOccurrences += hashlistCount
		}

		// Check if password is reused based on total occurrences (not user count)
		// Detects both single-user reuse across hashlists and multi-user reuse
		if totalOccurrences >= 2 {
			// Sort users alphabetically for consistent display
			sort.Slice(users, func(i, j int) bool {
				return users[i].Username < users[j].Username
			})

			passwordReuseList = append(passwordReuseList, models.PasswordReuseInfo{
				Password:         password,
				Users:            users,
				TotalOccurrences: totalOccurrences,
				UserCount:        len(users),
			})
			totalReused += totalOccurrences
		} else {
			// Not reused - single occurrence
			totalUnique += totalOccurrences
		}
	}

	// Sort by total occurrences (descending) - most reused passwords first
	sort.Slice(passwordReuseList, func(i, j int) bool {
		return passwordReuseList[i].TotalOccurrences > passwordReuseList[j].TotalOccurrences
	})

	total := totalReused + totalUnique
	percentageReused := 0.0
	if total > 0 {
		percentageReused = float64(totalReused) / float64(total) * 100
	}

	return models.ReuseStats{
		TotalReused:       totalReused,
		PercentageReused:  percentageReused,
		TotalUnique:       totalUnique,
		PasswordReuseInfo: passwordReuseList,
	}
}

// detectTemporalPatterns detects date/time related patterns
func (s *AnalyticsService) detectTemporalPatterns(passwords []*models.Hash) models.TemporalStats {
	containsYear := 0
	containsMonth := 0
	containsSeason := 0
	yearBreakdown := make(map[string]int)

	years := []string{"2024", "2023", "2022", "2021", "2020"}
	months := []string{"january", "jan", "february", "feb", "march", "mar", "april", "apr", "may", "june", "jun", "july", "jul", "august", "aug", "september", "sep", "october", "oct", "november", "nov", "december", "dec"}
	seasons := []string{"spring", "summer", "fall", "winter", "autumn"}

	for _, pwd := range passwords {
		if pwd.Password == nil {
			continue // Skip entries without passwords
		}
		lower := strings.ToLower(*pwd.Password)

		// Check years
		foundYear := false
		for _, year := range years {
			if strings.Contains(*pwd.Password, year) {
				yearBreakdown[year]++
				if !foundYear {
					containsYear++
					foundYear = true
				}
			}
		}

		// Check months
		for _, month := range months {
			if strings.Contains(lower, month) {
				containsMonth++
				break
			}
		}

		// Check seasons
		for _, season := range seasons {
			if strings.Contains(lower, season) {
				containsSeason++
				break
			}
		}
	}

	total := len(passwords)

	return models.TemporalStats{
		ContainsYear:   models.CategoryCount{Count: containsYear, Percentage: safePercentage(containsYear, total)},
		ContainsMonth:  models.CategoryCount{Count: containsMonth, Percentage: safePercentage(containsMonth, total)},
		ContainsSeason: models.CategoryCount{Count: containsSeason, Percentage: safePercentage(containsSeason, total)},
		YearBreakdown:  s.mapToCategories(yearBreakdown, total),
	}
}

// analyzeMasks generates hashcat-style masks
func (s *AnalyticsService) analyzeMasks(passwords []*models.Hash) models.MaskStats {
	maskCounts := make(map[string]struct {
		count   int
		example string
	})

	for _, pwd := range passwords {
		if pwd.Password == nil {
			continue // Skip entries without passwords
		}
		mask := s.passwordToMask(*pwd.Password)
		existing := maskCounts[mask]
		existing.count++
		if existing.example == "" {
			existing.example = *pwd.Password
		}
		maskCounts[mask] = existing
	}

	// Convert to slice and sort
	type maskItem struct {
		mask    string
		count   int
		example string
	}
	var masks []maskItem
	for mask, data := range maskCounts {
		masks = append(masks, maskItem{mask, data.count, data.example})
	}
	sort.Slice(masks, func(i, j int) bool {
		return masks[i].count > masks[j].count
	})

	// Take top 20
	topMasks := []models.MaskInfo{}
	total := len(passwords)
	for i := 0; i < len(masks) && i < 20; i++ {
		topMasks = append(topMasks, models.MaskInfo{
			Mask:       masks[i].mask,
			Count:      masks[i].count,
			Percentage: safePercentage(masks[i].count, total),
			Example:    masks[i].example,
		})
	}

	return models.MaskStats{
		TopMasks: topMasks,
	}
}

// passwordToMask converts a password to hashcat-style mask
func (s *AnalyticsService) passwordToMask(password string) string {
	var mask strings.Builder

	for _, r := range password {
		if unicode.IsLower(r) {
			mask.WriteString("?l")
		} else if unicode.IsUpper(r) {
			mask.WriteString("?u")
		} else if unicode.IsDigit(r) {
			mask.WriteString("?d")
		} else {
			mask.WriteString("?s")
		}
	}

	return mask.String()
}

// checkCustomPatterns checks for custom organization patterns
func (s *AnalyticsService) checkCustomPatterns(passwords []*models.Hash, customPatterns pq.StringArray, clientID string) models.CustomPatternStats {
	// TODO: Get client name from database to generate automatic patterns
	// For now, just use provided custom patterns

	patterns := []string{}
	patterns = append(patterns, customPatterns...)

	patternsDetected := make(map[string]int)

	for _, pwd := range passwords {
		if pwd.Password == nil {
			continue // Skip entries without passwords
		}
		lower := strings.ToLower(*pwd.Password)
		for _, pattern := range patterns {
			if strings.Contains(lower, strings.ToLower(pattern)) {
				patternsDetected[pattern]++
				break
			}
		}
	}

	total := len(passwords)

	return models.CustomPatternStats{
		PatternsDetected: s.mapToCategories(patternsDetected, total),
	}
}

// calculateStrengthMetrics calculates password strength metrics
func (s *AnalyticsService) calculateStrengthMetrics(passwords []*models.Hash, speeds []int64) models.StrengthStats {
	// Calculate average speed
	avgSpeed := int64(0)
	if len(speeds) > 0 {
		var total int64
		for _, speed := range speeds {
			total += speed
		}
		avgSpeed = total / int64(len(speeds))
	}

	// Calculate entropy distribution
	lowEntropy := 0
	moderateEntropy := 0
	highEntropy := 0

	for _, pwd := range passwords {
		if pwd.Password == nil {
			continue // Skip entries without passwords
		}
		entropy := s.calculateEntropy(*pwd.Password)

		if entropy < 78 {
			lowEntropy++
		} else if entropy < 128 {
			moderateEntropy++
		} else {
			highEntropy++
		}
	}

	total := len(passwords)

	entropyDist := models.EntropyDistribution{
		Low:      models.CategoryCount{Count: lowEntropy, Percentage: safePercentage(lowEntropy, total)},
		Moderate: models.CategoryCount{Count: moderateEntropy, Percentage: safePercentage(moderateEntropy, total)},
		High:     models.CategoryCount{Count: highEntropy, Percentage: safePercentage(highEntropy, total)},
	}

	// Calculate crack time estimates if we have speed data
	crackTimeEstimates := models.CrackTimeEstimates{}
	if avgSpeed > 0 {
		crackTimeEstimates.Speed50Percent = s.calculateSpeedLevelEstimate(passwords, avgSpeed/2)
		crackTimeEstimates.Speed75Percent = s.calculateSpeedLevelEstimate(passwords, avgSpeed*3/4)
		crackTimeEstimates.Speed100Percent = s.calculateSpeedLevelEstimate(passwords, avgSpeed)
		crackTimeEstimates.Speed150Percent = s.calculateSpeedLevelEstimate(passwords, avgSpeed*3/2)
		crackTimeEstimates.Speed200Percent = s.calculateSpeedLevelEstimate(passwords, avgSpeed*2)
	}

	return models.StrengthStats{
		AverageSpeedHPS:     avgSpeed,
		EntropyDistribution: entropyDist,
		CrackTimeEstimates:  crackTimeEstimates,
	}
}

// calculateEntropy calculates Shannon entropy for a password
func (s *AnalyticsService) calculateEntropy(password string) float64 {
	charTypes := s.detectCharacterTypes(password)
	charsetSize := charTypes.GetCharsetSize()

	if charsetSize == 0 {
		return 0
	}

	length := float64(len([]rune(password)))
	return length * math.Log2(float64(charsetSize))
}

// calculateSpeedLevelEstimate calculates crack time estimates for a specific speed
func (s *AnalyticsService) calculateSpeedLevelEstimate(passwords []*models.Hash, speedHPS int64) models.SpeedLevelEstimate {
	under1Hour := 0
	under1Day := 0
	under1Week := 0
	under1Month := 0
	under6Months := 0
	under1Year := 0
	over1Year := 0

	const (
		hour      = 3600
		day       = 86400
		week      = 604800
		month     = 2592000  // 30 days
		sixMonths = 15552000 // 180 days
		year      = 31536000 // 365 days
	)

	for _, pwd := range passwords {
		if pwd.Password == nil {
			continue // Skip entries without passwords
		}
		seconds := s.estimateCrackTime(*pwd.Password, speedHPS)

		if seconds < hour {
			under1Hour++
		} else if seconds < day {
			under1Day++
		} else if seconds < week {
			under1Week++
		} else if seconds < month {
			under1Month++
		} else if seconds < sixMonths {
			under6Months++
		} else if seconds < year {
			under1Year++
		} else {
			over1Year++
		}
	}

	total := len(passwords)

	return models.SpeedLevelEstimate{
		SpeedHPS:            speedHPS,
		PercentUnder1Hour:   safePercentage(under1Hour, total),
		PercentUnder1Day:    safePercentage(under1Day, total),
		PercentUnder1Week:   safePercentage(under1Week, total),
		PercentUnder1Month:  safePercentage(under1Month, total),
		PercentUnder6Months: safePercentage(under6Months, total),
		PercentUnder1Year:   safePercentage(under1Year, total),
		PercentOver1Year:    safePercentage(over1Year, total),
	}
}

// estimateCrackTime estimates time to crack a password in seconds
func (s *AnalyticsService) estimateCrackTime(password string, speedHPS int64) int64 {
	if speedHPS == 0 {
		return 0
	}

	charTypes := s.detectCharacterTypes(password)
	charsetSize := charTypes.GetCharsetSize()

	if charsetSize == 0 {
		return 0
	}

	length := len([]rune(password))
	keyspace := math.Pow(float64(charsetSize), float64(length))

	// Average case is half the keyspace
	avgKeyspace := keyspace / 2

	return int64(avgKeyspace / float64(speedHPS))
}

// getTopPasswords returns the most common passwords (only those used 2+ times)
func (s *AnalyticsService) getTopPasswords(passwords []*models.Hash, limit int) []models.TopPassword {
	passwordCounts := make(map[string]int)

	for _, pwd := range passwords {
		if pwd.Password == nil {
			continue // Skip entries without passwords
		}
		passwordCounts[*pwd.Password]++
	}

	topList := []models.TopPassword{}
	total := len(passwords)

	for password, count := range passwordCounts {
		if count >= 2 { // Only include passwords used 2+ times
			topList = append(topList, models.TopPassword{
				Password:   password,
				Count:      count,
				Percentage: safePercentage(count, total),
			})
		}
	}

	// Sort by count descending
	sort.Slice(topList, func(i, j int) bool {
		return topList[i].Count > topList[j].Count
	})

	if len(topList) > limit {
		return topList[:limit]
	}

	return topList
}

// generateRecommendations generates auto-recommendations based on analytics
func (s *AnalyticsService) generateRecommendations(data *models.AnalyticsData) []models.Recommendation {
	recs := []models.Recommendation{}
	total := data.Overview.TotalCracked

	// Length-based recommendations (if ANY passwords meet criteria)
	if data.LengthDistribution.CountUnder8 > 0 {
		count := data.LengthDistribution.CountUnder8
		percent := safePercentage(count, total)
		recs = append(recs, models.Recommendation{
			Severity:   "CRITICAL",
			Count:      count,
			Percentage: percent,
			Message:    fmt.Sprintf("%d passwords (%.2f%%) were below 8 characters. Meet industry standard of 12 characters minimum, but recommend 15+ characters for optimal security.", count, percent),
		})
	}

	if data.LengthDistribution.Count8to11 > 0 {
		count := data.LengthDistribution.Count8to11
		percent := safePercentage(count, total)
		recs = append(recs, models.Recommendation{
			Severity:   "HIGH",
			Count:      count,
			Percentage: percent,
			Message:    fmt.Sprintf("%d passwords (%.2f%%) were between 8 and 11 characters. Meet industry standard of 12 characters minimum, but recommend 15+ characters for optimal security.", count, percent),
		})
	}

	if data.LengthDistribution.CountUnder15 > 0 {
		count := data.LengthDistribution.CountUnder15
		percent := safePercentage(count, total)
		recs = append(recs, models.Recommendation{
			Severity:   "MEDIUM",
			Count:      count,
			Percentage: percent,
			Message:    fmt.Sprintf("%d passwords (%.2f%%) were less than 15 characters. Consider implementing 15-character minimum per NIST 2024 recommendations.", count, percent),
		})

		// Add average length info for sub-optimal passwords
		if data.LengthDistribution.AverageLengthUnder15 > 0 {
			recs = append(recs, models.Recommendation{
				Severity:   "INFO",
				Count:      count,
				Percentage: percent,
				Message:    fmt.Sprintf("Average password length for sub-optimal passwords (<15 chars) is %.1f characters. Educate users on creating longer passphrases.", data.LengthDistribution.AverageLengthUnder15),
			})
		}
	}

	// Complexity-based recommendations
	singleTypeCount := 0
	for _, cat := range data.ComplexityAnalysis.SingleType {
		singleTypeCount += cat.Count
	}
	if safePercentage(singleTypeCount, total) > 40 {
		percent := safePercentage(singleTypeCount, total)
		recs = append(recs, models.Recommendation{
			Severity:   "HIGH",
			Count:      singleTypeCount,
			Percentage: percent,
			Message:    fmt.Sprintf("%d passwords (%.2f%%) use only one character type. Require character diversity (at least 3 of 4 types).", singleTypeCount, percent),
		})
	}

	// Pattern-based recommendations
	if data.PatternDetection.KeyboardWalks.Percentage > 5 {
		recs = append(recs, models.Recommendation{
			Severity:   "HIGH",
			Count:      data.PatternDetection.KeyboardWalks.Count,
			Percentage: data.PatternDetection.KeyboardWalks.Percentage,
			Message:    fmt.Sprintf("%d passwords (%.2f%%) contain keyboard walks. Implement keyboard pattern detection in password validation.", data.PatternDetection.KeyboardWalks.Count, data.PatternDetection.KeyboardWalks.Percentage),
		})
	}

	// Username correlation
	if data.UsernameCorrelation.EqualsUsername.Percentage > 10 {
		recs = append(recs, models.Recommendation{
			Severity:   "CRITICAL",
			Count:      data.UsernameCorrelation.EqualsUsername.Count,
			Percentage: data.UsernameCorrelation.EqualsUsername.Percentage,
			Message:    fmt.Sprintf("%d passwords (%.2f%%) equal username. Block passwords containing username.", data.UsernameCorrelation.EqualsUsername.Count, data.UsernameCorrelation.EqualsUsername.Percentage),
		})
	}

	// Password reuse
	if data.PasswordReuse.PercentageReused > 5 {
		recs = append(recs, models.Recommendation{
			Severity:   "CRITICAL",
			Count:      data.PasswordReuse.TotalReused,
			Percentage: data.PasswordReuse.PercentageReused,
			Message:    fmt.Sprintf("%d passwords (%.2f%%) are reused. Enforce unique passwords across users.", data.PasswordReuse.TotalReused, data.PasswordReuse.PercentageReused),
		})
	}

	// Entropy-based
	if data.StrengthMetrics.EntropyDistribution.Low.Percentage > 30 {
		recs = append(recs, models.Recommendation{
			Severity:   "CRITICAL",
			Count:      data.StrengthMetrics.EntropyDistribution.Low.Count,
			Percentage: data.StrengthMetrics.EntropyDistribution.Low.Percentage,
			Message:    fmt.Sprintf("%d passwords (%.2f%%) have low entropy (<78 bits). Require longer, more complex passwords.", data.StrengthMetrics.EntropyDistribution.Low.Count, data.StrengthMetrics.EntropyDistribution.Low.Percentage),
		})
	}

	// Windows-specific recommendations
	if data.WindowsHashes != nil {
		// CRITICAL: LM hashes detected
		if data.WindowsHashes.LM.Total > 0 {
			recs = append(recs, models.Recommendation{
				Severity:   "CRITICAL",
				Count:      data.WindowsHashes.LM.Total,
				Percentage: float64(data.WindowsHashes.LM.Total) / float64(data.Overview.TotalHashes) * 100,
				Message:    fmt.Sprintf("%d LM hashes detected in the environment. LM hashing is a legacy authentication protocol with severe security weaknesses: passwords are converted to uppercase (reducing complexity), split into 7-character halves (enabling independent cracking), and use weak DES encryption. Organizations must immediately disable LM hash storage in Active Directory by configuring the 'Network security: Do not store LAN Manager hash value on next password change' Group Policy setting. All affected accounts should perform a password reset after this policy is applied to remove existing LM hashes from the domain.", data.WindowsHashes.LM.Total),
			})
		}

		// HIGH: NetNTLMv1 detected
		if data.WindowsHashes.NetNTLMv1.Total > 0 {
			recs = append(recs, models.Recommendation{
				Severity:   "HIGH",
				Count:      data.WindowsHashes.NetNTLMv1.Total,
				Percentage: float64(data.WindowsHashes.NetNTLMv1.Total) / float64(data.Overview.TotalHashes) * 100,
				Message:    fmt.Sprintf("%d NetNTLMv1 hashes detected. NetNTLMv1 is vulnerable to relay attacks and should be disabled in favor of NetNTLMv2. Configure the 'Network security: LAN Manager authentication level' Group Policy setting to 'Send NTLMv2 response only' or higher.", data.WindowsHashes.NetNTLMv1.Total),
			})
		}

		// MEDIUM: Weak Kerberos encryption
		if kerberosEtype23, ok := data.WindowsHashes.Kerberos.ByType["etype_23"]; ok && kerberosEtype23.Total > 0 {
			recs = append(recs, models.Recommendation{
				Severity:   "MEDIUM",
				Count:      kerberosEtype23.Total,
				Percentage: float64(kerberosEtype23.Total) / float64(data.Overview.TotalHashes) * 100,
				Message:    fmt.Sprintf("%d Kerberos hashes using RC4 encryption (etype 23) detected. RC4 is considered weak and vulnerable to brute-force attacks. Enable AES encryption (etype 17/18) in Active Directory by configuring the 'Network security: Configure encryption types allowed for Kerberos' Group Policy setting to prefer AES256 and AES128.", kerberosEtype23.Total),
			})
		}
	}

	// LM partial cracks
	if data.LMPartialCracks != nil && data.LMPartialCracks.TotalPartial > 0 {
		recs = append(recs, models.Recommendation{
			Severity:   "HIGH",
			Count:      data.LMPartialCracks.TotalPartial,
			Percentage: data.LMPartialCracks.PercentagePartial,
			Message:    fmt.Sprintf("%d LM hashes are partially cracked. This indicates the password has been partially recovered, making full compromise significantly easier. These accounts require immediate password resets and LM hash storage must be disabled domain-wide.", data.LMPartialCracks.TotalPartial),
		})
	}

	// Sort recommendations by severity: CRITICAL > HIGH > MEDIUM > LOW > INFO
	severityOrder := map[string]int{
		"CRITICAL": 0,
		"HIGH":     1,
		"MEDIUM":   2,
		"LOW":      3,
		"INFO":     4,
	}

	sort.Slice(recs, func(i, j int) bool {
		return severityOrder[recs[i].Severity] < severityOrder[recs[j].Severity]
	})

	return recs
}

// calculateWindowsHashStats generates Windows hash statistics
func (s *AnalyticsService) calculateWindowsHashStats(ctx context.Context, hashlistIDs []int64) (*models.WindowsHashStats, error) {
	// Get all Windows hash counts (raw counts for individual cards)
	counts, err := s.repo.GetWindowsHashCounts(ctx, hashlistIDs)
	if err != nil {
		return nil, err
	}

	// If no Windows hashes found, return nil
	if len(counts) == 0 {
		return nil, nil
	}

	// Get effective overview counts (linked-aware for overview section)
	totalWindows, crackedWindows, err := s.repo.GetWindowsOverviewCounts(ctx, hashlistIDs)
	if err != nil {
		return nil, err
	}

	percentageWindows := 0.0
	if totalWindows > 0 {
		percentageWindows = float64(crackedWindows) / float64(totalWindows) * 100
	}

	// Helper function to calculate percentage
	calcPercentage := func(cracked, total int) float64 {
		if total == 0 {
			return 0.0
		}
		return float64(cracked) / float64(total) * 100
	}

	// Build individual hash type stats
	ntlm := models.WindowsHashTypeStats{}
	if count, ok := counts[1000]; ok {
		ntlm.Total = count.Total
		ntlm.Cracked = count.Cracked
		ntlm.Percentage = calcPercentage(count.Cracked, count.Total)
	}

	// LM with length breakdown
	lm := models.LMHashStats{}
	if count, ok := counts[3000]; ok {
		lm.Total = count.Total
		lm.Cracked = count.Cracked
		lm.Percentage = calcPercentage(count.Cracked, count.Total)

		// Get LM password lengths
		underEight, eightToFourteen, err := s.repo.GetLMPasswordLengths(ctx, hashlistIDs)
		if err == nil {
			lm.UnderEight = underEight
			lm.EightToFourteen = eightToFourteen
		}

		// Get partial crack count
		partialCount, err := s.repo.GetLMPartialCrackCount(ctx, hashlistIDs)
		if err == nil {
			lm.PartiallyCracked = partialCount
		}
	}

	netntlmv1 := models.WindowsHashTypeStats{}
	if count, ok := counts[5500]; ok {
		netntlmv1.Total = count.Total
		netntlmv1.Cracked = count.Cracked
		netntlmv1.Percentage = calcPercentage(count.Cracked, count.Total)
	}
	// Also check 27000 (NetNTLMv1 NT variant)
	if count, ok := counts[27000]; ok {
		netntlmv1.Total += count.Total
		netntlmv1.Cracked += count.Cracked
		if netntlmv1.Total > 0 {
			netntlmv1.Percentage = calcPercentage(netntlmv1.Cracked, netntlmv1.Total)
		}
	}

	netntlmv2 := models.WindowsHashTypeStats{}
	if count, ok := counts[5600]; ok {
		netntlmv2.Total = count.Total
		netntlmv2.Cracked = count.Cracked
		netntlmv2.Percentage = calcPercentage(count.Cracked, count.Total)
	}
	// Also check 27100 (NetNTLMv2 NT variant)
	if count, ok := counts[27100]; ok {
		netntlmv2.Total += count.Total
		netntlmv2.Cracked += count.Cracked
		if netntlmv2.Total > 0 {
			netntlmv2.Percentage = calcPercentage(netntlmv2.Cracked, netntlmv2.Total)
		}
	}

	dcc := models.WindowsHashTypeStats{}
	if count, ok := counts[1100]; ok {
		dcc.Total = count.Total
		dcc.Cracked = count.Cracked
		dcc.Percentage = calcPercentage(count.Cracked, count.Total)
	}

	dcc2 := models.WindowsHashTypeStats{}
	if count, ok := counts[2100]; ok {
		dcc2.Total = count.Total
		dcc2.Cracked = count.Cracked
		dcc2.Percentage = calcPercentage(count.Cracked, count.Total)
	}

	// Kerberos stats (aggregate all types)
	kerberosTotal := 0
	kerberosCracked := 0
	kerberosTypes := make(map[string]models.WindowsHashTypeStats)

	// etype 23 (RC4)
	etype23Total := 0
	etype23Cracked := 0
	for _, typeID := range []int{7500, 13100, 18200} {
		if count, ok := counts[typeID]; ok {
			etype23Total += count.Total
			etype23Cracked += count.Cracked
			kerberosTotal += count.Total
			kerberosCracked += count.Cracked
		}
	}
	if etype23Total > 0 {
		kerberosTypes["etype_23"] = models.WindowsHashTypeStats{
			Total:      etype23Total,
			Cracked:    etype23Cracked,
			Percentage: calcPercentage(etype23Cracked, etype23Total),
		}
	}

	// etype 17 (AES128)
	etype17Total := 0
	etype17Cracked := 0
	for _, typeID := range []int{19600, 19800, 28800} {
		if count, ok := counts[typeID]; ok {
			etype17Total += count.Total
			etype17Cracked += count.Cracked
			kerberosTotal += count.Total
			kerberosCracked += count.Cracked
		}
	}
	if etype17Total > 0 {
		kerberosTypes["etype_17"] = models.WindowsHashTypeStats{
			Total:      etype17Total,
			Cracked:    etype17Cracked,
			Percentage: calcPercentage(etype17Cracked, etype17Total),
		}
	}

	// etype 18 (AES256)
	etype18Total := 0
	etype18Cracked := 0
	for _, typeID := range []int{19700, 19900, 28900} {
		if count, ok := counts[typeID]; ok {
			etype18Total += count.Total
			etype18Cracked += count.Cracked
			kerberosTotal += count.Total
			kerberosCracked += count.Cracked
		}
	}
	if etype18Total > 0 {
		kerberosTypes["etype_18"] = models.WindowsHashTypeStats{
			Total:      etype18Total,
			Cracked:    etype18Cracked,
			Percentage: calcPercentage(etype18Cracked, etype18Total),
		}
	}

	kerberos := models.KerberosStats{
		Total:      kerberosTotal,
		Cracked:    kerberosCracked,
		Percentage: calcPercentage(kerberosCracked, kerberosTotal),
		ByType:     kerberosTypes,
	}

	// Get linked hash correlation
	both, onlyNTLM, onlyLM, neither, err := s.repo.GetLinkedHashCorrelation(ctx, hashlistIDs)
	totalLinked := both + onlyNTLM + onlyLM + neither
	percentageBoth := 0.0
	if totalLinked > 0 {
		percentageBoth = float64(both) / float64(totalLinked) * 100
	}

	linkedCorrelation := models.LinkedHashCorrelationStats{
		TotalLinkedPairs: totalLinked,
		BothCracked:      both,
		OnlyNTLMCracked:  onlyNTLM,
		OnlyLMCracked:    onlyLM,
		NeitherCracked:   neither,
		PercentageBoth:   percentageBoth,
	}

	// Get unique user count
	uniqueUsers, err := s.repo.GetWindowsUniqueUserCount(ctx, hashlistIDs)
	if err != nil {
		// Log but don't fail - default to 0 if there's an error
		uniqueUsers = 0
	}

	return &models.WindowsHashStats{
		Overview: models.WindowsOverviewStats{
			TotalWindows:      totalWindows,
			CrackedWindows:    crackedWindows,
			PercentageWindows: percentageWindows,
			UniqueUsers:       uniqueUsers,
			LinkedPairs:       totalLinked,
		},
		NTLM:              ntlm,
		LM:                lm,
		NetNTLMv1:         netntlmv1,
		NetNTLMv2:         netntlmv2,
		DCC:               dcc,
		DCC2:              dcc2,
		Kerberos:          kerberos,
		LinkedCorrelation: linkedCorrelation,
	}, nil
}

// detectHashReuse analyzes hash value reuse across users (for NTLM/LM)
func (s *AnalyticsService) detectHashReuse(ctx context.Context, hashlistIDs []int64, hashTypes map[int]string) models.HashReuseStats {
	// Get hashes grouped by hash value for NTLM and LM
	hashesWithHashlists, err := s.repo.GetHashesGroupedByHashValue(ctx, hashlistIDs, []int{1000, 3000})
	if err != nil || len(hashesWithHashlists) == 0 {
		return models.HashReuseStats{
			TotalReused:   0,
			TotalUnique:   0,
			HashReuseInfo: []models.HashReuseInfo{},
		}
	}

	// Build map: hash_value -> username -> set of hashlist IDs
	hashValueUserHashlists := make(map[string]map[string]map[int64]bool)
	hashValueToType := make(map[string]int)
	hashValueToPassword := make(map[string]string)

	for _, hwh := range hashesWithHashlists {
		hashValue := hwh.Hash.HashValue
		username := "NULL"
		if hwh.Hash.Username != nil {
			username = *hwh.Hash.Username
		}

		// Track hash type and password
		hashValueToType[hashValue] = hwh.Hash.HashTypeID
		if hwh.Hash.Password != nil {
			hashValueToPassword[hashValue] = *hwh.Hash.Password
		}

		// Initialize nested maps
		if hashValueUserHashlists[hashValue] == nil {
			hashValueUserHashlists[hashValue] = make(map[string]map[int64]bool)
		}
		if hashValueUserHashlists[hashValue][username] == nil {
			hashValueUserHashlists[hashValue][username] = make(map[int64]bool)
		}

		// Track this hashlist for this user-hash combo
		hashValueUserHashlists[hashValue][username][hwh.HashlistID] = true
	}

	// Build HashReuseInfo entries for hashes used across 2+ hashlists
	hashReuseList := []models.HashReuseInfo{}
	totalReused := 0
	totalUnique := 0

	for hashValue, userHashlists := range hashValueUserHashlists {
		// Calculate total occurrences across all users
		users := []models.UserOccurrence{}
		totalOccurrences := 0

		for username, hashlists := range userHashlists {
			hashlistCount := len(hashlists)
			users = append(users, models.UserOccurrence{
				Username:      username,
				HashlistCount: hashlistCount,
			})
			totalOccurrences += hashlistCount
		}

		// Check if hash is reused based on total occurrences
		if totalOccurrences >= 2 {
			// Sort users alphabetically
			sort.Slice(users, func(i, j int) bool {
				return users[i].Username < users[j].Username
			})

			// Get hash type name
			hashTypeName := "Unknown"
			if typeID, ok := hashValueToType[hashValue]; ok {
				if name, found := hashTypes[typeID]; found {
					hashTypeName = name
				}
			}

			// Get password if available
			var password *string
			if pwd, ok := hashValueToPassword[hashValue]; ok {
				password = &pwd
			}

			hashReuseList = append(hashReuseList, models.HashReuseInfo{
				HashValue:        hashValue,
				HashType:         hashTypeName,
				Password:         password,
				Users:            users,
				TotalOccurrences: totalOccurrences,
				UserCount:        len(users),
			})
			totalReused += totalOccurrences
		} else {
			totalUnique += totalOccurrences
		}
	}

	// Sort by total occurrences (descending)
	sort.Slice(hashReuseList, func(i, j int) bool {
		return hashReuseList[i].TotalOccurrences > hashReuseList[j].TotalOccurrences
	})

	// Limit to top 50
	if len(hashReuseList) > 50 {
		hashReuseList = hashReuseList[:50]
	}

	total := totalReused + totalUnique
	percentageReused := 0.0
	if total > 0 {
		percentageReused = float64(totalReused) / float64(total) * 100
	}

	return models.HashReuseStats{
		TotalReused:      totalReused,
		PercentageReused: percentageReused,
		TotalUnique:      totalUnique,
		HashReuseInfo:    hashReuseList,
	}
}

// calculateLMPartialCracks generates LM partial crack statistics
func (s *AnalyticsService) calculateLMPartialCracks(ctx context.Context, hashlistIDs []int64, totalLMHashes int) (*models.LMPartialCrackStats, error) {
	// Get partial crack details (limit to 50)
	details, firstHalfOnly, secondHalfOnly, err := s.repo.GetLMPartialCracks(ctx, hashlistIDs, 50)
	if err != nil {
		return nil, err
	}

	totalPartial := len(details)
	if totalPartial == 0 {
		return nil, nil
	}

	percentagePartial := 0.0
	if totalLMHashes > 0 {
		percentagePartial = float64(totalPartial) / float64(totalLMHashes) * 100
	}

	return &models.LMPartialCrackStats{
		TotalPartial:        totalPartial,
		FirstHalfOnly:       firstHalfOnly,
		SecondHalfOnly:      secondHalfOnly,
		PercentagePartial:   percentagePartial,
		PartialCrackDetails: details,
	}, nil
}

// generateLMToNTLMMasks generates hashcat masks for cracking NTLM from LM passwords
func (s *AnalyticsService) generateLMToNTLMMasks(lmPasswords []*models.Hash) *models.LMToNTLMMaskStats {
	if len(lmPasswords) == 0 {
		return nil
	}

	// Pattern analysis: map LM pattern to count and examples
	type PatternInfo struct {
		Count     int
		Examples  []string
		Masks     []string
	}
	patterns := make(map[string]*PatternInfo)

	for _, hash := range lmPasswords {
		if hash.Password == nil {
			continue
		}

		password := *hash.Password
		pattern := generateLMPattern(password)

		if patterns[pattern] == nil {
			patterns[pattern] = &PatternInfo{
				Count:    0,
				Examples: []string{},
			}
		}

		patterns[pattern].Count++
		if len(patterns[pattern].Examples) < 5 {
			patterns[pattern].Examples = append(patterns[pattern].Examples, password)
		}
	}

	// Generate masks for each pattern
	totalLM := len(lmPasswords)
	var maskInfos []models.LMNTLMMaskInfo
	totalKeyspace := int64(0)

	for pattern, info := range patterns {
		masks := generateMasksForPattern(pattern)

		for _, mask := range masks {
			keyspace := calculateMaskKeyspace(mask)
			matchPercentage := safePercentage(info.Count, totalLM)
			percentage := matchPercentage // For individual mask

			example := ""
			if len(info.Examples) > 0 {
				example = info.Examples[0]
			}

			maskInfos = append(maskInfos, models.LMNTLMMaskInfo{
				Mask:              mask,
				LMPattern:         pattern,
				Count:             info.Count,
				Percentage:        percentage,
				MatchPercentage:   matchPercentage,
				EstimatedKeyspace: keyspace,
				ExampleLM:         example,
			})

			totalKeyspace += keyspace
		}
	}

	// Sort by match percentage (most effective first)
	sort.Slice(maskInfos, func(i, j int) bool {
		return maskInfos[i].MatchPercentage > maskInfos[j].MatchPercentage
	})

	// Limit to top 50
	if len(maskInfos) > 50 {
		maskInfos = maskInfos[:50]
	}

	return &models.LMToNTLMMaskStats{
		TotalLMCracked:        totalLM,
		TotalMasksGenerated:   len(maskInfos),
		Masks:                 maskInfos,
		TotalEstimatedKeyspace: totalKeyspace,
	}
}

// generateLMPattern generates a pattern string from an LM password
// A = alpha, D = digit, S = special
func generateLMPattern(password string) string {
	pattern := ""
	for _, char := range password {
		if unicode.IsLetter(char) {
			pattern += "A"
		} else if unicode.IsDigit(char) {
			pattern += "D"
		} else {
			pattern += "S"
		}
	}
	return pattern
}

// generateMasksForPattern generates hashcat masks for a given pattern
func generateMasksForPattern(pattern string) []string {
	masks := []string{}

	// Generate title case mask (most common)
	titleMask := ""
	for i, char := range pattern {
		if char == 'A' {
			if i == 0 {
				titleMask += "?u"
			} else {
				titleMask += "?l"
			}
		} else if char == 'D' {
			titleMask += "?d"
		} else {
			titleMask += "?s"
		}
	}
	masks = append(masks, titleMask)

	// Generate all uppercase mask
	allUpperMask := ""
	for _, char := range pattern {
		if char == 'A' {
			allUpperMask += "?u"
		} else if char == 'D' {
			allUpperMask += "?d"
		} else {
			allUpperMask += "?s"
		}
	}
	if allUpperMask != titleMask {
		masks = append(masks, allUpperMask)
	}

	// Generate all lowercase mask
	allLowerMask := ""
	for _, char := range pattern {
		if char == 'A' {
			allLowerMask += "?l"
		} else if char == 'D' {
			allLowerMask += "?d"
		} else {
			allLowerMask += "?s"
		}
	}
	if allLowerMask != titleMask && allLowerMask != allUpperMask {
		masks = append(masks, allLowerMask)
	}

	return masks
}

// calculateMaskKeyspace estimates the keyspace for a hashcat mask
func calculateMaskKeyspace(mask string) int64 {
	keyspace := int64(1)
	i := 0
	for i < len(mask) {
		if mask[i] == '?' && i+1 < len(mask) {
			switch mask[i+1] {
			case 'l': // lowercase
				keyspace *= 26
			case 'u': // uppercase
				keyspace *= 26
			case 'd': // digits
				keyspace *= 10
			case 's': // special
				keyspace *= 32
			case 'a': // all printable
				keyspace *= 95
			}
			i += 2
		} else {
			i++
		}
	}
	return keyspace
}

// ==================== Domain-Specific Windows Hash Service Methods ====================

// calculateWindowsHashStatsDomain generates Windows hash statistics for a specific domain
func (s *AnalyticsService) calculateWindowsHashStatsDomain(ctx context.Context, hashlistIDs []int64, domain string) (*models.WindowsHashStats, error) {
	// Get Windows hash counts for this domain (raw counts for individual cards)
	counts, err := s.repo.GetWindowsHashCountsDomain(ctx, hashlistIDs, domain)
	if err != nil {
		return nil, fmt.Errorf("failed to get Windows hash counts for domain %s: %w", domain, err)
	}

	// Get effective overview counts (linked-aware for overview section)
	totalWindows, crackedWindows, err := s.repo.GetWindowsOverviewCountsDomain(ctx, hashlistIDs, domain)
	if err != nil {
		return nil, fmt.Errorf("failed to get Windows overview counts for domain %s: %w", domain, err)
	}

	// If no Windows hashes in this domain, return nil
	if totalWindows == 0 {
		return nil, nil
	}

	percentageWindows := 0.0
	if totalWindows > 0 {
		percentageWindows = float64(crackedWindows) / float64(totalWindows) * 100
	}

	// Build individual hash type stats
	ntlm := models.WindowsHashTypeStats{}
	lm := models.LMHashStats{}
	netntlmv1 := models.WindowsHashTypeStats{}
	netntlmv2 := models.WindowsHashTypeStats{}
	dcc := models.WindowsHashTypeStats{}
	dcc2 := models.WindowsHashTypeStats{}
	kerberos := models.KerberosStats{}

	// NTLM (1000)
	if count, ok := counts[1000]; ok {
		ntlm.Total = count.Total
		ntlm.Cracked = count.Cracked
		if count.Total > 0 {
			ntlm.Percentage = float64(count.Cracked) / float64(count.Total) * 100
		}
	}

	// LM (3000)
	if count, ok := counts[3000]; ok {
		lm.Total = count.Total
		lm.Cracked = count.Cracked
		if count.Total > 0 {
			lm.Percentage = float64(count.Cracked) / float64(count.Total) * 100
		}

		// Get LM password length distribution
		underEight, eightToFourteen, err := s.repo.GetLMPasswordLengthsDomain(ctx, hashlistIDs, domain)
		if err != nil {
			return nil, fmt.Errorf("failed to get LM password lengths for domain %s: %w", domain, err)
		}
		lm.UnderEight = underEight
		lm.EightToFourteen = eightToFourteen

		// Get LM partial crack count
		partialCount, err := s.repo.GetLMPartialCrackCountDomain(ctx, hashlistIDs, domain)
		if err != nil {
			return nil, fmt.Errorf("failed to get LM partial crack count for domain %s: %w", domain, err)
		}
		lm.PartiallyCracked = partialCount
	}

	// NetNTLMv1 (5500 + 27000)
	if count, ok := counts[5500]; ok {
		netntlmv1.Total += count.Total
		netntlmv1.Cracked += count.Cracked
	}
	if count, ok := counts[27000]; ok {
		netntlmv1.Total += count.Total
		netntlmv1.Cracked += count.Cracked
	}
	if netntlmv1.Total > 0 {
		netntlmv1.Percentage = float64(netntlmv1.Cracked) / float64(netntlmv1.Total) * 100
	}

	// NetNTLMv2 (5600 + 27100)
	if count, ok := counts[5600]; ok {
		netntlmv2.Total += count.Total
		netntlmv2.Cracked += count.Cracked
	}
	if count, ok := counts[27100]; ok {
		netntlmv2.Total += count.Total
		netntlmv2.Cracked += count.Cracked
	}
	if netntlmv2.Total > 0 {
		netntlmv2.Percentage = float64(netntlmv2.Cracked) / float64(netntlmv2.Total) * 100
	}

	// DCC (1100)
	if count, ok := counts[1100]; ok {
		dcc.Total = count.Total
		dcc.Cracked = count.Cracked
		if count.Total > 0 {
			dcc.Percentage = float64(count.Cracked) / float64(count.Total) * 100
		}
	}

	// DCC2 (2100)
	if count, ok := counts[2100]; ok {
		dcc2.Total = count.Total
		dcc2.Cracked = count.Cracked
		if count.Total > 0 {
			dcc2.Percentage = float64(count.Cracked) / float64(count.Total) * 100
		}
	}

	// Kerberos aggregate and by encryption type
	kerberosTypes := make(map[string]models.WindowsHashTypeStats)

	// RC4/etype 23: 7500, 13100, 18200
	rc4Total, rc4Cracked := 0, 0
	for _, typeID := range []int{7500, 13100, 18200} {
		if count, ok := counts[typeID]; ok {
			rc4Total += count.Total
			rc4Cracked += count.Cracked
			kerberos.Total += count.Total
			kerberos.Cracked += count.Cracked
		}
	}
	if rc4Total > 0 {
		kerberosTypes["RC4 (etype 23)"] = models.WindowsHashTypeStats{
			Total:      rc4Total,
			Cracked:    rc4Cracked,
			Percentage: float64(rc4Cracked) / float64(rc4Total) * 100,
		}
	}

	// AES128/etype 17: 19600, 19800, 28800
	aes128Total, aes128Cracked := 0, 0
	for _, typeID := range []int{19600, 19800, 28800} {
		if count, ok := counts[typeID]; ok {
			aes128Total += count.Total
			aes128Cracked += count.Cracked
			kerberos.Total += count.Total
			kerberos.Cracked += count.Cracked
		}
	}
	if aes128Total > 0 {
		kerberosTypes["AES128 (etype 17)"] = models.WindowsHashTypeStats{
			Total:      aes128Total,
			Cracked:    aes128Cracked,
			Percentage: float64(aes128Cracked) / float64(aes128Total) * 100,
		}
	}

	// AES256/etype 18: 19700, 19900, 28900
	aes256Total, aes256Cracked := 0, 0
	for _, typeID := range []int{19700, 19900, 28900} {
		if count, ok := counts[typeID]; ok {
			aes256Total += count.Total
			aes256Cracked += count.Cracked
			kerberos.Total += count.Total
			kerberos.Cracked += count.Cracked
		}
	}
	if aes256Total > 0 {
		kerberosTypes["AES256 (etype 18)"] = models.WindowsHashTypeStats{
			Total:      aes256Total,
			Cracked:    aes256Cracked,
			Percentage: float64(aes256Cracked) / float64(aes256Total) * 100,
		}
	}

	if kerberos.Total > 0 {
		kerberos.Percentage = float64(kerberos.Cracked) / float64(kerberos.Total) * 100
		kerberos.ByType = kerberosTypes
	}

	// Get linked hash correlation
	both, onlyNTLM, onlyLM, neither, err := s.repo.GetLinkedHashCorrelationDomain(ctx, hashlistIDs, domain)
	if err != nil {
		return nil, fmt.Errorf("failed to get linked hash correlation for domain %s: %w", domain, err)
	}

	totalLinked := both + onlyNTLM + onlyLM + neither
	percentageBoth := 0.0
	if totalLinked > 0 {
		percentageBoth = float64(both) / float64(totalLinked) * 100
	}

	linkedCorrelation := models.LinkedHashCorrelationStats{
		TotalLinkedPairs: totalLinked,
		BothCracked:      both,
		PercentageBoth:   percentageBoth,
		OnlyNTLMCracked:  onlyNTLM,
		OnlyLMCracked:    onlyLM,
		NeitherCracked:   neither,
	}

	// Get unique user count for this domain
	uniqueUsers, err := s.repo.GetWindowsUniqueUserCountDomain(ctx, hashlistIDs, domain)
	if err != nil {
		// Log but don't fail - default to 0 if there's an error
		uniqueUsers = 0
	}

	return &models.WindowsHashStats{
		Overview: models.WindowsOverviewStats{
			TotalWindows:      totalWindows,
			CrackedWindows:    crackedWindows,
			PercentageWindows: percentageWindows,
			UniqueUsers:       uniqueUsers,
			LinkedPairs:       totalLinked,
		},
		NTLM:              ntlm,
		LM:                lm,
		NetNTLMv1:         netntlmv1,
		NetNTLMv2:         netntlmv2,
		DCC:               dcc,
		DCC2:              dcc2,
		Kerberos:          kerberos,
		LinkedCorrelation: linkedCorrelation,
	}, nil
}

// detectHashReuseDomain analyzes hash value reuse across users for a specific domain (for NTLM/LM)
func (s *AnalyticsService) detectHashReuseDomain(ctx context.Context, hashlistIDs []int64, hashTypes map[int]string, domain string) models.HashReuseStats {
	// Only analyze NTLM (1000) and LM (3000) for hash reuse
	windowsHashTypes := []int{1000, 3000}

	// Get hashes grouped by hash_value for this domain
	hashes, err := s.repo.GetHashesGroupedByHashValueDomain(ctx, hashlistIDs, windowsHashTypes, domain)
	if err != nil {
		// Return empty stats on error
		return models.HashReuseStats{
			TotalReused:      0,
			PercentageReused: 0,
			TotalUnique:      0,
			HashReuseInfo:    []models.HashReuseInfo{},
		}
	}

	// Group hashes by hash_value
	hashValueMap := make(map[string][]repository.HashWithHashlist)
	for _, h := range hashes {
		hashValueMap[h.Hash.HashValue] = append(hashValueMap[h.Hash.HashValue], h)
	}

	// Find reused hashes (appearing 2+ times)
	var reuseInfo []models.HashReuseInfo
	totalReused := 0
	totalUnique := len(hashValueMap)

	for hashValue, hashList := range hashValueMap {
		if len(hashList) < 2 {
			continue // Not reused
		}

		totalReused += len(hashList)

		// Group users by hashlist
		userOccurrences := make(map[string]int) // username -> count
		for _, h := range hashList {
			if h.Hash.Username != nil {
				userOccurrences[*h.Hash.Username]++
			}
		}

		// Convert to UserOccurrence slice
		var users []models.UserOccurrence
		for username, count := range userOccurrences {
			users = append(users, models.UserOccurrence{
				Username:      username,
				HashlistCount: count,
			})
		}

		// Sort users by username
		sort.Slice(users, func(i, j int) bool {
			return users[i].Username < users[j].Username
		})

		// Get hash type name
		hashTypeName := fmt.Sprintf("Mode %d", hashList[0].Hash.HashTypeID)
		if name, exists := hashTypes[hashList[0].Hash.HashTypeID]; exists {
			hashTypeName = name
		}

		var password *string
		if hashList[0].Hash.Password != nil {
			pwd := *hashList[0].Hash.Password
			password = &pwd
		}

		reuseInfo = append(reuseInfo, models.HashReuseInfo{
			HashValue:        hashValue,
			HashType:         hashTypeName,
			Password:         password,
			Users:            users,
			TotalOccurrences: len(hashList),
			UserCount:        len(userOccurrences),
		})
	}

	// Sort by user count (most reused first), limit to 50
	sort.Slice(reuseInfo, func(i, j int) bool {
		return reuseInfo[i].UserCount > reuseInfo[j].UserCount
	})
	if len(reuseInfo) > 50 {
		reuseInfo = reuseInfo[:50]
	}

	percentageReused := 0.0
	if len(hashes) > 0 {
		percentageReused = float64(totalReused) / float64(len(hashes)) * 100
	}

	return models.HashReuseStats{
		TotalReused:      totalReused,
		PercentageReused: percentageReused,
		TotalUnique:      totalUnique,
		HashReuseInfo:    reuseInfo,
	}
}

// calculateLMPartialCracksDomain generates LM partial crack statistics for a specific domain
func (s *AnalyticsService) calculateLMPartialCracksDomain(ctx context.Context, hashlistIDs []int64, domain string, totalLMHashes int) (*models.LMPartialCrackStats, error) {
	if totalLMHashes == 0 {
		return nil, nil
	}

	// Get partial crack count for this domain
	partialCount, err := s.repo.GetLMPartialCrackCountDomain(ctx, hashlistIDs, domain)
	if err != nil {
		return nil, fmt.Errorf("failed to get LM partial crack count for domain %s: %w", domain, err)
	}

	if partialCount == 0 {
		return nil, nil
	}

	// Get partial crack details (limit 50)
	details, firstHalfOnly, secondHalfOnly, err := s.repo.GetLMPartialCracksDomain(ctx, hashlistIDs, domain, 50)
	if err != nil {
		return nil, fmt.Errorf("failed to get LM partial crack details for domain %s: %w", domain, err)
	}

	percentagePartial := 0.0
	if totalLMHashes > 0 {
		percentagePartial = float64(partialCount) / float64(totalLMHashes) * 100
	}

	return &models.LMPartialCrackStats{
		TotalPartial:        partialCount,
		FirstHalfOnly:       firstHalfOnly,
		SecondHalfOnly:      secondHalfOnly,
		PercentagePartial:   percentagePartial,
		PartialCrackDetails: details,
	}, nil
}
