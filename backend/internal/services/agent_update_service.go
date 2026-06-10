package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// System setting keys for the agent auto-update pipeline (migration 000163).
const (
	SettingAgentAutoUpdateEnabled      = "agent_auto_update_enabled"
	SettingAgentUpdateMaxConcurrent    = "agent_update_max_concurrent"
	SettingAgentUpdateHealthTimeoutSec = "agent_update_health_timeout_seconds"
	SettingAgentUpdateMaxAttempts      = "agent_update_max_attempts"
)

// Defaults mirror migration 000163 so a missing/unparseable setting never
// breaks the calling path.
const (
	defaultAgentUpdateMaxConcurrent    = 2
	defaultAgentUpdateHealthTimeoutSec = 300
	defaultAgentUpdateMaxAttempts      = 3
)

// AgentUpdateSender pushes an update command to a connected agent over its
// WebSocket. It is implemented by the WebSocket handler and injected after
// construction. Kept as a narrow interface here so the services package does
// not import the websocket packages (which would create an import cycle).
type AgentUpdateSender interface {
	SendAgentUpdateCommand(agentID int, targetVersion, downloadURL, checksum string) error
}

// AgentUpdateConfig is the resolved auto-update configuration read from
// system_settings.
type AgentUpdateConfig struct {
	Enabled          bool
	MaxConcurrent    int
	HealthTimeoutSec int
	MaxAttempts      int
}

// AgentUpdateService decides when a version-stale agent should auto-update and
// drives the update lifecycle: it flips an idle agent into the 'updating'
// status (so the scheduler skips it) and sends the update command. The agent's
// launcher does the actual binary swap and restart; the relaunched agent
// reports its new version, which closes the loop via OnVersionReported.
//
// Concurrency, the never-interrupt-a-running-job guarantee, and retry/give-up
// are all enforced here. The service is safe to call with auto-update disabled
// (it becomes a no-op) so call sites need no conditional.
type AgentUpdateService struct {
	agentRepo     *repository.AgentRepository
	settingsRepo  *repository.SystemSettingsRepository
	binaryService *AgentBinaryService
	sender        AgentUpdateSender
}

// NewAgentUpdateService constructs the service. Call SetSender once the
// WebSocket handler exists to enable command delivery.
func NewAgentUpdateService(agentRepo *repository.AgentRepository, settingsRepo *repository.SystemSettingsRepository, binaryService *AgentBinaryService) *AgentUpdateService {
	return &AgentUpdateService{
		agentRepo:     agentRepo,
		settingsRepo:  settingsRepo,
		binaryService: binaryService,
	}
}

// SetSender wires the WebSocket command sender (resolves the handler<->service
// construction-order cycle).
func (s *AgentUpdateService) SetSender(sender AgentUpdateSender) {
	s.sender = sender
}

// ExpectedVersion returns the agent version the cluster expects (versions.json).
func (s *AgentUpdateService) ExpectedVersion() string {
	return s.binaryService.GetVersion()
}

// Config reads the four auto-update settings, falling back to defaults.
func (s *AgentUpdateService) Config(ctx context.Context) AgentUpdateConfig {
	enabled, _ := s.settingsRepo.GetSettingBool(ctx, SettingAgentAutoUpdateEnabled, true)
	return AgentUpdateConfig{
		Enabled:          enabled,
		MaxConcurrent:    s.getSettingInt(ctx, SettingAgentUpdateMaxConcurrent, defaultAgentUpdateMaxConcurrent),
		HealthTimeoutSec: s.getSettingInt(ctx, SettingAgentUpdateHealthTimeoutSec, defaultAgentUpdateHealthTimeoutSec),
		MaxAttempts:      s.getSettingInt(ctx, SettingAgentUpdateMaxAttempts, defaultAgentUpdateMaxAttempts),
	}
}

func (s *AgentUpdateService) getSettingInt(ctx context.Context, key string, def int) int {
	setting, err := s.settingsRepo.GetSetting(ctx, key)
	if err != nil || setting == nil || setting.Value == nil {
		return def
	}
	v, err := strconv.Atoi(strings.TrimSpace(*setting.Value))
	if err != nil {
		return def
	}
	return v
}

// IsStale reports whether an agent on agentVersion should be updated to the
// expected version. Uses a component-wise numeric compare (so 1.10.0 > 1.9.0)
// and never triggers on unknown/dev/non-semver versions (no downgrades, no
// touching dev builds).
func (s *AgentUpdateService) IsStale(agentVersion string) bool {
	expected := s.binaryService.GetVersion()
	if expected == "" || agentVersion == "" {
		return false
	}
	return semverLess(agentVersion, expected)
}

// ResolveIdleState is the interception hook called by every path that marks an
// agent active. The caller writes 'active' first; this then, when the agent is
// version-stale and auto-update is on, atomically flips it to 'updating' and
// sends the update command (if a concurrency slot is free), or flags it
// update_pending so the sweeper promotes it later. It is fire-and-forget and
// fully self-healing: any error leaves the agent 'active'.
func (s *AgentUpdateService) ResolveIdleState(ctx context.Context, agentID int) {
	cfg := s.Config(ctx)
	if !cfg.Enabled {
		return
	}
	agent, err := s.agentRepo.GetByID(ctx, agentID)
	if err != nil {
		debug.Error("auto-update: failed to load agent %d for readiness: %v", agentID, err)
		return
	}
	if !s.IsStale(agent.Version) {
		if agent.UpdatePending {
			if err := s.agentRepo.ClearUpdatePending(ctx, agentID); err != nil {
				debug.Error("auto-update: failed to clear update_pending for agent %d: %v", agentID, err)
			}
		}
		return
	}
	// Stale and given up: leave alone (admin can retry).
	if agent.UpdateAttempts >= cfg.MaxAttempts {
		return
	}
	if s.tryStartUpdate(ctx, agent, cfg) {
		return
	}
	// Couldn't start now (cap full, backoff, or lost the race): queue it.
	if err := s.agentRepo.MarkUpdatePending(ctx, agentID, s.binaryService.GetVersion()); err != nil {
		debug.Error("auto-update: failed to mark agent %d update_pending: %v", agentID, err)
	}
}

// OnVersionReported is called after an agent reports its version. If the agent
// is mid-update and now runs the target version, the update is complete and it
// returns to 'active'.
func (s *AgentUpdateService) OnVersionReported(ctx context.Context, agentID int, reportedVersion string) {
	agent, err := s.agentRepo.GetByID(ctx, agentID)
	if err != nil || agent.Status != models.AgentStatusUpdating {
		return
	}
	target := agent.TargetVersion.String
	if target == "" {
		target = s.binaryService.GetVersion()
	}
	if normalizeVersion(reportedVersion) == normalizeVersion(target) {
		if err := s.agentRepo.CompleteUpdate(ctx, agentID); err != nil {
			debug.Error("auto-update: failed to complete update for agent %d: %v", agentID, err)
			return
		}
		debug.Info("auto-update: agent %d completed update to %s", agentID, reportedVersion)
	}
}

// PromotePending is the sweeper's "promote" pass and the reliable trigger for
// auto-updates: it scans every idle active agent, and starts an update for any
// that is version-stale (respecting the cap, attempts, and backoff). Scanning
// idle agents directly — rather than relying on update_pending being pre-set —
// means an agent that became active via any path (heartbeat, sync, reconnect)
// is still caught within one sweep interval. The connect-time ResolveIdleState
// is just a fast path on top of this.
func (s *AgentUpdateService) PromotePending(ctx context.Context) {
	cfg := s.Config(ctx)
	if !cfg.Enabled {
		return
	}
	idle, err := s.agentRepo.ListIdleActiveAgentVersions(ctx)
	if err != nil {
		debug.Error("auto-update: failed to list idle active agents: %v", err)
		return
	}
	for id, version := range idle {
		// Cheap pre-filter: skip agents already on the expected version.
		if !s.IsStale(version) {
			continue
		}
		agent, err := s.agentRepo.GetByID(ctx, id)
		if err != nil {
			continue
		}
		if !s.IsStale(agent.Version) {
			_ = s.agentRepo.ClearUpdatePending(ctx, id)
			continue
		}
		// Exhausted retries: give up and clear the pending flag so the UI and
		// future sweeps stop treating it as queued.
		if agent.UpdateAttempts >= cfg.MaxAttempts {
			_ = s.agentRepo.ClearUpdatePending(ctx, id)
			continue
		}
		s.tryStartUpdate(ctx, agent, cfg)
	}
}

// SweepTimedOut is the sweeper's "timeout" pass: agents that have been
// 'updating' longer than the health timeout are declared failed. After this,
// the agent reconnecting on the old binary re-enters via ResolveIdleState and
// retries until MaxAttempts, then is left in error for the admin.
func (s *AgentUpdateService) SweepTimedOut(ctx context.Context) []int {
	cfg := s.Config(ctx)
	ids, err := s.agentRepo.ListTimedOutUpdates(ctx, cfg.HealthTimeoutSec)
	if err != nil {
		debug.Error("auto-update: failed to list timed-out updates: %v", err)
		return nil
	}
	var failed []int
	for _, id := range ids {
		if err := s.agentRepo.FailUpdate(ctx, id, "update health-check timed out: agent did not reconnect on the target version"); err != nil {
			debug.Error("auto-update: failed to fail timed-out update for agent %d: %v", id, err)
			continue
		}
		debug.Warning("auto-update: agent %d update timed out after %ds", id, cfg.HealthTimeoutSec)
		failed = append(failed, id)
	}
	return failed
}

// RetryUpdate clears an agent's update error/attempt state and re-queues it
// (admin manual recovery action).
func (s *AgentUpdateService) RetryUpdate(ctx context.Context, agentID int) error {
	return s.agentRepo.ResetUpdateState(ctx, agentID, true)
}

// tryStartUpdate attempts to begin an update for a loaded, version-stale agent.
// Returns true only when the agent has been atomically transitioned to
// 'updating' AND the command was sent. Enforces give-up, backoff, the
// concurrency cap, and the never-interrupt-a-running-job race guard (via
// BeginUpdate's NOT EXISTS clause).
func (s *AgentUpdateService) tryStartUpdate(ctx context.Context, agent *models.Agent, cfg AgentUpdateConfig) bool {
	if s.sender == nil {
		return false
	}
	if !eligibleForRetry(agent, cfg.MaxAttempts) {
		return false
	}
	count, err := s.agentRepo.CountUpdating(ctx)
	if err != nil {
		debug.Error("auto-update: count updating failed: %v", err)
		return false
	}
	if count >= cfg.MaxConcurrent {
		return false
	}
	binary, err := s.binaryForAgent(agent)
	if err != nil {
		// No binary available for this platform — can't update; don't churn.
		debug.Warning("auto-update: no binary for agent %d: %v", agent.ID, err)
		return false
	}
	target := s.binaryService.GetVersion()

	ok, err := s.agentRepo.BeginUpdate(ctx, agent.ID, target)
	if err != nil {
		debug.Error("auto-update: BeginUpdate failed for agent %d: %v", agent.ID, err)
		return false
	}
	if !ok {
		// Lost the race (agent got a task or is no longer active).
		return false
	}

	if err := s.sender.SendAgentUpdateCommand(agent.ID, target, binary.DownloadURL, binary.Checksum); err != nil {
		debug.Error("auto-update: failed to send update command to agent %d: %v", agent.ID, err)
		if ferr := s.agentRepo.FailUpdate(ctx, agent.ID, fmt.Sprintf("failed to send update command: %v", err)); ferr != nil {
			debug.Error("auto-update: failed to mark send failure for agent %d: %v", agent.ID, ferr)
		}
		return false
	}
	debug.Info("auto-update: agent %d updating %s -> %s", agent.ID, agent.Version, target)
	return true
}

// binaryForAgent resolves the agent binary matching the agent's reported
// platform (os_info.platform/arch are Go's GOOS/GOARCH, which key the binary
// service directly).
func (s *AgentUpdateService) binaryForAgent(agent *models.Agent) (*BinaryInfo, error) {
	osName, arch := agentPlatform(agent)
	if osName == "" || arch == "" {
		return nil, fmt.Errorf("agent %d has unknown os/arch", agent.ID)
	}
	return s.binaryService.GetBinary(osName, arch)
}

func agentPlatform(agent *models.Agent) (string, string) {
	if len(agent.OSInfo) == 0 {
		return "", ""
	}
	var info map[string]interface{}
	if err := json.Unmarshal(agent.OSInfo, &info); err != nil {
		return "", ""
	}
	osName, _ := info["platform"].(string)
	arch, _ := info["arch"].(string)
	return osName, arch
}

// eligibleForRetry enforces the give-up threshold and an exponential backoff
// window between attempts (30s, 60s, 120s, ... capped at 1h).
func eligibleForRetry(agent *models.Agent, maxAttempts int) bool {
	if agent.UpdateAttempts >= maxAttempts {
		return false
	}
	if agent.UpdateAttempts == 0 || !agent.UpdateLastAttemptAt.Valid {
		return true
	}
	backoff := 30 * time.Second
	for i := 1; i < agent.UpdateAttempts; i++ {
		backoff *= 2
		if backoff >= time.Hour {
			backoff = time.Hour
			break
		}
	}
	return time.Since(agent.UpdateLastAttemptAt.Time) >= backoff
}

// normalizeVersion trims a leading 'v' and surrounding whitespace for equality
// comparison.
func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// semverLess reports whether version a is strictly older than b using a
// component-wise numeric comparison. Tolerates a leading 'v' and
// pre-release/build suffixes. If either version doesn't parse to at least one
// numeric component, returns false (treat as not-older).
func semverLess(a, b string) bool {
	pa, oka := parseSemver(a)
	pb, okb := parseSemver(b)
	if !oka || !okb {
		return false
	}
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}

func parseSemver(v string) ([3]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	var out [3]int
	any := false
	for i := 0; i < 3 && i < len(parts); i++ {
		n, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err != nil {
			return out, false
		}
		out[i] = n
		any = true
	}
	return out, any
}
