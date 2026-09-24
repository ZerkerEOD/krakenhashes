package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
)

/*
 * A cloud claim voucher is a REGISTRATION CREDENTIAL. Provisioning mints one
 * per candidate offer, before the provider is called, for a machine that may
 * never exist — and until these paths existed nothing ever removed one. The
 * reference deployment carried 695, of which 673 belonged to instances that
 * had failed to launch and 4 were still inside their TTL and redeemable.
 *
 * These are DB-backed because both properties being guarded are properties of
 * the SQL predicate, not of Go control flow.
 */

func mintVoucher(t *testing.T, database *db.DB, code string, instanceID *uuid.UUID,
	expiresAt *time.Time, usedByAgent *int) {
	t.Helper()
	var exp sql.NullTime
	if expiresAt != nil {
		exp = sql.NullTime{Time: *expiresAt, Valid: true}
	}
	var usedAt sql.NullTime
	var agent sql.NullInt64
	if usedByAgent != nil {
		usedAt = sql.NullTime{Time: time.Now().Add(-time.Hour), Valid: true}
		agent = sql.NullInt64{Int64: int64(*usedByAgent), Valid: true}
	}
	_, err := database.ExecContext(context.Background(), `
		INSERT INTO claim_vouchers
			(code, created_by_id, created_at, updated_at, is_continuous, is_active,
			 used_at, used_by_agent_id, expires_at, cloud_instance_id)
		VALUES ($1, $2, NOW(), NOW(), false, true, $3, $4, $5, $6)`,
		code, models.SystemUserID, usedAt, agent, exp, instanceID)
	if err != nil {
		t.Fatalf("mint voucher %s: %v", code, err)
	}
}

func voucherActive(t *testing.T, database *db.DB, code string) bool {
	t.Helper()
	var active bool
	if err := database.QueryRowContext(context.Background(),
		`SELECT is_active FROM claim_vouchers WHERE code = $1`, code).Scan(&active); err != nil {
		t.Fatalf("read voucher %s: %v", code, err)
	}
	return active
}

func voucherExists(t *testing.T, database *db.DB, code string) bool {
	t.Helper()
	var n int
	if err := database.QueryRowContext(context.Background(),
		`SELECT count(*) FROM claim_vouchers WHERE code = $1`, code).Scan(&n); err != nil {
		t.Fatalf("count voucher %s: %v", code, err)
	}
	return n > 0
}

func voucherFixture(t *testing.T) (*db.DB, *ClaimVoucherRepository, uuid.UUID, int) {
	t.Helper()
	database := testutil.SetupTestDB(t)
	owner := testutil.CreateTestUser(t, database, "vo-"+uuid.NewString()[:8],
		"vo-"+uuid.NewString()[:8]+"@test.local", "pw", "admin")
	cfg := testutil.CreateTestCloudProviderConfig(t, database, "mock", "mock-"+uuid.NewString()[:8],
		testutil.ProviderConfigOpts{Enabled: true})
	instanceID, _ := testutil.CreateTestCloudInstance(t, database, cfg, testutil.InstanceOpts{})
	agentID := testutil.CreateTestAgent(t, database, owner.ID, &instanceID)
	return database, NewClaimVoucherRepository(database), instanceID, agentID
}

/*
 * The security fix: a failed launch must not leave a working claim code.
 */
func TestDeactivateForCloudInstanceKillsUnredeemedCodes(t *testing.T) {
	database, repo, instanceID, _ := voucherFixture(t)
	future := time.Now().Add(time.Hour)
	mintVoucher(t, database, "LIVE-"+uuid.NewString()[:8], &instanceID, &future, nil)

	n, err := repo.DeactivateForCloudInstance(context.Background(), instanceID)
	if err != nil {
		t.Fatalf("DeactivateForCloudInstance: %v", err)
	}
	if n != 1 {
		t.Fatalf("deactivated %d vouchers, want 1.\n"+
			"An unredeemed voucher for a failed instance is a registration credential "+
			"nobody can legitimately use, live until its TTL expires.", n)
	}
}

/*
 * The line that must not move: a REDEEMED voucher is the audit link between an
 * agent and the credential it joined with. used_at already blocks reuse, so
 * flipping is_active on it destroys a record and buys nothing.
 */
func TestDeactivateLeavesRedeemedVouchersAlone(t *testing.T) {
	database, repo, instanceID, agentID := voucherFixture(t)
	future := time.Now().Add(time.Hour)
	redeemed := "USED-" + uuid.NewString()[:8]
	mintVoucher(t, database, redeemed, &instanceID, &future, &agentID)

	if _, err := repo.DeactivateForCloudInstance(context.Background(), instanceID); err != nil {
		t.Fatalf("DeactivateForCloudInstance: %v", err)
	}
	if !voucherActive(t, database, redeemed) {
		t.Error("a REDEEMED voucher was deactivated.\n" +
			"used_at already prevents reuse. The row's remaining job is to record " +
			"which agent joined with which credential, and flipping is_active " +
			"discards that for no security gain.")
	}
}

// Deactivation must not touch another instance's credentials.
func TestDeactivateIsScopedToOneInstance(t *testing.T) {
	database, repo, instanceID, _ := voucherFixture(t)
	cfg := testutil.CreateTestCloudProviderConfig(t, database, "mock", "other-"+uuid.NewString()[:8],
		testutil.ProviderConfigOpts{Enabled: true})
	otherID, _ := testutil.CreateTestCloudInstance(t, database, cfg, testutil.InstanceOpts{})

	future := time.Now().Add(time.Hour)
	mine := "MINE-" + uuid.NewString()[:8]
	theirs := "THRS-" + uuid.NewString()[:8]
	mintVoucher(t, database, mine, &instanceID, &future, nil)
	mintVoucher(t, database, theirs, &otherID, &future, nil)

	if _, err := repo.DeactivateForCloudInstance(context.Background(), instanceID); err != nil {
		t.Fatalf("DeactivateForCloudInstance: %v", err)
	}
	if voucherActive(t, database, mine) {
		t.Error("the target instance's voucher survived")
	}
	if !voucherActive(t, database, theirs) {
		t.Error("another instance's voucher was deactivated; a failing launch would " +
			"strand a machine that is coming up fine")
	}
}

/*
 * The sweep must NOT depend on is_active.
 *
 * Deactivation and expiry are independent lifecycles: a voucher killed on
 * failure is exactly the kind this sweep exists to remove, so requiring
 * is_active = true would skip every one of them — which is most of them.
 */
func TestPurgeRemovesDeactivatedRowsToo(t *testing.T) {
	database, repo, instanceID, _ := voucherFixture(t)
	past := time.Now().Add(-90 * 24 * time.Hour)
	code := "DEAD-" + uuid.NewString()[:8]
	mintVoucher(t, database, code, &instanceID, &past, nil)
	if _, err := repo.DeactivateForCloudInstance(context.Background(), instanceID); err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	if _, err := repo.PurgeExpired(context.Background(), time.Now().AddDate(0, 0, -30)); err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if voucherExists(t, database, code) {
		t.Error("an expired, deactivated voucher survived the sweep.\n" +
			"Deactivation moves a row OUT of idx_claim_vouchers_expires_at (a partial " +
			"index on is_active = true), so a sweep predicated on is_active would " +
			"never collect the rows the failure path produces — which is nearly all " +
			"of them.")
	}
}

func TestPurgeSpares(t *testing.T) {
	database, repo, instanceID, agentID := voucherFixture(t)
	past := time.Now().Add(-90 * 24 * time.Hour)
	future := time.Now().Add(time.Hour)

	redeemed := "KEEP1-" + uuid.NewString()[:8]
	unexpired := "KEEP2-" + uuid.NewString()[:8]
	noExpiry := "KEEP3-" + uuid.NewString()[:8]
	stale := "GONE-" + uuid.NewString()[:8]

	mintVoucher(t, database, redeemed, &instanceID, &past, &agentID) // old but redeemed
	mintVoucher(t, database, unexpired, &instanceID, &future, nil)   // still in date
	mintVoucher(t, database, noExpiry, &instanceID, nil, nil)        // continuous-style
	mintVoucher(t, database, stale, &instanceID, &past, nil)         // the only target

	if _, err := repo.PurgeExpired(context.Background(), time.Now().AddDate(0, 0, -30)); err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}

	for _, c := range []struct{ code, why string }{
		{redeemed, "REDEEMED vouchers are the audit link between an agent and its credential, whatever their age"},
		{unexpired, "it has not expired; deleting it would revoke a credential a booting machine still needs"},
		{noExpiry, "a NULL expires_at never expires and must never be swept"},
	} {
		if !voucherExists(t, database, c.code) {
			t.Errorf("the sweep deleted %s — %s", c.code, c.why)
		}
	}
	if voucherExists(t, database, stale) {
		t.Error("the sweep did not remove an expired, never-redeemed voucher")
	}
}
