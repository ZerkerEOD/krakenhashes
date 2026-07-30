package pdf

import (
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
)

// TestBuildExternalAnalyticsStripsBloodhoundAccounts verifies that the external (redacted) report
// keeps every aggregate BloodHound count/percentage but drops all per-account identity leaves, and
// that the caller's input struct is not mutated.
func TestBuildExternalAnalyticsStripsBloodhoundAccounts(t *testing.T) {
	in := &models.AnalyticsData{
		ADPrivilege: &models.ADPrivilegeStats{
			CrackedPrivileged:        3,
			CrackedEffectiveDA:       1,
			PercentPrivilegedCracked: 42.5,
			Accounts: []models.CompromisedAccount{
				{Username: "admin", Domain: "corp.local", SID: "S-1-5-21-1-1001"},
			},
		},
		PathToDA: &models.PathToDAStats{
			CrackedWithPath: 2,
			ShortestHops:    3,
			Accounts: []models.PathAccount{
				{CompromisedAccount: models.CompromisedAccount{Username: "svc"}, Hops: 3},
			},
		},
		DCSyncCracked: &models.DCSyncCrackStats{
			Cracked:  1,
			Accounts: []models.CompromisedAccount{{Username: "repl"}},
		},
		LocalAdminBlast: &models.LocalAdminBlastStats{
			CrackedWithLocalAdmin: 1,
			TopAccounts: []models.BlastAccount{
				{CompromisedAccount: models.CompromisedAccount{Username: "wsadmin"}, ComputerCount: 12},
			},
		},
		KerberoastCracked: &models.RoastableCrackStats{
			Cracked:  1,
			Accounts: []models.CompromisedAccount{{Username: "sql"}},
		},
	}

	out := BuildExternalAnalytics(in)

	// Aggregates preserved.
	if out.ADPrivilege == nil || out.ADPrivilege.CrackedEffectiveDA != 1 || out.ADPrivilege.PercentPrivilegedCracked != 42.5 {
		t.Fatalf("privilege aggregates not preserved: %+v", out.ADPrivilege)
	}
	if out.PathToDA == nil || out.PathToDA.CrackedWithPath != 2 || out.PathToDA.ShortestHops != 3 {
		t.Fatalf("path aggregates not preserved: %+v", out.PathToDA)
	}
	if out.LocalAdminBlast == nil || out.LocalAdminBlast.CrackedWithLocalAdmin != 1 {
		t.Fatal("local-admin aggregates not preserved")
	}

	// Identity leaves stripped.
	if out.ADPrivilege.Accounts != nil {
		t.Error("ADPrivilege.Accounts should be nil in external report")
	}
	if out.PathToDA.Accounts != nil {
		t.Error("PathToDA.Accounts should be nil in external report")
	}
	if out.DCSyncCracked.Accounts != nil {
		t.Error("DCSyncCracked.Accounts should be nil in external report")
	}
	if out.LocalAdminBlast.TopAccounts != nil {
		t.Error("LocalAdminBlast.TopAccounts should be nil in external report")
	}
	if out.KerberoastCracked.Accounts != nil {
		t.Error("KerberoastCracked.Accounts should be nil in external report")
	}

	// Caller's input must not be mutated (deep-copy contract).
	if in.ADPrivilege.Accounts == nil {
		t.Error("BuildExternalAnalytics must not mutate the caller's input")
	}
}
