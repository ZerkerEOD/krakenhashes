package launcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// serviceName is the OS service identifier used for install/uninstall and the
// Windows SCM dispatcher.
const serviceName = "KrakenHashesAgent"

// InstallOptions configures a service installation. The service always runs the
// LAUNCHER (never the agent directly) so updates work; Host/ClaimCode/ExtraArgs
// are forwarded to the agent on first run.
type InstallOptions struct {
	System       bool     // install a root/system service instead of the default user service
	LauncherPath string   // absolute path to the launcher executable
	ConfigDir    string   // KH_CONFIG_DIR for the service (may be empty -> launcher default)
	DataDir      string   // KH_DATA_DIR for the service (may be empty -> launcher default)
	Host         string   // --host value forwarded to the agent (host:port)
	ClaimCode    string   // --claim value forwarded to the agent
	ExtraArgs    []string // additional args forwarded to the agent
}

// agentArgsFromOptions builds the launcher "run" argument list embedded in the
// service (these are forwarded to the agent child).
func agentArgsFromOptions(opts InstallOptions) []string {
	args := []string{"run"}
	if opts.Host != "" {
		args = append(args, "--host", opts.Host)
	}
	if opts.ClaimCode != "" {
		args = append(args, "--claim", opts.ClaimCode)
	}
	args = append(args, opts.ExtraArgs...)
	return args
}

// UninstallOptions configures service removal. When Purge is set, the installed
// binaries and the config/data directories are also deleted (a destructive,
// opt-in cleanup that removes the agent's credentials and synced files).
type UninstallOptions struct {
	System       bool   // remove the root/system service instead of the default user service
	Purge        bool   // also delete binaries + config/data dirs
	LauncherPath string // launcher binary to remove on purge (this executable)
	AgentBinary  string // agent binary to remove on purge
	ConfigDir    string // config dir to remove on purge (certs/keys/.env)
	DataDir      string // data dir to remove on purge (wordlists/rules)
}

// purgeFiles deletes the launcher/agent binaries and the config/data dirs.
// Best-effort: it logs but never fails the uninstall. On Windows the launcher's
// own running .exe is locked by the OS and cannot be deleted in-place; that is
// reported so the operator can remove it after the process exits.
func purgeFiles(opts UninstallOptions) {
	remove := func(kind, path string) {
		if path == "" {
			return
		}
		if err := os.RemoveAll(path); err != nil {
			fmt.Printf("warning: could not remove %s %q: %v\n", kind, path, err)
			return
		}
		fmt.Printf("Removed %s: %s\n", kind, path)
	}
	if opts.AgentBinary != "" {
		remove("agent binary", opts.AgentBinary)
		remove("agent binary backup", opts.AgentBinary+".bak")
		remove("agent staging binary", opts.AgentBinary+".new")
	}
	remove("config dir", safeDir(opts.ConfigDir))
	remove("data dir", safeDir(opts.DataDir))
	// Remove the launcher binary last (on Unix a running file can be unlinked;
	// on Windows this is expected to fail while the process is still alive).
	remove("launcher binary", opts.LauncherPath)
}

// safeDir resolves p to an absolute path and refuses obviously-dangerous
// targets (the filesystem/volume root or an empty/relative-only path), returning
// "" to mean "skip" so a misconfigured purge can never delete a root directory.
func safeDir(p string) string {
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return ""
	}
	abs = filepath.Clean(abs)
	if abs == "." || filepath.Dir(abs) == abs { // "." or a volume root like "/" or "C:\\"
		return ""
	}
	return abs
}

// RunMode describes how the launcher is being executed. It only affects
// logging/stop semantics — restart is always handled internally by the
// supervisor regardless of mode.
type RunMode int

const (
	ModeForeground RunMode = iota
	ModeSystemd
	ModeLaunchd
	ModeWindowsService
)

func (m RunMode) String() string {
	switch m {
	case ModeSystemd:
		return "systemd"
	case ModeLaunchd:
		return "launchd"
	case ModeWindowsService:
		return "windows-service"
	default:
		return "foreground"
	}
}

// DetectRunMode infers how the launcher was started. systemd sets INVOCATION_ID
// (and JOURNAL_STREAM); launchd sets XPC_SERVICE_NAME; Windows SCM is detected
// via the service API.
func DetectRunMode() RunMode {
	if isWindowsService() {
		return ModeWindowsService
	}
	if os.Getenv("INVOCATION_ID") != "" || os.Getenv("JOURNAL_STREAM") != "" {
		return ModeSystemd
	}
	if os.Getenv("XPC_SERVICE_NAME") != "" {
		return ModeLaunchd
	}
	return ModeForeground
}

// RunService runs the supervisor. Under the Windows Service Control Manager it
// runs via the SCM dispatcher (so Start/Stop are honored); otherwise it runs
// the supervisor directly until ctx is cancelled.
func RunService(ctx context.Context, sup *Supervisor) error {
	if isWindowsService() {
		return runUnderSCM(ctx, sup)
	}
	return sup.Run(ctx)
}
