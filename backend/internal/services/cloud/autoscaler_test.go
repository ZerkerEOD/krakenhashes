package cloud

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

/*
 * The autoscaler is the one component in this feature that spends money without
 * a human in the loop. Every test here is a bound on that: how many instances it
 * may rent, and the conditions under which it must rent none at all.
 */

// fakeProvisioner records what the autoscaler asked for and lets each call be
// made to fail independently.
type fakeProvisioner struct {
	mu sync.Mutex

	eligible    []EligibleJob
	eligibleErr error

	// perJobLive is the live instance count reported per job, incremented on
	// each successful provision so repeated ticks see the effect of the last.
	perJobLive map[uuid.UUID]int
	countErr   error

	provisionErr  error
	provisionCall []uuid.UUID
}

func newFakeProvisioner(jobs ...EligibleJob) *fakeProvisioner {
	return &fakeProvisioner{eligible: jobs, perJobLive: map[uuid.UUID]int{}}
}

func (f *fakeProvisioner) ProvisionForJob(_ context.Context, jobID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.provisionErr != nil {
		return f.provisionErr
	}
	f.provisionCall = append(f.provisionCall, jobID)
	f.perJobLive[jobID]++
	return nil
}

func (f *fakeProvisioner) LiveInstanceCountForJob(_ context.Context, jobID uuid.UUID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.countErr != nil {
		return 0, f.countErr
	}
	return f.perJobLive[jobID], nil
}

func (f *fakeProvisioner) CloudEligibleJobs(_ context.Context, candidates []uuid.UUID) ([]EligibleJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.eligibleErr != nil {
		return nil, f.eligibleErr
	}
	want := make(map[uuid.UUID]bool, len(candidates))
	for _, c := range candidates {
		want[c] = true
	}
	var out []EligibleJob
	for _, j := range f.eligible {
		if want[j.JobExecutionID] {
			out = append(out, j)
		}
	}
	return out, nil
}

func (f *fakeProvisioner) calls() []uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]uuid.UUID, len(f.provisionCall))
	copy(out, f.provisionCall)
	return out
}

// freshSnapshot publishes starving jobs so Read reports the data as current.
func freshSnapshot(idleOnPrem int, jobs ...uuid.UUID) *StarvationSnapshot {
	s := NewStarvationSnapshot()
	m := make(map[uuid.UUID]bool, len(jobs))
	for _, j := range jobs {
		m[j] = true
	}
	s.Publish(m, idleOnPrem)
	return s
}

/*
 * TestAutoscaler_RespectsGlobalInstanceCap is the outermost money bound. It is
 * the setting an operator reaches for when they want "never more than N rented
 * boxes, whatever else the system thinks".
 */
func TestAutoscaler_RespectsGlobalInstanceCap(t *testing.T) {
	jobA, jobB, jobC := uuid.New(), uuid.New(), uuid.New()
	prov := newFakeProvisioner(
		EligibleJob{JobExecutionID: jobA, MaxInstances: 10},
		EligibleJob{JobExecutionID: jobB, MaxInstances: 10},
		EligibleJob{JobExecutionID: jobC, MaxInstances: 10},
	)

	a := NewAutoscaler(freshSnapshot(0, jobA, jobB, jobC), prov)
	a.GlobalInstanceCap = 2
	// Counts what has actually been rented, which is what the real
	// CountLive does. A counter that only refreshed between passes would let
	// a single pass blow through the cap once per starving job.
	a.LiveInstanceCount = func(context.Context) (int, error) { return len(prov.calls()), nil }

	for i := 0; i < 5; i++ {
		a.ScaleOnce(context.Background())
	}

	if got := len(prov.calls()); got > 2 {
		t.Fatalf("rented %d instances with a global cap of 2", got)
	}
	if got := len(prov.calls()); got != 2 {
		t.Errorf("rented %d instances; the cap should have been reached, not undershot", got)
	}
}

/*
 * TestAutoscaler_GlobalCapWithoutCounterIsUnlimited documents a live footgun
 * rather than asserting desired behaviour.
 *
 * ScaleOnce only consults GlobalInstanceCap when LiveInstanceCount is also set,
 * so a cap configured without a counter silently enforces nothing. This test
 * exists so that if anyone ever changes that, they change it deliberately — and
 * so the main.go wiring that sets the pair together has a reason recorded.
 */
func TestAutoscaler_GlobalCapWithoutCounterIsUnlimited(t *testing.T) {
	jobA := uuid.New()
	prov := newFakeProvisioner(EligibleJob{JobExecutionID: jobA, MaxInstances: 10})

	a := NewAutoscaler(freshSnapshot(0, jobA), prov)
	a.GlobalInstanceCap = 1
	// LiveInstanceCount deliberately left nil.

	for i := 0; i < 3; i++ {
		a.ScaleOnce(context.Background())
	}

	if len(prov.calls()) == 1 {
		t.Fatal("the cap appears to be enforced without a counter — if that is now " +
			"true, delete this test; if it is not, main.go must keep setting " +
			"GlobalInstanceCap and LiveInstanceCount together")
	}
}

// TestAutoscaler_RespectsPerJobCap: cloud_max_instances bounds one job's spend
// independently of the global cap.
func TestAutoscaler_RespectsPerJobCap(t *testing.T) {
	jobA := uuid.New()
	prov := newFakeProvisioner(EligibleJob{JobExecutionID: jobA, MaxInstances: 2})
	a := NewAutoscaler(freshSnapshot(0, jobA), prov)

	for i := 0; i < 6; i++ {
		a.ScaleOnce(context.Background())
	}

	if got := len(prov.calls()); got != 2 {
		t.Fatalf("rented %d instances for a job capped at 2", got)
	}
}

/*
 * TestAutoscaler_StaleSnapshotDoesNothing.
 *
 * A stale snapshot means the scheduler is wedged or stopped. Acting on it would
 * rent GPUs for work that may have finished minutes ago, and — worse — would
 * keep renting on every tick for as long as the scheduler stayed down.
 */
func TestAutoscaler_StaleSnapshotDoesNothing(t *testing.T) {
	jobA := uuid.New()
	prov := newFakeProvisioner(EligibleJob{JobExecutionID: jobA, MaxInstances: 5})

	s := NewStarvationSnapshot()
	s.Publish(map[uuid.UUID]bool{jobA: true}, 0)
	// Backdate past the 30s freshness window used by ScaleOnce.
	s.mu.Lock()
	s.updatedAt = time.Now().Add(-31 * time.Second)
	s.mu.Unlock()

	NewAutoscaler(s, prov).ScaleOnce(context.Background())

	if len(prov.calls()) != 0 {
		t.Fatal("rented capacity from a stale scheduler snapshot")
	}
}

// TestAutoscaler_NeverPublishedDoesNothing: at boot the scheduler has not run a
// cycle yet, and an empty snapshot must not read as "nothing is starving, but
// also everything is fine".
func TestAutoscaler_NeverPublishedDoesNothing(t *testing.T) {
	jobA := uuid.New()
	prov := newFakeProvisioner(EligibleJob{JobExecutionID: jobA, MaxInstances: 5})

	NewAutoscaler(NewStarvationSnapshot(), prov).ScaleOnce(context.Background())

	if len(prov.calls()) != 0 {
		t.Fatal("rented capacity before the scheduler had published a single cycle")
	}
}

// TestAutoscaler_IdleOnPremSuppressesScaling: free capacity is always preferred
// to paid capacity.
func TestAutoscaler_IdleOnPremSuppressesScaling(t *testing.T) {
	jobA := uuid.New()
	prov := newFakeProvisioner(EligibleJob{JobExecutionID: jobA, MaxInstances: 5})

	NewAutoscaler(freshSnapshot(1, jobA), prov).ScaleOnce(context.Background())

	if len(prov.calls()) != 0 {
		t.Fatal("rented a GPU while an on-prem agent sat idle")
	}
}

// TestAutoscaler_IneligibleJobsAreNotProvisioned: starvation alone is not
// consent. cloud_burst_enabled, client funding and the provider allowlist are
// all checked by CloudEligibleJobs, and its answer is final.
func TestAutoscaler_IneligibleJobsAreNotProvisioned(t *testing.T) {
	starving, eligible := uuid.New(), uuid.New()
	prov := newFakeProvisioner(EligibleJob{JobExecutionID: eligible, MaxInstances: 5})

	NewAutoscaler(freshSnapshot(0, starving), prov).ScaleOnce(context.Background())

	if len(prov.calls()) != 0 {
		t.Fatalf("provisioned for a job that is not cloud-eligible: %v", prov.calls())
	}
}

// TestAutoscaler_EligibilityErrorDoesNotProvision: a failed eligibility lookup
// must not be read as "everything is eligible".
func TestAutoscaler_EligibilityErrorDoesNotProvision(t *testing.T) {
	jobA := uuid.New()
	prov := newFakeProvisioner(EligibleJob{JobExecutionID: jobA, MaxInstances: 5})
	prov.eligibleErr = errors.New("database unavailable")

	NewAutoscaler(freshSnapshot(0, jobA), prov).ScaleOnce(context.Background())

	if len(prov.calls()) != 0 {
		t.Fatal("rented capacity despite being unable to confirm eligibility")
	}
}

/*
 * TestAutoscaler_CountErrorSkipsJobRatherThanRenting.
 *
 * If the per-job live count cannot be read, the autoscaler cannot know whether
 * cloud_max_instances is already satisfied. Renting anyway would be unbounded
 * for as long as the error persisted.
 */
func TestAutoscaler_CountErrorSkipsJobRatherThanRenting(t *testing.T) {
	jobA := uuid.New()
	prov := newFakeProvisioner(EligibleJob{JobExecutionID: jobA, MaxInstances: 5})
	prov.countErr = errors.New("database unavailable")

	NewAutoscaler(freshSnapshot(0, jobA), prov).ScaleOnce(context.Background())

	if len(prov.calls()) != 0 {
		t.Fatal("rented capacity without being able to check the per-job cap")
	}
}

/*
 * TestAutoscaler_OneInstancePerJobPerPass.
 *
 * Each launch commits real money and takes minutes to become useful. Renting a
 * job's whole cap in one pass would spend it all before the first instance had
 * proven it can even register.
 */
func TestAutoscaler_OneInstancePerJobPerPass(t *testing.T) {
	jobA, jobB := uuid.New(), uuid.New()
	prov := newFakeProvisioner(
		EligibleJob{JobExecutionID: jobA, MaxInstances: 5},
		EligibleJob{JobExecutionID: jobB, MaxInstances: 5},
	)

	NewAutoscaler(freshSnapshot(0, jobA, jobB), prov).ScaleOnce(context.Background())

	calls := prov.calls()
	if len(calls) != 2 {
		t.Fatalf("expected one instance for each of two starving jobs, got %d", len(calls))
	}
	seen := map[uuid.UUID]int{}
	for _, c := range calls {
		seen[c]++
	}
	for job, n := range seen {
		if n != 1 {
			t.Errorf("job %s got %d instances in a single pass, want 1", job, n)
		}
	}
}

/*
 * TestAutoscaler_SpendsInSchedulerOrder.
 *
 * A constrained budget must be spent on the work the scheduler would dispatch
 * first, or an operator's priority setting means one thing to the scheduler and
 * something else to the wallet.
 */
func TestAutoscaler_SpendsInSchedulerOrder(t *testing.T) {
	low, high, oldSame := uuid.New(), uuid.New(), uuid.New()
	prov := newFakeProvisioner(
		EligibleJob{JobExecutionID: low, MaxInstances: 5, Priority: 1, CreatedAtNanos: 100},
		EligibleJob{JobExecutionID: high, MaxInstances: 5, Priority: 9, CreatedAtNanos: 300},
		EligibleJob{JobExecutionID: oldSame, MaxInstances: 5, Priority: 9, CreatedAtNanos: 50},
	)

	// A cap of 2 against 3 starving jobs forces the ordering to matter: one job
	// gets nothing, and which one is the whole assertion.
	a := NewAutoscaler(freshSnapshot(0, low, high, oldSame), prov)
	a.GlobalInstanceCap = 2
	a.LiveInstanceCount = func(context.Context) (int, error) { return len(prov.calls()), nil }

	a.ScaleOnce(context.Background())

	calls := prov.calls()
	if len(calls) == 0 {
		t.Fatal("nothing was provisioned")
	}
	// Priority 9 beats priority 1; within priority 9, the older job wins.
	if calls[0] != oldSame {
		t.Errorf("first spend went to %s; the highest-priority, oldest job is %s", calls[0], oldSame)
	}
	for _, c := range calls {
		if c == low {
			t.Error("the low-priority job was funded while priority-9 work was still starving")
		}
	}
}

/*
 * TestStarvationSnapshot_ReadIsACopy.
 *
 * ScaleOnce iterates the map it gets back while the scheduler keeps publishing
 * every 3 seconds. Handing out the live map would be a concurrent map read and
 * write — a hard crash, not a wrong number.
 */
func TestStarvationSnapshot_ReadIsACopy(t *testing.T) {
	jobA := uuid.New()
	s := freshSnapshot(0, jobA)

	got, _, ok := s.Read(time.Minute)
	if !ok {
		t.Fatal("a just-published snapshot must read as fresh")
	}
	got[uuid.New()] = true

	again, _, _ := s.Read(time.Minute)
	if len(again) != 1 {
		t.Fatalf("mutating a returned snapshot changed the published one (%d entries)", len(again))
	}
}

// TestStarvationSnapshot_ConcurrentPublishAndRead is the race-detector target
// for the 3-second publisher against the 60-second reader.
func TestStarvationSnapshot_ConcurrentPublishAndRead(t *testing.T) {
	s := NewStarvationSnapshot()
	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			s.Publish(map[uuid.UUID]bool{uuid.New(): true}, i%3)
		}
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if m, _, ok := s.Read(time.Minute); ok {
				for range m {
				}
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}
