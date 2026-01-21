package version

import (
	"context"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		wantType    PatternType
		wantMajor   *int
		wantMinor   *int
		wantPatch   *int
		wantSuffix  string
		wantErr     bool
	}{
		{
			name:     "default pattern",
			pattern:  "default",
			wantType: PatternTypeDefault,
		},
		{
			name:     "default pattern uppercase",
			pattern:  "DEFAULT",
			wantType: PatternTypeDefault,
		},
		{
			name:      "major wildcard",
			pattern:   "7.x",
			wantType:  PatternTypeWildcardMajor,
			wantMajor: intPtr(7),
		},
		{
			name:      "major wildcard uppercase X",
			pattern:   "7.X",
			wantType:  PatternTypeWildcardMajor,
			wantMajor: intPtr(7),
		},
		{
			name:      "minor wildcard",
			pattern:   "7.1.x",
			wantType:  PatternTypeWildcardMinor,
			wantMajor: intPtr(7),
			wantMinor: intPtr(1),
		},
		{
			name:      "exact version",
			pattern:   "7.1.2",
			wantType:  PatternTypeExact,
			wantMajor: intPtr(7),
			wantMinor: intPtr(1),
			wantPatch: intPtr(2),
		},
		{
			name:       "exact version with suffix",
			pattern:    "7.1.2-NTLMv3",
			wantType:   PatternTypeExactWithSuffix,
			wantMajor:  intPtr(7),
			wantMinor:  intPtr(1),
			wantPatch:  intPtr(2),
			wantSuffix: "NTLMv3",
		},
		{
			name:       "exact version with complex suffix",
			pattern:    "7.1.2-custom-build-123",
			wantType:   PatternTypeExactWithSuffix,
			wantMajor:  intPtr(7),
			wantMinor:  intPtr(1),
			wantPatch:  intPtr(2),
			wantSuffix: "custom-build-123",
		},
		{
			name:       "exact version with + suffix (hashcat beta format)",
			pattern:    "7.1.2+338.7",
			wantType:   PatternTypeExactWithSuffix,
			wantMajor:  intPtr(7),
			wantMinor:  intPtr(1),
			wantPatch:  intPtr(2),
			wantSuffix: "338.7",
		},
		{
			name:       "exact version with + suffix and additional suffix",
			pattern:    "7.1.2+338.7-ARM",
			wantType:   PatternTypeExactWithSuffix,
			wantMajor:  intPtr(7),
			wantMinor:  intPtr(1),
			wantPatch:  intPtr(2),
			wantSuffix: "338.7-ARM",
		},
		{
			name:    "empty pattern",
			pattern: "",
			wantErr: true,
		},
		{
			name:    "invalid pattern",
			pattern: "foo",
			wantErr: true,
		},
		{
			name:    "invalid version format",
			pattern: "7.1",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if got.Type != tt.wantType {
				t.Errorf("Parse() type = %v, want %v", got.Type, tt.wantType)
			}

			if !intPtrEqual(got.Major, tt.wantMajor) {
				t.Errorf("Parse() major = %v, want %v", got.Major, tt.wantMajor)
			}

			if !intPtrEqual(got.Minor, tt.wantMinor) {
				t.Errorf("Parse() minor = %v, want %v", got.Minor, tt.wantMinor)
			}

			if !intPtrEqual(got.Patch, tt.wantPatch) {
				t.Errorf("Parse() patch = %v, want %v", got.Patch, tt.wantPatch)
			}

			if got.Suffix != tt.wantSuffix {
				t.Errorf("Parse() suffix = %v, want %v", got.Suffix, tt.wantSuffix)
			}
		})
	}
}

func TestMatches(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		version string
		want    bool
	}{
		// Default pattern matches everything
		{"default matches any", "default", "7.1.2", true},
		{"default matches with suffix", "default", "7.1.2-NTLMv3", true},

		// Major wildcard
		{"7.x matches 7.0.0", "7.x", "7.0.0", true},
		{"7.x matches 7.1.2", "7.x", "7.1.2", true},
		{"7.x matches 7.2.0-custom", "7.x", "7.2.0-custom", true},
		{"7.x does not match 6.1.2", "7.x", "6.1.2", false},
		{"7.x does not match 8.0.0", "7.x", "8.0.0", false},

		// Minor wildcard
		{"7.1.x matches 7.1.0", "7.1.x", "7.1.0", true},
		{"7.1.x matches 7.1.2", "7.1.x", "7.1.2", true},
		{"7.1.x matches 7.1.2-NTLMv3", "7.1.x", "7.1.2-NTLMv3", true},
		{"7.1.x does not match 7.2.0", "7.1.x", "7.2.0", false},
		{"7.1.x does not match 6.1.0", "7.1.x", "6.1.0", false},

		// Exact version (matches any suffix)
		{"7.1.2 matches 7.1.2", "7.1.2", "7.1.2", true},
		{"7.1.2 matches 7.1.2-NTLMv3", "7.1.2", "7.1.2-NTLMv3", true},
		{"7.1.2 matches 7.1.2-custom", "7.1.2", "7.1.2-custom", true},
		{"7.1.2 does not match 7.1.3", "7.1.2", "7.1.3", false},
		{"7.1.2 does not match 7.2.2", "7.1.2", "7.2.2", false},

		// Exact version with suffix (exact match only)
		{"7.1.2-NTLMv3 matches 7.1.2-NTLMv3", "7.1.2-NTLMv3", "7.1.2-NTLMv3", true},
		{"7.1.2-NTLMv3 does not match 7.1.2", "7.1.2-NTLMv3", "7.1.2", false},
		{"7.1.2-NTLMv3 does not match 7.1.2-other", "7.1.2-NTLMv3", "7.1.2-other", false},

		// + suffix (hashcat beta build format)
		{"default matches + suffix version", "default", "7.1.2+338.7", true},
		{"7.x matches + suffix version", "7.x", "7.1.2+338.7", true},
		{"7.1.x matches + suffix version", "7.1.x", "7.1.2+338.7", true},
		{"7.1.2 matches + suffix version", "7.1.2", "7.1.2+338.7", true},
		{"7.1.2+338.7 matches exactly", "7.1.2+338.7", "7.1.2+338.7", true},
		{"7.1.2+338.7 does not match 7.1.2", "7.1.2+338.7", "7.1.2", false},
		{"7.1.2+338.7 does not match 7.1.2-NTLMv3", "7.1.2+338.7", "7.1.2-NTLMv3", false},
		{"7.1.2+338.7-ARM matches exactly", "7.1.2+338.7-ARM", "7.1.2+338.7-ARM", true},
		{"7.1.2 matches 7.1.2+338.7-ARM", "7.1.2", "7.1.2+338.7-ARM", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := MustParse(tt.pattern)
			got := Matches(pattern, tt.version)
			if got != tt.want {
				t.Errorf("Matches(%q, %q) = %v, want %v", tt.pattern, tt.version, got, tt.want)
			}
		})
	}
}

func TestIsCompatible(t *testing.T) {
	tests := []struct {
		name         string
		agentPattern string
		jobPattern   string
		want         bool
	}{
		// Default patterns
		{"default agent, default job", "default", "default", true},
		{"default agent, specific job", "default", "7.1.2", true},
		{"specific agent, default job", "7.x", "default", true},

		// Agent 7.x (can run any v7 binary)
		{"agent 7.x, job 7.x", "7.x", "7.x", true},
		{"agent 7.x, job 7.1.x", "7.x", "7.1.x", true},
		{"agent 7.x, job 7.1.2", "7.x", "7.1.2", true},
		{"agent 7.x, job 7.1.2-NTLMv3", "7.x", "7.1.2-NTLMv3", true},
		{"agent 7.x, job 6.x (incompatible)", "7.x", "6.x", false},
		{"agent 7.x, job 6.1.2 (incompatible)", "7.x", "6.1.2", false},

		// Agent 7.1.x (can run any v7.1 binary)
		{"agent 7.1.x, job 7.x", "7.1.x", "7.x", true},
		{"agent 7.1.x, job 7.1.x", "7.1.x", "7.1.x", true},
		{"agent 7.1.x, job 7.1.2", "7.1.x", "7.1.2", true},
		{"agent 7.1.x, job 7.2.x (incompatible)", "7.1.x", "7.2.x", false},
		{"agent 7.1.x, job 7.2.0 (incompatible)", "7.1.x", "7.2.0", false},

		// Agent 7.1.2 (can run 7.1.2 with any suffix)
		{"agent 7.1.2, job 7.x", "7.1.2", "7.x", true},
		{"agent 7.1.2, job 7.1.x", "7.1.2", "7.1.x", true},
		{"agent 7.1.2, job 7.1.2", "7.1.2", "7.1.2", true},
		{"agent 7.1.2, job 7.1.2-NTLMv3", "7.1.2", "7.1.2-NTLMv3", true},
		{"agent 7.1.2, job 7.1.3 (incompatible)", "7.1.2", "7.1.3", false},

		// Agent 7.1.2-NTLMv3 (most restrictive)
		{"agent 7.1.2-NTLMv3, job 7.x", "7.1.2-NTLMv3", "7.x", true},
		{"agent 7.1.2-NTLMv3, job 7.1.x", "7.1.2-NTLMv3", "7.1.x", true},
		{"agent 7.1.2-NTLMv3, job 7.1.2", "7.1.2-NTLMv3", "7.1.2", true},
		{"agent 7.1.2-NTLMv3, job 7.1.2-NTLMv3", "7.1.2-NTLMv3", "7.1.2-NTLMv3", true},
		{"agent 7.1.2-NTLMv3, job 7.1.2-other (incompatible)", "7.1.2-NTLMv3", "7.1.2-other", false},

		// Cross-major incompatibility
		{"agent 6.x, job 7.x (incompatible)", "6.x", "7.x", false},
		{"agent 6.2.6, job 7.1.2 (incompatible)", "6.2.6", "7.1.2", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentPattern := MustParse(tt.agentPattern)
			jobPattern := MustParse(tt.jobPattern)
			got := IsCompatible(agentPattern, jobPattern)
			if got != tt.want {
				t.Errorf("IsCompatible(agent=%q, job=%q) = %v, want %v", tt.agentPattern, tt.jobPattern, got, tt.want)
			}
		})
	}
}

func TestVersionCompare(t *testing.T) {
	tests := []struct {
		v1   string
		v2   string
		want int
	}{
		{"7.0.0", "7.0.0", 0},
		{"7.1.0", "7.0.0", 1},
		{"7.0.0", "7.1.0", -1},
		{"7.1.2", "7.1.1", 1},
		{"8.0.0", "7.9.9", 1},
		{"7.1.2", "7.1.2-NTLMv3", -1}, // No suffix < with suffix alphabetically
		{"7.1.2-NTLMv3", "7.1.2-aaa", -1}, // NTLMv3 < aaa (uppercase < lowercase in ASCII)
	}

	for _, tt := range tests {
		t.Run(tt.v1+" vs "+tt.v2, func(t *testing.T) {
			v1 := MustParseVersion(tt.v1)
			v2 := MustParseVersion(tt.v2)
			got := v1.Compare(v2)
			if got != tt.want {
				t.Errorf("Compare(%q, %q) = %v, want %v", tt.v1, tt.v2, got, tt.want)
			}
		})
	}
}

func TestGenerateAvailablePatterns(t *testing.T) {
	binaries := []BinaryInfo{
		{ID: 1, Version: "6.2.6", IsDefault: false, IsActive: true},
		{ID: 2, Version: "7.1.1", IsDefault: false, IsActive: true},
		{ID: 3, Version: "7.1.2", IsDefault: false, IsActive: true},
		{ID: 4, Version: "7.1.2-NTLMv3", IsDefault: true, IsActive: true},
		{ID: 5, Version: "7.2.1", IsDefault: false, IsActive: true},
	}

	resp := GenerateAvailablePatterns(binaries)

	// Check patterns exist
	if len(resp.Patterns) < 3 {
		t.Errorf("Expected at least 3 patterns, got %d", len(resp.Patterns))
	}

	// First pattern should be default with correct fields
	if resp.Patterns[0].Pattern != "default" {
		t.Errorf("First pattern should be 'default', got %q", resp.Patterns[0].Pattern)
	}
	if resp.Patterns[0].Type != "default" {
		t.Errorf("First pattern type should be 'default', got %q", resp.Patterns[0].Type)
	}
	if resp.Patterns[0].Display != "System Default" {
		t.Errorf("First pattern display should be 'System Default', got %q", resp.Patterns[0].Display)
	}

	// Check that 7.x pattern exists with correct type
	found7x := false
	for _, p := range resp.Patterns {
		if p.Pattern == "7.x" {
			found7x = true
			if p.Type != "major_wildcard" {
				t.Errorf("Pattern 7.x should have type 'major_wildcard', got %q", p.Type)
			}
			if p.Display != "Hashcat 7.x (latest)" {
				t.Errorf("Pattern 7.x should have display 'Hashcat 7.x (latest)', got %q", p.Display)
			}
		}
	}
	if !found7x {
		t.Error("Pattern 7.x not found")
	}

	// Check ActiveBinaryIds contains all 5 binaries
	if len(resp.ActiveBinaryIds) != 5 {
		t.Errorf("Expected 5 active binary IDs, got %d", len(resp.ActiveBinaryIds))
	}

	// Check DefaultBinaryId points to binary 4 (the default one)
	if resp.DefaultBinaryId == nil {
		t.Error("DefaultBinaryId should not be nil")
	} else if *resp.DefaultBinaryId != 4 {
		t.Errorf("DefaultBinaryId should be 4, got %d", *resp.DefaultBinaryId)
	}

	// Check exact version patterns exist and have correct IsDefault
	foundDefaultExact := false
	for _, p := range resp.Patterns {
		if p.Pattern == "7.1.2-NTLMv3" {
			if !p.IsDefault {
				t.Error("Pattern 7.1.2-NTLMv3 should have IsDefault=true")
			}
			if p.Type != "exact" {
				t.Errorf("Pattern 7.1.2-NTLMv3 should have type 'exact', got %q", p.Type)
			}
			foundDefaultExact = true
		}
	}
	if !foundDefaultExact {
		t.Error("Exact pattern 7.1.2-NTLMv3 not found")
	}
}

// Mock store for testing resolver
type mockBinaryStore struct {
	binaries []BinaryInfo
}

func (m *mockBinaryStore) ListActive(ctx context.Context) ([]BinaryInfo, error) {
	return m.binaries, nil
}

func (m *mockBinaryStore) GetDefault(ctx context.Context) (*BinaryInfo, error) {
	for _, b := range m.binaries {
		if b.IsDefault {
			return &b, nil
		}
	}
	return nil, nil
}

func TestResolver_ResolveForTask(t *testing.T) {
	store := &mockBinaryStore{
		binaries: []BinaryInfo{
			{ID: 1, Version: "6.2.6", IsDefault: false, IsActive: true},
			{ID: 2, Version: "7.1.1", IsDefault: false, IsActive: true},
			{ID: 3, Version: "7.1.2", IsDefault: true, IsActive: true},
			{ID: 4, Version: "7.1.2-NTLMv3", IsDefault: false, IsActive: true},
			{ID: 5, Version: "7.2.1", IsDefault: false, IsActive: true},
		},
	}
	resolver := NewResolver(store)

	tests := []struct {
		name         string
		agentPattern string
		jobPattern   string
		wantID       int64
		wantErr      bool
	}{
		// Both default - return server default
		{"both default", "default", "default", 3, false},

		// Job default, agent restricted - return highest for agent
		{"agent 6.x, job default", "6.x", "default", 1, false}, // Only 6.2.6 matches
		{"agent 7.x, job default", "7.x", "default", 3, false}, // Default 7.1.2 matches

		// Job specific, agent default
		{"agent default, job 7.1.2", "default", "7.1.2", 3, false}, // Default 7.1.2 matches
		{"agent default, job 7.1.2-NTLMv3", "default", "7.1.2-NTLMv3", 4, false},

		// Both specific
		{"agent 7.x, job 7.1.2", "7.x", "7.1.2", 3, false}, // Default matches
		{"agent 7.1.x, job 7.1.2", "7.1.x", "7.1.2", 3, false}, // Default matches
		{"agent 7.1.2, job 7.1.2-NTLMv3", "7.1.2", "7.1.2-NTLMv3", 4, false},

		// Incompatible
		{"agent 6.x, job 7.1.2", "6.x", "7.1.2", 0, true},
		{"agent 7.1.x, job 7.2.1", "7.1.x", "7.2.1", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := resolver.ResolveForTaskStr(context.Background(), tt.agentPattern, tt.jobPattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveForTask() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && id != tt.wantID {
				t.Errorf("ResolveForTask() = %v, want %v", id, tt.wantID)
			}
		})
	}
}

// Helper functions
func intPtr(i int) *int {
	return &i
}

func intPtrEqual(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
