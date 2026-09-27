package monitor

import (
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

// The reconcile pass must only flag standalone monitor-managed wordlists as
// missing (GH #93); potfiles, ephemeral/filtered children, and
// association/client lists are owned by other flows.
func TestIsMonitorManagedWordlist(t *testing.T) {
	parent := 7
	job := uuid.New()
	tests := []struct {
		name string
		wl   *models.Wordlist
		want bool
	}{
		{"standalone", &models.Wordlist{FileName: "general/rockyou.txt"}, true},
		{"potfile", &models.Wordlist{FileName: "custom/potfile.txt", IsPotfile: true}, false},
		{"ephemeral", &models.Wordlist{FileName: "__eph__x.txt", IsEphemeral: true}, false},
		{"filtered child", &models.Wordlist{FileName: "general/filtered.txt", ParentWordlistID: &parent}, false},
		{"association", &models.Wordlist{FileName: "association/a.txt"}, false},
		{"client", &models.Wordlist{FileName: "clients/c.txt"}, false},
		{"ephemeral by job only", &models.Wordlist{FileName: "general/j.txt", IsEphemeral: true, OwnerJobID: &job}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isMonitorManagedWordlist(tt.wl); got != tt.want {
				t.Errorf("isMonitorManagedWordlist(%s) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}
