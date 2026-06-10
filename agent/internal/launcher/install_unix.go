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
// it installs a root/system service instead (the documented "advanced" path).
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
// installed binaries and config/data directories.
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
// alongside the binary.
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
// ($XDG_CONFIG_HOME/systemd/user or ~/.config/systemd/user).
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
// user how to enable it (the service still runs while they're logged in).
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
// launcher's directory so config/data land alongside the binary.
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

func uninstallLaunchdSystem() error {
	_ = run("launchctl", "unload", "-w", launchdDaemonPath)
	if err := os.Remove(launchdDaemonPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove launchd plist: %w", err)
	}
	fmt.Println("Removed system daemon")
	return nil
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}
