package cloud

import (
	"context"
	"encoding/json"
	"fmt"
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

	// Aggregate speed of agents currently on the job.
	if err := e.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(ab.speed), 0)
		FROM job_tasks t
		JOIN scheduling_units su ON su.id = t.scheduling_unit_id
		JOIN job_executions je ON je.id = su.parent_job_id
		JOIN hashlists h ON h.id = je.hashlist_id
		JOIN agents a ON a.id = t.agent_id
		LEFT JOIN agent_benchmarks ab
		       ON ab.agent_id = a.id AND ab.attack_mode = su.attack_mode AND ab.hash_type = h.hash_type_id
		WHERE su.parent_job_id = $1
		  AND t.status IN ('assigned','running')
		  AND a.cloud_instance_id IS NULL`, jobID).Scan(&p.OnPremSpeed); err != nil {
		return nil, fmt.Errorf("estimator: on-prem speed: %w", err)
	}
	p.CloudSpeed = extraCloudSpeed

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
		p.TimeToFinish = time.Duration(float64(p.RemainingBase)/float64(total)) * time.Second
	} else {
		// seconds = remainingBase * effective / (base * totalSpeed)
		num := new(big.Int).Mul(big.NewInt(p.RemainingBase), effBig)
		den := new(big.Int).Mul(big.NewInt(baseKeyspace), big.NewInt(total))
		if den.Sign() > 0 {
			secs := new(big.Int).Div(num, den)
			if secs.IsInt64() {
				p.TimeToFinish = time.Duration(secs.Int64()) * time.Second
			}
		}
	}

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
