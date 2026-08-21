package cloud

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
)

// These tests are pure: no database, no network, no clock. They cover the
// arithmetic that decides WHICH hardware the feature rents, which is the
// difference between paying for a job once and paying for it three times.

const (
	// referenceSpeed is a round absolute speed for one reference GPU. Its value
	// is arbitrary on purpose: it is the anchor that cancels out of the ORDER,
	// so every relative expectation below holds whatever it is set to.
	referenceSpeed = int64(100_000_000_000)
)

var rankBase = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func obsMap(rows ...ObservedSpeed) map[string]ObservedSpeed {
	m := make(map[string]ObservedSpeed, len(rows))
	for _, r := range rows {
		m[observationKey(r.Provider, r.GPUKey, r.GPUCount)] = r
	}
	return m
}

// speedRow builds an observation dated now, which is what "fresh" means for
// every case that is not specifically about staleness.
func speedRow(p models.CloudProvider, key string, count int, speed int64, samples int) ObservedSpeed {
	return ObservedSpeed{
		Provider:    p,
		GPUKey:      key,
		GPUCount:    count,
		Speed:       speed,
		SampleCount: samples,
		UpdatedAt:   rankBase,
	}
}

func nearly(got, want float64) bool {
	const eps = 1e-9
	scale := math.Abs(want)
	if scale < 1 {
		scale = 1
	}
	return math.Abs(got-want) <= eps*scale
}

func offerOf(id, model string, count, rateCents int) Offer {
	return Offer{ID: id, GPUModel: model, GPUCount: count, HourlyRateCents: rateCents}
}

func rankIDs(ranked []RankedOffer) []string {
	ids := make([]string, 0, len(ranked))
	for _, r := range ranked {
		ids = append(ids, r.ID)
	}
	return ids
}

func TestSpeedConfidenceOrdering(t *testing.T) {
	// Greater is better evidence, and the ranker uses that ordering directly as
	// a tie-break. Cross-provider deliberately sits BELOW a thin local sample:
	// a measurement of the hardware we would actually rent beats a measurement
	// of the same die in someone else's chassis.
	ordered := []SpeedConfidence{
		ConfidenceNone, ConfidenceClassOnly, ConfidenceCrossTier,
		ConfidenceProvisional, ConfidenceObserved,
	}
	for i := 1; i < len(ordered); i++ {
		if !(ordered[i-1] < ordered[i]) {
			t.Errorf("%s must rank below %s", ordered[i-1], ordered[i])
		}
	}

	names := map[SpeedConfidence]string{
		ConfidenceNone:        "none",
		ConfidenceClassOnly:   "class_only",
		ConfidenceCrossTier:   "cross_tier",
		ConfidenceProvisional: "provisional",
		ConfidenceObserved:    "observed",
	}
	for c, want := range names {
		if got := c.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
	}
}

func TestObservationKey(t *testing.T) {
	base := observationKey(models.CloudProviderVastAI, "rtx_4090", 1)

	tests := []struct {
		name string
		key  string
	}{
		{"different GPU count", observationKey(models.CloudProviderVastAI, "rtx_4090", 2)},
		{"different provider", observationKey(models.CloudProviderAWS, "rtx_4090", 1)},
		{"different model", observationKey(models.CloudProviderVastAI, "rtx_5090", 1)},
		// The separator must not be something NormalizeGPUModel can emit, or a
		// model whose slug ends in a digit collides with a GPU count.
		{"model that would collide under an underscore separator",
			observationKey(models.CloudProviderVastAI, "rtx_4090_1", 1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.key == base {
				t.Errorf("key %q collides with %q", tc.key, base)
			}
		})
	}

	if observationKey(models.CloudProviderVastAI, "rtx_4090", 1) != base {
		t.Error("observationKey must be stable for the same inputs")
	}
}

func TestMultiGPUEfficiency(t *testing.T) {
	tests := []struct {
		n    int
		want float64
	}{
		{0, 1.00},
		{1, 1.00},
		{2, 0.97},
		{4, 0.91},
		// The linear haircut would reach 0.79 at eight cards; the floor holds
		// it, because past that we are guessing about a regime we have no data
		// for and a continued line eventually reaches zero.
		{8, 0.80},
		{16, 0.80},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("x%d", tc.n), func(t *testing.T) {
			got := multiGPUEfficiency(tc.n)
			if !nearly(got, tc.want) {
				t.Errorf("multiGPUEfficiency(%d) = %v, want %v", tc.n, got, tc.want)
			}
			if got > 1 || got < multiGPUEfficiencyFloor {
				t.Errorf("multiGPUEfficiency(%d) = %v, outside [%v, 1]", tc.n, got, multiGPUEfficiencyFloor)
			}
		})
	}

	// Monotonically non-increasing: more cards in a box never scales better
	// than fewer.
	for n := 2; n <= 12; n++ {
		if multiGPUEfficiency(n) > multiGPUEfficiency(n-1) {
			t.Errorf("efficiency rose from x%d to x%d", n-1, n)
		}
	}
}

// TestCalibrationAnchor covers the step that puts measured hashes/sec onto the
// same scale as the class table. Without it a blend would be adding a speed to
// a ratio.
func TestCalibrationAnchor(t *testing.T) {
	tests := []struct {
		name         string
		rows         []ObservedSpeed
		wantAnchored bool
		wantAnchor   float64
	}{
		{
			name: "no observations at all",
			rows: nil,
		},
		{
			// Tier 1: the reference card's own speed IS the anchor, because its
			// relative value is 1.00 by definition.
			name: "reference GPU at the sample floor anchors directly",
			rows: []ObservedSpeed{
				speedRow(models.CloudProviderVastAI, "rtx_4090", 1, referenceSpeed, 10),
				speedRow(models.CloudProviderVastAI, "rtx_5090", 1, 200_000_000_000, 10),
			},
			wantAnchored: true, wantAnchor: 1e11,
		},
		{
			// The multi-GPU haircut is undone first, or an anchor taken from a
			// dual-GPU box would be about 3% low and every relative estimate
			// would inherit the error.
			name: "reference GPU observed on two cards",
			rows: []ObservedSpeed{
				speedRow(models.CloudProviderVastAI, "rtx_4090", 2, 194_000_000_000, 10),
			},
			wantAnchored: true, wantAnchor: 1e11,
		},
		{
			// A thin reference measurement does not get to set the scale on its
			// own, but it still votes in the median.
			name: "thin reference falls back to the implied median",
			rows: []ObservedSpeed{
				speedRow(models.CloudProviderVastAI, "rtx_4090", 1, referenceSpeed, 1),
				speedRow(models.CloudProviderVastAI, "rtx_5090", 1, 200_000_000_000, 10),
			},
			// The thin rtx_4090 row is below MinSamples so it does not vote in
			// the fallback either -- a row that may not own its own estimate
			// must certainly not set the scale every other card is measured
			// against. Only the 10-sample 5090 counts: 200e9 / 1.60.
			wantAnchored: true, wantAnchor: 1.25e11,
		},
		{
			// Median, not mean: one throttled or noisy-neighbour host must not
			// drag the scale every other card is measured against.
			name: "median resists one bad host",
			rows: []ObservedSpeed{
				speedRow(models.CloudProviderVastAI, "rtx_5090", 1, 160_000_000_000, 5),
				speedRow(models.CloudProviderAWS, "rtx_a5000", 1, 26_000_000_000, 5),
				speedRow(models.CloudProviderVastAI, "rtx_3090", 1, 8_000_000_000, 5),
			},
			// implied: 100e9, 100e9, 20e9 -> median 100e9.
			wantAnchored: true, wantAnchor: 1e11,
		},
		{
			// A family prior is deliberately PESSIMISTIC, so dividing a real
			// measurement by one inflates the implied reference (90e9 / 0.30
			// implies 300e9, not the true 100e9). An inflated anchor makes every
			// MEASURED card look slower while class-only cards keep their table
			// value, so the ranker would prefer hardware it has never measured.
			// No scale at all is better than a biased one.
			name: "family-only observation cannot anchor",
			rows: []ObservedSpeed{
				speedRow(models.CloudProviderVastAI, "rtx_5070_ti", 1, 90_000_000_000, 5),
			},
		},
		{
			name: "a recognised model outranks a family guess",
			rows: []ObservedSpeed{
				speedRow(models.CloudProviderVastAI, "rtx_5070_ti", 1, 180_000_000_000, 5),
				speedRow(models.CloudProviderVastAI, "rtx_a5000", 1, 26_000_000_000, 5),
			},
			// Only the recognised rtx_a5000 row counts: 26e9 / 0.26.
			wantAnchored: true, wantAnchor: 1e11,
		},
		{
			// Deliberately NOT anchored: an anchor derived from hardware the
			// table has never heard of is calibrated against a guess, and would
			// then rescale every known model's prior against that guess.
			name: "unknown models alone cannot anchor",
			rows: []ObservedSpeed{
				speedRow(models.CloudProviderVastAI, "acme_z1", 1, 50_000_000_000, 20),
			},
		},
		{
			// Below MinSamples: fromObservation already refuses to let such a
			// row own its own estimate, and letting it set the global scale
			// would spread that error to every other model.
			name: "a thin row cannot anchor",
			rows: []ObservedSpeed{
				speedRow(models.CloudProviderVastAI, "rtx_4090", 1, 100_000_000_000, 1),
			},
		},
		{
			name: "a zero-speed row is not evidence",
			rows: []ObservedSpeed{
				speedRow(models.CloudProviderVastAI, "rtx_4090", 1, 0, 20),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newThroughputResolver(RankInput{
				Provider:          models.CloudProviderVastAI,
				Observed:          obsMap(tc.rows...),
				Class:             DefaultGPUClasses,
				MinSamples:        4,
				MaxObservationAge: 24 * time.Hour,
				UnknownRelative:   DefaultUnknownRelative,
				Now:               rankBase,
			})
			if r.anchored != tc.wantAnchored {
				t.Fatalf("anchored = %v, want %v (anchor %v)", r.anchored, tc.wantAnchored, r.anchor)
			}
			if tc.wantAnchored && !nearly(r.anchor, tc.wantAnchor) {
				t.Errorf("anchor = %v, want %v", r.anchor, tc.wantAnchor)
			}
		})
	}
}

// TestResolveThroughputLadder walks every rung of the resolution ladder.
func TestResolveThroughputLadder(t *testing.T) {
	// Present in every case that needs the observed rungs readable: it fixes
	// the reference speed so relative expectations are exact.
	anchorRow := speedRow(models.CloudProviderVastAI, "rtx_4090", 1, referenceSpeed, 10)

	tests := []struct {
		name         string
		provider     models.CloudProvider
		rows         []ObservedSpeed
		gpuModel     string
		gpuCount     int
		wantRel      float64
		wantConf     SpeedConfidence
		wantEvidence string
	}{
		{
			name:     "rung 1: exact shape at the sample floor is believed outright",
			provider: models.CloudProviderVastAI,
			rows: []ObservedSpeed{anchorRow,
				speedRow(models.CloudProviderVastAI, "rtx_5090", 1, 160_000_000_000, 8)},
			gpuModel: "RTX 5090", gpuCount: 1,
			wantRel: 1.60, wantConf: ConfidenceObserved, wantEvidence: "observed, 8 samples",
		},
		{
			// The first sample on a peer marketplace often comes from a
			// throttled box. Believing 0.80x outright would sink the 5090 in the
			// cost-per-work order and it would never be rented again, so the bad
			// number would never be corrected.
			name:     "rung 2: below the floor blends with the class prior",
			provider: models.CloudProviderVastAI,
			rows: []ObservedSpeed{anchorRow,
				speedRow(models.CloudProviderVastAI, "rtx_5090", 1, 80_000_000_000, 1)},
			gpuModel: "RTX 5090", gpuCount: 1,
			// w = 1/4: 0.25*0.80 + 0.75*1.60
			wantRel: 1.40, wantConf: ConfidenceProvisional,
			wantEvidence: "provisional, 1 of 4 samples blended with the class prior",
		},
		{
			name:     "rung 2: nothing to blend with is still better than the unknown default",
			provider: models.CloudProviderVastAI,
			rows: []ObservedSpeed{anchorRow,
				speedRow(models.CloudProviderVastAI, "acme_z1", 1, 50_000_000_000, 1)},
			gpuModel: "Acme Z1", gpuCount: 1,
			wantRel: 0.50, wantConf: ConfidenceProvisional,
			wantEvidence: "provisional, 1 samples, no class prior to temper it",
		},
		{
			name:     "rung 3: same provider and model, different GPU count",
			provider: models.CloudProviderVastAI,
			rows: []ObservedSpeed{anchorRow,
				speedRow(models.CloudProviderVastAI, "rtx_5090", 1, 160_000_000_000, 8)},
			gpuModel: "RTX 5090", gpuCount: 2,
			// Per-GPU throughput carries over; the x2 haircut is applied once,
			// in rankOffers, not here.
			wantRel: 1.60, wantConf: ConfidenceProvisional,
			wantEvidence: "observed, 8 samples, rescaled from a x1 observation",
		},
		{
			name:     "rung 3: a thin source demotes twice",
			provider: models.CloudProviderVastAI,
			rows: []ObservedSpeed{anchorRow,
				speedRow(models.CloudProviderVastAI, "rtx_5090", 1, 80_000_000_000, 1)},
			gpuModel: "RTX 5090", gpuCount: 4,
			wantRel: 1.40, wantConf: ConfidenceCrossTier,
			wantEvidence: "provisional, 1 of 4 samples blended with the class prior, rescaled from a x1 observation",
		},
		{
			name:     "rung 3: sample count beats closeness of GPU count",
			provider: models.CloudProviderVastAI,
			rows: []ObservedSpeed{anchorRow,
				speedRow(models.CloudProviderVastAI, "rtx_5090", 1, 160_000_000_000, 30),
				speedRow(models.CloudProviderVastAI, "rtx_5090", 2, 155_200_000_000, 2)},
			gpuModel: "RTX 5090", gpuCount: 4,
			wantRel: 1.60, wantConf: ConfidenceProvisional,
			wantEvidence: "observed, 30 samples, rescaled from a x1 observation",
		},
		{
			name:     "rung 4: same model on another provider",
			provider: models.CloudProviderAWS,
			rows: []ObservedSpeed{anchorRow,
				speedRow(models.CloudProviderVastAI, "rtx_5090", 1, 160_000_000_000, 8)},
			gpuModel: "RTX 5090", gpuCount: 1,
			wantRel: 1.60, wantConf: ConfidenceCrossTier,
			wantEvidence: "observed on vastai, 8 samples",
		},
		{
			name:     "rung 5: no observation, exact class prior",
			provider: models.CloudProviderVastAI,
			rows:     []ObservedSpeed{anchorRow},
			gpuModel: "RTX A5000", gpuCount: 1,
			wantRel: 0.26, wantConf: ConfidenceClassOnly, wantEvidence: "class prior",
		},
		{
			// 0.30 is the Blackwell family fallback, which is deliberately the
			// LOW end of that family rather than near its typical value. An
			// unlisted 5070 Ti is a mid-range part, and an optimistic guess
			// about it would make it beat a real 5090 on cost per work. See
			// TestFamilyFallbacksArePessimistic.
			name:     "rung 5: unlisted model estimated from its family",
			provider: models.CloudProviderVastAI,
			rows:     []ObservedSpeed{anchorRow},
			gpuModel: "RTX 5070 Ti", gpuCount: 1,
			wantRel: 0.30, wantConf: ConfidenceClassOnly,
			wantEvidence: "class prior for its GPU family",
		},
		{
			name:     "rung 6: nothing knows this model",
			provider: models.CloudProviderVastAI,
			rows:     []ObservedSpeed{anchorRow},
			gpuModel: "Acme Z1", gpuCount: 1,
			wantRel: DefaultUnknownRelative, wantConf: ConfidenceNone,
			wantEvidence: "unknown model, pessimistic default",
		},
		{
			// A host that swapped its GPU, or a hashcat version bump, invalidates
			// old numbers. A stale row is worse than no row: it outranks the
			// class prior while being wrong.
			name:     "a stale exact observation is discarded, not believed",
			provider: models.CloudProviderVastAI,
			rows: []ObservedSpeed{anchorRow, {
				Provider: models.CloudProviderVastAI, GPUKey: "rtx_5090", GPUCount: 1,
				Speed: 80_000_000_000, SampleCount: 50, UpdatedAt: rankBase.Add(-48 * time.Hour),
			}},
			gpuModel: "RTX 5090", gpuCount: 1,
			wantRel: 1.60, wantConf: ConfidenceClassOnly, wantEvidence: "class prior",
		},
		{
			name:     "a stale row does not block a fresh one of another shape",
			provider: models.CloudProviderVastAI,
			rows: []ObservedSpeed{anchorRow,
				{Provider: models.CloudProviderVastAI, GPUKey: "rtx_5090", GPUCount: 1,
					Speed: 80_000_000_000, SampleCount: 50, UpdatedAt: rankBase.Add(-48 * time.Hour)},
				speedRow(models.CloudProviderVastAI, "rtx_5090", 2, 310_400_000_000, 8)},
			gpuModel: "RTX 5090", gpuCount: 1,
			// 310.4e9 over two cards with the 0.97 haircut undone is 160e9 each.
			wantRel: 1.60, wantConf: ConfidenceProvisional,
			wantEvidence: "observed, 8 samples, rescaled from a x2 observation",
		},
		{
			// Observations that cannot be placed on the reference scale are not
			// evidence, so the ladder falls through rather than inventing one.
			name:     "observations with no anchor fall through to the class table",
			provider: models.CloudProviderVastAI,
			rows: []ObservedSpeed{
				speedRow(models.CloudProviderVastAI, "acme_z1", 1, 50_000_000_000, 20)},
			gpuModel: "RTX 5090", gpuCount: 1,
			wantRel: 1.60, wantConf: ConfidenceClassOnly, wantEvidence: "class prior",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newThroughputResolver(RankInput{
				Provider:          tc.provider,
				Work:              WorkSignature{AttackMode: 0, HashType: 1000, SaltCount: intPtr(1)},
				Observed:          obsMap(tc.rows...),
				Class:             DefaultGPUClasses,
				MinSamples:        4,
				MaxObservationAge: 24 * time.Hour,
				UnknownRelative:   DefaultUnknownRelative,
				Now:               rankBase,
			})

			rel, conf, evidence := r.resolveThroughput(NormalizeGPUModel(tc.gpuModel), tc.gpuCount)
			if !nearly(rel, tc.wantRel) {
				t.Errorf("relative = %v, want %v", rel, tc.wantRel)
			}
			if conf != tc.wantConf {
				t.Errorf("confidence = %s, want %s", conf, tc.wantConf)
			}
			if evidence != tc.wantEvidence {
				t.Errorf("evidence = %q, want %q", evidence, tc.wantEvidence)
			}
		})
	}
}

/*
 * TestRankOffersColdTable is the worked example from gpuclass.go: a fresh
 * deployment with no observations at all still orders offers correctly, because
 * ranking needs only relative throughput.
 *
 * Sorting by price per hour picks the A5000 (27c against 99c) and pays more per
 * hash for it. That is the bug this whole file exists to fix.
 */
func TestRankOffersColdTable(t *testing.T) {
	ranked, reason := rankOffers(RankInput{
		Provider: models.CloudProviderVastAI,
		Candidates: []Offer{
			offerOf("a5000", "RTX A5000", 1, 27),
			offerOf("5090", "RTX 5090", 1, 99),
		},
		Now: rankBase,
	})

	if got := rankIDs(ranked); got[0] != "5090" {
		t.Fatalf("order = %v, want the 5090 first despite costing 3.7x per hour", got)
	}
	// 99 / 1.60 against 27 / 0.26.
	if !nearly(ranked[0].CostPerWorkUnitCents, 61.875) {
		t.Errorf("5090 cost per work unit = %v, want 61.875", ranked[0].CostPerWorkUnitCents)
	}
	if !nearly(ranked[1].CostPerWorkUnitCents, 27.0/0.26) {
		t.Errorf("A5000 cost per work unit = %v, want %v", ranked[1].CostPerWorkUnitCents, 27.0/0.26)
	}
	for _, r := range ranked {
		if r.Confidence != ConfidenceClassOnly {
			t.Errorf("%s confidence = %s, want class_only on a cold table", r.ID, r.Confidence)
		}
	}
	if reason == "" {
		t.Error("the winner must carry a reason")
	}
}

// TestRankOffersWinnerReason pins the operator-facing sentence. It has to name
// the numbers, the way budget.go's reasons do, or "why did it pick that box" is
// unanswerable after the fact.
func TestRankOffersWinnerReason(t *testing.T) {
	ranked, reason := rankOffers(RankInput{
		Provider: models.CloudProviderVastAI,
		Candidates: []Offer{
			offerOf("4090", "RTX 4090", 1, 34),
			offerOf("a5000", "RTX A5000", 1, 14),
		},
		Observed: obsMap(
			speedRow(models.CloudProviderVastAI, "rtx_4090", 1, referenceSpeed, 7)),
		MinSamples:        3,
		MaxObservationAge: 24 * time.Hour,
		Now:               rankBase,
	})

	want := "RTX 4090 x1 at 34c/hr, 1.00x reference throughput (observed, 7 samples) = " +
		"34.0 cents per reference-GPU-hour; next best 53.8 (RTX A5000 x1)"
	if reason != want {
		t.Errorf("reason =\n  %q\nwant\n  %q", reason, want)
	}
	if ranked[0].Reason == "" || ranked[1].Reason == "" {
		t.Error("every ranked offer carries its own reason, not just the winner")
	}
}

/*
 * TestRankOffersUnknownDoesNotWin is the pessimism trap, stated as a test.
 *
 * Cost per work unit divides by throughput, so an OPTIMISTIC guess about
 * hardware nothing has measured produces the lowest cost and wins. The system
 * would then rent exactly the hardware it understands least, every time, and
 * the next unknown card would win for the same reason. The same input with the
 * two defaults flips the order, which is the whole argument.
 */
func TestRankOffersUnknownDoesNotWin(t *testing.T) {
	candidates := []Offer{
		offerOf("4090", "RTX 4090", 1, 34),
		offerOf("mystery", "Acme Z1", 1, 30),
	}

	tests := []struct {
		name            string
		unknownRelative float64
		wantFirst       string
	}{
		{"pessimistic default keeps the known card in front", DefaultUnknownRelative, "4090"},
		{"unset field falls back to the pessimistic default", 0, "4090"},
		{"an optimistic default would hand the unknown card the job", 1.5, "mystery"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ranked, _ := rankOffers(RankInput{
				Provider:        models.CloudProviderVastAI,
				Candidates:      candidates,
				UnknownRelative: tc.unknownRelative,
				Now:             rankBase,
			})
			if got := rankIDs(ranked); got[0] != tc.wantFirst {
				t.Errorf("order = %v, want %s first", got, tc.wantFirst)
			}
		})
	}
}

// TestRankOffersProvisionalBlend checks that a single hostile sample moves the
// order without owning it: the model drops below where its prior put it, but not
// to where one throttled host would put it.
func TestRankOffersProvisionalBlend(t *testing.T) {
	in := RankInput{
		Provider: models.CloudProviderVastAI,
		Candidates: []Offer{
			offerOf("5090", "RTX 5090", 1, 99),
			offerOf("a5000", "RTX A5000", 1, 27),
		},
		Observed: obsMap(
			speedRow(models.CloudProviderVastAI, "rtx_4090", 1, referenceSpeed, 10),
			// One sample, at half the speed the table expects.
			speedRow(models.CloudProviderVastAI, "rtx_5090", 1, 80_000_000_000, 1)),
		MinSamples:        4,
		MaxObservationAge: 24 * time.Hour,
		Now:               rankBase,
	}

	ranked, _ := rankOffers(in)
	if ranked[0].ID != "5090" {
		t.Fatalf("order = %v, want the 5090 still in front", rankIDs(ranked))
	}
	if ranked[0].Confidence != ConfidenceProvisional {
		t.Errorf("confidence = %s, want provisional", ranked[0].Confidence)
	}
	// Blended 1.40x rather than the measured 0.80x or the prior 1.60x.
	if !nearly(ranked[0].RelativeThroughput, 1.40) {
		t.Errorf("relative throughput = %v, want the blended 1.40", ranked[0].RelativeThroughput)
	}

	// Once the samples pass the floor the measurement owns the estimate, and
	// the 5090 loses the ranking it only held on the strength of its prior.
	in.Observed = obsMap(
		speedRow(models.CloudProviderVastAI, "rtx_4090", 1, referenceSpeed, 10),
		speedRow(models.CloudProviderVastAI, "rtx_5090", 1, 80_000_000_000, 9))
	ranked, _ = rankOffers(in)
	if ranked[0].ID != "a5000" {
		t.Fatalf("order = %v, want the A5000 once the 5090 is measured slow", rankIDs(ranked))
	}
	if ranked[1].Confidence != ConfidenceObserved || !nearly(ranked[1].RelativeThroughput, 0.80) {
		t.Errorf("5090 = %v at %s, want the measured 0.80 observed",
			ranked[1].RelativeThroughput, ranked[1].Confidence)
	}
}

// TestRankOffersMultiGPU checks the haircut is applied exactly once: a box of
// four cards is worth a little under four, and an observation taken at a
// different count is not charged for it twice.
func TestRankOffersMultiGPU(t *testing.T) {
	ranked, _ := rankOffers(RankInput{
		Provider: models.CloudProviderVastAI,
		Candidates: []Offer{
			offerOf("quad", "RTX 4090", 4, 136),
			offerOf("single", "RTX 4090", 1, 34),
		},
		Observed: obsMap(
			speedRow(models.CloudProviderVastAI, "rtx_4090", 1, referenceSpeed, 10)),
		MinSamples:        4,
		MaxObservationAge: 24 * time.Hour,
		Now:               rankBase,
	})

	// Four cards at four times the price are slightly worse per unit of work,
	// which is the honest answer: they share a PCIe complex and a thermal
	// budget.
	if ranked[0].ID != "single" {
		t.Fatalf("order = %v, want the single card first", rankIDs(ranked))
	}
	quad := ranked[1]
	if !nearly(quad.RelativeThroughput, 4*0.91) {
		t.Errorf("quad relative throughput = %v, want %v", quad.RelativeThroughput, 4*0.91)
	}
	if !nearly(quad.CostPerWorkUnitCents, 136/(4*0.91)) {
		t.Errorf("quad cost = %v, want %v", quad.CostPerWorkUnitCents, 136/(4*0.91))
	}
}

// TestRankOffersUnrankableSortLast covers the divide-by-zero guards. None of
// these may produce +Inf or NaN: a NaN sort key compares false against
// everything, which makes the order depend on the input permutation.
func TestRankOffersUnrankableSortLast(t *testing.T) {
	ranked, reason := rankOffers(RankInput{
		Provider: models.CloudProviderVastAI,
		Candidates: []Offer{
			offerOf("no-gpus", "RTX 4090", 0, 50),
			offerOf("negative-gpus", "RTX 4090", -2, 50),
			// A free GPU is a parse failure, not a gift.
			offerOf("free", "RTX 4090", 1, 0),
			offerOf("negative-price", "RTX 4090", 1, -10),
			offerOf("real", "RTX A5000", 1, 27),
		},
		Now: rankBase,
	})

	if ranked[0].ID != "real" {
		t.Fatalf("order = %v, want the only rankable offer first", rankIDs(ranked))
	}
	for _, r := range ranked[1:] {
		if r.CostPerWorkUnitCents != unrankableCost {
			t.Errorf("%s cost = %v, want the unrankable sentinel", r.ID, r.CostPerWorkUnitCents)
		}
		if math.IsInf(r.CostPerWorkUnitCents, 0) || math.IsNaN(r.CostPerWorkUnitCents) {
			t.Errorf("%s cost is not finite: %v", r.ID, r.CostPerWorkUnitCents)
		}
		if r.Reason == "" {
			t.Errorf("%s must say why it is unrankable", r.ID)
		}
	}
	// Nothing is dropped: the caller asked for an order, and silently
	// shortening the list reports "no offer satisfies the request" for the
	// wrong reason.
	if len(ranked) != 5 {
		t.Errorf("ranked %d offers, want all 5 returned", len(ranked))
	}
	// The unrankable runner-up is not advertised as a fallback price.
	if reason != ranked[0].Reason {
		t.Errorf("reason = %q, want no next-best clause when the runner-up is unrankable", reason)
	}
}

func TestRankOffersEmpty(t *testing.T) {
	ranked, reason := rankOffers(RankInput{Provider: models.CloudProviderVastAI, Now: rankBase})
	if len(ranked) != 0 {
		t.Errorf("ranked = %v, want empty", ranked)
	}
	if reason == "" {
		t.Error("an empty ranking still owes the caller an explanation")
	}
}

/*
 * TestRankOffersDeterminism guards the property that makes this ranker
 * auditable: the same set of offers produces the same order regardless of the
 * order the provider listed them in, and regardless of Go's randomised map
 * iteration over the observation table.
 *
 * Without it, "why did we rent a different box this time" has no answer.
 */
func TestRankOffersDeterminism(t *testing.T) {
	candidates := []Offer{
		offerOf("d", "RTX 4090", 1, 34),
		offerOf("b", "RTX 4090", 1, 34),
		offerOf("c", "RTX A5000", 1, 27),
		offerOf("a", "RTX 4090", 1, 34),
		// Two unrankable offers whose only difference is how well the model is
		// understood: the confidence tie-break has to settle them.
		offerOf("known-broken", "RTX 4090", 0, 40),
		offerOf("unknown-broken", "Acme Z1", 0, 40),
	}
	observed := obsMap(
		speedRow(models.CloudProviderVastAI, "rtx_4090", 1, referenceSpeed, 10),
		speedRow(models.CloudProviderVastAI, "rtx_5090", 1, 160_000_000_000, 6),
		speedRow(models.CloudProviderAWS, "rtx_5090", 1, 150_000_000_000, 6),
		speedRow(models.CloudProviderVastAI, "rtx_a5000", 2, 50_440_000_000, 6),
		speedRow(models.CloudProviderVastAI, "rtx_3090", 1, 40_000_000_000, 6),
	)

	newInput := func(offers []Offer) RankInput {
		return RankInput{
			Provider:          models.CloudProviderVastAI,
			Candidates:        offers,
			Observed:          observed,
			MinSamples:        4,
			MaxObservationAge: 24 * time.Hour,
			Now:               rankBase,
		}
	}

	first, firstReason := rankOffers(newInput(candidates))
	wantIDs := rankIDs(first)

	// Identical cost falls to the ID, so the three 4090s come out sorted.
	if wantIDs[0] != "a" || wantIDs[1] != "b" || wantIDs[2] != "d" {
		t.Errorf("tied offers = %v, want them ordered by ID", wantIDs[:3])
	}
	// Among unrankable offers the better-understood model still comes first.
	if wantIDs[len(wantIDs)-1] != "unknown-broken" {
		t.Errorf("order = %v, want the unknown broken offer dead last", wantIDs)
	}

	for i := 0; i < 100; i++ {
		// Rotate the input so no run sees the same permutation.
		rotated := append(append([]Offer{}, candidates[i%len(candidates):]...),
			candidates[:i%len(candidates)]...)
		got, reason := rankOffers(newInput(rotated))
		gotIDs := rankIDs(got)
		for j := range wantIDs {
			if gotIDs[j] != wantIDs[j] {
				t.Fatalf("run %d order = %v, want %v", i, gotIDs, wantIDs)
			}
		}
		if reason != firstReason {
			t.Fatalf("run %d reason = %q, want %q", i, reason, firstReason)
		}
	}
}

// TestRankOffersCrossTierIsNotLocalEvidence checks the confidence a
// cross-provider measurement earns, since that is what decides ties between two
// offers that cost the same per unit of work.
func TestRankOffersCrossTierIsNotLocalEvidence(t *testing.T) {
	ranked, _ := rankOffers(RankInput{
		Provider:   models.CloudProviderAWS,
		Candidates: []Offer{offerOf("5090", "RTX 5090", 1, 99)},
		Observed: obsMap(
			speedRow(models.CloudProviderVastAI, "rtx_4090", 1, referenceSpeed, 10),
			speedRow(models.CloudProviderVastAI, "rtx_5090", 1, 160_000_000_000, 8)),
		MinSamples:        4,
		MaxObservationAge: 24 * time.Hour,
		Now:               rankBase,
	})

	if ranked[0].Confidence != ConfidenceCrossTier {
		t.Errorf("confidence = %s, want cross_tier for a measurement from another provider",
			ranked[0].Confidence)
	}
	if !nearly(ranked[0].RelativeThroughput, 1.60) {
		t.Errorf("relative throughput = %v, want the measured 1.60", ranked[0].RelativeThroughput)
	}
}
