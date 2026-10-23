package services

import "testing"

func TestSupportsAutoUpdate(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"2.1.0", false},  // last pre-auto-update release binary
		{"v2.1.2", false}, // "v" prefix tolerated
		{"2.9.9", false},
		{"3.0.0", true},
		{"3.0.1", true},
		{"3.1.0", true},
		{"10.0.0", true},
		{"2.1.0-9df8480-dev", true}, // dev build: built from current source
		{"3.0.0-abc-dev", true},
		{"", true},    // unknown never passes IsStale anyway
		{"dev", true}, // non-semver
	}
	for _, tc := range tests {
		if got := SupportsAutoUpdate(tc.version); got != tc.want {
			t.Errorf("SupportsAutoUpdate(%q) = %v, want %v", tc.version, got, tc.want)
		}
	}
}

func TestNoteUnsupportedLogsOncePerVersion(t *testing.T) {
	s := &AgentUpdateService{}
	s.noteUnsupported(7, "2.1.0")
	s.noteUnsupported(7, "2.1.0")
	if got := s.unsupportedLogged[7]; got != "2.1.0" {
		t.Fatalf("unsupportedLogged[7] = %q, want 2.1.0", got)
	}
	s.noteUnsupported(7, "2.1.2")
	if got := s.unsupportedLogged[7]; got != "2.1.2" {
		t.Fatalf("after version change, unsupportedLogged[7] = %q, want 2.1.2", got)
	}
}
