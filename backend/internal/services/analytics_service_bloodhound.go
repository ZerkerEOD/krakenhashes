package services

import (
	"context"
	"fmt"
	"sort"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/bloodhound"
	"github.com/google/uuid"
)

const (
	bhMaxAccounts = 500 // cap per-section account lists to bound JSONB size
	bhMaxBlast    = 50  // cap local-admin blast-radius top accounts
)

// enrichReportWithBloodhound loads any staged BloodHound context for the report and cross-references
// it against the report's cracked accounts to populate the AD-privilege sections on data. It returns
// whether a context was present (so the caller can clear it after the report is persisted). When no
// dump was uploaded, it is a no-op and returns (false, nil), leaving the report unchanged.
func (s *AnalyticsService) enrichReportWithBloodhound(ctx context.Context, reportID uuid.UUID, hashlistIDs []int64, data *models.AnalyticsData) (bool, error) {
	dc, err := s.repo.GetBloodhoundContext(ctx, reportID)
	if err != nil {
		return false, err
	}
	if dc == nil {
		return false, nil
	}
	crackedRefs, err := s.repo.GetCrackedAccountRefs(ctx, hashlistIDs)
	if err != nil {
		return true, err
	}
	enrichAnalyticsWithBloodhound(data, dc, crackedRefs)
	return true, nil
}

// crackedMatch pairs a cracked identity with its resolved AD facts.
type crackedMatch struct {
	ref models.AccountRef
	f   bloodhound.AccountFacts
}

// enrichAnalyticsWithBloodhound is the pure cross-reference: it fills the BloodHound sections on data
// from the derived context and the set of cracked identities. It also appends high-severity
// recommendations and re-sorts the recommendation list by severity.
func enrichAnalyticsWithBloodhound(data *models.AnalyticsData, dc *bloodhound.DerivedContext, crackedRefs []models.AccountRef) {
	if dc == nil {
		return
	}

	// Resolve cracked identities to facts, deduped by SID.
	seen := make(map[string]bool)
	var matches []crackedMatch
	for _, ref := range crackedRefs {
		f, ok := dc.Lookup(ref.Username, ref.Domain)
		if !ok {
			continue
		}
		if f.SID != "" {
			if seen[f.SID] {
				continue
			}
			seen[f.SID] = true
		}
		matches = append(matches, crackedMatch{ref: ref, f: f})
	}

	// In-scope denominators from all in-scope accounts (cracked or not).
	var inPriv, inKerb, inASREP, inAdmin, inPath int
	for _, f := range dc.Accounts {
		if f.Privileged() {
			inPriv++
		}
		if f.HasSPN {
			inKerb++
		}
		if f.DontReqPreauth {
			inASREP++
		}
		if f.AdminCount {
			inAdmin++
		}
		if f.HasPathToDA {
			inPath++
		}
	}

	// --- Privileged compromise (headline) ---
	adp := &models.ADPrivilegeStats{
		InScopePrivileged:     inPriv,
		DomainPrivilegedTotal: sumDomainTotal(dc, func(d bloodhound.DomainTotals) int { return d.PrivilegedUsers }),
	}
	for _, m := range matches {
		if m.f.EffectiveDomainAdmin {
			adp.CrackedEffectiveDA++
		}
		if m.f.IsTierZero {
			adp.CrackedTierZero++
		}
		if m.f.Privileged() {
			adp.CrackedPrivileged++
			adp.Accounts = append(adp.Accounts, compromisedAccount(m.ref, m.f))
		}
	}
	adp.PercentPrivilegedCracked = safePercentage(adp.CrackedPrivileged, inPriv)
	adp.Accounts = capAccounts(sortAccounts(adp.Accounts), bhMaxAccounts)
	data.ADPrivilege = adp

	// --- Kerberoastable cracked ---
	kerb := &models.RoastableCrackStats{
		DomainTotal:  sumDomainTotal(dc, func(d bloodhound.DomainTotals) int { return d.Kerberoastable }),
		InScopeTotal: inKerb,
	}
	for _, m := range matches {
		if m.f.HasSPN {
			kerb.Cracked++
			if m.f.Privileged() {
				kerb.CrackedPrivileged++
			}
			kerb.Accounts = append(kerb.Accounts, compromisedAccount(m.ref, m.f))
		}
	}
	kerb.PercentCracked = safePercentage(kerb.Cracked, inKerb)
	kerb.Accounts = capAccounts(sortAccounts(kerb.Accounts), bhMaxAccounts)
	data.KerberoastCracked = kerb

	// --- AS-REP roastable cracked ---
	asrep := &models.RoastableCrackStats{
		DomainTotal:  sumDomainTotal(dc, func(d bloodhound.DomainTotals) int { return d.ASREPRoastable }),
		InScopeTotal: inASREP,
	}
	for _, m := range matches {
		if m.f.DontReqPreauth {
			asrep.Cracked++
			if m.f.Privileged() {
				asrep.CrackedPrivileged++
			}
			asrep.Accounts = append(asrep.Accounts, compromisedAccount(m.ref, m.f))
		}
	}
	asrep.PercentCracked = safePercentage(asrep.Cracked, inASREP)
	asrep.Accounts = capAccounts(sortAccounts(asrep.Accounts), bhMaxAccounts)
	data.ASREPRoastCracked = asrep

	// --- AdminCount cracked ---
	ac := &models.AdminCountCrackStats{
		DomainTotal:  sumDomainTotal(dc, func(d bloodhound.DomainTotals) int { return d.AdminCount }),
		InScopeTotal: inAdmin,
	}
	for _, m := range matches {
		if m.f.AdminCount {
			ac.Cracked++
			ac.Accounts = append(ac.Accounts, compromisedAccount(m.ref, m.f))
		}
	}
	ac.PercentCracked = safePercentage(ac.Cracked, inAdmin)
	ac.Accounts = capAccounts(sortAccounts(ac.Accounts), bhMaxAccounts)
	data.AdminCountCracked = ac

	// --- DCSync cracked ---
	dcs := &models.DCSyncCrackStats{
		DomainPrincipals: sumDomainTotal(dc, func(d bloodhound.DomainTotals) int { return d.DCSyncPrincipals }),
	}
	for _, m := range matches {
		if m.f.DCSync {
			dcs.Cracked++
			dcs.Accounts = append(dcs.Accounts, compromisedAccount(m.ref, m.f))
		}
	}
	dcs.Accounts = capAccounts(sortAccounts(dcs.Accounts), bhMaxAccounts)
	data.DCSyncCracked = dcs

	// --- Local-admin blast radius ---
	lab := &models.LocalAdminBlastStats{}
	for _, m := range matches {
		if m.f.LocalAdminCount > 0 {
			lab.CrackedWithLocalAdmin++
			lab.TotalAdminRelationships += m.f.LocalAdminCount
			if m.f.LocalAdminCount > lab.MaxComputersSingle {
				lab.MaxComputersSingle = m.f.LocalAdminCount
			}
			lab.TopAccounts = append(lab.TopAccounts, models.BlastAccount{
				CompromisedAccount: compromisedAccount(m.ref, m.f),
				ComputerCount:      m.f.LocalAdminCount,
			})
		}
	}
	sort.SliceStable(lab.TopAccounts, func(i, j int) bool {
		if lab.TopAccounts[i].ComputerCount != lab.TopAccounts[j].ComputerCount {
			return lab.TopAccounts[i].ComputerCount > lab.TopAccounts[j].ComputerCount
		}
		return lab.TopAccounts[i].Username < lab.TopAccounts[j].Username
	})
	if len(lab.TopAccounts) > bhMaxBlast {
		lab.TopAccounts = lab.TopAccounts[:bhMaxBlast]
	}
	data.LocalAdminBlast = lab

	// --- Path to Domain Admin ---
	p2da := &models.PathToDAStats{Skipped: dc.PathsSkipped, InScopeWithPath: inPath}
	shortest := 0
	for _, m := range matches {
		if m.f.HasPathToDA {
			p2da.CrackedWithPath++
			p2da.Accounts = append(p2da.Accounts, models.PathAccount{
				CompromisedAccount: compromisedAccount(m.ref, m.f),
				Hops:               m.f.PathToDAHops,
			})
			if shortest == 0 || m.f.PathToDAHops < shortest {
				shortest = m.f.PathToDAHops
			}
		}
	}
	p2da.ShortestHops = shortest
	sort.SliceStable(p2da.Accounts, func(i, j int) bool {
		if p2da.Accounts[i].Hops != p2da.Accounts[j].Hops {
			return p2da.Accounts[i].Hops < p2da.Accounts[j].Hops
		}
		return p2da.Accounts[i].Username < p2da.Accounts[j].Username
	})
	if len(p2da.Accounts) > bhMaxAccounts {
		p2da.Accounts = p2da.Accounts[:bhMaxAccounts]
	}
	data.PathToDA = p2da

	appendBloodhoundRecommendations(data)
}

// appendBloodhoundRecommendations adds high-severity findings for the AD sections and re-sorts the
// whole recommendation list by severity so BloodHound criticals surface at the top.
func appendBloodhoundRecommendations(data *models.AnalyticsData) {
	add := func(sev string, count int, msg string) {
		data.Recommendations = append(data.Recommendations, models.Recommendation{
			Severity: sev,
			Count:    count,
			Message:  msg,
		})
	}

	if p := data.PathToDA; p != nil && p.CrackedWithPath > 0 {
		add("CRITICAL", p.CrackedWithPath, fmt.Sprintf(
			"%d cracked account(s) have an attack path to Domain Admin (shortest %d hop(s)). Reset these credentials and remediate the path.",
			p.CrackedWithPath, p.ShortestHops))
	}
	if d := data.DCSyncCracked; d != nil && d.Cracked > 0 {
		add("CRITICAL", d.Cracked, fmt.Sprintf(
			"%d cracked account(s) hold DCSync (domain replication) rights, enabling full domain credential theft. Rotate immediately (incl. krbtgt).",
			d.Cracked))
	}
	if a := data.ADPrivilege; a != nil {
		if a.CrackedEffectiveDA > 0 {
			add("CRITICAL", a.CrackedEffectiveDA, fmt.Sprintf(
				"%d cracked account(s) are effective Domain/Enterprise Admins. Treat as full domain compromise.", a.CrackedEffectiveDA))
		} else if a.CrackedPrivileged > 0 {
			add("HIGH", a.CrackedPrivileged, fmt.Sprintf(
				"%d cracked account(s) hold privileged / tier-0 group membership.", a.CrackedPrivileged))
		}
	}
	if k := data.KerberoastCracked; k != nil && k.Cracked > 0 {
		add("HIGH", k.Cracked, fmt.Sprintf(
			"%d Kerberoastable account(s) were cracked (%d privileged). Enforce long/managed service-account passwords (gMSA).",
			k.Cracked, k.CrackedPrivileged))
	}
	if r := data.ASREPRoastCracked; r != nil && r.Cracked > 0 {
		add("HIGH", r.Cracked, fmt.Sprintf(
			"%d AS-REP roastable account(s) were cracked. Enable Kerberos pre-authentication on these accounts.", r.Cracked))
	}
	if l := data.LocalAdminBlast; l != nil && l.CrackedWithLocalAdmin > 0 {
		add("HIGH", l.CrackedWithLocalAdmin, fmt.Sprintf(
			"%d cracked account(s) are local admin across machines (up to %d on a single account). Limit local-admin sprawl and enable LAPS.",
			l.CrackedWithLocalAdmin, l.MaxComputersSingle))
	}
	if a := data.AdminCountCracked; a != nil && a.Cracked > 0 {
		add("MEDIUM", a.Cracked, fmt.Sprintf(
			"%d cracked account(s) are AdminSDHolder-protected (admincount=1). Review whether their elevated standing is still required.", a.Cracked))
	}

	sort.SliceStable(data.Recommendations, func(i, j int) bool {
		return severityRank(data.Recommendations[i].Severity) < severityRank(data.Recommendations[j].Severity)
	})
}

func severityRank(sev string) int {
	switch sev {
	case "CRITICAL":
		return 0
	case "HIGH":
		return 1
	case "MEDIUM":
		return 2
	case "INFO":
		return 3
	default:
		return 4
	}
}

// sumDomainTotal sums a per-domain field across the dump. Returns -1 if any domain reported the
// field as skipped (-1), so an unknown total is not silently understated.
func sumDomainTotal(dc *bloodhound.DerivedContext, pick func(bloodhound.DomainTotals) int) int {
	total := 0
	for _, d := range dc.Domains {
		v := pick(d)
		if v < 0 {
			return -1
		}
		total += v
	}
	return total
}

func compromisedAccount(ref models.AccountRef, f bloodhound.AccountFacts) models.CompromisedAccount {
	domain := ""
	if ref.Domain != nil {
		domain = *ref.Domain
	}
	return models.CompromisedAccount{
		Username:         ref.Username,
		Domain:           domain,
		SID:              f.SID,
		PrivilegedGroups: f.PrivilegedGroups,
		Enabled:          f.Enabled,
	}
}

func sortAccounts(accts []models.CompromisedAccount) []models.CompromisedAccount {
	sort.SliceStable(accts, func(i, j int) bool {
		// More privileged (more group memberships) first, then alphabetical.
		if len(accts[i].PrivilegedGroups) != len(accts[j].PrivilegedGroups) {
			return len(accts[i].PrivilegedGroups) > len(accts[j].PrivilegedGroups)
		}
		if accts[i].Username != accts[j].Username {
			return accts[i].Username < accts[j].Username
		}
		return accts[i].Domain < accts[j].Domain
	})
	return accts
}

func capAccounts(accts []models.CompromisedAccount, max int) []models.CompromisedAccount {
	if len(accts) > max {
		return accts[:max]
	}
	return accts
}
