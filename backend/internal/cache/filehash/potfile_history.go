package filehash

import (
	"sync"
	"time"
)

// PotfileHashEntry stores a single potfile hash with timestamp
type PotfileHashEntry struct {
	MD5Hash   string
	Timestamp time.Time
	Size      int64
}

// PotfileHistory keeps a rolling window of recent potfile hashes
// to handle race conditions during heavy crack ingestion
type PotfileHistory struct {
	entries []PotfileHashEntry
	maxAge  time.Duration
	mu      sync.RWMutex
}

// NewPotfileHistory creates a new potfile history with the specified max age
func NewPotfileHistory(maxAge time.Duration) *PotfileHistory {
	return &PotfileHistory{
		entries: make([]PotfileHashEntry, 0),
		maxAge:  maxAge, // typically 5 minutes
	}
}

// Add records a new potfile hash (called after UpdatePotfileMetadata)
func (h *PotfileHistory) Add(md5Hash string, size int64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Prune old entries
	cutoff := time.Now().Add(-h.maxAge)
	valid := make([]PotfileHashEntry, 0)
	for _, e := range h.entries {
		if e.Timestamp.After(cutoff) {
			valid = append(valid, e)
		}
	}

	// Add new entry
	h.entries = append(valid, PotfileHashEntry{
		MD5Hash:   md5Hash,
		Timestamp: time.Now(),
		Size:      size,
	})
}

// IsValid checks if an agent's potfile MD5 matches any recent hash
func (h *PotfileHistory) IsValid(agentMD5 string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	cutoff := time.Now().Add(-h.maxAge)
	for _, e := range h.entries {
		if e.Timestamp.After(cutoff) && e.MD5Hash == agentMD5 {
			return true
		}
	}
	return false
}

// GetCurrent returns the most recent potfile hash
func (h *PotfileHistory) GetCurrent() (string, int64, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.entries) == 0 {
		return "", 0, false
	}
	latest := h.entries[len(h.entries)-1]
	return latest.MD5Hash, latest.Size, true
}
