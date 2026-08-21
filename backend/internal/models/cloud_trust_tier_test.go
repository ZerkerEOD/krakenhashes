package models

import "testing"

/*
 * TestRequiresThirdPartyAck is an EXHAUSTIVE table over every provider kind, and
 * it is deliberately exhaustive rather than spot-checking the interesting ones.
 *
 * The failure this guards is a one-character edit. Someone adding a provider
 * extends the condition with `|| p == CloudProviderNewThing`, or "tidies" it
 * into a set membership over all cloud kinds, and AWS or RunPod Secure quietly
 * acquires a data-exposure acknowledgement gate. Nothing breaks loudly: an
 * admin just finds they cannot enable AWS without clicking through a warning
 * about hardware they own. The cost is not the click — it is that operators
 * learn the warning is noise, and the one place it is real stops being read.
 *
 * The reverse direction is worse and just as cheap to introduce: a peer kind
 * dropping out of this predicate silently removes the consent chain for
 * hardware whose owner has root over the container.
 */
func TestRequiresThirdPartyAck(t *testing.T) {
	cases := []struct {
		provider CloudProvider
		want     bool
		why      string
	}{
		{CloudProviderVastAI, true,
			"individually-owned consumer machines; the owner has root over the container"},
		{CloudProviderRunPodCommunity, true,
			"peer-operated hosts, and RunPod's SOC 2 / ISO 27001 / PCI DSS attestations cover only Secure Cloud"},

		{CloudProviderAWS, false,
			"the operator's own AWS account, their own IAM, AWS datacentres"},
		{CloudProviderRunPod, false,
			"RunPod's own SOC 2 Type II datacentres, single-tenant per host"},
		{CloudProviderMock, false,
			"local agent --test-mode processes; no hardware is rented at all"},
	}

	for _, tc := range cases {
		t.Run(string(tc.provider), func(t *testing.T) {
			if got := tc.provider.RequiresThirdPartyAck(); got != tc.want {
				t.Errorf("%s.RequiresThirdPartyAck() = %v, want %v — %s",
					tc.provider, got, tc.want, tc.why)
			}
		})
	}
}

/*
 * TestTheTwoRunPodTiersAreDistinctKinds pins the split itself.
 *
 * Collapsing them into one kind plus a settings flag is the modelling RunPod's
 * own API suggests, and it would put peer hardware inside the allowlist of
 * every client who permitted "runpod" — without any of them having agreed to
 * it, and with no error anywhere to notice.
 */
func TestTheTwoRunPodTiersAreDistinctKinds(t *testing.T) {
	if CloudProviderRunPod == CloudProviderRunPodCommunity {
		t.Fatal("the Secure and Community tiers must be separate provider kinds; " +
			"they sit on opposite sides of the third-party trust boundary")
	}
	if CloudProviderRunPod.RequiresThirdPartyAck() == CloudProviderRunPodCommunity.RequiresThirdPartyAck() {
		t.Error("the two RunPod tiers must land on OPPOSITE sides of the acknowledgement gate")
	}
}
