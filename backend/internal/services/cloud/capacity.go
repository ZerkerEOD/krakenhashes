package cloud

import (
	"context"
	"time"
)

/*
 * CapacityExplorer is the read-only "where could I actually get a GPU?" view a
 * provider can offer an operator before they commit to a configuration.
 *
 * Deliberately NOT part of Provider. Not every provider has a placement
 * dimension the operator has to choose, and a mandatory method would force the
 * ones that do not to return an empty report forever. Callers type-assert:
 *
 *	if ex, ok := provider.(CapacityExplorer); ok { ... }
 *
 * EVERY PROVIDER'S SELECTION PROBLEM IS THE SAME SHAPE: a grid of PLACEMENT x
 * HARDWARE, where each ticked cell is one independent chance of getting a GPU.
 * Only the vocabulary differs:
 *
 *	AWS      availability zone  x  instance type
 *	Vast.ai  country            x  GPU model
 *	RunPod   data centre        x  GPU type
 *
 * So the report is one shape with provider-supplied axis labels, rather than
 * three parallel types. What genuinely differs between providers is not the
 * structure but the QUALITY OF THE SIGNALS — see SignalDescriptor, which exists
 * so the UI can stop rendering a guess and a measurement identically.
 *
 * NOTHING HERE GATES A LAUNCH. Every number in the report is advisory except
 * the Offered flag. On AWS the headline signal is the least trustworthy thing
 * on the screen: spot placement scores read 1/10 across all three us-east-2
 * zones at a moment when a g4dn.xlarge launch succeeded on the first try. The
 * scores hint at which pool to TRY FIRST, never at which to strike off the
 * list — the only capacity signal EC2 gives that means anything is a failed
 * RunInstances.
 */
type CapacityExplorer interface {
	// ExploreCapacity enumerates placements and the hardware available in each,
	// annotated with whatever advisory signals the provider exposes.
	//
	// Partial failure is normal and must NOT fail the call: an operator whose
	// IAM role lacks ec2:GetSpotPlacementScores should still get the zone and
	// offering data, with the gap named in Warnings.
	ExploreCapacity(ctx context.Context) (*CapacityReport, error)
}

// CapacityReport is one provider's placement picture.
type CapacityReport struct {
	// Region scopes the report where the provider has one. Empty for providers
	// that are global, like Vast.ai.
	Region string `json:"region,omitempty"`
	/*
	 * PlacementLabel and HardwareLabel name the two axes in the provider's own
	 * vocabulary — "Availability zone" x "Instance type" on AWS, "Country" x
	 * "GPU model" on Vast.ai. The UI renders these as column headings rather
	 * than hardcoding AWS's words, because an operator reading "zone" on a
	 * Vast.ai screen would reasonably go looking for a setting that does not
	 * exist.
	 */
	PlacementLabel string `json:"placement_label"`
	HardwareLabel  string `json:"hardware_label"`

	Placements []PlacementCapacity `json:"placements"`
	// Hardware is every hardware key the operator could select, in a stable
	// order, so the UI can render a fixed column set even for a placement that
	// offers none of them.
	Hardware []string `json:"hardware"`
	/*
	 * Signals declares which per-cell columns this provider can actually fill
	 * and HOW MUCH TO TRUST EACH. Without it the UI would render AWS's
	 * placement score — a relative hint that was measured at 1/10 in every zone
	 * minutes before a successful launch — with exactly the same weight as
	 * Vast.ai's live count of rentable machines, which is real inventory.
	 */
	Signals []SignalDescriptor `json:"signals,omitempty"`
	// Warnings names each signal that could not be collected and what the
	// operator loses by it. An empty report with no warnings is a real "there
	// is nothing here"; an empty report WITH warnings is a permissions problem
	// wearing the same clothes, and the two must not look alike.
	Warnings []string `json:"warnings,omitempty"`
	// PoolCount is the number of selected, actually-offered cells: the number
	// of distinct capacity pools a launch may fall back through. This is the
	// number the whole screen exists to raise.
	PoolCount int `json:"pool_count"`
}

/*
 * dedupeStrings removes repeated warnings while preserving order.
 *
 * Providers collect warnings from several independent lookups, and more than
 * one of them can fail for the same underlying reason — a single missing IAM
 * permission is read by both the per-cell and per-placement score queries. The
 * operator should be told once.
 */
func dedupeStrings(in []string) []string {
	if len(in) < 2 {
		return in
	}
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// SignalKind identifies a per-cell column so the UI can bind it.
type SignalKind string

const (
	// SignalOffered is the provider saying this hardware exists in this
	// placement at all. Definitive where present.
	SignalOffered SignalKind = "offered"
	// SignalAvailableCount is a live count of rentable units. Real inventory.
	SignalAvailableCount SignalKind = "available_count"
	// SignalLivePrice is what the provider is charging right now.
	SignalLivePrice SignalKind = "live_price"
	// SignalConfiguredPrice is the operator's own declared rate, which is what
	// the budget reserves against. Only providers whose live pricing cannot be
	// trusted have one — on AWS the Pricing API silently returns wrong SKUs.
	SignalConfiguredPrice SignalKind = "configured_price"
	// SignalScore is a provider's own 1-10 ranking of a pool.
	SignalScore SignalKind = "score"
)

// SignalTrust is how much weight the UI should give a signal.
type SignalTrust string

const (
	// TrustDefinitive means acting on it cannot be wrong: the provider is
	// stating a fact about its own catalogue, not predicting inventory.
	TrustDefinitive SignalTrust = "definitive"
	// TrustMeasured means it is a real observation that may be stale or may
	// not survive to launch time — a live price, a live stock count.
	TrustMeasured SignalTrust = "measured"
	// TrustAdvisory means it is the provider's opinion and has been observed
	// to be misleading. Order by it; never filter on it.
	TrustAdvisory SignalTrust = "advisory"
)

/*
 * SignalDescriptor tells the UI what one column means and how far to trust it.
 *
 * The Explanation is shown to the operator verbatim, and it should say what the
 * signal is worth rather than what it is called. "AWS's own 1-10 ranking, which
 * read 1/10 in every zone minutes before a launch succeeded" is useful;
 * "placement score" is not.
 */
type SignalDescriptor struct {
	Kind        SignalKind  `json:"kind"`
	Label       string      `json:"label"`
	Trust       SignalTrust `json:"trust"`
	Explanation string      `json:"explanation"`
}

// PlacementCapacity is one placement the operator can tick: an AWS zone, a
// Vast.ai country, a RunPod data centre.
type PlacementCapacity struct {
	// ID is the stable key the provider uses internally and the one to join
	// any capacity API on. On AWS this is the zone ID (use2-az1), which is
	// distinct from the per-account Name alias — two accounts' "us-east-2a"
	// generally point at different datacentres.
	ID string `json:"id"`
	// Name is what to show a human. Falls back to ID when the provider has only
	// one identifier, which is the common case outside AWS.
	Name string `json:"name"`
	// Ref is what a launch actually needs to reach this placement: a subnet id
	// on AWS, a data centre id on RunPod, a geolocation string on Vast.ai.
	// Empty means the provider places instances itself.
	Ref string `json:"ref,omitempty"`
	// Detail is provider-specific context shown under the name — a subnet's
	// name and free-address count, a country's host count.
	Detail string `json:"detail,omitempty"`
	// Selected reflects the CURRENT configuration, so the screen opens showing
	// what is live rather than a blank slate.
	Selected bool               `json:"selected"`
	Hardware []HardwareCapacity `json:"hardware"`
	// Score is a placement-wide ranking where the provider has one, 1-10, ZERO
	// MEANING UNKNOWN. On AWS this is the placement score for all priced
	// instance types taken TOGETHER, which is a different and usually much
	// better number than any single type's — see the aws_zones.go comment for
	// the measurement.
	Score int `json:"score,omitempty"`
	// Usable is false when this placement can never launch. Notes says which.
	Usable bool     `json:"usable"`
	Notes  []string `json:"notes,omitempty"`
}

// HardwareCapacity is one cell of the placement x hardware grid.
type HardwareCapacity struct {
	// ID is the provider's key — an AWS instance type, a Vast.ai GPU name, a
	// RunPod gpuType id. This is what gets written into the launch request.
	ID string `json:"id"`
	/*
	 * Offered is the provider saying it sells this hardware in this placement.
	 * FALSE IS A HARD NO and the only definitive signal in the grid; everything
	 * else is a probability.
	 *
	 * Providers that cannot answer must report true, not false. "We could not
	 * find out" rendered as "not available" hides capacity that exists, and the
	 * operator has no way to tell the two apart.
	 */
	Offered  bool `json:"offered"`
	Selected bool `json:"selected"`
	// ConfiguredCents is the operator's own declared rate where the provider
	// has one. Zero when the provider prices live.
	ConfiguredCents int `json:"configured_cents,omitempty"`
	/*
	 * LiveCents is what the provider is charging right now, ZERO MEANING
	 * UNKNOWN — never free.
	 *
	 * Worth surfacing next to ConfiguredCents wherever both exist, because the
	 * two drift and only one of them is real. One deployment had g4dn.xlarge
	 * declared at 53c while spot was quoting 27c. Reservations are denominated
	 * in the CONFIGURED figure, so drifting high merely over-reserves, but an
	 * operator who priced BELOW the live rate is under-reserving against a cap
	 * they were told was hard.
	 */
	LiveCents  int       `json:"live_cents,omitempty"`
	ObservedAt time.Time `json:"observed_at,omitempty"`
	/*
	 * AvailableCount is a live count of rentable units, ZERO MEANING UNKNOWN.
	 *
	 * This is the strongest per-cell signal any provider gives, and only the
	 * marketplace providers give it: Vast.ai returns rentable machines and
	 * RunPod returns totalCount minus rentedCount. AWS has no equivalent — the
	 * only way EC2 reports capacity is by failing a launch.
	 */
	AvailableCount int `json:"available_count,omitempty"`
	/*
	 * Score is the provider's 1-10 ranking for THIS cell, ZERO MEANING UNKNOWN.
	 *
	 * Read it as "relative to other pools right now", never as a probability.
	 * Measured on AWS: 1/10 in every zone, minutes before a launch in one of
	 * them succeeded first try. It orders the list. It must never shorten it.
	 */
	Score int `json:"score,omitempty"`
	// GPUModel and GPUCount describe what the cell actually is where the
	// hardware key does not say so on its own. An AWS instance type implies a
	// card the API never names, so a blank model here is exactly why an offer
	// ranks as unknown hardware in the cost-per-work ladder.
	GPUModel string `json:"gpu_model,omitempty"`
	GPUCount int    `json:"gpu_count,omitempty"`
	// Note is a per-cell caveat, e.g. "declared but not priced".
	Note string `json:"note,omitempty"`
}
