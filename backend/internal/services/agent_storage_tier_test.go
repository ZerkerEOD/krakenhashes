package services

import (
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
)

// TestNormalizeAgentStorageTier locks the backend-side clamp applied to a
// seed-only storage tier reported by an agent at registration. Unlike the
// agent-side helper it matches the tier exactly (no lowercasing): an unknown or
// wrong-case value, or network_direct with no mount path, clamps to full_cache
// so registration never fails over a misconfigured agent, and the mount path is
// kept only for network_direct.
func TestNormalizeAgentStorageTier(t *testing.T) {
	cases := []struct {
		name      string
		tier      string
		mount     string
		wantTier  string
		wantMount string
	}{
		{"on_demand", models.StorageTierOnDemand, "", models.StorageTierOnDemand, ""},
		{"on_demand drops mount path", models.StorageTierOnDemand, "/mnt/x", models.StorageTierOnDemand, ""},
		{"network_direct with mount", models.StorageTierNetworkDirect, "/mnt/kh-agent-share", models.StorageTierNetworkDirect, "/mnt/kh-agent-share"},
		{"network_direct trims mount", models.StorageTierNetworkDirect, "  /mnt/x  ", models.StorageTierNetworkDirect, "/mnt/x"},
		{"network_direct without mount clamps", models.StorageTierNetworkDirect, "", models.StorageTierFullCache, ""},
		{"network_direct whitespace mount clamps", models.StorageTierNetworkDirect, "   ", models.StorageTierFullCache, ""},
		{"empty clamps to full_cache", "", "", models.StorageTierFullCache, ""},
		{"full_cache passes through default", models.StorageTierFullCache, "", models.StorageTierFullCache, ""},
		{"wrong case not matched (server is exact)", "Network_Direct", "/mnt/x", models.StorageTierFullCache, ""},
		{"unknown clamps to full_cache", "bogus", "/mnt/x", models.StorageTierFullCache, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotTier, gotMount := normalizeAgentStorageTier(c.tier, c.mount)
			if gotTier != c.wantTier || gotMount != c.wantMount {
				t.Fatalf("normalizeAgentStorageTier(%q, %q) = (%q, %q); want (%q, %q)",
					c.tier, c.mount, gotTier, gotMount, c.wantTier, c.wantMount)
			}
		})
	}
}
