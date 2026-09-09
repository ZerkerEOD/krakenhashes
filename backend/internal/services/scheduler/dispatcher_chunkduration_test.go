package scheduler

import "testing"

/*
 * TestResolveChunkDuration.
 *
 * The regression this pins: cloud_chunk_duration_seconds was applied and then
 * unconditionally overwritten by job_executions.chunk_size_seconds, a column
 * that is INT DEFAULT 900 and therefore never NULL and never 0. The documented
 * "NULL or 0 falls back to the system setting" branch was unreachable, so the
 * cloud value never applied to a single job and every rented GPU ran the
 * on-prem chunk size.
 *
 * Cases 1 and 2 are the ones that would have caught it. Case 3 is the on-prem
 * no-regression guard — it fails if anyone later applies the floor
 * unconditionally. Cases 8-12 are the TTL-clamp preservation contract: the
 * clamp is the only rule allowed to lower the result, and it must still be able
 * to lower it below the floor.
 */
func TestResolveChunkDuration(t *testing.T) {
	cases := []struct {
		name                                string
		targetSec, jobSec, cloudSec, minSec int
		ttl, slack                          int
		isCloud                             bool
		wantDur                             int
		wantSkip                            bool
	}{
		{
			// THE REGRESSION. 900 is the column DEFAULT from migration 000049,
			// which is what a job created without an explicit chunk size
			// carries. Before the fix this returned 900.
			name:      "cloud agent, job carries the column DEFAULT",
			targetSec: 1200, jobSec: 900, cloudSec: 3600, minSec: 5,
			ttl: 7200, slack: 120, isCloud: true,
			wantDur: 3600,
		},
		{
			// The value actually observed on every job in the dev database.
			name:      "cloud agent, job carries the live inherited value",
			targetSec: 1200, jobSec: 1200, cloudSec: 3600, minSec: 5,
			ttl: 7200, slack: 120, isCloud: true,
			wantDur: 3600,
		},
		{
			// The floor must never leak onto on-prem agents.
			name:      "on-prem is untouched by the cloud floor",
			targetSec: 1200, jobSec: 1200, cloudSec: 3600, minSec: 5,
			isCloud: false,
			wantDur: 1200,
		},
		{
			// max(), not assignment: upward authority stays with the operator.
			name:      "floor never lowers a longer job value",
			targetSec: 1200, jobSec: 7200, cloudSec: 3600, minSec: 5,
			ttl: 14400, slack: 120, isCloud: true,
			wantDur: 7200,
		},
		{
			// The documented escape hatch for a deployment that wants per-job
			// authority on rented hardware.
			name:      "cloud floor of 0 disables the floor",
			targetSec: 1200, jobSec: 1200, cloudSec: 0, minSec: 5,
			ttl: 7200, slack: 120, isCloud: true,
			wantDur: 1200,
		},
		{
			name:      "no job value, on-prem, falls back to the system target",
			targetSec: 1200, jobSec: 0, cloudSec: 3600, minSec: 5,
			isCloud: false,
			wantDur: 1200,
		},
		{
			name:      "no job value, cloud, takes the floor",
			targetSec: 1200, jobSec: 0, cloudSec: 3600, minSec: 5,
			ttl: 7200, slack: 120, isCloud: true,
			wantDur: 3600,
		},
		{
			// The clamp must still bind BELOW the floor, or the floor would let
			// a chunk outlive the instance it runs on.
			name:      "TTL clamp binds below the floor",
			targetSec: 1200, jobSec: 1200, cloudSec: 3600, minSec: 5,
			ttl: 900, slack: 120, isCloud: true,
			wantDur: 780,
		},
		{
			name:      "TTL clamp binds on the floor exactly",
			targetSec: 1200, jobSec: 0, cloudSec: 3600, minSec: 5,
			ttl: 3600, slack: 120, isCloud: true,
			wantDur: 3480,
		},
		{
			name:      "TTL exhausted skips, floor irrelevant",
			targetSec: 1200, jobSec: 1200, cloudSec: 3600, minSec: 5,
			ttl: 100, slack: 120, isCloud: true,
			wantSkip: true,
		},
		{
			// An unknown TTL reads as 0 from the map default. Pins the existing
			// behaviour: skip rather than dispatch unclamped.
			name:      "unknown TTL on a cloud agent skips",
			targetSec: 1200, jobSec: 1200, cloudSec: 3600, minSec: 5,
			ttl: 0, slack: 120, isCloud: true,
			wantSkip: true,
		},
		{
			// Boundary: affordable == minSec is dispatchable, not skipped.
			name:      "affordable exactly equal to minSec dispatches",
			targetSec: 1200, jobSec: 1200, cloudSec: 3600, minSec: 5,
			ttl: 125, slack: 120, isCloud: true,
			wantDur: 5,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveChunkDuration(
				tc.targetSec, tc.jobSec, tc.cloudSec, tc.minSec,
				tc.ttl, tc.slack, tc.isCloud,
			)
			if got.Skip != tc.wantSkip {
				t.Fatalf("Skip = %v, want %v (plan %+v)", got.Skip, tc.wantSkip, got)
			}
			if tc.wantSkip {
				return
			}
			if got.DurationSec != tc.wantDur {
				t.Errorf("DurationSec = %d, want %d", got.DurationSec, tc.wantDur)
			}
		})
	}
}
