package cloud

import (
	"math"
	"math/big"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"testing"
	"time"
)

/*
 * TestBigSecondsToDurationSaturates is the regression guard for a wrap that
 * turned a six-century projection into 290 milliseconds.
 *
 * The old code was `time.Duration(secs.Int64()) * time.Second` behind an
 * `IsInt64()` check. The check guards the Int64() conversion but not the
 * multiply by 1e9, so any projection over ~292 years wraps mod 2^64 into a
 * small positive number that looks entirely plausible downstream.
 *
 * Both failure directions are load-bearing, and both fire on the same value:
 * the skip-if-finishing-soon rule refuses to rent for the job forever, and
 * ProjectedCostCents collapses to zero, which sets CoveragePct to 100 and
 * WillFinish to true — the budget reported as covering a 500-year run.
 */
func TestBigSecondsToDurationSaturates(t *testing.T) {
	// The exact value from the bcrypt scenario: ~585 years in seconds. Under
	// the old arithmetic this produced 290ms.
	wrapping := big.NewInt(18_446_744_074)
	got := bigSecondsToDuration(wrapping)

	if got != maxProjection {
		t.Errorf("585 years must saturate, got %v", got)
	}
	if got < time.Hour {
		t.Errorf("a multi-century projection reported as %v — this is the wrap "+
			"that makes a starving job look like it finishes instantly", got)
	}

	// Well past int64 nanoseconds AND past int64 seconds, so IsInt64() is false
	// too. The old code left TimeToFinish at zero here, which the package's own
	// warning says means "no throughput", not "instant".
	astronomical := new(big.Int).Mul(big.NewInt(math.MaxInt64), big.NewInt(1000))
	if got := bigSecondsToDuration(astronomical); got != maxProjection {
		t.Errorf("a projection too large for int64 seconds must saturate, not fall to %v", got)
	}

	// Exactly at the boundary saturates rather than wrapping to minInt64.
	atLimit := big.NewInt(int64(maxProjection / time.Second))
	if got := bigSecondsToDuration(atLimit); got != maxProjection {
		t.Errorf("the boundary second must saturate, got %v", got)
	}
	if got := bigSecondsToDuration(new(big.Int).Sub(atLimit, big.NewInt(1))); got <= 0 {
		t.Errorf("one second under the limit must stay positive, got %v", got)
	}
}

// TestBigSecondsToDurationOrdinaryValues: saturation must not distort the range
// every real job lives in.
func TestBigSecondsToDurationOrdinaryValues(t *testing.T) {
	for _, secs := range []int64{0, 1, 59, 3600, 86_400, 31_536_000} {
		want := time.Duration(secs) * time.Second
		if got := bigSecondsToDuration(big.NewInt(secs)); got != want {
			t.Errorf("bigSecondsToDuration(%d) = %v, want %v", secs, got, want)
		}
	}
	// Negatives are not a duration; the estimator treats them as no projection.
	if got := bigSecondsToDuration(big.NewInt(-5)); got != 0 {
		t.Errorf("negative seconds must be 0, got %v", got)
	}
}

/*
 * TestSecondsToDurationSaturates covers the float path, which the estimator
 * takes whenever there is no multiplier signal.
 *
 * NaN is reachable there: the division is remainingBase/total, and while total
 * is known non-zero, 0/0 and Inf/Inf both arrive as NaN through float
 * conversion of big values. NaN must land on "not finishing soon" — a NaN
 * compared against any window is false, so letting it through as 0 would make
 * the finishing-soon rule refuse on a value that means nothing at all.
 */
func TestSecondsToDurationSaturates(t *testing.T) {
	cases := []struct {
		name string
		secs float64
		want time.Duration
	}{
		{"ordinary", 90, 90 * time.Second},
		{"sub-second", 0.5, 500 * time.Millisecond},
		{"zero", 0, 0},
		{"negative", -1, 0},
		{"past int64 nanoseconds", 1e12, maxProjection},
		{"positive infinity", math.Inf(1), maxProjection},
		{"not a number", math.NaN(), maxProjection},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := secondsToDuration(tc.secs); got != tc.want {
				t.Errorf("secondsToDuration(%v) = %v, want %v", tc.secs, got, tc.want)
			}
		})
	}
}

/*
 * TestSaturatedProjectionIsNotFinishingSoon closes the loop on the reason any
 * of this matters: the saturated value has to survive contact with the rule
 * that spends money.
 */
func TestSaturatedProjectionIsNotFinishingSoon(t *testing.T) {
	in := healthyInput()
	in.TimeToFinish = bigSecondsToDuration(big.NewInt(18_446_744_074))
	in.HaveThroughput = true
	in.ProjectionAvailable = true

	rules := &models.CloudProvisioningRules{
		SkipIfFinishingWithinSeconds: intPtr(900),
	}

	allowed, reason := decideProvisioningAction(in, rules)
	if !allowed {
		t.Errorf("a job projected to run for centuries must not be skipped as "+
			"finishing soon, refused with %q", reason)
	}
}
