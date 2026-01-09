# Association Attack Architecture

This document describes the technical implementation of association attack mode (-a 9) in KrakenHashes.

## Overview

Association attacks require a 1:1 mapping between hashes and password candidates. Unlike other attack modes where hashlists are deduplicated and candidates are tested against all hashes, mode 9 tests each candidate against only its corresponding hash by line number.

## Key Requirements

### Hash Order Preservation

**Challenge:** KrakenHashes normally processes uploaded hashlists to:
- Remove duplicates
- Extract usernames
- Normalize format

This changes the order of hashes, breaking the 1:1 correspondence needed for association attacks.

**Solution:** When a hashlist is uploaded, the system now:
1. Saves a copy of the original uploaded file (`original_file_path`)
2. For mode 9 jobs, agents download the original file instead of the processed version

### Line Count Validation

The association wordlist line count must exactly match the hashlist hash count. Validation occurs:
- At upload time (immediate feedback)
- At job creation time (prevents stale data issues)

### Mixed Work Factor Detection

Hash types with variable computational costs (bcrypt, scrypt, etc.) are detected during processing:
- If hashes have different cost parameters, `has_mixed_work_factors` flag is set
- Association attacks are blocked for these hashlists
- UI displays a warning explaining why

## Data Flow

```
User uploads hashlist
    │
    ▼
Process hashlist ──► Save original file path
    │                      │
    ▼                      ▼
User uploads association wordlist
    │
    ▼
Validate line count matches hash count
    │
    ▼
User creates mode 9 job
    │
    ▼
Agent receives task assignment
    │
    ▼
Agent downloads ORIGINAL hashlist (not processed)
    │
    ▼
Agent downloads association wordlist
    │
    ▼
Agent executes: hashcat -a 9 -m {mode} original.hash assoc.txt [rules]
```

## File Storage

```
/data/krakenhashes/
├── hashlists/
│   ├── {id}.hash          # Processed hashlist (normal attacks)
│   └── original/
│       └── {id}_{filename} # Original uploaded file (mode 9)
└── wordlists/
    └── association/
        └── {hashlist_id}_{filename}  # Association wordlists
```

## Keyspace Calculation

Mode 9 does not support hashcat's `--keyspace` flag. Keyspace is estimated as:

```
keyspace = association_wordlist_lines × rule_count
```

If `rule_count = 0`, keyspace equals the wordlist line count.

!!! note "Forced Benchmark"
    Because keyspace cannot be calculated accurately, association attack jobs always trigger a forced benchmark to obtain actual speed from hashcat's `progress[1]` value.

## Rule Splitting Support

Association attacks support rule splitting for large rule files:
- Same threshold-based logic as straight attacks (mode 0)
- Decision deferred until benchmark provides actual speed
- Each task processes full wordlist with a subset of rules

See [Rule Splitting](rule-splitting.md) for details on how rule splitting works.

## Database Schema

### New Tables

**association_wordlists**
```sql
CREATE TABLE association_wordlists (
    id UUID PRIMARY KEY,
    hashlist_id INTEGER NOT NULL REFERENCES hashlists(id),
    file_name VARCHAR(255) NOT NULL,
    file_path VARCHAR(500) NOT NULL,
    line_count BIGINT NOT NULL,
    file_size BIGINT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

**agent_association_files**
```sql
CREATE TABLE agent_association_files (
    id SERIAL PRIMARY KEY,
    agent_id INTEGER NOT NULL REFERENCES agents(id),
    association_wordlist_id UUID NOT NULL REFERENCES association_wordlists(id),
    downloaded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    file_path VARCHAR(500) NOT NULL
);
```

### Modified Tables

**hashlists**
- `original_file_path VARCHAR(500)` - Path to original uploaded file
- `has_mixed_work_factors BOOLEAN DEFAULT FALSE` - Flag for mixed cost parameters

**job_executions**
- `association_wordlist_id UUID REFERENCES association_wordlists(id)` - Selected association wordlist

## Agent File Download

When an agent receives a mode 9 task:

1. **Hashlist Download**: Agent calls the hashlist download endpoint with `?mode=9` query parameter
   - Backend returns original file content instead of processed hashlist
   - Content-Type header indicates original filename

2. **Association Wordlist Download**: Agent downloads from dedicated endpoint
   - Path: `/api/agent/wordlists/association/{hashlist_id}/{filename}`
   - Stored in `data/wordlists/association/` directory

3. **File Cleanup**: After job completion, downloaded association files are tracked for cleanup
   - `agent_association_files` table tracks what each agent has
   - Cleanup triggered when hashlist or association wordlist is deleted

## Hashcat Command Construction

The agent builds the hashcat command for mode 9:

```bash
hashcat -a 9 -m {hash_type} {original_hashlist} {association_wordlist} [rules...]
```

Key differences from other modes:
- Uses `-a 9` instead of `-a 0` (straight)
- Uses original hashlist file (preserves line order)
- Association wordlist is the primary input (not a standard wordlist)
- No `-skip` or `-limit` parameters (entire keyspace processed)

## Error Handling

### Line Count Mismatch

If line count doesn't match at upload time:
```json
{
  "error": "Association wordlist line count (1500) does not match hashlist hash count (1000)"
}
```

### Mixed Work Factors

If hashlist has mixed work factors:
```json
{
  "error": "Association attacks not available for hashlists with mixed work factors"
}
```

### Job Creation Validation

The system validates at job creation time:
1. Association wordlist exists and belongs to the selected hashlist
2. Line count still matches (hashlist may have changed)
3. Hashlist doesn't have mixed work factors

## Related Documentation

- [Jobs & Workflows - Association Attacks](../../user-guide/jobs-workflows.md#association-attacks-v140)
- [Hashlists - Association Wordlists](../../user-guide/hashlists.md#association-wordlists-v140)
- [Rule Splitting](rule-splitting.md)
- [Glossary - Association Attack](../glossary.md#association-attack)
