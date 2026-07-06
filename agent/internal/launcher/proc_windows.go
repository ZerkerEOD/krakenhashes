//go:build windows

package launcher

import (
	"os/exec"
	"syscall"
)

// setSysProcAttr starts the agent in a new process group so console control
// setSysProcAttr sets cmd.SysProcAttr to request creation of the child process in a new process group (CREATE_NEW_PROCESS_GROUP), enabling targeted console event delivery.
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// gracefulStop best-effort stops the agent. Windows has no SIGTERM; a hard stop
// gracefulStop attempts a best-effort stop of the provided command's process on Windows.
// If the command has no associated process it returns nil. Otherwise it kills the process
// and returns any error from that operation.
func gracefulStop(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

// killProcess terminates the given command's process immediately.
// If cmd has no associated process, killProcess does nothing and returns nil.
// Otherwise it returns the error from attempting to kill the process.
func killProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
