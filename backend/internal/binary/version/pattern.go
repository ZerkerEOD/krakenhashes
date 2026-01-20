// Package version provides version pattern matching for binary version management.
// It supports patterns like "default", "7.x", "7.1.x", "7.1.2", "7.1.2-NTLMv3"
package version

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// PatternType represents the type of version pattern
type PatternType int

const (
	// PatternTypeDefault matches any version
	PatternTypeDefault PatternType = iota
	// PatternTypeWildcardMajor matches any version with the same major (e.g., "7.x")
	PatternTypeWildcardMajor
	// PatternTypeWildcardMinor matches any version with same major.minor (e.g., "7.1.x")
	PatternTypeWildcardMinor
	// PatternTypeExact matches exact version, optionally with any suffix (e.g., "7.1.2")
	PatternTypeExact
	// PatternTypeExactWithSuffix matches exact version including suffix (e.g., "7.1.2-NTLMv3")
	PatternTypeExactWithSuffix
)

// String returns a human-readable representation of the pattern type
func (pt PatternType) String() string {
	switch pt {
	case PatternTypeDefault:
		return "default"
	case PatternTypeWildcardMajor:
		return "wildcard-major"
	case PatternTypeWildcardMinor:
		return "wildcard-minor"
	case PatternTypeExact:
		return "exact"
	case PatternTypeExactWithSuffix:
		return "exact-with-suffix"
	default:
		return "unknown"
	}
}

// Pattern represents a parsed version pattern
type Pattern struct {
	// Raw is the original pattern string
	Raw string
	// Type is the pattern type
	Type PatternType
	// Major version component (nil for default)
	Major *int
	// Minor version component (nil for default or major wildcard)
	Minor *int
	// Patch version component (nil for default or wildcards)
	Patch *int
	// Suffix is the version suffix (e.g., "NTLMv3")
	Suffix string
}

// Regular expressions for pattern parsing
var (
	// Matches "7.x" or "7.X"
	wildcardMajorRegex = regexp.MustCompile(`^(\d+)\.x$`)
	// Matches "7.1.x" or "7.1.X"
	wildcardMinorRegex = regexp.MustCompile(`^(\d+)\.(\d+)\.x$`)
	// Matches "7.1.2" (exact version without suffix)
	exactVersionRegex = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)
	// Matches "7.1.2-suffix" (exact version with suffix)
	exactWithSuffixRegex = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)-(.+)$`)
)

// Parse parses a version pattern string into a Pattern struct.
// Supported patterns:
//   - "default" - matches any version
//   - "7.x" - matches any version with major 7
//   - "7.1.x" - matches any version with major 7, minor 1
//   - "7.1.2" - matches exact version 7.1.2 with any suffix
//   - "7.1.2-NTLMv3" - matches exact version 7.1.2-NTLMv3
func Parse(pattern string) (*Pattern, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, fmt.Errorf("empty pattern")
	}

	// Normalize case for 'x' wildcard
	normalizedPattern := strings.ToLower(pattern)

	// Check for default pattern
	if normalizedPattern == "default" {
		return &Pattern{
			Raw:  pattern,
			Type: PatternTypeDefault,
		}, nil
	}

	// Try wildcard major pattern (e.g., "7.x")
	if matches := wildcardMajorRegex.FindStringSubmatch(normalizedPattern); matches != nil {
		major, _ := strconv.Atoi(matches[1])
		return &Pattern{
			Raw:   pattern,
			Type:  PatternTypeWildcardMajor,
			Major: &major,
		}, nil
	}

	// Try wildcard minor pattern (e.g., "7.1.x")
	if matches := wildcardMinorRegex.FindStringSubmatch(normalizedPattern); matches != nil {
		major, _ := strconv.Atoi(matches[1])
		minor, _ := strconv.Atoi(matches[2])
		return &Pattern{
			Raw:   pattern,
			Type:  PatternTypeWildcardMinor,
			Major: &major,
			Minor: &minor,
		}, nil
	}

	// Try exact version with suffix (e.g., "7.1.2-NTLMv3")
	if matches := exactWithSuffixRegex.FindStringSubmatch(pattern); matches != nil {
		major, _ := strconv.Atoi(matches[1])
		minor, _ := strconv.Atoi(matches[2])
		patch, _ := strconv.Atoi(matches[3])
		return &Pattern{
			Raw:    pattern,
			Type:   PatternTypeExactWithSuffix,
			Major:  &major,
			Minor:  &minor,
			Patch:  &patch,
			Suffix: matches[4],
		}, nil
	}

	// Try exact version without suffix (e.g., "7.1.2")
	if matches := exactVersionRegex.FindStringSubmatch(pattern); matches != nil {
		major, _ := strconv.Atoi(matches[1])
		minor, _ := strconv.Atoi(matches[2])
		patch, _ := strconv.Atoi(matches[3])
		return &Pattern{
			Raw:   pattern,
			Type:  PatternTypeExact,
			Major: &major,
			Minor: &minor,
			Patch: &patch,
		}, nil
	}

	return nil, fmt.Errorf("invalid version pattern: %q", pattern)
}

// MustParse is like Parse but panics on error.
// Useful for tests and known-good patterns.
func MustParse(pattern string) *Pattern {
	p, err := Parse(pattern)
	if err != nil {
		panic(err)
	}
	return p
}

// String returns the original pattern string
func (p *Pattern) String() string {
	return p.Raw
}

// IsDefault returns true if this is the default pattern
func (p *Pattern) IsDefault() bool {
	return p.Type == PatternTypeDefault
}

// IsWildcard returns true if this pattern contains a wildcard
func (p *Pattern) IsWildcard() bool {
	return p.Type == PatternTypeWildcardMajor || p.Type == PatternTypeWildcardMinor
}

// IsExact returns true if this pattern matches a specific version
func (p *Pattern) IsExact() bool {
	return p.Type == PatternTypeExact || p.Type == PatternTypeExactWithSuffix
}

// Version represents a parsed binary version string (not a pattern)
type Version struct {
	Raw    string
	Major  int
	Minor  int
	Patch  int
	Suffix string
}

// ParseVersion parses a version string (not a pattern).
// Version strings are always in the form "major.minor.patch" or "major.minor.patch-suffix"
func ParseVersion(version string) (*Version, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil, fmt.Errorf("empty version string")
	}

	// Try exact version with suffix (e.g., "7.1.2-NTLMv3")
	if matches := exactWithSuffixRegex.FindStringSubmatch(version); matches != nil {
		major, _ := strconv.Atoi(matches[1])
		minor, _ := strconv.Atoi(matches[2])
		patch, _ := strconv.Atoi(matches[3])
		return &Version{
			Raw:    version,
			Major:  major,
			Minor:  minor,
			Patch:  patch,
			Suffix: matches[4],
		}, nil
	}

	// Try exact version without suffix (e.g., "7.1.2")
	if matches := exactVersionRegex.FindStringSubmatch(version); matches != nil {
		major, _ := strconv.Atoi(matches[1])
		minor, _ := strconv.Atoi(matches[2])
		patch, _ := strconv.Atoi(matches[3])
		return &Version{
			Raw:   version,
			Major: major,
			Minor: minor,
			Patch: patch,
		}, nil
	}

	return nil, fmt.Errorf("invalid version string: %q", version)
}

// MustParseVersion is like ParseVersion but panics on error.
func MustParseVersion(version string) *Version {
	v, err := ParseVersion(version)
	if err != nil {
		panic(err)
	}
	return v
}

// String returns the original version string
func (v *Version) String() string {
	return v.Raw
}

// Compare compares two versions. Returns:
// -1 if v < other
// 0 if v == other
// 1 if v > other
// Suffix is compared alphabetically if versions are otherwise equal
func (v *Version) Compare(other *Version) int {
	if v.Major != other.Major {
		if v.Major < other.Major {
			return -1
		}
		return 1
	}
	if v.Minor != other.Minor {
		if v.Minor < other.Minor {
			return -1
		}
		return 1
	}
	if v.Patch != other.Patch {
		if v.Patch < other.Patch {
			return -1
		}
		return 1
	}
	// Versions are equal, compare suffix alphabetically
	if v.Suffix < other.Suffix {
		return -1
	}
	if v.Suffix > other.Suffix {
		return 1
	}
	return 0
}
