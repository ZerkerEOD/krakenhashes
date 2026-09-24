// Package cloud owns ephemeral cloud GPU provisioning: budget enforcement,
// provider adapters, instance lifecycle, and the reaper that guarantees
// teardown.
package cloud

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/google/uuid"
)

// BudgetAction is the most severe action the threshold ladder calls for.
// Ordered so that a numerically greater action subsumes the lesser ones.
type BudgetAction int

const (
	// BudgetActionNone: spend is below every configured threshold.
	BudgetActionNone BudgetAction = iota
	// BudgetActionNotify: tell the admins, keep running.
	BudgetActionNotify
	// BudgetActionStopProvisioning: no new instances; existing ones continue.
	BudgetActionStopProvisioning
	// BudgetActionDrain: stop dispatching new chunks, let in-flight work
	// finish within the drain timeout, then destroy.
	BudgetActionDrain
	// BudgetActionHardStop: destroy now.
	BudgetActionHardStop
)

func (a BudgetAction) String() string {
	switch a {
	case BudgetActionNotify:
		return "notify"
	case BudgetActionStopProvisioning:
		return "stop_provisioning"
	case BudgetActionDrain:
		return "drain"
	case BudgetActionHardStop:
		return "hard_stop"
	default:
		return "none"
	}
}

// AllowsNewInstances reports whether provisioning may proceed.
func (a BudgetAction) AllowsNewInstances() bool { return a < BudgetActionStopProvisioning }

// BudgetAssessment is the budget picture plus the action it implies.
type BudgetAssessment struct {
	State  *models.CloudBudgetState  `json:"state"`
	Policy *models.CloudBudgetPolicy `json:"policy"`
	Action BudgetAction              `json:"-"`
	Reason string                    `json:"reason,omitempty"`
}

/*
 * ErrRentalTooShort means the instance could be paid for but not usefully: the
 * runway on offer is so short that commissioning would eat most or all of it.
 *
 * Distinct from ErrInsufficientBudget because the two need different responses.
 * "You are out of money" is answered by raising the cap; "the rental would be
 * mostly setup" is answered by raising max_instance_ttl_minutes, or by
 * accepting less efficient rentals via cloud_max_commissioning_pct. Collapsing
 * them told the operator to top up a budget that was not the problem.
 */
var ErrRentalTooShort = errors.New("rental too short to be useful")

/*
 * The commissioning arithmetic, mirrored into this package on purpose.
 *
 * cloud must not import scheduler in production code — the dependency runs the
 * other way — so these constants restate numbers the scheduler owns. That is
 * only safe because a drift guard in budget_floor_test.go asserts they still
 * agree with their sources; if someone changes a sync grace or a benchmark
 * window, CI fails rather than the floor quietly drifting into meaninglessness.
 * The same trick, for the same reason, is what keeps CommissioningGrace honest.
 */
const (
	// commissioningBudget mirrors scheduler.ReadinessBudget(): the longest a
	// HEALTHY agent may go between becoming ready and receiving its first task.
	// This is the time a rental pays for before it can do anything at all.
	commissioningBudget = 20 * time.Minute
	// defaultTeardownSlack mirrors the cloud_teardown_slack_seconds seed. The
	// dispatcher subtracts it from remaining TTL before sizing a chunk.
	defaultTeardownSlack = 120 * time.Second
	// minUsefulChunk mirrors the scheduler's min_chunk_seconds default. Below
	// this much runway resolveChunkDuration skips dispatch outright, so an
	// instance with less can never be given work.
	minUsefulChunk = 5 * time.Second
)

/*
 * MinRentalTTL is the shortest rental worth making, in two rungs.
 *
 * CAPABILITY (always): commissioning + teardown slack + one minimum chunk.
 * Below this the dispatcher provably refuses to hand the instance any work at
 * all, so the rental cannot do anything by construction. This rung cannot be
 * switched off.
 *
 * EFFICIENCY (when maxCommissioningPct > 0): the length at which commissioning
 * is no more than the configured share of the bill. At the default 33% and a
 * 20-minute commissioning budget this is just over an hour, and it is the rung
 * that actually binds in practice.
 *
 * Pure and total, so the whole policy can be exercised as a table of cases
 * rather than through a provisioning run — the same reason decideBudgetAction
 * and rankOffers are pure.
 */
func MinRentalTTL(maxCommissioningPct int) time.Duration {
	/*
	 * Both rungs are rounded to whole MINUTES, because the number they are
	 * compared against is max_instance_ttl_minutes — an operator types 60, not
	 * 60.606. Unrounded, the default worked out to 1h0m36.363636363s, so the
	 * single most obvious value anyone would enter was refused by 36 seconds,
	 * and the refusal said so in nine decimal places.
	 *
	 * The directions are opposite on purpose. Capability rounds UP: it is a hard
	 * requirement — below it the dispatcher will not size a chunk — so losing
	 * seconds off it would sell a rental that cannot work. Efficiency rounds
	 * DOWN: it is a preference, and rounding a preference towards permitting
	 * more never refuses something the ratio itself would have allowed.
	 */
	capability := ceilMinute(commissioningBudget + defaultTeardownSlack + minUsefulChunk)
	if maxCommissioningPct <= 0 {
		return capability
	}
	if maxCommissioningPct > 100 {
		maxCommissioningPct = 100
	}
	efficiency := time.Duration(int64(commissioningBudget) * 100 / int64(maxCommissioningPct)).
		Truncate(time.Minute)
	if efficiency > capability {
		return efficiency
	}
	return capability
}

// ceilMinute rounds up to the next whole minute, leaving exact minutes alone.
func ceilMinute(d time.Duration) time.Duration {
	if r := d % time.Minute; r != 0 {
		return d - r + time.Minute
	}
	return d
}

// BudgetEngine enforces per-client cloud spend limits.
type BudgetEngine struct {
	repo *repository.CloudBudgetRepository

	/*
	 * MaxCommissioningPct comes from cloud_max_commissioning_pct and sets the
	 * minimum useful rental via MinRentalTTL.
	 *
	 * Zero is NOT "no floor": MinRentalTTL still returns the capability rung,
	 * which refuses only what provably cannot be given a chunk. That matters
	 * because a BudgetEngine built without wiring this — a test, or a code path
	 * someone adds later — must not silently lose the protection entirely. Set
	 * it explicitly to disable the efficiency rung; leaving it unset gets the
	 * shipped default.
	 */
	MaxCommissioningPct int
}

// NewBudgetEngine creates a budget engine with the compiled-in default floor,
// so a deployment whose settings table cannot be read still refuses a rental
// that is mostly commissioning. Callers that have loaded settings should
// overwrite MaxCommissioningPct from them.
func NewBudgetEngine(repo *repository.CloudBudgetRepository) *BudgetEngine {
	return &BudgetEngine{
		repo:                repo,
		MaxCommissioningPct: DefaultSettings().MaxCommissioningPct,
	}
}

// Assess computes the current budget state and the action it calls for.
func (e *BudgetEngine) Assess(ctx context.Context, clientID uuid.UUID) (*BudgetAssessment, error) {
	state, err := e.repo.GetBudgetState(ctx, clientID)
	if err != nil {
		return nil, err
	}
	policy, err := e.repo.GetPolicy(ctx, clientID)
	if err != nil {
		return nil, err
	}

	a := &BudgetAssessment{State: state, Policy: policy}
	a.Action, a.Reason = decideBudgetAction(state, policy)
	return a, nil
}

/*
 * decideBudgetAction maps a budget state onto the threshold ladder.
 *
 * Split out from Assess so the ladder can be exercised as a table of pure
 * cases: Assess itself does two database reads before reaching this switch,
 * which would make every boundary case a round trip.
 *
 * Thresholds are inclusive (>=), so a policy with hard_stop at 100 fires
 * exactly at 100.0%, not just above it.
 */
func decideBudgetAction(state *models.CloudBudgetState, policy *models.CloudBudgetPolicy) (BudgetAction, string) {
	// An unfunded client can never provision. This is distinct from "at 100%":
	// there is no budget at all, so there is nothing to drain either.
	if state.CapCents == nil {
		return BudgetActionStopProvisioning, "client has no cloud budget configured"
	}

	pct := state.UsedPct
	switch {
	case pct >= float64(policy.HardStopPct):
		return BudgetActionHardStop,
			fmt.Sprintf("spend at %.1f%% of cap (hard stop at %d%%)", pct, policy.HardStopPct)
	case pct >= float64(policy.DrainPct):
		return BudgetActionDrain,
			fmt.Sprintf("spend at %.1f%% of cap (drain at %d%%)", pct, policy.DrainPct)
	case pct >= float64(policy.StopProvisionPct):
		return BudgetActionStopProvisioning,
			fmt.Sprintf("spend at %.1f%% of cap (stop provisioning at %d%%)", pct, policy.StopProvisionPct)
	case policy.NotifyPct != nil && pct >= float64(*policy.NotifyPct):
		return BudgetActionNotify,
			fmt.Sprintf("spend at %.1f%% of cap (notify at %d%%)", pct, *policy.NotifyPct)
	default:
		return BudgetActionNone, ""
	}
}

// LaunchBudget describes how much runway a client's budget buys for one
// instance at a given price.
type LaunchBudget struct {
	// TTL is how long the instance may live. Never longer than MaxTTL, never
	// longer than the budget can pay for.
	TTL time.Duration
	// ReserveCents is what will be committed for that TTL.
	ReserveCents int64
	// Assessment is the pre-launch budget picture.
	Assessment *BudgetAssessment
}

/*
 * PlanLaunch decides how long an instance may run and what that costs, without
 * committing anything.
 *
 * ttl = min(maxTTL, available_budget / hourly_rate)
 *
 * This is NPK's `campaign_max_price / spotPrice` idea with their bug fixed:
 * theirs forgot to multiply by instance count, so an N-node fleet silently got
 * N times the intended budget window. Here each instance reserves its own
 * runway, so N instances consume N reservations and the cap holds.
 *
 * extraCents covers non-compute charges the provider bills separately —
 * Vast.ai storage and bandwidth, AWS EBS — plus teardown slack.
 */
func (e *BudgetEngine) PlanLaunch(
	ctx context.Context,
	clientID uuid.UUID,
	hourlyRateCents int,
	maxTTL time.Duration,
	extraCents int64,
) (*LaunchBudget, error) {
	assessment, err := e.Assess(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if !assessment.Action.AllowsNewInstances() && !assessment.Policy.AllowOverage {
		return nil, fmt.Errorf("provisioning blocked: %s", assessment.Reason)
	}
	if hourlyRateCents <= 0 {
		return nil, fmt.Errorf("hourly rate must be positive, got %d", hourlyRateCents)
	}
	if maxTTL <= 0 {
		return nil, fmt.Errorf("max TTL must be positive, got %s", maxTTL)
	}

	available := assessment.State.AvailableCents

	/*
	 * Both branches fall through to ONE floor check below.
	 *
	 * The overage branch used to return here directly, which meant a client
	 * with allow_overage set got no minimum-rental check at all — the one
	 * client type that can spend past its cap was also the one that could buy a
	 * rental too short to do anything. Whatever the floor is, it has to apply to
	 * every client.
	 */
	var ttl time.Duration
	if assessment.Policy.AllowOverage {
		// Overage permitted: the cap stops bounding the TTL, so only the
		// operator's max TTL does.
		ttl = maxTTL
	} else {
		spendable := available - extraCents
		if spendable <= 0 {
			return nil, fmt.Errorf("%w: %d cents available, %d needed for storage/bandwidth alone",
				repository.ErrInsufficientBudget, available, extraCents)
		}

		// Hours the remaining budget buys, floored to whole seconds.
		affordable := time.Duration(float64(spendable) / float64(hourlyRateCents) * float64(time.Hour))
		ttl = affordable
		if ttl > maxTTL {
			ttl = maxTTL
		}
	}
	ttl = ttl.Truncate(time.Second)

	/*
	 * Refuse a rental that would be spent mostly on getting ready to work.
	 *
	 * The old test was a bare `ttl < 5 * time.Minute`, a number picked to mean
	 * "obviously too short" at a time when nothing in this package knew how long
	 * commissioning actually takes. It does now: MinRentalTTL derives the floor
	 * from the commissioning budget and the operator's tolerance for paying for
	 * it. Measured cold start on real AWS hardware was 138 seconds to register
	 * ALONE, before file sync and before the benchmark — a five-minute rental
	 * never had a chance.
	 *
	 * The two messages are deliberately different. Which bound is binding
	 * decides what the operator should go and change, and telling someone to top
	 * up a budget when their own TTL ceiling is the problem sends them to the
	 * wrong screen.
	 */
	if floor := MinRentalTTL(e.MaxCommissioningPct); ttl < floor {
		if maxTTL <= floor {
			return nil, fmt.Errorf(
				"%w: the client's maximum instance lifetime is %s, but commissioning alone takes about %s, "+
					"so a useful rental needs at least %s. Raise max_instance_ttl_minutes, "+
					"or raise cloud_max_commissioning_pct to accept less efficient rentals",
				ErrRentalTooShort, maxTTL, commissioningBudget, floor)
		}
		return nil, fmt.Errorf(
			"%w: the remaining budget buys %s at %d cents/hr, but commissioning alone takes about %s, "+
				"so a useful rental needs at least %s. Raise the client's budget, "+
				"or raise cloud_max_commissioning_pct to accept less efficient rentals",
			ErrRentalTooShort, ttl, hourlyRateCents, commissioningBudget, floor)
	}

	return &LaunchBudget{
		TTL:          ttl,
		ReserveCents: costFor(ttl, hourlyRateCents) + extraCents,
		Assessment:   assessment,
	}, nil
}

// Reserve commits a planned launch against the client's budget. This is the
// atomic check-and-commit; PlanLaunch alone guarantees nothing.
func (e *BudgetEngine) Reserve(
	ctx context.Context,
	clientID uuid.UUID,
	instanceID uuid.UUID,
	jobExecutionID *uuid.UUID,
	plan *LaunchBudget,
	note string,
) (*models.CloudBudgetState, error) {
	return e.repo.Reserve(ctx, clientID, instanceID, jobExecutionID,
		plan.ReserveCents, plan.Assessment.Policy.AllowOverage, note)
}

// AccrueInstance converts elapsed wall-clock into incurred spend and returns
// the amount newly recorded.
//
// billedFrom must be the PROVIDER's billing start, not our launch timestamp:
// Vast.ai bills storage from contract creation, well before the container is
// ready. Treating our own timestamp as the start systematically under-reports.
func (e *BudgetEngine) AccrueInstance(ctx context.Context, inst *models.CloudInstance, billedFrom, now time.Time) (int64, error) {
	delta := accrualDelta(inst, billedFrom, now)
	if delta <= 0 {
		return 0, nil
	}
	if err := e.repo.RecordIncurred(ctx, inst.ClientID, inst.ID, delta,
		fmt.Sprintf("accrual to %s", now.UTC().Format(time.RFC3339))); err != nil {
		return 0, err
	}
	return delta, nil
}

/*
 * accrualDelta computes how much NEW spend to record for an instance, given
 * how long it has been billing.
 *
 * Split out from AccrueInstance so the arithmetic — which decides what the
 * ledger says a client owes — is testable without a database.
 *
 * Returns 0 rather than a negative number when the recorded estimate is
 * already ahead of wall clock: accrual must never run backwards, or a clock
 * skew would hand back budget that was genuinely spent.
 */
func accrualDelta(inst *models.CloudInstance, billedFrom, now time.Time) int64 {
	if billedFrom.IsZero() || now.Before(billedFrom) {
		return 0
	}
	elapsed := now.Sub(billedFrom)
	// Never accrue past the TTL: beyond it the instance should be gone, and
	// charging further would silently eat budget the reaper already released.
	if inst.TTLEpoch.Valid {
		if capped := inst.TTLEpoch.Time.Sub(billedFrom); capped > 0 && elapsed > capped {
			elapsed = capped
		}
	}

	delta := costFor(elapsed, inst.HourlyRateCents) - inst.EstimatedCostCents
	if delta <= 0 {
		return 0
	}
	return delta
}

// SettleInstance releases the unused remainder of an instance's reservation at
// teardown, so a job that finished early hands its budget back.
func (e *BudgetEngine) SettleInstance(ctx context.Context, inst *models.CloudInstance) error {
	unused := inst.ReservedCents - inst.EstimatedCostCents
	if unused <= 0 {
		return nil
	}
	return e.repo.ReleaseUnused(ctx, inst.ClientID, inst.ID, unused,
		fmt.Sprintf("unused reservation for %s", inst.Label))
}

// costFor returns the cost of running for d at centsPerHour, rounded up to the
// nearest cent. Rounding up matters: rounding down lets a long tail of
// sub-cent remainders accumulate into real unbudgeted spend.
func costFor(d time.Duration, centsPerHour int) int64 {
	if d <= 0 || centsPerHour <= 0 {
		return 0
	}
	return int64(math.Ceil(d.Hours() * float64(centsPerHour)))
}
