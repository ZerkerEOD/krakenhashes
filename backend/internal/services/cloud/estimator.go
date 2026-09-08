package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/google/uuid"
)

/*
 * Estimator answers the two questions an operator asks before spending money:
 * "how long will this take?" and "will it finish before the budget runs out?"
 *
 * Everything is projected in BASE keyspace, never effective. The dispatcher
 * already documents why: for salted hash types effective_keyspace SHRINKS as
 * salts crack, so an effective-based projection drifts as the job progresses.
 * Base coverage is the drift-free denominator.
 *
 * The projection is therefore pessimistic for salted types — real throughput
 * improves as salts fall away — which is the safe direction for a spending
 * decision, and the UI says so rather than silently over-reserving.
 */
type Estimator struct {
	db *db.DB
}

// NewEstimator creates an estimator.
func NewEstimator(database *db.DB) *Estimator {
	return &Estimator{db: database}
}

// Projection is the answer for one job.
type Projection struct {
	JobExecutionID uuid.UUID `json:"job_execution_id"`

	RemainingBase int64 `json:"remaining_base_keyspace"`
	// OnPremSpeed and CloudSpeed are effective hashes/sec.
	OnPremSpeed int64 `json:"onprem_speed"`
	CloudSpeed  int64 `json:"cloud_speed"`

	// TimeToFinish is the projection at the combined speed. Zero when no
	// throughput is available, which the UI must render as "unknown", never
	// as "instant".
	//
	// Kept unexported to JSON and emitted through TimeToFinishSeconds below:
	// a time.Duration marshals as nanoseconds, so serialising it directly
	// under a field named "..._seconds" would hand the UI a number 1e9 too
	// large and turn every ETA into nonsense.
	TimeToFinish time.Duration `json:"-"`

	/*
	 * TimeToFinishKnown separates "finishes immediately" from "nothing is
	 * working on this", which TimeToFinish collapses onto the same zero.
	 *
	 * Every consumer previously had to reconstruct this from
	 * OnPremSpeed+CloudSpeed, and the comment above only asks the UI to get it
	 * right. That was survivable while the sole consumer was a dialog; it stops
	 * being survivable once a provisioning rule reads this value, because a job
	 * with no throughput is EXACTLY the starving job worth renting for, and
	 * reading its zero as "finishes instantly" refuses to provision at the
	 * precise moment provisioning is needed — silently, on every pass.
	 *
	 * False also when the remaining keyspace is zero or the projection floored
	 * to sub-second: in all three cases the duration carries no information.
	 */
	TimeToFinishKnown bool `json:"time_to_finish_known"`

	// ProjectedCostCents is what the cloud portion would cost over
	// TimeToFinish.
	ProjectedCostCents int64 `json:"projected_cost_cents"`
	AvailableCents     int64 `json:"available_cents"`

	/*
	 * CoveragePct is NPK's idea, and the most useful single number here:
	 * what percentage of the remaining work the budget can actually pay to
	 * complete. Below 100 means the money runs out first.
	 *
	 * NPK gated submission between 125% and 2400%; we surface the number and
	 * require an explicit confirmation below 100 rather than blocking, since
	 * partial progress is still progress and the operator may want it.
	 */
	CoveragePct float64 `json:"coverage_pct"`

	// WillFinish is CoveragePct >= 100.
	WillFinish bool `json:"will_finish"`
	// Pessimistic marks a salted projection, where real throughput will
	// exceed this estimate.
	Pessimistic bool   `json:"pessimistic"`
	Note        string `json:"note,omitempty"`
}

// MarshalJSON emits TimeToFinish in whole seconds, matching the field name the
// UI reads.
func (p Projection) MarshalJSON() ([]byte, error) {
	// Alias breaks the recursion into this method.
	type projectionAlias Projection
	return json.Marshal(struct {
		projectionAlias
		TimeToFinishSeconds int64 `json:"time_to_finish_seconds"`
	}{
		projectionAlias:     projectionAlias(p),
		TimeToFinishSeconds: int64(p.TimeToFinish.Seconds()),
	})
}

// Project estimates completion for a job with an optional additional cloud
// contribution.
func (e *Estimator) Project(
	ctx context.Context,
	jobID uuid.UUID,
	extraCloudSpeed int64,
	cloudHourlyRateCents int,
	availableCents int64,
) (*Projection, error) {
	p := &Projection{JobExecutionID: jobID, AvailableCents: availableCents}

	// Remaining BASE keyspace across every unit of the job, excluding failed
	// intervals (which have re-opened their range and are not coverage).
	err := e.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(su.base_keyspace), 0) - COALESCE((
			SELECT SUM(i.range_end - i.range_start)
			FROM job_keyspace_intervals i
			JOIN scheduling_units s2 ON s2.id = i.scheduling_unit_id
			WHERE s2.parent_job_id = $1 AND i.status <> 'failed'
		), 0)
		FROM scheduling_units su
		WHERE su.parent_job_id = $1`, jobID).Scan(&p.RemainingBase)
	if err != nil {
		return nil, fmt.Errorf("estimator: remaining keyspace: %w", err)
	}
	if p.RemainingBase < 0 {
		p.RemainingBase = 0
	}

	// Whether the job's hash type is salted, and the base/effective ratio we
	// need to convert an effective hash rate into base words/sec.
	var salted bool
	var baseKeyspace int64
	var effective string
	err = e.db.QueryRowContext(ctx, `
		SELECT COALESCE(ht.is_salted, false),
		       COALESCE(SUM(su.base_keyspace), 0),
		       COALESCE(SUM(su.effective_keyspace), 0)::text
		FROM scheduling_units su
		JOIN job_executions je ON je.id = su.parent_job_id
		JOIN hashlists h ON h.id = je.hashlist_id
		LEFT JOIN hash_types ht ON ht.id = h.hash_type_id
		WHERE su.parent_job_id = $1
		GROUP BY ht.is_salted`, jobID).Scan(&salted, &baseKeyspace, &effective)
	if err != nil {
		return nil, fmt.Errorf("estimator: keyspace shape: %w", err)
	}
	p.Pessimistic = salted
	if salted {
		p.Note = "salted hash type: effective keyspace shrinks as salts crack, so real throughput will exceed this projection"
	}

	/*
	 * Aggregate speed of agents currently on the job, split by kind.
	 *
	 * The salt_count match is load-bearing, not defensive. agent_benchmarks is
	 * unique on (agent, attack_mode, hash_type, salt_count), so a salted hash
	 * type legitimately has SEVERAL rows per (agent, attack_mode, hash_type) —
	 * one per salt count benchmarked. Joining without it made SUM(speed) add
	 * them all together and report a fleet several times faster than it is,
	 * which shortens every projection and makes the finishing-soon rule fire
	 * when it should not. Derived the same way HandleBenchmarkResult stores it:
	 * total_hashes for a salted type, NULL otherwise, compared NULL-safely.
	 */
	const speedSelect = `
		SELECT COALESCE(SUM(ab.speed), 0)
		FROM job_tasks t
		JOIN scheduling_units su ON su.id = t.scheduling_unit_id
		JOIN job_executions je ON je.id = su.parent_job_id
		JOIN hashlists h ON h.id = je.hashlist_id
		JOIN hash_types hty ON hty.id = h.hash_type_id
		JOIN agents a ON a.id = t.agent_id
		LEFT JOIN agent_benchmarks ab
		       ON ab.agent_id = a.id
		      AND ab.attack_mode = su.attack_mode
		      AND ab.hash_type = h.hash_type_id
		      AND ab.salt_count IS NOT DISTINCT FROM
		          (CASE WHEN hty.is_salted AND h.total_hashes > 0 THEN h.total_hashes END)
		WHERE su.parent_job_id = $1
		  AND t.status IN ('assigned','running')`

	if err := e.db.QueryRowContext(ctx,
		speedSelect+` AND a.cloud_instance_id IS NULL`, jobID).Scan(&p.OnPremSpeed); err != nil {
		return nil, fmt.Errorf("estimator: on-prem speed: %w", err)
	}

	/*
	 * Running cloud speed, added to the caller's hypothetical.
	 *
	 * Without this the projection saw only on-prem agents, so in a cloud-only
	 * deployment total was always 0, TimeToFinishKnown stayed false, and
	 * skip_if_finishing_within_seconds could never fire — one of the two
	 * brakes that stops the autoscaler renting for a job that is about to
	 * finish anyway.
	 *
	 * Kept as a separate query rather than dropping the cloud exclusion above,
	 * because OnPremSpeed and CloudSpeed are reported separately in the admin
	 * estimate dialog, and because extraCloudSpeed is a HYPOTHETICAL ("what if
	 * I add one more instance") — folding real cloud speed into that same
	 * field would double-count it on the manual path.
	 */
	var runningCloudSpeed int64
	if err := e.db.QueryRowContext(ctx,
		speedSelect+`
		  AND a.cloud_instance_id IS NOT NULL
		  AND EXISTS (
		        SELECT 1 FROM cloud_instances ci
		         WHERE ci.id = a.cloud_instance_id
		           AND ci.state NOT IN ('terminated','failed')
		      )`, jobID).Scan(&runningCloudSpeed); err != nil {
		return nil, fmt.Errorf("estimator: cloud speed: %w", err)
	}
	p.CloudSpeed = runningCloudSpeed + extraCloudSpeed

	total := p.OnPremSpeed + p.CloudSpeed
	if total <= 0 || p.RemainingBase <= 0 {
		return p, nil
	}

	// Convert the effective hash rate into base words/sec, then into a
	// duration. big.Int throughout: base x effective overflows int64 for a
	// large wordlist on a fast hash, and pre-dividing truncates the ratio to
	// zero for heavily-salted types.
	effBig, ok := new(big.Int).SetString(effective, 10)
	if !ok || effBig.Sign() <= 0 || baseKeyspace <= 0 {
		// No multiplier signal: treat the effective rate as a base rate.
		p.TimeToFinish = secondsToDuration(float64(p.RemainingBase) / float64(total))
	} else {
		// seconds = remainingBase * effective / (base * totalSpeed)
		num := new(big.Int).Mul(big.NewInt(p.RemainingBase), effBig)
		den := new(big.Int).Mul(big.NewInt(baseKeyspace), big.NewInt(total))
		if den.Sign() > 0 {
			p.TimeToFinish = bigSecondsToDuration(new(big.Int).Div(num, den))
		}
	}

	// Past the guard above there is real throughput and real work left, so the
	// duration means something — even if the division floored it to zero, which
	// genuinely is "under a second away".
	p.TimeToFinishKnown = true

	if cloudHourlyRateCents > 0 && p.TimeToFinish > 0 {
		p.ProjectedCostCents = costFor(p.TimeToFinish, cloudHourlyRateCents)
	}

	// Coverage: how much of the needed runtime the budget can pay for.
	switch {
	case p.ProjectedCostCents <= 0:
		// Nothing to pay for — on-prem alone finishes it.
		p.CoveragePct = 100
	case availableCents <= 0:
		p.CoveragePct = 0
	default:
		p.CoveragePct = (float64(availableCents) / float64(p.ProjectedCostCents)) * 100
	}
	p.WillFinish = p.CoveragePct >= 100

	return p, nil
}

/*
 * maxProjection is the largest span time.Duration can represent, ~292 years.
 *
 * Hashcat projections reach it easily and legitimately: a bcrypt hashlist with
 * a billion remaining base words and a five-figure rule multiplier, against one
 * weak GPU, is a genuine multi-century number. It is not an error to be
 * discarded — it is the answer, and "longer than the heat death of this
 * engagement" is exactly what the operator needs to see.
 */
const maxProjection = time.Duration(math.MaxInt64)

/*
 * bigSecondsToDuration converts whole seconds to a Duration, SATURATING at
 * maxProjection rather than wrapping or giving up.
 *
 * Both of the obvious alternatives produce a small positive duration from an
 * enormous one, which is the single most dangerous shape this value can take.
 *
 *   time.Duration(secs.Int64()) * time.Second wraps mod 2^64. 18_446_744_074
 *   seconds — about 585 years — becomes 290ms.
 *
 *   Skipping the assignment when !secs.IsInt64() leaves TimeToFinish at 0 while
 *   TimeToFinishKnown is still set true below, and this package's own warning
 *   is that a zero here means NO THROUGHPUT, never "instant".
 *
 * Either way a job needing six centuries is reported as finishing immediately.
 * Downstream that is not a cosmetic error: the skip-if-finishing-soon rule
 * refuses to rent for it on every pass, permanently and silently, and
 * ProjectedCostCents falls to zero, which drives CoveragePct to 100 and
 * WillFinish to true — "the budget covers this job" for a 500-year run.
 * Saturating keeps the value both huge and honest, so every one of those
 * comparisons lands the right way round.
 */
func bigSecondsToDuration(secs *big.Int) time.Duration {
	if secs.Sign() <= 0 {
		return 0
	}
	if secs.Cmp(big.NewInt(int64(maxProjection/time.Second))) >= 0 {
		return maxProjection
	}
	return time.Duration(secs.Int64()) * time.Second
}

// secondsToDuration is the float64 counterpart, saturating for the same
// reasons. NaN and +Inf are possible here — the caller divides by a speed it
// only knows to be non-zero — and both must land on a value that reads as "not
// finishing soon" rather than as zero.
func secondsToDuration(secs float64) time.Duration {
	if math.IsNaN(secs) {
		return maxProjection
	}
	if secs <= 0 {
		return 0
	}
	if secs >= float64(maxProjection/time.Second) {
		return maxProjection
	}
	return time.Duration(secs * float64(time.Second))
}
