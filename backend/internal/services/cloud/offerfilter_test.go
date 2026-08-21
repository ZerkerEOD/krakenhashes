package cloud

import (
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
)

/*
 * These tests are pure: no database, no network, no provider.
 *
 * What they defend is the direction the filter fails in. Every case below has a
 * plausible-looking implementation that passes the "obvious" half and gets the
 * other half backwards — rejecting unknown VRAM instead of admitting it, or
 * letting an allow list resurrect a denied card. Both of those cost money and
 * neither produces an error anywhere.
 */

// keptIDs is the only thing the assertions care about.
func keptIDs(offers []Offer) []string {
	ids := make([]string, 0, len(offers))
	for _, o := range offers {
		ids = append(ids, o.ID)
	}
	return ids
}

func hasID(offers []Offer, id string) bool {
	for _, o := range offers {
		if o.ID == id {
			return true
		}
	}
	return false
}

/*
 * TestApplyOfferConstraints_UnknownVRAMPasses is the central rule.
 *
 * AWS reports no VRAM at all, so VRAMGBPerGPU == 0 is the normal state for an
 * entire provider. If unknown were treated as "0GB, below any floor" then the
 * first operator to set MinVRAMGBPerGPU would delete every AWS offer, silently,
 * and the only symptom would be that AWS stopped being rented.
 */
func TestApplyOfferConstraints_UnknownVRAMPasses(t *testing.T) {
	offers := []Offer{
		{ID: "unknown-vram", GPUModel: "g5.xlarge", VRAMGBPerGPU: 0},
		{ID: "known-24gb", GPUModel: "RTX 4090", VRAMGBPerGPU: 24},
	}

	t.Run("passes a floor", func(t *testing.T) {
		got := applyOfferConstraints(offers, OfferQuery{MinVRAMGBPerGPU: 16})
		if !hasID(got, "unknown-vram") {
			t.Fatalf("offer with unknown VRAM was dropped by a 16GB floor; got %v. "+
				"Every AWS offer reports 0 here, so this deletes a whole provider with nothing in the logs", keptIDs(got))
		}
	})

	t.Run("passes a ceiling", func(t *testing.T) {
		// The ceiling is the case that is easy to get right by accident and
		// easy to get wrong on purpose: 0 <= any ceiling, so a naive
		// implementation passes it. It must pass for the UNKNOWN reason, which
		// is why it is asserted alongside the floor.
		got := applyOfferConstraints(offers, OfferQuery{MaxVRAMGBPerGPU: 48})
		if !hasID(got, "unknown-vram") {
			t.Fatalf("offer with unknown VRAM was dropped by a 48GB ceiling; got %v", keptIDs(got))
		}
	})

	t.Run("passes a floor and a ceiling together", func(t *testing.T) {
		got := applyOfferConstraints(offers, OfferQuery{MinVRAMGBPerGPU: 16, MaxVRAMGBPerGPU: 48})
		if len(got) != 2 {
			t.Fatalf("expected both offers to survive 16-48GB, got %v", keptIDs(got))
		}
	})
}

// TestApplyOfferConstraints_KnownVRAMOutsideRangeDropped is the other half: a
// provider that DOES report VRAM must actually be held to the constraint, or
// the unknown-passes rule has quietly disabled the filter for everyone.
func TestApplyOfferConstraints_KnownVRAMOutsideRangeDropped(t *testing.T) {
	offers := []Offer{
		{ID: "too-small", GPUModel: "RTX 3080", VRAMGBPerGPU: 10},
		{ID: "just-right", GPUModel: "RTX 4090", VRAMGBPerGPU: 24},
		// A B300 is the reason the ceiling exists: 288GB of HBM hashcat will
		// never touch, priced as though it will.
		{ID: "too-big", GPUModel: "B300", VRAMGBPerGPU: 288},
	}

	got := applyOfferConstraints(offers, OfferQuery{MinVRAMGBPerGPU: 16, MaxVRAMGBPerGPU: 48})
	if len(got) != 1 || got[0].ID != "just-right" {
		t.Fatalf("16-48GB should keep only the 24GB card, got %v", keptIDs(got))
	}
}

/*
 * TestApplyOfferConstraints_DenyBeatsAllow.
 *
 * A deny list is an operator's emergency lever — "that card keeps failing on
 * this hash mode, stop renting it". If an allow list can override it, the lever
 * silently does nothing on exactly the deployments that configured both.
 */
func TestApplyOfferConstraints_DenyBeatsAllow(t *testing.T) {
	offers := []Offer{
		{ID: "denied", GPUModel: "RTX 4090"},
		{ID: "allowed", GPUModel: "RTX 5090"},
	}

	got := applyOfferConstraints(offers, OfferQuery{
		AllowedGPUModels: []string{"rtx_4090", "rtx_5090"},
		DeniedGPUModels:  []string{"rtx_4090"},
	})
	if hasID(got, "denied") {
		t.Fatalf("a denied model survived because it was also allowed; got %v", keptIDs(got))
	}
	if !hasID(got, "allowed") {
		t.Fatalf("deny of one model removed another; got %v", keptIDs(got))
	}
}

// TestApplyOfferConstraints_EmptyAllowMeansAny guards the default. An empty
// allow list read as "allow nothing" returns zero offers for every deployment
// that never configured one, i.e. all of them.
func TestApplyOfferConstraints_EmptyAllowMeansAny(t *testing.T) {
	offers := []Offer{
		{ID: "a", GPUModel: "RTX 4090"},
		{ID: "b", GPUModel: "L40S"},
		{ID: "c", GPUModel: "some card nobody has heard of"},
	}

	got := applyOfferConstraints(offers, OfferQuery{})
	if len(got) != 3 {
		t.Fatalf("an empty allow list must mean any model, got %v", keptIDs(got))
	}

	// And a deny list alone still works without an allow list present.
	got = applyOfferConstraints(offers, OfferQuery{DeniedGPUModels: []string{"l40s"}})
	if len(got) != 2 || hasID(got, "b") {
		t.Fatalf("deny list alone should drop only the denied model, got %v", keptIDs(got))
	}
}

/*
 * TestApplyOfferConstraints_NormalizedModelMatching.
 *
 * Providers spell the same card differently: "NVIDIA GeForce RTX 4090" from one
 * and "RTX 4090" from the next. Matching raw strings makes an operator's
 * allow/deny list appear to work while matching nothing on one provider — the
 * worst version of this bug, because the list looks configured.
 */
func TestApplyOfferConstraints_NormalizedModelMatching(t *testing.T) {
	offers := []Offer{
		{ID: "verbose", GPUModel: "NVIDIA GeForce RTX 4090"},
		{ID: "terse", GPUModel: "RTX 4090"},
		{ID: "other", GPUModel: "RTX A5000"},
	}

	t.Run("allow list written tersely matches both spellings", func(t *testing.T) {
		got := applyOfferConstraints(offers, OfferQuery{AllowedGPUModels: []string{"rtx_4090"}})
		if len(got) != 2 || !hasID(got, "verbose") || !hasID(got, "terse") {
			t.Fatalf("both spellings of a 4090 must match the key rtx_4090, got %v", keptIDs(got))
		}
	})

	t.Run("list written the way a provider spells it also matches", func(t *testing.T) {
		// The operator copy-pasted the model out of a provider's UI. Both sides
		// go through NormalizeGPUModel, so it still works.
		got := applyOfferConstraints(offers, OfferQuery{DeniedGPUModels: []string{"NVIDIA GeForce RTX 4090"}})
		if len(got) != 1 || got[0].ID != "other" {
			t.Fatalf("a deny list written in a provider's own spelling must still match, got %v", keptIDs(got))
		}
	})
}

/*
 * TestApplyOfferConstraints_Availability.
 *
 * Unknown availability is the AWS case again: EC2 only reports capacity by
 * failing a launch. A floor it cannot answer must not remove it. AvailabilityNone
 * is a real, reported answer and must be removed by even the lowest floor.
 */
func TestApplyOfferConstraints_Availability(t *testing.T) {
	tests := []struct {
		name    string
		offer   Offer
		floor   OfferAvailability
		wantKep bool
	}{
		{"unknown passes a high floor", Offer{ID: "aws", Availability: AvailabilityUnknown}, AvailabilityHigh, true},
		{"none fails a low floor", Offer{ID: "empty", Availability: AvailabilityNone}, AvailabilityLow, false},
		{"low meets a low floor", Offer{ID: "low", Availability: AvailabilityLow}, AvailabilityLow, true},
		{"low fails a medium floor", Offer{ID: "low", Availability: AvailabilityLow}, AvailabilityMedium, false},
		{"high meets a medium floor", Offer{ID: "high", Availability: AvailabilityHigh}, AvailabilityMedium, true},
		// With no floor requested, even a reported "none" is the caller's
		// problem to rank, not this filter's to delete.
		{"none survives when no floor is set", Offer{ID: "empty", Availability: AvailabilityNone}, AvailabilityUnknown, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := applyOfferConstraints([]Offer{tc.offer}, OfferQuery{MinAvailability: tc.floor})
			if kept := len(got) == 1; kept != tc.wantKep {
				t.Fatalf("availability %s against floor %s: kept=%v, want %v",
					tc.offer.Availability, tc.floor, kept, tc.wantKep)
			}
		})
	}
}

/*
 * TestVastVRAMGB is the 1024x trap.
 *
 * Vast.ai's gpu_ram is MEGABYTES. Passing it through as GB makes a 24GB card
 * claim 24576GB, which passes every floor and fails every ceiling — the filter
 * appears to run and enforces nothing. The rounding half matters too: Vast
 * reports USABLE VRAM, so a 24GB card is 24564MB and truncation calls it 23.
 */
func TestVastVRAMGB(t *testing.T) {
	tests := []struct {
		name string
		mb   float64
		want int
	}{
		{"a 24GB card is 24, not 24576", 24576, 24},
		{"usable VRAM still rounds to the nominal size", 24564, 24},
		{"a 48GB card", 49140, 48},
		{"an 80GB datacenter card", 81920, 80},
		{"a 16GB card", 16384, 16},
		{"an 11GB 2080 Ti", 11264, 11},
		// Zero in, zero out: that is UNKNOWN and the filter treats it as such.
		{"absent field stays unknown", 0, 0},
		{"a negative reading is unknown, not negative VRAM", -1, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := vastVRAMGB(tc.mb); got != tc.want {
				t.Errorf("vastVRAMGB(%v) = %d, want %d", tc.mb, got, tc.want)
			}
		})
	}
}

// TestVastVRAMGB_FeedsTheFilter closes the loop: the converted value has to
// satisfy the operator's floor expressed in GB. A 24GB card and a 24GB floor
// must agree, which is only true if the conversion and the comparison use the
// same unit.
func TestVastVRAMGB_FeedsTheFilter(t *testing.T) {
	offer := Offer{ID: "vast-4090", GPUModel: "RTX 4090", VRAMGBPerGPU: vastVRAMGB(24564)}

	got := applyOfferConstraints([]Offer{offer}, OfferQuery{MinVRAMGBPerGPU: 24, MaxVRAMGBPerGPU: 32})
	if len(got) != 1 {
		t.Fatalf("a 24564MB card must satisfy a 24GB floor; it converted to %dGB", offer.VRAMGBPerGPU)
	}
}

/*
 * TestApplyOfferConstraints_ReAppliesTheRestOfTheQuery.
 *
 * The reason this function exists at all is that provider-side filters are
 * advisory — Vast.ai accepts a `rented` filter and ignores it. So every
 * constraint that CAN be re-checked against an Offer is re-checked, and this
 * pins that: each case is an offer a provider might return despite the query
 * saying not to.
 */
func TestApplyOfferConstraints_ReAppliesTheRestOfTheQuery(t *testing.T) {
	tests := []struct {
		name    string
		offer   Offer
		query   OfferQuery
		wantKep bool
	}{
		{
			name:    "too few GPUs",
			offer:   Offer{ID: "single", GPUCount: 1},
			query:   OfferQuery{MinGPUCount: 4},
			wantKep: false,
		},
		{
			name:    "unreported GPU count is unknown, not zero GPUs",
			offer:   Offer{ID: "no-count", GPUCount: 0},
			query:   OfferQuery{MinGPUCount: 4},
			wantKep: true,
		},
		{
			name:    "over the hourly cap",
			offer:   Offer{ID: "pricey", HourlyRateCents: 250},
			query:   OfferQuery{MaxHourlyRateCents: 200},
			wantKep: false,
		},
		{
			name:    "exactly at the hourly cap",
			offer:   Offer{ID: "at-cap", HourlyRateCents: 200},
			query:   OfferQuery{MaxHourlyRateCents: 200},
			wantKep: true,
		},
		{
			name:  "expires before the job could finish",
			offer: Offer{ID: "short", MaxDuration: time.Hour},
			query: OfferQuery{MinDuration: 8 * time.Hour},
			// An offer that ends mid-job is spend with nothing to show for it.
			wantKep: false,
		},
		{
			name:    "zero MaxDuration is unbounded, not expired",
			offer:   Offer{ID: "unbounded", MaxDuration: 0},
			query:   OfferQuery{MinDuration: 8 * time.Hour},
			wantKep: true,
		},
		{
			name:    "unverified host against VerifiedOnly",
			offer:   Offer{ID: "unverified", Raw: models.JSONMap{"verified": false}},
			query:   OfferQuery{VerifiedOnly: true},
			wantKep: false,
		},
		{
			name:    "verified host against VerifiedOnly",
			offer:   Offer{ID: "verified", Raw: models.JSONMap{"verified": true}},
			query:   OfferQuery{VerifiedOnly: true},
			wantKep: true,
		},
		{
			name:    "provider that does not report verification passes",
			offer:   Offer{ID: "aws", Raw: nil},
			query:   OfferQuery{VerifiedOnly: true},
			wantKep: true,
		},
		{
			name:    "instance type outside the operator allowlist",
			offer:   Offer{ID: "g6", InstanceType: "g6.xlarge"},
			query:   OfferQuery{AllowedInstanceTypes: []string{"g5.xlarge"}},
			wantKep: false,
		},
		{
			name:    "instance type inside the operator allowlist",
			offer:   Offer{ID: "g5", InstanceType: "g5.xlarge"},
			query:   OfferQuery{AllowedInstanceTypes: []string{"g5.xlarge"}},
			wantKep: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := applyOfferConstraints([]Offer{tc.offer}, tc.query)
			if kept := len(got) == 1; kept != tc.wantKep {
				t.Fatalf("offer %s: kept=%v, want %v", tc.offer.ID, kept, tc.wantKep)
			}
		})
	}
}

// TestApplyOfferConstraints_PreservesOrder pins that this is a filter and
// nothing else. The providers return their offers in a deliberate order and the
// cost-per-work ranker re-orders them afterwards; a filter that shuffled or
// truncated in between would silently change which offer gets rented.
func TestApplyOfferConstraints_PreservesOrder(t *testing.T) {
	offers := []Offer{
		{ID: "first", HourlyRateCents: 27, GPUModel: "RTX A5000", VRAMGBPerGPU: 24},
		{ID: "second", HourlyRateCents: 55, GPUModel: "L40S", VRAMGBPerGPU: 48},
		{ID: "third", HourlyRateCents: 99, GPUModel: "RTX 5090", VRAMGBPerGPU: 32},
	}

	// Limit is a response-size hint, not a viability constraint: truncating here
	// would cut the list in the provider's own price-per-HOUR order, which is
	// exactly the ranking key cost-per-work exists to replace.
	got := applyOfferConstraints(offers, OfferQuery{Limit: 1})
	if len(got) != 3 {
		t.Fatalf("Limit must not truncate the candidate list before ranking, got %v", keptIDs(got))
	}
	for i, want := range []string{"first", "second", "third"} {
		if got[i].ID != want {
			t.Fatalf("order changed: got %v", keptIDs(got))
		}
	}
}
