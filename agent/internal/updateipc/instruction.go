// Package updateipc defines the on-disk contract between the agent and its
// launcher/supervisor for performing a binary auto-update. It is a leaf
// package (stdlib only) so it can be imported by both the agent (which writes
// the instruction and exits with ExitCodeUpdateRequested) and the launcher
// (which reads it, swaps the binary, and restarts the agent).
//
// The handoff is deliberately file-based: no socket/port/named-pipe is needed,
// the instruction survives the agent's exit, and it is read only after the
// child has been reaped. The distinguished exit code is the trigger; the
// instruction file is the payload. Both files live in the agent's config dir,
// which both processes resolve identically (the launcher forwards
// KH_CONFIG_DIR to the agent child).
package updateipc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// ExitCodeCleanShutdown is returned on a normal Ctrl-C / SIGTERM shutdown.
	ExitCodeCleanShutdown = 0
	// ExitCodeUpdateRequested signals the launcher that the agent wants to
	// update. Chosen well clear of the 128+signal range and common crash codes.
	ExitCodeUpdateRequested = 75

	instructionFile = "update.json"
	instructionTmp  = "update.json.tmp"
	readyFile       = "ready.json"
	readyTmp        = "ready.json.tmp"

	// CurrentSchemaVersion is the instruction file format version.
	CurrentSchemaVersion = 1
)

// UpdateInstruction is written by the agent and read by the launcher to drive
// a binary update. DownloadURL may be relative to ServerBaseURL.
type UpdateInstruction struct {
	SchemaVersion  int    `json:"schema_version"`
	TargetVersion  string `json:"target_version"`
	FromVersion    string `json:"from_version"`
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	DownloadURL    string `json:"download_url"`
	ServerBaseURL  string `json:"server_base_url"`
	SHA256         string `json:"sha256"`
	RequestedAtUTC string `json:"requested_at_utc"`
	// Attempts is incremented by the launcher each time it tries this update,
	// so a launcher crash mid-update can't loop forever.
	Attempts int `json:"attempts"`
}

// ReadyInfo is written by the agent once its WebSocket connection is up, so the
// launcher can confirm a freshly-swapped agent actually came online (and on
// which version) within the health-check window.
type ReadyInfo struct {
	Version     string `json:"version"`
	PID         int    `json:"pid"`
	ConnectedAt string `json:"connected_at"`
}

// InstructionPath returns the absolute path to the agent's update instruction file
// located inside the provided configDir (filename "update.json").
func InstructionPath(configDir string) string {
	return filepath.Join(configDir, instructionFile)
}

// ReadyPath returns the absolute path to the readiness breadcrumb file inside configDir.
func ReadyPath(configDir string) string {
	return filepath.Join(configDir, readyFile)
}

// WriteInstruction atomically writes the instruction (tmp + rename) so a crash
// WriteInstruction writes instr to the package's instruction file (update.json) inside configDir.
// If instr.SchemaVersion is zero it will be set to CurrentSchemaVersion.
// The instruction is encoded as indented JSON and written with permissions 0600 to a temporary
// file which is atomically renamed into place, ensuring a reader never observes a torn file.
// It returns an error if JSON marshaling or any filesystem step (write/rename) fails.
func WriteInstruction(configDir string, instr UpdateInstruction) error {
	if instr.SchemaVersion == 0 {
		instr.SchemaVersion = CurrentSchemaVersion
	}
	data, err := json.MarshalIndent(instr, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal update instruction: %w", err)
	}
	tmp := filepath.Join(configDir, instructionTmp)
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write update instruction tmp: %w", err)
	}
	if err := os.Rename(tmp, InstructionPath(configDir)); err != nil {
		return fmt.Errorf("rename update instruction: %w", err)
	}
	return nil
}

// ReadInstruction reads and parses the update instruction file from the given
// config directory and returns the instruction structure.
//
// If the instruction file does not exist, ReadInstruction returns (nil, nil).
// If the file is present but cannot be read or parsed, it returns a wrapped
// error describing the failure.
func ReadInstruction(configDir string) (*UpdateInstruction, error) {
	data, err := os.ReadFile(InstructionPath(configDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read update instruction: %w", err)
	}
	var instr UpdateInstruction
	if err := json.Unmarshal(data, &instr); err != nil {
		return nil, fmt.Errorf("parse update instruction: %w", err)
	}
	return &instr, nil
}

// ClearInstruction removes the update instruction file from the given config directory.
// It ignores a missing file and returns a wrapped error for any other removal failure.
func ClearInstruction(configDir string) error {
	if err := os.Remove(InstructionPath(configDir)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear update instruction: %w", err)
	}
	return nil
}

// WriteReady atomically writes the readiness breadcrumb for the agent into the
// ready.json file inside configDir.
// It serializes info to JSON, writes it to ready.json.tmp with permissions 0600,
// and atomically renames the temp file into place; an error is returned if any
// step fails.
func WriteReady(configDir string, info ReadyInfo) error {
	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("marshal ready info: %w", err)
	}
	tmp := filepath.Join(configDir, readyTmp)
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write ready tmp: %w", err)
	}
	if err := os.Rename(tmp, ReadyPath(configDir)); err != nil {
		return fmt.Errorf("rename ready: %w", err)
	}
	return nil
}

// ReadReady loads the readiness breadcrumb from the config directory's ready.json.
// If the file does not exist it returns (nil, nil). On success it returns a pointer to the parsed ReadyInfo.
// I/O and JSON parsing errors are returned.
func ReadReady(configDir string) (*ReadyInfo, error) {
	data, err := os.ReadFile(ReadyPath(configDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read ready info: %w", err)
	}
	var info ReadyInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("parse ready info: %w", err)
	}
	return &info, nil
}

// ClearReady removes the readiness breadcrumb file from the given config directory.
// If the file does not exist the call succeeds without error; other filesystem errors are returned.
func ClearReady(configDir string) error {
	if err := os.Remove(ReadyPath(configDir)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear ready info: %w", err)
	}
	return nil
}
