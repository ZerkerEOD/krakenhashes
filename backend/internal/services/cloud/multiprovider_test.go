package cloud

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

/*
 * Guards for the two multi-provider gaps that only show up once more than one
 * provider is configured — which is exactly the configuration nobody tests
 * with until it is in production.
 */

/*
 * A provider-local failure must not take the other providers down with it.
 *
 * rankedCandidates goes out of its way to isolate a dead provider one step
 * earlier ("renting nothing because Vast.ai is having an outage, while AWS sits
 * there ready, is a worse answer"), and the launch walk used to undo that one
 * step later: any error other than ErrOfferUnavailable returned immediately,
 * including for candidates belonging to a completely different provider.
 */
func TestProviderLocalFailuresDoNotAbortOtherProvidersCandidates(t *testing.T) {
	err := fmt.Errorf("%w: VPN credential for Vast: %w", errProviderLocal,
		errors.New("reusable key expired"))

	if !errors.Is(err, errProviderLocal) {
		t.Fatal("the sentinel does not survive wrapping, so the walk cannot recognise it")
	}
	if errors.Is(err, ErrOfferUnavailable) {
		t.Error("a provider-local failure must not also read as ErrOfferUnavailable: " +
			"that sentinel releases the budget reservation")
	}
	// The operator has to be able to see WHICH provider failed and why, since
	// the walk will otherwise quietly succeed elsewhere and they will never
	// learn their Vast key expired.
	if got := err.Error(); !contains(got, "Vast") || !contains(got, "reusable key expired") {
		t.Errorf("the wrapped error lost its context: %q", got)
	}
}

/*
 * The high bar for the sentinel, pinned.
 *
 * Anything ambiguous — anything that could have left an instance billing — must
 * keep aborting the walk. The cheap mistake is to reach for this sentinel to
 * make retries "more robust" and thereby mark a provider timeout as safe.
 */
func TestAmbiguousErrorsAreNotProviderLocal(t *testing.T) {
	ambiguous := []error{
		errors.New("record instance: connection reset"),
		errors.New("create voucher: context deadline exceeded"),
		fmt.Errorf("launch g4dn.xlarge@us-east-2b: %w", errors.New("i/o timeout")),
	}
	for _, err := range ambiguous {
		if errors.Is(err, errProviderLocal) {
			t.Errorf("%v is marked provider-local.\n"+
				"It happens at or after the instance row is written, so an instance may "+
				"exist and be billing. Continuing the walk would rent a SECOND one while "+
				"the first is unaccounted for.", err)
		}
	}
}

/*
 * The instance cap must be enforced on the manual path too.
 *
 * It used to live only in the autoscaler, so the admin "Provision now" button
 * walked straight past it. The spend cap was the only thing left standing, and
 * a ceiling denominated in dollars does not stop an operator having ten
 * instances — it only stops them having them for long.
 */
func TestGlobalInstanceCapIsEnforcedOnTheManualProvisionPath(t *testing.T) {
	count := func(n int) func() (int, error) {
		return func() (int, error) { return n, nil }
	}

	if err := enforceInstanceCap(0, count(99)); err != nil {
		t.Errorf("a cap of 0 means unlimited and must not refuse: %v", err)
	}
	if err := enforceInstanceCap(3, count(2)); err != nil {
		t.Errorf("2 live against a cap of 3 should be allowed: %v", err)
	}

	err := enforceInstanceCap(3, count(3))
	if err == nil {
		t.Fatal("3 live against a cap of 3 was allowed.\n" +
			"ProvisionForJob is the manual \"Provision now\" path and does not go through " +
			"the autoscaler, so this is the only place the cap is enforced for it. " +
			"Without it an operator who set \"never more than three\" can click their way " +
			"to a fourth, and the spend cap will not stop them: a ceiling in dollars does " +
			"not limit how MANY instances run, only how long.")
	}
	// The operator has to be able to act on it, so the message names both
	// numbers and the setting to change.
	for _, want := range []string{"3 of 3", SettingGlobalInstanceCap} {
		if !contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %q", want, err)
		}
	}
}

/*
 * A cap that cannot be evaluated must refuse, not pass.
 *
 * This is the same shape as the autoscaler's nil-counter guard and exists for
 * the same reason: the two halves are wired up separately, so skipping the
 * check when the counter is missing makes the omission invisible.
 */
func TestInstanceCapFailsClosedWhenItCannotBeChecked(t *testing.T) {
	if err := enforceInstanceCap(3, nil); err == nil {
		t.Error("a configured cap with no counter was treated as unlimited.\n" +
			"The operator set a limit and would get none, with nothing in the logs, the " +
			"settings screen or the tests to say the limit is not real.")
	}
	// But an unset cap with no counter is genuinely unlimited, and must not
	// turn into a spurious refusal that blocks every provision.
	if err := enforceInstanceCap(0, nil); err != nil {
		t.Errorf("an unset cap must not refuse just because no counter exists: %v", err)
	}

	boom := func() (int, error) { return 0, errors.New("database is down") }
	if err := enforceInstanceCap(3, boom); err == nil {
		t.Error("an uncountable fleet was allowed to provision; renting now could be the " +
			"launch that breaches the cap")
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
