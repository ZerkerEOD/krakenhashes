package cloud

import (
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

/*
 * applyOfferConstraints re-applies every OfferQuery constraint to what a
 * provider actually returned.
 *
 * WHY THIS EXISTS
 *
 * Provider-side filters are ADVISORY. Vast.ai already proves it in this
 * package: the bundles endpoint accepts a `rented` filter, ignores it, and
 * SearchOffers post-filters `rentable` client-side to compensate. Trusting the
 * other filters is the same bet with money on it. A VRAM floor the server
 * quietly drops means renting a card the job OOMs on — paid for a boot, a file
 * sync and nothing else. A deny list the server quietly drops means renting the
 * exact card the operator banned. Server-side filters stay, because they keep
 * the response small, but this function is where the constraints are TRUE.
 *
 * THE RULE THAT MATTERS: UNKNOWN VALUES PASS.
 *
 * Offer.VRAMGBPerGPU == 0 and Availability == AvailabilityUnknown mean the
 * provider does not publish the figure, NOT that the figure is zero. AWS
 * publishes neither. Treating unknown as a failure means the first operator who
 * sets a VRAM floor silently deletes every AWS offer, and the symptom is "AWS
 * stopped being used" with nothing in the logs. So unknowns pass, and each
 * filter call logs once when it let something through on missing data, because
 * a permissive default that is invisible is how the next person concludes the
 * filter is broken.
 *
 * WHAT IS DELIBERATELY NOT RE-APPLIED
 *
 *   - MinDiskGB. Offer carries no disk figure to check it against; the
 *     providers fold it into the price instead (Vast.ai via allocated_storage,
 *     AWS via ebsCentsPerHour), so re-checking here is not possible, only
 *     pretending.
 *   - Limit. It is a response-size hint, not a viability constraint. Truncating
 *     here would cut the candidate list in the PROVIDER's order — dph_total
 *     ascending on Vast.ai — which is price per HOUR, the exact ranking key
 *     cost-per-work exists to replace. Dropping the RTX 5090 that wins on cost
 *     per hash because it was 26th cheapest per hour is a silent ranking bug
 *     with no symptom at all.
 */
func applyOfferConstraints(offers []Offer, q OfferQuery) []Offer {
	allowedModels := normalizedModelSet(q.AllowedGPUModels)
	deniedModels := normalizedModelSet(q.DeniedGPUModels)

	allowedTypes := make(map[string]bool, len(q.AllowedInstanceTypes))
	for _, t := range q.AllowedInstanceTypes {
		if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
			allowedTypes[t] = true
		}
	}

	wantVRAM := q.MinVRAMGBPerGPU > 0 || q.MaxVRAMGBPerGPU > 0
	wantAvailability := q.MinAvailability > AvailabilityUnknown

	var passedOnUnknownVRAM, passedOnUnknownAvailability, droppedByAllowList int
	var droppedByRegion, droppedByReliability int

	kept := make([]Offer, 0, len(offers))
	for _, o := range offers {
		// GPUCount 0 is the same class of unknown as VRAM 0: a provider that
		// does not report a count has not reported "no GPUs".
		if q.MinGPUCount > 0 && o.GPUCount > 0 && o.GPUCount < q.MinGPUCount {
			continue
		}
		if q.MaxHourlyRateCents > 0 && o.HourlyRateCents > q.MaxHourlyRateCents {
			continue
		}
		// MaxDuration 0 means UNBOUNDED per the Offer contract, not "expires
		// immediately" — it satisfies any floor.
		if q.MinDuration > 0 && o.MaxDuration > 0 && o.MaxDuration < q.MinDuration {
			continue
		}
		if q.VerifiedOnly {
			if verified, known := offerVerified(o); known && !verified {
				continue
			}
		}
		// An offer with no instance type reported cannot be matched against the
		// allowlist, so it passes; one that reports a type it does not contain
		// is exactly what the allowlist exists to remove.
		if len(allowedTypes) > 0 && o.InstanceType != "" &&
			!allowedTypes[strings.ToLower(o.InstanceType)] {
			continue
		}

		vramUnknown := o.VRAMGBPerGPU <= 0
		if wantVRAM && !vramUnknown {
			if q.MinVRAMGBPerGPU > 0 && o.VRAMGBPerGPU < q.MinVRAMGBPerGPU {
				continue
			}
			if q.MaxVRAMGBPerGPU > 0 && o.VRAMGBPerGPU > q.MaxVRAMGBPerGPU {
				continue
			}
		}

		// Matching happens on the NORMALISED key so an operator writes
		// "rtx_4090" once and it matches "NVIDIA GeForce RTX 4090" from one
		// provider and "RTX 4090" from the next. Deny wins over allow: a deny
		// list is an operator's emergency lever ("that card keeps failing,
		// stop renting it") and an allow list must never be able to override it.
		if key := NormalizeGPUModel(o.GPUModel); key != "" {
			if deniedModels[key] {
				continue
			}
			if len(allowedModels) > 0 && !allowedModels[key] {
				droppedByAllowList++
				continue
			}
		}

		availabilityUnknown := o.Availability == AvailabilityUnknown
		if wantAvailability && !availabilityUnknown && o.Availability < q.MinAvailability {
			continue
		}

		// Region unknown passes, for the same reason as every unknown above: a
		// provider that does not publish placement has not published "nowhere".
		if len(q.AllowedRegions) > 0 && o.Region != "" && !offerInRegion(o.Region, q.AllowedRegions) {
			droppedByRegion++
			continue
		}

		if q.MinReliability > 0 {
			if rel, known := offerReliability(o); known && rel < q.MinReliability {
				droppedByReliability++
				continue
			}
		}

		kept = append(kept, o)

		// Counted only for offers that SURVIVED, so the log below reports what
		// was actually let through on missing data rather than what happened to
		// be missing data on its way out for some other reason.
		if wantVRAM && vramUnknown {
			passedOnUnknownVRAM++
		}
		if wantAvailability && availabilityUnknown {
			passedOnUnknownAvailability++
		}
	}

	if passedOnUnknownVRAM > 0 {
		debug.Info("cloud offer filter: %d of %d offers report no per-GPU VRAM and were kept against a "+
			"min=%dGB max=%dGB constraint; the provider does not publish VRAM (AWS never does) and "+
			"rejecting unknown would silently remove that provider entirely",
			passedOnUnknownVRAM, len(offers), q.MinVRAMGBPerGPU, q.MaxVRAMGBPerGPU)
	}
	if passedOnUnknownAvailability > 0 {
		debug.Info("cloud offer filter: %d of %d offers report no availability and were kept against a "+
			"%q floor; the provider publishes no stock signal, so this floor cannot be enforced for it",
			passedOnUnknownAvailability, len(offers), q.MinAvailability.String())
	}
	if droppedByAllowList > 0 {
		// Worth its own line: an allow list holds GPU MODEL keys, and an AWS
		// offer's GPUModel is the instance type it was configured under, so a
		// model allow list drops AWS offers wholesale. That is intended — AWS's
		// equivalent lever is the operator-declared instance_type_rates map —
		// but it must not be something an operator has to guess at.
		debug.Info("cloud offer filter: %d of %d offers dropped for not matching allowed GPU models %v",
			droppedByAllowList, len(offers), q.AllowedGPUModels)
	}
	if droppedByRegion > 0 {
		// Logged because a region allow list is the easiest of these to get
		// silently wrong: the strings are provider-shaped and coarse, so a
		// plausible-looking "United States" matches nothing when the provider
		// reports "US, Texas", and the symptom is a provider that quietly stops
		// producing candidates.
		debug.Info("cloud offer filter: %d of %d offers dropped for not matching allowed regions %v; "+
			"region strings are provider-shaped, so check these against what the provider actually reports",
			droppedByRegion, len(offers), q.AllowedRegions)
	}
	if droppedByReliability > 0 {
		debug.Info("cloud offer filter: %d of %d offers dropped below the %.2f host reliability floor",
			droppedByReliability, len(offers), q.MinReliability)
	}

	return kept
}

// normalizedModelSet turns operator-written model names into the same canonical
// keys the offers are matched on. Normalising BOTH sides is the point: a list
// written as "NVIDIA GeForce RTX 4090" has to match an offer spelled "RTX 4090"
// or the allow/deny lists are a trap that appears to work and matches nothing.
func normalizedModelSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		if key := NormalizeGPUModel(n); key != "" {
			set[key] = true
		}
	}
	return set
}

// offerVerified reads the provider's own trust flag back out of Raw. Only
// Vast.ai publishes one, so `known` is false everywhere else — and an unknown
// trust flag passes VerifiedOnly for the same reason unknown VRAM passes a
// floor: the alternative is deleting every offer from every other provider.
func offerVerified(o Offer) (verified, known bool) {
	if o.Raw == nil {
		return false, false
	}
	if v, ok := o.Raw["verified"].(bool); ok {
		return v, true
	}
	return false, false
}

/*
 * offerInRegion matches a provider's coarse placement string against an
 * operator's allow list, case-insensitively and by SUBSTRING in both directions.
 *
 * Equality would be wrong in both directions at once. Vast.ai reports
 * "US, Texas" where an operator writes "US"; AWS reports "us-east-2a" where an
 * operator may well write "us-east-2" meaning the whole region. Requiring an
 * exact match makes the obvious entry silently match nothing, and the symptom
 * is a provider that stops producing candidates with no error anywhere.
 */
func offerInRegion(region string, allowed []string) bool {
	r := strings.ToLower(strings.TrimSpace(region))
	for _, a := range allowed {
		want := strings.ToLower(strings.TrimSpace(a))
		if want == "" {
			continue
		}
		if strings.Contains(r, want) || strings.Contains(want, r) {
			return true
		}
	}
	return false
}

/*
 * offerReliability reads a provider's host reliability score back out of Raw.
 *
 * ZERO IS TREATED AS UNKNOWN, and that is not a rounding convenience — it is
 * the only correct reading. The provider fills Raw from a Go struct field, so a
 * score absent from the JSON decodes to 0.0 and is then written into Raw as a
 * present key with a terrible value. "No data" and "the worst possible host"
 * become indistinguishable at exactly the point the filter reads them.
 *
 * Reading that zero as known deletes EVERY offer the moment a floor is set,
 * which is precisely what happened the first time this was tested — the whole
 * provider silently vanishes from the candidate list with nothing to say why.
 * A real 0.0 host is worth excluding, but not at the price of also excluding
 * every host whose score simply was not reported.
 */
func offerReliability(o Offer) (score float64, known bool) {
	if o.Raw == nil {
		return 0, false
	}
	var v float64
	switch raw := o.Raw["reliability"].(type) {
	case float64:
		v = raw
	case float32:
		v = float64(raw)
	case int:
		v = float64(raw)
	default:
		return 0, false
	}
	if v <= 0 {
		return 0, false
	}
	return v, true
}
