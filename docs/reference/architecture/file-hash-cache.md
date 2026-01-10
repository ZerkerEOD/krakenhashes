# File Hash Cache System

## Overview

The File Hash Cache is an in-memory caching system that optimizes the directory monitor service by eliminating redundant MD5 hash calculations for unchanged files. This dramatically reduces disk I/O and prevents SSD wear in deployments with large wordlists and rule files.

## Problem Statement

The directory monitor service runs every 30 seconds to detect changes in wordlist and rule files. Before the cache implementation:

- **Every scan calculated MD5 hashes for ALL files** regardless of whether they changed
- **For large wordlists (10-15GB+)**, this caused ~500MB/s constant disk I/O
- **SSD wear**: Continuous reading would rapidly wear out solid-state drives
- **Resource waste**: CPU cycles spent hashing unchanged files

## Solution Architecture

### File Hash Cache (`backend/internal/cache/filehash/cache.go`)

The cache stores file metadata alongside cached hash values:

```go
type CachedFileInfo struct {
    Path    string
    ModTime time.Time
    Size    int64
    MD5Hash string
}

type Cache struct {
    entries map[string]CachedFileInfo
    mu      sync.RWMutex
}
```

**Key Features:**

1. **ModTime+Size Validation**: Before recalculating MD5, the cache checks if the file's modification time and size have changed
2. **Thread-Safe**: Uses RWMutex for concurrent read access with exclusive writes
3. **Self-Populating**: Cache entries are created on first access via `GetOrCalculate()`
4. **Background Population**: Asynchronous startup population to avoid blocking server start

### Cache Lookup Flow

```
GetOrCalculate(filePath)
    │
    ├── os.Stat(filePath) → Get current modTime, size
    │
    ├── RLock → Check cache
    │   │
    │   └── Cache hit? (modTime AND size match)
    │       │
    │       ├── YES → Return cached hash (no disk read)
    │       │
    │       └── NO → Calculate MD5, update cache
    │
    └── Return hash
```

### Integration Points

```
┌─────────────────────┐
│     main.go         │
│  (creates cache)    │
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│   MonitorService    │
│   (receives cache)  │
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│  DirectoryMonitor   │
│  (uses cache for    │
│   hash lookups)     │
└─────────────────────┘
```

## Potfile Hash History

### The Problem

During heavy crack ingestion (e.g., processing 24 million cracked passwords over several hours):

1. Potfile MD5 changes every few seconds as batches are written
2. Agent downloads potfile (takes ~30 seconds for large files)
3. By the time download completes, the potfile has changed
4. Agent's hash doesn't match current hash → triggers re-download
5. **Infinite loop**: Agent continuously re-downloads the potfile

### The Solution: Rolling Hash History

The `PotfileHistory` maintains a 5-minute window of recent potfile hashes:

```go
type PotfileHashEntry struct {
    MD5Hash   string
    Timestamp time.Time
    Size      int64
}

type PotfileHistory struct {
    entries []PotfileHashEntry
    maxAge  time.Duration  // 5 minutes
    mu      sync.RWMutex
}
```

### How It Works

1. **Recording**: After each potfile update, the new MD5 hash is added to the history
2. **Validation**: When an agent reports its potfile hash, the system checks if it matches ANY hash in the 5-minute window
3. **Acceptance**: If the agent's hash is in the history, the potfile is considered "in sync enough"
4. **Expiration**: After ingestion stops, old hashes expire, ensuring eventual consistency

### Flow During Heavy Ingestion

```
Heavy Ingestion Scenario:

t=0:   Batch N written → MD5_N added to history
t=5:   Agent starts downloading potfile
t=10:  Batch N+1 written → MD5_N+1 added to history
t=35:  Agent finishes download with MD5_N
t=35:  File sync check: "Is MD5_N valid?"
       → potfileHistory.IsValid(MD5_N) = TRUE (within 5-min window)
       → Agent skips re-download

After ingestion stops (5+ minutes idle):
t=340: Old hashes expire from history
t=345: Next agent sync: only current MD5 in history
t=350: Agent with old MD5 → IsValid() = FALSE → downloads latest
```

### WebSocket Handler Integration

In `determineFilesToSync()`, the potfile check occurs before standard MD5 comparison:

```go
for _, file := range backendFiles {
    agentFile, exists := agentFileMap[key]

    // Special handling for potfile during heavy ingestion
    if file.FileType == "wordlist" && strings.HasSuffix(file.Name, "potfile.txt") {
        if exists && h.potfileHistory.IsValid(agentFile.MD5Hash) {
            // Agent has a recent valid potfile - skip re-download
            continue
        }
    }

    // Normal comparison for all other files
    if !exists || agentFile.MD5Hash != file.MD5Hash {
        filesToSync = append(filesToSync, file)
    }
}
```

## Implementation Details

### Files Created

| File | Purpose |
|------|---------|
| `backend/internal/cache/filehash/cache.go` | File hash cache with modTime+size validation |
| `backend/internal/cache/filehash/potfile_history.go` | Rolling 5-minute potfile hash history |

### Files Modified

| File | Changes |
|------|---------|
| `backend/internal/monitor/directory_monitor.go` | Inject cache, use `GetOrCalculate()` |
| `backend/internal/services/monitor_service.go` | Accept and pass cache to DirectoryMonitor |
| `backend/cmd/server/main.go` | Create cache and history, wire dependencies |
| `backend/internal/services/potfile_service.go` | Add hash to history after updates |
| `backend/internal/handlers/websocket/handler.go` | Check potfile history during sync |
| `backend/internal/routes/websocket_with_jobs.go` | Pass potfileHistory to handler |

## Performance Metrics

### Before vs After

| Metric | Before | After | Improvement |
|--------|--------|-------|-------------|
| Disk I/O (steady state) | ~500 MB/s | Near zero | 99%+ reduction |
| MD5 calculations per cycle | All files | Only changed files | Variable |
| Memory usage | N/A | ~100 bytes/file | Minimal |
| Agent potfile re-downloads during ingestion | Continuous | None | 100% reduction |

### Memory Footprint

- **File hash cache**: ~100 bytes per file entry
- **Potfile history**: ~50 bytes per entry, pruned every 5 minutes
- **Typical deployment**: <10MB total memory overhead

## Configuration

**No configuration required.** The file hash cache and potfile history are:

- Automatically initialized at server startup
- Self-managing (automatic population and expiration)
- Transparent to users and administrators

### Startup Behavior

1. Cache is created empty
2. Background goroutine populates cache by walking directories
3. Server continues starting without waiting for population
4. Cache entries are also created on-demand during directory scans

### Skip Patterns

The following patterns are excluded from cache population:

- `potfile.txt` - Handled separately via potfile history
- `association/` - Association attack wordlists are job-specific

## Debugging

### Log Messages

Cache activity is logged at DEBUG level:

```
DEBUG: Skipping wordlist with unchanged hash: general/crackstation.txt
DEBUG: Skipping rule with unchanged hash: hashcat/best64.rule
DEBUG: Agent has valid recent potfile hash 8ef087e..., skipping sync
```

### Verifying Cache is Working

Check backend logs for "Skipping ... with unchanged hash" messages during directory monitor cycles.

### Verifying Potfile History

Look for "Agent has valid recent potfile hash" messages during agent file sync operations.

## Risk Assessment

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Cache returns stale hash | Low | modTime+size checked on every access |
| Memory exhaustion | Very Low | ~100 bytes per file, bounded by filesystem |
| Concurrency issues | Low | RWMutex pattern, proven in production |
| Potfile sync issues | Low | 5-minute window ensures eventual consistency |

## Related Documentation

- [Job Update System](job-update-system.md) - How file changes trigger job updates
- [Potfile Management](../../admin-guide/operations/potfile.md) - Potfile operational guide
- [Performance Tuning](../../admin-guide/advanced/performance.md) - General performance optimization
