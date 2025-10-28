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

	// Generate all analytics for "All" view
	analyticsData := &models.AnalyticsData{
		Overview:            s.calculateOverview(totalHashes, totalCracked, hashCounts, hashTypes, domainBreakdown),
		LengthDistribution:  s.calculateLengthDistribution(passwords),
		ComplexityAnalysis:  s.calculateComplexity(passwords),
		PositionalAnalysis:  s.calculatePositionalAnalysis(passwords),
		PatternDetection:    s.detectPatterns(passwords),
		UsernameCorrelation: s.analyzeUsernameCorrelation(passwords),
		PasswordReuse:       s.detectPasswordReuse(passwordsWithHashlists),
		TemporalPatterns:    s.detectTemporalPatterns(passwords),
		MaskAnalysis:        s.analyzeMasks(passwords),
		CustomPatterns:      s.checkCustomPatterns(passwords, report.CustomPatterns, report.ClientID.String()),
		StrengthMetrics:     s.calculateStrengthMetrics(passwords, speeds),
		TopPasswords:        s.getTopPasswords(passwords, 50),
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

			// Calculate all analytics for this domain
			domainAnalytic := models.DomainAnalytics{
				Domain:              domain,
				Overview:            s.calculateOverview(domainTotal, domainCracked, domainHashCounts, hashTypes, nil),
				LengthDistribution:  s.calculateLengthDistribution(domainPasswords),
				ComplexityAnalysis:  s.calculateComplexity(domainPasswords),
				PositionalAnalysis:  s.calculatePositionalAnalysis(domainPasswords),
				PatternDetection:    s.detectPatterns(domainPasswords),
				UsernameCorrelation: s.analyzeUsernameCorrelation(domainPasswords),
				PasswordReuse:       s.detectPasswordReuse(domainPasswordsWithHashlists),
				TemporalPatterns:    s.detectTemporalPatterns(domainPasswords),
				MaskAnalysis:        s.analyzeMasks(domainPasswords),
				CustomPatterns:      s.checkCustomPatterns(domainPasswords, report.CustomPatterns, report.ClientID.String()),
				StrengthMetrics:     s.calculateStrengthMetrics(domainPasswords, speeds),
				TopPasswords:        s.getTopPasswords(domainPasswords, 50),
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

	// Update summary fields (total_hashlists, total_hashes, total_cracked)
	totalHashlists := len(hashlistIDs)
	if err := s.repo.UpdateSummaryFields(ctx, reportID, totalHashlists, totalHashes, totalCracked); err != nil {
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
		cat.Percentage = float64(cat.Count) / float64(total) * 100
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

	avgLength := float64(totalLength) / float64(total)
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
		FourTypes:    models.CategoryCount{Count: fourTypesCount, Percentage: float64(fourTypesCount) / float64(total) * 100},
		ComplexShort: models.CategoryCount{Count: complexShortCount, Percentage: float64(complexShortCount) / float64(total) * 100},
		ComplexLong:  models.CategoryCount{Count: complexLongCount, Percentage: float64(complexLongCount) / float64(total) * 100},
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
			Percentage: float64(count) / float64(total) * 100,
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
		StartsUppercase: models.CategoryCount{Count: startsUpper, Percentage: float64(startsUpper) / float64(total) * 100},
		EndsNumber:      models.CategoryCount{Count: endsNumber, Percentage: float64(endsNumber) / float64(total) * 100},
		EndsSpecial:     models.CategoryCount{Count: endsSpecial, Percentage: float64(endsSpecial) / float64(total) * 100},
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
		KeyboardWalks:   models.CategoryCount{Count: keyboardWalks, Percentage: float64(keyboardWalks) / float64(total) * 100},
		Sequential:      models.CategoryCount{Count: sequential, Percentage: float64(sequential) / float64(total) * 100},
		RepeatingChars:  models.CategoryCount{Count: repeating, Percentage: float64(repeating) / float64(total) * 100},
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
		EqualsUsername:     models.CategoryCount{Count: equals, Percentage: float64(equals) / float64(total) * 100},
		ContainsUsername:   models.CategoryCount{Count: contains, Percentage: float64(contains) / float64(total) * 100},
		UsernamePlusSuffix: models.CategoryCount{Count: suffix, Percentage: float64(suffix) / float64(total) * 100},
		ReversedUsername:   models.CategoryCount{Count: reversed, Percentage: float64(reversed) / float64(total) * 100},
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
		ContainsYear:   models.CategoryCount{Count: containsYear, Percentage: float64(containsYear) / float64(total) * 100},
		ContainsMonth:  models.CategoryCount{Count: containsMonth, Percentage: float64(containsMonth) / float64(total) * 100},
		ContainsSeason: models.CategoryCount{Count: containsSeason, Percentage: float64(containsSeason) / float64(total) * 100},
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
			Percentage: float64(masks[i].count) / float64(total) * 100,
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
		Low:      models.CategoryCount{Count: lowEntropy, Percentage: float64(lowEntropy) / float64(total) * 100},
		Moderate: models.CategoryCount{Count: moderateEntropy, Percentage: float64(moderateEntropy) / float64(total) * 100},
		High:     models.CategoryCount{Count: highEntropy, Percentage: float64(highEntropy) / float64(total) * 100},
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
		PercentUnder1Hour:   float64(under1Hour) / float64(total) * 100,
		PercentUnder1Day:    float64(under1Day) / float64(total) * 100,
		PercentUnder1Week:   float64(under1Week) / float64(total) * 100,
		PercentUnder1Month:  float64(under1Month) / float64(total) * 100,
		PercentUnder6Months: float64(under6Months) / float64(total) * 100,
		PercentUnder1Year:   float64(under1Year) / float64(total) * 100,
		PercentOver1Year:    float64(over1Year) / float64(total) * 100,
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

	for password, count := range passwordCounts {
		if count >= 2 { // Only include passwords used 2+ times
			topList = append(topList, models.TopPassword{
				Password:   password,
				Count:      count,
				Percentage: float64(count) / float64(len(passwords)) * 100,
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
		percent := float64(count) / float64(total) * 100
		recs = append(recs, models.Recommendation{
			Severity:   "CRITICAL",
			Count:      count,
			Percentage: percent,
			Message:    fmt.Sprintf("%d passwords (%.2f%%) were below 8 characters. Meet industry standard of 12 characters minimum, but recommend 15+ characters for optimal security.", count, percent),
		})
	}

	if data.LengthDistribution.Count8to11 > 0 {
		count := data.LengthDistribution.Count8to11
		percent := float64(count) / float64(total) * 100
		recs = append(recs, models.Recommendation{
			Severity:   "HIGH",
			Count:      count,
			Percentage: percent,
			Message:    fmt.Sprintf("%d passwords (%.2f%%) were between 8 and 11 characters. Meet industry standard of 12 characters minimum, but recommend 15+ characters for optimal security.", count, percent),
		})
	}

	if data.LengthDistribution.CountUnder15 > 0 {
		count := data.LengthDistribution.CountUnder15
		percent := float64(count) / float64(total) * 100
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
	if float64(singleTypeCount)/float64(total)*100 > 40 {
		percent := float64(singleTypeCount) / float64(total) * 100
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

	return recs
}
