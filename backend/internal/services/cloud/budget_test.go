package cloud

import (
	"database/sql"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

// These tests are pure: no database, no network. They cover the arithmetic
// that decides how much money the feature is allowed to spend.

func intPtr(v int) *int     { return &v }
func i64Ptr(v int64) *int64 { return &v }

// defaultPolicy mirrors the migration-seeded system default (80/95/99/100).
func defaultPolicy() *models.CloudBudgetPolicy {
	return &models.CloudBudgetPolicy{
		NotifyPct:           intPtr(80),
		StopProvisionPct:    95,
		DrainPct:            99,
		HardStopPct:         100,
		AllowOverage:        false,
		DrainTimeoutSeconds: 300,
	}
}

func stateAt(usedPct float64, cap *int64) *models.CloudBudgetState {
	return &models.CloudBudgetState{ClientID: uuid.New(), CapCents: cap, UsedPct: usedPct}
}

func TestCostFor(t *testing.T) {
	tests := []struct {
		name         string
		d            time.Duration
		centsPerHour int
		want         int64
	}{
		{"exact hour", time.Hour, 100, 100},
		{"half hour", 30 * time.Minute, 100, 50},
		// Rounding UP is the whole point: rounding down lets a long tail of
		// sub-cent remainders accumulate into real unbudgeted spend.
		{"rounds up a fraction of a cent", 36 * time.Second, 100, 1},
		{"rounds up just over a cent", 37 * time.Second, 100, 2},
		{"one second is never free", time.Second, 100, 1},
		{"zero duration", 0, 100, 0},
		{"negative duration", -time.Hour, 100, 0},
		{"zero rate", time.Hour, 0, 0},
		{"negative rate", time.Hour, -100, 0},
		{"expensive GPU for four hours", 4 * time.Hour, 2200, 8800},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := costFor(tc.d, tc.centsPerHour); got != tc.want {
				t.Errorf("costFor(%s, %d) = %d, want %d", tc.d, tc.centsPerHour, got, tc.want)
			}
		})
	}
}

func TestDecideBudgetAction(t *testing.T) {
	cap := i64Ptr(10_000)

	tests := []struct {
		name       string
		state      *models.CloudBudgetState
		policy     *models.CloudBudgetPolicy
		wantAction BudgetAction
	}{
		{"well under everything", stateAt(10, cap), defaultPolicy(), BudgetActionNone},
		{"just under notify", stateAt(79.9, cap), defaultPolicy(), BudgetActionNone},
		// Thresholds are inclusive, so each boundary fires exactly at its value.
		{"exactly at notify", stateAt(80, cap), defaultPolicy(), BudgetActionNotify},
		{"between notify and stop", stateAt(94.9, cap), defaultPolicy(), BudgetActionNotify},
		{"exactly at stop provisioning", stateAt(95, cap), defaultPolicy(), BudgetActionStopProvisioning},
		{"exactly at drain", stateAt(99, cap), defaultPolicy(), BudgetActionDrain},
		{"exactly at hard stop", stateAt(100, cap), defaultPolicy(), BudgetActionHardStop},
		{"past hard stop", stateAt(140, cap), defaultPolicy(), BudgetActionHardStop},

		// An unfunded client is NOT the same as one at 100%: there is no budget
		// at all, so there is nothing to drain.
		{"no funded budget", stateAt(0, nil), defaultPolicy(), BudgetActionStopProvisioning},
		{"no funded budget, high pct", stateAt(500, nil), defaultPolicy(), BudgetActionStopProvisioning},

		{
			name:       "notify disabled entirely",
			state:      stateAt(85, cap),
			policy:     &models.CloudBudgetPolicy{NotifyPct: nil, StopProvisionPct: 95, DrainPct: 99, HardStopPct: 100},
			wantAction: BudgetActionNone,
		},
		{
			name:       "notify disabled does not suppress the harder rungs",
			state:      stateAt(99, cap),
			policy:     &models.CloudBudgetPolicy{NotifyPct: nil, StopProvisionPct: 95, DrainPct: 99, HardStopPct: 100},
			wantAction: BudgetActionDrain,
		},
		{
			// "Stop at 99, hard stop at 100, never notify me" is a legitimate
			// configuration and must not be special-cased away.
			name:       "operator ladder with no notify and a tight top",
			state:      stateAt(99.5, cap),
			policy:     &models.CloudBudgetPolicy{NotifyPct: nil, StopProvisionPct: 99, DrainPct: 99, HardStopPct: 100},
			wantAction: BudgetActionDrain,
		},
		{
			// All four thresholds equal: the most severe must win.
			name:       "collapsed ladder resolves to the most severe",
			state:      stateAt(100, cap),
			policy:     &models.CloudBudgetPolicy{NotifyPct: intPtr(100), StopProvisionPct: 100, DrainPct: 100, HardStopPct: 100},
			wantAction: BudgetActionHardStop,
		},
		{
			// allow_overage lets spend exceed the cap; the ladder still reports
			// the truth. Suppression happens in PlanLaunch, not here.
			name:       "overage policy still reports hard stop",
			state:      stateAt(150, cap),
			policy:     &models.CloudBudgetPolicy{NotifyPct: intPtr(80), StopProvisionPct: 95, DrainPct: 99, HardStopPct: 100, AllowOverage: true},
			wantAction: BudgetActionHardStop,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			action, reason := decideBudgetAction(tc.state, tc.policy)
			if action != tc.wantAction {
				t.Errorf("action = %v (%q), want %v", action, reason, tc.wantAction)
			}
			if action != BudgetActionNone && reason == "" {
				t.Error("a non-none action must carry an explanatory reason")
			}
		})
	}
}

func TestBudgetActionAllowsNewInstances(t *testing.T) {
	allowed := map[BudgetAction]bool{
		BudgetActionNone:             true,
		BudgetActionNotify:           true,
		BudgetActionStopProvisioning: false,
		BudgetActionDrain:            false,
		BudgetActionHardStop:         false,
	}
	for action, want := range allowed {
		if got := action.AllowsNewInstances(); got != want {
			t.Errorf("%v.AllowsNewInstances() = %v, want %v", action, got, want)
		}
	}
}

// TestAccrueInstanceClamps covers the accrual arithmetic without a database by
// exercising the pure part: elapsed computation and the TTL clamp. The delta is
// what would be written; a zero delta means nothing is recorded.
func TestAccrueInstanceArithmetic(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		billedFrom time.Time
		now        time.Time
		ttlEpoch   *time.Time
		rate       int
		alreadyEst int64
		wantDelta  int64
	}{
		{
			name:       "one hour at 100c with nothing accrued yet",
			billedFrom: base, now: base.Add(time.Hour),
			rate: 100, alreadyEst: 0, wantDelta: 100,
		},
		{
			name:       "second accrual only records the increment",
			billedFrom: base, now: base.Add(2 * time.Hour),
			rate: 100, alreadyEst: 100, wantDelta: 100,
		},
		{
			// Accrual must never go backwards, or a clock skew would hand
			// budget back that was genuinely spent.
			name:       "already ahead of wall clock records nothing",
			billedFrom: base, now: base.Add(time.Hour),
			rate: 100, alreadyEst: 500, wantDelta: 0,
		},
		{
			name:       "zero billedFrom records nothing",
			billedFrom: time.Time{}, now: base.Add(time.Hour),
			rate: 100, alreadyEst: 0, wantDelta: 0,
		},
		{
			name:       "now before billedFrom records nothing",
			billedFrom: base.Add(time.Hour), now: base,
			rate: 100, alreadyEst: 0, wantDelta: 0,
		},
		{
			// Past the TTL the instance should be gone. Charging further would
			// silently eat budget the reaper already released.
			name:       "elapsed is clamped at the TTL",
			billedFrom: base, now: base.Add(10 * time.Hour),
			ttlEpoch: timePtr(base.Add(2 * time.Hour)),
			rate:     100, alreadyEst: 0, wantDelta: 200,
		},
		{
			name:       "TTL clamp with prior accrual",
			billedFrom: base, now: base.Add(10 * time.Hour),
			ttlEpoch: timePtr(base.Add(2 * time.Hour)),
			rate:     100, alreadyEst: 150, wantDelta: 50,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inst := &models.CloudInstance{
				HourlyRateCents:    tc.rate,
				EstimatedCostCents: tc.alreadyEst,
			}
			if tc.ttlEpoch != nil {
				inst.TTLEpoch = sql.NullTime{Time: *tc.ttlEpoch, Valid: true}
			}

			got := accrualDelta(inst, tc.billedFrom, tc.now)
			if got != tc.wantDelta {
				t.Errorf("accrualDelta = %d, want %d", got, tc.wantDelta)
			}
		})
	}
}

func timePtr(t time.Time) *time.Time { return &t }

// TestSettleUnused covers the release arithmetic.
func TestSettleUnused(t *testing.T) {
	tests := []struct {
		name       string
		reserved   int64
		estimated  int64
		wantUnused int64
	}{
		{"finished early releases the remainder", 1000, 300, 700},
		{"used exactly what it reserved", 1000, 1000, 0},
		{"overran its reservation releases nothing", 1000, 1400, 0},
		{"nothing reserved", 0, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inst := &models.CloudInstance{ReservedCents: tc.reserved, EstimatedCostCents: tc.estimated}
			got := inst.ReservedCents - inst.EstimatedCostCents
			if got < 0 {
				got = 0
			}
			if got != tc.wantUnused {
				t.Errorf("unused = %d, want %d", got, tc.wantUnused)
			}
		})
	}
}
