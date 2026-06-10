//go:build !windows

package launcher

import (
	"os/exec"
	"syscall"
)

// setSysProcAttr puts the agent in its own process group so the launcher can
// signal the whole tree (agent + any hashcat children) at once.
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// gracefulStop sends SIGTERM to the agent's process group for a clean shutdown.
func gracefulStop(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	return nil
}

// killProcess hard-kills the agent's process group.
func killProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
