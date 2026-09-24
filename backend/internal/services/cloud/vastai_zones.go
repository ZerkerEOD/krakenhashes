package cloud

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

/*
 * ExploreCapacity surveys the Vast.ai marketplace as a country x GPU-model grid.
 *
 * This report is BETTER EVIDENCE than the AWS one, and the difference is worth
 * understanding because the two screens look alike. AWS can only offer a
 * catalogue fact ("EC2 sells this type in this zone") plus an advisory score
 * that has been measured contradicting reality. Vast.ai is a marketplace: the
 * bundles endpoint returns the ACTUAL MACHINES available to rent right now, so
 * every cell here is a real count and a real price, not a prediction.
 *
 * The query is deliberately run with the operator's OWN filters relaxed except
 * the trust tier. An availability screen has to show what could be selected,
 * not only what already is — a country the operator has not ticked showing
 * forty free 4090s is exactly the information the screen exists to deliver. The
 * verified/datacenter tier is the one thing left applied, because opening it is
 * a security decision that belongs in the settings form with its warning, not
 * in an availability preview that quietly displays hosts the operator has not
 * agreed to use.
 */
func (v *VastAIProvider) ExploreCapacity(ctx context.Context) (*CapacityReport, error) {
	report := &CapacityReport{
		PlacementLabel: "Country",
		HardwareLabel:  "GPU model",
		Signals:        vastSignals(),
	}

	query := map[string]interface{}{
		"external": map[string]interface{}{"eq": false},
		"rentable": map[string]interface{}{"eq": true},
		"rented":   map[string]interface{}{"eq": false},
		"num_gpus": map[string]interface{}{"gte": 1},
		"type":     "ondemand",
		"order":    [][]string{{"dph_total", "asc"}},
		// Far above the launch path's 25: this is a survey, and a truncated one
		// would report "no 4090s in Germany" when the answer is "none in the
		// first 25 cheapest machines worldwide", which is a different and much
		// more misleading statement.
		"limit": 1000,
	}
	// Trust tier stays applied — see the doc comment.
	if !v.Settings.AllowUnverified {
		query["verified"] = map[string]interface{}{"eq": true}
	}
	if !v.Settings.AllowResidential {
		query["datacenter"] = map[string]interface{}{"eq": true}
	}

	var resp struct {
		Offers []vastOffer `json:"offers"`
	}
	if err := v.do(ctx, http.MethodPost, "/api/v0/bundles/", query, &resp); err != nil {
		return nil, fmt.Errorf("vastai: survey marketplace: %w", err)
	}

	if !v.Settings.AllowUnverified || !v.Settings.AllowResidential {
		report.Warnings = append(report.Warnings,
			"showing verified datacenter hosts only. The unverified and residential tiers are excluded "+
				"by your settings, and they are where most of the marketplace's capacity actually is — "+
				"but they place client hash material on machines Vast.ai has not checked.")
	}

	/*
	 * Bucket by country, keeping per-cell counts and the cheapest live price.
	 *
	 * Cheapest rather than mean: the launch path takes the best-ranked offer,
	 * so a mean would describe a machine nobody would rent. The count is the
	 * number that answers "will I get one at all".
	 */
	type cell struct {
		count        int
		cheapestCent int
		gpuCount     int
		vramGB       int
	}
	countries := map[string]map[string]*cell{}
	modelSeen := map[string]int{}

	for _, o := range resp.Offers {
		if !o.Rentable {
			continue
		}
		country := vastCountry(o.Geolocation)
		model := strings.TrimSpace(o.GPUName)
		if country == "" || model == "" {
			continue
		}
		if countries[country] == nil {
			countries[country] = map[string]*cell{}
		}
		c := countries[country][model]
		if c == nil {
			c = &cell{cheapestCent: 1 << 30}
			countries[country][model] = c
		}
		c.count++
		modelSeen[model]++
		if cents := int(o.DPHTotal * 100); cents > 0 && cents < c.cheapestCent {
			c.cheapestCent = cents
		}
		if o.NumGPUs > c.gpuCount {
			c.gpuCount = o.NumGPUs
		}
		if g := vastVRAMGB(o.GPURAM); g > c.vramGB {
			c.vramGB = g
		}
	}

	/*
	 * Hardware columns are the models actually seen, most plentiful first, then
	 * capped. A marketplace this size returns a long tail of one-off cards, and
	 * a grid two hundred columns wide is not a picker.
	 */
	report.Hardware = topModels(modelSeen, vastMaxModelColumns)
	if dropped := len(modelSeen) - len(report.Hardware); dropped > 0 {
		// Never truncate silently: a hidden column reads as "that card does not
		// exist here", which is a different answer from "it is rare".
		report.Warnings = append(report.Warnings, fmt.Sprintf(
			"showing the %d most plentiful GPU models; %d rarer models were found and are not shown. "+
				"They can still be selected by naming them in the model allow list.",
			len(report.Hardware), dropped))
	}

	selectedCountries := lowerSet(v.Settings.Countries)
	selectedModels := lowerSet(v.Settings.GPUModels)

	for country, cells := range countries {
		pc := PlacementCapacity{
			// Vast has one identifier for a country, so ID and Name are the
			// same — unlike AWS, where the alias and the stable id differ.
			ID:   country,
			Name: country,
			Ref:  country,
			// Empty settings mean "anywhere", so everything reads as selected.
			// Showing it all unticked would invert what the config actually does.
			Selected: len(selectedCountries) == 0 || selectedCountries[strings.ToLower(country)],
			Usable:   true,
		}

		var hosts int
		for _, hw := range report.Hardware {
			c := cells[hw]
			hc := HardwareCapacity{
				ID: hw,
				// Offered means "there is at least one rentable machine right
				// now", which on a marketplace is a stronger claim than AWS's
				// catalogue flag — and a more perishable one.
				Offered:  c != nil && c.count > 0,
				Selected: pc.Selected && (len(selectedModels) == 0 || selectedModels[strings.ToLower(hw)]),
				GPUModel: hw,
			}
			if c != nil {
				hc.AvailableCount = c.count
				hc.GPUCount = c.gpuCount
				if c.cheapestCent < 1<<30 {
					hc.LiveCents = c.cheapestCent
				}
				hosts += c.count
			}
			pc.Hardware = append(pc.Hardware, hc)
			if hc.Selected && hc.Offered {
				report.PoolCount++
			}
		}
		pc.Detail = fmt.Sprintf("%d machines available now", hosts)
		report.Placements = append(report.Placements, pc)
	}

	// Most capacity first: on a marketplace the useful ordering is where the
	// machines are, not the alphabet.
	sort.Slice(report.Placements, func(i, j int) bool {
		a, b := placementCount(report.Placements[i]), placementCount(report.Placements[j])
		if a != b {
			return a > b
		}
		return report.Placements[i].Name < report.Placements[j].Name
	})

	if len(report.Placements) == 0 {
		report.Warnings = append(report.Warnings,
			"the marketplace returned no rentable machines at all under the current trust tier. "+
				"This is unusual and more likely a filter or credential problem than an empty market.")
	}
	return report, nil
}

// vastMaxModelColumns caps grid width. Chosen to cover the cards anyone
// actually rents while keeping the table readable.
const vastMaxModelColumns = 12

/*
 * vastCountry reduces Vast's geolocation to the country part.
 *
 * The field is a free-ish string, commonly "US, Texas" or "DE, Bavaria" but
 * sometimes a bare code. Country is the right granularity for the picker:
 * region-level buckets would fragment the grid into dozens of near-empty rows
 * and make the availability numbers look far worse than they are.
 */
func vastCountry(geolocation string) string {
	g := strings.TrimSpace(geolocation)
	if g == "" {
		return ""
	}
	if idx := strings.Index(g, ","); idx > 0 {
		return strings.TrimSpace(g[:idx])
	}
	return g
}

// topModels returns the n most frequently seen keys, ties broken by name so the
// column order does not shuffle between refreshes.
func topModels(seen map[string]int, n int) []string {
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if seen[keys[i]] != seen[keys[j]] {
			return seen[keys[i]] > seen[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if len(keys) > n {
		keys = keys[:n]
	}
	// Alphabetical for display once the top-n cut is made: the cut wants
	// popularity, the reader wants to find a card.
	sort.Strings(keys)
	return keys
}

func placementCount(p PlacementCapacity) int {
	total := 0
	for _, h := range p.Hardware {
		total += h.AvailableCount
	}
	return total
}

func lowerSet(in []string) map[string]bool {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]bool, len(in))
	for _, s := range in {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			out[s] = true
		}
	}
	return out
}

/*
 * vastSignals declares what Vast.ai's grid columns are worth.
 *
 * Note what is absent: there is no score and no configured price. Vast needs
 * neither — it prices live, and it answers the availability question with a
 * count of real machines instead of an opinion. This is the provider whose
 * columns deserve the most trust, which is exactly why the UI must be told, or
 * it would render this grid with the same hedging as AWS's.
 */
func vastSignals() []SignalDescriptor {
	return []SignalDescriptor{{
		Kind:  SignalOffered,
		Label: "Available",
		Trust: TrustMeasured,
		Explanation: "At least one machine with this GPU is rentable in this country right now. " +
			"Real inventory rather than a catalogue entry — but a marketplace moves, so it is a " +
			"snapshot, not a reservation.",
	}, {
		Kind:  SignalAvailableCount,
		Label: "Machines free",
		Trust: TrustMeasured,
		Explanation: "How many rentable machines matched, right now. This is the strongest " +
			"availability signal any provider here gives: it is a count of real hardware, not a score.",
	}, {
		Kind:  SignalLivePrice,
		Label: "From",
		Trust: TrustMeasured,
		Explanation: "The cheapest live total hourly price in this cell, including storage and " +
			"bandwidth. Cheapest rather than average, because the launch path rents the best-ranked " +
			"offer and an average would describe a machine nobody would take.",
	}}
}
