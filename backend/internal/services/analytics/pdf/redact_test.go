package pdf

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
)

func strptr(s string) *string { return &s }

// sensitiveSentinels are unique strings seeded into every sensitive field. None
// may appear anywhere in the marshaled external output.
var sensitiveSentinels = []string{
	"SENTINEL_TOP_PASSWORD",
	"SENTINEL_REUSE_PASSWORD",
	"SENTINEL_REUSE_USERNAME",
	"SENTINEL_HASH_VALUE",
	"SENTINEL_HASH_PASSWORD",
	"SENTINEL_HASH_USERNAME",
	"SENTINEL_LM_USERNAME",
	"SENTINEL_LM_FIRSTHALF",
	"SENTINEL_LM_SECONDHALF",
	"SENTINEL_MASK_EXAMPLE",
	"SENTINEL_LMNTLM_EXAMPLE",
	"SENTINEL_BASE_WORD",
	// domain-scoped copies
	"SENTINEL_DOM_TOP_PASSWORD",
	"SENTINEL_DOM_REUSE_PASSWORD",
	"SENTINEL_DOM_REUSE_USERNAME",
	"SENTINEL_DOM_HASH_VALUE",
	"SENTINEL_DOM_HASH_USERNAME",
	"SENTINEL_DOM_MASK_EXAMPLE",
	"SENTINEL_DOM_BASE_WORD",
}

func sensitiveFixture() *models.AnalyticsData {
	mk := func(prefix string) struct {
		reuse    models.ReuseStats
		hashReu  models.HashReuseStats
		masks    models.MaskStats
		lmPart   *models.LMPartialCrackStats
		lmToNTLM *models.LMToNTLMMaskStats
		top      []models.TopPassword
	} {
		return struct {
			reuse    models.ReuseStats
			hashReu  models.HashReuseStats
			masks    models.MaskStats
			lmPart   *models.LMPartialCrackStats
			lmToNTLM *models.LMToNTLMMaskStats
			top      []models.TopPassword
		}{
			reuse: models.ReuseStats{
				TotalReused:      42,
				PercentageReused: 12.5,
				TotalUnique:      300,
				PasswordReuseInfo: []models.PasswordReuseInfo{{
					Password: prefix + "REUSE_PASSWORD",
					Users:    []models.UserOccurrence{{Username: prefix + "REUSE_USERNAME", HashlistCount: 2}},
				}},
			},
			hashReu: models.HashReuseStats{
				TotalReused:      7,
				PercentageReused: 3.0,
				TotalUnique:      90,
				HashReuseInfo: []models.HashReuseInfo{{
					HashValue: prefix + "HASH_VALUE",
					Password:  strptr(prefix + "HASH_PASSWORD"),
					Users:     []models.UserOccurrence{{Username: prefix + "HASH_USERNAME"}},
				}},
			},
			masks: models.MaskStats{TopMasks: []models.MaskInfo{{
				Mask: "?l?l?l?d?d", Count: 11, Percentage: 4.0, Example: prefix + "MASK_EXAMPLE",
			}}},
			lmPart: &models.LMPartialCrackStats{
				TotalPartial: 4,
				PartialCrackDetails: []models.LMPartialCrackDetail{{
					Username:      strptr(prefix + "LM_USERNAME"),
					FirstHalfPwd:  strptr(prefix + "LM_FIRSTHALF"),
					SecondHalfPwd: strptr(prefix + "LM_SECONDHALF"),
				}},
			},
			lmToNTLM: &models.LMToNTLMMaskStats{
				TotalLMCracked: 4,
				Masks:          []models.LMNTLMMaskInfo{{Mask: "?u?l?l", LMPattern: "AAA", Count: 2, ExampleLM: prefix + "LMNTLM_EXAMPLE"}},
			},
			top: []models.TopPassword{{Password: prefix + "TOP_PASSWORD", Count: 9, Percentage: 2.0}},
		}
	}

	top := mk("SENTINEL_")
	dom := mk("SENTINEL_DOM_")

	data := &models.AnalyticsData{
		Overview: models.OverviewStats{
			TotalHashes:     1000,
			TotalCracked:    400,
			CrackPercentage: 40,
			DomainBreakdown: []models.DomainStats{{Domain: "corp.local", TotalHashes: 500, CrackedHashes: 200, CrackPercentage: 40}},
		},
		PatternDetection: models.PatternStats{
			CommonBaseWords: map[string]models.CategoryCount{"SENTINEL_BASE_WORD": {Count: 30, Percentage: 7.5}},
		},
		PasswordReuse:   top.reuse,
		HashReuse:       top.hashReu,
		MaskAnalysis:    top.masks,
		LMPartialCracks: top.lmPart,
		LMToNTLMMasks:   top.lmToNTLM,
		TopPasswords:    top.top,
		DomainAnalytics: []models.DomainAnalytics{{
			Domain:          "corp.local",
			PasswordReuse:   dom.reuse,
			HashReuse:       &dom.hashReu,
			MaskAnalysis:    dom.masks,
			LMPartialCracks: dom.lmPart,
			LMToNTLMMasks:   dom.lmToNTLM,
			TopPasswords:    dom.top,
		}},
	}
	// Seed a per-domain base word so redaction of the domain section is covered too.
	data.DomainAnalytics[0].PatternDetection.CommonBaseWords = map[string]models.CategoryCount{
		"SENTINEL_DOM_BASE_WORD": {Count: 5, Percentage: 1.0},
	}
	return data
}

func TestBuildExternalAnalytics_NoSensitiveLeak(t *testing.T) {
	in := sensitiveFixture()
	ext := BuildExternalAnalytics(in)
	if ext == nil {
		t.Fatal("BuildExternalAnalytics returned nil")
	}

	b, err := json.Marshal(ext)
	if err != nil {
		t.Fatalf("marshal external: %v", err)
	}
	out := string(b)

	for _, s := range sensitiveSentinels {
		if strings.Contains(out, s) {
			t.Errorf("external output leaked sensitive value %q", s)
		}
	}
}

func TestBuildExternalAnalytics_PreservesAggregates(t *testing.T) {
	ext := BuildExternalAnalytics(sensitiveFixture())

	if ext.PasswordReuse.TotalReused != 42 {
		t.Errorf("PasswordReuse.TotalReused = %d, want 42 (aggregate must survive)", ext.PasswordReuse.TotalReused)
	}
	if ext.PasswordReuse.PasswordReuseInfo != nil {
		t.Error("PasswordReuse.PasswordReuseInfo must be nil in external output")
	}
	if ext.HashReuse.TotalReused != 7 {
		t.Errorf("HashReuse.TotalReused = %d, want 7", ext.HashReuse.TotalReused)
	}
	if ext.HashReuse.HashReuseInfo != nil {
		t.Error("HashReuse.HashReuseInfo must be nil in external output")
	}
	if ext.TopPasswords != nil {
		t.Error("TopPasswords must be nil in external output")
	}
	if len(ext.MaskAnalysis.TopMasks) != 1 || ext.MaskAnalysis.TopMasks[0].Mask != "?l?l?l?d?d" {
		t.Error("mask pattern must be preserved")
	}
	if ext.MaskAnalysis.TopMasks[0].Example != "" {
		t.Error("mask example must be cleared")
	}
	if len(ext.PatternDetection.CommonBaseWords) != 0 {
		t.Error("common base words must be stripped in external output (a base word can be a recovered password)")
	}
	if len(ext.Overview.DomainBreakdown) != 1 {
		t.Error("domain breakdown (aggregate) must be preserved")
	}
	// Per-domain section must be redacted too.
	if len(ext.DomainAnalytics) != 1 {
		t.Fatal("domain analytics dropped")
	}
	d := ext.DomainAnalytics[0]
	if d.TopPasswords != nil || d.PasswordReuse.PasswordReuseInfo != nil {
		t.Error("per-domain sensitive detail must be redacted")
	}
	if d.HashReuse != nil && d.HashReuse.HashReuseInfo != nil {
		t.Error("per-domain hash reuse detail must be redacted")
	}
	if len(d.PatternDetection.CommonBaseWords) != 0 {
		t.Error("per-domain common base words must be stripped")
	}
}

func TestBuildExternalAnalytics_DoesNotMutateInput(t *testing.T) {
	in := sensitiveFixture()
	_ = BuildExternalAnalytics(in)

	if in.TopPasswords == nil {
		t.Error("input TopPasswords was mutated (deep copy failed)")
	}
	if in.PasswordReuse.PasswordReuseInfo == nil {
		t.Error("input PasswordReuse detail was mutated")
	}
	if in.DomainAnalytics[0].TopPasswords == nil {
		t.Error("input domain TopPasswords was mutated")
	}
}

func TestBuildExternalAnalytics_NilInput(t *testing.T) {
	if BuildExternalAnalytics(nil) != nil {
		t.Error("nil input should return nil")
	}
}
