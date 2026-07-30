package bloodhound

import "sort"

// userRec holds the retained per-user properties needed for privilege resolution and domain totals.
type userRec struct {
	sam            string
	domain         string
	enabled        bool
	adminCount     bool
	hasSPN         bool
	dontReqPreauth bool
	sensitive      bool
	unconstrained  bool
	tierZero       bool
}

type groupInfo struct {
	name     string
	tierZero bool // own high-value/tier-0 flag from Properties
}

// Collector accumulates the AD graph incrementally while a dump is streamed, then resolves it into
// a compact DerivedContext. The raw graph it holds is transient and discarded after Resolve().
type Collector struct {
	lim Limits

	inScope map[string]struct{} // canonical account keys in the report's hashlists; nil => all in scope

	users  map[string]*userRec  // userSID → retained properties
	groups map[string]groupInfo // groupSID → info

	memberToGroups map[string][]string // principal SID → groups it is a DIRECT member of (MemberOf)

	replRights       map[string]uint8               // principal → GetChanges(bit0)|GetChangesAll(bit1)
	adminOnComputers map[string]map[string]struct{} // principal → set(computerSID) it is local admin on

	// attack graph (reverse adjacency: target → sources with an edge into it)
	reverseAdj   map[string][]string
	targets      map[string]struct{}
	domainSIDs   []string // domain object SIDs (DCSync targets)
	edgeCount    int
	pathsSkipped bool

	objectCount int
	overLimit   bool

	closureMemo map[string]map[string]struct{} // groupSID → transitive parent-group set (memoized)
}

// NewCollector builds a Collector. inScope is the set of canonical account keys present in the
// report's hashlists; pass nil to keep facts for every account in the dump.
func NewCollector(inScope map[string]struct{}, lim Limits) *Collector {
	return &Collector{
		lim:              lim,
		inScope:          inScope,
		users:            make(map[string]*userRec),
		groups:           make(map[string]groupInfo),
		memberToGroups:   make(map[string][]string),
		replRights:       make(map[string]uint8),
		adminOnComputers: make(map[string]map[string]struct{}),
		reverseAdj:       make(map[string][]string),
		targets:          make(map[string]struct{}),
		closureMemo:      make(map[string]map[string]struct{}),
	}
}

// SetInScope replaces the in-scope account-key set applied during Resolve. Pass keys built with
// ScopeKeys from the report's hashlist identities. A nil map keeps facts for every account. This lets
// a caller ingest the dump first (in-scope not yet known) and set the scope just before Resolve.
func (c *Collector) SetInScope(keys map[string]struct{}) {
	c.inScope = keys
}

func (c *Collector) countObject() bool {
	if c.objectCount >= c.lim.MaxObjects {
		c.overLimit = true
		return false
	}
	c.objectCount++
	return true
}

// addEdge records a forward attack edge from→to as reverse adjacency for the path BFS. Once the
// edge budget is exhausted, path analysis is abandoned and the (potentially huge) adjacency is
// released so privilege resolution can still complete within memory bounds.
func (c *Collector) addEdge(from, to string) {
	if from == "" || to == "" || c.pathsSkipped {
		return
	}
	if c.edgeCount >= c.lim.MaxEdges {
		c.pathsSkipped = true
		c.reverseAdj = nil
		return
	}
	c.reverseAdj[to] = append(c.reverseAdj[to], from)
	c.edgeCount++
}

func (c *Collector) addUser(u *RawUser) {
	if u.ObjectIdentifier == "" || !c.countObject() {
		return
	}
	sam := u.Properties.str("samaccountname")
	name := u.Properties.str("name")
	domain := u.Properties.str("domain")
	if domain == "" && name != "" {
		if i := lastIndexByte(name, '@'); i >= 0 {
			domain = name[i+1:]
		}
	}
	if sam == "" && name != "" {
		if i := lastIndexByte(name, '@'); i >= 0 {
			sam = name[:i]
		} else {
			sam = name
		}
	}
	tierZero := u.Properties.isTierZero()
	enabled := u.Properties.boolean("enabled")
	// Defensive de-collision: two DIFFERENT accounts can carry the SAME ObjectIdentifier in
	// malformed, merged, or synthetic dumps (real AD never reuses a live RID). The whole graph is
	// SID-keyed, so only one userRec can occupy a SID. Never let a DISABLED record clobber an
	// ENABLED one — otherwise a live, possibly-privileged account (e.g. a cracked gMSA) is silently
	// dropped in favor of a tombstoned namesake, and its facts vanish from the report. Enabled wins
	// regardless of file order; same-enabled keeps last-seen (harmless for a genuine re-collection of
	// the same account). Skipping the rest of addUser on reject also keeps the loser's edges/ACEs off
	// the surviving account's SID.
	if existing, ok := c.users[u.ObjectIdentifier]; ok && existing.enabled && !enabled {
		return
	}
	c.users[u.ObjectIdentifier] = &userRec{
		sam:            sam,
		domain:         domain,
		enabled:        enabled,
		adminCount:     u.Properties.boolean("admincount"),
		hasSPN:         u.Properties.boolean("hasspn"),
		dontReqPreauth: u.Properties.boolean("dontreqpreauth"),
		sensitive:      u.Properties.boolean("sensitive"),
		unconstrained:  u.Properties.boolean("unconstraineddelegation"),
		tierZero:       tierZero,
	}
	if tierZero {
		c.targets[u.ObjectIdentifier] = struct{}{}
	}
	if u.PrimaryGroupSID != "" {
		c.memberToGroups[u.ObjectIdentifier] = append(c.memberToGroups[u.ObjectIdentifier], u.PrimaryGroupSID)
		c.addEdge(u.ObjectIdentifier, u.PrimaryGroupSID)
	}
	c.addAces(u.ObjectIdentifier, u.Aces, false)
	for _, m := range u.AllowedToDelegate {
		c.addEdge(u.ObjectIdentifier, m.ObjectIdentifier)
	}
}

func (c *Collector) addGroup(g *RawGroup) {
	if g.ObjectIdentifier == "" || !c.countObject() {
		return
	}
	own := g.Properties.isTierZero()
	c.groups[g.ObjectIdentifier] = groupInfo{name: g.Properties.str("name"), tierZero: own}
	if own || isPrivilegedSID(g.ObjectIdentifier) {
		c.targets[g.ObjectIdentifier] = struct{}{}
	}
	for _, m := range g.Members {
		if m.ObjectIdentifier == "" {
			continue
		}
		c.memberToGroups[m.ObjectIdentifier] = append(c.memberToGroups[m.ObjectIdentifier], g.ObjectIdentifier)
		c.addEdge(m.ObjectIdentifier, g.ObjectIdentifier)
	}
	c.addAces(g.ObjectIdentifier, g.Aces, false)
}

func (c *Collector) addComputer(comp *RawComputer) {
	if comp.ObjectIdentifier == "" || !c.countObject() {
		return
	}
	addAdmins := func(set RawPrincipalSet) {
		for _, m := range set.Results {
			if m.ObjectIdentifier == "" {
				continue
			}
			s := c.adminOnComputers[m.ObjectIdentifier]
			if s == nil {
				s = make(map[string]struct{})
				c.adminOnComputers[m.ObjectIdentifier] = s
			}
			s[comp.ObjectIdentifier] = struct{}{}
			c.addEdge(m.ObjectIdentifier, comp.ObjectIdentifier)
		}
	}
	addAdmins(comp.LocalAdmins)
	addAdmins(comp.AdminRights)
	for _, set := range []RawPrincipalSet{comp.RemoteDesktopUsers, comp.PSRemoteUsers, comp.DcomUsers, comp.AllowedToAct} {
		for _, m := range set.Results {
			c.addEdge(m.ObjectIdentifier, comp.ObjectIdentifier)
		}
	}
	// Controlling a computer lets an attacker steal the sessions on it: computer → user.
	for _, m := range comp.Sessions.Results {
		c.addEdge(comp.ObjectIdentifier, m.ObjectIdentifier)
	}
	if comp.PrimaryGroupSID != "" {
		c.memberToGroups[comp.ObjectIdentifier] = append(c.memberToGroups[comp.ObjectIdentifier], comp.PrimaryGroupSID)
		c.addEdge(comp.ObjectIdentifier, comp.PrimaryGroupSID)
	}
	for _, m := range comp.AllowedToDelegate {
		c.addEdge(comp.ObjectIdentifier, m.ObjectIdentifier)
	}
	c.addAces(comp.ObjectIdentifier, comp.Aces, false)
}

func (c *Collector) addDomain(d *RawDomain) {
	if d.ObjectIdentifier == "" {
		return
	}
	c.targets[d.ObjectIdentifier] = struct{}{}
	c.domainSIDs = append(c.domainSIDs, d.ObjectIdentifier)
	c.addAces(d.ObjectIdentifier, d.Aces, true)
}

// addAces records control-edge ACEs into the attack graph and, for domain objects, folds
// replication rights into the DCSync bitset.
func (c *Collector) addAces(target string, aces []RawAce, isDomain bool) {
	for _, a := range aces {
		if a.PrincipalSID == "" {
			continue
		}
		if isDomain {
			switch a.RightName {
			case "GetChanges":
				c.replRights[a.PrincipalSID] |= 1
			case "GetChangesAll":
				c.replRights[a.PrincipalSID] |= 2
			case "All", "DCSync":
				c.replRights[a.PrincipalSID] |= 3
			}
		}
		if isControlRight(a.RightName) {
			c.addEdge(a.PrincipalSID, target)
		}
	}
}

// groupClosure returns the set of all groups `g` is transitively a member of (its parent groups),
// memoized. Cycles are broken by seeding an in-progress placeholder before recursing.
func (c *Collector) groupClosure(g string) map[string]struct{} {
	if r, ok := c.closureMemo[g]; ok {
		return r
	}
	c.closureMemo[g] = map[string]struct{}{} // in-progress placeholder breaks cycles
	res := make(map[string]struct{})
	for _, parent := range c.memberToGroups[g] {
		res[parent] = struct{}{}
		for k := range c.groupClosure(parent) {
			res[k] = struct{}{}
		}
	}
	c.closureMemo[g] = res
	return res
}

// effectiveGroupsOf returns every group (direct + nested) a principal effectively belongs to.
func (c *Collector) effectiveGroupsOf(sid string) map[string]struct{} {
	res := make(map[string]struct{})
	for _, g := range c.memberToGroups[sid] {
		res[g] = struct{}{}
		for k := range c.groupClosure(g) {
			res[k] = struct{}{}
		}
	}
	return res
}

// factsFor resolves the full privilege standing of a principal.
func (c *Collector) factsFor(sid string) AccountFacts {
	u := c.users[sid]
	if u == nil {
		u = &userRec{}
	}
	eff := c.effectiveGroupsOf(sid)
	f := AccountFacts{
		SID:                     sid,
		Enabled:                 u.enabled,
		AdminCount:              u.adminCount,
		HasSPN:                  u.hasSPN,
		DontReqPreauth:          u.dontReqPreauth,
		Sensitive:               u.sensitive,
		UnconstrainedDelegation: u.unconstrained,
		IsTierZero:              u.tierZero,
	}
	var privGroups []string
	for g := range eff {
		gi := c.groups[g]
		if isPrivilegedSID(g) || gi.tierZero {
			f.IsTierZero = true
			name := gi.name
			if name == "" {
				name = g
			}
			privGroups = append(privGroups, name)
		}
		if isDomainAdminSID(g) {
			f.EffectiveDomainAdmin = true
		}
	}
	sort.Strings(privGroups)
	f.PrivilegedGroups = dedupeSorted(privGroups)
	f.DCSync = c.hasDCSync(sid, eff)
	f.LocalAdminCount = c.localAdminCount(sid, eff)
	return f
}

// hasDCSync reports whether the principal (directly or via an effective group) holds both
// GetChanges and GetChangesAll on a domain — the replication rights required for a DCSync attack.
func (c *Collector) hasDCSync(sid string, eff map[string]struct{}) bool {
	if c.replRights[sid] == 3 {
		return true
	}
	for g := range eff {
		if c.replRights[g] == 3 {
			return true
		}
	}
	return false
}

// localAdminCount returns the number of distinct computers the principal is local admin on, directly
// or through an effective group.
func (c *Collector) localAdminCount(sid string, eff map[string]struct{}) int {
	seen := make(map[string]struct{})
	for comp := range c.adminOnComputers[sid] {
		seen[comp] = struct{}{}
	}
	for g := range eff {
		for comp := range c.adminOnComputers[g] {
			seen[comp] = struct{}{}
		}
	}
	return len(seen)
}

func dedupeSorted(sorted []string) []string {
	if len(sorted) < 2 {
		return sorted
	}
	out := sorted[:1]
	for _, s := range sorted[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}
	return out
}

func lastIndexByte(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}
