package cloud

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

/*
 * The host deadline watchdog is the ONLY kill path that survives losing the
 * backend: if the backend is down, the reaper cannot terminate anything, and
 * the container cannot terminate itself (no CAP_SYS_BOOT). Everything else is
 * a convenience.
 *
 * The existing bootstrap tests assert the script's TEXT — where the deadline
 * lives, which systemd targets it orders against. None of them ever RUN it, so
 * a logic inversion in the comparison would pass every one of them and only
 * show up as an instance that bills until someone notices.
 *
 * These tests extract the shipped script from the generated user data and
 * execute it with poweroff stubbed, so the four branches are verified as
 * behaviour rather than as source text.
 */

// extractWatchdogScript pulls the body of the WATCHDOG heredoc out of the
// rendered user data. Deliberately taken from the real output rather than
// copied into the test, so the test cannot drift away from what ships.
func extractWatchdogScript(t *testing.T) string {
	t.Helper()

	userData, err := BuildCloudInitUserData(testLaunchRequest(map[string]string{"KH_HOST": "h"}))
	if err != nil {
		t.Fatalf("BuildCloudInitUserData: %v", err)
	}

	const open = "cat >/usr/local/bin/kh-deadline-watchdog <<'WATCHDOG'\n"
	start := strings.Index(userData, open)
	if start < 0 {
		t.Fatal("watchdog heredoc not found in user data")
	}
	start += len(open)

	end := strings.Index(userData[start:], "\nWATCHDOG\n")
	if end < 0 {
		t.Fatal("watchdog heredoc is not terminated")
	}
	return userData[start : start+end]
}

// runWatchdog runs the extracted script with poweroff and logger stubbed onto
// PATH, and reports whether poweroff was invoked within the window.
//
// The script's first iteration runs immediately, so a short window is enough;
// it then sleeps 30s, which is why the process must be killed rather than
// waited on.
func runWatchdog(t *testing.T, deadlineContents string, writeFile bool) bool {
	t.Helper()

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}

	marker := filepath.Join(dir, "poweroff-called")
	stub := fmt.Sprintf("#!/bin/sh\ntouch %s\nexit 0\n", marker)
	if err := os.WriteFile(filepath.Join(binDir, "poweroff"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	// logger is absent on minimal images; without a stub the script would fail
	// on it before reaching poweroff and the test would silently pass.
	if err := os.WriteFile(filepath.Join(binDir, "logger"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	deadlineFile := filepath.Join(dir, "deadline")
	if writeFile {
		if err := os.WriteFile(deadlineFile, []byte(deadlineContents+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The shipped script hardcodes the production deadline path. Point it at
	// the temp file; this is the only edit, and it is a path substitution, not
	// a logic change.
	script := strings.Replace(
		extractWatchdogScript(t),
		"DEADLINE_FILE="+hostDeadlinePath,
		"DEADLINE_FILE="+deadlineFile,
		1,
	)
	if !strings.Contains(script, deadlineFile) {
		t.Fatalf("failed to redirect DEADLINE_FILE; script still points at %s", hostDeadlinePath)
	}

	scriptPath := filepath.Join(dir, "watchdog")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("/bin/bash", scriptPath)
	cmd.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"))
	if err := cmd.Start(); err != nil {
		t.Fatalf("start watchdog: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// A deadline of 0 is the container asking the host to terminate it — the only
// self-destruct an unprivileged container has on AWS. If this branch breaks,
// every in-guest kill rail (absolute deadline, heartbeat loss, never-registered)
// silently stops working, because all three end in "write 0 to this file".
func TestHostWatchdog_ZeroDeadlineTerminates(t *testing.T) {
	if !runWatchdog(t, "0", true) {
		t.Error("deadline of 0 did not power off; the container's only AWS self-destruct is broken")
	}
}

func TestHostWatchdog_PastDeadlineTerminates(t *testing.T) {
	past := fmt.Sprintf("%d", time.Now().Add(-1*time.Hour).Unix())
	if !runWatchdog(t, past, true) {
		t.Error("an expired deadline did not power off; instances would bill past their TTL")
	}
}

// Terminating on a missing file is deliberate: something removed the only
// record of when this machine should die, so the safe reading is "die now".
func TestHostWatchdog_MissingFileTerminates(t *testing.T) {
	if !runWatchdog(t, "", false) {
		t.Error("a missing deadline file did not power off; an instance with no kill record would run unbounded")
	}
}

// The converse, and the one that costs nothing to get wrong in the other
// direction: a live instance inside its lease must NOT be killed.
func TestHostWatchdog_FutureDeadlineDoesNotTerminate(t *testing.T) {
	future := fmt.Sprintf("%d", time.Now().Add(1*time.Hour).Unix())
	if runWatchdog(t, future, true) {
		t.Error("a deadline an hour away powered off anyway; running jobs would be killed mid-chunk")
	}
}
