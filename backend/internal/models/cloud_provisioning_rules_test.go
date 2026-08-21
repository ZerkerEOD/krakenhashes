package models

import (
	"testing"

	"github.com/google/uuid"
)

func intp(v int) *int              { return &v }
func i64p(v int64) *int64          { return &v }
func strp(v string) *string        { return &v }
func uuidp(v uuid.UUID) *uuid.UUID { return &v }

// systemDefault mirrors the row the migration seeds, so a test that drifts from
// production defaults fails here rather than in an invoice.
func systemDefault() *CloudProvisioningRules {
	return &CloudProvisioningRules{
		ID:                           uuid.New(),
		ClientID:                     nil,
		MinJobPriority:               intp(0),
		MinStarvationSeconds:         intp(180),
		SkipIfFinishingWithinSeconds: intp(900),
		MaxSpendPerJobCents:          i64p(0),
		ProvisioningWindowTZ:         strp("UTC"),
	}
}

/*
 * TestMergeInheritsUnsetOverrideFields is the ordinary case: NULL on an override
 * means inherit, a set value wins, and an explicit 0 is a VALUE (the in-band OFF
 * switch) rather than an absence.
 *
 * That last one is the whole reason every field is a pointer. If 0 were treated
 * as unset, a client could never opt out of an inherited rule.
 */
func TestMergeInheritsUnsetOverrideFields(t *testing.T) {
	def := systemDefault()
	def.MinJobPriority = intp(700)

	override := &CloudProvisioningRules{
		ClientID: uuidp(uuid.New()),
		// Explicitly opts out of the inherited starvation delay.
		MinStarvationSeconds: intp(0),
		// Sets its own cap.
		MaxSpendPerJobCents: i64p(50_000),
		// Everything else unset: inherit.
	}

	got := MergeProvisioningRules(def, override)
	if got == nil {
		t.Fatal("merge of a real default and override returned nil")
	}

	if *got.MinJobPriority != 700 {
		t.Errorf("unset override field must inherit the default, got %d", *got.MinJobPriority)
	}
	if *got.MinStarvationSeconds != 0 {
		t.Errorf("an explicit 0 is the OFF value and must survive the merge, got %d — "+
			"if this reads 180 the client cannot opt out of an inherited rule at all",
			*got.MinStarvationSeconds)
	}
	if *got.MaxSpendPerJobCents != 50_000 {
		t.Errorf("a set override field must win, got %d", *got.MaxSpendPerJobCents)
	}
	if *got.SkipIfFinishingWithinSeconds != 900 {
		t.Errorf("unset override field must inherit the default, got %d", *got.SkipIfFinishingWithinSeconds)
	}
}

/*
 * TestMergeTightenedDefaultReachesPartialOverrides is the reason this merges per
 * field instead of picking a whole row like GetPolicy does.
 *
 * An admin tightens the system default. Under whole-row semantics the clients
 * that already had an override — the ones most likely to need tightening — would
 * silently keep the old, looser value.
 */
func TestMergeTightenedDefaultReachesPartialOverrides(t *testing.T) {
	def := systemDefault()
	def.MinJobPriority = intp(900) // admin tightens the floor

	// A client override that says nothing about priority.
	override := &CloudProvisioningRules{
		ClientID:            uuidp(uuid.New()),
		MaxSpendPerJobCents: i64p(10_000),
	}

	got := MergeProvisioningRules(def, override)
	if *got.MinJobPriority != 900 {
		t.Errorf("a tightened default must reach clients with partial overrides, got %d", *got.MinJobPriority)
	}
}

/*
 * TestMergeWindowBoundsTravelAsAPair guards the half-window: a start inherited
 * from the default against an end from an override is not a window anyone
 * configured, and the pair only means anything together.
 */
func TestMergeWindowBoundsTravelAsAPair(t *testing.T) {
	def := systemDefault()
	def.ProvisioningWindowStart = strp("22:00:00")
	def.ProvisioningWindowEnd = strp("06:00:00")

	// An override with only ONE bound set is not a window; both must come from
	// the default rather than being spliced together.
	override := &CloudProvisioningRules{
		ClientID:                uuidp(uuid.New()),
		ProvisioningWindowStart: strp("09:00:00"),
	}

	got := MergeProvisioningRules(def, override)
	if *got.ProvisioningWindowStart != "22:00:00" || *got.ProvisioningWindowEnd != "06:00:00" {
		t.Errorf("a half-configured override window must not be spliced with the default; got %s-%s",
			*got.ProvisioningWindowStart, *got.ProvisioningWindowEnd)
	}
}

/*
 * TestMergeWindowZoneInheritsIndependentlyOfBounds is the expensive one, and it
 * fails in the direction that spends money.
 *
 * A client sets its own overnight window and leaves the zone alone, which is the
 * obvious way to say "these hours, in the zone you already configured". If the
 * zone only inherits alongside the bounds, this override gets no zone and falls
 * back to UTC. With a default of America/Chicago that turns 22:00-06:00 into
 * 17:00-01:00 local: the autoscaler rents straight through the client's evening
 * business hours and stops at 01:00 — the exact inverse of the policy, with no
 * error and nothing in the logs.
 */
func TestMergeWindowZoneInheritsIndependentlyOfBounds(t *testing.T) {
	def := systemDefault()
	def.ProvisioningWindowStart = strp("20:00:00")
	def.ProvisioningWindowEnd = strp("04:00:00")
	def.ProvisioningWindowTZ = strp("America/Chicago")

	t.Run("override sets its own bounds and inherits the zone", func(t *testing.T) {
		override := &CloudProvisioningRules{
			ClientID:                uuidp(uuid.New()),
			ProvisioningWindowStart: strp("22:00:00"),
			ProvisioningWindowEnd:   strp("06:00:00"),
			// No TZ. This must NOT silently become UTC.
		}

		got := MergeProvisioningRules(def, override)
		if got.ProvisioningWindowTZ == nil {
			t.Fatal("an override with its own window inherited no timezone; it will be evaluated in UTC")
		}
		if *got.ProvisioningWindowTZ != "America/Chicago" {
			t.Errorf("window zone must inherit independently of the bounds, got %q", *got.ProvisioningWindowTZ)
		}
		if *got.ProvisioningWindowStart != "22:00:00" {
			t.Errorf("the override's own bounds must still win, got %s", *got.ProvisioningWindowStart)
		}
	})

	t.Run("zone-only override keeps its zone against the inherited bounds", func(t *testing.T) {
		// "Same hours, our local time" — the natural other half of the same
		// idea. Folding the zone into the bounds unit validates this, stores
		// it, and then quietly discards it.
		override := &CloudProvisioningRules{
			ClientID:             uuidp(uuid.New()),
			ProvisioningWindowTZ: strp("Europe/London"),
		}

		got := MergeProvisioningRules(def, override)
		if *got.ProvisioningWindowTZ != "Europe/London" {
			t.Errorf("a zone-only override must survive the merge, got %q", *got.ProvisioningWindowTZ)
		}
		if *got.ProvisioningWindowStart != "20:00:00" || *got.ProvisioningWindowEnd != "04:00:00" {
			t.Errorf("bounds should still inherit, got %s-%s",
				*got.ProvisioningWindowStart, *got.ProvisioningWindowEnd)
		}
	})
}

/*
 * TestMergeNilDefaultFailsClosed pins the asymmetry that makes this safe.
 *
 * Returning an empty struct here would be the natural-looking choice and is the
 * dangerous one: under these semantics every nil field is a rail that constrains
 * nothing, so an all-nil result reads as "any job, any priority, any hour, no
 * cap". A nil default does not mean the rules are unconfigured — it means the
 * system-default ROW IS MISSING. Broken table, unrestricted spending.
 */
func TestMergeNilDefaultFailsClosed(t *testing.T) {
	override := &CloudProvisioningRules{
		ClientID:       uuidp(uuid.New()),
		MinJobPriority: intp(500),
	}

	if got := MergeProvisioningRules(nil, override); got != nil {
		t.Errorf("a missing system default must not resolve to a usable policy, got %+v", got)
	}
	if got := MergeProvisioningRules(nil, nil); got != nil {
		t.Errorf("a missing system default must not resolve to a usable policy, got %+v", got)
	}
}

// TestMergeNilOverrideCopiesRatherThanAliases: callers mutate merged results
// (GetRules stamps the ClientID onto one), so handing back the caller's own
// default pointer would let one client's read corrupt the shared default.
func TestMergeNilOverrideCopiesRatherThanAliases(t *testing.T) {
	def := systemDefault()
	got := MergeProvisioningRules(def, nil)
	if got == def {
		t.Fatal("merge returned the default itself; a caller mutating the result would corrupt it for everyone")
	}

	got.MinJobPriority = intp(999)
	if *def.MinJobPriority != 0 {
		t.Errorf("mutating the merged result changed the default, now %d", *def.MinJobPriority)
	}
}
