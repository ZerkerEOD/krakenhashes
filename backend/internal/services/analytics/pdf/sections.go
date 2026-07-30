package pdf

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
)

// renderAll renders the full report body for the top-level analytics data.
func (r *renderer) renderAll(report *models.AnalyticsReport, data *models.AnalyticsData) {
	r.sectionTitle("Executive Summary")
	r.paragraph(fmt.Sprintf(
		"This report analyzes %d password hashes collected for this engagement, of which %d (%.2f%%) "+
			"were successfully recovered. The sections below quantify password length, complexity, reuse, "+
			"predictable patterns, and other risk indicators, and conclude with prioritized remediation "+
			"recommendations.",
		data.Overview.TotalHashes, data.Overview.TotalCracked, data.Overview.CrackPercentage))
	if r.class == External {
		r.note("This is the external summary edition: aggregate statistics only. Recovered passwords, " +
			"usernames, and hash values are intentionally omitted.")
	}

	r.renderOverviewSection(data.Overview)
	r.renderLengthSection(data.LengthDistribution)
	r.renderComplexitySection(data.ComplexityAnalysis)
	r.renderPositionalSection(data.PositionalAnalysis)
	r.renderPatternSection(data.PatternDetection)
	r.renderUsernameSection(data.UsernameCorrelation)
	r.renderReuseSection(data.PasswordReuse)
	r.renderHashReuseSection(data.HashReuse)
	r.renderTemporalSection(data.TemporalPatterns)
	r.renderMaskSection(data.MaskAnalysis)
	r.renderCustomPatternSection(data.CustomPatterns)
	r.renderStrengthSection(data.StrengthMetrics)
	if r.class == Internal {
		r.renderTopPasswordsSection(data.TopPasswords)
	}
	if data.WindowsHashes != nil {
		r.renderWindowsSection(data.WindowsHashes)
	}
	if data.LMPartialCracks != nil {
		r.renderLMPartialSection(data.LMPartialCracks)
	}
	if data.LMToNTLMMasks != nil {
		r.renderLMToNTLMSection(data.LMToNTLMMasks)
	}
	r.renderBloodhoundSection(data)
	r.renderRecommendationsSection(data.Recommendations)

	// Bound the number of per-domain sections rendered. Reports created before the
	// domain-extraction fix can contain a very large number of spurious domains;
	// rendering a page each would produce an enormous document.
	domains := data.DomainAnalytics
	truncatedDomains := 0
	if len(domains) > maxDomainSections {
		truncatedDomains = len(domains) - maxDomainSections
		domains = domains[:maxDomainSections]
	}
	for _, d := range domains {
		r.renderDomainSection(d)
	}
	if truncatedDomains > 0 {
		r.pdf.AddPage()
		r.note(fmt.Sprintf("%d additional domain sections were omitted from this document to keep its "+
			"size manageable. See the in-app report for the full per-domain breakdown.", truncatedDomains))
	}
}

const (
	maxDomainSections = 50
	maxDomainRows     = 50
)

// renderDomainSection renders the per-domain analytics on its own page.
func (r *renderer) renderDomainSection(d models.DomainAnalytics) {
	r.pdf.AddPage()
	domain := d.Domain
	if domain == "" {
		domain = "(no domain)"
	}
	r.sectionTitle("Domain: " + domain)
	r.renderOverviewSection(d.Overview)
	r.renderLengthSection(d.LengthDistribution)
	r.renderComplexitySection(d.ComplexityAnalysis)
	r.renderPositionalSection(d.PositionalAnalysis)
	r.renderPatternSection(d.PatternDetection)
	r.renderUsernameSection(d.UsernameCorrelation)
	r.renderReuseSection(d.PasswordReuse)
	if d.HashReuse != nil {
		r.renderHashReuseSection(*d.HashReuse)
	}
	r.renderTemporalSection(d.TemporalPatterns)
	r.renderMaskSection(d.MaskAnalysis)
	r.renderCustomPatternSection(d.CustomPatterns)
	r.renderStrengthSection(d.StrengthMetrics)
	if r.class == Internal {
		r.renderTopPasswordsSection(d.TopPasswords)
	}
	if d.WindowsHashes != nil {
		r.renderWindowsSection(d.WindowsHashes)
	}
	if d.LMPartialCracks != nil {
		r.renderLMPartialSection(d.LMPartialCracks)
	}
	if d.LMToNTLMMasks != nil {
		r.renderLMToNTLMSection(d.LMToNTLMMasks)
	}
}

// --- individual sections ---

func (r *renderer) renderOverviewSection(ov models.OverviewStats) {
	r.sectionTitle("Overview")
	r.keyValues([][2]string{
		{"Total Hashes", intStr(ov.TotalHashes)},
		{"Total Cracked", intStr(ov.TotalCracked)},
		{"Crack Rate", pctStr(ov.CrackPercentage)},
	})

	if len(ov.HashModes) > 0 {
		r.subTitle("Hash Types")
		rows := make([][]string, 0, len(ov.HashModes))
		modes := append([]models.HashModeStats(nil), ov.HashModes...)
		sort.Slice(modes, func(i, j int) bool { return modes[i].Total > modes[j].Total })
		for _, m := range modes {
			rows = append(rows, []string{
				fmt.Sprintf("%s (%d)", m.ModeName, m.ModeID),
				intStr(m.Total), intStr(m.Cracked), pctStr(m.Percentage),
			})
		}
		r.dataTable([]string{"Mode", "Total", "Cracked", "Crack %"},
			[]float64{90, 30, 30, 30}, rows)
	}

	if len(ov.DomainBreakdown) > 0 {
		r.subTitle("Domain Breakdown")
		doms := append([]models.DomainStats(nil), ov.DomainBreakdown...)
		sort.Slice(doms, func(i, j int) bool { return doms[i].TotalHashes > doms[j].TotalHashes })
		truncated := 0
		if len(doms) > maxDomainRows {
			truncated = len(doms) - maxDomainRows
			doms = doms[:maxDomainRows]
		}
		rows := make([][]string, 0, len(doms))
		for _, d := range doms {
			rows = append(rows, []string{d.Domain, intStr(d.TotalHashes), intStr(d.CrackedHashes), pctStr(d.CrackPercentage)})
		}
		r.dataTable([]string{"Domain", "Total", "Cracked", "Crack %"},
			[]float64{90, 30, 30, 30}, rows)
		if truncated > 0 {
			r.note(fmt.Sprintf("Showing the top %d domains by hash count; %d more omitted.", maxDomainRows, truncated))
		}
	}
}

func (r *renderer) renderLengthSection(l models.LengthStats) {
	r.sectionTitle("Password Length")
	r.keyValues([][2]string{
		{"Average Length", fmt.Sprintf("%.1f", l.AverageLength)},
		{"Average Length (<15 chars)", fmt.Sprintf("%.1f", l.AverageLengthUnder15)},
		{"Under 8 characters", intStr(l.CountUnder8)},
		{"8 to 11 characters", intStr(l.Count8to11)},
		{"Under 15 characters", intStr(l.CountUnder15)},
	})
	if len(l.Distribution) > 0 {
		r.subTitle("Length Distribution")
		r.barChart(categoriesToBars(l.Distribution, true, 12))
	}
}

func (r *renderer) renderComplexitySection(c models.ComplexityStats) {
	r.sectionTitle("Character Complexity")
	items := []barItem{}
	items = append(items, sumCategoryBar("Single character type", c.SingleType))
	items = append(items, sumCategoryBar("Two character types", c.TwoTypes))
	items = append(items, sumCategoryBar("Three character types", c.ThreeTypes))
	items = append(items, barItem{"All four character types", c.FourTypes.Percentage, c.FourTypes.Count})
	items = append(items, barItem{"Complex but short (<=14)", c.ComplexShort.Percentage, c.ComplexShort.Count})
	items = append(items, barItem{"Complex and long (15+)", c.ComplexLong.Percentage, c.ComplexLong.Count})
	r.barChart(items)
}

func (r *renderer) renderPositionalSection(p models.PositionalStats) {
	r.sectionTitle("Positional Analysis")
	r.barChart([]barItem{
		{"Starts with uppercase", p.StartsUppercase.Percentage, p.StartsUppercase.Count},
		{"Ends with a number", p.EndsNumber.Percentage, p.EndsNumber.Count},
		{"Ends with a special char", p.EndsSpecial.Percentage, p.EndsSpecial.Count},
	})
}

func (r *renderer) renderPatternSection(p models.PatternStats) {
	r.sectionTitle("Pattern Detection")
	r.barChart([]barItem{
		{"Keyboard walks", p.KeyboardWalks.Percentage, p.KeyboardWalks.Count},
		{"Sequential characters", p.Sequential.Percentage, p.Sequential.Count},
		{"Repeating characters", p.RepeatingChars.Percentage, p.RepeatingChars.Count},
	})
	// Common base words are derived from cracked passwords and can be a recovered
	// password verbatim, so they are internal-only (also stripped by BuildExternalAnalytics).
	if r.class == Internal && len(p.CommonBaseWords) > 0 {
		r.subTitle("Common Base Words")
		rows := categoriesToRows(p.CommonBaseWords, 25)
		r.dataTable([]string{"Base Word", "Count", "Percentage"}, []float64{110, 35, 35}, rows)
	}
}

func (r *renderer) renderUsernameSection(u models.UsernameStats) {
	r.sectionTitle("Username Correlation")
	r.barChart([]barItem{
		{"Password equals username", u.EqualsUsername.Percentage, u.EqualsUsername.Count},
		{"Password contains username", u.ContainsUsername.Percentage, u.ContainsUsername.Count},
		{"Username plus suffix", u.UsernamePlusSuffix.Percentage, u.UsernamePlusSuffix.Count},
		{"Reversed username", u.ReversedUsername.Percentage, u.ReversedUsername.Count},
	})
}

func (r *renderer) renderReuseSection(re models.ReuseStats) {
	r.sectionTitle("Password Reuse")
	r.keyValues([][2]string{
		{"Reused (occurrences)", intStr(re.TotalReused)},
		{"Unique (occurrences)", intStr(re.TotalUnique)},
		{"Reuse Rate", pctStr(re.PercentageReused)},
	})
	if r.class == Internal && len(re.PasswordReuseInfo) > 0 {
		r.subTitle("Most Reused Passwords")
		rows := make([][]string, 0, len(re.PasswordReuseInfo))
		for _, info := range re.PasswordReuseInfo {
			rows = append(rows, []string{info.Password, intStr(info.UserCount), intStr(info.TotalOccurrences)})
		}
		r.dataTable([]string{"Password", "Users", "Occurrences"}, []float64{110, 35, 35}, rows)
	}
}

func (r *renderer) renderHashReuseSection(h models.HashReuseStats) {
	r.sectionTitle("Hash Reuse")
	r.keyValues([][2]string{
		{"Reused (occurrences)", intStr(h.TotalReused)},
		{"Unique (occurrences)", intStr(h.TotalUnique)},
		{"Reuse Rate", pctStr(h.PercentageReused)},
	})
	if r.class == Internal && len(h.HashReuseInfo) > 0 {
		r.subTitle("Most Reused Hashes")
		rows := make([][]string, 0, len(h.HashReuseInfo))
		for _, info := range h.HashReuseInfo {
			pw := ""
			if info.Password != nil {
				pw = *info.Password
			}
			rows = append(rows, []string{truncate(info.HashValue, 40), info.HashType, pw, intStr(info.UserCount)})
		}
		r.dataTable([]string{"Hash", "Type", "Password", "Users"}, []float64{70, 30, 50, 30}, rows)
	}
}

func (r *renderer) renderTemporalSection(t models.TemporalStats) {
	r.sectionTitle("Temporal Patterns")
	r.barChart([]barItem{
		{"Contains a year", t.ContainsYear.Percentage, t.ContainsYear.Count},
		{"Contains a month", t.ContainsMonth.Percentage, t.ContainsMonth.Count},
		{"Contains a season", t.ContainsSeason.Percentage, t.ContainsSeason.Count},
	})
	if len(t.YearBreakdown) > 0 {
		r.subTitle("Year Breakdown")
		rows := categoriesToRows(t.YearBreakdown, 20)
		r.dataTable([]string{"Year", "Count", "Percentage"}, []float64{110, 35, 35}, rows)
	}
}

func (r *renderer) renderMaskSection(m models.MaskStats) {
	r.sectionTitle("Mask Analysis")
	if len(m.TopMasks) == 0 {
		r.note("No mask data for this section.")
		return
	}
	headers := []string{"Mask", "Count", "Percentage"}
	widths := []float64{110, 35, 35}
	if r.class == Internal {
		headers = []string{"Mask", "Count", "Percentage", "Example"}
		widths = []float64{75, 25, 30, 50}
	}
	rows := make([][]string, 0, len(m.TopMasks))
	for _, mi := range m.TopMasks {
		if r.class == Internal {
			rows = append(rows, []string{mi.Mask, intStr(mi.Count), pctStr(mi.Percentage), mi.Example})
		} else {
			rows = append(rows, []string{mi.Mask, intStr(mi.Count), pctStr(mi.Percentage)})
		}
	}
	r.dataTable(headers, widths, rows)
}

func (r *renderer) renderCustomPatternSection(c models.CustomPatternStats) {
	if len(c.PatternsDetected) == 0 {
		return
	}
	r.sectionTitle("Custom Patterns")
	rows := categoriesToRows(c.PatternsDetected, 50)
	r.dataTable([]string{"Pattern", "Count", "Percentage"}, []float64{110, 35, 35}, rows)
}

func (r *renderer) renderStrengthSection(s models.StrengthStats) {
	r.sectionTitle("Strength Metrics")
	r.keyValues([][2]string{
		{"Baseline Speed (H/s)", intStr(int(s.AverageSpeedHPS))},
	})
	r.subTitle("Entropy Distribution")
	r.barChart([]barItem{
		{"Low (<78 bits)", s.EntropyDistribution.Low.Percentage, s.EntropyDistribution.Low.Count},
		{"Moderate (78-127 bits)", s.EntropyDistribution.Moderate.Percentage, s.EntropyDistribution.Moderate.Count},
		{"High (128+ bits)", s.EntropyDistribution.High.Percentage, s.EntropyDistribution.High.Count},
	})
	r.note("Strength is estimated from brute-force keyspace (length and character classes), " +
		"not information-theoretic entropy; dictionary and pattern-based weakness may not be fully reflected.")

	r.subTitle("Estimated Crack Time (% of passwords)")
	est := s.CrackTimeEstimates
	speeds := []struct {
		name string
		e    models.SpeedLevelEstimate
	}{
		{"50% speed", est.Speed50Percent},
		{"75% speed", est.Speed75Percent},
		{"100% speed", est.Speed100Percent},
		{"150% speed", est.Speed150Percent},
		{"200% speed", est.Speed200Percent},
	}
	rows := make([][]string, 0, len(speeds))
	for _, sp := range speeds {
		rows = append(rows, []string{
			sp.name,
			pctStr(sp.e.PercentUnder1Hour),
			pctStr(sp.e.PercentUnder1Day),
			pctStr(sp.e.PercentUnder1Week),
			pctStr(sp.e.PercentUnder1Year),
			pctStr(sp.e.PercentOver1Year),
		})
	}
	r.dataTable([]string{"Speed", "<1h", "<1d", "<1w", "<1y", ">1y"},
		[]float64{40, 28, 28, 28, 28, 28}, rows)
}

func (r *renderer) renderTopPasswordsSection(tp []models.TopPassword) {
	r.sectionTitle("Top Passwords")
	if len(tp) == 0 {
		r.note("No repeated passwords (used 2+ times) were recovered.")
		return
	}
	rows := make([][]string, 0, len(tp))
	for _, p := range tp {
		rows = append(rows, []string{p.Password, intStr(p.Count), pctStr(p.Percentage)})
	}
	r.dataTable([]string{"Password", "Count", "Percentage"}, []float64{110, 35, 35}, rows)
}

func (r *renderer) renderWindowsSection(w *models.WindowsHashStats) {
	r.sectionTitle("Windows Hashes")
	r.keyValues([][2]string{
		{"Total Windows Hashes", intStr(w.Overview.TotalWindows)},
		{"Cracked", intStr(w.Overview.CrackedWindows)},
		{"Crack Rate", pctStr(w.Overview.PercentageWindows)},
		{"Unique Users", intStr(w.Overview.UniqueUsers)},
		{"LM/NTLM Linked Pairs", intStr(w.Overview.LinkedPairs)},
	})

	r.subTitle("By Hash Type")
	// Only list hash types that were actually present (Total > 0); rows of zeros for
	// types that were never in the dataset are noise.
	rows := [][]string{}
	addWH := func(name string, s models.WindowsHashTypeStats) {
		if s.Total > 0 {
			rows = append(rows, whRow(name, s))
		}
	}
	addWH("NTLM", w.NTLM)
	addWH("LM", w.LM.WindowsHashTypeStats)
	addWH("NetNTLMv1", w.NetNTLMv1)
	addWH("NetNTLMv2", w.NetNTLMv2)
	addWH("DCC", w.DCC)
	addWH("DCC2", w.DCC2)
	if w.Kerberos.Total > 0 {
		rows = append(rows, []string{"Kerberos", intStr(w.Kerberos.Total), intStr(w.Kerberos.Cracked), pctStr(w.Kerberos.Percentage)})
	}
	r.dataTable([]string{"Type", "Total", "Cracked", "Crack %"}, []float64{90, 30, 30, 30}, rows)

	if w.LM.Total > 0 {
		r.subTitle("LM Detail")
		r.keyValues([][2]string{
			{"LM <= 7 chars", intStr(w.LM.UnderEight)},
			{"LM 8-14 chars", intStr(w.LM.EightToFourteen)},
			{"LM partially cracked", intStr(w.LM.PartiallyCracked)},
		})
	}

	lc := w.LinkedCorrelation
	if lc.TotalLinkedPairs > 0 {
		r.subTitle("LM/NTLM Linked Correlation")
		r.dataTable([]string{"Both Cracked", "Only NTLM", "Only LM", "Neither", "Both %"},
			[]float64{36, 36, 36, 36, 36},
			[][]string{{intStr(lc.BothCracked), intStr(lc.OnlyNTLMCracked), intStr(lc.OnlyLMCracked), intStr(lc.NeitherCracked), pctStr(lc.PercentageBoth)}})
	}
}

func (r *renderer) renderLMPartialSection(l *models.LMPartialCrackStats) {
	r.sectionTitle("LM Partial Cracks")
	r.keyValues([][2]string{
		{"Total Partial", intStr(l.TotalPartial)},
		{"First Half Only", intStr(l.FirstHalfOnly)},
		{"Second Half Only", intStr(l.SecondHalfOnly)},
		{"Partial Rate (of LM)", pctStr(l.PercentagePartial)},
	})
	if r.class == Internal && len(l.PartialCrackDetails) > 0 {
		r.subTitle("Partial Crack Detail")
		rows := make([][]string, 0, len(l.PartialCrackDetails))
		for _, d := range l.PartialCrackDetails {
			rows = append(rows, []string{
				deref(d.Username), deref(d.Domain),
				halfStr(d.FirstHalfCracked, d.FirstHalfPwd),
				halfStr(d.SecondHalfCracked, d.SecondHalfPwd),
			})
		}
		r.dataTable([]string{"Username", "Domain", "First Half", "Second Half"},
			[]float64{50, 40, 45, 45}, rows)
	}
}

func (r *renderer) renderLMToNTLMSection(l *models.LMToNTLMMaskStats) {
	r.sectionTitle("LM-to-NTLM Masks")
	r.keyValues([][2]string{
		{"LM Cracked", intStr(l.TotalLMCracked)},
		{"Masks Generated", intStr(l.TotalMasksGenerated)},
		{"Total Estimated Keyspace", intStr(int(l.TotalEstimatedKeyspace))},
	})
	if len(l.Masks) == 0 {
		return
	}
	r.subTitle("Generated Masks")
	headers := []string{"Mask", "LM Pattern", "Count", "Match %"}
	widths := []float64{70, 40, 30, 40}
	rows := make([][]string, 0, len(l.Masks))
	for _, m := range l.Masks {
		rows = append(rows, []string{m.Mask, m.LMPattern, intStr(m.Count), pctStr(m.MatchPercentage)})
	}
	r.dataTable(headers, widths, rows)
}

func (r *renderer) renderRecommendationsSection(recs []models.Recommendation) {
	r.sectionTitle("Recommendations")
	if len(recs) == 0 {
		r.note("No recommendations were generated.")
		return
	}
	for _, rec := range recs {
		r.ensureSpace(14)
		// severity chip
		r.severityFill(rec.Severity)
		r.whiteText()
		r.pdf.SetFont("Helvetica", "B", 8)
		r.pdf.SetX(pageMarginLeft)
		r.pdf.CellFormat(28, 6, r.s(rec.Severity), "", 0, "C", true, 0, "")
		r.darkText()
		r.pdf.SetFont("Helvetica", "", 10)
		x := pageMarginLeft + 30
		r.pdf.SetX(x)
		r.pdf.MultiCell(contentWidth-30, 5, r.tr(rec.Message), "", "L", false)
		r.pdf.Ln(1)
	}
}

// renderBloodhoundSection renders the AD-privilege enrichment on its own page. Per-account detail
// (usernames/SIDs/groups) is Internal-only; External shows aggregate counts only (the account
// slices are already nil after redaction, so the Internal gate is defense-in-depth).
func (r *renderer) renderBloodhoundSection(data *models.AnalyticsData) {
	if data.ADPrivilege == nil && data.PathToDA == nil && data.KerberoastCracked == nil &&
		data.ASREPRoastCracked == nil && data.AdminCountCracked == nil &&
		data.LocalAdminBlast == nil && data.DCSyncCracked == nil {
		return
	}
	r.pdf.AddPage()
	// The external (client-facing) edition omits the tooling name from the heading; the internal
	// edition keeps "(BloodHound)" for the assessors' reference.
	adTitle := "Active Directory Privilege Exposure"
	if r.class == Internal {
		adTitle += " (BloodHound)"
	}
	r.sectionTitle(adTitle)
	r.paragraph("Cracked accounts cross-referenced against the uploaded BloodHound collection. This " +
		"identifies which recovered credentials confer privileged access — from effective Domain Admin " +
		"down to local-admin reach and graph attack paths to Domain Admin.")

	if a := data.ADPrivilege; a != nil {
		r.subTitle("Privileged Account Compromise")
		r.keyValues([][2]string{
			{"Cracked privileged accounts", intStr(a.CrackedPrivileged)},
			{"Cracked effective Domain Admins", intStr(a.CrackedEffectiveDA)},
			{"Cracked tier-0 accounts", intStr(a.CrackedTierZero)},
			{"Privileged accounts in scope", intStr(a.InScopePrivileged)},
			{"Privileged cracked rate", pctStr(a.PercentPrivilegedCracked)},
			{"Privileged in domain (dump)", totalOrUnknown(a.DomainPrivilegedTotal)},
		})
		if r.class == Internal {
			r.renderCompromisedTable(a.Accounts)
		}
	}
	if p := data.PathToDA; p != nil {
		r.subTitle("Path to Domain Admin")
		if p.Skipped {
			r.note("Attack-path analysis was skipped because the collection's graph exceeded the analysis limit.")
		} else {
			r.keyValues([][2]string{
				{"Cracked accounts with a path to DA", intStr(p.CrackedWithPath)},
				{"In-scope accounts with a path to DA", intStr(p.InScopeWithPath)},
				{"Shortest path (hops)", intStr(p.ShortestHops)},
			})
			if r.class == Internal && len(p.Accounts) > 0 {
				rows := make([][]string, 0, len(p.Accounts))
				for _, ac := range p.Accounts {
					rows = append(rows, []string{ac.Username, ac.Domain, intStr(ac.Hops)})
				}
				r.dataTable([]string{"Username", "Domain", "Hops"}, []float64{80, 60, 30}, rows)
			}
		}
	}
	if k := data.KerberoastCracked; k != nil && (k.Cracked > 0 || k.InScopeTotal > 0) {
		r.subTitle("Kerberoastable Cracked")
		r.keyValues([][2]string{
			{"Cracked", intStr(k.Cracked)},
			{"Cracked (privileged)", intStr(k.CrackedPrivileged)},
			{"In scope", intStr(k.InScopeTotal)},
			{"In domain (dump)", totalOrUnknown(k.DomainTotal)},
			{"Cracked rate", pctStr(k.PercentCracked)},
		})
		if r.class == Internal {
			r.renderCompromisedTable(k.Accounts)
		}
	}
	if a := data.ASREPRoastCracked; a != nil && (a.Cracked > 0 || a.InScopeTotal > 0) {
		r.subTitle("AS-REP Roastable Cracked")
		r.keyValues([][2]string{
			{"Cracked", intStr(a.Cracked)},
			{"Cracked (privileged)", intStr(a.CrackedPrivileged)},
			{"In scope", intStr(a.InScopeTotal)},
			{"In domain (dump)", totalOrUnknown(a.DomainTotal)},
			{"Cracked rate", pctStr(a.PercentCracked)},
		})
		if r.class == Internal {
			r.renderCompromisedTable(a.Accounts)
		}
	}
	if a := data.AdminCountCracked; a != nil && (a.Cracked > 0 || a.InScopeTotal > 0) {
		r.subTitle("AdminCount (AdminSDHolder) Cracked")
		r.keyValues([][2]string{
			{"Cracked", intStr(a.Cracked)},
			{"In scope", intStr(a.InScopeTotal)},
			{"In domain (dump)", totalOrUnknown(a.DomainTotal)},
			{"Cracked rate", pctStr(a.PercentCracked)},
		})
		if r.class == Internal {
			r.renderCompromisedTable(a.Accounts)
		}
	}
	if d := data.DCSyncCracked; d != nil {
		r.subTitle("DCSync-Capable Cracked")
		r.keyValues([][2]string{
			{"Cracked with DCSync rights", intStr(d.Cracked)},
			{"DCSync principals in domain (dump)", totalOrUnknown(d.DomainPrincipals)},
		})
		if r.class == Internal {
			r.renderCompromisedTable(d.Accounts)
		}
	}
	if l := data.LocalAdminBlast; l != nil && l.CrackedWithLocalAdmin > 0 {
		r.subTitle("Local-Admin Blast Radius")
		r.keyValues([][2]string{
			{"Cracked accounts with local admin", intStr(l.CrackedWithLocalAdmin)},
			{"Total admin relationships", intStr(l.TotalAdminRelationships)},
			{"Largest single-account reach", intStr(l.MaxComputersSingle)},
		})
		if r.class == Internal && len(l.TopAccounts) > 0 {
			rows := make([][]string, 0, len(l.TopAccounts))
			for _, ac := range l.TopAccounts {
				rows = append(rows, []string{ac.Username, ac.Domain, intStr(ac.ComputerCount)})
			}
			r.dataTable([]string{"Username", "Domain", "Computers"}, []float64{80, 60, 30}, rows)
		}
	}
}

// renderCompromisedTable renders an Internal-only per-account table. It is a no-op for an empty list.
// Full group names (including "@DOMAIN", which disambiguates same-named groups across domains) are
// preserved and word-wrapped onto as many lines as needed, so nothing runs off the page.
func (r *renderer) renderCompromisedTable(accts []models.CompromisedAccount) {
	if len(accts) == 0 {
		return
	}
	rows := make([][]string, 0, len(accts))
	for _, ac := range accts {
		rows = append(rows, []string{ac.Username, ac.Domain, strings.Join(ac.PrivilegedGroups, ", ")})
	}
	r.wrappedTable([]string{"Username", "Domain", "Privileged Groups"}, []float64{40, 40, 100}, rows, 2)
}

// wrappedTable renders a bordered table where the column at wrapCol word-wraps onto multiple lines;
// each row grows to fit the wrapped content. All other columns are single-line and vertically
// centered. SplitLines and MultiCell both subtract the cell margin, so the measured line count
// matches what MultiCell renders exactly.
func (r *renderer) wrappedTable(headers []string, widths []float64, rows [][]string, wrapCol int) {
	if len(rows) == 0 {
		r.note("No data for this section.")
		return
	}
	const lineH = 4.6
	const minRowH = 6.5
	const cellMargin = 1.0

	r.resetDrawColor()
	oldMargin := r.pdf.GetCellMargin()
	r.pdf.SetCellMargin(cellMargin)
	defer r.pdf.SetCellMargin(oldMargin)

	wrapX := pageMarginLeft
	for i := 0; i < wrapCol && i < len(widths); i++ {
		wrapX += widths[i]
	}

	drawHeader := func() {
		r.headerRowFill()
		r.darkText()
		r.pdf.SetFont("Helvetica", "B", 9)
		r.pdf.SetX(pageMarginLeft)
		for i, h := range headers {
			r.pdf.CellFormat(widths[i], 7, r.s(h), "1", 0, "L", true, 0, "")
		}
		r.pdf.Ln(-1)
	}

	r.ensureSpace(14)
	drawHeader()
	r.pdf.SetFont("Helvetica", "", 9)

	for ri, row := range rows {
		wrapTxt := ""
		if wrapCol < len(row) {
			wrapTxt = r.tr(row[wrapCol])
		}
		nLines := len(r.pdf.SplitLines([]byte(wrapTxt), widths[wrapCol]))
		if nLines < 1 {
			nLines = 1
		}
		rowH := float64(nLines) * lineH
		if rowH < minRowH {
			rowH = minRowH
		}

		if r.pdf.GetY()+rowH > pageBreakTrigger {
			r.pdf.AddPage()
			drawHeader()
			r.pdf.SetFont("Helvetica", "", 9)
		}

		if ri%2 == 1 {
			r.zebraFill()
		} else {
			r.pdf.SetFillColor(255, 255, 255)
		}
		y0 := r.pdf.GetY()

		// Cell backgrounds + borders. The wrap cell gets an empty box; its text is drawn after.
		x := pageMarginLeft
		for i := range headers {
			cell := ""
			if i != wrapCol && i < len(row) {
				cell = r.s(row[i])
			}
			r.pdf.SetXY(x, y0)
			r.pdf.CellFormat(widths[i], rowH, cell, "1", 0, "L", true, 0, "")
			x += widths[i]
		}

		// Wrapped text, vertically centered within the (possibly taller) row.
		r.pdf.SetXY(wrapX, y0+(rowH-float64(nLines)*lineH)/2)
		r.pdf.MultiCell(widths[wrapCol], lineH, wrapTxt, "", "L", false)

		r.pdf.SetXY(pageMarginLeft, y0+rowH)
	}
	r.pdf.Ln(2)
}

// totalOrUnknown renders a domain-wide total, showing "n/a" when it was skipped (-1).
func totalOrUnknown(v int) string {
	if v < 0 {
		return "n/a"
	}
	return intStr(v)
}

func (r *renderer) severityFill(sev string) {
	switch sev {
	case "CRITICAL":
		r.pdf.SetFillColor(183, 28, 28)
	case "HIGH":
		r.pdf.SetFillColor(230, 81, 0)
	case "MEDIUM":
		r.pdf.SetFillColor(245, 166, 35)
	case "LOW":
		r.pdf.SetFillColor(67, 160, 71)
	default: // INFO
		r.pdf.SetFillColor(69, 90, 100)
	}
}

// --- small helpers ---

func whRow(name string, s models.WindowsHashTypeStats) []string {
	return []string{name, intStr(s.Total), intStr(s.Cracked), pctStr(s.Percentage)}
}

type kc struct {
	key string
	cc  models.CategoryCount
}

func sortedCategories(m map[string]models.CategoryCount) []kc {
	out := make([]kc, 0, len(m))
	for k, v := range m {
		out = append(out, kc{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].cc.Count != out[j].cc.Count {
			return out[i].cc.Count > out[j].cc.Count
		}
		return out[i].key < out[j].key
	})
	return out
}

func categoriesToBars(m map[string]models.CategoryCount, prettyLen bool, limit int) []barItem {
	cats := sortedCategories(m)
	if len(cats) > limit {
		cats = cats[:limit]
	}
	items := make([]barItem, 0, len(cats))
	for _, c := range cats {
		label := c.key
		if prettyLen {
			label = label + " chars"
		}
		items = append(items, barItem{label, c.cc.Percentage, c.cc.Count})
	}
	return items
}

func categoriesToRows(m map[string]models.CategoryCount, limit int) [][]string {
	cats := sortedCategories(m)
	if len(cats) > limit {
		cats = cats[:limit]
	}
	rows := make([][]string, 0, len(cats))
	for _, c := range cats {
		rows = append(rows, []string{c.key, intStr(c.cc.Count), pctStr(c.cc.Percentage)})
	}
	return rows
}

func sumCategoryBar(label string, m map[string]models.CategoryCount) barItem {
	count := 0
	pct := 0.0
	for _, v := range m {
		count += v.Count
		pct += v.Percentage
	}
	return barItem{label, pct, count}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func halfStr(cracked bool, pwd *string) string {
	if cracked && pwd != nil {
		return *pwd
	}
	if cracked {
		return "(cracked)"
	}
	return "-"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
