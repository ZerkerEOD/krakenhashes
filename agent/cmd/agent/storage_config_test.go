package main

import "testing"

// TestNormalizeStorageConfig locks the agent-side seed-only clamp: the tier is
// trimmed+lowercased, the mount path is kept only for network_direct, and an
// invalid combination (unknown tier, or network_direct with no mount) falls
// back to full_cache so a misconfigured agent still starts.
func TestNormalizeStorageConfig(t *testing.T) {
	cases := []struct {
		name      string
		tier      string
		mount     string
		wantTier  string
		wantMount string
	}{
		{"empty defaults to full_cache", "", "", "full_cache", ""},
		{"full_cache explicit", "full_cache", "", "full_cache", ""},
		{"full_cache drops mount path", "full_cache", "/mnt/x", "full_cache", ""},
		{"on_demand", "on_demand", "", "on_demand", ""},
		{"on_demand drops mount path", "on_demand", "/mnt/x", "on_demand", ""},
		{"network_direct with mount", "network_direct", "/mnt/kh-agent-share", "network_direct", "/mnt/kh-agent-share"},
		{"network_direct without mount clamps", "network_direct", "", "full_cache", ""},
		{"network_direct whitespace mount clamps", "network_direct", "   ", "full_cache", ""},
		{"tier is case-insensitive", "NETWORK_DIRECT", "/mnt/x", "network_direct", "/mnt/x"},
		{"tier and mount are trimmed", "  network_direct  ", "  /mnt/x  ", "network_direct", "/mnt/x"},
		{"unknown tier clamps to full_cache", "weird", "/mnt/x", "full_cache", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotTier, gotMount := normalizeStorageConfig(c.tier, c.mount)
			if gotTier != c.wantTier || gotMount != c.wantMount {
				t.Fatalf("normalizeStorageConfig(%q, %q) = (%q, %q); want (%q, %q)",
					c.tier, c.mount, gotTier, gotMount, c.wantTier, c.wantMount)
			}
		})
	}
}
