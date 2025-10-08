package version

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

var (
	// Version is set during build
	Version string

	// Versions holds all component versions
	Versions struct {
		Release  string `json:"release"`
		Backend  string `json:"backend"`
		Frontend string `json:"frontend"`
		Agent    string `json:"agent"`
		API      string `json:"api"`
		Database string `json:"database"`
	}
)

// LoadVersions loads version information from versions.json
func LoadVersions(path string) error {
	debug.Debug("Loading versions from: %s", path)

	absPath, err := filepath.Abs(path)
	if err != nil {
		debug.Error("Failed to get absolute path for versions file: %v", err)
		return err
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		debug.Error("Failed to read versions file: %v", err)
		return err
	}

	if err := json.Unmarshal(data, &Versions); err != nil {
		debug.Error("Failed to unmarshal versions data: %v", err)
		return err
	}

	debug.Info("Loaded versions - Release: %s, Backend: %s, Frontend: %s, Agent: %s",
		Versions.Release, Versions.Backend, Versions.Frontend, Versions.Agent)

	return nil
}

// GetVersionInfo returns all version information
func GetVersionInfo() map[string]string {
	debug.Debug("Retrieving version information")
	return map[string]string{
		"release":  Versions.Release,
		"backend":  Versions.Backend,
		"frontend": Versions.Frontend,
		"agent":    Versions.Agent,
		"api":      Versions.API,
		"database": Versions.Database,
	}
}
