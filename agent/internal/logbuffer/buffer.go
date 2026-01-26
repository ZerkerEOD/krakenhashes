package logbuffer

import (
	"sync"
	"time"
)

const (
	// DefaultBufferSize is the default number of log entries to keep
	DefaultBufferSize = 1000
	// MaxEntrySize is the maximum size of a single log entry in bytes
	MaxEntrySize = 2048
)

// LogEntry represents a single log entry in the buffer
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	File      string    `json:"file"`
	Line      int       `json:"line"`
	Function  string    `json:"function"`
}

// RingBuffer is a thread-safe circular buffer for log entries
type RingBuffer struct {
	mu       sync.RWMutex
	entries  []LogEntry
	head     int  // next write position
	count    int  // number of entries currently stored
	capacity int  // maximum number of entries
	full     bool // whether the buffer has wrapped
}

// New creates a new RingBuffer with the specified capacity
func New(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = DefaultBufferSize
	}
	return &RingBuffer{
		entries:  make([]LogEntry, capacity),
		capacity: capacity,
	}
}

// Add adds a new log entry to the buffer
// If the message exceeds MaxEntrySize, it will be truncated
func (rb *RingBuffer) Add(entry LogEntry) {
	// Truncate message if too large
	if len(entry.Message) > MaxEntrySize {
		entry.Message = entry.Message[:MaxEntrySize-3] + "..."
	}

	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.entries[rb.head] = entry
	rb.head = (rb.head + 1) % rb.capacity

	if rb.count < rb.capacity {
		rb.count++
	} else {
		rb.full = true
	}
}

// GetSince returns all log entries since the specified time
// Returns entries in chronological order (oldest first)
func (rb *RingBuffer) GetSince(since time.Time) []LogEntry {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.count == 0 {
		return nil
	}

	// Calculate the starting index for reading
	var start int
	if rb.full {
		start = rb.head // oldest entry is at head (it will be overwritten next)
	} else {
		start = 0
	}

	result := make([]LogEntry, 0, rb.count)

	// Read entries in chronological order
	for i := 0; i < rb.count; i++ {
		idx := (start + i) % rb.capacity
		entry := rb.entries[idx]

		if !entry.Timestamp.Before(since) {
			result = append(result, entry)
		}
	}

	return result
}

// GetAll returns all log entries in chronological order
func (rb *RingBuffer) GetAll() []LogEntry {
	return rb.GetSince(time.Time{})
}

// Clear removes all entries from the buffer
func (rb *RingBuffer) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.head = 0
	rb.count = 0
	rb.full = false
	// No need to zero out entries, they'll be overwritten
}

// Count returns the number of entries currently in the buffer
func (rb *RingBuffer) Count() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.count
}

// Capacity returns the maximum number of entries the buffer can hold
func (rb *RingBuffer) Capacity() int {
	return rb.capacity
}

// IsFull returns true if the buffer has wrapped at least once
func (rb *RingBuffer) IsFull() bool {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.full
}
