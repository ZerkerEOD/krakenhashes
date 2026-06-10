//go:build !windows

package launcher

import (
	"os/exec"
	"syscall"
)

// setSysProcAttr puts the agent in its own process group so the launcher can
// setSysProcAttr sets cmd.SysProcAttr to run the child in its own process group
// (Setpgid=true), enabling signals to be delivered to the whole process group.
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// If sending to the process group fails, it falls back to signalling the single process and returns that error (if any).
func gracefulStop(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	return nil
}

// killProcess sends SIGKILL to the command's process group; it is a no-op if cmd.Process is nil.
// If sending SIGKILL to the group fails, it falls back to killing the individual process.
func killProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
