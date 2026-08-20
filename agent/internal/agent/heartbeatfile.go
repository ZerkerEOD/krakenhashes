package agent

import (
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/ZerkerEOD/krakenhashes/agent/pkg/debug"
)

/*
 * Liveness file for the cloud watchdog.
 *
 * A rented instance runs an in-guest watchdog (docker-entrypoint-cloud.sh) that
 * destroys the machine if this file goes stale for KH_HEARTBEAT_LOSS_TIMEOUT
 * seconds. That timer exists for the case no backend-side teardown can cover:
 * the control plane dies an hour into a six-hour rental, so nothing is left to
 * notice the instance except the instance itself.
 *
 * The agent is the only component that knows whether the backend is still
 * reachable, so the agent is what refreshes the file. The entrypoint stamps it
 * once at boot to start the clock; if nothing ever refreshed it after that, a
 * PERFECTLY HEALTHY agent would destroy its own machine
 * KH_HEARTBEAT_LOSS_TIMEOUT seconds after boot. At the shipped 900s default
 * that is shorter than a single 3600s cloud chunk, so no work could ever
 * complete and every rental would be pure loss.
 *
 * Deliberately env-gated rather than always-on: an on-prem agent has no
 * watchdog and no KH_HEARTBEAT_FILE, so this compiles down to one atomic load
 * per message.
 */

// heartbeatFilePath is empty for any agent not running under a watchdog, which
// makes noteBackendContact a no-op for every on-prem deployment.
var heartbeatFilePath = os.Getenv("KH_HEARTBEAT_FILE")

// lastHeartbeatWrite is the unix time of the most recent successful write.
// Contact happens on every inbound frame; the watchdog polls every 30s and
// tolerates minutes of staleness, so throttling costs nothing and keeps this
// off the hot path.
var lastHeartbeatWrite atomic.Int64

// heartbeatWriteInterval is how often the file is actually rewritten. Far
// tighter than any usable KH_HEARTBEAT_LOSS_TIMEOUT, so throttling can never be
// what makes the file look stale.
const heartbeatWriteInterval = 5 * time.Second

/*
 * noteBackendContact records that the backend was just heard from.
 *
 * Call this on ANY inbound traffic — a ping, a pong, a heartbeat, a task
 * assignment. All of them are proof the tunnel and the control plane are up,
 * which is the only thing the watchdog is asking about. Gating on a narrower
 * signal (say, only heartbeat messages) would make the watchdog fire whenever
 * that one message type stalled, destroying a machine that was working.
 *
 * Never returns an error and never blocks the caller: this is a liveness hint,
 * and failing to write it must not take down the connection that is proving
 * liveness. A persistent write failure is visible in the logs and ends in the
 * watchdog destroying the instance, which is the correct fail-safe direction.
 */
func noteBackendContact() {
	if heartbeatFilePath == "" {
		return
	}

	now := time.Now().Unix()
	last := lastHeartbeatWrite.Load()
	if now-last < int64(heartbeatWriteInterval/time.Second) {
		return
	}
	// CompareAndSwap so concurrent readPump/writePump goroutines produce one
	// write rather than a burst.
	if !lastHeartbeatWrite.CompareAndSwap(last, now) {
		return
	}

	if err := os.WriteFile(heartbeatFilePath, []byte(strconv.FormatInt(now, 10)), 0o600); err != nil {
		debug.Warning("Failed to refresh cloud watchdog heartbeat file %s: %v "+
			"(the in-guest watchdog will destroy this instance if this keeps failing)",
			heartbeatFilePath, err)
	}
}
