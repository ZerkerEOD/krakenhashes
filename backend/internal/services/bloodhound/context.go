package bloodhound

import "strings"

// DerivedContextVersion is the schema version of the persisted DerivedContext. Bump it if the
// shape of the persisted JSON changes so a stale staged context can be detected/ignored.
const DerivedContextVersion = 1

// AccountFacts is the compact per-account AD-privilege fact set. It is the ONLY BloodHound-derived
// data persisted to the database (the raw graph is discarded after upload). It carries no plaintext
// and no session/ACE detail — just the resolved standing of a single principal.
type AccountFacts struct {
	SID                     string   `json:"sid"`
	Enabled                 bool     `json:"enabled"`
	AdminCount              bool     `json:"admin_count"`
	HasSPN                  bool     `json:"has_spn"`          // Kerberoastable
	DontReqPreauth          bool     `json:"dont_req_preauth"` // AS-REP roastable
	Sensitive               bool     `json:"sensitive"`
	UnconstrainedDelegation bool     `json:"unconstrained_delegation"`
	IsTierZero              bool     `json:"is_tier_zero"`
	EffectiveDomainAdmin    bool     `json:"effective_domain_admin"`
	DCSync                  bool     `json:"dcsync"`
	PrivilegedGroups        []string `json:"privileged_groups,omitempty"`
	LocalAdminCount         int      `json:"local_admin_count"`
	HasPathToDA             bool     `json:"has_path_to_da"`
	PathToDAHops            int      `json:"path_to_da_hops"`
}

// Privileged reports whether this account confers any elevated standing.
func (f AccountFacts) Privileged() bool {
	return f.EffectiveDomainAdmin || f.IsTierZero || len(f.PrivilegedGroups) > 0
}

// DomainTotals holds domain-wide denominators computed over the ENTIRE dump (not just in-scope
// accounts), so a report can say "cracked 3 of 5 Domain Admins in the domain". A value of -1 means
// the total was skipped because the dump exceeded MaxUsersForDomainAgg.
type DomainTotals struct {
	Users            int `json:"users"`
	Enabled          int `json:"enabled"`
	Kerberoastable   int `json:"kerberoastable"`
	ASREPRoastable   int `json:"asrep_roastable"`
	AdminCount       int `json:"admin_count"`
	PrivilegedUsers  int `json:"privileged_users"`
	TierZeroUsers    int `json:"tier_zero_users"`
	DCSyncPrincipals int `json:"dcsync_principals"`
}

// DerivedContext is the compact persisted object. It contains facts ONLY for in-scope accounts
// (accounts present in the report's hashlists) plus per-domain aggregate denominators.
type DerivedContext struct {
	Version      int                     `json:"version"`
	PathsSkipped bool                    `json:"paths_skipped"` // attack-path analysis skipped (graph too large)
	Truncated    bool                    `json:"truncated"`     // object cap hit; dump partially ingested
	Accounts     map[string]AccountFacts `json:"accounts"`      // canonical key = lower(fqdn)+"\\"+lower(sam)
	AltIndex     map[string]string       `json:"alt_index"`     // lower(firstLabel)+"\\"+lower(sam) → canonical key
	SamIndex     map[string]string       `json:"sam_index"`     // lower(sam) → canonical key, only when sam is globally unique
	Domains      map[string]DomainTotals `json:"domains"`       // lower(fqdn) → totals
}

// normSam normalizes a sAMAccountName for keying and matching: lowercased, trimmed, with a trailing
// '$' removed. Machine and group-managed-service accounts (gMSA) carry a trailing '$' in AD but not
// always in a recovered credential, so stripping it here makes those accounts key CONSISTENTLY across
// context build, in-scope seeding, and cracked-account lookup. Without this, gMSA/computer accounts
// key as "…\name$" on build but are looked up as "…\name" and never match.
func normSam(sam string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(sam)), "$")
}

// accountKey builds the canonical lookup key from a domain and sAMAccountName.
func accountKey(domain, sam string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), ".")) + "\\" + normSam(sam)
}

// firstLabel returns the lowercased first DNS label of a domain (e.g. "corp.local" → "corp").
func firstLabel(domain string) string {
	d := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if i := strings.IndexByte(d, '.'); i >= 0 {
		return d[:i]
	}
	return d
}

// splitDomainUser splits "DOMAIN\\user" or "user@domain" into (domain, user). Returns ("", user)
// when no domain component is present.
func splitDomainUser(username string) (domain, user string) {
	username = strings.TrimSpace(username)
	if i := strings.LastIndex(username, "\\"); i >= 0 {
		return username[:i], username[i+1:]
	}
	if i := strings.LastIndex(username, "@"); i > 0 {
		return username[i+1:], username[:i]
	}
	return "", username
}

// Lookup resolves a cracked account's identity to its AD facts. domain may be nil/empty (some hash
// formats carry no domain). Matching is case-insensitive and layered: an embedded DOMAIN\ prefix,
// then the provided domain (full FQDN or NetBIOS first label), then a globally-unique
// sAMAccountName fallback. Ambiguous sAMAccountNames are deliberately NOT in SamIndex, so a miss is
// preferred over a mis-attribution.
func (dc *DerivedContext) Lookup(username string, domain *string) (AccountFacts, bool) {
	if dc == nil {
		return AccountFacts{}, false
	}
	embDom, rawSam := splitDomainUser(username)
	sam := normSam(rawSam)
	if sam == "" {
		return AccountFacts{}, false
	}
	if embDom != "" {
		if f, ok := dc.accountByDomainSam(embDom, sam); ok {
			return f, true
		}
	}
	if domain != nil {
		if d := strings.TrimSpace(*domain); d != "" {
			if f, ok := dc.accountByDomainSam(d, sam); ok {
				return f, true
			}
		}
	}
	if key, ok := dc.SamIndex[sam]; ok {
		if f, ok := dc.Accounts[key]; ok {
			return f, true
		}
	}
	return AccountFacts{}, false
}

// accountByDomainSam tries the canonical FQDN key first, then the NetBIOS first-label alt index.
func (dc *DerivedContext) accountByDomainSam(domain, sam string) (AccountFacts, bool) {
	s := strings.ToLower(sam)
	if f, ok := dc.Accounts[accountKey(domain, s)]; ok {
		return f, true
	}
	if key, ok := dc.AltIndex[firstLabel(domain)+"\\"+s]; ok {
		if f, ok := dc.Accounts[key]; ok {
			return f, true
		}
	}
	return AccountFacts{}, false
}
