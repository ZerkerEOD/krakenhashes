package version

import (
	"context"
	"fmt"
	"sort"
)

// BinaryInfo represents the minimal binary information needed for resolution
type BinaryInfo struct {
	ID        int64
	Version   string
	IsDefault bool
	IsActive  bool
}

// BinaryStore defines the interface for accessing binary version data
type BinaryStore interface {
	// ListActive returns all active binary versions
	ListActive(ctx context.Context) ([]BinaryInfo, error)
	// GetDefault returns the default binary version, or nil if none set
	GetDefault(ctx context.Context) (*BinaryInfo, error)
}

// Resolver resolves version patterns to specific binary IDs
type Resolver struct {
	store BinaryStore
}

// NewResolver creates a new version resolver
func NewResolver(store BinaryStore) *Resolver {
	return &Resolver{store: store}
}

// ResolveForTask resolves the best binary ID for a task given agent and job patterns.
//
// Algorithm:
// 1. Get all candidates (binaries matching job requirement)
//   - If job is "default", candidates = ALL active binaries
//   - Otherwise, candidates = binaries matching job pattern
//
// 2. Filter by agent compatibility
//   - Remove binaries that don't match agent pattern
//
// 3. Select best
//   - If server default is in candidates, return it
//   - Otherwise, return highest version
//
// Returns error if no compatible binary found.
func (r *Resolver) ResolveForTask(ctx context.Context, agentPattern, jobPattern *Pattern) (int64, error) {
	// Get all active binaries
	binaries, err := r.store.ListActive(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to list active binaries: %w", err)
	}

	if len(binaries) == 0 {
		return 0, fmt.Errorf("no active binaries available")
	}

	// Step 1: Get candidates matching job requirement
	var candidates []BinaryInfo
	for _, b := range binaries {
		if jobPattern.IsDefault() {
			// Job accepts any binary
			candidates = append(candidates, b)
		} else if Matches(jobPattern, b.Version) {
			candidates = append(candidates, b)
		}
	}

	if len(candidates) == 0 {
		return 0, fmt.Errorf("no binary matches job requirement %q", jobPattern.Raw)
	}

	// Step 2: Filter by agent compatibility
	var compatible []BinaryInfo
	for _, b := range candidates {
		if agentPattern.IsDefault() {
			// Agent accepts any binary
			compatible = append(compatible, b)
		} else if Matches(agentPattern, b.Version) {
			compatible = append(compatible, b)
		}
	}

	if len(compatible) == 0 {
		return 0, fmt.Errorf("no binary compatible with both agent %q and job %q", agentPattern.Raw, jobPattern.Raw)
	}

	// Step 3: Select best
	// Prefer server default if it's in the compatible list
	for _, b := range compatible {
		if b.IsDefault {
			return b.ID, nil
		}
	}

	// Otherwise, return highest version
	best := selectHighestVersion(compatible)
	return best.ID, nil
}

// ResolveForTaskStr is a convenience wrapper that parses patterns first.
func (r *Resolver) ResolveForTaskStr(ctx context.Context, agentPattern, jobPattern string) (int64, error) {
	agent, err := Parse(agentPattern)
	if err != nil {
		return 0, fmt.Errorf("invalid agent pattern: %w", err)
	}

	job, err := Parse(jobPattern)
	if err != nil {
		return 0, fmt.Errorf("invalid job pattern: %w", err)
	}

	return r.ResolveForTask(ctx, agent, job)
}

// GetMatchingBinaries returns all binaries that match the given pattern.
func (r *Resolver) GetMatchingBinaries(ctx context.Context, pattern *Pattern) ([]BinaryInfo, error) {
	binaries, err := r.store.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list active binaries: %w", err)
	}

	if pattern.IsDefault() {
		return binaries, nil
	}

	var matching []BinaryInfo
	for _, b := range binaries {
		if Matches(pattern, b.Version) {
			matching = append(matching, b)
		}
	}

	return matching, nil
}

// selectHighestVersion returns the binary with the highest semantic version
func selectHighestVersion(binaries []BinaryInfo) BinaryInfo {
	if len(binaries) == 0 {
		return BinaryInfo{}
	}

	if len(binaries) == 1 {
		return binaries[0]
	}

	// Sort by version descending
	sorted := make([]BinaryInfo, len(binaries))
	copy(sorted, binaries)

	sort.Slice(sorted, func(i, j int) bool {
		vi, err := ParseVersion(sorted[i].Version)
		if err != nil {
			return false
		}
		vj, err := ParseVersion(sorted[j].Version)
		if err != nil {
			return true
		}
		return vi.Compare(vj) > 0 // Descending order
	})

	return sorted[0]
}

// PatternInfo represents a pattern option for the frontend dropdown
type PatternInfo struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

// VersionInfo represents a specific version for the frontend dropdown
type VersionInfo struct {
	ID    int64  `json:"id"`
	Value string `json:"value"`
	Label string `json:"label"`
}

// PatternsResponse is the response for the patterns endpoint
type PatternsResponse struct {
	Patterns []PatternInfo `json:"patterns"`
	Versions []VersionInfo `json:"versions"`
}

// GenerateAvailablePatterns generates pattern options from available binaries.
// Returns patterns in order: default, major wildcards (desc), minor wildcards (desc)
// And specific versions sorted by version descending.
func GenerateAvailablePatterns(binaries []BinaryInfo) *PatternsResponse {
	if len(binaries) == 0 {
		return &PatternsResponse{
			Patterns: []PatternInfo{{Value: "default", Label: "Default (any version)", Count: 0}},
			Versions: []VersionInfo{},
		}
	}

	// Count binaries by major and major.minor
	majorCounts := make(map[int]int)
	minorCounts := make(map[string]int)

	var versions []VersionInfo

	for _, b := range binaries {
		ver, err := ParseVersion(b.Version)
		if err != nil {
			continue
		}

		majorCounts[ver.Major]++
		minorKey := fmt.Sprintf("%d.%d", ver.Major, ver.Minor)
		minorCounts[minorKey]++

		versions = append(versions, VersionInfo{
			ID:    b.ID,
			Value: b.Version,
			Label: b.Version,
		})
	}

	// Build patterns list
	var patterns []PatternInfo

	// Always add default first
	patterns = append(patterns, PatternInfo{
		Value: "default",
		Label: "Default (any version)",
		Count: len(binaries),
	})

	// Add major patterns (sorted descending)
	var majors []int
	for major := range majorCounts {
		majors = append(majors, major)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(majors)))

	for _, major := range majors {
		count := majorCounts[major]
		patterns = append(patterns, PatternInfo{
			Value: fmt.Sprintf("%d.x", major),
			Label: fmt.Sprintf("All %d.x", major),
			Count: count,
		})
	}

	// Add minor patterns (sorted by major desc, then minor desc)
	type minorKey struct {
		major int
		minor int
		key   string
	}
	var minors []minorKey
	for key, count := range minorCounts {
		var major, minor int
		fmt.Sscanf(key, "%d.%d", &major, &minor)
		// Only add minor pattern if there's more than one binary in that minor version
		// or if the count differs from major count (meaning there are multiple minors)
		if count > 1 || minorCounts[key] < majorCounts[major] {
			minors = append(minors, minorKey{major, minor, key})
		}
	}
	sort.Slice(minors, func(i, j int) bool {
		if minors[i].major != minors[j].major {
			return minors[i].major > minors[j].major
		}
		return minors[i].minor > minors[j].minor
	})

	for _, mk := range minors {
		count := minorCounts[mk.key]
		patterns = append(patterns, PatternInfo{
			Value: fmt.Sprintf("%d.%d.x", mk.major, mk.minor),
			Label: fmt.Sprintf("All %d.%d.x", mk.major, mk.minor),
			Count: count,
		})
	}

	// Sort versions by version descending
	sort.Slice(versions, func(i, j int) bool {
		vi, err := ParseVersion(versions[i].Value)
		if err != nil {
			return false
		}
		vj, err := ParseVersion(versions[j].Value)
		if err != nil {
			return true
		}
		return vi.Compare(vj) > 0
	})

	return &PatternsResponse{
		Patterns: patterns,
		Versions: versions,
	}
}

// CountCompatibleAgents counts how many agents can run a job with the given binary pattern.
func CountCompatibleAgents(agentPatterns []string, jobPattern *Pattern) int {
	count := 0
	for _, agentPatternStr := range agentPatterns {
		agentPattern, err := Parse(agentPatternStr)
		if err != nil {
			continue
		}
		if IsCompatible(agentPattern, jobPattern) {
			count++
		}
	}
	return count
}
