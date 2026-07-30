package bloodhound

import "strconv"

// builtinPrivilegedSIDs are well-known BUILTIN group SIDs that confer tier-0 / privileged standing.
var builtinPrivilegedSIDs = map[string]struct{}{
	"S-1-5-32-544": {}, // Administrators
	"S-1-5-32-548": {}, // Account Operators
	"S-1-5-32-549": {}, // Server Operators
	"S-1-5-32-550": {}, // Print Operators
	"S-1-5-32-551": {}, // Backup Operators
}

// ridOf returns the trailing RID of a SID, or -1 if it cannot be parsed.
func ridOf(sid string) int {
	i := lastDash(sid)
	if i < 0 || i+1 >= len(sid) {
		return -1
	}
	rid, err := strconv.Atoi(sid[i+1:])
	if err != nil {
		return -1
	}
	return rid
}

func lastDash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '-' {
			return i
		}
	}
	return -1
}

// isPrivilegedSID reports whether a group SID is a well-known privileged/tier-0 group. Matching is by
// trailing RID, which is robust to BloodHound's domain-prefixed BUILTIN form (e.g.
// "CORP.LOCAL-S-1-5-32-544") as well as the bare form ("S-1-5-32-544"). RIDs 544/548-551 only ever
// occur as BUILTIN local groups, so matching them by RID cannot collide with a domain group.
func isPrivilegedSID(sid string) bool {
	if _, ok := builtinPrivilegedSIDs[sid]; ok {
		return true
	}
	switch ridOf(sid) {
	case 512, // Domain Admins
		519,                     // Enterprise Admins
		518,                     // Schema Admins
		516,                     // Domain Controllers
		517,                     // Cert Publishers
		520,                     // Group Policy Creator Owners
		526,                     // Key Admins
		527,                     // Enterprise Key Admins
		544,                     // BUILTIN\Administrators
		548, 549, 550, 551:      // Operators (Account/Server/Print/Backup)
		return true
	}
	return false
}

// isDomainAdminSID reports whether a group SID grants effective Domain Admin (i.e. full domain
// compromise): Domain Admins, Enterprise Admins, or BUILTIN\Administrators. Matched by trailing RID
// so the domain-prefixed BUILTIN form is handled.
func isDomainAdminSID(sid string) bool {
	switch ridOf(sid) {
	case 512, // Domain Admins
		519, // Enterprise Admins
		544: // BUILTIN\Administrators
		return true
	}
	return false
}

// isControlRight reports whether an ACE right name is an attack-usable control edge in BloodHound's
// taxonomy (grants the principal a way to take over the target object).
func isControlRight(r string) bool {
	switch r {
	case "GenericAll", "GenericWrite", "WriteDacl", "WriteOwner", "Owns", "Owner",
		"AddMember", "AddMembers", "ForceChangePassword", "AllExtendedRights",
		"ReadLAPSPassword", "ReadGMSAPassword", "AddKeyCredentialLink", "AddSelf",
		"WriteSPN", "DCSync", "SyncLAPSPassword", "WriteAccountRestrictions":
		return true
	}
	return false
}

// addReplicationEdges adds a control edge from each full-DCSync principal to every domain object.
func (c *Collector) addReplicationEdges() {
	for p, bits := range c.replRights {
		if bits == 3 {
			for _, d := range c.domainSIDs {
				c.addEdge(p, d)
			}
		}
	}
}

// reverseReachable performs a single reverse-BFS from the target set over the reversed attack graph.
// It returns, for every principal that has a forward attack path to any tier-0 target, the shortest
// number of hops. Runs in O(V+E) once for all accounts. Returns nil when path analysis was skipped.
func (c *Collector) reverseReachable() map[string]int {
	if c.pathsSkipped || c.reverseAdj == nil {
		return nil
	}
	// A principal holding full replication rights (GetChanges + GetChangesAll) can DCSync the domain,
	// which is domain compromise — model that as an attack edge principal → domain.
	c.addReplicationEdges()
	if c.pathsSkipped || c.reverseAdj == nil {
		return nil
	}
	dist := make(map[string]int, len(c.reverseAdj))
	queue := make([]string, 0, len(c.targets))
	for t := range c.targets {
		if _, ok := dist[t]; !ok {
			dist[t] = 0
			queue = append(queue, t)
		}
	}
	for i := 0; i < len(queue); i++ {
		cur := queue[i]
		next := dist[cur] + 1
		for _, src := range c.reverseAdj[cur] {
			if _, ok := dist[src]; !ok {
				dist[src] = next
				queue = append(queue, src)
			}
		}
	}
	return dist
}
