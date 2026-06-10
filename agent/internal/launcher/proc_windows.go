//go:build windows

package launcher

import (
	"os/exec"
	"syscall"
)

// setSysProcAttr starts the agent in a new process group so console control
// events can be targeted at it.
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// gracefulStop best-effort stops the agent. Windows has no SIGTERM; a hard stop
// is used and the backend's disconnect-grace recovers any in-flight task.
func gracefulStop(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

// killProcess hard-kills the agent.
func killProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
