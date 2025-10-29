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
- Formula with parallel workers: `work_mem × hash_mem_multiplier × (parallel_workers + 1)`

**KrakenHashes Default:** 256MB (with parallel hash disabled)

**Why This Configuration Works:**

Setting work_mem provides significant performance benefits, but requires careful tuning to avoid memory explosion with parallel hash joins:

**Memory Allocation Math:**
- Default: 256MB × 1 (hash_mem_multiplier) × 1 (no parallel hash) = **256MB per operation**
- With parallel hash (disabled): Each worker builds own hash table independently
- Total memory: Predictable and manageable even with 2-3 parallel workers

**Parallel Hash vs. Non-Parallel Hash:**

| Configuration | Memory per Query | Performance | Stability |
|--------------|------------------|-------------|-----------|
| 4MB default, parallel hash on | 4MB × 2 × 3 = 24MB | Fast (but won't parallelize) | ✅ Stable |
| 256MB, parallel hash ON | 256MB × 2 × 3 = **1.5GB** | Fastest (risky) | ❌ Memory failures |
| **256MB, parallel hash OFF** | 256MB × 1 × 1 = 256MB | Fast | ✅ Stable |

**Benefits of 256MB work_mem:**
- ✅ 64x more memory than default for large sorts/aggregations
- ✅ Faster query performance for bulk operations
- ✅ Better handling of array operations (10k crack batches)
- ✅ Parallel workers still active for scans/sorts (not hash joins)

!!! success "Recommended Configuration"
    Use 256MB work_mem **with parallel hash disabled** for optimal balance of performance and stability. This provides significant speed improvements while avoiding memory allocation failures.

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
100 connections × 256MB × 3 operations = 76.8 GB theoretical max
```

!!! note "Real-World Usage"
    In practice, not all connections are active simultaneously. KrakenHashes typically uses 10-20 active connections under normal load.

---

### enable_parallel_hash - Parallel Hash Join Control

**What It Does:**
- Controls whether hash joins use shared parallel hash tables
- Introduced in PostgreSQL 11 for performance optimization
- **KrakenHashes disables this** to prevent memory explosion

**KrakenHashes Setting:** `off` (disabled)

**Why Disabled:**

Parallel hash joins create shared hash tables across workers, multiplying memory usage:

**With `enable_parallel_hash=on` (PostgreSQL default):**
- Workers share one large hash table
- Memory formula: `work_mem × hash_mem_multiplier × (workers + 1)`
- Example: 256MB × 2 × 3 = **1.5GB per hash join**
- Risk: Exceeds shared memory segment limits → "No space left on device"

**With `enable_parallel_hash=off` (KrakenHashes):**
- Each worker builds own smaller hash table
- Memory formula: `work_mem × hash_mem_multiplier × 1`
- Example: 256MB × 1 × 1 = **256MB per worker**
- Benefit: Predictable memory usage, no shared memory failures

**What You Still Get:**
- ✅ Parallel sequential scans (faster table scans)
- ✅ Parallel aggregations (faster GROUP BY)
- ✅ Parallel sorts (faster ORDER BY)
- ❌ Parallel hash joins (disabled for stability)

!!! success "Recommended Setting"
    Keep `enable_parallel_hash=off` for stable operation with 256MB work_mem. The performance loss is minimal compared to the stability gain.

---

### hash_mem_multiplier - Hash Operation Memory Multiplier

**What It Does:**
- Multiplies available memory for hash-based operations (hash joins, hash aggregations)
- Allows hash operations to use more memory than sorts
- Default in PostgreSQL 15+: 2.0 (doubled from 1.0)

**KrakenHashes Setting:** `1` (reduced from default)

**Why Reduced:**

The default 2.0 multiplier doubles memory allocation for hash operations:

**Math with 256MB work_mem:**
- `hash_mem_multiplier=2`: 256MB × 2 = **512MB per hash operation**
- `hash_mem_multiplier=1`: 256MB × 1 = **256MB per hash operation**

**Combined with Parallel Workers:**
- With multiplier=2: 512MB × 3 workers = 1.5GB (risky)
- With multiplier=1: 256MB × 1 worker = 256MB (safe)

**Benefits of Setting to 1:**
- ✅ Reduces memory pressure by 50%
- ✅ More predictable resource usage
- ✅ Safer for concurrent queries
- ✅ Still 64x more than default 4MB

!!! tip "Performance vs. Stability"
    Setting `hash_mem_multiplier=1` provides excellent performance (256MB for hashes) while maintaining stability. The default 2.0 is optimized for systems with abundant memory, but KrakenHashes prioritizes reliable operation at scale.

---

## Batch Size and Memory Relationship

### Why 10k Batch Size is Optimal

KrakenHashes uses 10,000 crack batches for several reasons:

1. **Memory Efficiency**: ~1.7 MB per batch works well with PostgreSQL's default memory settings
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

# effective_cache_size: 50% of total RAM
POSTGRES_EFFECTIVE_CACHE_SIZE = Total_RAM × 0.50

# maintenance_work_mem: 3% of total RAM
POSTGRES_MAINTENANCE_WORK_MEM = Total_RAM × 0.03
```

**Examples:**

**For 8GB system:**
- shared_buffers: 8192 MB × 0.12 = **983 MB** (round to 1GB)
- effective_cache_size: 8192 MB × 0.50 = **4096 MB** (4GB)
- maintenance_work_mem: 8192 MB × 0.03 = **245 MB** (round to 256MB)

**For 32GB system:**
- shared_buffers: 32768 MB × 0.12 = **3932 MB** (round to 4GB)
- effective_cache_size: 32768 MB × 0.50 = **16384 MB** (16GB)
- maintenance_work_mem: 32768 MB × 0.03 = **983 MB** (round to 1GB)

### Aggressive Formula (Dedicated Database Server)

If KrakenHashes is the only major application on the system:

```bash
# shared_buffers: 25% of total RAM
POSTGRES_SHARED_BUFFERS = Total_RAM × 0.25

# effective_cache_size: 75% of total RAM
POSTGRES_EFFECTIVE_CACHE_SIZE = Total_RAM × 0.75

# maintenance_work_mem: 5% of total RAM
POSTGRES_MAINTENANCE_WORK_MEM = Total_RAM × 0.05
```

!!! warning "Dedicated Server Only"
    Use aggressive settings only if KrakenHashes is the primary workload. Leave room for OS and other services!

!!! note "work_mem Not Configured"
    KrakenHashes intentionally does not configure work_mem, using PostgreSQL's default (4MB). Do not add work_mem to your configuration.

## Troubleshooting

### "No space left on device" Errors

**Symptom:**
```
ERROR: pq: could not resize shared memory segment "/PostgreSQL.XXXXXX" to XXXXXX bytes: No space left on device
```

**Root Cause:** Large `work_mem` settings cause PostgreSQL to attempt oversized memory allocations that exceed kernel limits

**Solution:**
1. **Remove work_mem configuration** if you've manually set it:
   ```bash
   # In .env file - REMOVE these lines if present:
   # POSTGRES_WORK_MEM=...
   ```

2. Remove from docker-compose files if manually added

3. Restart PostgreSQL:
   ```bash
   docker-compose restart postgres
   ```

4. Verify using default:
   ```bash
   docker exec krakenhashes-postgres psql -U krakenhashes -d krakenhashes -c "SHOW work_mem"
   # Should show: 4MB
   ```

**Why This Works:** PostgreSQL's default 4MB work_mem prevents oversized shared memory allocations. KrakenHashes queries are optimized to work efficiently with this default.

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

**Root Cause:** Insufficient `shared_buffers` causing disk I/O contention under concurrent load

**Solution:**
1. Increase `shared_buffers`:
   ```bash
   POSTGRES_SHARED_BUFFERS=2GB  # or higher based on your RAM
   ```

2. Restart PostgreSQL

3. Test with high-concurrency workload

**Goal:** Zero retry attempts under normal load

**Note:** Do NOT increase work_mem - this can make the problem worse by triggering shared memory allocation errors.

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
WHERE name IN ('shared_buffers', 'effective_cache_size', 'maintenance_work_mem', 'max_connections')
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
