package repository

import (
	"context"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests run against real Postgres because the behaviour under test is not
// expressible in Go alone: the two ON CONFLICT targets differ only in whether
// they can see a partial unique index, and the window round-trip is a question
// about how the driver decodes a TIME column.

/*
 * seedSystemDefaultRules restores the row the migration inserts.
 *
 * testutil.SetupTestDB truncates every table outside preservedTables and then
 * re-seeds only what testutil.SeedDefaults knows about — which covers
 * cloud_budget_policies but NOT cloud_provisioning_rules. Without this the
 * migration's system default is gone before the first assertion runs and every
 * GetRules fails with "the system default row is missing", which looks like a
 * repository bug rather than a fixture gap.
 *
 * Values are exactly the migration's, so these tests see production defaults:
 * 20260821090000_add_cloud_provisioning_rules.up.sql.
 */
func seedSystemDefaultRules(t *testing.T, database *db.DB) {
	t.Helper()

	_, err := database.Exec(`
		INSERT INTO cloud_provisioning_rules (
			client_id, min_job_priority, min_starvation_seconds,
			skip_if_finishing_within_seconds, max_spend_per_job_cents,
			provisioning_window_start, provisioning_window_end, provisioning_window_tz
		) VALUES (NULL, 0, 180, 900, 0, NULL, NULL, 'UTC')
		ON CONFLICT DO NOTHING`)
	if err != nil {
		t.Fatalf("Failed to seed the system default provisioning rules: %v", err)
	}
}

func newProvisioningRulesRepo(t *testing.T) (*CloudProvisioningRulesRepository, *db.DB) {
	t.Helper()
	database := testutil.SetupTestDB(t)
	seedSystemDefaultRules(t, database)
	return NewCloudProvisioningRulesRepository(database), database
}

// rulesClient creates a client to hang an override off. Cloud budget and
// allowlist are irrelevant here — these rules govern timing, not spend.
func rulesClient(t *testing.T, database *db.DB) uuid.UUID {
	t.Helper()
	return testutil.CreateTestClient(t, database, "rules-"+uuid.NewString()[:8],
		testutil.ClientCloudOpts{CloudEnabled: true})
}

func strPtrLocal(v string) *string { return &v }

// TestGetRules_NoOverrideReturnsSystemDefault: a client that has never been
// configured is subject to the seeded defaults, unchanged.
func TestGetRules_NoOverrideReturnsSystemDefault(t *testing.T) {
	repo, database := newProvisioningRulesRepo(t)
	ctx := context.Background()
	clientID := rulesClient(t, database)

	got, err := repo.GetRules(ctx, clientID)
	require.NoError(t, err)

	// The VALUES are the default row's, but the IDENTITY is the client's: this
	// is "the rules in force for client X", not the default row. See
	// TestGetRules_ResultCannotBeUpsertedOverTheSystemDefault for what a nil
	// here would let an admin do by accident.
	require.NotNil(t, got.ClientID, "the merged view must carry the client it describes")
	assert.Equal(t, clientID, *got.ClientID)
	assert.Equal(t, uuid.Nil, got.ID, "there is no stored override row, so there is no row id to hand out")

	require.NotNil(t, got.MinJobPriority)
	assert.Equal(t, 0, *got.MinJobPriority, "a priority floor must be opt-in; it is the setting that silently disables everything")
	require.NotNil(t, got.MinStarvationSeconds)
	assert.Equal(t, 180, *got.MinStarvationSeconds, "three ticks at the default 60s autoscaler interval")
	require.NotNil(t, got.SkipIfFinishingWithinSeconds)
	assert.Equal(t, 900, *got.SkipIfFinishingWithinSeconds)
	require.NotNil(t, got.MaxSpendPerJobCents)
	assert.Equal(t, int64(0), *got.MaxSpendPerJobCents)
	assert.Nil(t, got.ProvisioningWindowStart, "the default configures no window")
	assert.Nil(t, got.ProvisioningWindowEnd)
}

// TestGetRules_OverrideWinsAndUnsetFieldsInherit: the per-field merge. Two of
// the four rules are overridden; the other two must still come from the default.
func TestGetRules_OverrideWinsAndUnsetFieldsInherit(t *testing.T) {
	repo, database := newProvisioningRulesRepo(t)
	ctx := context.Background()
	clientID := rulesClient(t, database)

	require.NoError(t, repo.UpsertRules(ctx, &models.CloudProvisioningRules{
		ClientID:            &clientID,
		MinJobPriority:      testutil.IntPtr(50),
		MaxSpendPerJobCents: testutil.Int64Ptr(25000),
	}))

	got, err := repo.GetRules(ctx, clientID)
	require.NoError(t, err)

	require.NotNil(t, got.MinJobPriority)
	assert.Equal(t, 50, *got.MinJobPriority, "a field the override sets must win")
	require.NotNil(t, got.MaxSpendPerJobCents)
	assert.Equal(t, int64(25000), *got.MaxSpendPerJobCents)

	require.NotNil(t, got.MinStarvationSeconds)
	assert.Equal(t, 180, *got.MinStarvationSeconds, "NULL on an override means inherit, not 'unconfigured'")
	require.NotNil(t, got.SkipIfFinishingWithinSeconds)
	assert.Equal(t, 900, *got.SkipIfFinishingWithinSeconds, "NULL on an override means inherit, not 'unconfigured'")
}

/*
 * TestGetRules_OverrideOptsOutWithZero is the case the pointer types exist for.
 *
 * The default makes every client wait 600s of continuous starvation before paid
 * capacity is rented. A client overriding that to 0 means "rent on the first
 * starving tick" — an explicit opt-out, not an absent value.
 *
 * An implementation that treats zero as unset (`if override.X != 0`) inherits
 * 600 here, so the opt-out does nothing. That failure is invisible from the
 * table, indistinguishable from a UI bug, and surfaces only as capacity that
 * takes ten minutes to arrive for the one client configured to get it at once.
 */
func TestGetRules_OverrideOptsOutWithZero(t *testing.T) {
	repo, database := newProvisioningRulesRepo(t)
	ctx := context.Background()
	clientID := rulesClient(t, database)

	// Tighten the default first so 0 on the override is unambiguously a
	// deliberate opt-out from a non-zero inherited value.
	require.NoError(t, repo.UpsertRules(ctx, &models.CloudProvisioningRules{
		MinJobPriority:               testutil.IntPtr(10),
		MinStarvationSeconds:         testutil.IntPtr(600),
		SkipIfFinishingWithinSeconds: testutil.IntPtr(1800),
		MaxSpendPerJobCents:          testutil.Int64Ptr(500000),
		ProvisioningWindowTZ:         strPtrLocal("UTC"),
	}))

	require.NoError(t, repo.UpsertRules(ctx, &models.CloudProvisioningRules{
		ClientID:             &clientID,
		MinStarvationSeconds: testutil.IntPtr(0),
		MaxSpendPerJobCents:  testutil.Int64Ptr(0),
	}))

	got, err := repo.GetRules(ctx, clientID)
	require.NoError(t, err)

	require.NotNil(t, got.MinStarvationSeconds)
	assert.Equal(t, 0, *got.MinStarvationSeconds,
		"an explicit 0 is the rule's OFF value and must beat the inherited 600, never be read as unset")
	require.NotNil(t, got.MaxSpendPerJobCents)
	assert.Equal(t, int64(0), *got.MaxSpendPerJobCents,
		"0 means no per-job cap and must beat the inherited 500000")

	require.NotNil(t, got.MinJobPriority)
	assert.Equal(t, 10, *got.MinJobPriority, "fields the override did not touch still inherit")
	require.NotNil(t, got.SkipIfFinishingWithinSeconds)
	assert.Equal(t, 1800, *got.SkipIfFinishingWithinSeconds, "fields the override did not touch still inherit")
}

/*
 * TestGetRules_TightenedDefaultReachesOverriddenClient is the whole reason this
 * repository merges instead of reusing GetPolicy's ORDER BY ... LIMIT 1.
 *
 * The client overrides max_spend_per_job_cents and nothing else. An admin then
 * raises the starvation threshold on the system default. Under whole-row
 * resolution the client's row wins outright, its NULL min_starvation_seconds
 * reads as "not configured, constrains nothing", and the tightening never
 * reaches the account it was aimed at — which is precisely the account someone
 * had already singled out for special handling.
 */
func TestGetRules_TightenedDefaultReachesOverriddenClient(t *testing.T) {
	repo, database := newProvisioningRulesRepo(t)
	ctx := context.Background()
	clientID := rulesClient(t, database)

	require.NoError(t, repo.UpsertRules(ctx, &models.CloudProvisioningRules{
		ClientID:            &clientID,
		MaxSpendPerJobCents: testutil.Int64Ptr(50000),
	}))

	before, err := repo.GetRules(ctx, clientID)
	require.NoError(t, err)
	require.NotNil(t, before.MinStarvationSeconds)
	require.Equal(t, 180, *before.MinStarvationSeconds)

	// The admin edits the defaults the supported way: read them unmerged,
	// change one rule, write them back.
	def, err := repo.GetSystemDefault(ctx)
	require.NoError(t, err)
	assert.Nil(t, def.ClientID, "GetSystemDefault must return the default row, never a client's")
	def.MinStarvationSeconds = testutil.IntPtr(3600)
	require.NoError(t, repo.UpsertRules(ctx, def))

	after, err := repo.GetRules(ctx, clientID)
	require.NoError(t, err)
	require.NotNil(t, after.MinStarvationSeconds)
	assert.Equal(t, 3600, *after.MinStarvationSeconds,
		"tightening the default must reach a client that overrode an unrelated field")
	require.NotNil(t, after.MaxSpendPerJobCents)
	assert.Equal(t, int64(50000), *after.MaxSpendPerJobCents, "and the client's own override must survive it")
}

/*
 * TestUpsertRules_SystemDefaultUpdatesInPlace covers the partial-index conflict
 * target.
 *
 * ON CONFLICT (client_id) cannot see the system default row: NULLs are distinct
 * under a UNIQUE constraint, so the insert never conflicts on that target,
 * proceeds as a plain INSERT and trips
 * idx_cloud_provisioning_rules_system_default — the admin's edit fails outright
 * rather than applying. Drop that index and the same statement would instead
 * leave a second system default behind for GetRules to pick between. Only real
 * Postgres can tell the two targets apart.
 */
func TestUpsertRules_SystemDefaultUpdatesInPlace(t *testing.T) {
	repo, database := newProvisioningRulesRepo(t)
	ctx := context.Background()

	first := &models.CloudProvisioningRules{
		MinStarvationSeconds: testutil.IntPtr(240),
		ProvisioningWindowTZ: strPtrLocal("UTC"),
	}
	require.NoError(t, repo.UpsertRules(ctx, first))

	second := &models.CloudProvisioningRules{
		MinStarvationSeconds: testutil.IntPtr(360),
		ProvisioningWindowTZ: strPtrLocal("UTC"),
	}
	require.NoError(t, repo.UpsertRules(ctx, second))
	assert.Equal(t, first.ID, second.ID, "the second write must update the seeded row, not create another")

	var n int
	require.NoError(t, database.QueryRow(
		`SELECT count(*) FROM cloud_provisioning_rules WHERE client_id IS NULL`).Scan(&n))
	assert.Equal(t, 1, n, "there must be exactly one system default at all times")

	got, err := repo.GetSystemDefault(ctx)
	require.NoError(t, err)
	require.NotNil(t, got.MinStarvationSeconds)
	assert.Equal(t, 360, *got.MinStarvationSeconds)
}

// TestUpsertRules_ClientOverrideUpdatesInPlace covers the ON CONFLICT
// (client_id) branch: the UNIQUE constraint does cover a non-NULL client_id, so
// a second write for the same client updates rather than inserting.
func TestUpsertRules_ClientOverrideUpdatesInPlace(t *testing.T) {
	repo, database := newProvisioningRulesRepo(t)
	ctx := context.Background()
	clientID := rulesClient(t, database)

	first := &models.CloudProvisioningRules{ClientID: &clientID, MinJobPriority: testutil.IntPtr(10)}
	require.NoError(t, repo.UpsertRules(ctx, first))

	second := &models.CloudProvisioningRules{ClientID: &clientID, MinJobPriority: testutil.IntPtr(20)}
	require.NoError(t, repo.UpsertRules(ctx, second))
	assert.Equal(t, first.ID, second.ID, "the second write must update the same override row")

	var n int
	require.NoError(t, database.QueryRow(
		`SELECT count(*) FROM cloud_provisioning_rules WHERE client_id = $1`, clientID).Scan(&n))
	assert.Equal(t, 1, n)

	got, err := repo.GetRules(ctx, clientID)
	require.NoError(t, err)
	require.NotNil(t, got.MinJobPriority)
	assert.Equal(t, 20, *got.MinJobPriority)
}

/*
 * TestUpsertRules_ClearingAFieldRestoresInheritance: an upsert replaces the
 * whole override row, so a field dropped from the struct must actually be
 * NULLed.
 *
 * A DO UPDATE SET that skipped nil fields would let an admin change an
 * override's value but never give the field back to the default, making the
 * first override of any rule permanent — and undoing it would require deleting
 * the whole override and re-entering every other field.
 */
func TestUpsertRules_ClearingAFieldRestoresInheritance(t *testing.T) {
	repo, database := newProvisioningRulesRepo(t)
	ctx := context.Background()
	clientID := rulesClient(t, database)

	require.NoError(t, repo.UpsertRules(ctx, &models.CloudProvisioningRules{
		ClientID:             &clientID,
		MinStarvationSeconds: testutil.IntPtr(45),
		MinJobPriority:       testutil.IntPtr(7),
	}))
	got, err := repo.GetRules(ctx, clientID)
	require.NoError(t, err)
	require.NotNil(t, got.MinStarvationSeconds)
	require.Equal(t, 45, *got.MinStarvationSeconds)

	// Same client, starvation dropped from the override.
	require.NoError(t, repo.UpsertRules(ctx, &models.CloudProvisioningRules{
		ClientID:       &clientID,
		MinJobPriority: testutil.IntPtr(7),
	}))

	got, err = repo.GetRules(ctx, clientID)
	require.NoError(t, err)
	require.NotNil(t, got.MinStarvationSeconds)
	assert.Equal(t, 180, *got.MinStarvationSeconds, "the cleared field must fall back to the default's 180")
	require.NotNil(t, got.MinJobPriority)
	assert.Equal(t, 7, *got.MinJobPriority, "the field that stayed must still be overridden")
}

// TestDeleteClientRules_FallsBackToDefault: dropping the override returns the
// client to the defaults, and deleting an override that is already gone is a
// no-op rather than an error — the caller's intent is already satisfied.
func TestDeleteClientRules_FallsBackToDefault(t *testing.T) {
	repo, database := newProvisioningRulesRepo(t)
	ctx := context.Background()
	clientID := rulesClient(t, database)

	require.NoError(t, repo.UpsertRules(ctx, &models.CloudProvisioningRules{
		ClientID:             &clientID,
		MinStarvationSeconds: testutil.IntPtr(30),
	}))
	got, err := repo.GetRules(ctx, clientID)
	require.NoError(t, err)
	require.NotNil(t, got.MinStarvationSeconds)
	require.Equal(t, 30, *got.MinStarvationSeconds)

	require.NoError(t, repo.DeleteClientRules(ctx, clientID))

	got, err = repo.GetRules(ctx, clientID)
	require.NoError(t, err)
	// The VALUES fall back to the default row; the identity stays the client's,
	// so this result can never be saved back over the system default.
	assert.Equal(t, uuid.Nil, got.ID, "no override row survives, so no row id should be handed out")
	require.NotNil(t, got.ClientID)
	assert.Equal(t, clientID, *got.ClientID)
	require.NotNil(t, got.MinStarvationSeconds)
	assert.Equal(t, 180, *got.MinStarvationSeconds, "the override is gone, so the default value is what remains")

	require.NoError(t, repo.DeleteClientRules(ctx, clientID), "deleting an absent override must be idempotent")
}

/*
 * TestDeleteClientRules_RefusesSystemDefault: uuid.Nil is the only way this
 * signature can name the default row, and it must be rejected.
 *
 * Deleting it would break GetRules for every client at once, not just this one.
 * Silently succeeding would be no better: `client_id = $1` never matches a NULL
 * client_id, so an admin trying to reset the defaults would be told it worked
 * and find the table unchanged.
 */
func TestDeleteClientRules_RefusesSystemDefault(t *testing.T) {
	repo, database := newProvisioningRulesRepo(t)
	ctx := context.Background()

	err := repo.DeleteClientRules(ctx, uuid.Nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "system default", "the message must name what was refused")

	var n int
	require.NoError(t, database.QueryRow(
		`SELECT count(*) FROM cloud_provisioning_rules WHERE client_id IS NULL`).Scan(&n))
	assert.Equal(t, 1, n, "the system default must still be there")

	clientID := rulesClient(t, database)
	_, err = repo.GetRules(ctx, clientID)
	require.NoError(t, err, "and resolution must still work for every client")
}

/*
 * TestUpsertRules_WindowRoundTripsThroughTIME pins the read/write format
 * agreement, which is the classic silent failure for a TIME column.
 *
 * lib/pq decodes TIME into a time.Time and database/sql renders a time.Time
 * into a string as RFC3339Nano, so selecting the bare column would return
 * "0000-01-01T22:30:00Z" instead of "22:30:00". That value writes back to
 * Postgres without complaint and fails only much later, when whatever evaluates
 * the window cannot parse it and quietly decides the window is shut. The read
 * goes through to_char(col, 'HH24:MI:SS') to keep the driver out of it.
 */
func TestUpsertRules_WindowRoundTripsThroughTIME(t *testing.T) {
	repo, database := newProvisioningRulesRepo(t)
	ctx := context.Background()

	// The database must actually hold a TIME. If someone "fixes" a format
	// problem by widening the column to TEXT, every assertion below still
	// passes while the CHECK-able semantics of a time value are gone.
	var pgType string
	require.NoError(t, database.QueryRow(`
		SELECT data_type FROM information_schema.columns
		WHERE table_name = 'cloud_provisioning_rules'
		  AND column_name = 'provisioning_window_start'`).Scan(&pgType))
	require.Equal(t, "time without time zone", pgType)

	cases := []struct {
		name       string
		start, end string
		wantStart  string
		wantEnd    string
	}{
		// Seconds omitted on one bound and supplied on the other: both must
		// normalise to the same canonical form the read returns.
		{"overnight window", "22:30", "06:00:00", "22:30:00", "06:00:00"},
		{"unpadded hour", "9:05", "17:00", "09:05:00", "17:00:00"},
		{"midnight bounds", "00:00", "23:59:59", "00:00:00", "23:59:59"},
		// start == end is the in-band OFF value: always open, no second column.
		{"always open", "00:00:00", "00:00:00", "00:00:00", "00:00:00"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clientID := rulesClient(t, database)
			require.NoError(t, repo.UpsertRules(ctx, &models.CloudProvisioningRules{
				ClientID:                &clientID,
				ProvisioningWindowStart: strPtrLocal(tc.start),
				ProvisioningWindowEnd:   strPtrLocal(tc.end),
				ProvisioningWindowTZ:    strPtrLocal("America/New_York"),
			}))

			got, err := repo.GetRules(ctx, clientID)
			require.NoError(t, err)

			require.NotNil(t, got.ProvisioningWindowStart)
			require.NotNil(t, got.ProvisioningWindowEnd)
			assert.Equal(t, tc.wantStart, *got.ProvisioningWindowStart)
			assert.Equal(t, tc.wantEnd, *got.ProvisioningWindowEnd)
			require.NotNil(t, got.ProvisioningWindowTZ)
			assert.Equal(t, "America/New_York", *got.ProvisioningWindowTZ,
				"a UTC server must still be able to express local business hours")

			// Re-parsing with the layout it was written in is what the window
			// evaluation will do; if that fails the value is useless.
			_, err = time.Parse("15:04:05", *got.ProvisioningWindowStart)
			require.NoError(t, err)
			_, err = time.Parse("15:04:05", *got.ProvisioningWindowEnd)
			require.NoError(t, err)
		})
	}
}

// TestGetRules_WindowIsInheritedAsAUnit: a start taken from the default paired
// with an end from an override is a window nobody configured, so an override
// that sets neither bound inherits both — and the zone with them, since the
// bounds mean nothing without it.
func TestGetRules_WindowIsInheritedAsAUnit(t *testing.T) {
	repo, database := newProvisioningRulesRepo(t)
	ctx := context.Background()
	clientID := rulesClient(t, database)

	require.NoError(t, repo.UpsertRules(ctx, &models.CloudProvisioningRules{
		ProvisioningWindowStart: strPtrLocal("19:00"),
		ProvisioningWindowEnd:   strPtrLocal("07:00"),
		ProvisioningWindowTZ:    strPtrLocal("America/Chicago"),
	}))
	require.NoError(t, repo.UpsertRules(ctx, &models.CloudProvisioningRules{
		ClientID:       &clientID,
		MinJobPriority: testutil.IntPtr(25),
	}))

	got, err := repo.GetRules(ctx, clientID)
	require.NoError(t, err)

	require.NotNil(t, got.ProvisioningWindowStart)
	require.NotNil(t, got.ProvisioningWindowEnd)
	require.NotNil(t, got.ProvisioningWindowTZ)
	assert.Equal(t, "19:00:00", *got.ProvisioningWindowStart)
	assert.Equal(t, "07:00:00", *got.ProvisioningWindowEnd, "end < start wraps midnight; both bounds must come from the same row")
	assert.Equal(t, "America/Chicago", *got.ProvisioningWindowTZ, "the zone travels with the bounds it qualifies")
}

/*
 * TestGetRules_ResultCannotBeUpsertedOverTheSystemDefault covers a defect that
 * is invisible in any single call and only appears as a SEQUENCE — which is
 * also the exact sequence an admin screen performs.
 *
 * Load client X's rules to populate a form, change one field, save. If GetRules
 * hands back the default row verbatim for a client with no override, that
 * result carries ClientID == nil, and UpsertRules selects its ON CONFLICT
 * target off precisely that nil. The save then rewrites the SYSTEM DEFAULT —
 * changing policy for every client — while the screen that did it names one
 * client and reports success. Nothing errors, and the next admin to read the
 * defaults sees one client's numbers presented as everyone's.
 *
 * Two other clients are present so the blast radius is observable rather than
 * inferred.
 */
func TestGetRules_ResultCannotBeUpsertedOverTheSystemDefault(t *testing.T) {
	repo, database := newProvisioningRulesRepo(t)
	ctx := context.Background()
	clientX := rulesClient(t, database)
	bystander := rulesClient(t, database)

	// The admin flow: read what applies to X (which inherits everything), edit
	// one field, save it back.
	loaded, err := repo.GetRules(ctx, clientX)
	require.NoError(t, err)
	loaded.MaxSpendPerJobCents = testutil.Int64Ptr(25_000)
	require.NoError(t, repo.UpsertRules(ctx, loaded))

	// The system default must be untouched.
	def, err := repo.GetSystemDefault(ctx)
	require.NoError(t, err)
	require.NotNil(t, def.MaxSpendPerJobCents)
	assert.Equal(t, int64(0), *def.MaxSpendPerJobCents,
		"editing one client's rules rewrote the system default for every client")

	// And no second system-default row was created either.
	var defaults int
	require.NoError(t, database.QueryRow(
		`SELECT count(*) FROM cloud_provisioning_rules WHERE client_id IS NULL`).Scan(&defaults))
	assert.Equal(t, 1, defaults)

	// The edit landed where the screen said it would.
	gotX, err := repo.GetRules(ctx, clientX)
	require.NoError(t, err)
	require.NotNil(t, gotX.MaxSpendPerJobCents)
	assert.Equal(t, int64(25_000), *gotX.MaxSpendPerJobCents, "the client's own override should hold the edit")

	// An unrelated client still sees the untouched default.
	gotBystander, err := repo.GetRules(ctx, bystander)
	require.NoError(t, err)
	require.NotNil(t, gotBystander.MaxSpendPerJobCents)
	assert.Equal(t, int64(0), *gotBystander.MaxSpendPerJobCents,
		"an unrelated client inherited a cap from another client's edit")
}

/*
 * TestGetRules_WindowZoneInheritsWhenTheOverrideSetsItsOwnBounds is the money
 * case for zone inheritance, through the real TIME columns.
 *
 * A client sets an overnight window and says nothing about the zone, meaning
 * "these hours, in the zone already configured". If the zone only travels with
 * the bounds, this override inherits none and silently evaluates in UTC: with
 * a default of America/Chicago, 22:00-06:00 becomes 17:00-01:00 local, so the
 * autoscaler rents through the client's evening business hours and stops at
 * 01:00 — the policy inverted, with no error and nothing logged.
 */
func TestGetRules_WindowZoneInheritsWhenTheOverrideSetsItsOwnBounds(t *testing.T) {
	repo, database := newProvisioningRulesRepo(t)
	ctx := context.Background()
	clientID := rulesClient(t, database)

	require.NoError(t, repo.UpsertRules(ctx, &models.CloudProvisioningRules{
		ProvisioningWindowStart: strPtrLocal("20:00"),
		ProvisioningWindowEnd:   strPtrLocal("04:00"),
		ProvisioningWindowTZ:    strPtrLocal("America/Chicago"),
	}))
	require.NoError(t, repo.UpsertRules(ctx, &models.CloudProvisioningRules{
		ClientID:                &clientID,
		ProvisioningWindowStart: strPtrLocal("22:00"),
		ProvisioningWindowEnd:   strPtrLocal("06:00"),
		// No zone: inherit it.
	}))

	got, err := repo.GetRules(ctx, clientID)
	require.NoError(t, err)

	require.NotNil(t, got.ProvisioningWindowTZ,
		"an override with its own window inherited no zone and will be evaluated in UTC")
	assert.Equal(t, "America/Chicago", *got.ProvisioningWindowTZ,
		"the zone must inherit independently of the bounds")
	require.NotNil(t, got.ProvisioningWindowStart)
	assert.Equal(t, "22:00:00", *got.ProvisioningWindowStart, "the override's own bounds still win")
	require.NotNil(t, got.ProvisioningWindowEnd)
	assert.Equal(t, "06:00:00", *got.ProvisioningWindowEnd)
}

// TestUpsertRules_RejectsBadInput: each of these has a DB constraint behind it,
// but a raw constraint-violation string is not something to show an admin.
func TestUpsertRules_RejectsBadInput(t *testing.T) {
	repo, database := newProvisioningRulesRepo(t)
	ctx := context.Background()
	clientID := rulesClient(t, database)

	cases := []struct {
		name  string
		rules *models.CloudProvisioningRules
	}{
		{"negative priority floor", &models.CloudProvisioningRules{
			ClientID: &clientID, MinJobPriority: testutil.IntPtr(-1)}},
		{"negative starvation", &models.CloudProvisioningRules{
			ClientID: &clientID, MinStarvationSeconds: testutil.IntPtr(-1)}},
		{"negative skip-if-finishing", &models.CloudProvisioningRules{
			ClientID: &clientID, SkipIfFinishingWithinSeconds: testutil.IntPtr(-1)}},
		{"negative per-job cap", &models.CloudProvisioningRules{
			ClientID: &clientID, MaxSpendPerJobCents: testutil.Int64Ptr(-1)}},
		// A lone bound has no reading, and guessing the other one would invent
		// a policy nobody chose.
		{"window start without end", &models.CloudProvisioningRules{
			ClientID: &clientID, ProvisioningWindowStart: strPtrLocal("09:00")}},
		{"window end without start", &models.CloudProvisioningRules{
			ClientID: &clientID, ProvisioningWindowEnd: strPtrLocal("17:00")}},
		// Rejected here rather than at evaluation time, which happens
		// unattended and — for anyone using a window — usually at night.
		{"unknown timezone", &models.CloudProvisioningRules{
			ClientID: &clientID, ProvisioningWindowTZ: strPtrLocal("Mars/Olympus_Mons")}},
		{"unparseable window bound", &models.CloudProvisioningRules{
			ClientID:                &clientID,
			ProvisioningWindowStart: strPtrLocal("9am"),
			ProvisioningWindowEnd:   strPtrLocal("17:00")}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, repo.UpsertRules(ctx, tc.rules))
		})
	}

	// None of the rejections may have left a partial row behind: a client with
	// a half-written override would resolve to rules nobody entered.
	var n int
	require.NoError(t, database.QueryRow(
		`SELECT count(*) FROM cloud_provisioning_rules WHERE client_id = $1`, clientID).Scan(&n))
	assert.Zero(t, n, "a rejected upsert must write nothing")
}

/*
 * TestGetRules_MissingSystemDefaultIsAnError names the problem instead of
 * papering over it.
 *
 * Synthesising an empty default would be worse than failing: under these
 * semantics every nil field means "constrains nothing", so a migration that did
 * not run would hand the autoscaler permission to rent for any job, at any
 * priority, at any hour, with no per-job ceiling. Mirrors GetPolicy's
 * "the system default row is missing".
 */
func TestGetRules_MissingSystemDefaultIsAnError(t *testing.T) {
	repo, database := newProvisioningRulesRepo(t)
	ctx := context.Background()
	clientID := rulesClient(t, database)

	_, err := database.Exec(`DELETE FROM cloud_provisioning_rules WHERE client_id IS NULL`)
	require.NoError(t, err)

	_, err = repo.GetRules(ctx, clientID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "system default row is missing")

	_, err = repo.GetSystemDefault(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "system default row is missing")
}

/*
 * TestDeleteClientRules_CascadeFromClient pins the ON DELETE CASCADE.
 *
 * Deleting a client must not leave its override behind: client ids come from
 * gen_random_uuid, so an orphaned row could never be reached through GetRules
 * again, but it would still hold the partial unique index's neighbour slot and
 * would reappear in any admin listing of configured clients.
 */
func TestDeleteClientRules_CascadeFromClient(t *testing.T) {
	repo, database := newProvisioningRulesRepo(t)
	ctx := context.Background()
	clientID := rulesClient(t, database)

	require.NoError(t, repo.UpsertRules(ctx, &models.CloudProvisioningRules{
		ClientID:       &clientID,
		MinJobPriority: testutil.IntPtr(15),
	}))

	_, err := database.Exec(`DELETE FROM clients WHERE id = $1`, clientID)
	require.NoError(t, err)

	var n int
	require.NoError(t, database.QueryRow(
		`SELECT count(*) FROM cloud_provisioning_rules WHERE client_id = $1`, clientID).Scan(&n))
	assert.Zero(t, n, "the override must go with the client")
}
