package cloud

import (
	"regexp"
	"strings"
)

/*
 * GPU classes: relative hashcat throughput per GPU model.
 *
 * WHY THIS EXISTS
 *
 * Offers were ranked by price per HOUR, which is the wrong key for renting
 * compute. An RTX A5000 at 27c/hr is not cheaper than an RTX 5090 at 99c/hr if
 * it is four times slower — it is roughly 40% more expensive per hash. Sorting
 * on dph_total picks the A5000 every time and quietly costs more.
 *
 * Ranking on cost per unit of WORK needs a throughput estimate per model. Real
 * observations (cloud_gpu_benchmarks) are always better and always win, but a
 * fresh deployment has none, and a cold table must still produce a correct
 * ORDER. It can: ranking needs only RELATIVE speed, and the absolute anchor is
 * a constant common to every offer that cancels out of the comparison.
 *
 * WHY HASHCAT NUMBERS AND NOT SPEC SHEETS
 *
 * Hashcat is integer/ALU bound. It does not touch tensor cores, and it barely
 * cares about memory bandwidth or VRAM capacity for most fast hashes. So the
 * ranking that matters here is very different from the one a GPU vendor
 * advertises:
 *
 *   - Consumer cards dominate on price/performance. An RTX 4090 beats an A100
 *     at hashing while costing a third as much per hour.
 *   - Datacenter and AI-training cards are POOR value here. A B300 has 288GB of
 *     HBM that hashcat will never use, and prices it accordingly. This table is
 *     the reason "never rent a B300 to crack hashes" can be expressed as
 *     arithmetic rather than as a deny list an operator has to maintain.
 *
 * WHY A Go VAR AND NOT A TABLE
 *
 * It is a constant of the software, not deployment configuration. Nothing here
 * is operator-editable. Seeded rows would go stale asymmetrically — two
 * deployments migrated at different times would rank the same two offers
 * differently, with no way to tell from the outside which was right — and every
 * ranking test would need a database fixture instead of a table of cases.
 *
 * The cost, stated honestly: this goes stale between releases, and an operator
 * renting a brand-new SKU cannot correct it. Three things blunt that: the
 * observed table overrides it after a single real benchmark, an unknown model
 * falls back to its family rather than being dropped, and the deny list in
 * OfferQuery is an immediate operator lever.
 *
 * VALUES ARE APPROXIMATE, and deliberately so — they are seeds for a system
 * that measures. Relative ORDER is what must be right; a 20% error in a
 * magnitude is corrected by the first real benchmark on that model.
 */

// referenceGPU is the model every Relative value is expressed against. Chosen
// because it is the most widely available card across all three providers, so
// the anchor is one almost every deployment will actually observe and correct.
const referenceGPU = "rtx_4090"

// GPUClass is a normalised GPU identity and its relative hashcat throughput.
type GPUClass struct {
	// Key is the canonical normalised model, e.g. "rtx_4090".
	Key string
	// Family groups architectures so an unrecognised model can still be
	// estimated from its generation rather than dropped.
	Family string
	// VRAMGB is nominal per-GPU memory, used by the VRAM floor/ceiling filter
	// for providers that do not report it themselves.
	VRAMGB int
	// Relative is throughput against referenceGPU on a fast unsalted hash.
	Relative float64
}

// GPUClassTable maps normalised keys to classes.
type GPUClassTable map[string]GPUClass

/*
 * familyRelative is the fallback when a model is unknown but its architecture
 * is recognisable from the name (e.g. a new "RTX 5070 Ti" nobody has listed
 * yet).
 *
 * These are the LOW END OF EACH FAMILY, and the distinction matters more than
 * it looks. The obvious mistake is to pick something near the family's typical
 * value — but the models NOT in DefaultGPUClasses are, almost by construction,
 * the cheap ones: the halo parts get listed first. So the right anchor is the
 * weakest plausible member, not the average one.
 *
 * Getting this wrong is not symmetric. Cost per work is rate/throughput, so an
 * OPTIMISTIC guess makes an unknown card look cheap and WIN, and the system
 * then systematically rents exactly the hardware it understands least. An
 * earlier revision of this table used near-typical values, and an unlisted
 * "RTX 4060 Ti" at 12c/hr scored 24.0 against a real 4090 at 34c/hr — winning,
 * while actually costing about 60% more per hash.
 *
 * A pessimistic guess only costs a missed bargain, and the first real
 * benchmark on that model corrects it permanently.
 */
var familyRelative = map[string]float64{
	"blackwell": 0.30, // unlisted = 5060/5070 class, not a 5090
	"hopper":    0.50,
	"ada":       0.15, // unlisted = 4060/4070 class, not a 4090
	"ampere":    0.12,
	"turing":    0.08,
}

/*
 * DefaultGPUClasses is the cold-start table.
 *
 * Relative values are anchored on RTX 4090 = 1.00 using published hashcat MD5
 * (-m 0) figures. MD5 is the right anchor for a general-purpose seed: it is the
 * most widely benchmarked mode and it is purely ALU bound, so it reflects the
 * property that actually differentiates these cards for cracking work. Slow and
 * salted modes compress the spread considerably, which is precisely what the
 * per-(attack_mode, hash_type, salt_count) observations exist to correct.
 */
var DefaultGPUClasses = GPUClassTable{
	// Blackwell — current consumer/pro generation, the best value for hashing.
	"rtx_5090":     {Key: "rtx_5090", Family: "blackwell", VRAMGB: 32, Relative: 1.60},
	"rtx_5080":     {Key: "rtx_5080", Family: "blackwell", VRAMGB: 16, Relative: 1.05},
	"rtx_pro_6000": {Key: "rtx_pro_6000", Family: "blackwell", VRAMGB: 96, Relative: 1.70},
	// B200/B300 are AI-training parts. Fast in absolute terms, and priced far
	// beyond what that speed is worth for hashcat — they exist here so the
	// ranker can reject them on arithmetic rather than on a hardcoded rule.
	"b200": {Key: "b200", Family: "blackwell", VRAMGB: 180, Relative: 1.50},
	"b300": {Key: "b300", Family: "blackwell", VRAMGB: 288, Relative: 1.70},

	// Ada Lovelace.
	"rtx_4090":     {Key: "rtx_4090", Family: "ada", VRAMGB: 24, Relative: 1.00},
	"rtx_4080":     {Key: "rtx_4080", Family: "ada", VRAMGB: 16, Relative: 0.72},
	"rtx_6000_ada": {Key: "rtx_6000_ada", Family: "ada", VRAMGB: 48, Relative: 0.85},
	"rtx_5000_ada": {Key: "rtx_5000_ada", Family: "ada", VRAMGB: 32, Relative: 0.60},
	"rtx_4000_ada": {Key: "rtx_4000_ada", Family: "ada", VRAMGB: 20, Relative: 0.32},
	"rtx_2000_ada": {Key: "rtx_2000_ada", Family: "ada", VRAMGB: 16, Relative: 0.16},
	"l4":           {Key: "l4", Family: "ada", VRAMGB: 24, Relative: 0.18},
	"l40":          {Key: "l40", Family: "ada", VRAMGB: 48, Relative: 0.55},
	"l40s":         {Key: "l40s", Family: "ada", VRAMGB: 48, Relative: 0.62},
	"rtx_4000_sff": {Key: "rtx_4000_sff", Family: "ada", VRAMGB: 20, Relative: 0.28},

	// Hopper — HBM and tensor cores hashcat cannot use, at HBM prices.
	"h100_sxm":  {Key: "h100_sxm", Family: "hopper", VRAMGB: 80, Relative: 0.85},
	"h100_pcie": {Key: "h100_pcie", Family: "hopper", VRAMGB: 80, Relative: 0.73},
	"h100_nvl":  {Key: "h100_nvl", Family: "hopper", VRAMGB: 94, Relative: 0.80},
	"h200":      {Key: "h200", Family: "hopper", VRAMGB: 141, Relative: 0.90},

	// Ampere.
	"rtx_3090":  {Key: "rtx_3090", Family: "ampere", VRAMGB: 24, Relative: 0.40},
	"rtx_3080":  {Key: "rtx_3080", Family: "ampere", VRAMGB: 10, Relative: 0.30},
	"rtx_a6000": {Key: "rtx_a6000", Family: "ampere", VRAMGB: 48, Relative: 0.33},
	"rtx_a5000": {Key: "rtx_a5000", Family: "ampere", VRAMGB: 24, Relative: 0.26},
	"rtx_a4500": {Key: "rtx_a4500", Family: "ampere", VRAMGB: 20, Relative: 0.22},
	"rtx_a4000": {Key: "rtx_a4000", Family: "ampere", VRAMGB: 16, Relative: 0.16},
	"a100_sxm":  {Key: "a100_sxm", Family: "ampere", VRAMGB: 80, Relative: 0.48},
	"a100_pcie": {Key: "a100_pcie", Family: "ampere", VRAMGB: 80, Relative: 0.42},
	"a40":       {Key: "a40", Family: "ampere", VRAMGB: 48, Relative: 0.30},
	"a10":       {Key: "a10", Family: "ampere", VRAMGB: 24, Relative: 0.20},
	"a10g":      {Key: "a10g", Family: "ampere", VRAMGB: 24, Relative: 0.20},
	"a16":       {Key: "a16", Family: "ampere", VRAMGB: 64, Relative: 0.12},

	// Turing — still common and cheap on peer marketplaces.
	"rtx_2080_ti":     {Key: "rtx_2080_ti", Family: "turing", VRAMGB: 11, Relative: 0.18},
	"rtx_titan":       {Key: "rtx_titan", Family: "turing", VRAMGB: 24, Relative: 0.19},
	"tesla_t4":        {Key: "tesla_t4", Family: "turing", VRAMGB: 16, Relative: 0.09},
	"quadro_rtx_6000": {Key: "quadro_rtx_6000", Family: "turing", VRAMGB: 24, Relative: 0.17},
}

// Relative returns the throughput estimate for a normalised key, falling back
// through the family before giving up. The bool reports whether the model
// itself was recognised, which the ranker turns into a confidence level.
func (t GPUClassTable) Relative(key string) (float64, bool) {
	if c, ok := t[key]; ok {
		return c.Relative, true
	}
	if rel, ok := familyRelative[familyOf(key)]; ok {
		return rel, false
	}
	return 0, false
}

// VRAMGB returns nominal per-GPU memory for a normalised key, or 0 when
// unknown. Zero means UNKNOWN and must never be filtered as "no VRAM".
func (t GPUClassTable) VRAMGB(key string) int {
	if c, ok := t[key]; ok {
		return c.VRAMGB
	}
	return 0
}

/*
 * familyOf guesses an architecture from a normalised key.
 *
 * Ordered most-specific first: "rtx_5000_ada" must match Ada on its suffix
 * before the leading "rtx_5" is mistaken for a Blackwell 50-series part. Naming
 * across vendors and generations is genuinely ambiguous, so this is a
 * best-effort fallback whose only job is to beat "unknown", not to be exact.
 */
func familyOf(key string) string {
	switch {
	case strings.HasSuffix(key, "_ada"):
		return "ada"
	case strings.HasPrefix(key, "b2"), strings.HasPrefix(key, "b3"),
		strings.HasPrefix(key, "rtx_50"), strings.HasPrefix(key, "rtx_pro_"):
		return "blackwell"
	// Spelled out rather than "h1"/"h2": AWS instance families collide with
	// those prefixes, and h1.16xlarge is a STORAGE instance with no GPU at all.
	// Calling it Hopper 0.50 would have the ranker believe a disk box is a
	// competitive cracking machine.
	case strings.HasPrefix(key, "h100"), strings.HasPrefix(key, "h200"):
		return "hopper"
	case strings.HasPrefix(key, "rtx_40"), strings.HasPrefix(key, "l4"):
		return "ada"
	case strings.HasPrefix(key, "rtx_30"), strings.HasPrefix(key, "rtx_a"),
		strings.HasPrefix(key, "a100"), strings.HasPrefix(key, "a4"),
		strings.HasPrefix(key, "a1"):
		return "ampere"
	case strings.HasPrefix(key, "rtx_20"), strings.HasPrefix(key, "tesla_t"),
		strings.HasPrefix(key, "quadro_"):
		return "turing"
	}
	return ""
}

// nonAlnum collapses every run of non-alphanumeric characters to one separator.
var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

/*
 * gpuNameNoise is vendor and marketing text that carries no model information.
 *
 * Providers spell the same card very differently: RunPod returns
 * "NVIDIA GeForce RTX 4090", Vast.ai returns "RTX 4090", and an operator
 * configuring AWS might write "g5.xlarge (A10G)". All three must normalise to
 * the same key or the observed table can never be read back by the code that
 * wrote it.
 */
var gpuNameNoise = []string{
	"nvidia_", "geforce_", "tesla_", "_oem",
	"_80gb", "_48gb", "_40gb", "_32gb", "_24gb", "_20gb", "_16gb", "_12gb",
	"_11gb", "_10gb", "_8gb", "_gb",
}

/*
 * sxmGeneration canonicalises SXM4/SXM5/SXM2 to a bare "sxm".
 *
 * Run BEFORE gpuNameNoise, and sxm is deliberately absent from that list.
 *
 * An earlier revision stripped "_sxm4"/"_sxm5" as noise, which quietly defeated
 * the whole SXM/PCIe distinction: "A100 SXM4 80GB" lost its sxm marker before
 * the model rules ran and fell through to the ^a100 rule, landing on
 * a100_pcie. Three things broke at once. The same physical card got two
 * different observation keys depending on which provider spelled it with a
 * generation number, so measurements never accumulated. SXM speeds were folded
 * into the PCIe row's EWMA. And SXM offers were priced with the PCIe prior — a
 * systematic underestimate on the most expensive cards in the table.
 *
 * The generation digit itself carries no information we rank on (an SXM4 and an
 * SXM5 A100 are the same silicon in different sockets), so collapsing the digit
 * while KEEPING the sxm marker is exactly the right amount of normalisation.
 */
var sxmGeneration = regexp.MustCompile(`sxm[0-9]+`)

/*
 * NormalizeGPUModel maps a provider's model string onto a canonical key.
 *
 * MUST be used on both the read and the write path. The observed-speed table is
 * keyed on this output, so a write that normalises differently from a read
 * produces a table that silently never matches — the failure mode being that
 * cost-per-work ranking quietly degrades to the static prior forever, with
 * nothing in the logs to say so.
 *
 * Unrecognised input is returned as a cleaned-up slug rather than rejected. An
 * unknown key still ranks (via familyOf, then UnknownRelative) and still
 * accumulates observations under a stable name, so the system learns about
 * hardware this table has never heard of.
 */
func NormalizeGPUModel(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	s = nonAlnum.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")

	// Collapse the SXM generation digit but KEEP the sxm marker, so the
	// SXM/PCIe split survives the noise pass below. See sxmGeneration.
	s = sxmGeneration.ReplaceAllString(s, "sxm")

	// Vendor prefixes and capacity suffixes, so "nvidia_geforce_rtx_4090"
	// and "rtx_4090_24gb" converge before any model rule runs.
	for _, noise := range gpuNameNoise {
		s = strings.ReplaceAll(s, noise, "_")
	}
	s = strings.Trim(nonAlnum.ReplaceAllString(s, "_"), "_")

	for _, r := range gpuModelRules {
		if r.match.MatchString(s) {
			return r.key
		}
	}
	return s
}

// gpuModelRules maps the spellings providers actually emit onto canonical keys.
// First match wins, so more specific patterns are listed first.
var gpuModelRules = []struct {
	match *regexp.Regexp
	key   string
}{
	// Datacenter, most specific first — the SXM/PCIe split is real.
	{regexp.MustCompile(`^h100.*sxm`), "h100_sxm"},
	{regexp.MustCompile(`^h100.*nvl`), "h100_nvl"},
	{regexp.MustCompile(`^h100`), "h100_pcie"},
	{regexp.MustCompile(`^h200`), "h200"},
	{regexp.MustCompile(`^b200`), "b200"},
	{regexp.MustCompile(`^b300`), "b300"},
	{regexp.MustCompile(`^a100.*sxm`), "a100_sxm"},
	{regexp.MustCompile(`^a100`), "a100_pcie"},

	// Ada workstation cards carry the generation as a SUFFIX, so these must
	// precede the plain rtx_N000 rules or "rtx_4000_ada" collapses onto a
	// nonexistent Ampere "rtx_4000".
	{regexp.MustCompile(`^rtx_?6000_?ada|^rtx_?ada_?6000`), "rtx_6000_ada"},
	{regexp.MustCompile(`^rtx_?5000_?ada|^rtx_?ada_?5000`), "rtx_5000_ada"},
	{regexp.MustCompile(`^rtx_?4000_?ada|^rtx_?ada_?4000`), "rtx_4000_ada"},
	{regexp.MustCompile(`^rtx_?2000_?ada|^rtx_?ada_?2000`), "rtx_2000_ada"},
	{regexp.MustCompile(`^rtx_?pro_?6000|^rtx_?6000_?blackwell`), "rtx_pro_6000"},

	// Consumer.
	{regexp.MustCompile(`^rtx_?5090`), "rtx_5090"},
	{regexp.MustCompile(`^rtx_?5080`), "rtx_5080"},
	{regexp.MustCompile(`^rtx_?4090`), "rtx_4090"},
	{regexp.MustCompile(`^rtx_?4080`), "rtx_4080"},
	{regexp.MustCompile(`^rtx_?3090`), "rtx_3090"},
	{regexp.MustCompile(`^rtx_?3080`), "rtx_3080"},
	{regexp.MustCompile(`^rtx_?2080_?ti`), "rtx_2080_ti"},

	// Ampere workstation.
	{regexp.MustCompile(`^rtx_?a6000`), "rtx_a6000"},
	{regexp.MustCompile(`^rtx_?a5000`), "rtx_a5000"},
	{regexp.MustCompile(`^rtx_?a4500`), "rtx_a4500"},
	{regexp.MustCompile(`^rtx_?a4000`), "rtx_a4000"},

	// Inference and virtualisation parts.
	{regexp.MustCompile(`^l40s`), "l40s"},
	{regexp.MustCompile(`^l40`), "l40"},
	{regexp.MustCompile(`^l4$|^l4_`), "l4"},
	{regexp.MustCompile(`^a40`), "a40"},
	{regexp.MustCompile(`^a10g`), "a10g"},
	{regexp.MustCompile(`^a10$|^a10_`), "a10"},
	{regexp.MustCompile(`^a16`), "a16"},
	{regexp.MustCompile(`^t4$|^t4_`), "tesla_t4"},
}
