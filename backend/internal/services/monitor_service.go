package services

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/cache/filehash"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/config"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
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

// filteredRegenNotifier implements monitor.RegenFailureNotifier. It dispatches an
// auditable `wordlist_regen_failed` notification, which the dispatcher automatically
// writes to the audit_log and broadcasts as a system alert to admins (GH #40
// follow-up). It resolves the global dispatcher lazily, since the dispatcher is wired
// up during route setup, after the monitor is constructed.
type filteredRegenNotifier struct{}

func (filteredRegenNotifier) NotifyFilteredRegenFailed(ctx context.Context, child *models.Wordlist, cause error) {
	dispatcher := GetGlobalDispatcher()
	if dispatcher == nil {
		debug.Warning("No notification dispatcher available; dropping filtered-wordlist regen failure for %d (%s)", child.ID, child.Name)
		return
	}
	params := models.NotificationDispatchParams{
		UserID:  child.CreatedBy,
		Type:    models.NotificationTypeWordlistRegenFailed,
		Title:   "Filtered wordlist regeneration failed",
		Message: fmt.Sprintf("Automatic regeneration of filtered wordlist %q failed: %v", child.Name, cause),
		Data: map[string]interface{}{
			"wordlist_id":        child.ID,
			"wordlist_name":      child.Name,
			"parent_wordlist_id": child.ParentWordlistID,
			"cause":              cause.Error(),
		},
		SourceType: "wordlist",
		SourceID:   strconv.Itoa(child.ID),
	}
	if err := dispatcher.Dispatch(ctx, params); err != nil {
		debug.Error("Failed to dispatch filtered-wordlist regen failure notification for %d: %v", child.ID, err)
	}
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
		filteredRegenNotifier{}, // auto-regen failures → admins + audit log (GH #40)
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
