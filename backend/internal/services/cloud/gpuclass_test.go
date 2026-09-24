package cloud

import "testing"

/*
 * Every test here is a regression guard for a defect that was actually present
 * in this file, found by review before it ever ranked a real offer. They are
 * written as the failure they prevent rather than as the behaviour they assert,
 * because in each case the wrong answer is silent and shows up on an invoice.
 */

/*
 * TestNormalizeGPUModel_ConvergesAcrossProviders.
 *
 * The observed-speed table is keyed on this output. A write that normalises
 * differently from a read produces a table that never matches, and the symptom
 * is not an error — it is cost-per-work ranking quietly degrading to the static
 * prior forever, with nothing in the logs.
 */
func TestNormalizeGPUModel_ConvergesAcrossProviders(t *testing.T) {
	groups := map[string][]string{
		// RunPod returns the long marketing string; Vast returns the short one.
		"rtx_4090":  {"NVIDIA GeForce RTX 4090", "RTX 4090", "rtx 4090", "GeForce RTX 4090 24GB"},
		"rtx_5090":  {"NVIDIA GeForce RTX 5090", "RTX 5090"},
		"rtx_a5000": {"NVIDIA RTX A5000", "RTX A5000"},
		"l40s":      {"NVIDIA L40S", "L40S"},
		"a10g":      {"NVIDIA A10G", "A10G"},
	}
	for want, spellings := range groups {
		for _, raw := range spellings {
			if got := NormalizeGPUModel(raw); got != want {
				t.Errorf("NormalizeGPUModel(%q) = %q, want %q — providers spelling the "+
					"same card differently must converge or observations never accumulate",
					raw, got, want)
			}
		}
	}
}

/*
 * TestNormalizeGPUModel_KeepsTheSXMDistinction.
 *
 * "_sxm4"/"_sxm5" used to be stripped as vendor noise BEFORE the model rules
 * ran, so "A100 SXM4 80GB" lost its marker and fell through to the ^a100 rule,
 * landing on a100_pcie. Three things broke at once: the same card got two keys
 * depending on the provider's spelling, SXM measurements were folded into the
 * PCIe row's average, and SXM offers were priced with the PCIe prior — a
 * systematic underestimate on the most expensive cards in the table.
 */
func TestNormalizeGPUModel_KeepsTheSXMDistinction(t *testing.T) {
	cases := map[string]string{
		"A100 SXM4 80GB":       "a100_sxm",
		"A100-SXM4-80GB":       "a100_sxm",
		"NVIDIA A100 SXM 80GB": "a100_sxm",
		"A100 SXM5":            "a100_sxm",
		"A100 PCIe 80GB":       "a100_pcie",
		"NVIDIA A100 80GB":     "a100_pcie",
		"H100 SXM5 80GB":       "h100_sxm",
		"H100 PCIe 80GB":       "h100_pcie",
		"H100 NVL":             "h100_nvl",
	}
	for raw, want := range cases {
		if got := NormalizeGPUModel(raw); got != want {
			t.Errorf("NormalizeGPUModel(%q) = %q, want %q", raw, got, want)
		}
	}
}

/*
 * TestNormalizeGPUModel_AdaSuffixBeatsGenerationPrefix.
 *
 * "rtx_4000_ada" must not collapse onto a nonexistent Ampere "rtx_4000", and
 * "rtx_5000_ada" must not be read as a Blackwell 50-series part. The workstation
 * cards carry their generation as a SUFFIX, which is the opposite convention to
 * the consumer line.
 */
func TestNormalizeGPUModel_AdaSuffixBeatsGenerationPrefix(t *testing.T) {
	cases := map[string]string{
		"RTX 4000 Ada Generation": "rtx_4000_ada",
		"RTX 5000 Ada":            "rtx_5000_ada",
		"RTX 6000 Ada":            "rtx_6000_ada",
		"RTX 2000 Ada":            "rtx_2000_ada",
		"RTX PRO 6000":            "rtx_pro_6000",
		"RTX 4090":                "rtx_4090",
		"RTX 5090":                "rtx_5090",
	}
	for raw, want := range cases {
		if got := NormalizeGPUModel(raw); got != want {
			t.Errorf("NormalizeGPUModel(%q) = %q, want %q", raw, got, want)
		}
	}
}

/*
 * TestFamilyFallbacksArePessimistic is the money test in this file.
 *
 * Cost per work is rate/throughput, so an OPTIMISTIC guess about unknown
 * hardware makes it look cheap and WIN — the system then systematically rents
 * exactly the cards it understands least. An earlier revision used
 * near-typical family values, and an unlisted "RTX 4060 Ti" beat a real 4090
 * while actually costing about 60% more per hash.
 *
 * The rule: a family fallback must never exceed the WEAKEST listed member of
 * that family. The unlisted models are the cheap ones — halo parts get listed
 * first — so the weakest listed member is already generous.
 */
func TestFamilyFallbacksArePessimistic(t *testing.T) {
	weakest := map[string]float64{}
	for _, c := range DefaultGPUClasses {
		if cur, ok := weakest[c.Family]; !ok || c.Relative < cur {
			weakest[c.Family] = c.Relative
		}
	}

	for family, fallback := range familyRelative {
		low, ok := weakest[family]
		if !ok {
			continue
		}
		if fallback > low {
			t.Errorf("familyRelative[%q] = %.2f exceeds the weakest listed member of that "+
				"family (%.2f). An unlisted card would be rated better than the worst card "+
				"we actually know about, and cost-per-work ranking would prefer it.",
				family, fallback, low)
		}
	}
}

/*
 * TestFamilyOf_DoesNotClaimNonGPUInstances.
 *
 * familyOf matched a bare "h1" prefix, which made the AWS h1.16xlarge — a
 * STORAGE instance with no GPU at all — look like Hopper-class hardware. The
 * ranker would then rate a disk box as a competitive cracking machine.
 */
func TestFamilyOf_DoesNotClaimNonGPUInstances(t *testing.T) {
	notGPUs := []string{
		"h1_16xlarge", "h1_8xlarge", // storage-optimised, no GPU
		"c5_large", "m5_xlarge", "r5_2xlarge",
	}
	for _, key := range notGPUs {
		if fam := familyOf(key); fam != "" {
			t.Errorf("familyOf(%q) = %q; a non-GPU instance type must not be assigned a "+
				"GPU family, or the ranker credits it with throughput it does not have",
				key, fam)
		}
	}

	// The real Hopper parts must still resolve.
	for _, key := range []string{"h100_sxm", "h100_pcie", "h200"} {
		if fam := familyOf(key); fam != "hopper" {
			t.Errorf("familyOf(%q) = %q, want hopper", key, fam)
		}
	}
}

// TestReferenceGPUIsInTheTable: every Relative is expressed against
// referenceGPU, and the ranker derives its anchor from it. A typo in the
// constant would leave the anchor permanently underived.
func TestReferenceGPUIsInTheTable(t *testing.T) {
	c, ok := DefaultGPUClasses[referenceGPU]
	if !ok {
		t.Fatalf("referenceGPU %q is not in DefaultGPUClasses", referenceGPU)
	}
	if c.Relative != 1.0 {
		t.Errorf("referenceGPU %q has Relative %.2f, want exactly 1.00 — every other "+
			"value in the table is expressed against it", referenceGPU, c.Relative)
	}
}

// TestDatacentreCardsAreNotRatedAboveConsumer encodes the insight the whole
// table exists for: hashcat is integer/ALU bound, so it never touches the
// tensor cores or HBM that datacentre cards charge for.
func TestDatacentreCardsAreNotRatedAboveConsumer(t *testing.T) {
	ref := DefaultGPUClasses[referenceGPU].Relative
	for _, key := range []string{"a100_pcie", "a100_sxm", "h100_pcie", "l4", "a40"} {
		c, ok := DefaultGPUClasses[key]
		if !ok {
			t.Fatalf("%s missing from the table", key)
		}
		if c.Relative >= ref {
			t.Errorf("%s is rated %.2f, at or above the RTX 4090's %.2f. If that is now "+
				"true, the table needs new measurements; if not, cost-per-work ranking "+
				"will prefer an expensive AI card over a cheaper faster one.",
				key, c.Relative, ref)
		}
	}
}
