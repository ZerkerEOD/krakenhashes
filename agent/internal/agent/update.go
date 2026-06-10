package agent

import (
	"encoding/json"
	"runtime"
	"time"

	"github.com/ZerkerEOD/krakenhashes/agent/internal/config"
	"github.com/ZerkerEOD/krakenhashes/agent/internal/jobs"
	"github.com/ZerkerEOD/krakenhashes/agent/internal/updateipc"
	"github.com/ZerkerEOD/krakenhashes/agent/internal/version"
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/debug"
)

// UpdateExitChan returns a channel that is closed when the agent has accepted
// an update command and should exit with updateipc.ExitCodeUpdateRequested so
// its launcher performs the binary swap. main() selects on this alongside the
// OS signal channel.
func (c *Connection) UpdateExitChan() <-chan struct{} {
	return c.updateExit
}

// requestUpdateExit signals main() to exit for an update (idempotent).
func (c *Connection) requestUpdateExit() {
	c.updateExitOnce.Do(func() {
		close(c.updateExit)
	})
}

// handleAgentUpdateCommand processes a server update command. It only proceeds
// when the agent is idle (never interrupts a running job); on acceptance it
// writes the launcher instruction and signals main() to exit with the update
// code. Runs on its own goroutine off the read loop.
func (c *Connection) handleAgentUpdateCommand(payload json.RawMessage) {
	var cmd AgentUpdateCommandPayload
	if err := json.Unmarshal(payload, &cmd); err != nil {
		debug.Error("Failed to unmarshal agent update command: %v", err)
		return
	}

	debug.Info("Received agent update command: target=%s (current=%s)", cmd.TargetVersion, version.GetVersion())

	if cmd.TargetVersion == "" || cmd.Checksum == "" {
		c.sendAgentUpdateAck(cmd.TargetVersion, "rejected", "missing target version or checksum")
		return
	}
	if cmd.TargetVersion == version.GetVersion() {
		c.sendAgentUpdateAck(cmd.TargetVersion, "rejected", "already on target version")
		return
	}

	// Never interrupt a running job: defer the update if busy. The backend
	// only sends this to idle agents, but the agent double-checks locally to
	// close any race between the backend's view and ours.
	if c.jobManager != nil {
		state, _ := c.jobManager.GetState()
		hasPending, _ := c.jobManager.GetCompletionPending()
		if state != jobs.TaskStateIdle || hasPending {
			debug.Info("Deferring update %s: agent busy (state=%v, completionPending=%v)", cmd.TargetVersion, state, hasPending)
			c.sendAgentUpdateAck(cmd.TargetVersion, "deferred_busy", "agent has an active task")
			return
		}
		// Refuse any new task_assignment from this instant so nothing starts
		// between writing the instruction and exiting. BeginShutdown isn't on
		// the JobManager interface, so assert to the concrete type (as the
		// rest of connection.go does).
		if jm, ok := c.jobManager.(*jobs.JobManager); ok {
			jm.BeginShutdown()
		}
	}

	instr := updateipc.UpdateInstruction{
		SchemaVersion:  updateipc.CurrentSchemaVersion,
		TargetVersion:  cmd.TargetVersion,
		FromVersion:    version.GetVersion(),
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		DownloadURL:    cmd.DownloadURL,
		ServerBaseURL:  c.urlConfig.BaseURL,
		SHA256:         cmd.Checksum,
		RequestedAtUTC: time.Now().UTC().Format(time.RFC3339),
	}

	if err := updateipc.WriteInstruction(config.GetConfigDir(), instr); err != nil {
		debug.Error("Failed to write update instruction: %v", err)
		c.sendAgentUpdateAck(cmd.TargetVersion, "rejected", "failed to write update instruction")
		return
	}

	debug.Info("Update instruction written; signaling exit for launcher to apply %s", cmd.TargetVersion)
	c.sendAgentUpdateAck(cmd.TargetVersion, "accepted", "")
	c.requestUpdateExit()
}

// sendAgentUpdateAck reports the agent's response to an update command.
func (c *Connection) sendAgentUpdateAck(targetVersion, status, detail string) {
	body, err := json.Marshal(AgentUpdateAckPayload{
		TargetVersion: targetVersion,
		Status:        status,
		Detail:        detail,
	})
	if err != nil {
		debug.Error("Failed to marshal update ack: %v", err)
		return
	}
	msg := &WSMessage{
		Type:      WSTypeAgentUpdateAck,
		Payload:   body,
		Timestamp: time.Now(),
	}
	if !c.safeSendMessage(msg, 2000) {
		debug.Warning("Failed to queue update ack (channel blocked or closed)")
	}
}
