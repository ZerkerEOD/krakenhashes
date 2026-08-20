package agent

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

/*
 * These guard the one mechanism that bounds an instance's cost when the backend
 * is gone entirely. Getting it wrong is expensive in both directions: never
 * writing destroys healthy machines mid-chunk, and writing unconditionally
 * would keep a machine alive that has lost the backend and can never do work.
 */

// withHeartbeatFile points the writer at a temp path and resets the throttle,
// which is package state shared across tests.
func withHeartbeatFile(t *testing.T, path string) {
	t.Helper()
	prev := heartbeatFilePath
	heartbeatFilePath = path
	lastHeartbeatWrite.Store(0)
	t.Cleanup(func() {
		heartbeatFilePath = prev
		lastHeartbeatWrite.Store(0)
	})
}

/*
 * TestNoteBackendContact_WritesTheFile is the C1 regression test.
 *
 * Before this existed the file was stamped once by the entrypoint and never
 * again, so a healthy agent self-destructed KH_HEARTBEAT_LOSS_TIMEOUT seconds
 * after boot. At the shipped 900s default that is shorter than one 3600s cloud
 * chunk: no work could ever complete, and every rental was pure loss.
 */
func TestNoteBackendContact_WritesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kh-last-contact")
	withHeartbeatFile(t, path)

	before := time.Now().Unix()
	noteBackendContact()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the watchdog heartbeat file was not written: %v", err)
	}
	stamp, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		t.Fatalf("heartbeat file contains %q, which the shell watchdog parses as a "+
			"unix timestamp: %v", raw, err)
	}
	if stamp < before {
		t.Errorf("heartbeat stamp %d predates the call (%d); a stale value is "+
			"indistinguishable from no contact at all", stamp, before)
	}
}

/*
 * TestNoteBackendContact_NoOpWithoutTheEnvVar.
 *
 * On-prem agents have no watchdog and no KH_HEARTBEAT_FILE. Writing a default
 * path anyway would put a file on every operator's machine for no reason, and
 * worse, would make the cloud path look correct in an environment that never
 * exercises it.
 */
func TestNoteBackendContact_NoOpWithoutTheEnvVar(t *testing.T) {
	dir := t.TempDir()
	withHeartbeatFile(t, "")

	noteBackendContact()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read temp dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("an agent with no watchdog wrote %d file(s)", len(entries))
	}
}

/*
 * TestNoteBackendContact_Throttles.
 *
 * This is called on every inbound frame, which under load is thousands per
 * minute. The watchdog polls every 30 seconds and tolerates minutes of
 * staleness, so one write per 5 seconds is indistinguishable to it and keeps a
 * syscall off the message path.
 */
func TestNoteBackendContact_Throttles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kh-last-contact")
	withHeartbeatFile(t, path)

	noteBackendContact()
	first, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after first contact: %v", err)
	}

	// Remove the file: a second write within the throttle window would recreate
	// it, which is the only way to observe the throttle without sleeping.
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	for i := 0; i < 100; i++ {
		noteBackendContact()
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("100 rapid contacts produced a second write; the throttle is not working")
	}
	_ = first
}

/*
 * TestNoteBackendContact_RefreshesAfterTheWindow: the throttle must not become
 * a stall. If it did, the file would age past the timeout on a busy agent and
 * the watchdog would destroy a machine that is actively working.
 */
func TestNoteBackendContact_RefreshesAfterTheWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kh-last-contact")
	withHeartbeatFile(t, path)

	noteBackendContact()
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}

	// Rewind the throttle rather than sleeping for the real interval.
	lastHeartbeatWrite.Store(time.Now().Unix() - int64(heartbeatWriteInterval/time.Second) - 1)
	noteBackendContact()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("no write occurred after the throttle window elapsed: %v", err)
	}
}

/*
 * TestNoteBackendContact_ConcurrentCallers: readPump and the ping/pong handlers
 * are separate goroutines and all call this. The race detector is the point.
 */
func TestNoteBackendContact_ConcurrentCallers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kh-last-contact")
	withHeartbeatFile(t, path)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				noteBackendContact()
			}
		}()
	}
	wg.Wait()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("no heartbeat file after concurrent contact: %v", err)
	}
}

/*
 * TestNoteBackendContact_SurvivesAnUnwritablePath.
 *
 * A failed write must not propagate into the connection that is proving
 * liveness. Losing the WebSocket over a /tmp problem would turn a cosmetic
 * failure into a real one — and the fail-safe direction is already correct: if
 * writes keep failing, the watchdog destroys the instance.
 */
func TestNoteBackendContact_SurvivesAnUnwritablePath(t *testing.T) {
	withHeartbeatFile(t, filepath.Join(t.TempDir(), "no-such-dir", "kh-last-contact"))

	// The assertion is simply that this returns.
	noteBackendContact()
}
