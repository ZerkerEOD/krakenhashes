package models

import "testing"

/*
 * TestEveryProviderDeclaresMaturity walks AllCloudProviders rather than a
 * hand-written table, which is the one place in this file that difference
 * matters: a hand-written table can be added to and a provider forgotten, and
 * forgetting here means an untested adapter ships looking exactly like a proven
 * one.
 *
 * MaturityUnknown is representable precisely so this test can catch it. The
 * tempting shape — a switch with `default: return MaturityStable` — makes the
 * omission unrepresentable and therefore invisible, and defaults it in the
 * direction that tells an operator to trust something nobody has verified.
 */
func TestEveryProviderDeclaresMaturity(t *testing.T) {
	if len(AllCloudProviders) == 0 {
		t.Fatal("AllCloudProviders is empty; this test would pass vacuously")
	}
	for _, p := range AllCloudProviders {
		if p.Maturity() == MaturityUnknown {
			t.Errorf("%s declares no maturity.\n"+
				"Add it to providerMaturity in cloud.go. Stable means it has been driven "+
				"end to end against the real provider INCLUDING teardown and the budget "+
				"refund — not that the code looks finished. If it has not been paid for, "+
				"it is experimental.", p)
		}
	}
}

/*
 * TestOnlyProvenProvidersAreStable is the counterweight. The test above only
 * asks that an answer exists; this one pins what the answers are, so promoting
 * a provider to stable is a deliberate edit to a test that names the evidence
 * rather than a side effect of touching a map.
 */
func TestOnlyProvenProvidersAreStable(t *testing.T) {
	cases := []struct {
		provider CloudProvider
		want     ProviderMaturity
		why      string
	}{
		{CloudProviderAWS, MaturityStable,
			"driven end to end on a real account: 141s commissioning, 3/3 cracked, " +
				"released at 3.8m, 54c of 57c reserved refunded"},
		{CloudProviderMock, MaturityStable,
			"spends nothing and rents no hardware; there is no operator risk to warn about"},

		{CloudProviderVastAI, MaturityExperimental,
			"fully implemented and never once paid for; the rental lifecycle is unproven " +
				"against the live marketplace"},
		{CloudProviderRunPod, MaturityExperimental,
			"written against the documented API with no account to verify it on"},
		{CloudProviderRunPodCommunity, MaturityExperimental,
			"as RunPod Secure, and teardown is reaper-only: no per-pod scoped credential " +
				"exists, so nothing inside the pod can stop it billing"},
	}

	if len(cases) != len(AllCloudProviders) {
		t.Fatalf("this table covers %d providers but AllCloudProviders has %d; "+
			"a provider was added without deciding what its maturity claim should be",
			len(cases), len(AllCloudProviders))
	}

	for _, tc := range cases {
		t.Run(string(tc.provider), func(t *testing.T) {
			if got := tc.provider.Maturity(); got != tc.want {
				t.Errorf("%s.Maturity() = %q, want %q — %s.\n"+
					"Promoting a provider to stable removes the warning that tells "+
					"operators to monitor their jobs. Do it only after a real paid run "+
					"reaches clean teardown, and say so here.", tc.provider, got, tc.want, tc.why)
			}
			if tc.provider.IsExperimental() != (tc.want == MaturityExperimental) {
				t.Errorf("%s.IsExperimental() disagrees with Maturity(); the UI branches "+
					"on IsExperimental", tc.provider)
			}
		})
	}
}

/*
 * TestMaturityIsIndependentOfTrustTier keeps two questions from collapsing into
 * one. "Have we proven this works?" and "does this put hash material on
 * hardware you do not control?" are different, and the pairs prove it:
 * RunPod Secure is experimental but not third-party, Mock is neither, Vast is
 * both.
 *
 * Collapsing them would mean either an untested first-party provider silently
 * acquiring a data-exposure consent gate, or a proven peer provider losing one.
 */
func TestMaturityIsIndependentOfTrustTier(t *testing.T) {
	if !CloudProviderRunPod.IsExperimental() || CloudProviderRunPod.RequiresThirdPartyAck() {
		t.Error("RunPod Secure must be experimental AND not third-party; it is the pair " +
			"that proves the two predicates are independent")
	}
	if CloudProviderMock.IsExperimental() || CloudProviderMock.RequiresThirdPartyAck() {
		t.Error("Mock must be neither experimental nor third-party")
	}
	if !CloudProviderVastAI.IsExperimental() || !CloudProviderVastAI.RequiresThirdPartyAck() {
		t.Error("Vast.ai must be both experimental and third-party")
	}
}
