package bloodhound

import "strings"

// ScopeKeys returns the candidate in-scope keys for a (domain, username) identity taken from a
// report's hashlists, used to seed a Collector's inScope set. It expands to the canonical FQDN key,
// the NetBIOS first-label key, and a bare-sam key so that domainless hashes still bridge to dump
// accounts. Pass the raw DB values (domain may be "").
func ScopeKeys(domain, username string) []string {
	embDom, rawSam := splitDomainUser(username)
	sam := normSam(rawSam)
	if sam == "" {
		return nil
	}
	keys := []string{accountKey("", sam)}
	if embDom != "" {
		keys = append(keys, accountKey(embDom, sam), accountKey(firstLabel(embDom), sam))
	}
	if d := strings.TrimSpace(domain); d != "" {
		keys = append(keys, accountKey(d, sam), accountKey(firstLabel(d), sam))
	}
	return keys
}

// userInScope reports whether a dump user matches the report's hashlist scope (nil scope => all).
func (c *Collector) userInScope(u *userRec) bool {
	if c.inScope == nil {
		return true
	}
	if u.sam == "" {
		return false
	}
	for _, k := range []string{
		accountKey(u.domain, u.sam),
		accountKey(firstLabel(u.domain), u.sam),
		accountKey("", u.sam),
	} {
		if _, ok := c.inScope[k]; ok {
			return true
		}
	}
	return false
}

// Resolve produces the compact DerivedContext from the accumulated graph. After it returns, the
// caller should discard the Collector; only the returned DerivedContext should be retained.
func (c *Collector) Resolve() *DerivedContext {
	dc := &DerivedContext{
		Version:      DerivedContextVersion,
		PathsSkipped: c.pathsSkipped,
		Truncated:    c.overLimit,
		Accounts:     make(map[string]AccountFacts),
		AltIndex:     make(map[string]string),
		SamIndex:     make(map[string]string),
		Domains:      make(map[string]DomainTotals),
	}

	var reach map[string]int
	if !c.pathsSkipped {
		reach = c.reverseReachable()
	}

	// Track sAMAccountName uniqueness across ALL users for the sam-only fallback index.
	samCount := make(map[string]int)
	samToKey := make(map[string]string)

	for sid, u := range c.users {
		samLower := normSam(u.sam)
		if samLower != "" {
			samCount[samLower]++
		}
		if u.sam == "" || !c.userInScope(u) {
			continue
		}
		key := accountKey(u.domain, u.sam)
		f := c.factsFor(sid)
		if reach != nil {
			if h, ok := reach[sid]; ok && h > 0 {
				f.HasPathToDA = true
				f.PathToDAHops = h
			}
		}
		dc.Accounts[key] = f
		samToKey[samLower] = key
		domNorm := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(u.domain), "."))
		if lbl := firstLabel(u.domain); lbl != domNorm {
			dc.AltIndex[lbl+"\\"+samLower] = key
		}
	}

	for sam, cnt := range samCount {
		if cnt != 1 {
			continue
		}
		if key, ok := samToKey[sam]; ok {
			if _, ok := dc.Accounts[key]; ok {
				dc.SamIndex[sam] = key
			}
		}
	}

	c.fillDomainTotals(dc)
	return dc
}

// fillDomainTotals computes per-domain aggregate denominators over the entire dump. Closure-based
// totals (privileged/tier-0/dcsync) are skipped (left at -1) when the dump exceeds
// MaxUsersForDomainAgg, so a huge dump still yields property-only totals plus per-account facts.
func (c *Collector) fillDomainTotals(dc *DerivedContext) {
	heavy := len(c.users) <= c.lim.MaxUsersForDomainAgg
	tmp := make(map[string]*DomainTotals)
	get := func(dom string) *DomainTotals {
		d := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(dom), "."))
		t := tmp[d]
		if t == nil {
			t = &DomainTotals{}
			if !heavy {
				t.PrivilegedUsers = -1
				t.TierZeroUsers = -1
				t.DCSyncPrincipals = -1
			}
			tmp[d] = t
		}
		return t
	}
	for sid, u := range c.users {
		t := get(u.domain)
		t.Users++
		if u.enabled {
			t.Enabled++
		}
		if u.hasSPN {
			t.Kerberoastable++
		}
		if u.dontReqPreauth {
			t.ASREPRoastable++
		}
		if u.adminCount {
			t.AdminCount++
		}
		if heavy {
			f := c.factsFor(sid)
			if f.Privileged() {
				t.PrivilegedUsers++
			}
			if f.IsTierZero {
				t.TierZeroUsers++
			}
			if f.DCSync {
				t.DCSyncPrincipals++
			}
		}
	}
	for d, t := range tmp {
		dc.Domains[d] = *t
	}
}
