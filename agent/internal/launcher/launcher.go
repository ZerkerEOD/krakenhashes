// Package launcher implements the KrakenHashes agent launcher/supervisor: a
// small, stable process that spawns the agent as a child, forwards its
// environment/args/working-dir so registration keeps working, and performs the
// download -> backup -> swap -> restart of the agent binary WHILE THE AGENT IS
// STOPPED. Because the launcher is never the binary being replaced, it cleanly
// solves the "a binary cannot call itself" problem.
package launcher

import (
	"context"
	"io"
	"log"
	"os"
	"os/exec"
	"sync/atomic"
	"time"

	"github.com/ZerkerEOD/krakenhashes/agent/internal/updateipc"
)

// maxLauncherAttempts bounds how many times the launcher will (re)try a single
// update instruction before giving up and running the old binary, so a
// launcher crash mid-update can't loop forever.
const maxLauncherAttempts = 3

// Config configures a Supervisor.
type Config struct {
	// AgentBinary is the path to the agent executable the launcher manages.
	AgentBinary string
	// ConfigDir is the agent's config dir (where update.json / ready.json live).
	// Must match KH_CONFIG_DIR exported to the child.
	ConfigDir string
	// WorkDir is the child's working directory.
	WorkDir string
	// AgentArgs are forwarded verbatim to the agent.
	AgentArgs []string
	// Env is the environment for the child (already including absolute
	// KH_CONFIG_DIR / KH_DATA_DIR).
	Env []string
	// HealthTimeout is how long a freshly-swapped agent has to come online
	// before the launcher rolls back.
	HealthTimeout time.Duration
	// BootstrapBaseURL is the server base URL (scheme://host:port) used to
	// fetch the agent binary on first run when AgentBinary is missing. Empty
	// disables bootstrap (the agent binary must already be present).
	BootstrapBaseURL string
	// Stdout/Stderr receive the child's output (defaults to the launcher's).
	Stdout io.Writer
	Stderr io.Writer
	// Logger for launcher messages (defaults to stderr).
	Logger *log.Logger
}

// Supervisor spawns and supervises the agent, applying updates between runs.
type Supervisor struct {
	cfg      Config
	log      *log.Logger
	stopping atomic.Bool
}

// outcome of a single supervise iteration.
type outcome int

const (
	outcomeStop outcome = iota
	outcomeUpdateRequested
	outcomeRestart
	outcomeUnhealthy
)

// for internal logging.
func New(cfg Config) *Supervisor {
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stderr, "[launcher] ", log.LstdFlags)
	}
	if cfg.Stdout == nil {
		cfg.Stdout = os.Stdout
	}
	if cfg.Stderr == nil {
		cfg.Stderr = os.Stderr
	}
	if cfg.HealthTimeout <= 0 {
		cfg.HealthTimeout = 45 * time.Second
	}
	if cfg.Env == nil {
		cfg.Env = os.Environ()
	}
	return &Supervisor{cfg: cfg, log: cfg.Logger}
}

func (s *Supervisor) logf(format string, args ...interface{}) {
	s.log.Printf(format, args...)
}

// Run supervises the agent until ctx is cancelled. It applies any pending
// update before each spawn, health-checks freshly-updated agents, rolls back on
// failure, and restarts crashes with exponential backoff.
func (s *Supervisor) Run(ctx context.Context) error {
	s.reconcile()

	healthVersion := "" // set to the target version right after a successful swap
	backoff := time.Second

	for {
		if ctx.Err() != nil {
			return nil
		}

		// First-run bootstrap: fetch the agent binary if it isn't present yet.
		if _, err := os.Stat(s.cfg.AgentBinary); os.IsNotExist(err) {
			if berr := s.bootstrapAgent(); berr != nil {
				s.logf("bootstrap failed: %v", berr)
				if !s.sleep(ctx, backoff) {
					return nil
				}
				backoff = nextBackoff(backoff)
				continue
			}
			backoff = time.Second
		}

		// Apply a pending update instruction before spawning. This also covers
		// the "agent wrote the instruction then crashed without exiting 75"
		// case, since we check for the file regardless of how the child exited.
		if instr, err := updateipc.ReadInstruction(s.cfg.ConfigDir); err != nil {
			s.logf("failed to read update instruction: %v", err)
		} else if instr != nil {
			if applied, target := s.applyUpdateSwap(*instr); applied {
				healthVersion = target
				s.logf("swapped in agent %s; health-checking on next start", target)
			} else {
				healthVersion = ""
			}
		}

		oc := s.superviseOnce(ctx, healthVersion)
		switch oc {
		case outcomeStop:
			return nil
		case outcomeUpdateRequested:
			healthVersion = ""
			backoff = time.Second
			continue
		case outcomeUnhealthy:
			s.logf("update unhealthy; rolling back to previous agent binary")
			s.rollback()
			healthVersion = ""
			backoff = time.Second
			continue
		case outcomeRestart:
			healthVersion = ""
			if !s.sleep(ctx, backoff) {
				return nil
			}
			backoff = nextBackoff(backoff)
			continue
		}
	}
}

// superviseOnce spawns the agent and supervises it until it exits, an update is
// requested, the post-update health check fails, or ctx is cancelled.
func (s *Supervisor) superviseOnce(ctx context.Context, healthCheckVersion string) outcome {
	// A pending instruction present at this point (e.g. wrote-then-crashed)
	// means: go apply it before spawning.
	if instr, _ := updateipc.ReadInstruction(s.cfg.ConfigDir); instr != nil {
		return outcomeUpdateRequested
	}

	_ = updateipc.ClearReady(s.cfg.ConfigDir)

	cmd := exec.Command(s.cfg.AgentBinary, s.cfg.AgentArgs...)
	cmd.Env = s.cfg.Env
	cmd.Dir = s.cfg.WorkDir
	cmd.Stdout = s.cfg.Stdout
	cmd.Stderr = s.cfg.Stderr
	cmd.Stdin = nil
	setSysProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		s.logf("failed to start agent %q: %v", s.cfg.AgentBinary, err)
		return outcomeRestart
	}
	s.logf("agent started (pid=%d)", cmd.Process.Pid)

	exitCh := make(chan error, 1)
	go func() { exitCh <- cmd.Wait() }()

	var healthCh <-chan bool
	if healthCheckVersion != "" {
		healthCh = s.healthCheck(ctx, healthCheckVersion)
	}
	healthConfirmed := false

	for {
		select {
		case <-ctx.Done():
			s.stopping.Store(true)
			s.logf("stop requested; signaling agent to shut down")
			_ = gracefulStop(cmd)
			s.awaitExit(exitCh, cmd)
			return outcomeStop

		case healthy := <-healthCh:
			healthCh = nil
			if !healthy {
				s.logf("post-update health check failed; terminating agent for rollback")
				_ = killProcess(cmd)
				s.awaitExit(exitCh, cmd)
				return outcomeUnhealthy
			}
			healthConfirmed = true
			s.logf("post-update health check passed")

		case err := <-exitCh:
			if s.stopping.Load() {
				return outcomeStop
			}
			code := exitCodeFromCmd(cmd)
			// An update instruction present OR the update exit code means the
			// agent handed off for a binary swap.
			if instr, _ := updateipc.ReadInstruction(s.cfg.ConfigDir); instr != nil || code == updateipc.ExitCodeUpdateRequested {
				s.logf("agent requested update (exit=%d)", code)
				return outcomeUpdateRequested
			}
			// Exited during a pending health check (before it passed): the new
			// binary is bad -> roll back.
			if healthCheckVersion != "" && !healthConfirmed {
				s.logf("updated agent exited (code=%d) before becoming healthy; rolling back", code)
				return outcomeUnhealthy
			}
			s.logf("agent exited (code=%d, err=%v); restarting", code, err)
			return outcomeRestart
		}
	}
}

// healthCheck reports whether a freshly-swapped agent became healthy within the
// health window. It returns true when ready.json shows the expected version, or
// when the window elapses with the child still running (stay-alive floor). It
// returns false only if ctx is cancelled first; a child that exits early is
// detected by superviseOnce, not here.
func (s *Supervisor) healthCheck(ctx context.Context, expectVersion string) <-chan bool {
	out := make(chan bool, 1)
	go func() {
		deadline := time.Now().Add(s.cfg.HealthTimeout)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				out <- false
				return
			case <-ticker.C:
				if info, _ := updateipc.ReadReady(s.cfg.ConfigDir); info != nil && normalizeVersion(info.Version) == normalizeVersion(expectVersion) {
					out <- true
					return
				}
				if time.Now().After(deadline) {
					// Window elapsed and the child is still alive (else
					// superviseOnce would have returned already): accept it.
					out <- true
					return
				}
			}
		}
	}()
	return out
}

// awaitExit drains the child's exit, hard-killing it if it lingers.
func (s *Supervisor) awaitExit(exitCh <-chan error, cmd *exec.Cmd) {
	select {
	case <-exitCh:
	case <-time.After(20 * time.Second):
		s.logf("agent did not exit within grace period; killing")
		_ = killProcess(cmd)
		<-exitCh
	}
}

// sleep waits for d or ctx cancellation; returns false if cancelled.
func (s *Supervisor) sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// nextBackoff computes the next exponential backoff by doubling d and capping the result at 30 seconds.
func nextBackoff(d time.Duration) time.Duration {
	d *= 2
	if d > 30*time.Second {
		return 30 * time.Second
	}
	return d
}

// exitCodeFromCmd returns the child's exit code (-1 if unknown / signaled).
// exitCodeFromCmd returns the exit code of the provided command's process, or -1 if the process state is not available.
func exitCodeFromCmd(cmd *exec.Cmd) int {
	if cmd.ProcessState == nil {
		return -1
	}
	return cmd.ProcessState.ExitCode()
}

// AgentBackupPath / AgentNewPath are siblings of the agent binary (same dir, so
// rename is atomic on the same filesystem).
func (s *Supervisor) backupPath() string { return s.cfg.AgentBinary + ".bak" }
func (s *Supervisor) newPath() string    { return s.cfg.AgentBinary + ".new" }

// reconcile repairs partial update state left by a launcher crash: it removes a
// leftover .new staging file, and restores the agent from .bak if the agent
// binary is missing (e.g. crash between remove and rename on Windows).
func (s *Supervisor) reconcile() {
	if _, err := os.Stat(s.newPath()); err == nil {
		_ = os.Remove(s.newPath())
		s.logf("reconcile: removed leftover staging binary")
	}
	if _, err := os.Stat(s.cfg.AgentBinary); os.IsNotExist(err) {
		if _, berr := os.Stat(s.backupPath()); berr == nil {
			if err := replaceFile(s.cfg.AgentBinary, s.backupPath()); err != nil {
				s.logf("reconcile: failed to restore agent from backup: %v", err)
			} else {
				s.logf("reconcile: restored agent binary from backup")
			}
		}
	}
}

// rollback restores the agent binary from its backup.
func (s *Supervisor) rollback() {
	if _, err := os.Stat(s.backupPath()); err != nil {
		s.logf("rollback: no backup available")
		return
	}
	if err := copyFile(s.backupPath(), s.cfg.AgentBinary); err != nil {
		s.logf("rollback: failed to restore agent binary: %v", err)
		return
	}
	s.logf("rollback: restored previous agent binary")
}
