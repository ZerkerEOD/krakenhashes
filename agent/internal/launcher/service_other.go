//go:build !windows

package launcher

import "context"

// isWindowsService is always false off Windows.
func isWindowsService() bool { return false }

// runUnderSCM is never reached off Windows (isWindowsService is false); it just
// runs the supervisor so the symbol exists for the portable RunService.
func runUnderSCM(ctx context.Context, sup *Supervisor) error {
	return sup.Run(ctx)
}
