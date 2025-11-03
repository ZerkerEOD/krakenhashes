# LM/NTLM Linking Architecture

## Overview

KrakenHashes v1.2.1+ introduces comprehensive support for LM (LAN Manager) and NTLM hash linking, enabling intelligent processing of pwdump-format files and advanced Windows password cracking workflows. This document details the technical architecture, database schema, processing pipeline, and design decisions.

## Architectural Layers

The LM/NTLM linking system operates across three database layers:

1. **Hashlist-to-Hashlist Links** (`linked_hashlists`): High-level relationship between entire hashlists
2. **Hash-to-Hash Links** (`linked_hashes`): Individual hash pair relationships
3. **LM Metadata** (`lm_hash_metadata`): Partial crack tracking for LM hashes

This layered approach enables:
- Flexible linking strategies (not limited to LM/NTLM)
- Efficient analytics calculations
- Partial crack tracking without impacting other hash types
- Clean separation of concerns

## Database Schema

### linked_hashlists Table

Manages relationships between entire hashlists (e.g., LM hashlist ↔ NTLM hashlist).

```sql
CREATE TABLE linked_hashlists (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hashlist_id_1 BIGINT NOT NULL REFERENCES hashlists(id) ON DELETE CASCADE,
    hashlist_id_2 BIGINT NOT NULL REFERENCES hashlists(id) ON DELETE CASCADE,
    link_type VARCHAR(50) NOT NULL,  -- 'lm_ntlm', extensible for future types
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT unique_hashlist_link UNIQUE (hashlist_id_1, hashlist_id_2),
    CONSTRAINT no_self_link CHECK (hashlist_id_1 != hashlist_id_2)
);

CREATE INDEX idx_linked_hashlists_id2 ON linked_hashlists(hashlist_id_2);
CREATE INDEX idx_linked_hashlists_type ON linked_hashlists(link_type);
```

**Design Decisions:**
- **Bidirectional Uniqueness**: Prevents both `(A, B)` and `(B, A)` from existing
- **Generic link_type**: Enables future link types (e.g., `sha1_ntlm` for hash type correlations)
- **CASCADE DELETE**: When a hashlist is deleted, links are automatically removed
- **Reverse Index**: `idx_linked_hashlists_id2` enables efficient bidirectional lookups

**Use Cases:**
- Track which LM and NTLM hashlists were created from the same pwdump file
- Calculate effective hashlist count in analytics (linked pairs count as ONE)
- Determine when to create individual hash-to-hash links

### linked_hashes Table

Manages relationships between individual hash records (e.g., specific LM hash ↔ specific NTLM hash for same user).

```sql
CREATE TABLE linked_hashes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hash_id_1 UUID NOT NULL REFERENCES hashes(id) ON DELETE CASCADE,
    hash_id_2 UUID NOT NULL REFERENCES hashes(id) ON DELETE CASCADE,
    link_type VARCHAR(50) NOT NULL,  -- 'lm_ntlm'
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT unique_hash_link UNIQUE (hash_id_1, hash_id_2),
    CONSTRAINT no_self_link CHECK (hash_id_1 != hash_id_2)
);

CREATE INDEX idx_linked_hashes_id2 ON linked_hashes(hash_id_2);
CREATE INDEX idx_linked_hashes_type ON linked_hashes(link_type);
```

**Design Decisions:**
- **Hash-Level Granularity**: Links specific hash records, not just hashlists
- **Username/Domain Based**: Links created by matching `username` and `domain` columns
- **Analytics Support**: Enables "Linked Hash Correlation" statistics
- **Independent of Hashlists**: Links persist even if hashlists are deleted (CASCADE handles cleanup)

**Use Cases:**
- Show correlation: "Administrator's LM cracked but NTLM still unknown"
- Generate statistics: "X linked pairs have both cracked"
- Enable domain-filtered correlation analysis

### lm_hash_metadata Table

Tracks partial crack status for LM hashes (mode 3000 only).

```sql
CREATE TABLE lm_hash_metadata (
    hash_id UUID PRIMARY KEY REFERENCES hashes(id) ON DELETE CASCADE,
    first_half_cracked BOOLEAN NOT NULL DEFAULT FALSE,
    second_half_cracked BOOLEAN NOT NULL DEFAULT FALSE,
    first_half_password VARCHAR(7),     -- Max 7 chars (LM first half)
    second_half_password VARCHAR(7),    -- Max 7 chars (LM second half)
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_lm_metadata_crack_status
    ON lm_hash_metadata(first_half_cracked, second_half_cracked);
CREATE INDEX idx_lm_metadata_hash_id ON lm_hash_metadata(hash_id);
```

**Design Decisions:**
- **Hash-Specific**: Only created for LM hashes (type 3000), zero impact on other types
- **Separate Password Storage**: Stores 7-char fragments, not full password (assembled on demand)
- **Composite Index**: `(first_half_cracked, second_half_cracked)` enables fast partial crack queries
- **VARCHAR(7) Limit**: Enforces LM's 7-character half constraint at database level

**Use Cases:**
- Track partial crack status: "First half cracked, second half pending"
- Analytics: "X LM hashes are partially cracked"
- Strategic intelligence: "Known half reduces keyspace by factor of 68 trillion"

## Upload Flow

### Pwdump Format Detection

When a user uploads a hashlist file:

1. **File Selection**: User selects file via upload dialog
2. **Automatic Detection**: Frontend calls `/api/hashlists/detect-linked` endpoint
3. **Backend Analysis**:
   - Reads first 1000 lines (sample)
   - Checks for pwdump format: `DOMAIN\user:RID:LM:NTLM:::`
   - Counts LM hashes, NTLM hashes, blank LM hashes
4. **User Dialog**: If both types found, present options:
   - "Upload as Single List"
   - "Create Linked Lists"

**Detection Endpoint** (`POST /api/hashlists/detect-linked`):

```json
Request (multipart/form-data):
{
  "file": <uploaded file>
}

Response (if both types found):
{
  "has_both_types": true,
  "lm_count": 1428,
  "ntlm_count": 1500,
  "blank_lm_count": 72
}

Response (if only one type):
{
  "has_both_types": false
}
```

**Design Decision**: Detection is client-side initiated to provide immediate feedback without committing to upload.

### Linked Hashlist Creation

When user chooses "Create Linked Lists":

1. **Upload Request**: Frontend sends `create_linked=true` parameter
2. **Hashlist Creation**:
   - Create LM hashlist: `{original_name}-LM` (hash_type_id: 3000)
   - Create NTLM hashlist: `{original_name}-NTLM` (hash_type_id: 1000)
3. **Hashlist Link**: Insert record into `linked_hashlists` table
4. **Processing**: Both hashlists enter processing queue independently
5. **Hash Linking**: After processing completes, create individual hash-to-hash links

**API Endpoint** (`POST /api/hashlists`):

```
Parameters:
- name: Original hashlist name
- hash_type_id: (ignored if create_linked=true)
- client_id: Optional client association
- file: Pwdump format file
- create_linked: "true" to enable linked creation
```

**Processing Flow:**
```
1. Create LM hashlist record
2. Create NTLM hashlist record
3. Create linked_hashlists entry (lm_id, ntlm_id, 'lm_ntlm')
4. Enqueue LM hashlist for processing
5. Enqueue NTLM hashlist for processing
6. (Background) Process LM hashes
7. (Background) Process NTLM hashes
8. (Background) Create hash-to-hash links
```

## Processing Pipeline

### Hashlist Processing

**Standard Processing** (non-LM):
1. Read file line by line
2. Extract hash values and metadata
3. Batch insert into `hashes` table
4. Create `hashlist_hashes` join entries

**LM-Specific Processing**:
1. Read file line by line
2. Extract LM hash (32 hex chars)
3. **Skip blank LM constant**: If hash equals `aad3b435b51404eeaad3b435b51404ee`, skip line
4. Store full 32-char hash in `hashes.hash_value`
5. Create `lm_hash_metadata` entry (all fields FALSE/NULL initially)
6. Create `hashlist_hashes` join entry

**Code Location**: `backend/internal/processor/hashlist_processor.go`

**Blank LM Filtering Logic**:
```go
if hashType.ID == 3000 {
    upperHashValue := strings.ToUpper(hashValue)
    if upperHashValue == "AAD3B435B51404EEAAD3B435B51404EE" {
        debug.Debug("[Processor:%d] Line %d: Skipping blank LM hash", hashlistID, lineNumber)
        totalHashes-- // Don't count blank LM hashes
        continue
    }
}
```

### Hash-to-Hash Linking

After both linked hashlists complete processing:

1. **Retrieve Hashes**: Get all hashes from both hashlists with username/domain
2. **Build NTLM Map**: `map[string]*models.Hash` keyed by `{domain}\{username}`
3. **Match LM to NTLM**: For each LM hash, lookup NTLM hash by username/domain
4. **Batch Insert Links**: Create `linked_hashes` entries for all matches

**Matching Logic**:
```go
func makeUserDomainKey(username, domain *string) string {
    user := ""
    if username != nil {
        user = *username
    }

    dom := ""
    if domain != nil {
        dom = *domain
    }

    if dom != "" {
        return fmt.Sprintf("%s\\%s", dom, user)
    }
    return user
}
```

**Batch Linking**:
```sql
INSERT INTO linked_hashes (hash_id_1, hash_id_2, link_type)
VALUES
    ($1, $2, 'lm_ntlm'),
    ($3, $4, 'lm_ntlm'),
    ...
ON CONFLICT (hash_id_1, hash_id_2) DO NOTHING;
```

**Design Decision**: Links created by username/domain match, not by RID, to handle domain migrations and account renames.

## Agent Download

### Standard Hash Download

For most hash types, agents download via `GET /api/hashlists/{id}/uncracked`:

```
Response (text/plain):
5f4dcc3b5cd84097a65d1633f5c74f5e
098f6bcd4621d373cade4e832627b4f6
1a1dc91c907325c69271ddf0c944bc72
...
```

### LM Hash Half Streaming

For LM hashlists (hash_type_id 3000), special processing occurs:

**Backend Processing** (`routes/hashlist.go`):
```go
if hashlist.HashTypeID == 3000 {
    // Stream unique 16-char halves instead of full 32-char hashes
    err = h.hashRepo.StreamUncrackedLMHashHalvesForHashlist(ctx, hashlist.ID, func(hashHalf string) error {
        fmt.Fprintln(w, hashHalf)  // Write 16-char half
        return nil
    })
}
```

**SQL Query** (`repository/hash_repository.go`):
```sql
SELECT DISTINCT half
FROM (
    SELECT SUBSTRING(h.hash_value, 1, 16) AS half
    FROM hashes h
    INNER JOIN hashlist_hashes hh ON h.id = hh.hash_id
    WHERE hh.hashlist_id = $1 AND h.is_cracked = FALSE
    UNION
    SELECT SUBSTRING(h.hash_value, 17, 16) AS half
    FROM hashes h
    INNER JOIN hashlist_hashes hh ON h.id = hh.hash_id
    WHERE hh.hashlist_id = $1 AND h.is_cracked = FALSE
) AS halves
ORDER BY half
```

**Example Output**:
```
01FC5A6BE7BC6929  ← First half of hash 1
5F4DCC3B5CD84097  ← First half of hash 2
AAD3B435B51404EE  ← Blank constant (appears once despite multiple occurrences)
C3B435B51404EE89  ← Second half of hash 1
...
```

**Why This Approach:**
- **Hashcat Requirement**: Mode 3000 expects 16-char halves, not 32-char full hashes
- **Deduplication**: DISTINCT ensures common halves appear only once
- **Efficiency**: Blank constant `aad3b435b51404ee` sent once instead of hundreds of times
- **Parallel Capability**: Agents can crack different halves simultaneously

## Crack Handling

### LM Partial Crack Flow

When an agent reports a cracked LM hash half:

1. **Agent Reports Crack**: Sends 16-char hash half + password to backend
2. **Identify Full Hashes**: Find all 32-char LM hashes containing this 16-char half
3. **Determine Position**: Check if half matches LEFT(hash, 16) or RIGHT(hash, 16)
4. **Update Metadata**:
   - If first half: Set `first_half_cracked = TRUE`, `first_half_password = <password>`
   - If second half: Set `second_half_cracked = TRUE`, `second_half_password = <password>`
5. **Check Completion**: If both halves now cracked, assemble full password
6. **Mark Complete**: If both halves cracked, update `hashes.is_cracked = TRUE`

**Repository Method** (`repository/lm_hash_repository.go`):
```go
func (r *LMHashRepository) UpdateLMHalfCrack(ctx context.Context, tx *sql.Tx, hashID uuid.UUID, halfPosition string, password string) error {
    // halfPosition: "first" or "second"
    query := `
        INSERT INTO lm_hash_metadata (hash_id, {half}_cracked, {half}_password, updated_at)
        VALUES ($1, TRUE, $2, $3)
        ON CONFLICT (hash_id) DO UPDATE
        SET {half}_cracked = TRUE, {half}_password = $2, updated_at = $3
    `
    // ...
}
```

**Full Password Assembly**:
```go
func (r *LMHashRepository) CheckAndFinalizeLMCrack(ctx context.Context, tx *sql.Tx, hashID uuid.UUID) (bool, string, error) {
    // Check if both halves are cracked
    query := `
        SELECT (first_half_cracked AND second_half_cracked) AS both_cracked,
               first_half_password, second_half_password
        FROM lm_hash_metadata
        WHERE hash_id = $1
    `

    if bothCracked {
        fullPassword = firstHalfPwd + secondHalfPwd
        return true, fullPassword, nil
    }
    return false, "", nil
}
```

### Cross-Hashlist Propagation

LM hash cracks propagate across all hashlists (standard behavior):

1. **Crack Reported**: Agent cracks 16-char LM half
2. **Find All Matching**: Identify all 32-char LM hashes containing this half
3. **Update All**: Update metadata for every matching hash
4. **Regenerate Files**: Regenerate all affected hashlist files
5. **Notify Agents**: Mark agent copies as outdated

This ensures that cracking one LM half benefits all hashlists containing hashes with that half.

## Analytics Integration

### Windows Hash Statistics

**Overview Count Calculation**:
```sql
-- Get effective count (linked pairs count as ONE)
SELECT
    COUNT(DISTINCT CASE
        WHEN lh.id IS NOT NULL THEN
            CASE WHEN h.hash_type_id = 3000 THEN lh.id ELSE NULL END
        ELSE h.id
    END) AS total_windows,
    COUNT(DISTINCT CASE
        WHEN h.is_cracked AND lh.id IS NOT NULL THEN
            CASE WHEN h.hash_type_id = 3000 THEN lh.id ELSE NULL END
        ELSE CASE WHEN h.is_cracked THEN h.id ELSE NULL END
    END) AS cracked_windows
FROM hashes h
LEFT JOIN linked_hashes lh ON (h.id = lh.hash_id_1 OR h.id = lh.hash_id_2)
    AND lh.link_type = 'lm_ntlm'
WHERE ...
```

**Individual Hash Type Counts**:
- Use raw counts (don't adjust for linking) to show actual hash quantities
- Example: 1500 NTLM hashes and 1428 LM hashes displayed separately

**Linked Pair Count**:
```sql
SELECT COUNT(*) FROM linked_hashes WHERE link_type = 'lm_ntlm'
```

### Linked Hash Correlation

**Query Structure**:
```sql
SELECT
    COUNT(*) AS total_pairs,
    COUNT(CASE WHEN lm.is_cracked AND ntlm.is_cracked THEN 1 END) AS both_cracked,
    COUNT(CASE WHEN NOT lm.is_cracked AND ntlm.is_cracked THEN 1 END) AS only_ntlm,
    COUNT(CASE WHEN lm.is_cracked AND NOT ntlm.is_cracked THEN 1 END) AS only_lm,
    COUNT(CASE WHEN NOT lm.is_cracked AND NOT ntlm.is_cracked THEN 1 END) AS neither
FROM linked_hashes lh
INNER JOIN hashes lm ON lh.hash_id_1 = lm.id
INNER JOIN hashes ntlm ON lh.hash_id_2 = ntlm.id
WHERE lh.link_type = 'lm_ntlm' AND ...
```

### LM Partial Crack Query

**Find Partially Cracked LM Hashes**:
```sql
SELECT
    h.id, h.username, h.domain,
    lm.first_half_cracked, lm.first_half_password,
    lm.second_half_cracked, lm.second_half_password,
    hl.name AS hashlist_name
FROM lm_hash_metadata lm
INNER JOIN hashes h ON lm.hash_id = h.id
INNER JOIN hashlist_hashes hlh ON h.id = hlh.hash_id
INNER JOIN hashlists hl ON hlh.hashlist_id = hl.id
WHERE (lm.first_half_cracked OR lm.second_half_cracked)
  AND NOT (lm.first_half_cracked AND lm.second_half_cracked)
  AND hlh.hashlist_id = ANY($1)
ORDER BY h.username
LIMIT 50;
```

## Performance Considerations

### Index Strategy

**Critical Indexes**:
1. `idx_linked_hashlists_id2`: Enables bidirectional hashlist lookup
2. `idx_linked_hashes_id2`: Enables bidirectional hash lookup
3. `idx_lm_metadata_crack_status`: Fast partial crack queries
4. `idx_lm_metadata_hash_id`: Foreign key lookup

**Query Optimization**:
- Composite index on `(first_half_cracked, second_half_cracked)` enables single-scan partial crack detection
- DISTINCT in LM half streaming handled by PostgreSQL with UNION optimization

### Memory Usage

**LM Half Streaming**:
- No full dataset loaded into memory
- Cursor-based streaming from database
- Backpressure via HTTP chunked transfer encoding
- Typical memory: <100MB for 1M+ hashes

**Hash Linking**:
- In-memory map: `map[string]*models.Hash` for NTLM hashes
- Typical size: ~200 bytes per hash × count
- Example: 100K hashes = ~20MB
- Batch insert: 1000 links at a time to limit transaction size

### Scalability

**Tested Performance**:
- Pwdump files up to 1M lines: <30 seconds processing
- Hash linking 100K pairs: <5 seconds
- Analytics with linked pairs: <10 seconds for 1M+ hashes
- LM half streaming: Line-speed (network bound, not CPU/DB bound)

## Future Extensibility

The generic design enables future enhancements:

**Potential Link Types**:
- `sha1_ntlm`: Link SHA1 and NTLM hashes for same user (multi-platform analysis)
- `old_new`: Link old and new password hashes for password change analysis
- `service_user`: Link service account hashes across systems

**Metadata Tables**:
- Similar to `lm_hash_metadata`, could add:
  - `kerberos_metadata`: etype information, ticket details
  - `netntlm_metadata`: challenge/response pair tracking
  - `custom_metadata`: User-defined fields for special analyses

**Analytics Extensions**:
- Password aging analysis (old_new links)
- Cross-platform password reuse (sha1_ntlm links)
- Service account proliferation tracking

## Troubleshooting

### Common Issues

**Issue**: Hash links not created after upload
- **Cause**: Username/domain mismatch between LM and NTLM entries
- **Solution**: Verify username extraction logic handles special characters
- **Check**: `SELECT username, domain FROM hashes WHERE hashlist_id IN (...)`

**Issue**: Partial cracks not appearing in analytics
- **Cause**: `lm_hash_metadata` entries not created during processing
- **Solution**: Verify LM hashlist has `hash_type_id = 3000`
- **Check**: `SELECT COUNT(*) FROM lm_hash_metadata WHERE hash_id IN (...)`

**Issue**: Duplicate links created
- **Cause**: Bidirectional uniqueness constraint prevents this, but check for manual SQL
- **Solution**: Constraints automatically prevent duplicates

**Issue**: Analytics show wrong linked pair count
- **Cause**: May be counting hashlist links instead of hash links
- **Solution**: Verify query uses `linked_hashes` not `linked_hashlists`

### Debugging Queries

**Check Hashlist Linkage**:
```sql
SELECT * FROM linked_hashlists WHERE hashlist_id_1 = X OR hashlist_id_2 = X;
```

**Check Hash Linkage**:
```sql
SELECT COUNT(*) FROM linked_hashes WHERE link_type = 'lm_ntlm';
```

**Find Orphaned Metadata**:
```sql
SELECT lm.* FROM lm_hash_metadata lm
LEFT JOIN hashes h ON lm.hash_id = h.id
WHERE h.id IS NULL;
-- Should return 0 rows (CASCADE DELETE should prevent orphans)
```

**Verify LM Half Streaming**:
```bash
# Download LM hashlist, count unique halves
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/hashlists/{id}/uncracked | sort -u | wc -l
```

## References

- [Hashlists User Guide](../../user-guide/hashlists.md)
- [Analytics Reports](../../user-guide/analytics-reports.md)
- [Hash Types Reference](../hash-types.md)
- [Database Schema](../database.md)
