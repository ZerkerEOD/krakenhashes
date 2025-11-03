package hashutils

import (
	"strings"
	"unicode"
)

// --- Username and Domain Extraction ---

// UsernameAndDomain holds extracted username and optional domain
type UsernameAndDomain struct {
	Username *string
	Domain   *string
}

// ParseDomainUsername attempts to parse domain from username
// Supports formats: DOMAIN\username and username@domain
func ParseDomainUsername(rawUsername string) (username string, domain *string) {
	// Check for DOMAIN\username format
	if idx := strings.Index(rawUsername, `\`); idx != -1 {
		domainPart := rawUsername[:idx]
		usernamePart := rawUsername[idx+1:]
		return usernamePart, &domainPart
	}

	// Check for username@domain format
	if idx := strings.Index(rawUsername, "@"); idx != -1 {
		usernamePart := rawUsername[:idx]
		domainPart := rawUsername[idx+1:]
		return usernamePart, &domainPart
	}

	// No domain found
	return rawUsername, nil
}

// --- Username Extraction ---

// UsernameRule defines how to extract a username from a hash string
type UsernameRule struct {
	Separator     string
	UsernameIndex int // 0 for first, -1 for last relative to separator splits.
}

// --- Custom Username/Domain Extractors ---

// CustomExtractor is a function that extracts username and domain from a hash
type CustomExtractor func(rawHash string) *UsernameAndDomain

// customUsernameExtractors maps hash_type_id to custom extraction functions
var customUsernameExtractors = map[int]CustomExtractor{
	1000:  extractNTLM,         // NTLM
	1100:  extractDCC,           // Domain Cached Credentials
	5500:  extractNetNTLMv1,     // NetNTLMv1
	5600:  extractNetNTLMv2,     // NetNTLMv2
	6800:  extractLastPass,      // LastPass
	18200: extractKerberos,      // Kerberos AS-REP
	27000: extractNetNTLMv1,     // NetNTLMv1 (NT) - same format as 5500
	27100: extractNetNTLMv2,     // NetNTLMv2 (NT) - same format as 5600
	35400: extractKerberos,      // Kerberos AS-REP (NT) - same format as 18200
}

// extractNTLM extracts username and domain from NTLM pwdump format
// Format: [DOMAIN\]username:rid:LMHASH:NTHASH:::
// Only extracts from pwdump format (4+ parts), not from LM:NT or bare hash
func extractNTLM(rawHash string) *UsernameAndDomain {
	parts := strings.Split(rawHash, ":")
	// Only extract from pwdump format with 4+ parts
	if len(parts) >= 4 && len(parts[3]) == 32 && isHexString(parts[3]) {
		rawUsername := parts[0]
		username, domain := ParseDomainUsername(rawUsername)
		return &UsernameAndDomain{
			Username: &username,
			Domain:   domain,
		}
	}
	return nil // LM:NT or bare hash formats have no username
}

// extractDCC extracts username from Domain Cached Credentials
// Format: hash:username
func extractDCC(rawHash string) *UsernameAndDomain {
	parts := strings.Split(rawHash, ":")
	if len(parts) >= 2 {
		username := parts[len(parts)-1] // Last part is username
		return &UsernameAndDomain{
			Username: &username,
			Domain:   nil,
		}
	}
	return nil
}

// extractNetNTLMv1 extracts username and domain from NetNTLMv1
// Format: username::domain:challenge:response
func extractNetNTLMv1(rawHash string) *UsernameAndDomain {
	// Find the :: separator
	idx := strings.Index(rawHash, "::")
	if idx == -1 {
		return nil
	}

	username := rawHash[:idx]

	// Try to extract domain (after ::)
	remainder := rawHash[idx+2:]
	parts := strings.Split(remainder, ":")
	var domain *string
	if len(parts) > 0 && parts[0] != "" {
		domain = &parts[0]
	}

	return &UsernameAndDomain{
		Username: &username,
		Domain:   domain,
	}
}

// extractNetNTLMv2 extracts username and domain from NetNTLMv2
// Format: username::domain:challenge:response
// Same format as NetNTLMv1
func extractNetNTLMv2(rawHash string) *UsernameAndDomain {
	return extractNetNTLMv1(rawHash) // Same format
}

// extractKerberos extracts username and domain from Kerberos AS-REP
// Format: $krb5asrep$23$user@domain.com:...
// Handles machine accounts with $ in username (e.g., COMPUTER$@domain.com)
func extractKerberos(rawHash string) *UsernameAndDomain {
	// Format: $krb5asrep$23$user@domain:hash...
	// Need to find the 3rd $ and extract everything after it
	// This preserves any $ in machine account names (e.g., WKS01$)

	// Find first $
	idx1 := strings.Index(rawHash, "$")
	if idx1 == -1 {
		return nil
	}

	// Find second $ (after first)
	idx2 := strings.Index(rawHash[idx1+1:], "$")
	if idx2 == -1 {
		return nil
	}
	idx2 += idx1 + 1 // Adjust to absolute position

	// Find third $ (after second)
	idx3 := strings.Index(rawHash[idx2+1:], "$")
	if idx3 == -1 {
		return nil
	}
	idx3 += idx2 + 1 // Adjust to absolute position

	// Everything after the 3rd $ is the user@domain:hash part
	userDomainPart := rawHash[idx3+1:]

	// Find the colon that separates user@domain from hash
	colonIdx := strings.Index(userDomainPart, ":")
	if colonIdx != -1 {
		userDomainPart = userDomainPart[:colonIdx]
	}

	// Parse username@domain (handles @ separator)
	username, domain := ParseDomainUsername(userDomainPart)
	return &UsernameAndDomain{
		Username: &username,
		Domain:   domain,
	}
}

// extractLastPass extracts username (email) from LastPass
// Format: hash:iterations:email
func extractLastPass(rawHash string) *UsernameAndDomain {
	parts := strings.Split(rawHash, ":")
	if len(parts) >= 3 {
		email := parts[2] // Email is third field
		return &UsernameAndDomain{
			Username: &email,
			Domain:   nil,
		}
	}
	return nil
}

// usernameExtractionRules maps hash_type_id to a specific extraction rule.
// Add more rules here as specific hash types are analyzed.
// NOTE: Custom extractors (above) take precedence over these simple rules
var usernameExtractionRules = map[int]UsernameRule{
	// Rules based on known formats (username often last after ':')
	12:   {Separator: ":", UsernameIndex: -1}, // PostgreSQL
	22:   {Separator: ":", UsernameIndex: -1}, // Juniper NetScreen/SSG
	23:   {Separator: ":", UsernameIndex: -1}, // Skype
	4711: {Separator: ":", UsernameIndex: -1}, // Huawei sha1(md5($pass).$salt)
	// Add more rules as needed...
}

// isValidUsernameCandidate performs basic checks on a potential username found by heuristic.
func isValidUsernameCandidate(candidate string) bool {
	if len(candidate) == 0 || len(candidate) > 128 { // Reject empty or excessively long strings
		return false
	}
	hasLetterOrDigit := false
	allHex := true
	for _, r := range candidate {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			hasLetterOrDigit = true
		}
		// Basic check for non-printable or space characters. Adjust if needed.
		if r <= ' ' || r > '~' {
			return false
		}
		// Check if it looks purely hexadecimal (potential hash fragment)
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			allHex = false
		}
	}
	// Reject if empty, or if it looks like purely hex and is long (likely a hash)
	if !hasLetterOrDigit || (allHex && len(candidate) > 16) {
		return false
	}

	return true
}

// ExtractUsernameAndDomain attempts to extract username and domain based on custom extractors,
// defined rules, or a heuristic focusing on the start of the hash.
// Returns nil if no username is confidently found.
func ExtractUsernameAndDomain(rawHash string, hashTypeID int) *UsernameAndDomain {
	// 1. Check for custom extractors first (highest priority)
	if extractor, exists := customUsernameExtractors[hashTypeID]; exists {
		result := extractor(rawHash)
		// If extractor found username, parse domain from it if not already extracted
		if result != nil && result.Username != nil && result.Domain == nil {
			username, domain := ParseDomainUsername(*result.Username)
			result.Username = &username
			result.Domain = domain
		}
		return result
	}

	// 2. Check for specific rules
	rule, exists := usernameExtractionRules[hashTypeID]
	if exists {
		parts := strings.Split(rawHash, rule.Separator)
		var username string
		isValidIndex := false

		if rule.UsernameIndex == -1 { // Last element
			if len(parts) > 1 {
				username = parts[len(parts)-1]
				isValidIndex = true
			}
		} else if rule.UsernameIndex >= 0 && rule.UsernameIndex < len(parts) { // Specific index
			username = parts[rule.UsernameIndex]
			isValidIndex = true
		}

		// If rule existed and we extracted based on index, return it
		if isValidIndex {
			// Parse domain from username if present
			finalUsername, domain := ParseDomainUsername(username)
			return &UsernameAndDomain{
				Username: &finalUsername,
				Domain:   domain,
			}
		}
		// If rule existed but index was out of bounds, don't proceed to heuristic.
		return nil
	}

	// 3. Apply Heuristic: Check start of string if no rule exists
	// NOTE: Only use colon as separator to preserve machine account $ suffix
	firstColon := strings.Index(rawHash, ":")

	// If a colon exists and is not the very first character
	if firstColon > 0 {
		potentialUsername := rawHash[0:firstColon]
		if isValidUsernameCandidate(potentialUsername) {
			// Parse domain from username if present
			finalUsername, domain := ParseDomainUsername(potentialUsername)
			return &UsernameAndDomain{
				Username: &finalUsername,
				Domain:   domain,
			}
		}
	}

	// 4. No username found by any method
	return nil
}

// ExtractUsername is a convenience wrapper that returns just the username
// Kept for backward compatibility with existing code
func ExtractUsername(rawHash string, hashTypeID int) *string {
	result := ExtractUsernameAndDomain(rawHash, hashTypeID)
	if result != nil {
		return result.Username
	}
	return nil
}

// --- Hash Processing ---

// HashProcessor defines a function type for processing a raw hash string.
type HashProcessor func(rawHash string) string

// hashProcessingRules maps hash_type_id to a specific processing function.
// Add more functions for types where needs_processing is true.
var hashProcessingRules = map[int]HashProcessor{
	1000: processNTLM, // NTLM needs specific NT hash extraction
	3000: processLM,   // LM needs specific LM hash extraction
}

// ProcessHashIfNeeded processes the hash only if required by the hash type's needs_processing flag.
// Otherwise, it returns the original hash.
func ProcessHashIfNeeded(rawHash string, hashTypeID int, needsProcessing bool) string {
	if !needsProcessing {
		return rawHash // No processing needed for this type
	}

	processor, exists := hashProcessingRules[hashTypeID]
	if !exists {
		// Log warning? logging.Warnf("Hash type %d needs processing, but no processor function found.", hashTypeID)
		return rawHash // No specific processor, return original as fallback
	}

	// Log info? logging.Debugf("Processing hash type %d using specific processor.", hashTypeID)
	return processor(rawHash)
}

// processNTLM extracts just the NTHASH portion for Hashcat mode 1000 processing.
// It prioritizes formats like user:rid:LM:NT::: and then LM:NT, finally just NT.
func processNTLM(rawHash string) string {
	parts := strings.Split(rawHash, ":")

	// Check common NTLM dump formats:
	// 1. user:rid:LMHASH:NTHASH:... (NT is index 3)
	if len(parts) >= 4 && len(parts[3]) == 32 && isHexString(parts[3]) {
		return parts[3] // Return NT hash
	}
	// 2. LMHASH:NTHASH (NT is index 1) - Common Hashcat input
	if len(parts) >= 2 && len(parts[1]) == 32 && isHexString(parts[1]) {
		return parts[1] // Return NT hash
	}
	// 3. Just NTHASH (NT is index 0) - Less common, but possible
	if len(parts) == 1 && len(parts[0]) == 32 && isHexString(parts[0]) {
		return parts[0] // Return NT hash
	}

	// Log warning? logging.Warnf("Could not extract NT hash from NTLM input format: %s", rawHash)
	// Fallback: return the original hash if format is unexpected or doesn't contain a clear NT hash.
	return rawHash
}

// processLM extracts just the LMHASH portion for Hashcat mode 3000 processing.
// It prioritizes formats like user:rid:LM:NT::: and then LM:NT, finally just LM.
func processLM(rawHash string) string {
	parts := strings.Split(rawHash, ":")

	// Check common LM dump formats:
	// 1. user:rid:LMHASH:NTHASH:... (LM is index 2)
	if len(parts) >= 4 && len(parts[2]) == 32 && isHexString(parts[2]) {
		return parts[2] // Return LM hash
	}
	// 2. LMHASH:NTHASH (LM is index 0) - Common Hashcat input
	if len(parts) >= 2 && len(parts[0]) == 32 && isHexString(parts[0]) {
		return parts[0] // Return LM hash
	}
	// 3. Just LMHASH (LM is index 0) - Less common, but possible
	if len(parts) == 1 && len(parts[0]) == 32 && isHexString(parts[0]) {
		return parts[0] // Return LM hash
	}

	// Log warning? logging.Warnf("Could not extract LM hash from LM input format: %s", rawHash)
	// Fallback: return the original hash if format is unexpected or doesn't contain a clear LM hash.
	return rawHash
}

// isHexString checks if a string consists only of hexadecimal characters.
func isHexString(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}
