package cloud

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
)

/*
 * The bootstrap script and the agent environment are the last things the
 * backend controls before a machine starts billing. Everything asserted here is
 * a defect that cost real money in an earlier revision of this branch, so each
 * test names the failure rather than the mechanism.
 */

func testLaunchRequest(env map[string]string) LaunchRequest {
	return LaunchRequest{
		Label: "kh-test",
		Image: "example/agent:latest",
		TTL:   2 * time.Hour,
		Env:   env,
	}
}

/*
 * TestUserData_DeadlineIsNotOnTmpfs (C3).
 *
 * The host watchdog is the only AWS kill path that survives losing the backend.
 * Its state used to live in /run, which is tmpfs — a reboot (GPU driver crash,
 * spot reclamation) wiped the deadline, the watchdog found nothing, and the
 * instance ran unbounded with nothing left to stop it.
 */
func TestUserData_DeadlineIsNotOnTmpfs(t *testing.T) {
	script, err := BuildCloudInitUserData(testLaunchRequest(map[string]string{"KH_HOST": "h"}))
	if err != nil {
		t.Fatalf("BuildCloudInitUserData: %v", err)
	}

	if strings.Contains(script, "/run/krakenhashes-deadline") {
		t.Error("the deadline is stored under /run, which is tmpfs; a reboot disarms " +
			"the only kill path that survives losing the backend")
	}
	if !strings.Contains(script, hostDeadlinePath) {
		t.Errorf("the deadline is not written to %s", hostDeadlinePath)
	}
	if !strings.Contains(script, "mkdir -p "+hostDeadlineDir) {
		t.Errorf("%s is never created, so the initial write would fail silently", hostDeadlineDir)
	}
}

/*
 * TestUserData_WatchdogWaitsForFilesystems (C3, second half).
 *
 * Moving the deadline off tmpfs introduces a boot-order hazard: a watchdog that
 * starts before /var is mounted reads no file. Under the missing-file rule that
 * means "terminate", so a healthy instance would power off during every boot.
 */
func TestUserData_WatchdogWaitsForFilesystems(t *testing.T) {
	script, err := BuildCloudInitUserData(testLaunchRequest(nil))
	if err != nil {
		t.Fatalf("BuildCloudInitUserData: %v", err)
	}
	if !strings.Contains(script, "After=local-fs.target") {
		t.Error("the watchdog unit does not order itself after local-fs.target; with " +
			"DefaultDependencies=no it can start before /var is mounted")
	}
}

/*
 * TestUserData_ContainerCanTerminateTheHost (C4).
 *
 * A rented container is unprivileged: poweroff fails for want of CAP_SYS_BOOT,
 * the fallback kills PID 1, and --restart=unless-stopped brings it right back.
 * The container therefore cannot end its own instance without a channel to the
 * host, and without one a heartbeat-loss self-destruct on AWS did nothing at
 * all while the machine kept billing.
 */
func TestUserData_ContainerCanTerminateTheHost(t *testing.T) {
	script, err := BuildCloudInitUserData(testLaunchRequest(nil))
	if err != nil {
		t.Fatalf("BuildCloudInitUserData: %v", err)
	}

	mount := "-v " + hostDeadlinePath + ":" + containerDeadlinePath
	if !strings.Contains(script, mount) {
		t.Errorf("the deadline file is not bind-mounted into the container (%s); the "+
			"container has no way to terminate an unprivileged instance", mount)
	}
	if !strings.Contains(script, EnvHostDeadlineFile+"="+containerDeadlinePath) {
		t.Errorf("the container is not told where the host deadline file is (%s)", EnvHostDeadlineFile)
	}
	if !strings.Contains(script, `[ "$DEADLINE" -eq 0 ]`) {
		t.Error("the host watchdog does not treat a deadline of 0 as terminate-now, so " +
			"the container's only working self-destruct signal is ignored")
	}
}

/*
 * TestUserData_PullFailureIsNotSwallowed (C6).
 *
 * `docker pull ... || true` turned a missing or private image into a GPU
 * instance that billed at full rate for its whole TTL with no agent on it. The
 * default image name points at a repository that is not published yet, so this
 * was the likely outcome of a first real run.
 */
func TestUserData_PullFailureIsNotSwallowed(t *testing.T) {
	script, err := BuildCloudInitUserData(testLaunchRequest(nil))
	if err != nil {
		t.Fatalf("BuildCloudInitUserData: %v", err)
	}
	if strings.Contains(script, "docker pull "+"example/agent:latest || true") {
		t.Fatal("an image pull failure is still swallowed; the instance would bill for " +
			"its entire TTL with no agent running")
	}
	if !strings.Contains(script, "if ! docker pull") {
		t.Error("the pull result is not checked")
	}
}

/*
 * TestBuildAgentEnv_WireGuardGetsAConfigNotAnAuthKey (C2).
 *
 * WireGuard's credential is a whole wireproxy config file and the entrypoint
 * reads it from KH_VPN_CONFIG. It used to be sent as KH_VPN_AUTH_KEY, leaving
 * KH_VPN_CONFIG empty, so start_wireguard hit its empty-config guard and the
 * container self-destructed on "VPN unavailable" every single time. WireGuard
 * was selectable in the UI and could never work.
 */
func TestBuildAgentEnv_WireGuardGetsAConfigNotAnAuthKey(t *testing.T) {
	const cfg = "[Interface]\nPrivateKey = abc\n"
	env := BuildAgentEnv("backend.internal", "CODE", string(models.VPNProviderWireGuard),
		cfg, "", "", time.Hour, 15*time.Minute, readyDeadlineWindow, "aws")

	if env[EnvVPNConfig] != cfg {
		t.Errorf("%s = %q, want the wireproxy config; the entrypoint fails closed on an "+
			"empty value and destroys the instance", EnvVPNConfig, env[EnvVPNConfig])
	}
	if _, present := env[EnvVPNAuthKey]; present {
		t.Errorf("%s is also set; the config carries a private key and provider metadata "+
			"is readable by the host operator, so it should appear exactly once", EnvVPNAuthKey)
	}
}

// TestBuildAgentEnv_TailscaleStillUsesAnAuthKey: the WireGuard routing must not
// have moved anyone else's credential.
func TestBuildAgentEnv_TailscaleStillUsesAnAuthKey(t *testing.T) {
	env := BuildAgentEnv("backend.internal", "CODE", string(models.VPNProviderTailscale),
		"tskey-abc", "https://login.example", "tag:kraken", time.Hour, 15*time.Minute, readyDeadlineWindow, "vastai")

	if env[EnvVPNAuthKey] != "tskey-abc" {
		t.Errorf("%s = %q, want the tailscale key", EnvVPNAuthKey, env[EnvVPNAuthKey])
	}
	if _, present := env[EnvVPNConfig]; present {
		t.Errorf("%s must not be set for tailscale", EnvVPNConfig)
	}
	if env[EnvVPNLoginServer] != "https://login.example" || env[EnvVPNTag] != "tag:kraken" {
		t.Error("login server or tag was dropped")
	}
}

/*
 * TestBuildAgentEnv_ProviderControlPlaneStaysOffTheTunnel.
 *
 * The self-destruct call is made precisely when the tunnel is suspect, so
 * routing it through the tunnel would break teardown in exactly the case
 * teardown exists for.
 */
func TestBuildAgentEnv_ProviderControlPlaneStaysOffTheTunnel(t *testing.T) {
	vast := BuildAgentEnv("h", "C", "tailscale", "k", "", "", time.Hour, time.Minute, readyDeadlineWindow, "vastai")
	if !strings.Contains(vast[EnvNoProxy], "console.vast.ai") {
		t.Errorf("%s = %q; the Vast destroy API must bypass the VPN", EnvNoProxy, vast[EnvNoProxy])
	}

	aws := BuildAgentEnv("h", "C", "tailscale", "k", "", "", time.Hour, time.Minute, readyDeadlineWindow, "aws")
	if !strings.Contains(aws[EnvNoProxy], "169.254.169.254") {
		t.Errorf("%s = %q; IMDS must bypass the VPN", EnvNoProxy, aws[EnvNoProxy])
	}
}

// TestBuildAgentEnv_DeadlineAndHeartbeatArePresent: both watchdog timers are
// configured by these two variables, and an unset one disables that timer.
func TestBuildAgentEnv_DeadlineAndHeartbeatArePresent(t *testing.T) {
	before := time.Now().Add(90 * time.Minute).Unix()
	env := BuildAgentEnv("h", "C", "tailscale", "k", "", "", 90*time.Minute, 12*time.Minute, readyDeadlineWindow, "aws")

	if env[EnvDeadlineEpoch] == "" || env[EnvDeadlineEpoch] == "0" {
		t.Fatalf("%s = %q; 0 disables the absolute deadline entirely", EnvDeadlineEpoch, env[EnvDeadlineEpoch])
	}
	epoch, err := strconv.ParseInt(env[EnvDeadlineEpoch], 10, 64)
	if err != nil {
		t.Fatalf("%s is not an integer: %v", EnvDeadlineEpoch, err)
	}
	if epoch < before-5 || epoch > before+5 {
		t.Errorf("%s = %d, want ~%d (now + TTL)", EnvDeadlineEpoch, epoch, before)
	}
	if env[EnvHeartbeatTimeoutSeconds] != "720" {
		t.Errorf("%s = %q, want 720", EnvHeartbeatTimeoutSeconds, env[EnvHeartbeatTimeoutSeconds])
	}
}

/*
 * TestBuildAgentEnv_ReadyDeadlineIsAbsoluteAndPresent.
 *
 * The gap this closes was found on a live AWS run: the VPN came up, so the
 * "VPN unavailable" rail passed; the agent could not reach the backend, so it
 * never registered and never wrote a heartbeat file, so the heartbeat rail had
 * no last-contact time to measure and stayed silent. Nothing in the guest
 * bounded the instance and it billed until a backend-side reaper -- which by
 * definition cannot help when the backend is what is unreachable -- ended it.
 *
 * Asserting the value is an absolute epoch rather than merely non-empty is the
 * point of the test. A duration here would be restarted by every container
 * restart under --restart=unless-stopped, which is precisely how the heartbeat
 * rail was defeated, and a crash-looping agent would never reach it.
 */
func TestBuildAgentEnv_ReadyDeadlineIsAbsoluteAndPresent(t *testing.T) {
	want := time.Now().Add(20 * time.Minute).Unix()
	env := BuildAgentEnv("h", "C", "tailscale", "k", "", "", time.Hour, 15*time.Minute, 20*time.Minute, "aws")

	raw, ok := env[EnvReadyDeadlineEpoch]
	if !ok {
		t.Fatalf("%s is absent; an agent that never registers is then bounded only by the "+
			"full TTL", EnvReadyDeadlineEpoch)
	}
	got, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		t.Fatalf("%s = %q, not an integer epoch", EnvReadyDeadlineEpoch, raw)
	}
	if got < want-5 || got > want+5 {
		t.Errorf("%s = %d, want ~%d (an absolute instant, not a duration)",
			EnvReadyDeadlineEpoch, got, want)
	}
}

/*
 * TestBuildAgentEnv_ZeroReadyWindowOmitsTheDeadline: an unset variable and a
 * zero one must mean the same thing to the entrypoint -- rail off. Emitting a
 * literal 0 would read as "the deadline passed in 1970" and destroy the
 * instance on the watchdog's first poll.
 */
func TestBuildAgentEnv_ZeroReadyWindowOmitsTheDeadline(t *testing.T) {
	env := BuildAgentEnv("h", "C", "tailscale", "k", "", "", time.Hour, 15*time.Minute, 0, "aws")
	if v, present := env[EnvReadyDeadlineEpoch]; present {
		t.Errorf("%s = %q; a disabled window must omit the variable, not send an epoch "+
			"already in the past", EnvReadyDeadlineEpoch, v)
	}
}
