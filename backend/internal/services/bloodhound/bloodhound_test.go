package bloodhound

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"
)

const (
	usersDoc = `{"meta":{"type":"users","count":3,"version":5},"data":[
	  {"ObjectIdentifier":"S-1-5-21-1-1001","Properties":{"samaccountname":"alice","name":"ALICE@CORP.LOCAL","domain":"CORP.LOCAL","enabled":true,"hasspn":true}},
	  {"ObjectIdentifier":"S-1-5-21-1-1002","Properties":{"samaccountname":"bob","name":"BOB@CORP.LOCAL","domain":"CORP.LOCAL","enabled":true,"dontreqpreauth":true}},
	  {"ObjectIdentifier":"S-1-5-21-1-1003","Properties":{"samaccountname":"carol","name":"CAROL@CORP.LOCAL","domain":"CORP.LOCAL","enabled":true,"admincount":true}}
	]}`

	// Domain Admins (RID 512) contains alice directly and the NESTED group; NESTED contains bob,
	// so bob is transitively a Domain Admin.
	groupsDoc = `{"meta":{"type":"groups","count":2,"version":5},"data":[
	  {"ObjectIdentifier":"S-1-5-21-1-512","Properties":{"name":"DOMAIN ADMINS@CORP.LOCAL"},"Members":[
	    {"ObjectIdentifier":"S-1-5-21-1-1001","ObjectType":"User"},
	    {"ObjectIdentifier":"S-1-5-21-1-1105","ObjectType":"Group"}]},
	  {"ObjectIdentifier":"S-1-5-21-1-1105","Properties":{"name":"NESTED@CORP.LOCAL"},"Members":[
	    {"ObjectIdentifier":"S-1-5-21-1-1002","ObjectType":"User"}]}
	]}`

	// carol holds GetChanges + GetChangesAll on the domain → full DCSync.
	domainsDoc = `{"meta":{"type":"domains","count":1,"version":5},"data":[
	  {"ObjectIdentifier":"S-1-5-21-1","Properties":{"name":"CORP.LOCAL"},"Aces":[
	    {"PrincipalSID":"S-1-5-21-1-1003","PrincipalType":"User","RightName":"GetChanges"},
	    {"PrincipalSID":"S-1-5-21-1-1003","PrincipalType":"User","RightName":"GetChangesAll"}]}
	]}`

	computersDoc = `{"meta":{"type":"computers","count":1,"version":5},"data":[
	  {"ObjectIdentifier":"S-1-5-21-1-2001","Properties":{"name":"WS01.CORP.LOCAL"},"LocalAdmins":{"Results":[
	    {"ObjectIdentifier":"S-1-5-21-1-1001","ObjectType":"User"}]}}
	]}`
)

func parseDocs(t *testing.T, docs ...string) *DerivedContext {
	t.Helper()
	c := NewCollector(nil, DefaultLimits())
	for i, d := range docs {
		if err := ParseInput(strings.NewReader(d), fmt.Sprintf("doc%d.json", i), c); err != nil {
			t.Fatalf("ParseInput(doc %d): %v", i, err)
		}
	}
	return c.Resolve()
}

func factsFor(t *testing.T, dc *DerivedContext, user string, domain *string) AccountFacts {
	t.Helper()
	f, ok := dc.Lookup(user, domain)
	if !ok {
		t.Fatalf("account %q not found in derived context", user)
	}
	return f
}

func strptr(s string) *string { return &s }

func TestResolvePrivilegeFacts(t *testing.T) {
	dc := parseDocs(t, usersDoc, groupsDoc, domainsDoc, computersDoc)

	if len(dc.Accounts) != 3 {
		t.Fatalf("expected 3 in-scope accounts, got %d", len(dc.Accounts))
	}

	alice := factsFor(t, dc, "alice", nil)
	if !alice.EffectiveDomainAdmin {
		t.Error("alice should be an effective Domain Admin (direct member of DA)")
	}
	if !alice.HasSPN {
		t.Error("alice should be Kerberoastable (hasspn)")
	}
	if alice.LocalAdminCount != 1 {
		t.Errorf("alice local-admin count = %d, want 1", alice.LocalAdminCount)
	}
	if !alice.HasPathToDA || alice.PathToDAHops != 1 {
		t.Errorf("alice path-to-DA = %v hops=%d, want true/1", alice.HasPathToDA, alice.PathToDAHops)
	}

	bob := factsFor(t, dc, "bob", nil)
	if !bob.EffectiveDomainAdmin {
		t.Error("bob should be an effective Domain Admin transitively (NESTED -> DA)")
	}
	if !bob.DontReqPreauth {
		t.Error("bob should be AS-REP roastable (dontreqpreauth)")
	}
	if !bob.HasPathToDA || bob.PathToDAHops != 2 {
		t.Errorf("bob path-to-DA hops = %d, want 2", bob.PathToDAHops)
	}

	carol := factsFor(t, dc, "carol", nil)
	if carol.EffectiveDomainAdmin {
		t.Error("carol should NOT be an effective Domain Admin")
	}
	if !carol.DCSync {
		t.Error("carol should have DCSync (GetChanges + GetChangesAll)")
	}
	if !carol.AdminCount {
		t.Error("carol should be admincount-flagged")
	}
	if !carol.HasPathToDA || carol.PathToDAHops != 1 {
		t.Errorf("carol path-to-DA hops = %d, want 1 (DCSync edge to domain)", carol.PathToDAHops)
	}
}

func TestDomainTotals(t *testing.T) {
	dc := parseDocs(t, usersDoc, groupsDoc, domainsDoc, computersDoc)
	tot, ok := dc.Domains["corp.local"]
	if !ok {
		t.Fatalf("no domain totals for corp.local; got %v", dc.Domains)
	}
	if tot.Users != 3 {
		t.Errorf("Users = %d, want 3", tot.Users)
	}
	if tot.PrivilegedUsers != 2 {
		t.Errorf("PrivilegedUsers = %d, want 2 (alice, bob)", tot.PrivilegedUsers)
	}
	if tot.DCSyncPrincipals != 1 {
		t.Errorf("DCSyncPrincipals = %d, want 1 (carol)", tot.DCSyncPrincipals)
	}
	if tot.Kerberoastable != 1 || tot.ASREPRoastable != 1 || tot.AdminCount != 1 {
		t.Errorf("roastable/admincount totals = %d/%d/%d, want 1/1/1",
			tot.Kerberoastable, tot.ASREPRoastable, tot.AdminCount)
	}
}

func TestMatching(t *testing.T) {
	dc := parseDocs(t, usersDoc, groupsDoc, domainsDoc, computersDoc)

	// Exact FQDN domain.
	if _, ok := dc.Lookup("bob", strptr("CORP.LOCAL")); !ok {
		t.Error("exact FQDN lookup for bob failed")
	}
	// NetBIOS first-label domain.
	if _, ok := dc.Lookup("carol", strptr("CORP")); !ok {
		t.Error("NetBIOS first-label lookup for carol failed")
	}
	// DOMAIN\user embedded form.
	if _, ok := dc.Lookup("CORP\\alice", nil); !ok {
		t.Error("DOMAIN\\user lookup for alice failed")
	}
	// Unknown account.
	if _, ok := dc.Lookup("nobody", nil); ok {
		t.Error("lookup for nonexistent account should fail")
	}
}

func TestMetaAfterData(t *testing.T) {
	// meta comes AFTER data, and the filename carries no type hint: the decoder must buffer and replay.
	doc := `{"data":[
	  {"ObjectIdentifier":"S-1-5-21-9-1001","Properties":{"samaccountname":"dave","domain":"CORP.LOCAL","enabled":true}}
	],"meta":{"type":"users","count":1,"version":6}}`
	c := NewCollector(nil, DefaultLimits())
	if err := ParseInput(strings.NewReader(doc), "unknown-name", c); err != nil {
		t.Fatalf("ParseInput: %v", err)
	}
	dc := c.Resolve()
	if _, ok := dc.Lookup("dave", nil); !ok {
		t.Fatal("dave not parsed when meta followed data")
	}
}

func TestZipIngest(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range map[string]string{
		"users.json":   usersDoc,
		"groups.json":  groupsDoc,
		"domains.json": domainsDoc,
	} {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}

	c := NewCollector(nil, DefaultLimits())
	if err := ParseInput(bytes.NewReader(buf.Bytes()), "BloodHound.zip", c); err != nil {
		t.Fatalf("ParseInput(zip): %v", err)
	}
	dc := c.Resolve()
	if alice, ok := dc.Lookup("alice", nil); !ok || !alice.EffectiveDomainAdmin {
		t.Fatalf("zip ingest did not resolve alice as DA (ok=%v)", ok)
	}
}

func TestInScopeFiltering(t *testing.T) {
	// Only bob is in scope; the derived context must retain facts for bob only.
	c := NewCollector(nil, DefaultLimits())
	for i, d := range []string{usersDoc, groupsDoc} {
		if err := ParseInput(strings.NewReader(d), fmt.Sprintf("d%d.json", i), c); err != nil {
			t.Fatalf("parse: %v", err)
		}
	}
	scope := map[string]struct{}{}
	for _, k := range ScopeKeys("CORP.LOCAL", "bob") {
		scope[k] = struct{}{}
	}
	c.SetInScope(scope)
	dc := c.Resolve()
	if len(dc.Accounts) != 1 {
		t.Fatalf("expected only bob in scope, got %d accounts", len(dc.Accounts))
	}
	if _, ok := dc.Lookup("bob", nil); !ok {
		t.Error("bob should be in scope")
	}
	if _, ok := dc.Lookup("alice", nil); ok {
		t.Error("alice should be out of scope")
	}
	// Domain totals are dump-wide and must still count all users.
	if dc.Domains["corp.local"].Users != 3 {
		t.Errorf("domain totals should be dump-wide (3 users), got %d", dc.Domains["corp.local"].Users)
	}
}

func TestBuiltinSIDPrefixHandling(t *testing.T) {
	// BloodHound (esp. CE) prefixes BUILTIN local-group SIDs with the domain, e.g.
	// "PHANTOM.CORP-S-1-5-32-544". Privilege detection must handle both forms via the trailing RID.
	cases := []struct {
		sid  string
		priv bool
		da   bool
	}{
		{"S-1-5-32-544", true, true},                                    // bare BUILTIN Administrators
		{"PHANTOM.CORP-S-1-5-32-544", true, true},                       // domain-prefixed BUILTIN Administrators
		{"PHANTOM.CORP-S-1-5-32-550", true, false},                      // Print Operators (privileged, not DA)
		{"S-1-5-21-2697957641-2271029196-387917394-512", true, true},    // Domain Admins
		{"S-1-5-21-2697957641-2271029196-387917394-519", true, true},    // Enterprise Admins
		{"S-1-5-21-2697957641-2271029196-387917394-1104", false, false}, // regular principal
	}
	for _, c := range cases {
		if got := isPrivilegedSID(c.sid); got != c.priv {
			t.Errorf("isPrivilegedSID(%q) = %v, want %v", c.sid, got, c.priv)
		}
		if got := isDomainAdminSID(c.sid); got != c.da {
			t.Errorf("isDomainAdminSID(%q) = %v, want %v", c.sid, got, c.da)
		}
	}
}

func TestMachineAccountDollarMatching(t *testing.T) {
	// gMSA / machine accounts carry a trailing '$' in AD. A recovered credential may or may not keep
	// it, so the account must resolve in scope AND via Lookup regardless of the '$'. Regression for the
	// bug where the derived context keyed "…\svc$" but lookups stripped to "…\svc" and never matched.
	usersDoc := `{"meta":{"type":"users","count":1,"version":6},"data":[
	  {"ObjectIdentifier":"S-1-5-21-7-1050","Properties":{"samaccountname":"gMSA_SVC$","name":"GMSA_SVC$@CORP.LOCAL","domain":"CORP.LOCAL","enabled":true}}]}`
	groupsDoc := `{"meta":{"type":"groups","count":1,"version":6},"data":[
	  {"ObjectIdentifier":"S-1-5-21-7-512","Properties":{"name":"DOMAIN ADMINS@CORP.LOCAL"},"Members":[
	    {"ObjectIdentifier":"S-1-5-21-7-1050","ObjectType":"User"}]}]}`

	c := NewCollector(nil, DefaultLimits())
	for i, d := range []string{usersDoc, groupsDoc} {
		if err := ParseInput(strings.NewReader(d), fmt.Sprintf("d%d.json", i), c); err != nil {
			t.Fatalf("parse: %v", err)
		}
	}
	// Seed in-scope from a hashlist that recovered "gMSA_SVC$" (with the '$'), exactly as the handler does.
	scope := map[string]struct{}{}
	for _, k := range ScopeKeys("CORP.LOCAL", "gMSA_SVC$") {
		scope[k] = struct{}{}
	}
	c.SetInScope(scope)
	dc := c.Resolve()

	if len(dc.Accounts) != 1 {
		t.Fatalf("gMSA account must be in scope; got %d accounts", len(dc.Accounts))
	}
	cases := []struct {
		user string
		dom  *string
	}{
		{"gMSA_SVC$", strptr("CORP.LOCAL")}, // with $, FQDN
		{"gMSA_SVC", strptr("CORP.LOCAL")},  // without $
		{"gMSA_SVC$", strptr("CORP")},       // NetBIOS domain
		{"gMSA_SVC$", nil},                  // sam-only fallback
	}
	for _, tc := range cases {
		f, ok := dc.Lookup(tc.user, tc.dom)
		if !ok {
			t.Errorf("Lookup(%q, %v) failed for gMSA account", tc.user, tc.dom)
			continue
		}
		if !f.EffectiveDomainAdmin {
			t.Errorf("Lookup(%q, %v) should resolve as effective Domain Admin", tc.user, tc.dom)
		}
	}
}

func TestDuplicateSIDDoesNotDropEnabledAccount(t *testing.T) {
	// Malformed/synthetic dumps can carry the SAME ObjectIdentifier on two DIFFERENT accounts
	// (real AD never reuses a live RID). The graph is SID-keyed, so a naive last-write-wins lets a
	// DISABLED namesake clobber an ENABLED, privileged account and silently drop it from the report.
	// Regression: the ENABLED account must survive the collision regardless of file order, and it must
	// keep the group memberships recorded against the shared SID.
	const sharedSID = "S-1-5-21-8-2186"
	enabledUser := `{"ObjectIdentifier":"` + sharedSID + `","Properties":{"samaccountname":"gMSA_SVC$","name":"GMSA_SVC$@CORP.LOCAL","domain":"CORP.LOCAL","enabled":true}}`
	disabledUser := `{"ObjectIdentifier":"` + sharedSID + `","Properties":{"samaccountname":"tombstone","name":"TOMBSTONE@CORP.LOCAL","domain":"CORP.LOCAL","enabled":false}}`
	groupsDoc := `{"meta":{"type":"groups","count":1,"version":6},"data":[
	  {"ObjectIdentifier":"S-1-5-21-8-512","Properties":{"name":"DOMAIN ADMINS@CORP.LOCAL"},"Members":[
	    {"ObjectIdentifier":"` + sharedSID + `","ObjectType":"User"}]}]}`

	// Both orderings must yield the enabled, privileged gMSA account.
	orders := map[string]string{
		"enabled-first": `{"meta":{"type":"users","count":2,"version":6},"data":[` + enabledUser + `,` + disabledUser + `]}`,
		"disabled-first": `{"meta":{"type":"users","count":2,"version":6},"data":[` + disabledUser + `,` + enabledUser + `]}`,
	}
	for name, usersDoc := range orders {
		t.Run(name, func(t *testing.T) {
			c := NewCollector(nil, DefaultLimits())
			for i, d := range []string{usersDoc, groupsDoc} {
				if err := ParseInput(strings.NewReader(d), fmt.Sprintf("d%d.json", i), c); err != nil {
					t.Fatalf("parse: %v", err)
				}
			}
			scope := map[string]struct{}{}
			for _, k := range ScopeKeys("CORP.LOCAL", "gMSA_SVC$") {
				scope[k] = struct{}{}
			}
			c.SetInScope(scope)
			dc := c.Resolve()

			f, ok := dc.Lookup("gMSA_SVC$", strptr("CORP.LOCAL"))
			if !ok {
				t.Fatal("enabled gMSA account was dropped by the disabled SID-collision namesake")
			}
			if !f.Enabled {
				t.Error("survivor should be the ENABLED record")
			}
			if !f.EffectiveDomainAdmin {
				t.Error("survivor should retain the Domain Admin membership recorded on the shared SID")
			}
		})
	}
}

func TestZipBombGuard(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxTotalDecompressed = 1024 // 1 KiB total budget
	c := NewCollector(nil, lim)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("users.json")
	// A document larger than the total-decompressed budget.
	big := `{"meta":{"type":"users","count":1,"version":5},"data":[` +
		strings.Repeat(`{"ObjectIdentifier":"S-1-5-21-1-1","Properties":{"samaccountname":"x"}},`, 200) +
		`{"ObjectIdentifier":"S-1-5-21-1-2","Properties":{"samaccountname":"y"}}]}`
	f.Write([]byte(big))
	zw.Close()

	err := ParseInput(bytes.NewReader(buf.Bytes()), "bomb.zip", c)
	if err != ErrTooLarge {
		t.Fatalf("expected ErrTooLarge for oversized zip entry, got %v", err)
	}
}
