package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

/*
 * The first tests this adapter has ever had.
 *
 * Until BaseURL became a field the endpoint was a package constant, so there
 * was no way to exercise a single line of vastai.go without a funded Vast.ai
 * account — and consequently none of it ever was. That is why this provider is
 * marked beta: it is fully implemented and has never been paid for. These tests
 * do not remove that gap, but they close the part of it that does not require
 * money: query construction, filter semantics, and the settings that decide
 * which hosts client hash material may land on.
 */

// vastServer stands up a fake marketplace, capturing the last query body so the
// tests can assert on what was actually asked for rather than only what came
// back. Returns the provider and a pointer to the captured query.
func vastServer(t *testing.T, offers []map[string]interface{}) (*VastAIProvider, *map[string]interface{}) {
	t.Helper()
	captured := map[string]interface{}{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v0/bundles/" {
			_ = json.NewDecoder(r.Body).Decode(&captured)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"offers": offers})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	p := NewVastAIProvider("test-key", VastSettings{})
	p.BaseURL = srv.URL
	// The 3s production floor exists for Vast's 429s, which a local server does
	// not have. Leaving it in would add 3s to every test in this file.
	p.MinInterval = 0
	return p, &captured
}

func vastOfferJSON(id int, gpu, geo string, dph float64, rentable, verified bool) map[string]interface{} {
	return map[string]interface{}{
		"id": id, "gpu_name": gpu, "num_gpus": 1, "gpu_ram": 24564,
		"dph_total": dph, "geolocation": geo, "rentable": rentable,
		"verified": verified, "reliability2": 0.99, "duration": 86400.0,
	}
}

func TestVerifiedAndDatacenterAreRestrictedByDefault(t *testing.T) {
	p, q := vastServer(t, []map[string]interface{}{
		vastOfferJSON(1, "RTX 4090", "US, Texas", 0.30, true, true),
	})

	if _, err := p.SearchOffers(context.Background(), OfferQuery{}); err != nil {
		t.Fatalf("SearchOffers: %v", err)
	}
	for _, key := range []string{"verified", "datacenter"} {
		f, ok := (*q)[key].(map[string]interface{})
		if !ok || f["eq"] != true {
			t.Errorf("query is missing %s={eq:true}: %v.\n"+
				"The zero VastSettings must reproduce the historical behaviour exactly. "+
				"Every existing config decodes to it, so a default that silently opened "+
				"the unverified or residential tier would move client hash material onto "+
				"machines the operator never agreed to.", key, (*q)[key])
		}
	}
}

/*
 * Opening a tier must OMIT the key, never send {eq:false}.
 *
 * This is the bug the asymmetry in SearchOffers exists to avoid: false does not
 * mean "don't care", it inverts the filter and returns ONLY unverified hosts,
 * silently excluding every good one. The symptom would be a mysterious collapse
 * in host quality rather than anything that looks like a broken query.
 */
func TestOpeningATierOmitsTheFilterRatherThanInvertingIt(t *testing.T) {
	p, q := vastServer(t, []map[string]interface{}{
		vastOfferJSON(1, "RTX 4090", "US, Texas", 0.30, true, false),
	})
	p.Settings = VastSettings{AllowUnverified: true, AllowResidential: true}

	if _, err := p.SearchOffers(context.Background(), OfferQuery{}); err != nil {
		t.Fatalf("SearchOffers: %v", err)
	}
	for _, key := range []string{"verified", "datacenter"} {
		if v, present := (*q)[key]; present {
			t.Errorf("%s was sent as %v when the tier was opened.\n"+
				"Omit the key. Sending {eq:false} does not relax the filter, it INVERTS "+
				"it: the response becomes unverified hosts ONLY, excluding every verified "+
				"one, and nothing about that looks like an error.", key, v)
		}
	}
}

/*
 * The other half of opening the tier: the shared post-filter has to be told
 * too, or the setting is worse than useless.
 */
func TestAllowUnverifiedAlsoRelaxesTheCallersVerifiedOnly(t *testing.T) {
	unverified := vastOfferJSON(1, "RTX 4090", "US, Texas", 0.30, true, false)

	p, _ := vastServer(t, []map[string]interface{}{unverified})
	offers, err := p.SearchOffers(context.Background(), OfferQuery{VerifiedOnly: true})
	if err != nil {
		t.Fatalf("SearchOffers: %v", err)
	}
	if len(offers) != 0 {
		t.Fatalf("an unverified offer survived VerifiedOnly with the default settings")
	}

	p2, _ := vastServer(t, []map[string]interface{}{unverified})
	p2.Settings = VastSettings{AllowUnverified: true}
	offers, err = p2.SearchOffers(context.Background(), OfferQuery{VerifiedOnly: true})
	if err != nil {
		t.Fatalf("SearchOffers: %v", err)
	}
	if len(offers) != 1 {
		t.Errorf("allow_unverified was set but the offer was still filtered out.\n" +
			"The caller hardcodes VerifiedOnly from before this setting existed. Unless " +
			"narrowQuery clears it, the server returns unverified offers and the shared " +
			"post-filter deletes every one — the operator sees \"no offers\" and nothing " +
			"anywhere says the setting they just enabled is what emptied the list.")
	}
}

func TestCountryAllowListFiltersOnRegion(t *testing.T) {
	p, _ := vastServer(t, []map[string]interface{}{
		vastOfferJSON(1, "RTX 4090", "US, Texas", 0.30, true, true),
		vastOfferJSON(2, "RTX 4090", "DE, Bavaria", 0.32, true, true),
	})
	p.Settings = VastSettings{Countries: []string{"US"}}

	offers, err := p.SearchOffers(context.Background(), OfferQuery{})
	if err != nil {
		t.Fatalf("SearchOffers: %v", err)
	}
	if len(offers) != 1 || offers[0].Region != "US, Texas" {
		t.Fatalf("got %d offers %v, want only the US one.\n"+
			"Offer.Region was written and read by nothing until now; a country allow "+
			"list that does not actually filter is a data-residency control that "+
			"silently does not work.", len(offers), offers)
	}
}

/*
 * "US" must match "US, Texas". Vast reports a country-comma-region string and
 * an operator writes the country, so an equality test would make the obvious
 * entry match nothing at all.
 */
func TestRegionMatchingIsSubstringNotEquality(t *testing.T) {
	cases := []struct {
		region  string
		allowed []string
		want    bool
	}{
		{"US, Texas", []string{"US"}, true},
		{"US, Texas", []string{"us"}, true}, // case-insensitive
		{"US", []string{"US, Texas"}, true}, // and in both directions
		{"DE, Bavaria", []string{"US"}, false},
		{"us-east-2a", []string{"us-east-2"}, true}, // an AWS zone under a region
		{"US, Texas", []string{""}, false},          // a blank entry allows nothing
	}
	for _, c := range cases {
		if got := offerInRegion(c.region, c.allowed); got != c.want {
			t.Errorf("offerInRegion(%q, %v) = %v, want %v", c.region, c.allowed, got, c.want)
		}
	}
}

func TestReliabilityFloorDropsFlakyHosts(t *testing.T) {
	low := vastOfferJSON(1, "RTX 4090", "US, Texas", 0.30, true, true)
	low["reliability2"] = 0.5
	high := vastOfferJSON(2, "RTX 4090", "US, Texas", 0.31, true, true)
	high["reliability2"] = 0.99

	p, _ := vastServer(t, []map[string]interface{}{low, high})
	p.Settings = VastSettings{MinReliability: 0.9}

	offers, err := p.SearchOffers(context.Background(), OfferQuery{})
	if err != nil {
		t.Fatalf("SearchOffers: %v", err)
	}
	if len(offers) != 1 || offers[0].ID != "2" {
		t.Errorf("got %v, want only the reliable host.\n"+
			"The reliability score was captured into Offer.Raw and read by nothing. A "+
			"host that drops the instance mid-chunk has still been paid for its "+
			"commissioning, so this floor buys fewer wasted rentals.", offers)
	}
}

func TestUnknownReliabilityPasses(t *testing.T) {
	noScore := vastOfferJSON(1, "RTX 4090", "US, Texas", 0.30, true, true)
	delete(noScore, "reliability2")

	p, _ := vastServer(t, []map[string]interface{}{noScore})
	p.Settings = VastSettings{MinReliability: 0.9}

	offers, err := p.SearchOffers(context.Background(), OfferQuery{})
	if err != nil {
		t.Fatalf("SearchOffers: %v", err)
	}
	if len(offers) != 1 {
		t.Error("an offer with no reliability score was dropped by the floor.\n" +
			"Unknown passes, everywhere in this filter. A provider that does not publish " +
			"a score has not published a bad one, and treating the two alike is how a " +
			"single setting silently deletes an entire provider.")
	}
}

/*
 * Empty means EVERYTHING in both the caller's list and the operator's, which is
 * what makes plain intersection wrong: it would produce "nothing allowed" from
 * two statements that each said "no restriction".
 */
func TestIntersectOrUnionRespectsEmptyMeaningEverything(t *testing.T) {
	if got := intersectOrUnion(nil, []string{"US"}); len(got) != 1 || got[0] != "US" {
		t.Errorf("empty caller list should yield the operator's list, got %v", got)
	}
	if got := intersectOrUnion([]string{"US"}, nil); len(got) != 1 || got[0] != "US" {
		t.Errorf("empty operator list should yield the caller's list, got %v", got)
	}
	if got := intersectOrUnion([]string{"US", "DE"}, []string{"DE"}); len(got) != 1 || got[0] != "DE" {
		t.Errorf("two populated lists should intersect, got %v", got)
	}

	// The dangerous case: disjoint lists must not collapse to "no restriction".
	got := intersectOrUnion([]string{"US"}, []string{"DE"})
	if len(got) == 0 {
		t.Fatal("two disjoint allow lists produced an empty result, which means " +
			"NO RESTRICTION — so the moment the caller and the operator disagree, both " +
			"of their constraints are silently discarded and anything may be rented")
	}
	if offerInRegion("US, Texas", got) || offerInRegion("DE, Bavaria", got) {
		t.Errorf("the disjoint sentinel %v matched a real region", got)
	}
}

func TestDenyListsAccumulateRatherThanIntersect(t *testing.T) {
	p, _ := vastServer(t, nil)
	p.Settings = VastSettings{DeniedGPUModels: []string{"rtx_3090"}}

	got := p.narrowQuery(OfferQuery{DeniedGPUModels: []string{"rtx_4090"}})
	if len(got.DeniedGPUModels) != 2 {
		t.Errorf("deny lists = %v, want both entries.\n"+
			"A deny list is an emergency lever — \"that card keeps failing, stop renting "+
			"it\" — so it must only ever grow. Intersecting two deny lists would let "+
			"either party's ban be cancelled by the other not having heard of it.",
			got.DeniedGPUModels)
	}
}

func TestExploreCapacityCountsRealMachines(t *testing.T) {
	p, q := vastServer(t, []map[string]interface{}{
		vastOfferJSON(1, "RTX 4090", "US, Texas", 0.30, true, true),
		vastOfferJSON(2, "RTX 4090", "US, Oregon", 0.28, true, true),
		vastOfferJSON(3, "RTX 4090", "DE, Bavaria", 0.35, true, true),
		vastOfferJSON(4, "RTX 3090", "US, Texas", 0.20, true, true),
	})

	report, err := p.ExploreCapacity(context.Background())
	if err != nil {
		t.Fatalf("ExploreCapacity: %v", err)
	}
	if report.PlacementLabel != "Country" || report.HardwareLabel != "GPU model" {
		t.Errorf("axes are %q x %q, want Country x GPU model — the UI renders these "+
			"verbatim, and an operator reading \"zone\" on a Vast screen would go looking "+
			"for a setting that does not exist",
			report.PlacementLabel, report.HardwareLabel)
	}

	// The survey must ask for far more than the launch path's 25, or "no 4090s
	// in Germany" really means "none in the 25 cheapest machines on Earth".
	if limit, _ := (*q)["limit"].(float64); limit < 100 {
		t.Errorf("survey limit is %v; a truncated survey reports absence it cannot "+
			"actually observe", (*q)["limit"])
	}

	var us *PlacementCapacity
	for i := range report.Placements {
		if report.Placements[i].Name == "US" {
			us = &report.Placements[i]
		}
	}
	if us == nil {
		t.Fatalf("no US placement in %v", report.Placements)
	}
	// Both US machines fold into one country row: region-level buckets would
	// fragment the grid and understate availability.
	var found bool
	for _, h := range us.Hardware {
		if h.ID == "RTX 4090" {
			found = true
			if h.AvailableCount != 2 {
				t.Errorf("US/RTX 4090 count = %d, want 2 (Texas + Oregon)", h.AvailableCount)
			}
			if h.LiveCents != 28 {
				t.Errorf("US/RTX 4090 price = %d, want the CHEAPEST (28), not an average: "+
					"the launch path rents the best-ranked offer, so a mean describes a "+
					"machine nobody would take", h.LiveCents)
			}
		}
	}
	if !found {
		t.Error("RTX 4090 missing from the US row")
	}

	// Ordering is by capacity, not the alphabet: on a marketplace the useful
	// question is where the machines are. US (3) must precede DE (1).
	if report.Placements[0].Name != "US" {
		t.Errorf("placements ordered %q first; want the country with the most "+
			"machines", report.Placements[0].Name)
	}
}

func TestExploreCapacityShowsUnselectedCountriesToo(t *testing.T) {
	p, _ := vastServer(t, []map[string]interface{}{
		vastOfferJSON(1, "RTX 4090", "US, Texas", 0.30, true, true),
		vastOfferJSON(2, "RTX 4090", "DE, Bavaria", 0.35, true, true),
	})
	p.Settings = VastSettings{Countries: []string{"US"}}

	report, err := p.ExploreCapacity(context.Background())
	if err != nil {
		t.Fatalf("ExploreCapacity: %v", err)
	}
	if len(report.Placements) != 2 {
		t.Fatalf("got %d placements, want 2.\n"+
			"The survey must relax the operator's own country filter. A screen that only "+
			"shows what is already selected cannot answer the question it exists for — "+
			"\"where else could I be getting GPUs\".", len(report.Placements))
	}
	for _, pc := range report.Placements {
		if pc.Name == "DE" && pc.Selected {
			t.Error("an unselected country is marked selected")
		}
		if pc.Name == "US" && !pc.Selected {
			t.Error("the configured country is not marked selected")
		}
	}
}

func TestEmptyCountrySettingReadsAsEverythingSelected(t *testing.T) {
	p, _ := vastServer(t, []map[string]interface{}{
		vastOfferJSON(1, "RTX 4090", "US, Texas", 0.30, true, true),
		vastOfferJSON(2, "RTX 4090", "DE, Bavaria", 0.35, true, true),
	})

	report, err := p.ExploreCapacity(context.Background())
	if err != nil {
		t.Fatalf("ExploreCapacity: %v", err)
	}
	for _, pc := range report.Placements {
		if !pc.Selected {
			t.Errorf("%s is unticked under empty settings.\n"+
				"Empty means ANYWHERE. Rendering it as nothing-selected inverts what the "+
				"config does, and the operator's first click would NARROW their setup "+
				"while appearing to widen it.", pc.Name)
		}
	}
}

func TestVastCountryExtraction(t *testing.T) {
	cases := map[string]string{
		"US, Texas":   "US",
		"DE, Bavaria": "DE",
		"SE":          "SE",
		"  US, Iowa ": "US",
		"":            "",
	}
	for in, want := range cases {
		if got := vastCountry(in); got != want {
			t.Errorf("vastCountry(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSearchOffersStillDropsUnrentableOffers(t *testing.T) {
	// The server ignores the `rented` filter; the client-side re-check is the
	// only thing that makes it true, and it predates all of this work.
	p, _ := vastServer(t, []map[string]interface{}{
		vastOfferJSON(1, "RTX 4090", "US, Texas", 0.30, false, true),
	})
	offers, err := p.SearchOffers(context.Background(), OfferQuery{})
	if err != nil {
		t.Fatalf("SearchOffers: %v", err)
	}
	if len(offers) != 0 {
		t.Error("an unrentable offer survived; Vast's server ignores the rented " +
			"filter, so the client-side re-check is what makes it real")
	}
}

func TestRateLimitFloorIsRespected(t *testing.T) {
	p, _ := vastServer(t, nil)
	p.MinInterval = 120 * time.Millisecond

	start := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := p.SearchOffers(context.Background(), OfferQuery{}); err != nil {
			// An empty marketplace is an error from SearchOffers; the timing is
			// what is under test.
			_ = err
		}
	}
	// Three calls means two enforced gaps.
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Errorf("three calls took %v, expected at least two %v gaps.\n"+
			"Vast.ai returns 429 with no Retry-After, so this self-imposed floor is the "+
			"only backoff there is.", elapsed, p.MinInterval)
	}
}
