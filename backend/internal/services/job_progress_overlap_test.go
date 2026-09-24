package services

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// readSourceFile loads a file from this package's directory so a test can assert
// on code shape that has no runtime seam — here, a SQL literal.
func readSourceFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

/*
An overlap is a property of stored rows, not an event. The progress loop runs
every two seconds, and re-logging every overlapping task on every pass produced
55,415 identical ERROR lines in 90 minutes on one deployment — every ERROR line
in the file. That buried the single line explaining a live outage and rotated the
log window down from the 720 hours an operator had requested to about 90 minutes.
*/

func TestReportOverlaps_LogsOncePerDistinctSet(t *testing.T) {
	s := &JobProgressCalculationService{}
	const scope = "11111111-1111-1111-1111-111111111111"

	first := []string{"task A starts at 10 but previous ended at 20 (overlap 10)"}

	// First report stores a fingerprint.
	s.reportOverlaps(scope, "job X", first)
	sig1, ok := s.reportedOverlaps.Load(scope)
	if !ok {
		t.Fatal("first report did not record a fingerprint")
	}

	// Repeating the identical set must not change anything — that is the
	// every-two-seconds case.
	for i := 0; i < 50; i++ {
		s.reportOverlaps(scope, "job X", first)
	}
	sig2, _ := s.reportedOverlaps.Load(scope)
	if sig1 != sig2 {
		t.Errorf("fingerprint changed while the overlap set was identical: %v -> %v", sig1, sig2)
	}

	// A different set must be reported again — a new overlap is new information.
	second := append(append([]string{}, first...), "task B starts at 30 but previous ended at 40 (overlap 10)")
	s.reportOverlaps(scope, "job X", second)
	sig3, _ := s.reportedOverlaps.Load(scope)
	if sig3 == sig2 {
		t.Error("a changed overlap set did not update the fingerprint; a new overlap would be silently suppressed")
	}
}

// Going clean must clear the entry, so the same overlap recurring later is
// reported rather than suppressed forever by a stale fingerprint.
func TestReportOverlaps_ClearingAllowsRecurrence(t *testing.T) {
	s := &JobProgressCalculationService{}
	const scope = "22222222-2222-2222-2222-222222222222"
	set := []string{"task A starts at 10 but previous ended at 20 (overlap 10)"}

	s.reportOverlaps(scope, "job Y", set)
	if _, ok := s.reportedOverlaps.Load(scope); !ok {
		t.Fatal("expected a fingerprint after reporting")
	}

	s.reportOverlaps(scope, "job Y", nil)
	if _, ok := s.reportedOverlaps.Load(scope); ok {
		t.Error("a clean pass left a stale fingerprint; a recurrence would go unreported")
	}

	s.reportOverlaps(scope, "job Y", set)
	if _, ok := s.reportedOverlaps.Load(scope); !ok {
		t.Error("recurrence after a clean pass was not reported")
	}
}

// Scopes are independent: one noisy job must not mask another.
func TestReportOverlaps_ScopesAreIndependent(t *testing.T) {
	s := &JobProgressCalculationService{}
	set := []string{"task A starts at 10 but previous ended at 20 (overlap 10)"}

	s.reportOverlaps("job-1", "job 1", set)
	s.reportOverlaps("job-2", "job 2", set)

	for _, scope := range []string{"job-1", "job-2"} {
		if _, ok := s.reportedOverlaps.Load(scope); !ok {
			t.Errorf("scope %s was not recorded", scope)
		}
	}
}

/*
'failed' is terminal — the repository's own UpdateStatus groups it with
'completed' and 'cancelled' and stamps completed_at. Listing it among the ACTIVE
statuses is what kept finished jobs in the recalculation loop forever.

This asserts the query shape rather than running it, so it fails loudly if
someone reinstates 'failed' as active without also removing the grace-window
clause.
*/
func TestGetJobsNeedingUpdate_ExcludesTerminalJobsFromTheActiveSet(t *testing.T) {
	src := readSourceFile(t, "job_progress_calculation_service.go")

	activeRe := regexp.MustCompile(`status IN \('pending', 'running', 'paused'([^)]*)\)`)
	m := activeRe.FindStringSubmatch(src)
	if m == nil {
		t.Fatal("could not find the active-status IN clause; did the query change shape?")
	}
	if strings.Contains(m[1], "failed") || strings.Contains(m[1], "completed") || strings.Contains(m[1], "cancelled") {
		t.Errorf("a terminal status is listed as active: %q — finished jobs will be recalculated forever", m[0])
	}

	// The grace window must still cover both terminal states so final numbers settle.
	if !strings.Contains(src, "status IN ('completed', 'failed') AND completed_at >") {
		t.Error("terminal jobs no longer get a settle window; final progress may be left stale")
	}
}
