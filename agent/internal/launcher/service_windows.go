//go:build windows

package launcher

import (
	"context"

	"golang.org/x/sys/windows/svc"
)

// isWindowsService reports whether the process is running under the Windows
// isWindowsService reports whether the current process is running under the
// Windows Service Control Manager (SCM). If detection fails, it returns false.
func isWindowsService() bool {
	is, err := svc.IsWindowsService()
	if err != nil {
		return false
	}
	return is
}

// scmHandler bridges the SCM control protocol to the supervisor lifecycle.
type scmHandler struct {
	ctx context.Context
	sup *Supervisor
}

func (h *scmHandler) Execute(args []string, r <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown

	status <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()

	done := make(chan struct{})
	go func() {
		_ = h.sup.Run(ctx)
		close(done)
	}()

	status <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case <-done:
			return false, 0
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				<-done
				return false, 0
			}
		}
	}
}

// runUnderSCM runs the Supervisor under the Windows Service Control Manager (SCM).
// It starts the SCM dispatcher with an scmHandler bound to the given context and
// supervisor, and returns any error returned by svc.Run.
func runUnderSCM(ctx context.Context, sup *Supervisor) error {
	return svc.Run(serviceName, &scmHandler{ctx: ctx, sup: sup})
}
