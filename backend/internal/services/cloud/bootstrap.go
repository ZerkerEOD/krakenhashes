package cloud

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
)

// Environment variables the cloud agent image understands. The entrypoint
// reads these; see agent/docker-entrypoint-cloud.sh.
const (
	EnvKHHost         = "KH_HOST"
	EnvKHClaimCode    = "KH_CLAIM_CODE"
	EnvKHEphemeral    = "KH_EPHEMERAL"
	EnvVPNProvider    = "KH_VPN_PROVIDER"
	EnvVPNAuthKey     = "KH_VPN_AUTH_KEY"
	EnvVPNLoginServer = "KH_VPN_LOGIN_SERVER"
	EnvVPNTag         = "KH_VPN_TAG"
	EnvVPNConfig      = "KH_VPN_CONFIG"
	// EnvDeadlineEpoch is the absolute kill time. The guest destroys itself at
	// this instant regardless of anything the backend does or fails to do.
	EnvDeadlineEpoch = "KH_DEADLINE_EPOCH"
	// EnvHeartbeatTimeout is how long the agent may be unable to reach the
	// backend before self-destructing.
	EnvHeartbeatTimeoutSeconds = "KH_HEARTBEAT_LOSS_TIMEOUT"
	// EnvNoProxy keeps the provider control plane and IMDS OFF the VPN. This
	// is not an optimization: routing the self-destruct call through the
	// tunnel would send it down a dead link in exactly the scenario the
	// self-destruct exists for.
	EnvNoProxy = "KH_NO_PROXY_EXTRA"
	// EnvHostDeadlineFile points the container at the host's deadline file,
	// bind-mounted in. Writing 0 to it is how an unprivileged container asks
	// the host to terminate — its own poweroff cannot work (no CAP_SYS_BOOT)
	// and --restart=unless-stopped undoes killing PID 1. Absent on providers
	// like Vast.ai where there is no host to reach, which is why the container
	// falls back to the provider's own destroy API there.
	EnvHostDeadlineFile = "KH_HOST_DEADLINE_FILE"
)

// Paths for the AWS host-side deadline watchdog.
const (
	// hostDeadlineDir is on a real filesystem, not tmpfs. See the bootstrap
	// script's comment for why that distinction is load-bearing.
	hostDeadlineDir  = "/var/lib/krakenhashes"
	hostDeadlinePath = "/var/lib/krakenhashes/deadline"
	// containerDeadlinePath is where that file appears inside the container.
	containerDeadlinePath = "/run/kh-host-deadline"
)

/*
 * BuildCloudInitUserData renders the EC2 user-data script.
 *
 * Ordering is the whole point. The absolute-deadline watchdog is armed as the
 * FIRST action, before the container image is pulled, before the VPN starts,
 * before anything that can hang or fail. If we armed it after the pull and the
 * pull hung, we would have a GPU instance billing indefinitely with no kill
 * path at all — which is precisely the hole NPK left in its GPU nodes (their
 * only working `trap ... EXIT; shutdown` lived on the cheap wordlist node).
 *
 * The watchdog uses an ABSOLUTE deadline written to disk plus a systemd timer,
 * not `shutdown -h +N`: that form is relative, cancellable with `shutdown -c`,
 * and lost across a reboot. `poweroff --force` combined with
 * InstanceInitiatedShutdownBehavior=terminate is what actually terminates and
 * stops billing.
 */
func BuildCloudInitUserData(req LaunchRequest) (string, error) {
	if req.Image == "" {
		return "", fmt.Errorf("launch request has no container image")
	}
	deadline := time.Now().Add(req.TTL).Unix()
	if req.TTL <= 0 {
		return "", fmt.Errorf("launch request has no TTL; refusing to build user data without a kill deadline")
	}

	var env strings.Builder
	keys := make([]string, 0, len(req.Env))
	for k := range req.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic output keeps user-data diffable
	for _, k := range keys {
		env.WriteString(fmt.Sprintf("  -e %s=%s \\\n", k, shellQuote(req.Env[k])))
	}

	script := fmt.Sprintf(`#!/bin/bash
# KrakenHashes ephemeral cloud agent bootstrap.
#
# Step 1 arms the kill timer. Nothing else runs before it.
set -uo pipefail

# %s, NOT /run. /run is tmpfs: a reboot — which a GPU driver crash or a spot
# reclamation warning can cause — would wipe the deadline, the watchdog would
# find nothing to read, and the ONLY kill path that survives losing the backend
# would be silently disarmed for the rest of the instance's life.
mkdir -p %s
DEADLINE=%d
echo "$DEADLINE" > %s
sync

cat >/usr/local/bin/kh-deadline-watchdog <<'WATCHDOG'
#!/bin/bash
# Absolute-deadline watchdog. Terminates the instance once the deadline passes,
# regardless of whether the backend is reachable, the agent is healthy, or the
# container ever started.
#
# It is also how the CONTAINER terminates the HOST. A rented container is
# unprivileged: it has no CAP_SYS_BOOT, so its own poweroff falls through to
# "kill -9 1", and --restart=unless-stopped then brings it straight back. The
# container instead writes 0 into this file (bind-mounted into it) and this
# watchdog powers the machine off within one poll.
#
# poweroff --force is deliberate: it bypasses a graceful shutdown that a wedged
# GPU driver could otherwise stall indefinitely. Combined with the instance's
# InstanceInitiatedShutdownBehavior=terminate, this stops billing.
DEADLINE_FILE=%s
while true; do
    if [ -r "$DEADLINE_FILE" ]; then
        DEADLINE=$(cat "$DEADLINE_FILE" 2>/dev/null || echo 0)
        NOW=$(date +%%s)
        # A deadline of 0 means "terminate now" — the container asking the host
        # to do what it cannot do itself.
        if [ "$DEADLINE" -eq 0 ] || [ "$NOW" -ge "$DEADLINE" ]; then
            logger -t krakenhashes "deadline reached (deadline=$DEADLINE now=$NOW); terminating instance"
            poweroff --force --force
        fi
    else
        # The file is gone. Something removed the only record of when this
        # machine should die, so terminate rather than run unbounded.
        logger -t krakenhashes "deadline file missing; terminating instance"
        poweroff --force --force
    fi
    sleep 30
done
WATCHDOG
chmod +x /usr/local/bin/kh-deadline-watchdog

cat >/etc/systemd/system/kh-deadline.service <<'UNIT'
[Unit]
Description=KrakenHashes absolute deadline watchdog
DefaultDependencies=no
# The deadline now lives on a real filesystem, so the watchdog must not start
# before that filesystem is mounted — it would read nothing and, under the
# missing-file rule above, power off a healthy instance during boot.
After=local-fs.target
Requires=local-fs.target
[Service]
Type=simple
ExecStart=/usr/local/bin/kh-deadline-watchdog
Restart=always
RestartSec=5
[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now kh-deadline.service

# Step 2: run the agent.
#
# A pull failure used to be swallowed with "|| true", so a missing or private
# image produced a GPU instance that billed at full rate until its TTL with no
# agent on it at all. Disarm the deadline instead and let the watchdog end it in
# under a minute.
if ! docker pull %s; then
    logger -t krakenhashes "agent image pull failed; terminating instance"
    echo 0 > %s
    sleep 60
fi

# The deadline file is bind-mounted so the container can request its own
# termination through the host. Read-write on purpose: writing 0 to it is the
# container's only working self-destruct on AWS.
docker run -d --restart=unless-stopped --name krakenhashes-agent \
  --gpus all \
  -v %s:%s \
  -e %s=%s \
%s  %s
`, hostDeadlinePath, hostDeadlineDir, deadline, hostDeadlinePath,
		hostDeadlinePath, req.Image, hostDeadlinePath,
		hostDeadlinePath, containerDeadlinePath,
		EnvHostDeadlineFile, containerDeadlinePath,
		env.String(), req.Image)

	return script, nil
}

// shellQuote makes a value safe inside a double-quoted shell context.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

/*
 * BuildAgentEnv assembles the environment handed to a cloud agent, on either
 * provider.
 *
 * NO_PROXY is set here rather than in the image because its contents are
 * provider-specific: the agent must reach its OWN provider's control plane
 * outside the tunnel in order to self-destruct when the tunnel is down.
 */
func BuildAgentEnv(
	backendHost string,
	claimCode string,
	vpnProvider string,
	vpnAuthKey string,
	vpnLoginServer string,
	vpnTag string,
	ttl time.Duration,
	heartbeatLoss time.Duration,
	provider string,
) map[string]string {
	env := map[string]string{
		EnvKHHost:      backendHost,
		EnvKHClaimCode: claimCode,
		// Ephemeral mode: read config from the environment and never write a
		// .env, so the claim code never lands on a disk the host operator owns.
		EnvKHEphemeral:             "true",
		EnvVPNProvider:             vpnProvider,
		EnvDeadlineEpoch:           fmt.Sprintf("%d", time.Now().Add(ttl).Unix()),
		EnvHeartbeatTimeoutSeconds: fmt.Sprintf("%d", int(heartbeatLoss.Seconds())),
	}
	/*
	 * WireGuard's credential is not a key, it is a whole wireproxy config file,
	 * and the entrypoint reads it from KH_VPN_CONFIG. Sending it as
	 * KH_VPN_AUTH_KEY — which is what happened before this branch — left
	 * KH_VPN_CONFIG empty, so start_wireguard hit its "FATAL: wireguard
	 * selected but KH_VPN_CONFIG is empty" guard and the container
	 * self-destructed on "VPN unavailable" every single time. WireGuard was a
	 * selectable option that could never work.
	 *
	 * Routed to exactly one variable, not both: the config carries a private
	 * key, and provider metadata is readable by the host operator, so it should
	 * appear there once rather than twice.
	 */
	if vpnAuthKey != "" {
		if vpnProvider == string(models.VPNProviderWireGuard) {
			env[EnvVPNConfig] = vpnAuthKey
		} else {
			env[EnvVPNAuthKey] = vpnAuthKey
		}
	}
	if vpnLoginServer != "" {
		env[EnvVPNLoginServer] = vpnLoginServer
	}
	if vpnTag != "" {
		env[EnvVPNTag] = vpnTag
	}

	switch provider {
	case "vastai":
		// console.vast.ai must stay reachable off-tunnel so the container can
		// DELETE itself using $CONTAINER_API_KEY when the VPN dies.
		env[EnvNoProxy] = "console.vast.ai"
	case "aws":
		// 169.254.169.254 is IMDS; routing it through a proxy breaks instance
		// identity lookups and the shutdown path.
		env[EnvNoProxy] = "169.254.169.254"
	case "runpod", "runpod_community":
		/*
		 * api.runpod.io is the v2 control plane, kept off-tunnel for the same
		 * reason as console.vast.ai: the teardown path must survive the VPN
		 * dying, and a self-destruct that needs the tunnel it is reacting to
		 * the loss of cannot work.
		 *
		 * Both tiers get it. The teardown ladder is identical — RunPod exposes
		 * no provider-enforced TTL on either side, so the in-guest deadline is
		 * doing real work here rather than acting as a backstop.
		 */
		env[EnvNoProxy] = "api.runpod.io"
	}
	return env
}
