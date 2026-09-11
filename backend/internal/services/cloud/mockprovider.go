package cloud

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

/*
 * MockProvider drives the entire cloud lifecycle against local
 * `agent --test-mode` processes, at zero cost.
 *
 * This is not a stub for unit tests — it is the primary development harness.
 * It exercises the real paths that matter and are otherwise only reachable by
 * spending money: provisioning, VPN-credential minting, claim-code
 * registration, job-scoped file sync, dispatch isolation, budget reservation
 * and accrual, drain, teardown, orphan reconciliation and the reaper's
 * dead-man's switch.
 *
 * It also makes the failure modes testable on demand. Killing the backend
 * between the database write and the launch, dropping a launch response, or
 * never registering an agent are all one field away here, and each costs a
 * real GPU-hour to reproduce on Vast.ai or AWS.
 */
type MockProvider struct {
	// AgentBinary is the path to a built agent binary.
	AgentBinary string
	// BackendHost is passed as --host.
	BackendHost string
	// BootDelay simulates provisioning latency.
	BootDelay time.Duration
	// HourlyRateCents is the pretend price, so budget math is exercised.
	HourlyRateCents int

	// FailLaunch makes Launch return an error, for testing the
	// reservation-release path.
	FailLaunch bool
	// DropLaunchResponse starts the process but reports failure, simulating a
	// lost API response: the instance exists and bills, but the caller never
	// learned its ID. Reconciliation by label is the only recovery.
	DropLaunchResponse bool
	// NeverRegister starts nothing, so the agent never appears, exercising the
	// ready-deadline reaper.
	NeverRegister bool
	// FailDestroy makes Destroy return an error, exercising the branch where
	// automation has lost control of something that is still billing: the
	// terminate-failure counter, the escalation to admins, and the decision to
	// keep the instance in a live state so it keeps accruing.
	FailDestroy bool

	mu        sync.Mutex
	instances map[string]*mockInstance
}

type mockInstance struct {
	label      string
	cmd        *exec.Cmd
	launchedAt time.Time
	destroyed  bool
	// configDir is this instance's private agent config directory, removed on
	// Destroy. See Launch for why each one needs its own.
	configDir string
}

// NewMockProvider creates a mock provider.
func NewMockProvider(agentBinary, backendHost string) *MockProvider {
	return &MockProvider{
		AgentBinary:     agentBinary,
		BackendHost:     backendHost,
		BootDelay:       2 * time.Second,
		HourlyRateCents: 100,
		instances:       make(map[string]*mockInstance),
	}
}

// Name identifies the provider.
func (m *MockProvider) Name() models.CloudProvider { return models.CloudProviderMock }

// Preflight checks the agent binary exists.
func (m *MockProvider) Preflight(ctx context.Context) (*PreflightReport, error) {
	r := &PreflightReport{Identity: "mock", QuotaSource: "mock", QuotaLimit: 1000}
	if m.AgentBinary == "" {
		r.Errors = append(r.Errors, "AgentBinary is not configured")
		return r, nil
	}
	if _, err := os.Stat(m.AgentBinary); err != nil {
		r.Errors = append(r.Errors, fmt.Sprintf("agent binary %q not found: %v", m.AgentBinary, err))
		return r, nil
	}
	r.OK = true
	return r, nil
}

// SearchOffers returns a single synthetic offer.
func (m *MockProvider) SearchOffers(ctx context.Context, req OfferQuery) ([]Offer, error) {
	gpus := req.MinGPUCount
	if gpus < 1 {
		gpus = 1
	}
	return []Offer{{
		ID:              "mock-offer",
		InstanceType:    "mock.gpu",
		GPUModel:        "Mock GPU",
		GPUCount:        gpus,
		HourlyRateCents: m.HourlyRateCents,
		Region:          "local",
		MaxDuration:     24 * time.Hour,
		Raw:             models.JSONMap{"mock": true},
	}}, nil
}

// Launch starts a local test-mode agent.
func (m *MockProvider) Launch(ctx context.Context, req LaunchRequest) (*LaunchResult, error) {
	if m.FailLaunch {
		return nil, fmt.Errorf("mock: launch failure injected")
	}

	now := time.Now()
	inst := &mockInstance{label: req.Label, launchedAt: now}

	if !m.NeverRegister {
		args := []string{"--test-mode", "--ephemeral", "--host", m.BackendHost}
		if code := req.Env["KH_CLAIM_CODE"]; code != "" {
			args = append(args, "--claim", code)
		}
		cmd := exec.Command(m.AgentBinary, args...)
		// Ephemeral mode reads configuration from the environment, so the
		// mock passes exactly what a real cloud image's entrypoint would.
		cmd.Env = append(os.Environ(), "KH_EPHEMERAL=true")
		for k, v := range req.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}

		/*
		 * Each mock instance gets its OWN config directory, because --ephemeral
		 * does not give it one.
		 *
		 * The agent resolves its config directory from KH_CONFIG_DIR
		 * (config.GetConfigDir), and the line above hands it os.Environ() — the
		 * BACKEND's environment, which in the dev container sets that to
		 * /etc/krakenhashes. So every mock agent wrote its identity to the same
		 * place, and the second rental of a session silently picked up the
		 * FIRST agent's agent.key and client.crt, reconnected as that agent, and
		 * never registered against its own instance. Its cloud_instances row
		 * stayed at ready_at IS NULL until the ready deadline killed it, which
		 * reads exactly like a provisioning failure and is not one.
		 *
		 * That made any rehearsal involving more than one rental impossible, and
		 * it is a pure artefact of running the agent beside the backend: a real
		 * rented machine boots with an empty disk. A private directory per
		 * instance is what restores that property.
		 *
		 * Appended AFTER req.Env so it wins over anything the caller set.
		 */
		configDir, err := os.MkdirTemp("", "kh-mock-agent-")
		if err != nil {
			return nil, fmt.Errorf("mock: create agent config dir: %w", err)
		}
		cmd.Env = append(cmd.Env, "KH_CONFIG_DIR="+configDir)
		inst.configDir = configDir

		if err := cmd.Start(); err != nil {
			os.RemoveAll(configDir)
			return nil, fmt.Errorf("mock: start agent: %w", err)
		}
		inst.cmd = cmd
	}

	m.mu.Lock()
	m.instances[req.Label] = inst
	m.mu.Unlock()

	if m.DropLaunchResponse {
		// The instance is running and billing, but we report failure. Only
		// reconciliation by label can find it — which is the point.
		return nil, fmt.Errorf("mock: launch response dropped (instance IS running under label %s)", req.Label)
	}

	return &LaunchResult{
		ProviderInstanceID: req.Label,
		LaunchedAt:         now,
		BilledFrom:         now,
		Raw:                models.JSONMap{"mock": true, "label": req.Label},
	}, nil
}

// Status reports on a mock instance.
func (m *MockProvider) Status(ctx context.Context, id string) (*InstanceStatus, error) {
	m.mu.Lock()
	inst, ok := m.instances[id]
	m.mu.Unlock()
	if !ok {
		return &InstanceStatus{ProviderInstanceID: id, State: ObservedGone, Terminal: true}, nil
	}
	if inst.destroyed {
		return &InstanceStatus{ProviderInstanceID: id, State: ObservedGone, Terminal: true}, nil
	}
	if inst.cmd != nil && inst.cmd.ProcessState != nil && inst.cmd.ProcessState.Exited() {
		return &InstanceStatus{ProviderInstanceID: id, State: ObservedError, Terminal: true,
			Message: "mock agent process exited"}, nil
	}
	if time.Since(inst.launchedAt) < m.BootDelay {
		return &InstanceStatus{ProviderInstanceID: id, State: ObservedPending}, nil
	}
	return &InstanceStatus{ProviderInstanceID: id, State: ObservedRunning}, nil
}

// ListOwned returns every mock instance, keyed by label.
func (m *MockProvider) ListOwned(ctx context.Context) (map[string]InstanceStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make(map[string]InstanceStatus, len(m.instances))
	for label, inst := range m.instances {
		if inst.destroyed {
			continue
		}
		out[label] = InstanceStatus{ProviderInstanceID: label, State: ObservedRunning}
	}
	return out, nil
}

// Destroy kills the local agent. Idempotent: destroying an unknown or
// already-destroyed instance is success, matching the contract every real
// provider adapter must honor.
func (m *MockProvider) Destroy(ctx context.Context, id string) error {
	if m.FailDestroy {
		return fmt.Errorf("mock: destroy refused (FailDestroy is set)")
	}

	m.mu.Lock()
	inst, ok := m.instances[id]
	m.mu.Unlock()
	if !ok || inst.destroyed {
		return nil
	}

	if inst.cmd != nil && inst.cmd.Process != nil {
		if err := inst.cmd.Process.Kill(); err != nil {
			debug.Warning("mock: failed to kill agent for %s: %v", id, err)
		}
		_ = inst.cmd.Wait()
	}

	// Take the identity with the machine. A real rented disk goes away at
	// teardown, so leaving these behind would let a later instance inherit this
	// one's credentials — the exact confusion the private directory prevents.
	// After Wait, so nothing is still writing into it.
	if inst.configDir != "" {
		if err := os.RemoveAll(inst.configDir); err != nil {
			debug.Warning("mock: failed to remove agent config dir for %s: %v", id, err)
		}
	}

	m.mu.Lock()
	inst.destroyed = true
	m.mu.Unlock()
	return nil
}

// CostSoFar reports no authoritative figure, so callers fall back to their own
// wall-clock estimate — the same path AWS takes before Cost Explorer catches up.
func (m *MockProvider) CostSoFar(ctx context.Context, id string) (int64, bool, error) {
	return 0, false, nil
}
