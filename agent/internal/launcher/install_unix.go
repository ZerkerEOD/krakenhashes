//go:build !windows

package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	systemdUnitPath      = "/etc/systemd/system/krakenhashes-agent.service"
	launchdDaemonPath    = "/Library/LaunchDaemons/com.krakenhashes.agent.plist"
	unitFileName         = "krakenhashes-agent.service"
	launchdAgentFileName = "com.krakenhashes.agent.plist"
)

// Install registers the launcher to auto-start and self-update. By default it
// installs a USER service (systemd --user on Linux, a per-user LaunchAgent on
// macOS) that runs as the invoking user from the launcher's own directory — no
// root required, matching the documented agent install model. With opts.System
// Install installs the launcher as either a system-wide service or a per-user service.
//
// Install selects system or user mode based on opts.System. In system mode it requires
// root privileges and installs a system service (Linux: systemd, macOS: launchd); in
// user mode it requires a non-root caller and installs a per-user service (Linux:
// systemd user unit, macOS: LaunchAgent). It returns an error for unsupported
// platforms or when the privilege requirements are not met.
func Install(opts InstallOptions) error {
	if opts.System {
		if os.Geteuid() != 0 {
			return fmt.Errorf("--system install requires root (re-run with sudo)")
		}
		switch runtime.GOOS {
		case "linux":
			return installSystemdSystem(opts)
		case "darwin":
			return installLaunchdSystem(opts)
		default:
			return fmt.Errorf("service install is not supported on %s", runtime.GOOS)
		}
	}
	// Default: user-level service, no root. Refuse root here so we don't install
	// the service under root's home/account by mistake.
	if os.Geteuid() == 0 {
		return fmt.Errorf("a user service must not be installed as root; re-run without sudo, or pass --system to install a system service")
	}
	switch runtime.GOOS {
	case "linux":
		return installSystemdUser(opts)
	case "darwin":
		return installLaunchdUser(opts)
	default:
		return fmt.Errorf("service install is not supported on %s", runtime.GOOS)
	}
}

// Uninstall removes the launcher service (the user service by default, or the
// system service with opts.System) and, when opts.Purge is set, deletes the
// Uninstall removes the installed service/agent and optionally purges installed binaries and data.
// 
// Uninstall removes either the system-scoped or user-scoped service depending on opts.System.
// For system-scoped uninstalls it requires root privileges and supports Linux (systemd) and macOS (launchd).
// For user-scoped uninstalls it supports Linux (per-user systemd unit) and macOS (LaunchAgent).
// If an uninstall step fails and opts.Purge is false the error is returned. If opts.Purge is true
// the function prints a warning for the uninstall error, continues to attempt cleanup, and calls
// purgeFiles(opts) to remove installed binaries and config/data directories.
func Uninstall(opts UninstallOptions) error {
	var err error
	if opts.System {
		if os.Geteuid() != 0 {
			return fmt.Errorf("--system uninstall requires root (re-run with sudo)")
		}
		switch runtime.GOOS {
		case "linux":
			err = uninstallSystemdSystem()
		case "darwin":
			err = uninstallLaunchdSystem()
		default:
			err = fmt.Errorf("service uninstall is not supported on %s", runtime.GOOS)
		}
	} else {
		switch runtime.GOOS {
		case "linux":
			err = uninstallSystemdUser()
		case "darwin":
			err = uninstallLaunchdUser()
		default:
			err = fmt.Errorf("service uninstall is not supported on %s", runtime.GOOS)
		}
	}
	// With --purge, keep going past a "service already gone" error so leftover
	// files are still cleaned up; otherwise surface the failure.
	if err != nil && !opts.Purge {
		return err
	}
	if err != nil {
		fmt.Printf("warning: %v (continuing with purge)\n", err)
	}
	if opts.Purge {
		purgeFiles(opts)
	}
	return nil
}

// systemdUnit renders the unit file. wantedBy/user vary between the user and
// system variants; workdir is the launcher's directory so config/data land
// systemdUnit constructs the textual contents of a systemd unit file for the KrakenHashes agent.
// It sets WorkingDirectory from opts.LauncherPath, builds ExecStart from the launcher path and agent arguments,
// and injects `Environment=` directives for KH_CONFIG_DIR and KH_DATA_DIR when those options are set.
// The provided userDirective is placed into the [Service] section (e.g. a `User=` line) and wantedBy is used in the [Install] section.
func systemdUnit(opts InstallOptions, userDirective, wantedBy string) string {
	var env strings.Builder
	if opts.ConfigDir != "" {
		fmt.Fprintf(&env, "Environment=KH_CONFIG_DIR=%s\n", opts.ConfigDir)
	}
	if opts.DataDir != "" {
		fmt.Fprintf(&env, "Environment=KH_DATA_DIR=%s\n", opts.DataDir)
	}
	workdir := filepath.Dir(opts.LauncherPath)
	execStart := opts.LauncherPath + " " + strings.Join(agentArgsFromOptions(opts), " ")
	return fmt.Sprintf(`[Unit]
Description=KrakenHashes Agent (auto-updating launcher)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=%s
ExecStart=%s
%s%sRestart=on-failure
RestartSec=5
KillSignal=SIGTERM
TimeoutStopSec=60

[Install]
WantedBy=%s
`, workdir, execStart, userDirective, env.String(), wantedBy)
}

// installSystemdUser creates a per-user systemd unit for the launcher, reloads the user systemd daemon,
// enables and starts the service, and performs a best-effort enablement of systemd lingering for the user.
// It returns an error if the user unit directory cannot be resolved or created, if writing the unit file fails,
// or if the required systemctl operations fail.
func installSystemdUser(opts InstallOptions) error {
	unitDir, err := userSystemdDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		return fmt.Errorf("create user unit dir: %w", err)
	}
	unitPath := filepath.Join(unitDir, unitFileName)
	if err := os.WriteFile(unitPath, []byte(systemdUnit(opts, "", "default.target")), 0o644); err != nil {
		return fmt.Errorf("write user systemd unit: %w", err)
	}
	if err := run("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	if err := run("systemctl", "--user", "enable", "--now", "krakenhashes-agent"); err != nil {
		return err
	}
	fmt.Printf("Installed and started user service: %s\n", unitPath)
	enableLinger()
	return nil
}

// uninstallSystemdUser disables the per-user systemd service, removes its unit file, reloads the user daemon, and prints a confirmation message.
// It attempts to disable the service (best-effort), removes the unit file in the user's systemd directory if that directory can be resolved (ignoring a missing file), runs `systemctl --user daemon-reload`, and returns an error only if removing the unit file fails for reasons other than non-existence.
func uninstallSystemdUser() error {
	_ = run("systemctl", "--user", "disable", "--now", "krakenhashes-agent")
	if unitDir, err := userSystemdDir(); err == nil {
		if rerr := os.Remove(filepath.Join(unitDir, unitFileName)); rerr != nil && !os.IsNotExist(rerr) {
			return fmt.Errorf("remove user systemd unit: %w", rerr)
		}
	}
	_ = run("systemctl", "--user", "daemon-reload")
	fmt.Println("Removed user service")
	return nil
}

// installSystemdSystem writes the system-level systemd unit for the agent, reloads systemd, and enables & starts the service.
// If SUDO_USER is set and not "root", the unit will include a `User=` directive to run the service as that user.
// Returns an error if writing the unit file, reloading systemd, or enabling/starting the service fails.
func installSystemdSystem(opts InstallOptions) error {
	// Scope the system service to the invoking (sudo) user when known, so the
	// agent runs as them rather than root.
	var userDirective string
	if su := os.Getenv("SUDO_USER"); su != "" && su != "root" {
		userDirective = fmt.Sprintf("User=%s\n", su)
	}
	if err := os.WriteFile(systemdUnitPath, []byte(systemdUnit(opts, userDirective, "multi-user.target")), 0o644); err != nil {
		return fmt.Errorf("write systemd unit: %w", err)
	}
	if err := run("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := run("systemctl", "enable", "--now", "krakenhashes-agent"); err != nil {
		return err
	}
	fmt.Printf("Installed and started system service: %s\n", systemdUnitPath)
	return nil
}

// uninstallSystemdSystem removes the system-wide systemd unit for the agent and attempts to disable it.
// It best-effort disables the service, removes the unit file at systemdUnitPath (ignoring if it does not exist), reloads the systemd daemon, prints "Removed system service", and returns an error only if unit file removal fails for reasons other than non-existence.
func uninstallSystemdSystem() error {
	_ = run("systemctl", "disable", "--now", "krakenhashes-agent")
	if err := os.Remove(systemdUnitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove systemd unit: %w", err)
	}
	_ = run("systemctl", "daemon-reload")
	fmt.Println("Removed system service")
	return nil
}

// userSystemdDir returns the per-user systemd unit directory
// userSystemdDir returns the per-user systemd unit directory path for the current user.
// If the XDG_CONFIG_HOME environment variable is set, its value is used as the base;
// otherwise the function falls back to $HOME/.config/systemd/user.
// It returns the resolved directory path, or an error if the home directory cannot be determined.
func userSystemdDir() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "systemd", "user"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("cannot resolve home directory for user service")
	}
	return filepath.Join(home, ".config", "systemd", "user"), nil
}

// enableLinger best-effort enables systemd lingering so the user service starts
// at boot before login. Linger needs privilege, so on failure we just tell the
// enableLinger attempts to enable systemd "linger" for the current user so user services can start at boot.
// If the current username cannot be determined the function does nothing.
// On success it prints a confirmation; on failure it prints a note with a suggested `sudo loginctl enable-linger <user>` command.
func enableLinger() {
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("LOGNAME")
	}
	if user == "" {
		return
	}
	// Discard output so a permission failure isn't noisy; we print our own note.
	if err := exec.Command("loginctl", "enable-linger", user).Run(); err != nil {
		fmt.Printf("note: the service runs while you are logged in. To also start it at boot before login, run:\n      sudo loginctl enable-linger %s\n", user)
		return
	}
	fmt.Printf("Enabled linger for %s (service starts at boot).\n", user)
}

// launchdPlist renders a LaunchAgent/LaunchDaemon plist. workdir is the
// launchdPlist builds a launchd plist XML string for the agent using the provided install options and working directory.
// 
// The plist's ProgramArguments array contains opts.LauncherPath followed by agentArgsFromOptions(opts).
// If opts.ConfigDir or opts.DataDir are non-empty they are added to EnvironmentVariables as KH_CONFIG_DIR and KH_DATA_DIR.
// The plist sets Label to "com.krakenhashes.agent", WorkingDirectory to workdir, and enables RunAtLoad and KeepAlive.
// It returns the complete plist XML as a string.
func launchdPlist(opts InstallOptions, workdir string) string {
	var args strings.Builder
	args.WriteString(fmt.Sprintf("        <string>%s</string>\n", opts.LauncherPath))
	for _, a := range agentArgsFromOptions(opts) {
		args.WriteString(fmt.Sprintf("        <string>%s</string>\n", a))
	}

	var env strings.Builder
	if opts.ConfigDir != "" || opts.DataDir != "" {
		env.WriteString("    <key>EnvironmentVariables</key>\n    <dict>\n")
		if opts.ConfigDir != "" {
			env.WriteString(fmt.Sprintf("        <key>KH_CONFIG_DIR</key><string>%s</string>\n", opts.ConfigDir))
		}
		if opts.DataDir != "" {
			env.WriteString(fmt.Sprintf("        <key>KH_DATA_DIR</key><string>%s</string>\n", opts.DataDir))
		}
		env.WriteString("    </dict>\n")
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.krakenhashes.agent</string>
    <key>ProgramArguments</key>
    <array>
%s    </array>
%s    <key>WorkingDirectory</key>
    <string>%s</string>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
`, args.String(), env.String(), workdir)
}

// installLaunchdUser installs and loads a per-user launchd agent plist for the launcher.
// It ensures ~/Library/LaunchAgents exists, writes the agent plist into that directory, attempts to unload any existing agent, and then loads the plist with `launchctl load -w`; it returns an error if the home directory cannot be resolved, the directory or plist cannot be written, or the load command fails.
func installLaunchdUser(opts InstallOptions) error {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return fmt.Errorf("cannot resolve home directory for user agent")
	}
	dir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}
	plistPath := filepath.Join(dir, launchdAgentFileName)
	workdir := filepath.Dir(opts.LauncherPath)
	if err := os.WriteFile(plistPath, []byte(launchdPlist(opts, workdir)), 0o644); err != nil {
		return fmt.Errorf("write launchd plist: %w", err)
	}
	// Reload if already present, then load.
	_ = run("launchctl", "unload", plistPath)
	if err := run("launchctl", "load", "-w", plistPath); err != nil {
		return err
	}
	fmt.Printf("Installed and loaded user agent: %s\n", plistPath)
	return nil
}

// uninstallLaunchdUser removes the current user's LaunchAgent plist for the agent and attempts to unload it.
// It resolves the user's home directory, performs a best-effort `launchctl unload -w <plist>`, and then removes
// ~/Library/LaunchAgents/<launchdAgentFileName>. If the home directory cannot be resolved or removal fails for
// reasons other than the file not existing, an error is returned. On success it prints "Removed user agent".
func uninstallLaunchdUser() error {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return fmt.Errorf("cannot resolve home directory")
	}
	plistPath := filepath.Join(home, "Library", "LaunchAgents", launchdAgentFileName)
	_ = run("launchctl", "unload", "-w", plistPath)
	if rerr := os.Remove(plistPath); rerr != nil && !os.IsNotExist(rerr) {
		return fmt.Errorf("remove launchd plist: %w", rerr)
	}
	fmt.Println("Removed user agent")
	return nil
}

// installLaunchdSystem writes the agent plist to the system LaunchDaemons path and loads it with launchctl.
// The plist's WorkingDirectory is set to the directory containing opts.LauncherPath. It returns an error if
// writing the plist or invoking `launchctl load -w` fails.
func installLaunchdSystem(opts InstallOptions) error {
	workdir := filepath.Dir(opts.LauncherPath)
	if err := os.WriteFile(launchdDaemonPath, []byte(launchdPlist(opts, workdir)), 0o644); err != nil {
		return fmt.Errorf("write launchd plist: %w", err)
	}
	if err := run("launchctl", "load", "-w", launchdDaemonPath); err != nil {
		return err
	}
	fmt.Printf("Installed and loaded system daemon: %s\n", launchdDaemonPath)
	return nil
}

// uninstallLaunchdSystem removes the system LaunchDaemon plist at launchdDaemonPath and attempts to unload it from launchd.
// Unload failures are ignored; a missing plist is treated as success. It returns an error only if removing the plist fails for reasons other than non-existence.
func uninstallLaunchdSystem() error {
	_ = run("launchctl", "unload", "-w", launchdDaemonPath)
	if err := os.Remove(launchdDaemonPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove launchd plist: %w", err)
	}
	fmt.Println("Removed system daemon")
	return nil
}

// run executes the named program with the provided arguments and connects the subprocess's
// stdout and stderr to the current process's stdout and stderr. It returns nil on success;
// on failure it returns an error that includes the full invoked command and the underlying error
// (formatted as "<name> <args...>: <err>").
func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}
