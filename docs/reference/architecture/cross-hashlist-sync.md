# Cross-Hashlist Crack Synchronization

## Overview

KrakenHashes implements a sophisticated cross-hashlist synchronization system that ensures when a hash is cracked, ALL hashlists containing that hash are automatically updated. This system maintains consistency across multiple hashlists while minimizing redundant work and ensuring agents always have current data.

## Core Concepts

### Hash Deduplication Model

KrakenHashes stores hashes in a central `hashes` table with two key fields:

- **`hash_value`**: The canonical hash value (e.g., `5F4DCC3B5AA765D61D8327DEB882CF99`)
- **`original_hash`**: The complete original line from upload (e.g., `Administrator:500:...:5F4DCC3B5AA765D61D8327DEB882CF99:::`)

**Key Insight**: Multiple users can share the same password hash but have different `original_hash` values. The system deduplicates by `hash_value` for cracking efficiency while preserving all original entries.

### Many-to-Many Relationship

The `hashlist_hashes` join table links hashlists to hashes:
```
hashlist_1 ───┐
hashlist_2 ───┼──→ hash_123 (hash_value: ABC...)
hashlist_3 ───┘
```

When `hash_123` is cracked, all three hashlists need their files regenerated.

## How It Works

### 1. Crack Detection

When an agent reports cracked hashes via the crack batch mechanism:

```go
crackedHashes := []string{
    "5F4DCC3B5AA765D61D8327DEB882CF99:password123",
    "098F6BCD4621D373CADE4E832627B4F6:test",
}
```

### 2. Hash Update

The system updates the central `hashes` table:
- Sets `is_cracked = true`
- Stores the plaintext password
- Updates `last_updated` timestamp

**Important**: ALL hashes with the same `hash_value` are marked as cracked, regardless of `original_hash` or hashlist association.

### 3. Affected Hashlist Identification

The system queries which hashlists contain the cracked hashes:

```sql
SELECT DISTINCT hl.*
FROM hashlists hl
JOIN hashlist_hashes hh ON hl.id = hh.hashlist_id
JOIN hashes h ON hh.hash_id = h.id
WHERE h.hash_value = ANY($1)
```

This identifies ALL hashlists that need file regeneration.

### 4. Counter Updates

For each affected hashlist, the system increments its `cracked_hashes` counter:

```go
// Example: If 2 cracked hashes belong to hashlists [98, 98, 99, 100]:
// - Hashlist 98 increments by 2
// - Hashlist 99 increments by 1
// - Hashlist 100 increments by 1
```

### 5. File Regeneration

Each affected hashlist file is regenerated from scratch:

**Process**:
1. Query all **uncracked** hashes for the hashlist
2. Write to temporary file: `{hashlist_id}.hash.tmp`
3. Atomically rename to `{hashlist_id}.hash`
4. Calculate new MD5 hash of the file

**Example**:
```
Before crack:
Administrator:500:...:5F4DCC3B5AA765D61D8327DEB882CF99:::
User1:501:...:098F6BCD4621D373CADE4E832627B4F6:::
Guest:502:...:E10ADC3949BA59ABBE56E057F20F883E:::

After cracking 5F4DCC3B... and 098F6BCD...:
Guest:502:...:E10ADC3949BA59ABBE56E057F20F883E:::
```

### 6. Agent Synchronization

For each affected hashlist, the system updates all agent records:
1. Updates `agent_hashlists.file_hash` to new MD5
2. This marks agent copies as outdated
3. On next connection, agents detect the mismatch
4. Agents automatically download the updated file

## Benefits

### 1. Consistency Across Hashlists

If the same hash appears in multiple hashlists (e.g., corporate environments with shared passwords), cracking it once updates all:

```
Scenario: Password "Summer2024!" used by:
- Hashlist A: john@domain.com
- Hashlist B: john.doe@otherdomain.com
- Hashlist C: jdoe@thirddomain.com

Result: Cracking ANY of these updates ALL three hashlists automatically
```

### 2. Efficient Cracking

Hashcat never receives duplicate hashes:
- Uses `DISTINCT hash_value` when generating hashlist files
- Even if 1000 users share password "Password1", hashcat only cracks it once
- System propagates the crack to all 1000 entries automatically

### 3. Real-Time Updates

Agents always work with current data:
- Stale hashlists automatically detected via MD5 mismatch
- Fresh files downloaded before task execution
- Prevents wasted work on already-cracked hashes

## Implementation Details

### Code Flow

**Backend: `HandleCrackBatch` in `job_websocket_integration.go`**

```go
// Track affected hashlists (map[hashlist_id]crack_count)
affectedHashlists := make(map[int64]int)

// Process each crack
for _, crackedEntry := range crackedHashes {
    // Update hash in database
    hash.IsCracked = true
    hash.Password = &plaintext

    // Find which hashlists contain this hash
    hashlistIDs, _ := s.hashRepo.GetHashlistIDsForHash(ctx, hash.ID)

    // Increment counter for each affected hashlist
    for _, hashlistID := range hashlistIDs {
        affectedHashlists[hashlistID]++
    }
}

// Update counters and regenerate files
for hashlistID, count := range affectedHashlists {
    s.hashlistRepo.IncrementCrackedCount(ctx, hashlistID, count)
}

// Trigger file regeneration for all affected hashlists
s.hashlistSyncService.UpdateHashlistAfterCracks(ctx, hashlistID, crackedHashValues)
```

**Backend: `UpdateHashlistAfterCracks` in `hashlist_sync_service.go`**

```go
// Find ALL hashlists containing these cracked hashes
affectedHashlists := s.hashlistRepo.GetHashlistsContainingHashes(ctx, hashValues)

// Regenerate each hashlist file
for _, hashlist := range affectedHashlists {
    // Get uncracked hashes
    uncrackedHashes := s.hashRepo.GetUncrackedHashValuesByHashlistID(ctx, hashlist.ID)

    // Write to temp file
    file.WriteString(hash + "\n") // for each uncrackedHash

    // Atomic replace
    os.Rename(tempFile, actualFile)

    // Update agent records
    for _, agentHashlist := range distribution {
        agentHashlist.FileHash = &newMD5
        s.agentHashlistRepo.CreateOrUpdate(ctx, agentHashlist)
    }
}
```

**Agent: `ensureHashlist` in `jobs.go`**

```go
// ALWAYS re-download for each task to ensure fresh copy
if _, err := os.Stat(localPath); err == nil {
    debug.Info("Removing existing hashlist to download fresh copy")
    os.Remove(localPath)
}

// Download fresh copy from backend
fileInfo := &filesync.FileInfo{
    Name:     fmt.Sprintf("%d.hash", hashlistID),
    FileType: "hashlist",
    ID:       int(hashlistID),
    MD5Hash:  "", // Skip verification for speed
}
s.fileSync.DownloadFileFromInfo(ctx, fileInfo)
```

## Performance Considerations

### Scalability

**File Regeneration Cost**: O(U) where U = uncracked hashes per hashlist
- Small hashlists (< 10k): Instant regeneration
- Medium hashlists (10k-100k): < 1 second
- Large hashlists (100k-1M): 1-5 seconds
- Very large (> 1M): 5-30 seconds

**Multi-Hashlist Impact**: If 10 hashlists share hashes, ALL 10 regenerate
- Sequential processing prevents race conditions
- Failures on one hashlist don't block others
- Agents notified asynchronously

### Optimization Strategies

1. **Batched Processing**: Cracks processed in batches (default: 50 per batch)
2. **Atomic Updates**: Temp files prevent partial writes
3. **Lazy Agent Sync**: Agents discover updates on-demand, not pushed
4. **Distinct Queries**: Hashcat receives deduplicated hashes

### Database Efficiency

**Counter Updates**: Batch increments reduce transaction overhead
```go
// Single update per hashlist, not per hash
IncrementCrackedCount(hashlistID, totalCracksForThisHashlist)
```

**Index Utilization**:
- `hash_value` indexed for fast duplicate detection
- `hashlist_hashes` join table indexed on both FKs
- `is_cracked` index for uncracked hash queries

## Edge Cases

### 1. Hash in Multiple Hashlists

**Scenario**: Same hash in 5 different hashlists

**Behavior**:
- Hash marked cracked once in `hashes` table
- All 5 hashlists get counter increments
- All 5 hashlist files regenerated
- All agents with any of the 5 hashlists notified

### 2. Partially Failed Regeneration

**Scenario**: Hashlist file regeneration fails for 1 of 5 affected hashlists

**Behavior**:
- Error logged but processing continues
- Other 4 hashlists still regenerated successfully
- Failed hashlist can retry on next crack
- Database counters still updated correctly

### 3. Agent Offline During Update

**Scenario**: Agent offline when hashlist updated

**Behavior**:
- Agent's `file_hash` still updated in database
- On reconnection, file sync detects mismatch
- Agent automatically downloads fresh file
- No manual intervention required

### 4. Empty Hashlist After Cracks

**Scenario**: All hashes in a hashlist get cracked

**Behavior**:
- Hashlist file becomes empty (0 bytes)
- File still exists (prevents 404 errors)
- Hashlist status remains "ready"
- Progress shows 100% cracked

## Monitoring and Debugging

### Key Metrics

Monitor these for cross-hashlist sync health:

1. **File Regeneration Time**: Track duration per hashlist
2. **Affected Hashlist Count**: How many hashlists per crack batch
3. **Agent Sync Lag**: Time between file update and agent download

### Debug Logging

Enable debug logging to trace sync flow:

```
DEBUG: Found affected hashlists for cross-hashlist update
DEBUG: affected_count=3, hashlist_ids=[98,99,100]
DEBUG: Regenerating hashlist file 98
DEBUG: Found 4523 uncracked hashes for hashlist 98
DEBUG: Updated hashlist 98 file and marked 5 agents for sync
```

### Common Issues

**Issue**: Agents keep downloading same hashlist repeatedly

**Cause**: File regeneration producing different MD5 each time

**Solution**: Ensure consistent hash ordering in queries (ORDER BY hash_value)

---

**Issue**: Hashlist counters don't match file contents

**Cause**: Counter increments succeeded but file regeneration failed

**Solution**: Check backend logs for file write errors, verify disk space

---

**Issue**: Cross-hashlist updates slow

**Cause**: Many hashlists sharing same hashes

**Solution**: Normal behavior, consider separating unrelated hashlists

## Best Practices

### For Users

1. **Separate Unrelated Hashlists**: Don't combine disparate hash sources if not needed
2. **Monitor Crack Rates**: Expect brief spikes in file I/O during large crack batches
3. **Agent Connectivity**: Keep agents connected for timely file updates

### For Administrators

1. **Disk I/O Monitoring**: Watch for I/O spikes during high-volume cracking
2. **Database Indexing**: Ensure indexes on join tables are maintained
3. **Log Review**: Periodically check for file regeneration failures

### For Developers

1. **Transaction Boundaries**: Always wrap counter updates and file operations together
2. **Error Handling**: Log failures but continue processing other hashlists
3. **Atomic File Operations**: Use temp files + rename for atomic updates
4. **Consistent Ordering**: Always ORDER BY hash_value for deterministic output

## Related Systems

- **[Crack Batching System](crack-batching-system.md)**: How cracks are collected and sent
- **[Job Update System](job-update-system.md)**: How jobs adapt to file changes
- **[File Sync](../../agent-guide/file-sync.md)**: Agent file synchronization mechanism

## Summary

Cross-hashlist crack synchronization ensures consistency across the entire KrakenHashes system. By automatically propagating cracks to all affected hashlists and regenerating files, the system eliminates redundant work while keeping all components synchronized. This architecture scales efficiently from small deployments to enterprise environments with thousands of hashlists.
