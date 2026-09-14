package cloud

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
)

/*
 * A cloud claim voucher is a REGISTRATION CREDENTIAL, minted before the
 * provider is called, for a machine that may never exist.
 *
 * Measured on a live deployment before these tests existed: 673 unredeemed
 * vouchers belonging to failed instances, every one still active, four of them
 * still inside their TTL and therefore redeemable. All four came from a single
 * provisioning run's capacity refusals — because the launch path mints one per
 * CANDIDATE OFFER, and widening AWS from one zone to three took the candidate
 * list from 3 to 9.
 */

// fakeVoucherIssuer records what the lifecycle asked it to do.
type fakeVoucherIssuer struct {
	mu          sync.Mutex
	minted      []uuid.UUID
	deactivated []uuid.UUID
	mintErr     error
	killErr     error
}

func (f *fakeVoucherIssuer) CreateCloudVoucher(_ context.Context, _ time.Duration, id uuid.UUID) (*models.ClaimVoucher, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mintErr != nil {
		return nil, f.mintErr
	}
	f.minted = append(f.minted, id)
	return &models.ClaimVoucher{Code: "TESTCODE", CloudInstanceID: &id, IsActive: true}, nil
}

func (f *fakeVoucherIssuer) DeactivateForCloudInstance(_ context.Context, id uuid.UUID) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.killErr != nil {
		return 0, f.killErr
	}
	f.deactivated = append(f.deactivated, id)
	return 1, nil
}

func (f *fakeVoucherIssuer) killedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.deactivated)
}

/*
 * THE TEST THAT MATTERS MOST, and the one whose absence would be expensive.
 *
 * There are three post-mint failure paths in attemptLaunch and only two may
 * deactivate. The third — an ambiguous launch error — leaves the instance in
 * `requested` precisely BECAUSE a timeout may have created a running, billing
 * machine. That machine's agent still has to redeem this code to register.
 *
 * Killing the voucher there converts a recoverable launch into guaranteed
 * waste: the instance bills for its full TTL, never registers, never takes a
 * chunk, and the reaper eventually destroys it having achieved nothing. The
 * failure is invisible in the moment and looks like a provider problem.
 */
func TestAmbiguousLaunchFailureMustNotKillTheVoucher(t *testing.T) {
	src, err := readServiceSource()
	if err != nil {
		t.Fatalf("read service.go: %v", err)
	}

	/*
	 * The ambiguous branch runs from the end of the ErrOfferUnavailable block
	 * to its own return. Anchored on the sentinel and on the state it sets,
	 * both of which are load-bearing and would not be renamed casually.
	 */
	amb := extractBetween(src,
		"offer no longer available",
		"return fmt.Errorf(\"launch: %w\", err)")
	if amb == "" {
		t.Fatal("could not isolate the ambiguous-failure branch; this guard is vacuous")
	}
	if !contains(amb, "models.CloudInstanceRequested") {
		t.Fatal("the isolated span is not the ambiguous branch; anchors have moved")
	}
	// Trim the preceding ErrOfferUnavailable branch, which legitimately kills.
	amb = amb[indexOf(amb, "models.CloudInstanceRequested"):]
	if contains(amb, "killVoucher") {
		t.Error("the AMBIGUOUS launch path kills the claim voucher.\n" +
			"It must not. That branch exists because a timeout may have created a " +
			"RUNNING instance — the row is deliberately left in 'requested' for the " +
			"reaper to reconcile by label. Killing the code strands a machine that is " +
			"already billing: it can never register, never takes a chunk, and bills its " +
			"full TTL before teardown. Only deactivate where the instance is KNOWN not " +
			"to exist.")
	}
}

/*
 * The converse: both definitive-failure paths must kill it. These are where the
 * 673 leaked credentials came from.
 */
func TestDefinitiveFailuresKillTheVoucher(t *testing.T) {
	src, err := readServiceSource()
	if err != nil {
		t.Fatalf("read service.go: %v", err)
	}

	cases := []struct {
		name, anchor, why string
	}{
		{"budget reservation failure", "budget reservation failed",
			"nothing was rented, so the credential can never be legitimately used"},
		{"offer vanished before launch", "offer no longer available",
			"the provider REJECTED the create — this is the path that leaked 673 credentials"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Look at the statements immediately following the state change.
			idx := indexOf(src, c.anchor)
			if idx < 0 {
				t.Fatalf("anchor %q not found; this guard is vacuous", c.anchor)
			}
			window := src[idx:minInt(idx+400, len(src))]
			if !contains(window, "killVoucher") {
				t.Errorf("%s does not deactivate the claim voucher.\n"+
					"%s. Until it does, the code stays redeemable until its TTL expires "+
					"— once per candidate offer, so up to nine per provision on a "+
					"three-zone AWS config.", c.name, c.why)
			}
		})
	}
}

/*
 * Teardown is the catch-all. The launch path only sees the failures it causes
 * itself; an instance that launches fine and then never registers, or simply
 * finishes, reaches its end in the reaper. Five unredeemed-but-terminated
 * vouchers on this deployment came from exactly that.
 */
func TestReaperFinalizeKillsTheVoucher(t *testing.T) {
	src, err := readSource("reaper.go")
	if err != nil {
		t.Fatalf("read reaper.go: %v", err)
	}
	fn := extractBetween(src, "func (r *Reaper) finalize(", "\nfunc ")
	if fn == "" {
		t.Fatal("could not isolate Reaper.finalize")
	}
	if !contains(fn, "DeactivateForCloudInstance") {
		t.Error("Reaper.finalize does not deactivate the instance's claim voucher.\n" +
			"This is the only place an instance that died AFTER a successful launch " +
			"gets cleaned up — a lapsed ready deadline, an idle drain, a finished job.")
	}
	if !contains(fn, "RetireAgentsForInstance") {
		t.Error("Reaper.finalize still retires by agent id.\n" +
			"`if inst.AgentID != nil` skips silently when the back-reference is null, " +
			"which is how three agents on this deployment outlived their terminated " +
			"instances. Key on cloud_instance_id instead.")
	}
}

// A failure to deactivate must never stop teardown: the instance still has to
// be destroyed, and the sweep removes the row later regardless.
func TestVoucherFailureDoesNotBlockTeardown(t *testing.T) {
	f := &fakeVoucherIssuer{killErr: errors.New("database is down")}
	s := &Service{vouchers: f}

	// Must not panic, must not propagate — killVoucher returns nothing.
	s.killVoucher(context.Background(), uuid.New(), "test")

	if f.killedCount() != 0 {
		t.Error("a failing deactivation should record nothing")
	}
}

func TestKillVoucherReportsWhatItDid(t *testing.T) {
	f := &fakeVoucherIssuer{}
	s := &Service{vouchers: f}
	id := uuid.New()
	s.killVoucher(context.Background(), id, "test")

	if f.killedCount() != 1 || f.deactivated[0] != id {
		t.Errorf("killVoucher did not deactivate the right instance: %v", f.deactivated)
	}
}
