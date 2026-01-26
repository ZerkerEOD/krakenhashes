package services

import (
	"path/filepath"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/cache/filehash"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/config"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/monitor"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/rule"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/wordlist"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// MonitorService manages directory monitoring
type MonitorService struct {
	directoryMonitor *monitor.DirectoryMonitor
}

// NewMonitorService creates a new monitor service
func NewMonitorService(
	wordlistManager wordlist.Manager,
	ruleManager rule.Manager,
	cfg *config.Config,
	systemUserID uuid.UUID,
	jobUpdateHandler monitor.JobUpdateHandler,
	hashCache *filehash.Cache,
) *MonitorService {
	// Create directory monitor
	directoryMonitor := monitor.NewDirectoryMonitor(
		wordlistManager,
		ruleManager,
		filepath.Join(cfg.DataDir, "wordlists"),
		filepath.Join(cfg.DataDir, "rules"),
		time.Second*30, // Check every 30 seconds
		systemUserID,   // This will be the system user (uuid.Nil)
		jobUpdateHandler,
		hashCache,
	)

	return &MonitorService{
		directoryMonitor: directoryMonitor,
	}
}

// Start starts the directory monitor
func (s *MonitorService) Start() {
	debug.Info("Starting monitor service")
	s.directoryMonitor.Start()
}

// Stop stops the directory monitor
func (s *MonitorService) Stop() {
	debug.Info("Stopping monitor service")
	s.directoryMonitor.Stop()
}
