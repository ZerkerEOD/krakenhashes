# PostgreSQL Tuning Guide

This guide explains PostgreSQL memory settings and how they impact KrakenHashes performance, particularly for high-volume crack processing operations.

## Overview

PostgreSQL memory configuration is **critical** for KrakenHashes performance. Poor memory settings can lead to:

- "No space left on device" errors during crack processing
- Slow hash lookups (disk I/O instead of memory)
- Unnecessary retry attempts
- Poor multi-agent scalability

With proper tuning, KrakenHashes can process millions of hashes efficiently with zero errors.

## Why Memory Matters for Password Cracking

Unlike traditional database applications, password cracking systems have unique characteristics:

1. **Bulk Hash Lookups**: Processing 10,000 hash values simultaneously in a single query
2. **High Write Volume**: Millions of crack updates in rapid succession
3. **Frequent Table Scans**: Checking hash status across large tables
4. **Memory-Intensive Operations**: Array operations for batch processing

Poor PostgreSQL memory configuration directly causes the errors you've seen:
```
pq: could not resize shared memory segment to 1669056 bytes: No space left on device
```

This isn't about disk space—it's about **PostgreSQL running out of working memory for query operations**.

## Understanding Each Setting

### shared_buffers - The Main Database Cache

**What It Does:**
- PostgreSQL's primary cache for frequently accessed data pages
- Shared across ALL database connections
- Caches table rows, indexes, and query results

**Default Value:** 128MB (way too low!)

**Recommended Values:**
- 4GB systems: 512MB
- 8GB systems: 1GB ⭐ (default)
- 16GB systems: 4GB
- 32GB+ systems: 8-16GB

**Formula:** 12-25% of total system RAM

**Impact on KrakenHashes:**

```
WITHOUT proper shared_buffers (128MB):
- First hash lookup: Reads from disk (slow)
- Second hash lookup: Still reads from disk (cache too small)
- 1.75M hash lookups: Constant disk thrashing
- Job completion time: 30+ minutes

WITH proper shared_buffers (4GB):
- First hash lookup: Reads from disk, caches in memory
- Second hash lookup: Reads from memory cache (100x faster!)
- 1.75M hash lookups: Nearly all from memory
- Job completion time: 2-3 minutes
```

!!! success "Performance Multiplier"
    Increasing `shared_buffers` from 128MB to 4GB can improve hash lookup performance by **50-100x** for large jobs!

---

### work_mem - Per-Operation Working Memory

**What It Does:**
- Memory allocated **per query operation** (sorts, hashes, joins, array operations)
- Each connection can use `work_mem` multiple times per query
- **CRITICAL** for bulk hash lookups with `ANY($1)` array operations

**Default Value:** 4MB (insufficient for 10k hash batches!)

**Recommended Values:**
- 4GB systems: 32MB
- 8GB systems: 64MB ⭐ (default)
- 16GB systems: 128MB
- 32GB+ systems: 256MB-1GB

**Formula:** 0.5-1% of total system RAM

**Why It Matters for Crack Processing:**

When processing a 10k crack batch, the backend executes:
```sql
SELECT * FROM hashes WHERE hash_value = ANY($1)
```

Where `$1` is an array of 10,000 hash strings.

**Memory Requirement Calculation:**
```
10,000 hashes × ~50 bytes per hash = ~500 KB base array
+ PostgreSQL hash table overhead = ~800 KB
+ Query planning overhead = ~400 KB
Total: ~1.7 MB per batch
```

**With 4MB work_mem (old default):**
- 10k batch barely fits
- Any other concurrent query activity → "No space left on device"
- Frequent retry attempts needed

**With 64MB work_mem (new default):**
- 10k batch uses only 2.7% of available memory
- 35x headroom for concurrent operations
- Zero memory errors

!!! warning "The Critical Setting"
    `work_mem` is THE most important setting for preventing "No space left on device" errors. If you experience memory errors, increase this first!

---

### effective_cache_size - Query Planner Hint

**What It Does:**
- Tells PostgreSQL's query planner how much memory is available for caching
- Does NOT allocate memory itself (just a planning hint)
- Influences whether PostgreSQL uses indexes or sequential scans

**Default Value:** 4GB (reasonable default)

**Recommended Values:**
- 4GB systems: 2GB
- 8GB systems: 4GB ⭐ (default)
- 16GB systems: 8GB
- 32GB+ systems: 16-32GB

**Formula:** 50% of total system RAM

**Impact:**
- Helps PostgreSQL make better decisions about query execution plans
- Higher values encourage index usage
- Lower values encourage sequential scans

!!! info "Planner Hint Only"
    Unlike `shared_buffers` and `work_mem`, this setting doesn't allocate memory. It's safe to set relatively high.

---

### maintenance_work_mem - Index and VACUUM Operations

**What It Does:**
- Memory for maintenance operations: `VACUUM`, `CREATE INDEX`, `ALTER TABLE`
- Used during index builds and database cleanup
- Does NOT affect normal query processing

**Default Value:** 64MB

**Recommended Values:**
- 4GB systems: 128MB
- 8GB systems: 256MB ⭐ (default)
- 16GB systems: 1GB
- 32GB+ systems: 2-8GB

**Formula:** 2-5% of total system RAM

**Impact on KrakenHashes:**
- Faster index creation when setting up hashlists
- Faster `VACUUM` operations (database cleanup)
- Better autovacuum performance

---

### max_connections - Connection Pool Size

**What It Does:**
- Maximum number of simultaneous database connections
- Each connection can use multiple `work_mem` allocations

**Default Value:** 100 ⭐ (good for most deployments)

**Recommended Values:**
- Most deployments: 100
- High-concurrency: 150-250
- Low-resource systems: 50

**Memory Implications:**

Each connection can potentially use:
- `work_mem` × (number of operations in query)
- Typical query: 2-3 work_mem allocations

**Maximum theoretical memory:**
```
max_connections × work_mem × operations per query
100 connections × 64MB × 3 operations = 19.2 GB theoretical max
```

!!! note "Real-World Usage"
    In practice, not all connections are active simultaneously. KrakenHashes typically uses 10-20 active connections under normal load.

## Batch Size and Memory Relationship

### Why 10k Batch Size is Optimal

KrakenHashes uses 10,000 crack batches for several reasons:

1. **Memory Efficiency**: ~1.7 MB per batch fits comfortably in 64MB work_mem
2. **Network Efficiency**: ~500 KB WebSocket message size (good balance)
3. **Transaction Time**: 10-15 seconds per batch (avoids long-running transactions)
4. **Lock Contention**: Shorter transactions = fewer deadlocks

### Could We Increase Batch Size?

**Technically, yes. Practically, no need.**

| Batch Size | Memory Required | Transaction Time | Benefit |
|-----------|-----------------|------------------|---------|
| 10k | 1.7 MB | 10-15s | ✅ Optimal |
| 20k | 3.4 MB | 20-30s | Minimal gain |
| 50k | 8.5 MB | 60-90s | Risk of timeouts |
| 100k | 17 MB | 2-3 minutes | Lock contention issues |

**Recommendation:** Keep 10k batch size. It's well-tested and performs excellently.

## Calculating Settings for Your System

### Conservative Formula (Recommended)

```bash
# shared_buffers: 12% of total RAM
POSTGRES_SHARED_BUFFERS = Total_RAM × 0.12

# work_mem: 0.75% of total RAM
POSTGRES_WORK_MEM = Total_RAM × 0.0075

# effective_cache_size: 50% of total RAM
POSTGRES_EFFECTIVE_CACHE_SIZE = Total_RAM × 0.50

# maintenance_work_mem: 3% of total RAM
POSTGRES_MAINTENANCE_WORK_MEM = Total_RAM × 0.03
```

**Examples:**

**For 8GB system:**
- shared_buffers: 8192 MB × 0.12 = **983 MB** (round to 1GB)
- work_mem: 8192 MB × 0.0075 = **61 MB** (round to 64MB)
- effective_cache_size: 8192 MB × 0.50 = **4096 MB** (4GB)
- maintenance_work_mem: 8192 MB × 0.03 = **245 MB** (round to 256MB)

**For 32GB system:**
- shared_buffers: 32768 MB × 0.12 = **3932 MB** (round to 4GB)
- work_mem: 32768 MB × 0.0075 = **245 MB** (round to 256MB)
- effective_cache_size: 32768 MB × 0.50 = **16384 MB** (16GB)
- maintenance_work_mem: 32768 MB × 0.03 = **983 MB** (round to 1GB)

### Aggressive Formula (Dedicated Database Server)

If KrakenHashes is the only major application on the system:

```bash
# shared_buffers: 25% of total RAM
POSTGRES_SHARED_BUFFERS = Total_RAM × 0.25

# work_mem: 1% of total RAM
POSTGRES_WORK_MEM = Total_RAM × 0.01

# effective_cache_size: 75% of total RAM
POSTGRES_EFFECTIVE_CACHE_SIZE = Total_RAM × 0.75

# maintenance_work_mem: 5% of total RAM
POSTGRES_MAINTENANCE_WORK_MEM = Total_RAM × 0.05
```

!!! warning "Dedicated Server Only"
    Use aggressive settings only if KrakenHashes is the primary workload. Leave room for OS and other services!

## Troubleshooting

### "No space left on device" Errors

**Symptom:**
```
ERROR: pq: could not resize shared memory segment "/PostgreSQL.XXXXXX" to XXXXXX bytes: No space left on device
```

**Root Cause:** `work_mem` too small for bulk operations

**Solution:**
1. Increase `work_mem` immediately:
   ```bash
   # In .env file
   POSTGRES_WORK_MEM=128MB  # Double current value
   ```

2. Restart PostgreSQL:
   ```bash
   docker-compose restart postgres
   ```

3. Monitor logs for improvement

**Prevention:** Use recommended `work_mem` values from [System Requirements](system-requirements.md)

---

### Slow Hash Lookups

**Symptom:**
- Jobs take much longer than expected
- High disk I/O usage
- `docker stats` shows low PostgreSQL memory usage

**Root Cause:** `shared_buffers` too small, forcing disk reads

**Solution:**
1. Increase `shared_buffers`:
   ```bash
   # In .env file
   POSTGRES_SHARED_BUFFERS=4GB  # or appropriate for your RAM
   ```

2. Restart PostgreSQL

3. Run a test job and monitor performance improvement

**Expected Results:**
- 10-100x faster hash lookups after cache warms up
- Higher PostgreSQL memory usage (this is good!)
- Reduced disk I/O

---

### Retry Logic Triggering Frequently

**Symptom:** Logs show many retry attempts:
```
[WARNING] Transient database error processing crack batch (attempt 1/3)
[INFO] Retrying crack batch processing (attempt 2/3, delay=1s)
```

**Root Cause:** Memory exhaustion under concurrent load

**Solution:**
1. Increase both `work_mem` and `shared_buffers`:
   ```bash
   POSTGRES_WORK_MEM=128MB
   POSTGRES_SHARED_BUFFERS=2GB
   ```

2. Restart PostgreSQL

3. Test with high-concurrency workload

**Goal:** Zero retry attempts under normal load

---

### High PostgreSQL Memory Usage

**Symptom:** `docker stats` shows PostgreSQL using lots of RAM

**Is This a Problem?** **NO!** This is correct behavior.

**Explanation:**
- PostgreSQL actively uses `shared_buffers` for caching
- High memory usage = good caching = fast queries
- "Free" memory is wasted memory in database systems

**When It IS a Problem:**
- Host system runs out of memory
- Other services are starved
- System becomes unresponsive

**Solution (if needed):**
- Reduce `shared_buffers` to leave more room for OS
- Consider upgrading system RAM
- Review [System Requirements](system-requirements.md) for proper sizing

## Monitoring PostgreSQL Performance

### Check Current Settings

```bash
docker exec krakenhashes-postgres psql -U krakenhashes -d krakenhashes -c "
SELECT name, setting, unit, source
FROM pg_settings
WHERE name IN ('shared_buffers', 'work_mem', 'effective_cache_size', 'maintenance_work_mem', 'max_connections')
ORDER BY name;"
```

### Monitor Memory Usage

```bash
# PostgreSQL memory usage
docker stats krakenhashes-postgres --no-stream

# Detailed breakdown
docker exec krakenhashes-postgres psql -U krakenhashes -d krakenhashes -c "
SELECT
    pg_size_pretty(pg_database_size('krakenhashes')) as db_size,
    pg_size_pretty(pg_total_relation_size('hashes')) as hashes_table_size,
    pg_size_pretty(pg_total_relation_size('hashlists')) as hashlists_table_size;"
```

### Query Performance Analysis

```bash
# Show slow queries (>1 second)
docker exec krakenhashes-postgres psql -U krakenhashes -d krakenhashes -c "
SELECT query, calls, total_time, mean_time
FROM pg_stat_statements
WHERE mean_time > 1000
ORDER BY total_time DESC
LIMIT 10;"
```

## Best Practices

### 1. Start with Defaults, Tune as Needed

KrakenHashes ships with sensible 8GB defaults. Don't over-optimize prematurely.

### 2. Monitor Before Tuning

Collect performance data first:
- Are there memory errors?
- How long do jobs take?
- What's the retry rate?

### 3. Change One Setting at a Time

When tuning:
1. Change one setting
2. Restart PostgreSQL
3. Test thoroughly
4. Measure impact
5. Repeat

### 4. Document Your Changes

Keep notes on what you changed and why. Include:
- System specifications
- Workload characteristics
- Performance before/after
- Any issues encountered

### 5. Leave Room for Growth

Don't allocate 100% of system RAM to PostgreSQL. Leave buffer for:
- OS overhead (1-2 GB)
- Backend processes (1 GB)
- Temporary spikes
- Future growth

## Advanced Tuning

### For Very Large Jobs (50M+ hashes)

```bash
POSTGRES_SHARED_BUFFERS=16GB
POSTGRES_WORK_MEM=512MB
POSTGRES_EFFECTIVE_CACHE_SIZE=32GB
POSTGRES_MAINTENANCE_WORK_MEM=4GB
POSTGRES_MAX_CONNECTIONS=150
```

### For High Concurrency (20+ agents)

```bash
POSTGRES_SHARED_BUFFERS=8GB
POSTGRES_WORK_MEM=128MB
POSTGRES_EFFECTIVE_CACHE_SIZE=16GB
POSTGRES_MAINTENANCE_WORK_MEM=2GB
POSTGRES_MAX_CONNECTIONS=200
```

### For Memory-Constrained Systems (4GB RAM)

```bash
POSTGRES_SHARED_BUFFERS=512MB
POSTGRES_WORK_MEM=32MB
POSTGRES_EFFECTIVE_CACHE_SIZE=2GB
POSTGRES_MAINTENANCE_WORK_MEM=128MB
POSTGRES_MAX_CONNECTIONS=50
```

## Related Documentation

- [System Requirements](system-requirements.md) - Hardware sizing guide
- [Performance Optimization](../admin-guide/advanced/performance.md) - System-wide tuning
- [Troubleshooting](../user-guide/troubleshooting.md) - Common issues and solutions
