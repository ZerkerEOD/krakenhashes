package certs

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/env"
)

// ReloadOutcome describes an attempt to make nginx pick up a reissued
// certificate. It is deliberately not an error: nginx is a second, independent
// consumer of server.crt, and whether it reloaded is information the admin needs
// alongside a reissue that otherwise succeeded -- not a reason to fail the
// reissue itself.
type ReloadOutcome struct {
	// Attempted is false when there is no nginx to reload (a bare-metal install
	// has neither nginx nor supervisord). That is not a failure.
	Attempted bool `json:"attempted"`
	Succeeded bool `json:"succeeded"`
	// Detail carries the command output on failure, verbatim. A silent nginx
	// failure leaves the browser seeing the old certificate on :443 while agents
	// on :31337 already see the new one, which is a genuinely confusing state to
	// debug without the underlying message.
	//
	// Deliberately no "run this command to fix it" field. Any such command has to
	// name the container or service, and those differ per deployment -- printing a
	// specific one is wrong more often than it is right. Restarting KrakenHashes
	// always works and is something an operator already knows how to do for their
	// own setup.
	Detail string `json:"detail,omitempty"`
}

// ReloadNginx validates the nginx configuration and signals it to reload so it
// picks up a reissued certificate.
//
// nginx holds its own copy of /etc/krakenhashes/certs/server.crt for the :443
// listener and, unlike the Go listener, has no hot-swap callback -- it only
// rereads the file on SIGHUP. Reissuing a certificate without this leaves the
// web UI presenting the previous one until the container restarts.
//
// Never returns an error: see ReloadOutcome.
func ReloadNginx() ReloadOutcome {
	// Outside Docker there is no nginx and no supervisord. Reporting a failure
	// to reload something that does not exist would put a spurious warning on
	// every bare-metal reissue.
	if !env.GetBool("KH_IN_DOCKER") {
		debug.Debug("Not running in Docker; skipping nginx reload")
		return ReloadOutcome{Attempted: false}
	}

	// Test before signalling. SIGHUP against a broken configuration takes nginx
	// down rather than reloading it, which would turn a certificate reissue into
	// a web UI outage.
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		detail := strings.TrimSpace(string(out))
		debug.Error("nginx configuration test failed, refusing to reload: %v (%s)", err, detail)
		return ReloadOutcome{
			Attempted: true,
			Succeeded: false,
			Detail:    fmt.Sprintf("nginx -t failed: %v (%s)", err, detail),
		}
	}

	debug.Info("Reloading nginx via supervisorctl to pick up the new certificate")
	out, err := exec.Command("supervisorctl", "signal", "HUP", "nginx").CombinedOutput()
	detail := strings.TrimSpace(string(out))
	if err != nil {
		debug.Error("Failed to reload nginx: %v, output: %s", err, detail)
		return ReloadOutcome{
			Attempted: true,
			Succeeded: false,
			Detail:    fmt.Sprintf("%v (%s)", err, detail),
		}
	}

	debug.Info("nginx reload successful: %s", detail)
	return ReloadOutcome{Attempted: true, Succeeded: true, Detail: detail}
}
