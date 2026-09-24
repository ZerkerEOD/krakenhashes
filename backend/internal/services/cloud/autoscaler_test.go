package cloud

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
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

	// perJobDOA is how many of a job's instances billed and died without the
	// agent ever registering.
	perJobDOA map[uuid.UUID]int
	doaErr    error

	// globalDOA is the deployment-wide consecutive streak.
	globalDOA    int
	globalDOAErr error

	// perJobCommissioning is how many of a job's live instances have not yet
	// been given a task. Zero by default so the existing tests describe a fleet
	// of WARM instances, which is what they were written against; the settling
	// rule is exercised by the tests that set this explicitly.
	perJobCommissioning map[uuid.UUID]int
}

func (f *fakeProvisioner) ConsecutiveDeadOnArrivals(_ context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.globalDOAErr != nil {
		return 0, f.globalDOAErr
	}
	return f.globalDOA, nil
}

func newFakeProvisioner(jobs ...EligibleJob) *fakeProvisioner {
	return &fakeProvisioner{
		eligible:            jobs,
		perJobLive:          map[uuid.UUID]int{},
		perJobDOA:           map[uuid.UUID]int{},
		perJobCommissioning: map[uuid.UUID]int{},
	}
}

func (f *fakeProvisioner) DeadOnArrivalCountForJob(_ context.Context, jobID uuid.UUID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.doaErr != nil {
		return 0, f.doaErr
	}
	return f.perJobDOA[jobID], nil
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

func (f *fakeProvisioner) JobInstanceState(_ context.Context, jobID uuid.UUID) (int, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.countErr != nil {
		return 0, 0, f.countErr
	}
	return f.perJobLive[jobID], f.perJobCommissioning[jobID], nil
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

// freshSnapshot publishes starving jobs so ReadAges reports them as current.
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
 * TestAutoscaler_GlobalCapWithoutCounterRefuses pins the fail-closed direction
 * of a half-wired cap.
 *
 * GlobalInstanceCap and LiveInstanceCount are separate fields set by whoever
 * builds the autoscaler, so forgetting the counter is a one-line mistake at any
 * future construction site. Skipping the check in that case turns an operator's
 * "never more than N rented boxes" into unlimited, silently — no error, no log,
 * and no test failure. Refusing makes the same mistake loud and costs nothing
 * but a paused autoscaler.
 *
 * The assertion is on an exact call count in both directions, because the
 * previous version of this test (`if len(calls) == 1 { fail }`) also passed when
 * provisioning was broken outright and would have passed if the cap were made
 * to fail closed — it pinned neither outcome.
 */
func TestAutoscaler_GlobalCapWithoutCounterRefuses(t *testing.T) {
	jobA := uuid.New()
	prov := newFakeProvisioner(EligibleJob{JobExecutionID: jobA, MaxInstances: 10})

	a := NewAutoscaler(freshSnapshot(0, jobA), prov)
	a.GlobalInstanceCap = 1
	// LiveInstanceCount deliberately left nil: the half-wired case.

	for i := 0; i < 3; i++ {
		a.ScaleOnce(context.Background())
	}

	if n := len(prov.calls()); n != 0 {
		t.Fatalf("a configured global cap with no counter must refuse to provision, got %d launches — "+
			"the operator's instance ceiling is silently unlimited", n)
	}
}

// TestAutoscaler_GlobalCapWithCounterStillProvisions is the control for the test
// above: fail-closed must apply to the missing counter, not to the cap itself.
func TestAutoscaler_GlobalCapWithCounterStillProvisions(t *testing.T) {
	jobA := uuid.New()
	prov := newFakeProvisioner(EligibleJob{JobExecutionID: jobA, MaxInstances: 10})

	a := NewAutoscaler(freshSnapshot(0, jobA), prov)
	a.GlobalInstanceCap = 5
	a.LiveInstanceCount = func(context.Context) (int, error) { return 0, nil }

	a.ScaleOnce(context.Background())

	if n := len(prov.calls()); n != 1 {
		t.Fatalf("a properly wired cap below its ceiling must still provision, got %d launches", n)
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
 * TestStarvationSnapshot_ReadAgesIsACopy.
 *
 * ScaleOnce iterates the map it gets back while the scheduler keeps publishing
 * every 3 seconds. Handing out the live map would be a concurrent map read and
 * write — a hard crash, not a wrong number.
 */
func TestStarvationSnapshot_ReadAgesIsACopy(t *testing.T) {
	jobA := uuid.New()
	s := freshSnapshot(0, jobA)

	got, _, ok := s.ReadAges(time.Minute, time.Now())
	if !ok {
		t.Fatal("a just-published snapshot must read as fresh")
	}
	got[uuid.New()] = time.Hour

	again, _, _ := s.ReadAges(time.Minute, time.Now())
	if len(again) != 1 {
		t.Fatalf("mutating a returned snapshot changed the published one (%d entries)", len(again))
	}
}

/*
 * TestStarvationSnapshot_AgeAccumulatesAcrossPublishes.
 *
 * This is the property min_starvation_seconds is built on. Publish replaces the
 * set wholesale, so the obvious implementation restamps every job every 3
 * seconds and no job ever ages past one scheduler cycle — a threshold of
 * minutes would then be met either instantly or never, depending on which side
 * of the restamp the reader landed.
 */
func TestStarvationSnapshot_AgeAccumulatesAcrossPublishes(t *testing.T) {
	jobA := uuid.New()
	s := NewStarvationSnapshot()

	s.Publish(map[uuid.UUID]bool{jobA: true}, 0)
	time.Sleep(50 * time.Millisecond)
	s.Publish(map[uuid.UUID]bool{jobA: true}, 0)

	ages, _, ok := s.ReadAges(time.Minute, time.Now())
	if !ok {
		t.Fatal("a just-published snapshot must read as fresh")
	}
	if ages[jobA] < 50*time.Millisecond {
		t.Fatalf("age reset to %s on the second publish; the first-seen time must carry forward", ages[jobA])
	}
}

/*
 * TestStarvationSnapshot_ReEnteringRestartsTheClock.
 *
 * The rule asks whether a job has been unable to make ANY progress for N
 * seconds, so getting an agent has to zero the clock. A high-water mark would
 * let a job that starves briefly over and over eventually qualify for paid
 * capacity it never needed.
 *
 * Asserted as the gap between two jobs read at the SAME instant: jobB starved
 * throughout, jobA dropped out and came back, so under a high-water mark their
 * ages would be equal. Comparing the two removes any dependence on how long the
 * test itself took to reach the read.
 */
func TestStarvationSnapshot_ReEnteringRestartsTheClock(t *testing.T) {
	jobA, jobB := uuid.New(), uuid.New()
	s := NewStarvationSnapshot()

	s.Publish(map[uuid.UUID]bool{jobA: true, jobB: true}, 0)
	time.Sleep(50 * time.Millisecond)
	// jobA got an agent: it has made progress, so its run ends here.
	s.Publish(map[uuid.UUID]bool{jobB: true}, 0)
	time.Sleep(50 * time.Millisecond)
	s.Publish(map[uuid.UUID]bool{jobA: true, jobB: true}, 0)

	ages, _, ok := s.ReadAges(time.Minute, time.Now())
	if !ok {
		t.Fatal("a just-published snapshot must read as fresh")
	}
	if gap := ages[jobB] - ages[jobA]; gap < 90*time.Millisecond {
		t.Fatalf("the re-entering job is only %s younger than the job that never recovered; "+
			"its clock kept running across a cycle in which it was not starving", gap)
	}
}

/*
 * TestStarvationSnapshot_DepartedJobsAreDropped.
 *
 * The age map is process-lifetime state fed by a 3-second publisher. If entries
 * outlived their starvation it would grow with every job the server ever ran,
 * and a job returning hours later would arrive pre-aged past any threshold.
 */
func TestStarvationSnapshot_DepartedJobsAreDropped(t *testing.T) {
	jobA, jobB := uuid.New(), uuid.New()
	s := NewStarvationSnapshot()

	s.Publish(map[uuid.UUID]bool{jobA: true, jobB: true}, 0)
	s.Publish(map[uuid.UUID]bool{jobA: true}, 0)

	ages, _, _ := s.ReadAges(time.Minute, time.Now())
	if _, ok := ages[jobB]; ok {
		t.Error("a job the scheduler stopped reporting is still being aged")
	}

	s.mu.RLock()
	tracked := len(s.startedAt)
	s.mu.RUnlock()
	if tracked != 1 {
		t.Fatalf("tracking %d jobs after a publish naming one; entries must be dropped, not retained", tracked)
	}
}

/*
 * TestStarvationSnapshot_FreshnessBounds.
 *
 * Both directions are money guards. Before the first publish there is no
 * starvation data at all, and an empty age map must not read as "nothing is
 * starving and everything is fine"; past maxAge the scheduler is wedged and the
 * ages are counting up against work that may already be finished.
 */
func TestStarvationSnapshot_FreshnessBounds(t *testing.T) {
	jobA := uuid.New()
	s := NewStarvationSnapshot()

	if _, _, ok := s.ReadAges(30*time.Second, time.Now()); ok {
		t.Fatal("an unpublished snapshot read as fresh")
	}

	s.Publish(map[uuid.UUID]bool{jobA: true}, 0)
	if _, _, ok := s.ReadAges(30*time.Second, time.Now()); !ok {
		t.Fatal("a just-published snapshot must read as fresh")
	}
	if _, _, ok := s.ReadAges(30*time.Second, time.Now().Add(31*time.Second)); ok {
		t.Fatal("a snapshot older than maxAge read as fresh")
	}
}

// TestStarvationSnapshot_ConcurrentPublishAndReadAges is the race-detector
// target for the 3-second publisher against the 60-second reader. Publish now
// reads the previous run's start times while it writes the new ones, so the
// window in which an unlocked read could tear is wider than it was.
func TestStarvationSnapshot_ConcurrentPublishAndReadAges(t *testing.T) {
	s := NewStarvationSnapshot()
	// A job present in every publish keeps the carry-forward path hot rather
	// than exercising only first-sighting of brand new UUIDs.
	stable := uuid.New()
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
			s.Publish(map[uuid.UUID]bool{stable: true, uuid.New(): true}, i%3)
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
			if m, _, ok := s.ReadAges(time.Minute, time.Now()); ok {
				for range m {
				}
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}

/*
 * TestAutoscaler_HaltsAfterDeadOnArrivalInstances covers the failure that live
 * AWS testing surfaced and that no existing rail caught.
 *
 * The instance launches, bills, cannot reach the backend over the VPN and
 * self-destructs a few minutes later. ProvisionForJob returned nil, so nothing
 * in the error path fires; the job is still starving on the next tick, so the
 * autoscaler rents again. Observed live: three launches in seven minutes, on
 * course to bill indefinitely with every step reporting success.
 *
 * Asserting an exact count matters here. A test that only checked "fewer than
 * ten" would pass against a backoff that merely slowed the drain, and the point
 * of the rail is that this failure never recovers on its own.
 */
func TestAutoscaler_HaltsAfterDeadOnArrivalInstances(t *testing.T) {
	jobA := uuid.New()
	prov := newFakeProvisioner(EligibleJob{JobExecutionID: jobA, MaxInstances: 10})

	a := NewAutoscaler(freshSnapshot(0, jobA), prov)

	// Each pass rents one instance which then dies without ever registering,
	// exactly as the VPN-unreachable case does: the live count returns to zero
	// and the dead-on-arrival tally grows.
	for i := 0; i < 10; i++ {
		a.ScaleOnce(context.Background())
		prov.mu.Lock()
		if prov.perJobLive[jobA] > 0 {
			prov.perJobLive[jobA] = 0
			prov.perJobDOA[jobA]++
		}
		prov.mu.Unlock()
	}

	if n := len(prov.calls()); n != defaultDeadOnArrivalLimit {
		t.Fatalf("a job whose instances never register must stop being re-rented after %d attempts, "+
			"got %d launches — each one is a full instance-launch of billing that bought nothing",
			defaultDeadOnArrivalLimit, n)
	}
}

/*
 * TestAutoscaler_DeadOnArrivalCountErrorRefuses: the rail exists to stop money
 * leaving on a failure nothing else reports, so an unreadable tally has to
 * behave like a tripped breaker rather than an absent one.
 */
func TestAutoscaler_DeadOnArrivalCountErrorRefuses(t *testing.T) {
	jobA := uuid.New()
	prov := newFakeProvisioner(EligibleJob{JobExecutionID: jobA, MaxInstances: 10})
	prov.doaErr = errors.New("database unavailable")

	a := NewAutoscaler(freshSnapshot(0, jobA), prov)
	a.ScaleOnce(context.Background())

	if n := len(prov.calls()); n != 0 {
		t.Fatalf("an unreadable dead-on-arrival count must refuse to provision, got %d launches", n)
	}
}

/*
 * TestAutoscaler_DeadOnArrivalDoesNotBlockScaleUp is the control. The tally is a
 * lifetime total, so a job that recovered and now holds a healthy instance must
 * still be able to add a second one — otherwise past failures would
 * permanently cap a job that is currently working.
 */
func TestAutoscaler_DeadOnArrivalDoesNotBlockScaleUp(t *testing.T) {
	jobA := uuid.New()
	prov := newFakeProvisioner(EligibleJob{JobExecutionID: jobA, MaxInstances: 10})
	prov.perJobDOA[jobA] = 99
	prov.perJobLive[jobA] = 1 // one instance registered and is working

	a := NewAutoscaler(freshSnapshot(0, jobA), prov)
	a.ScaleOnce(context.Background())

	if n := len(prov.calls()); n != 1 {
		t.Fatalf("a job with a live registered instance must still scale up despite past "+
			"dead-on-arrival launches, got %d launches", n)
	}
}

/*
 * TestAutoscaler_GlobalDeadOnArrivalHaltsEverything.
 *
 * The per-job breaker cannot see a broken DEPLOYMENT: a wrong backend address
 * or a lapsed VPN credential fails identically for every job, and the per-job
 * count starts at zero for each new one. Without this rail an operator who
 * responds to a stuck job by creating another one pays the full per-job limit
 * again, indefinitely.
 *
 * Asserted across several passes and several DIFFERENT jobs, because a version
 * that only halted the job it first saw fail would pass a single-job test.
 */
func TestAutoscaler_GlobalDeadOnArrivalHaltsEverything(t *testing.T) {
	jobA, jobB := uuid.New(), uuid.New()
	prov := newFakeProvisioner(
		EligibleJob{JobExecutionID: jobA, MaxInstances: 10},
		EligibleJob{JobExecutionID: jobB, MaxInstances: 10},
	)
	prov.globalDOA = defaultGlobalDeadOnArrivalLimit // the deployment is broken

	a := NewAutoscaler(freshSnapshot(0, jobA, jobB), prov)
	for i := 0; i < 5; i++ {
		a.ScaleOnce(context.Background())
	}

	if n := len(prov.calls()); n != 0 {
		t.Fatalf("provisioned %d instance(s) while the last %d launches all failed to register; "+
			"a broken deployment must stop spending, not restart per job", n, prov.globalDOA)
	}
}

/*
 * TestAutoscaler_GlobalDeadOnArrivalClearsOnSuccess: the streak is the reset
 * mechanism. One instance that registers proves rented hardware can reach the
 * backend, and the autoscaler must resume — otherwise the rail is a permanent
 * off switch and the only recovery is editing the database.
 */
func TestAutoscaler_GlobalDeadOnArrivalClearsOnSuccess(t *testing.T) {
	jobA := uuid.New()
	prov := newFakeProvisioner(EligibleJob{JobExecutionID: jobA, MaxInstances: 10})
	prov.globalDOA = defaultGlobalDeadOnArrivalLimit

	a := NewAutoscaler(freshSnapshot(0, jobA), prov)
	a.ScaleOnce(context.Background())
	if n := len(prov.calls()); n != 0 {
		t.Fatalf("expected the breaker to hold, got %d launches", n)
	}

	// An operator fixes the configuration and provisions one by hand; it
	// registers, so the streak is broken.
	prov.mu.Lock()
	prov.globalDOA = 0
	prov.mu.Unlock()

	a.ScaleOnce(context.Background())
	if n := len(prov.calls()); n != 1 {
		t.Fatalf("a cleared streak must release the autoscaler, got %d launches", n)
	}
}

// TestAutoscaler_GlobalDeadOnArrivalErrorRefuses: an unreadable streak must
// behave like a tripped breaker, matching every other money rail here.
func TestAutoscaler_GlobalDeadOnArrivalErrorRefuses(t *testing.T) {
	jobA := uuid.New()
	prov := newFakeProvisioner(EligibleJob{JobExecutionID: jobA, MaxInstances: 10})
	prov.globalDOAErr = errors.New("database unavailable")

	a := NewAutoscaler(freshSnapshot(0, jobA), prov)
	a.ScaleOnce(context.Background())

	if n := len(prov.calls()); n != 0 {
		t.Fatalf("an unreadable dead-on-arrival streak must refuse to provision, got %d launches", n)
	}
}

/*
 * TestAutoscaler_WaitsForACommissioningInstance (the settling rule).
 *
 * A job publishes as starving for as long as it gets no NEW allocation, which
 * includes the entire time a freshly rented instance spends booting, syncing a
 * ~467 MB hashcat archive and benchmarking. Counting only live instances made
 * the autoscaler rent another one on every 60-second tick throughout that
 * window — a burst of ten to twenty paid instances for a job that asked for
 * one, with the per-job cap frequently blank and therefore unlimited.
 *
 * MaxInstances is deliberately 0 (uncapped) here so the ONLY thing that can
 * stop the second rental is the settling rule.
 */
func TestAutoscaler_WaitsForACommissioningInstance(t *testing.T) {
	jobA := uuid.New()
	prov := newFakeProvisioner(EligibleJob{JobExecutionID: jobA, MaxInstances: 0})
	a := NewAutoscaler(freshSnapshot(0, jobA), prov)

	// First pass rents one. ProvisionForJob bumps perJobLive; mark it as still
	// commissioning, which is what a real instance is for its first minutes.
	a.ScaleOnce(context.Background())
	prov.mu.Lock()
	prov.perJobCommissioning[jobA] = 1
	prov.mu.Unlock()

	for i := 0; i < 10; i++ {
		a.ScaleOnce(context.Background())
	}

	if got := len(prov.calls()); got != 1 {
		t.Fatalf("rented %d instances while the first was still commissioning; want 1. "+
			"Ten ticks against a cold start is exactly the burst this rule exists to stop", got)
	}
}

// Once the instance has taken work it is no longer commissioning, and normal
// scaling must resume — otherwise the settling rule would be a permanent cap of
// one and cloud burst could never scale a job out.
func TestAutoscaler_ResumesOnceInstanceTakesWork(t *testing.T) {
	jobA := uuid.New()
	prov := newFakeProvisioner(EligibleJob{JobExecutionID: jobA, MaxInstances: 3})
	a := NewAutoscaler(freshSnapshot(0, jobA), prov)

	a.ScaleOnce(context.Background())
	prov.mu.Lock()
	prov.perJobCommissioning[jobA] = 1
	prov.mu.Unlock()
	a.ScaleOnce(context.Background())

	if got := len(prov.calls()); got != 1 {
		t.Fatalf("expected the settling rule to hold at 1, got %d", got)
	}

	// The instance takes a task.
	prov.mu.Lock()
	prov.perJobCommissioning[jobA] = 0
	prov.mu.Unlock()

	a.ScaleOnce(context.Background())
	if got := len(prov.calls()); got != 2 {
		t.Fatalf("after the first instance took work the autoscaler rented %d total; want 2. "+
			"The settling rule must reopen, not become a permanent cap of one", got)
	}
}

// fakeDiag records what the autoscaler would surface to an operator.
type fakeDiag struct {
	mu       sync.Mutex
	recorded []string // "reason|detail"
	cleared  []string
}

func (f *fakeDiag) Record(scope, scopeID, reason, severity, detail string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recorded = append(f.recorded, reason+"|"+detail)
}

func (f *fakeDiag) ClearScope(scope, scopeID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleared = append(f.cleared, scopeID)
}

func (f *fakeDiag) reasons() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.recorded))
	for _, r := range f.recorded {
		out = append(out, strings.SplitN(r, "|", 2)[0])
	}
	return out
}

/*
 * In a cloud-only deployment this diagnostic is the ONLY signal an operator
 * gets. The per-agent path cannot serve them: recordIdleReasons iterates agents
 * and is not even reached when there are none, which is precisely their
 * situation. So a refusal that records nothing is a job sitting at pending with
 * no explanation anywhere in the product.
 */
func TestAutoscaler_RecordsWhyItRefused(t *testing.T) {
	jobA := uuid.New()
	prov := newFakeProvisioner(EligibleJob{JobExecutionID: jobA, MaxInstances: 0})
	prov.provisionErr = errors.New("budget: provisioning blocked: spend at 103.3% of cap (hard stop at 100%)")

	diag := &fakeDiag{}
	a := NewAutoscaler(freshSnapshot(0, jobA), prov)
	a.Diagnostics = diag

	a.ScaleOnce(context.Background())

	got := diag.reasons()
	if len(got) != 1 || got[0] != models.DiagReasonCloudBudgetBlocked {
		t.Fatalf("recorded %v, want exactly [%s]", got, models.DiagReasonCloudBudgetBlocked)
	}
	if !strings.Contains(diag.recorded[0], "103.3%") {
		t.Errorf("detail lost the actual reason: %q", diag.recorded[0])
	}
}

// The settling rule is a normal, self-resolving wait. It still has to be
// visible, or "nothing is happening" looks identical to a fault.
func TestAutoscaler_RecordsSettlingWait(t *testing.T) {
	jobA := uuid.New()
	prov := newFakeProvisioner(EligibleJob{JobExecutionID: jobA, MaxInstances: 0})
	diag := &fakeDiag{}
	a := NewAutoscaler(freshSnapshot(0, jobA), prov)
	a.Diagnostics = diag

	a.ScaleOnce(context.Background())
	prov.mu.Lock()
	prov.perJobCommissioning[jobA] = 1
	prov.mu.Unlock()
	a.ScaleOnce(context.Background())

	found := false
	for _, r := range diag.reasons() {
		if r == models.DiagReasonCloudSettling {
			found = true
		}
	}
	if !found {
		t.Errorf("recorded %v, want %s among them", diag.reasons(), models.DiagReasonCloudSettling)
	}
}

// A successful provision must clear the job's stale reasons, or "budget
// blocked" lingers on screen after the operator has raised the budget.
func TestAutoscaler_ClearsDiagnosticsOnSuccess(t *testing.T) {
	jobA := uuid.New()
	prov := newFakeProvisioner(EligibleJob{JobExecutionID: jobA, MaxInstances: 5})
	diag := &fakeDiag{}
	a := NewAutoscaler(freshSnapshot(0, jobA), prov)
	a.Diagnostics = diag

	a.ScaleOnce(context.Background())

	if len(diag.cleared) != 1 || diag.cleared[0] != jobA.String() {
		t.Errorf("cleared %v, want the provisioned job's scope cleared", diag.cleared)
	}
}

// The classifier is what makes the message actionable: "out of money" and "AWS
// had no capacity" need opposite responses from the operator.
func TestClassifyProvisionFailure(t *testing.T) {
	cases := []struct {
		err        error
		wantReason string
	}{
		{repository.ErrInsufficientBudget, models.DiagReasonCloudBudgetBlocked},
		{ErrOfferUnavailable, models.DiagReasonCloudNoCapacity},
		{fmt.Errorf("wrapped: %w", ErrOfferUnavailable), models.DiagReasonCloudNoCapacity},
		{errors.New("budget: provisioning blocked: spend at 99% of cap"), models.DiagReasonCloudBudgetBlocked},
		{errors.New("every one of the 1 ranked cloud offers became unavailable"), models.DiagReasonCloudNoCapacity},
		{errors.New("no VPN provider configured"), models.DiagReasonCloudLaunchFailed},
	}
	for _, tc := range cases {
		reason, severity := classifyProvisionFailure(tc.err)
		if reason != tc.wantReason {
			t.Errorf("classifyProvisionFailure(%v) = %s, want %s", tc.err, reason, tc.wantReason)
		}
		if severity == "" {
			t.Errorf("classifyProvisionFailure(%v) returned an empty severity", tc.err)
		}
	}
}
