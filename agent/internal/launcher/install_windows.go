//go:build windows

package launcher

import (
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

// Install registers the launcher to auto-start. By default it creates a per-user
// Scheduled Task that runs the launcher at logon as the current user (no
// Administrator required), matching the no-root model used on Linux/macOS. With
// opts.System it creates an elevated Windows service via the SCM instead.
func Install(opts InstallOptions) error {
	if opts.System {
		return installWindowsService(opts)
	}
	return installScheduledTask(opts)
}

// Uninstall removes the launcher autostart (the logon task by default, or the
// system service with opts.System) and, when opts.Purge is set, deletes the
// installed binaries and config/data directories.
func Uninstall(opts UninstallOptions) error {
	var err error
	if opts.System {
		err = removeWindowsService()
	} else {
		err = removeScheduledTask()
	}
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

// installScheduledTask creates a logon-triggered scheduled task that runs the
// launcher at limited (non-elevated) integrity. The launcher resolves its
// config/data dirs from its own executable path, so no working directory needs
// to be set on the task.
func installScheduledTask(opts InstallOptions) error {
	tr := fmt.Sprintf(`"%s" %s`, opts.LauncherPath, strings.Join(agentArgsFromOptions(opts), " "))
	create := exec.Command("schtasks", "/Create",
		"/TN", serviceName,
		"/TR", tr,
		"/SC", "ONLOGON",
		"/RL", "LIMITED",
		"/F")
	if out, err := create.CombinedOutput(); err != nil {
		return fmt.Errorf("create scheduled task: %v: %s", err, strings.TrimSpace(string(out)))
	}
	fmt.Printf("Installed logon task: %s\n", serviceName)

	// Start it now so the agent comes up without waiting for the next logon.
	if out, err := exec.Command("schtasks", "/Run", "/TN", serviceName).CombinedOutput(); err != nil {
		fmt.Printf("warning: task created but failed to start now: %v: %s\n", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func removeScheduledTask() error {
	_ = exec.Command("schtasks", "/End", "/TN", serviceName).Run()
	out, err := exec.Command("schtasks", "/Delete", "/TN", serviceName, "/F").CombinedOutput()
	if err != nil {
		return fmt.Errorf("delete scheduled task %s: %v: %s", serviceName, err, strings.TrimSpace(string(out)))
	}
	fmt.Printf("Removed logon task: %s\n", serviceName)
	return nil
}

// installWindowsService registers the launcher as a Windows service via the SCM
// so it auto-starts and self-updates. Requires an elevated (Administrator)
// prompt.
func installWindowsService(opts InstallOptions) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager (run as Administrator): %w", err)
	}
	defer m.Disconnect()

	if s, err := m.OpenService(serviceName); err == nil {
		s.Close()
		return fmt.Errorf("service %s already exists", serviceName)
	}

	s, err := m.CreateService(serviceName, opts.LauncherPath, mgr.Config{
		StartType:   mgr.StartAutomatic,
		DisplayName: "KrakenHashes Agent",
		Description: "KrakenHashes distributed cracking agent (auto-updating launcher).",
	}, agentArgsFromOptions(opts)...)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()

	if err := eventlog.InstallAsEventCreate(serviceName, eventlog.Error|eventlog.Warning|eventlog.Info); err != nil {
		// Non-fatal: the service is created; event-log source is best-effort.
		fmt.Printf("warning: could not install event log source: %v\n", err)
	}

	if err := s.Start("run"); err != nil {
		fmt.Printf("warning: service created but failed to start: %v\n", err)
	}

	fmt.Printf("Installed Windows service: %s\n", serviceName)
	return nil
}

// removeWindowsService deletes the SCM service and its event-log source.
func removeWindowsService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager (run as Administrator): %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %s not installed: %w", serviceName, err)
	}
	defer s.Close()

	if err := s.Delete(); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	_ = eventlog.Remove(serviceName)

	fmt.Printf("Removed Windows service: %s\n", serviceName)
	return nil
}
