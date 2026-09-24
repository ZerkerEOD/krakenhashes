# Data Retention

KrakenHashes includes a comprehensive data retention system that automatically and securely purges old hashlists and associated data based on configurable retention policies.

## Overview

The retention system ensures compliance with data retention policies by automatically removing expired data from both the database and filesystem. It includes secure deletion mechanisms to prevent data recovery.

!!! warning "Potfile Exclusion"
    The retention system does **NOT** affect the potfile (`/var/lib/krakenhashes/wordlists/custom/potfile.txt`), which contains plaintext passwords from all cracked hashes. The potfile is considered a permanent system resource and must be manually managed. If you need to remove passwords associated with deleted hashlists from the potfile, you must do so manually or implement a separate cleanup process.

## Retention Policy Configuration

### Default Retention Policy

-   **Purpose:** Sets the default number of months after which hashlists are automatically deleted
-   **Scope:** Applies to all hashlists unless overridden by client-specific settings
-   **Special Values:**
    -   `0` = Keep forever (no automatic deletion)
    -   `NULL` = Use system default
    -   Any positive integer = Number of months to retain data

### Client-Specific Retention

Each client can have their own retention period that overrides the system default:
-   Set during client creation or via the client management interface
-   Takes precedence over the default retention setting
-   Applies to all hashlists associated with that client

## Automatic Purge Process

### Scheduling

The retention purge runs automatically:
-   **On Startup:** 15 seconds after backend initialization
-   **Daily:** At midnight (server time)
-   Processes hashlists in batches of 1000 for scalability

### What Gets Deleted

When a hashlist expires based on retention policy:

1. **Database Records:**
   - Hashlist record from `hashlists` table
   - All associations in `hashlist_hashes` table
   - Orphaned hashes (not associated with any other hashlist)
   - Related records via CASCADE deletion:
     - `agent_hashlists` entries
     - `job_executions` entries

2. **Filesystem:**
   - Hashlist file from `/var/lib/krakenhashes/hashlists/`
   - File is securely overwritten with random data before deletion
   - Prevents recovery via filesystem forensics

3. **PostgreSQL Maintenance:**
   - `VACUUM ANALYZE` runs on affected tables
   - Reclaims storage space
   - Removes dead tuples to prevent WAL recovery

### What Does NOT Get Deleted

!!! important "Retained Data"
    The following data is **NOT** removed by the retention system:

    - **Potfile**: Contains plaintext passwords for ALL cracked hashes across all hashlists
    - **Clients**: Client records remain even when all their hashlists are deleted
    - **Users**: User accounts are never deleted by retention policies
    - **Wordlists**: Permanent resources not affected by retention
    - **Rules**: Permanent resources not affected by retention
    - **Binaries**: System binaries remain unchanged

### Security Features

-   **Secure File Deletion:** Files are overwritten with random data before removal
-   **Transaction Safety:** All database operations occur within a single transaction
-   **Audit Logging:** Complete audit trail of all deletion operations
-   **VACUUM Operations:** Prevents data recovery from PostgreSQL internals

## Agent-Side Cleanup

Agents automatically clean up old files to prevent storage accumulation:

### Retention Policy
-   **3-day retention** for temporary files
-   Runs every 6 hours automatically
-   Initial cleanup 1 minute after agent startup

### Files Cleaned
-   **Hashlists:** Removed after 3 days of inactivity
-   **Rule Chunks:** Temporary rule segments deleted after 3 days
-   **Chunk ID Files:** Removed when associated chunks are deleted
-   **Preserved Files:** Base rules, wordlists, and binaries are never auto-deleted

## Configuration via API

### View Current Settings

**`GET /api/admin/settings/retention`**

Response:
```json
{
  "data": {
    "key": "default_data_retention_months",
    "value": "36",
    "description": "Default data retention period in months for clients without a specific setting. 0 means keep forever.",
    "updated_at": "2026-09-17T10:35:00.391Z"
  }
}
```

This endpoint returns the default retention setting only. Purge timestamps are read
directly from `client_settings` — see [Verifying a purge ran](#verifying-a-purge-ran) below.

### Update Retention Settings

**`PUT /api/admin/settings/retention`**

Request:
```json
{
  "default_retention_months": 24
}
```

Requires administrator privileges.

## Important Considerations

### Data Preservation
-   Clients themselves are never deleted by retention policies
-   Hashes shared across multiple hashlists are only deleted when orphaned
-   User accounts and system settings are unaffected
-   **Potfile retains ALL plaintext passwords permanently** - must be manually managed

### Compliance
-   Retention policies help meet data protection regulations
-   Audit logs provide evidence of proper data disposal
-   Secure deletion prevents unauthorized data recovery
-   **NOTE**: Potfile retention may conflict with data protection requirements - consider implementing separate potfile cleanup procedures

### Performance Impact
-   VACUUM operations may temporarily impact database performance
-   Purge operations are logged but don't block normal operations
-   Large deletions are handled in batches to minimize impact

### Potfile Management

!!! danger "Security Critical"
    The potfile (`/var/lib/krakenhashes/wordlists/custom/potfile.txt`) contains plaintext passwords from ALL cracked hashes and is NOT managed by the retention system. This has important implications:

    1. **Privacy Risk**: Passwords from deleted hashlists remain in the potfile
    2. **Compliance Issue**: May violate data protection regulations requiring complete deletion
    3. **Manual Management Required**: You must implement separate procedures to clean the potfile

    **Recommended Actions:**
    - Implement a separate potfile cleanup script that removes entries for deleted hashlists
    - Consider rotating or archiving the potfile periodically
    - Document potfile management procedures for compliance audits
    - Restrict access to the potfile to authorized personnel only

## Monitoring

Check retention activity in the backend logs:
```bash
docker exec krakenhashes-app tail -f /var/log/krakenhashes/backend/backend.log | grep -i purge
```

### Verifying a purge ran

The two retention purges record their own timestamps:

```sql
SELECT key, value FROM client_settings
WHERE key IN ('last_purge_run', 'last_analytics_purge_run');
```

- `last_purge_run` — the hashlist data retention purge
- `last_analytics_purge_run` — the analytics report retention purge

A `NULL` value means that purge has not completed since the keys were introduced. Both
were previously written to the same key, so on older deployments whichever purge ran last
overwrote the other. 