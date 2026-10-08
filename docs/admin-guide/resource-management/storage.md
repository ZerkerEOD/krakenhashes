# Storage Architecture

## Overview

KrakenHashes implements a centralized file storage system with intelligent deduplication, hash verification, and performance optimizations. This guide covers the storage architecture, capacity planning, and maintenance procedures.

By default the server keeps all wordlists and rules on local disk. For large deployments it can instead keep them on a **network share** (see [Network Share Storage](#network-share-storage)), and individual agents can be assigned a **storage tier** that controls whether they cache every list, download per job, or read directly off a mounted share (see [Per-Agent Storage Tiers](#per-agent-storage-tiers)).

## Storage Directory Structure

The system organizes files into a hierarchical structure under the configured data directory (default: `/var/lib/krakenhashes` in Docker, `~/.krakenhashes-data` locally):

```
/var/lib/krakenhashes/           # Root data directory (KH_DATA_DIR)
├── binaries/                    # Hashcat/John binaries
│   ├── hashcat_7.6.0_linux64.tar.gz
│   └── john_1.9.0_linux64.tar.gz
├── wordlists/                   # Wordlist files by category
│   ├── general/                 # Common wordlists
│   ├── specialized/             # Domain-specific lists
│   ├── targeted/                # Custom targeted lists
│   └── custom/                  # User-uploaded lists
├── rules/                       # Rule files by type
│   ├── hashcat/                 # Hashcat-compatible rules
│   ├── john/                    # John-compatible rules
│   └── custom/                  # Custom rule sets
├── hashlists/                   # Processed hashlist files
│   ├── 1.hash                   # Uncracked hashes for job ID 1
│   └── 2.hash                   # Uncracked hashes for job ID 2
├── hashlist_uploads/            # Temporary upload storage
│   └── <user-id>/              # User-specific upload directories
└── local/                       # Extracted binaries (server-side)
```

### Directory Permissions

All directories are created with mode `0750` (rwxr-x---) to ensure:
- Owner has full access
- Group has read and execute access
- Others have no access

## Network Share Storage

Wordlists and rules can grow to hundreds of gigabytes. Instead of giving every server that much local disk, KrakenHashes can keep the master copy of `wordlists/` and `rules/` on a network share (NFS or SMB/CIFS). Everything else — hashlists, binaries, charsets, uploads, and the global potfile master — always stays on local disk.

Which backend is active is **database state** (`storage_backend` = `local` or `share`), not an environment variable, so a restart simply reads the current backend. You switch between them with an in-app, resumable migration — there is no manual file copying and no downtime window beyond the migration's own lock phase.

### How it fits together

- **The server consumes the share; it does not mount or manage it.** You mount the share on the host (or in the container) via `docker-compose`, and point the backend at that path with `KH_SHARE_DIR` (host side `KH_SHARE_DIR_HOST`). KrakenHashes only reads and writes files under that path.
- **No credentials are stored by KrakenHashes.** The server's share credentials live in your compose/mount configuration. Agent-side share credentials are entered by the operator when they mount the share (see [agent setup](#on-prem-agent-setup)). The application never holds or distributes share secrets.
- **The HTTP file API is the relay.** Agents on the `full_cache` and `on_demand` tiers keep downloading files over HTTPS exactly as before — the server just happens to be reading them off the share. Only `network_direct` agents read the share directly.
- **The potfile is always served over HTTP**, never read directly off the share, because it is mutable.

### The Storage settings panel

Configure everything from **Admin → System Settings → Storage**. The panel has two parts:

1. **Server storage mount (read-only status + actions).** Shows the server's mounted share path (`KH_SHARE_DIR`), its health, the current backend (`local`/`share`), and any in-progress migration. From here you **Enable** the network share, **Validate** the mount (stats the path, confirms readable `wordlists/`+`rules/`, runs a scoped write/throughput probe, reports free space), and start a **Migration** in either direction.
2. **On-prem agent mount command (optional).** Generates a templated `mount` command (protocol, VPN host, share name, options) that operators paste on a `network_direct` agent host. Username/password are placeholders you fill in; KrakenHashes does not store them.

!!! note "Cloud agents use the relay, not a mount"
    Cloud GPU instances run their VPN in userspace / SOCKS-only mode and cannot perform a kernel mount, so they **cannot** use `network_direct`. They consume wordlists/rules over the HTTP relay (as `full_cache`/`on_demand`) across the VPN. `network_direct` is an **on-prem-only** tier.

### Migrating to the share (and back)

The migration runs inside the live server process — no restart — and is driven by the `storage_backend` / migration state in the database. It is resumable and idempotent (interrupt it and it picks up where it left off), and MD5-validates every file before flipping the backend. The same engine runs in reverse for rollback/decommission.

| Phase | What happens | System availability |
|-------|--------------|---------------------|
| **Drain** | Wait for in-flight tasks to finish and agents to reconnect. Starts at a floor of `KH_MIGRATION_DRAIN_FLOOR_SECONDS` (default 720s / 12 min), refined by the longest-running task's ETA. | Fully operational; no *new* dispatch. |
| **Lock & migrate** | Wordlist/rule writes are frozen and a maintenance state is shown; the worker copies files to the target, skipping any already copied (verified by MD5). | Reads continue; uploads and new dispatch paused. |
| **Validate** | Every migrated file is re-verified by MD5 against the source. | Still locked. |
| **Flip & resume** | `storage_backend` flips to the target; writes, uploads and dispatch resume. | Back to normal, serving from the new backend. |

On **any failure the backend is left unchanged** (writes/dispatch resume on the original storage), so a failed migration is safe. The source copies are **not deleted** after a successful migration — reclaim that space manually once you've confirmed the new backend is serving correctly.

!!! warning "A share outage is treated as transient, not as missing files"
    If the mounted share becomes unreachable while the backend is `share`, KrakenHashes raises a degraded-storage condition and pauses share-backed dispatch rather than marking files missing or failing jobs. Restore the mount and work resumes.

## Per-Agent Storage Tiers

Each agent has a **storage tier** that controls how it obtains wordlists/rules for a job. Set it per agent in **Admin → System Settings → Storage** (or on the agent detail page); it can also be seeded at the agent via `--storage-tier` / `KH_STORAGE_TIER` (see [agent configuration](../../agent-guide/configuration.md#storage-tier-network-share-feature)), but the admin UI stays authoritative.

| Tier | Behavior | Use when |
|------|----------|----------|
| `full_cache` *(default)* | Downloads over HTTP and keeps every list locally. Identical to pre-feature behavior. | The agent has ample local disk and runs many jobs. |
| `on_demand` | Downloads lists per task over HTTP, then evicts least-recently-used wordlist/rule files once free space drops below `KH_AGENT_ONDEMAND_TARGET_FREE_GB` (default 20 GiB). Files in use by a running task are never evicted. | Limited local disk; still over the relay. |
| `network_direct` | Reads immutable wordlists/rules **directly off a mounted share** — no download. On-prem only. | On-prem agent on the same share as the server. |

Notes for `network_direct`:

- The agent only *observes* an already-mounted path — it never mounts anything itself.
- The agent **refuses to start if its own binary lives on a network mount** (only the data path may be a network share).
- The potfile and client-specific wordlists still come over HTTP even on this tier.
- A missing-on-mount file makes the agent cleanly reject the task so the scheduler re-dispatches it elsewhere, rather than failing the job.

### On-prem agent setup

On a `network_direct` agent host: mount the share read-only (use the templated command from the Storage panel, filling in your own credentials), then point the agent at that path:

```bash
# Example: share already mounted read-only at /mnt/kh-agent-share
./krakenhashes-agent \
  --host your-server:31337 \
  --claim YOUR_CLAIM_CODE \
  --storage-tier network_direct \
  --network-share-mount-path /mnt/kh-agent-share
```

See [Agent Configuration → Storage Tier](../../agent-guide/configuration.md#storage-tier-network-share-feature) for the equivalent `.env` keys and the launcher/service form.

### Latency & remote shares

`network_direct` is designed for a **low-latency, LAN-adjacent share** — an agent sitting on the same network as the server's share, where reading a multi-gigabyte wordlist straight off the mount is as fast as (or faster than) downloading it. It is **not** a good fit for a share in another building, state, or country: every job reads the whole list over that link before hashcat starts, and high latency turns a cold read into minutes of stalled startup.

**Put remote or high-latency shares on the download tiers instead.** `full_cache` (download once, keep) or `on_demand` (download per job, evict under disk pressure) pull wordlists/rules over the HTTP relay, which retries and tolerates a slow link far better than a kernel mount — the same reason cloud agents always use the relay. The server still reads the share; only the agent changes.

The system hardens `network_direct` against a slow-but-working share so it degrades instead of failing:

- **Mount commands fail fast.** The generated mount command defaults to `soft` (and, for NFS, `timeo`/`retrans`) so a hung or dropped share returns an I/O error quickly instead of freezing the mount on the kernel's `hard` default. You can override any option in the Storage panel. The trade-off is that a transient blip surfaces as an error (and a rerouted task) rather than a silent wait — the right choice here, because it pairs with the next point.
- **Fail-closed share health.** A `network_direct` agent runs a bounded, background liveness check on its mount and reports `share_ready`. If the mount is slow or gone, the agent stays connected (its heartbeat is never blocked) but reports not-ready, and the scheduler reroutes work to other agents until the share recovers.
- **Wider benchmark/startup windows.** A `network_direct` agent's benchmark reads the wordlist off the mount before hashcat emits a result; its benchmark speed-test window and its task startup grace are widened (×4) so a large cold read isn't mistaken for a stall and evicted. A benchmark read that times out on the share is treated as a non-counting transient — it never marches the job toward a failure or blocklists the agent — so a slow share costs you time, not a failed job. If a share keeps timing out even with the wider window, that is the signal to move it to `full_cache`/`on_demand`.

## File Deduplication and Hash Verification

### MD5-Based Deduplication

KrakenHashes uses MD5 hashes for file deduplication across all resource types:

1. **Upload Processing**
   - Calculate MD5 hash of uploaded file
   - Check database for existing file with same hash
   - If exists, reference existing file instead of storing duplicate
   - If new, store file and record hash in database

2. **Verification States**
   - `pending` - File uploaded but not yet verified
   - `verified` - File hash matches database record
   - `failed` - Hash mismatch or file corrupted
   - `deleted` - File removed from storage

3. **Database Schema**
   ```sql
   -- Example: Wordlists table
   CREATE TABLE wordlists (
       id SERIAL PRIMARY KEY,
       name VARCHAR(255) NOT NULL,
       file_name VARCHAR(255) NOT NULL,
       md5_hash VARCHAR(32) NOT NULL,
       file_size BIGINT NOT NULL,
       verification_status VARCHAR(20) DEFAULT 'pending',
       UNIQUE(md5_hash)  -- Ensures deduplication
   );
   ```

### File Synchronization

The agent file sync system ensures consistency across distributed agents:

1. **Sync Protocol**
   - Agent reports current files with MD5 hashes
   - Server compares against master file list
   - Server sends list of files to download
   - Agent downloads only missing/changed files

2. **Hash Verification**
   - Files are verified after download
   - Failed verifications trigger re-download
   - Corrupted files are automatically replaced

## Storage Requirements and Capacity Planning

### Estimating Storage Needs

Calculate storage requirements based on:

1. **Wordlists**
   - Common wordlists: 10-50 GB
   - Specialized lists: 50-200 GB
   - Large collections: 500+ GB

2. **Rules**
   - Basic rule sets: 1-10 MB
   - Comprehensive sets: 100-500 MB

3. **Hashlists**
   - Original uploads: Variable
   - Processed files: ~32 bytes per hash
   - Example: 1M hashes ≈ 32 MB

4. **Binaries**
   - Hashcat package: ~100 MB
   - John package: ~50 MB
   - Multiple versions: Plan for 3-5 versions

### Recommended Minimums

| Deployment Size | Storage | Rationale |
|----------------|---------|-----------|
| Development | 50 GB | Basic wordlists and testing |
| Small Team | 200 GB | Standard wordlists + custom data |
| Enterprise | 1 TB+ | Comprehensive wordlists + history |

### Growth Considerations

- **Hashlist accumulation**: ~10-20% monthly growth typical
- **Wordlist expansion**: New lists added periodically
- **Binary versions**: Keep 3-5 recent versions
- **Backup overhead**: 2x storage for full backups

## Backup Considerations

### What to Backup

1. **Critical Data**
   - PostgreSQL database (contains all metadata)
   - Custom wordlists and rules
   - Configuration files (`/etc/krakenhashes`)

2. **Recoverable Data**
   - Standard wordlists (can be re-downloaded)
   - Binaries (can be re-downloaded)
   - Processed hashlists (can be regenerated)

### Backup Strategy

```bash
#!/bin/bash
# Example backup script

# Backup database
pg_dump -h postgres -U krakenhashes krakenhashes > backup/db_$(date +%Y%m%d).sql

# Backup custom data
rsync -av /var/lib/krakenhashes/wordlists/custom/ backup/wordlists/
rsync -av /var/lib/krakenhashes/rules/custom/ backup/rules/
rsync -av /etc/krakenhashes/ backup/config/

# Backup file metadata
docker-compose exec backend \
  psql -c "COPY (SELECT * FROM wordlists) TO STDOUT CSV" > backup/wordlists_meta.csv
```

### Restore Procedures

1. **Database Restore**
   ```bash
   psql -h postgres -U krakenhashes krakenhashes < backup/db_20240115.sql
   ```

2. **File Restore**
   ```bash
   rsync -av backup/wordlists/ /var/lib/krakenhashes/wordlists/custom/
   rsync -av backup/rules/ /var/lib/krakenhashes/rules/custom/
   ```

3. **Verify Integrity**
   - Run file verification for all restored files
   - Check MD5 hashes against database records

## Performance Optimization

### File System Considerations

1. **File System Choice**
   - ext4: Good general performance
   - XFS: Better for large files
   - ZFS: Built-in deduplication and compression

2. **Mount Options**
   ```bash
   # Example /etc/fstab entry with optimizations
   /dev/sdb1 /var/lib/krakenhashes ext4 defaults,noatime,nodiratime 0 2
   ```

3. **Storage Layout**
   - Use separate volumes for different data types
   - Consider SSD for hashlists (frequent reads)
   - HDDs acceptable for wordlists (sequential reads)

### Caching Strategy

1. **Application-Level Caching**
   - Recently used wordlists kept in memory
   - Hash type definitions cached
   - File metadata cached for 15 minutes

2. **File System Caching**
   - Linux page cache handles frequently accessed files
   - Monitor with `free -h` and adjust `vm.vfs_cache_pressure`

### I/O Optimization

```bash
# Tune kernel parameters for better I/O
echo 'vm.dirty_ratio = 5' >> /etc/sysctl.conf
echo 'vm.dirty_background_ratio = 2' >> /etc/sysctl.conf
echo 'vm.vfs_cache_pressure = 50' >> /etc/sysctl.conf
sysctl -p
```

## Docker Volume Management

### Volume Configuration

Docker Compose creates named volumes for persistent storage:

```yaml
volumes:
  krakenhashes_data:        # Main data directory
    name: krakenhashes_app_data
  postgres_data:            # Database storage
    name: krakenhashes_postgres_data
```

### Volume Operations

1. **Inspect Volumes**
   ```bash
   docker volume inspect krakenhashes_app_data
   docker volume ls
   ```

2. **Backup Volumes**
   ```bash
   # Backup data volume
   docker run --rm -v krakenhashes_app_data:/data \
     -v $(pwd)/backup:/backup \
     alpine tar czf /backup/data_backup.tar.gz -C /data .
   ```

3. **Restore Volumes**
   ```bash
   # Restore data volume
   docker run --rm -v krakenhashes_app_data:/data \
     -v $(pwd)/backup:/backup \
     alpine tar xzf /backup/data_backup.tar.gz -C /data
   ```

### Storage Driver Optimization

For production deployments:

```json
{
  "storage-driver": "overlay2",
  "storage-opts": [
    "overlay2.override_kernel_check=true"
  ]
}
```

## File Cleanup and Maintenance

### Automated Cleanup

The system includes automated cleanup for:

1. **Temporary Upload Files**
   - Deleted after successful processing
   - Orphaned files cleaned after 24 hours

2. **Old Hashlist Files**
   - Configurable retention period
   - Default: Keep for job lifetime + 30 days

### Manual Cleanup Procedures

1. **Remove Orphaned Files**
   ```bash
   # Find files not referenced in database
   docker-compose exec backend bash
   cd /var/lib/krakenhashes
   
   # Check for orphaned wordlists
   find wordlists -type f -name "*.txt" | while read f; do
     hash=$(md5sum "$f" | cut -d' ' -f1)
     # Query database for hash
   done
   ```

2. **Clean Old Hashlists**
   ```sql
   -- Remove hashlists older than 90 days with no active jobs
   DELETE FROM hashlists 
   WHERE updated_at < NOW() - INTERVAL '90 days'
   AND id NOT IN (
     SELECT DISTINCT hashlist_id 
     FROM job_executions 
     WHERE status IN ('pending', 'running')
   );
   ```

3. **Vacuum Database**
   ```bash
   docker-compose exec postgres \
     psql -U krakenhashes -c "VACUUM ANALYZE;"
   ```

### Storage Monitoring

1. **Disk Usage Monitoring**
   ```bash
   # Monitor storage usage
   df -h /var/lib/krakenhashes
   du -sh /var/lib/krakenhashes/*
   
   # Set up alerts
   echo '0 * * * * root df -h | grep krakenhashes | \
     awk '\''$5+0 > 80 {print "Storage warning: " $0}'\''' \
     >> /etc/crontab
   ```

2. **File Count Monitoring**
   ```sql
   -- Monitor file counts
   SELECT 
     'wordlists' as type, COUNT(*) as count,
     SUM(file_size)/1024/1024/1024 as size_gb
   FROM wordlists
   WHERE verification_status = 'verified'
   UNION ALL
   SELECT 'rules', COUNT(*), SUM(file_size)/1024/1024/1024
   FROM rules
   WHERE verification_status = 'verified';
   ```

### Best Practices

1. **Regular Maintenance Schedule**
   - Weekly: Check disk usage and clean temp files
   - Monthly: Verify file integrity and clean old hashlists
   - Quarterly: Full backup and storage audit

2. **Monitoring Alerts**
   - Set up alerts for >80% disk usage
   - Monitor file verification failures
   - Track deduplication efficiency

3. **Documentation**
   - Document custom wordlist sources
   - Maintain changelog for rule modifications
   - Record storage growth trends

## Troubleshooting

### Common Issues

1. **Disk Space Exhaustion**
   ```bash
   # Emergency cleanup
   find /var/lib/krakenhashes/hashlist_uploads -mtime +1 -delete
   docker system prune -f
   ```

2. **File Verification Failures**
   ```sql
   -- Find failed verifications
   SELECT * FROM wordlists 
   WHERE verification_status = 'failed';
   
   -- Reset for re-verification
   UPDATE wordlists 
   SET verification_status = 'pending' 
   WHERE verification_status = 'failed';
   ```

3. **Permission Issues**
   ```bash
   # Fix permissions
   chown -R 1000:1000 /var/lib/krakenhashes
   chmod -R 750 /var/lib/krakenhashes
   ```

### Debug Commands

```bash
# Check file system integrity
fsck -n /dev/sdb1

# Monitor I/O performance
iostat -x 1

# Check open files
lsof | grep krakenhashes

# Verify Docker volumes
docker volume inspect krakenhashes_app_data
```