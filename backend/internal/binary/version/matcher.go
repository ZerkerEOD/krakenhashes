package version

// Matches checks if a version string matches the given pattern.
// Examples:
//   - Pattern "default" matches any version
//   - Pattern "7.x" matches "7.0.0", "7.1.2", "7.2.0-custom"
//   - Pattern "7.1.x" matches "7.1.0", "7.1.2", "7.1.2-NTLMv3"
//   - Pattern "7.1.2" matches "7.1.2", "7.1.2-NTLMv3" (any suffix)
//   - Pattern "7.1.2-NTLMv3" matches only "7.1.2-NTLMv3" (exact)
func Matches(pattern *Pattern, versionStr string) bool {
	// Default pattern matches everything
	if pattern.IsDefault() {
		return true
	}

	// Parse the version string
	ver, err := ParseVersion(versionStr)
	if err != nil {
		// Invalid version string doesn't match any specific pattern
		return false
	}

	return MatchesVersion(pattern, ver)
}

// MatchesVersion checks if a Version matches the given pattern.
func MatchesVersion(pattern *Pattern, ver *Version) bool {
	// Default pattern matches everything
	if pattern.IsDefault() {
		return true
	}

	switch pattern.Type {
	case PatternTypeWildcardMajor:
		// "7.x" matches any version with major 7
		return ver.Major == *pattern.Major

	case PatternTypeWildcardMinor:
		// "7.1.x" matches any version with major 7 and minor 1
		return ver.Major == *pattern.Major && ver.Minor == *pattern.Minor

	case PatternTypeExact:
		// "7.1.2" matches version with same major.minor.patch, any suffix
		return ver.Major == *pattern.Major &&
			ver.Minor == *pattern.Minor &&
			ver.Patch == *pattern.Patch

	case PatternTypeExactWithSuffix:
		// "7.1.2-NTLMv3" matches only exact version including suffix
		return ver.Major == *pattern.Major &&
			ver.Minor == *pattern.Minor &&
			ver.Patch == *pattern.Patch &&
			ver.Suffix == pattern.Suffix
	}

	return false
}

// IsCompatible checks if an agent with agentPattern can run a job requiring jobPattern.
// The agent is compatible if it can run at least one binary that satisfies the job's requirement.
//
// Compatibility rules:
//   - Agent "default" is compatible with any job requirement (can run any binary)
//   - Job "default" is compatible with any agent (job accepts any binary)
//   - Agent "7.x" is compatible with job "7.1.2" (agent can run 7.1.2)
//   - Agent "6.x" is NOT compatible with job "7.1.2" (agent can't run any v7 binary)
//   - More permissive agent patterns are compatible with more specific job requirements
func IsCompatible(agentPattern, jobPattern *Pattern) bool {
	// If agent accepts any binary, it's always compatible
	if agentPattern.IsDefault() {
		return true
	}

	// If job accepts any binary, any agent is compatible
	if jobPattern.IsDefault() {
		return true
	}

	// Both patterns are specific - check if there's overlap
	// Agent can run job if agent's pattern is same or more permissive than job's pattern

	// If patterns are at the same specificity level, they must overlap
	// If agent pattern is more general (wildcard), it can run more specific job patterns

	switch agentPattern.Type {
	case PatternTypeWildcardMajor:
		// Agent "7.x" - check if job's pattern is within major 7
		switch jobPattern.Type {
		case PatternTypeWildcardMajor:
			// Both are major wildcards - must be same major
			return *agentPattern.Major == *jobPattern.Major

		case PatternTypeWildcardMinor:
			// Agent "7.x", Job "7.1.x" - compatible if same major
			return *agentPattern.Major == *jobPattern.Major

		case PatternTypeExact, PatternTypeExactWithSuffix:
			// Agent "7.x", Job "7.1.2" or "7.1.2-NTLMv3" - compatible if same major
			return *agentPattern.Major == *jobPattern.Major
		}

	case PatternTypeWildcardMinor:
		// Agent "7.1.x" - check if job's pattern is within 7.1.*
		switch jobPattern.Type {
		case PatternTypeWildcardMajor:
			// Agent "7.1.x", Job "7.x" - agent is more restrictive
			// Agent can only run 7.1.* binaries, but job accepts any 7.*
			// Compatible because agent can provide 7.1.* which satisfies 7.*
			return *agentPattern.Major == *jobPattern.Major

		case PatternTypeWildcardMinor:
			// Both are minor wildcards - must be same major.minor
			return *agentPattern.Major == *jobPattern.Major &&
				*agentPattern.Minor == *jobPattern.Minor

		case PatternTypeExact, PatternTypeExactWithSuffix:
			// Agent "7.1.x", Job "7.1.2" - compatible if same major.minor
			return *agentPattern.Major == *jobPattern.Major &&
				*agentPattern.Minor == *jobPattern.Minor
		}

	case PatternTypeExact:
		// Agent "7.1.2" (matches any suffix) - check if job's pattern overlaps
		switch jobPattern.Type {
		case PatternTypeWildcardMajor:
			// Agent "7.1.2", Job "7.x" - compatible if agent's major matches
			return *agentPattern.Major == *jobPattern.Major

		case PatternTypeWildcardMinor:
			// Agent "7.1.2", Job "7.1.x" - compatible if agent's major.minor matches
			return *agentPattern.Major == *jobPattern.Major &&
				*agentPattern.Minor == *jobPattern.Minor

		case PatternTypeExact:
			// Both are exact without suffix - must match major.minor.patch
			return *agentPattern.Major == *jobPattern.Major &&
				*agentPattern.Minor == *jobPattern.Minor &&
				*agentPattern.Patch == *jobPattern.Patch

		case PatternTypeExactWithSuffix:
			// Agent "7.1.2" (any suffix), Job "7.1.2-NTLMv3" - compatible
			return *agentPattern.Major == *jobPattern.Major &&
				*agentPattern.Minor == *jobPattern.Minor &&
				*agentPattern.Patch == *jobPattern.Patch
		}

	case PatternTypeExactWithSuffix:
		// Agent "7.1.2-NTLMv3" - most restrictive, only matches exact version
		switch jobPattern.Type {
		case PatternTypeWildcardMajor:
			// Agent "7.1.2-NTLMv3", Job "7.x" - compatible if same major
			return *agentPattern.Major == *jobPattern.Major

		case PatternTypeWildcardMinor:
			// Agent "7.1.2-NTLMv3", Job "7.1.x" - compatible if same major.minor
			return *agentPattern.Major == *jobPattern.Major &&
				*agentPattern.Minor == *jobPattern.Minor

		case PatternTypeExact:
			// Agent "7.1.2-NTLMv3", Job "7.1.2" (any suffix) - compatible
			return *agentPattern.Major == *jobPattern.Major &&
				*agentPattern.Minor == *jobPattern.Minor &&
				*agentPattern.Patch == *jobPattern.Patch

		case PatternTypeExactWithSuffix:
			// Both have exact suffix - must match exactly
			return *agentPattern.Major == *jobPattern.Major &&
				*agentPattern.Minor == *jobPattern.Minor &&
				*agentPattern.Patch == *jobPattern.Patch &&
				agentPattern.Suffix == jobPattern.Suffix
		}
	}

	return false
}

// IsCompatibleStr is a convenience wrapper that parses patterns first.
// Returns false if either pattern is invalid.
func IsCompatibleStr(agentPattern, jobPattern string) bool {
	agent, err := Parse(agentPattern)
	if err != nil {
		return false
	}

	job, err := Parse(jobPattern)
	if err != nil {
		return false
	}

	return IsCompatible(agent, job)
}
