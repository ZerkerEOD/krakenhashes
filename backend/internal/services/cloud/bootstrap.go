package cloud

import (
	"fmt"
	"sort"
	"strings"
	"time"
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

DEADLINE=%d
echo "$DEADLINE" > /run/krakenhashes-deadline

cat >/usr/local/bin/kh-deadline-watchdog <<'WATCHDOG'
#!/bin/bash
# Absolute-deadline watchdog. Terminates the instance once the deadline passes,
# regardless of whether the backend is reachable, the agent is healthy, or the
# container ever started.
#
# poweroff --force is deliberate: it bypasses a graceful shutdown that a wedged
# GPU driver could otherwise stall indefinitely. Combined with the instance's
# InstanceInitiatedShutdownBehavior=terminate, this stops billing.
DEADLINE_FILE=/run/krakenhashes-deadline
while true; do
    if [ -r "$DEADLINE_FILE" ]; then
        DEADLINE=$(cat "$DEADLINE_FILE")
        NOW=$(date +%%s)
        if [ "$NOW" -ge "$DEADLINE" ]; then
            logger -t krakenhashes "absolute deadline reached; terminating instance"
            poweroff --force --force
        fi
    fi
    sleep 30
done
WATCHDOG
chmod +x /usr/local/bin/kh-deadline-watchdog

cat >/etc/systemd/system/kh-deadline.service <<'UNIT'
[Unit]
Description=KrakenHashes absolute deadline watchdog
DefaultDependencies=no
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

# Step 2: run the agent. Failures below are survivable precisely because the
# watchdog above is already running.
docker pull %s || true
docker run -d --restart=unless-stopped --name krakenhashes-agent \
  --gpus all \
%s  %s
`, deadline, req.Image, env.String(), req.Image)

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
	if vpnAuthKey != "" {
		env[EnvVPNAuthKey] = vpnAuthKey
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
	}
	return env
}
