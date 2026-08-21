package cloud

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

/*
 * Cost-per-work ranking of cloud offers.
 *
 * WHY NOT PRICE PER HOUR
 *
 * Ranking offers by dph/hourly rate is the wrong key for renting compute. An
 * RTX A5000 at 27c/hr is not cheaper than an RTX 5090 at 99c/hr if it is a
 * quarter of the speed — it is substantially more expensive per hash, and a
 * price-per-hour sort picks it every single time. The key that matters is cost
 * per unit of WORK: cents per hour divided by throughput.
 *
 * WHY A COLD BENCHMARK TABLE STILL RANKS CORRECTLY
 *
 * The divisor only has to be RELATIVE throughput. The absolute anchor (how many
 * hashes/sec a reference GPU does on this attack mode and hash type) is a
 * constant common to every candidate, so it cancels out of the ORDER even
 * though it changes every offer's printed number. That is what lets a fresh
 * deployment with zero observations rank offers correctly from the static class
 * table alone, and it is why the ladder below can mix a measured speed with a
 * table prior at all: everything is converted onto the same reference scale
 * first.
 *
 * WHAT THIS FILE IS NOT
 *
 * It decides ORDER, not affordability. Whether the winning offer can be paid
 * for is budget.go's job, and whether it will finish in time is estimator.go's.
 * Keeping those apart means each is a pure function over its own inputs and can
 * be exercised as a table of cases rather than through a provider and a
 * database.
 */

// SpeedConfidence records how good the evidence behind a throughput estimate
// is. Ordered so a greater value is better evidence, which makes it usable
// directly as a sort tie-break.
type SpeedConfidence int

const (
	// ConfidenceNone: the model appears in neither the observations nor the
	// class table. The estimate is RankInput.UnknownRelative, a guess.
	ConfidenceNone SpeedConfidence = iota
	// ConfidenceClassOnly: the static generation table, never measured here.
	ConfidenceClassOnly
	// ConfidenceCrossTier: measured, but on a DIFFERENT provider. Peer
	// marketplaces and hyperscalers do not host identical hardware — same die,
	// different cooling, different power limit — so this is real evidence about
	// the model and weak evidence about what we would actually rent.
	ConfidenceCrossTier
	// ConfidenceProvisional: measured here, but below the sample floor.
	ConfidenceProvisional
	// ConfidenceObserved: measured here, at or above the sample floor.
	ConfidenceObserved
)

func (c SpeedConfidence) String() string {
	switch c {
	case ConfidenceClassOnly:
		return "class_only"
	case ConfidenceCrossTier:
		return "cross_tier"
	case ConfidenceProvisional:
		return "provisional"
	case ConfidenceObserved:
		return "observed"
	default:
		return "none"
	}
}

/*
 * WorkSignature is the attack the ranking is for.
 *
 * It is a SCOPE DECLARATION, not an input to the arithmetic: RankInput.Observed
 * must already contain only rows measured for this signature, because a GPU's
 * relative standing changes with the mode. The spread between a 4090 and an
 * A5000 on fast unsalted MD5 is far wider than on bcrypt, so blending an MD5
 * observation into a bcrypt ranking would produce a confident wrong answer.
 *
 * Carrying it here rather than leaving it implicit means the caller has to name
 * the signature it queried on at the point it hands the rows over.
 */
type WorkSignature struct {
	AttackMode int
	HashType   int
	// SaltCount is nil when the hash type is unsalted, matching the nullable
	// column in cloud_gpu_benchmarks.
	SaltCount *int
}

// ObservedSpeed is one measured row from cloud_gpu_benchmarks, already narrowed
// to a single WorkSignature.
type ObservedSpeed struct {
	Provider models.CloudProvider
	// GPUKey is ALREADY normalised by NormalizeGPUModel. Storing a raw provider
	// spelling here makes the row unmatchable: the read path normalises, so an
	// unnormalised write is a row that is never read again.
	GPUKey   string
	GPUCount int
	// Speed is as observed, i.e. for all GPUCount GPUs together, not per GPU.
	Speed       int64
	SampleCount int
	UpdatedAt   time.Time
}

/*
 * DefaultUnknownRelative is the throughput assumed for a GPU model that is in
 * neither the observations nor the class table: half a reference card.
 *
 * IT MUST BE PESSIMISTIC. This is the single most important number in this
 * file. Cost per work unit is rate/throughput, so an OPTIMISTIC default makes
 * the unknown model divide by a big number, produce the lowest cost, and win —
 * meaning the system would systematically rent exactly the hardware it
 * understands least, then rank the next unknown card first for the same reason,
 * forever. A pessimistic default fails the other way: an unknown card has to be
 * genuinely cheap before it displaces a known one.
 *
 * Stated honestly, a constant cannot really settle this. It is an
 * explore/exploit tension — never renting unknown hardware means never learning
 * what it does, and this table only improves by observing. The pessimistic
 * constant chooses "exploit" and leaves exploration to the operator, whose
 * levers are OfferQuery.AllowedGPUModels to force a trial and
 * OfferQuery.DeniedGPUModels to shut one out. The deny list, not this number,
 * is the right place to express "never rent that".
 */
const DefaultUnknownRelative = 0.5

/*
 * DefaultMinSamples is the observation count at or above which a measurement is
 * believed outright rather than blended with the class prior.
 *
 * Five, because the first measurement on a peer marketplace often comes from a
 * thermally-throttled or noisy-neighbour host. At five samples one bad host
 * moves the estimate by a fifth rather than defining it.
 */
const DefaultMinSamples = 5

/*
 * DefaultMaxObservationAge discards measurements old enough to be about
 * different hardware or a different hashcat.
 *
 * Thirty days: long enough that a rarely-rented GPU model keeps its history,
 * short enough that a host which swapped its card, or a hashcat version bump
 * that changed a kernel's throughput, ages out rather than being trusted
 * forever at full confidence.
 */
const DefaultMaxObservationAge = 30 * 24 * time.Hour

/*
 * unrankableCost is the sort key for an offer whose cost per work unit cannot
 * be computed — no usable throughput estimate, or no usable price.
 *
 * A sentinel rather than the arithmetic's natural +Inf or NaN: NaN compares
 * false against everything, so a NaN sort key makes the comparison
 * non-transitive and the resulting order depends on the input permutation. That
 * is a bug that only shows up as "the ranker picked a different offer this
 * time" in production, with no way to reproduce it.
 */
const unrankableCost = math.MaxFloat64

// RankInput is everything the ranker needs. Every field is data; nothing here
// reads a clock, a database or a provider.
type RankInput struct {
	// Provider is the provider every Candidate came from. The ladder needs it to
	// tell "measured on the hardware we would actually rent" from "measured
	// somewhere else", which is a two-rung confidence difference.
	Provider   models.CloudProvider
	Candidates []Offer
	Work       WorkSignature
	// Observed is keyed by observationKey. Rows must all be for Work.
	Observed map[string]ObservedSpeed
	// Class is the static prior. Nil means DefaultGPUClasses. An empty table is
	// NOT "no priors": GPUClassTable.Relative still falls through to the
	// architecture family map, which is a property of the software rather than
	// of the table.
	Class GPUClassTable
	// MinSamples is the count at which an observation is believed outright.
	// Below it the observation is blended with the class prior. Values below 1
	// are treated as 1.
	MinSamples int
	// MaxObservationAge discards rows older than itself. Zero disables the
	// check.
	MaxObservationAge time.Duration
	// UnknownRelative is the estimate for a model nothing knows about. Values at
	// or below zero are replaced with DefaultUnknownRelative — an unset field is
	// an unset field, not a policy of refusing unknown hardware. See the
	// constant for why this must be pessimistic.
	UnknownRelative float64
	Now             time.Time
}

// RankedOffer is one candidate with its cost-per-work verdict.
type RankedOffer struct {
	Offer
	// RelativeThroughput is the WHOLE offer's throughput in reference-GPU units,
	// so a 2x RTX 4090 box is a little under 2.00 rather than 1.00. Zero means
	// no usable estimate.
	RelativeThroughput float64
	// CostPerWorkUnitCents is the ranking key, lower wins: cents per hour of one
	// reference GPU's worth of work.
	CostPerWorkUnitCents float64
	Confidence           SpeedConfidence
	Reason               string
}

/*
 * observationKey identifies one row of the observed-speed table.
 *
 * The separator must be a character NormalizeGPUModel cannot emit — it collapses
 * everything outside [a-z0-9] to "_", so a "_" separator would let
 * ("rtx_4090", 1) and a hypothetical model normalising to "rtx_4090_1" collide.
 * A collision here is silent: the ranker would read one card's speed under
 * another card's name and stay wrong until someone benchmarked by hand.
 */
func observationKey(p models.CloudProvider, gpuKey string, gpuCount int) string {
	return fmt.Sprintf("%s|%s|%d", p, gpuKey, gpuCount)
}

const (
	multiGPUPenaltyPerGPU = 0.03
	// multiGPUEfficiencyFloor stops the linear haircut before it becomes
	// nonsense. Past roughly eight cards the behaviour is a different regime we
	// have no data for, and continuing the line would eventually reach zero and
	// rank a large box as doing no work at all.
	multiGPUEfficiencyFloor = 0.80
)

/*
 * multiGPUEfficiency is the haircut for putting N GPUs in one box.
 *
 * Hashcat scales close to linearly across GPUs in a single host but not
 * exactly: they share one PCIe complex, one CPU feeding candidates, one power
 * envelope and one thermal budget, and the second card in a dense chassis runs
 * hotter than the first.
 *
 * Deliberately a small per-GPU haircut rather than a per-count lookup table.
 * The real curve is specific to the chassis, the cooling and the attack mode,
 * so a table would be a longer way of writing the same guess while implying a
 * precision it does not have — and any real multi-GPU observation supersedes
 * this the moment one reports in.
 */
func multiGPUEfficiency(n int) float64 {
	if n <= 1 {
		return 1
	}
	eff := 1 - multiGPUPenaltyPerGPU*float64(n-1)
	if eff < multiGPUEfficiencyFloor {
		return multiGPUEfficiencyFloor
	}
	return eff
}

/*
 * rankOffers orders candidates by cost per unit of work, cheapest first, and
 * returns the reason the winner won.
 *
 * Pure, like decideBudgetAction: the whole point is that the decision which
 * spends money can be exercised as a table of cases instead of through a
 * provider search and a database read.
 */
func rankOffers(in RankInput) ([]RankedOffer, string) {
	r := newThroughputResolver(in)

	ranked := make([]RankedOffer, 0, len(in.Candidates))
	for _, offer := range in.Candidates {
		key := NormalizeGPUModel(offer.GPUModel)
		perGPU, confidence, evidence := r.resolveThroughput(key, offer.GPUCount)
		total := perGPU * float64(offer.GPUCount) * multiGPUEfficiency(offer.GPUCount)

		ro := RankedOffer{Offer: offer, RelativeThroughput: total, Confidence: confidence}
		switch {
		case total <= 0 || math.IsNaN(total) || math.IsInf(total, 0):
			// An offer claiming no GPUs, or a throughput estimate that came out
			// non-finite. Ranked last rather than dropped: the caller asked what
			// the order is, and silently shortening its list is how "no offer
			// satisfies the request" gets reported for the wrong reason.
			ro.RelativeThroughput = 0
			ro.CostPerWorkUnitCents = unrankableCost
			ro.Reason = fmt.Sprintf("%s x%d at %dc/hr: no usable throughput estimate (%s), ranked last",
				offer.GPUModel, offer.GPUCount, offer.HourlyRateCents, evidence)
		case offer.HourlyRateCents <= 0:
			// A free GPU is a parse failure, not a gift. Ranking it first would
			// send every launch at whatever offer the provider adapter broke on.
			ro.CostPerWorkUnitCents = unrankableCost
			ro.Reason = fmt.Sprintf("%s x%d has no usable price (%d cents/hr), ranked last",
				offer.GPUModel, offer.GPUCount, offer.HourlyRateCents)
		default:
			cost := float64(offer.HourlyRateCents) / total
			if math.IsInf(cost, 0) || math.IsNaN(cost) {
				ro.CostPerWorkUnitCents = unrankableCost
				ro.Reason = fmt.Sprintf("%s x%d at %dc/hr: cost per work unit is not finite, ranked last",
					offer.GPUModel, offer.GPUCount, offer.HourlyRateCents)
				break
			}
			ro.CostPerWorkUnitCents = cost
			ro.Reason = fmt.Sprintf("%s x%d at %dc/hr, %.2fx reference throughput (%s) = %.1f cents per reference-GPU-hour",
				offer.GPUModel, offer.GPUCount, offer.HourlyRateCents, total, evidence, cost)
		}
		ranked = append(ranked, ro)
	}

	/*
	 * Every tie-break is total, so the order is a function of the input set and
	 * not of the order the provider happened to list it in. Without the trailing
	 * ID comparison two identically-priced offers would swap between runs, and
	 * "why did we rent a different box this time" is unanswerable after the
	 * fact.
	 */
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if a.CostPerWorkUnitCents != b.CostPerWorkUnitCents {
			return a.CostPerWorkUnitCents < b.CostPerWorkUnitCents
		}
		if a.Confidence != b.Confidence {
			return a.Confidence > b.Confidence
		}
		if a.HourlyRateCents != b.HourlyRateCents {
			return a.HourlyRateCents < b.HourlyRateCents
		}
		return a.ID < b.ID
	})

	if len(ranked) == 0 {
		return ranked, "no candidate offers to rank"
	}
	reason := ranked[0].Reason
	if len(ranked) > 1 && ranked[1].CostPerWorkUnitCents < unrankableCost {
		reason += fmt.Sprintf("; next best %.1f (%s x%d)",
			ranked[1].CostPerWorkUnitCents, ranked[1].GPUModel, ranked[1].GPUCount)
	}
	return ranked, reason
}

// usableObservation is an observation that survived the freshness and sanity
// filters, with its per-GPU speed precomputed.
type usableObservation struct {
	key string
	obs ObservedSpeed
	// perGPU is the speed of ONE of its GPUs, with the multi-GPU haircut undone
	// so that observations taken at different GPU counts are comparable.
	perGPU float64
}

/*
 * throughputResolver holds the per-ranking state the ladder needs: the usable
 * observations and the calibration anchor that puts them on the same scale as
 * the class table.
 *
 * A struct rather than free functions taking a dozen arguments, and built once
 * per ranking rather than per candidate, because the anchor is a median over
 * every observation — recomputing it for each offer would be the same answer at
 * N times the cost, and worse, would invite someone to "optimise" it into a
 * per-offer anchor, which is exactly the bug that makes the ordering depend on
 * which offer is being looked at.
 */
type throughputResolver struct {
	in  RankInput
	obs []usableObservation
	// anchor is the absolute speed of ONE reference GPU for this work
	// signature. anchored is false when no observation could establish it.
	anchor   float64
	anchored bool
}

func newThroughputResolver(in RankInput) *throughputResolver {
	// Normalise the input so every rung below can assume sane values. Zero
	// fields are unset fields, not policy.
	if in.MinSamples < 1 {
		in.MinSamples = 1
	}
	if in.UnknownRelative <= 0 {
		in.UnknownRelative = DefaultUnknownRelative
	}
	if in.Class == nil {
		in.Class = DefaultGPUClasses
	}

	/*
	 * A zero Now silently disables staleness, which is worse than the staleness
	 * it was meant to prevent.
	 *
	 * With Now == time.Time{}, Now.Sub(UpdatedAt) is a large NEGATIVE duration
	 * that can never exceed MaxObservationAge, so every row passes — and
	 * future-dated rows are kept on purpose, so nothing downstream catches it
	 * either. A caller who populated Observed and MaxObservationAge but forgot
	 * Now would believe year-old benchmarks at full confidence, with nothing in
	 * the output to say the filter never ran.
	 *
	 * Defaulting to the wall clock is right rather than lazy: it is what every
	 * caller means, and tests that care about staleness set Now explicitly.
	 */
	if in.Now.IsZero() {
		in.Now = time.Now()
	}

	/*
	 * A zero Provider makes every local observation look cross-provider.
	 *
	 * Rung 1's key becomes "|rtx_4090|1" and matches nothing; rung 3's
	 * same-provider predicate matches nothing; and rung 4's "different
	 * provider" predicate matches EVERYTHING. The result is that a Vast.ai
	 * 5090 gets priced from an AWS measurement — precisely the distinction
	 * ConfidenceCrossTier exists to make. There is no sane default, so refuse
	 * to use observations at all rather than use them wrongly.
	 */
	if in.Provider == "" && len(in.Observed) > 0 {
		debug.Warning("cloud ranker: RankInput.Provider is empty with %d observation(s); "+
			"ignoring them rather than treating every row as cross-provider", len(in.Observed))
		in.Observed = nil
	}

	r := &throughputResolver{in: in}
	for key, o := range in.Observed {
		if !r.usable(o) {
			continue
		}
		eff := multiGPUEfficiency(o.GPUCount)
		r.obs = append(r.obs, usableObservation{
			key:    key,
			obs:    o,
			perGPU: float64(o.Speed) / (float64(o.GPUCount) * eff),
		})
	}
	// Map iteration order is randomised, so anything downstream that scans this
	// slice would otherwise pick a different winner between two equally good
	// observations on different runs. Sorting by key once here is what makes
	// every "first match wins" below deterministic.
	sort.Slice(r.obs, func(i, j int) bool { return r.obs[i].key < r.obs[j].key })

	r.calibrate()
	return r
}

/*
 * usable rejects observations that cannot be trusted or cannot be divided by.
 *
 * Age is the important one. A marketplace host that swapped its GPU, or a
 * hashcat version bump that changed the kernels, invalidates every number
 * measured before it, and a stale row is worse than no row: it outranks the
 * class prior on the confidence ladder while being wrong.
 *
 * A row dated in the FUTURE is kept rather than discarded. Small clock skew
 * between this process and whatever wrote the row is normal, and discarding on
 * it would throw away the newest evidence at precisely the moment it arrived.
 */
func (r *throughputResolver) usable(o ObservedSpeed) bool {
	if o.Speed <= 0 || o.GPUCount <= 0 {
		return false
	}
	if r.in.MaxObservationAge > 0 && r.in.Now.Sub(o.UpdatedAt) > r.in.MaxObservationAge {
		return false
	}
	return true
}

/*
 * calibrate finds the absolute speed of one reference GPU for this work
 * signature, which is what converts a measured hashes/sec into the relative
 * units the class table speaks.
 *
 * Three tiers, best first:
 *
 *  1. The reference GPU itself, measured with enough samples. Its relative
 *     value is 1.00 by definition, so its per-GPU speed IS the anchor.
 *  2. The median anchor implied by every observation of a model the class table
 *     recognises: speed / prior. Median, not mean, because one thermally
 *     throttled or noisy-neighbour host would drag a mean and rescale every
 *     other card's estimate with it.
 *  3. The same over models only matched at family level, which is a coarser
 *     prior but still beats having no scale at all.
 *
 * Deliberately NOT a fourth tier over unrecognised models: an anchor derived
 * from hardware the table has never heard of is calibrated against
 * UnknownRelative, a guess, and it would then silently rescale every KNOWN
 * model's prior against that guess. Better to admit there is no anchor and let
 * the ladder fall through to the class table, which at least is internally
 * consistent.
 *
 * A consequence worth being explicit about: with exactly one observation and no
 * reference measurement, tier 2 makes that observation self-anchoring — its
 * relative value comes back out as its own prior and the ranking is unchanged.
 * That is correct. One measurement establishes a scale, not a comparison; the
 * second model to report in is where real information starts.
 */
func (r *throughputResolver) calibrate() {
	var reference *usableObservation
	for i := range r.obs {
		o := &r.obs[i]
		if o.obs.GPUKey != referenceGPU {
			continue
		}
		if reference == nil || o.obs.SampleCount > reference.obs.SampleCount {
			reference = o
		}
	}
	if reference != nil && reference.obs.SampleCount >= r.in.MinSamples {
		r.anchor, r.anchored = reference.perGPU, true
		return
	}

	/*
	 * Fallback anchor, derived from whatever else has been measured.
	 *
	 * Only rows at or above MinSamples get a vote. fromObservation already
	 * refuses to let a thin sample own its OWN estimate; letting that same row
	 * set the scale every other card is measured against would be strictly
	 * worse, because the error is no longer contained to one model.
	 *
	 * Concretely, without this floor: MinSamples=4, observations are a 5090
	 * with 8 samples (implying a 100e9 anchor) and a throttled 3090 with 1
	 * sample (implying 20e9). The median of two values IS their mean, so the
	 * anchor lands at 60e9, the 5090 resolves to 2.67x instead of 1.60x, and a
	 * single bad row about an unrelated card wins the ranking for a different
	 * one. The median provides no robustness at n=2, which is the normal early
	 * state, so it cannot be the only defence.
	 */
	/*
	 * Only EXACTLY recognised models may anchor, and only at or above
	 * MinSamples.
	 *
	 * A family-derived anchor is not a weaker version of a good anchor, it is
	 * biased in a specific direction. Family priors are deliberately
	 * pessimistic (see familyRelative), so dividing a real measurement by one
	 * INFLATES the implied reference: an unlisted Blackwell card measured at
	 * 90e9 against a 0.30 family prior implies a 300e9 reference rather than
	 * the true 100e9.
	 *
	 * The anchor converts observed absolute speeds into relative units, so an
	 * inflated anchor makes every MEASURED card look proportionally slower
	 * while class-only cards keep their table value untouched. The ranker would
	 * then systematically prefer hardware it has never measured over hardware
	 * it has — the exact opposite of what observations are for.
	 *
	 * With no recognised model measured, there is simply no scale. Leaving
	 * anchored false is correct: every offer falls back to its class prior,
	 * which is consistent and unbiased, and the first benchmark on any listed
	 * card fixes it permanently.
	 */
	var recognised []float64
	for _, o := range r.obs {
		if o.obs.SampleCount < r.in.MinSamples {
			continue
		}
		rel, exact := r.in.Class.Relative(o.obs.GPUKey)
		if !exact || rel <= 0 {
			continue
		}
		recognised = append(recognised, o.perGPU/rel)
	}
	if len(recognised) > 0 {
		r.anchor, r.anchored = median(recognised), true
	}
}

func median(values []float64) float64 {
	sort.Float64s(values)
	mid := len(values) / 2
	if len(values)%2 == 1 {
		return values[mid]
	}
	return (values[mid-1] + values[mid]) / 2
}

/*
 * resolveThroughput places one GPU model on the reference scale, returning the
 * PER-GPU relative throughput, how good the evidence was, and a phrase naming
 * that evidence for the reason string.
 *
 * Per-GPU rather than per-offer so the multi-GPU haircut is applied in exactly
 * one place (rankOffers). Applying it here as well would double-count it for
 * any offer whose count matched its observation, which is the common case.
 *
 * The ladder, best evidence first:
 *
 *  1. This provider, this model, this GPU count, at or above the sample floor.
 *  2. The same, below the floor: blended with the class prior.
 *  3. This provider and model at a different GPU count, one confidence rung
 *     lower than the source earned.
 *  4. This model on another provider.
 *  5. The class table.
 *  6. UnknownRelative.
 */
func (r *throughputResolver) resolveThroughput(gpuKey string, gpuCount int) (float64, SpeedConfidence, string) {
	classRel, exactClass := r.in.Class.Relative(gpuKey)

	// Every observation rung needs the anchor to convert hashes/sec into
	// reference units. Without one they are unreadable numbers, not evidence.
	if r.anchored {
		if o, ok := r.byKey(observationKey(r.in.Provider, gpuKey, gpuCount)); ok {
			return r.fromObservation(o, classRel)
		}

		if o, ok := r.best(gpuCount, func(s ObservedSpeed) bool {
			return s.Provider == r.in.Provider && s.GPUKey == gpuKey && s.GPUCount != gpuCount
		}); ok {
			rel, confidence, evidence := r.fromObservation(o, classRel)
			return rel, demote(confidence),
				fmt.Sprintf("%s, rescaled from a x%d observation", evidence, o.obs.GPUCount)
		}

		if o, ok := r.best(gpuCount, func(s ObservedSpeed) bool {
			return s.GPUKey == gpuKey && s.Provider != r.in.Provider
		}); ok {
			rel, _, _ := r.fromObservation(o, classRel)
			return rel, ConfidenceCrossTier,
				fmt.Sprintf("observed on %s, %d samples", o.obs.Provider, o.obs.SampleCount)
		}
	}

	if classRel > 0 {
		if exactClass {
			return classRel, ConfidenceClassOnly, "class prior"
		}
		return classRel, ConfidenceClassOnly, "class prior for its GPU family"
	}
	return r.in.UnknownRelative, ConfidenceNone, "unknown model, pessimistic default"
}

/*
 * fromObservation converts one measured row into relative throughput, tempering
 * it with the class prior while the sample count is still thin.
 *
 * WHY BLEND AT ALL. The first observation of a model on a peer marketplace very
 * often comes from a thermally throttled box, a host running something else on
 * the same card, or a container that never got the GPU it was promised.
 * Believing a single sample outright would push that model to the bottom of the
 * cost-per-work order and keep it there — the model would never be rented
 * again, so the bad number would never be corrected. The blend makes a lone
 * sample nudge the prior instead of replacing it, and w rises to 1 as evidence
 * accumulates.
 */
func (r *throughputResolver) fromObservation(o usableObservation, classRel float64) (float64, SpeedConfidence, string) {
	rel := o.perGPU / r.anchor

	if o.obs.SampleCount >= r.in.MinSamples {
		return rel, ConfidenceObserved, fmt.Sprintf("observed, %d samples", o.obs.SampleCount)
	}
	if classRel <= 0 {
		// Nothing to temper it with. Better a thin measurement of this exact
		// card than UnknownRelative, which knows strictly less.
		return rel, ConfidenceProvisional,
			fmt.Sprintf("provisional, %d samples, no class prior to temper it", o.obs.SampleCount)
	}

	w := float64(o.obs.SampleCount) / float64(r.in.MinSamples)
	if w < 0 {
		w = 0
	}
	return w*rel + (1-w)*classRel, ConfidenceProvisional,
		fmt.Sprintf("provisional, %d of %d samples blended with the class prior",
			o.obs.SampleCount, r.in.MinSamples)
}

// byKey finds a USABLE observation by exact key. Going through the filtered
// slice rather than the raw map is what makes a stale exact-shape row fall
// through to the rungs below instead of being believed.
func (r *throughputResolver) byKey(key string) (usableObservation, bool) {
	for _, o := range r.obs {
		if o.key == key {
			return o, true
		}
	}
	return usableObservation{}, false
}

/*
 * best picks the most informative observation matching pred.
 *
 * Sample count dominates distance from the target GPU count: rescaling between
 * counts is the same arithmetic whichever row it starts from, so a well
 * measured x1 row beats a barely measured x8 one even when the target is x8.
 *
 * r.obs is sorted by key, and this replaces only on strictly better, so a
 * complete tie resolves to the lowest key — deterministic without needing the
 * comparison spelled out.
 */
func (r *throughputResolver) best(target int, pred func(ObservedSpeed) bool) (usableObservation, bool) {
	var found bool
	var best usableObservation
	for _, o := range r.obs {
		if !pred(o.obs) {
			continue
		}
		if !found || betterObservation(o, best, target) {
			best, found = o, true
		}
	}
	return best, found
}

func betterObservation(candidate, current usableObservation, target int) bool {
	if candidate.obs.SampleCount != current.obs.SampleCount {
		return candidate.obs.SampleCount > current.obs.SampleCount
	}
	cd, ud := absInt(candidate.obs.GPUCount-target), absInt(current.obs.GPUCount-target)
	if cd != ud {
		return cd < ud
	}
	return candidate.obs.GPUCount < current.obs.GPUCount
}

// demote drops one confidence rung, for an estimate derived from an observation
// of a different shape than the offer being ranked.
func demote(c SpeedConfidence) SpeedConfidence {
	if c > ConfidenceNone {
		return c - 1
	}
	return c
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
