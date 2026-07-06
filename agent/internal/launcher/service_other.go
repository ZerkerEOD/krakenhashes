//go:build !windows

package launcher

import "context"

// isWindowsService reports whether the process is running as a Windows service; on non-Windows platforms it always returns false.
func isWindowsService() bool { return false }

// runUnderSCM is never reached off Windows (isWindowsService is false); it just
// runUnderSCM runs the supervisor so the symbol exists for the portable RunService.
// It calls sup.Run(ctx) and returns the error produced by that call.
func runUnderSCM(ctx context.Context, sup *Supervisor) error {
	return sup.Run(ctx)
}
