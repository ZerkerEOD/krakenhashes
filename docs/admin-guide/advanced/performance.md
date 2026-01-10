# Performance Tuning Guide

This guide provides comprehensive performance optimization strategies for KrakenHashes deployments. All recommendations are based on the actual implementation details and system architecture.

## Table of Contents

1. [Database Performance](#database-performance)
2. [Job Execution Optimization](#job-execution-optimization)
3. [Agent Performance](#agent-performance)
4. [File I/O Optimization](#file-io-optimization)
5. [Network and WebSocket Tuning](#network-and-websocket-tuning)
6. [Storage Performance](#storage-performance)
7. [Monitoring and Benchmarking](#monitoring-and-benchmarking)
8. [System Settings Reference](#system-settings-reference)

## Database Performance

### Connection Pool Configuration

The system uses PostgreSQL with connection pooling. Current default settings:

```go
// From backend/internal/db/db.go
db.SetMaxOpenConns(25)      // Maximum number of open connections
db.SetMaxIdleConns(5)       // Maximum number of idle connections
db.SetConnMaxLifetime(5 * time.Minute)  // Connection lifetime
```

**Optimization recommendations:**

1. **For small deployments (1-10 agents):**
   ```bash
   # Keep defaults
   MAX_OPEN_CONNS=25
   MAX_IDLE_CONNS=5
   ```

2. **For medium deployments (10-50 agents):**
   ```bash
   # Increase connection pool
   MAX_OPEN_CONNS=50
   MAX_IDLE_CONNS=10
   ```

3. **For large deployments (50+ agents):**
   ```bash
   # Use external connection pooler (PgBouncer)
   MAX_OPEN_CONNS=100
   MAX_IDLE_CONNS=20
   ```

### Index Optimization

The system uses extensive indexing. Key performance indexes:

```sql
-- Agent performance queries
idx_agents_status
idx_agents_last_heartbeat
idx_agent_performance_metrics_device_lookup (composite)

-- Job execution queries
idx_job_tasks_job_chunk (composite)
idx_job_tasks_status
idx_job_executions_status

-- Hash lookups
idx_hashes_hash_value (unique)
idx_hashlists_status
```

**Monitoring index usage:**

```sql
-- Check index usage statistics
SELECT 
    schemaname,
    tablename,
    indexname,
    idx_scan,
    idx_tup_read,
    idx_tup_fetch
FROM pg_stat_user_indexes
ORDER BY idx_scan DESC;

-- Find missing indexes
SELECT 
    schemaname,
    tablename,
    attname,
    n_distinct,
    most_common_vals
FROM pg_stats
WHERE schemaname = 'public'
AND n_distinct > 100
AND tablename NOT IN (
    SELECT tablename 
    FROM pg_indexes 
    WHERE schemaname = 'public'
);
```

### Query Optimization

1. **Batch Processing Configuration:**
   ```bash
   # Environment variable
   export KH_HASHLIST_BATCH_SIZE=1000  # Default: 1000
   
   # Recommendations:
   # - Small hashlists (<100K): 500
   # - Medium hashlists (100K-1M): 1000
   # - Large hashlists (>1M): 2000-5000
   ```

2. **Pagination Optimization:**
   - Use cursor-based pagination for large result sets
   - Limit page sizes to 100-500 items
   - Add appropriate indexes for ORDER BY columns

## Job Execution Optimization

### Chunking System Configuration

The dynamic chunking system optimizes workload distribution based on agent performance.

**Key system settings:**

```sql
-- Configure chunk behavior
UPDATE system_settings SET value = '20' 
WHERE key = 'chunk_fluctuation_percentage';  -- Default: 20%

-- Benchmark cache duration
UPDATE system_settings SET value = '168' 
WHERE key = 'benchmark_cache_duration_hours';  -- Default: 168 (7 days)
```

### Chunk Size Calculation

The system calculates chunks based on:
1. Agent benchmark speeds
2. Desired chunk duration
3. Attack mode modifiers
4. Available keyspace

**Optimization strategies:**

1. **For GPU clusters with similar performance:**
   ```sql
   -- Larger chunks, less overhead
   UPDATE job_executions 
   SET chunk_duration = 3600  -- 1 hour chunks
   WHERE id = ?;
   ```

2. **For mixed hardware environments:**
   ```sql
   -- Smaller chunks for better distribution
   UPDATE job_executions 
   SET chunk_duration = 900  -- 15 minute chunks
   WHERE id = ?;
   ```

3. **For time-sensitive jobs:**
   ```sql
   -- Very small chunks for quick feedback
   UPDATE job_executions 
   SET chunk_duration = 300  -- 5 minute chunks
   WHERE id = ?;
   ```

### Job Priority and Scheduling

The scheduler processes jobs based on priority and available agents:

1. **Priority levels:**
   - Critical: Process immediately
   - High: Process within minutes
   - Normal: Standard queue processing
   - Low: Process when resources available

2. **Scheduling optimization:**
   ```sql
   -- Configure scheduler check interval (updated in v1.1+)
   UPDATE system_settings
   SET value = '3'  -- 3 seconds for faster response
   WHERE key = 'scheduler_check_interval_seconds';
   ```

### Parallel Scheduling Performance Improvements

**Version 1.1+** introduced dramatic performance improvements through parallel execution:

#### Parallel Benchmarking System

The benchmark system now executes all benchmark requests simultaneously using goroutines:

**Performance Impact:**
- **Sequential (old)**: 15 agents × 30s = 450 seconds
- **Parallel (new)**: 15 agents in ~12 seconds
- **Improvement**: 96% reduction (37.5x faster)

**How it works:**
1. Identifies all agents needing benchmarks
2. Sends all benchmark requests in parallel via goroutines
3. Waits for completion using database polling
4. Proceeds with task assignment once benchmarks complete

**Configuration:**
```sql
-- Benchmark cache duration
UPDATE system_settings
SET value = '168'  -- 7 days (default)
WHERE key = 'benchmark_cache_duration_hours';

-- Speedtest timeout (applies to each benchmark)
UPDATE system_settings
SET value = '180'  -- 3 minutes
WHERE key = 'speedtest_timeout_seconds';
```

**Benefits:**
- Eliminates benchmark bottleneck for large agent deployments
- Scales linearly with agent count
- No configuration changes required
- Automatic with v1.1+ upgrade

#### Parallel Task Assignment System

The task assignment system now uses two-phase parallel execution:

**Performance Impact:**
- **Sequential (old)**: 15 agents × 30s file sync = 450 seconds
- **Parallel (new)**: All agents in ~20 seconds
- **Improvement**: 95% reduction (22.5x faster)

**How it works:**
1. **Phase 1 (Sequential - ~35ms)**: Calculate all chunk ranges upfront
   - Prevents keyspace overlaps
   - Determines rule split chunks
   - Plans all assignments
2. **Phase 2 (Parallel - ~20s)**: Execute all operations via goroutines
   - Create rule chunk files
   - Sync hashlists to agents
   - Sync files (30s blocking window, but all agents in parallel)
   - Create task database records
   - Send WebSocket assignments

**Benefits:**
- Eliminates file sync bottleneck
- All agents receive work simultaneously
- Maintains sequential planning for correctness
- Automatic with v1.1+ upgrade

#### Combined Impact

For a typical deployment with 15 agents:

| Operation | Sequential | Parallel | Improvement |
|-----------|-----------|----------|-------------|
| Benchmarking (15 agents) | 450s | 12s | 96% faster |
| Task Assignment (15 agents) | 450s | 20s | 95% faster |
| **Total Scheduling Cycle** | **900s** | **32s** | **96% faster** |

**Scaling characteristics:**
- Sequential system: Time increases linearly with agent count
- Parallel system: Time remains constant (limited by slowest operation)
- Benefits increase with larger deployments

#### Scheduler Interval Reduction

With parallel execution eliminating bottlenecks, the scheduler interval was reduced:

- **Old**: 30 seconds between checks
- **New**: 3 seconds between checks
- **Result**: Jobs start 10x faster after being queued

**When to adjust:**
```sql
-- For very large deployments (100+ agents), consider increasing slightly
UPDATE system_settings
SET value = '5'  -- 5 seconds
WHERE key = 'scheduler_check_interval_seconds';

-- For small deployments (<10 agents), can reduce further
UPDATE system_settings
SET value = '1'  -- 1 second for maximum responsiveness
WHERE key = 'scheduler_check_interval_seconds';
```

## Agent Performance

### Hardware Detection and Benchmarking

The system automatically detects GPU capabilities and runs benchmarks.

**Benchmark optimization:**

```sql
-- Configure speedtest timeout
UPDATE system_settings 
SET value = '180'  -- 3 minutes
WHERE key = 'speedtest_timeout_seconds';

-- For faster benchmarks (less accurate)
UPDATE system_settings 
SET value = '60'  -- 1 minute
WHERE key = 'speedtest_timeout_seconds';
```

### Agent Configuration

1. **GPU-specific optimizations:**
   ```yaml
   # Agent config.yaml
   extra_parameters: "--optimize-kernel-workload --force"
   
   # For NVIDIA GPUs
   extra_parameters: "-O -w 4"
   
   # For AMD GPUs
   extra_parameters: "-O -w 3"
   ```

2. **Memory management:**
   ```yaml
   # Limit GPU memory usage
   extra_parameters: "--gpu-memory-fraction=0.8"
   ```

### Workload Distribution

The system supports multiple distribution strategies:

1. **Round-robin:** Equal distribution
2. **Performance-based:** More work to faster agents
3. **Priority-based:** Specific agents for specific jobs

## File I/O Optimization

### Hash List Processing

The system uses buffered I/O and batch processing for efficient file handling.

**Current implementation:**
- Buffered reading with `bufio.Scanner`
- Configurable batch sizes
- Streaming processing (no full file load)

**Optimization tips:**

1. **For NVMe storage:**
   ```bash
   export KH_HASHLIST_BATCH_SIZE=5000  # Larger batches
   ```

2. **For network storage:**
   ```bash
   export KH_HASHLIST_BATCH_SIZE=500   # Smaller batches
   ```

3. **File upload limits:**
   ```bash
   export KH_MAX_UPLOAD_SIZE_MB=32     # Default: 32MB
   # Increase for trusted environments
   export KH_MAX_UPLOAD_SIZE_MB=256    # 256MB
   ```

### File Synchronization

The agent file sync system uses:
- Chunk-based transfers
- Resume capability
- Integrity verification

**Optimization:**

1. **LAN deployments:**
   - Increase chunk sizes
   - Disable compression

2. **WAN deployments:**
   - Enable compression
   - Smaller chunk sizes
   - More aggressive retry policies

### File Hash Caching

The directory monitor uses an in-memory file hash cache to dramatically reduce disk I/O when monitoring wordlist and rule directories.

**How It Works:**

1. **ModTime+Size Validation**: Before calculating MD5, the cache checks if file modification time and size have changed
2. **Cache Hit**: If unchanged, returns cached hash (no disk read)
3. **Cache Miss**: Calculates MD5, updates cache for future requests
4. **Background Population**: Cache is populated asynchronously at startup

**Performance Impact:**

| Metric | Before | After |
|--------|--------|-------|
| Disk I/O (steady state) | ~500MB/s constant | Near zero |
| MD5 calculations | Every file, every 30s | Only changed files |
| SSD wear | High | Negligible |

**Key Benefits:**

- **Automatic**: No configuration required
- **Memory efficient**: ~100 bytes per cached file
- **Thread-safe**: RWMutex pattern for concurrent access
- **Self-healing**: Automatically recalculates when files change

**Potfile Sync Optimization:**

During heavy crack ingestion (thousands of passwords per minute), the potfile changes frequently. A 5-minute hash history window prevents agents from repeatedly re-downloading the potfile:

- Agent's potfile hash is checked against recent valid hashes
- If hash is within the 5-minute window, sync is skipped
- After ingestion stops, agents sync to the latest version

For technical details, see the [File Hash Cache Architecture](../../reference/architecture/file-hash-cache.md) documentation.

## Network and WebSocket Tuning

### WebSocket Configuration

The system uses WebSocket for real-time agent communication.

**Key optimizations:**

1. **Message processing:**
   - Asynchronous handlers for non-blocking operation
   - Goroutine-based processing for heavy operations
   - 30-second timeout for async operations

2. **Heartbeat optimization:**
   ```go
   // Agent sends heartbeat every 30 seconds
   // Server expects heartbeat within 90 seconds
   ```

3. **Connection management:**
   ```nginx
   # Nginx configuration for WebSocket
   proxy_read_timeout 3600s;
   proxy_send_timeout 3600s;
   proxy_connect_timeout 60s;
   
   # Buffer sizes
   proxy_buffer_size 64k;
   proxy_buffers 8 32k;
   proxy_busy_buffers_size 128k;
   ```

### TLS/SSL Performance

The system supports multiple TLS modes with configurable parameters:

```bash
# Certificate configuration
export KH_CERT_KEY_SIZE=2048        # Faster handshakes
# or
export KH_CERT_KEY_SIZE=4096        # Better security

# For high-traffic deployments
export KH_TLS_SESSION_CACHE=on
export KH_TLS_SESSION_TIMEOUT=300
```

## Storage Performance

### Directory Structure Optimization

```bash
/data/krakenhashes/
├── binaries/      # Hashcat binaries (SSD recommended)
├── wordlists/     # Large wordlists (HDD acceptable)
├── rules/         # Rule files (SSD preferred)
├── hashlists/     # User hashlists (SSD recommended)
└── temp/          # Temporary files (RAM disk optimal)
```

### Storage Recommendations

1. **SSD for critical paths:**
   - Database files
   - Hashcat binaries
   - Active hashlists
   - Temporary processing

2. **HDD acceptable for:**
   - Large wordlist storage
   - Archived hashlists
   - Backup data

3. **RAM disk for temporary files:**
   ```bash
   # Create RAM disk for temp files
   sudo mkdir -p /mnt/ramdisk
   sudo mount -t tmpfs -o size=2G tmpfs /mnt/ramdisk
   
   # Link to KrakenHashes temp
   ln -s /mnt/ramdisk /data/krakenhashes/temp
   ```

## Monitoring and Benchmarking

### Metrics Collection and Retention

The system includes automatic metrics aggregation:

```sql
-- Configure retention
UPDATE system_settings 
SET value = '30'  -- Keep realtime data for 30 days
WHERE key = 'metrics_retention_days';

-- Enable/disable aggregation
UPDATE system_settings 
SET value = 'true' 
WHERE key = 'enable_aggregation';
```

**Aggregation levels:**
- Realtime → Daily (after 24 hours)
- Daily → Weekly (after 7 days)
- Cleanup runs daily at 2 AM

### Performance Monitoring Queries

```sql
-- Agent performance overview
SELECT 
    a.name,
    a.status,
    COUNT(DISTINCT jt.id) as active_tasks,
    AVG(apm.hashes_per_second) as avg_speed,
    MAX(apm.temperature) as max_temp
FROM agents a
LEFT JOIN job_tasks jt ON a.id = jt.agent_id AND jt.status = 'in_progress'
LEFT JOIN agent_performance_metrics apm ON a.id = apm.agent_id
WHERE apm.created_at > NOW() - INTERVAL '1 hour'
GROUP BY a.id, a.name, a.status;

-- Job execution performance
SELECT 
    je.id,
    je.status,
    je.created_at,
    je.completed_at,
    je.progress,
    COUNT(jt.id) as total_chunks,
    COUNT(CASE WHEN jt.status = 'completed' THEN 1 END) as completed_chunks
FROM job_executions je
LEFT JOIN job_tasks jt ON je.id = jt.job_execution_id
GROUP BY je.id
ORDER BY je.created_at DESC;
```

### Benchmarking Best Practices

1. **Initial benchmarking:**
   - Run comprehensive benchmarks on agent registration
   - Test all hash types your organization uses
   - Store results for 7 days (default)

2. **Periodic re-benchmarking:**
   - After driver updates
   - After hardware changes
   - Monthly for consistency

3. **Benchmark commands:**
   ```bash
   # Force re-benchmark for specific agent
   curl -X POST https://api.krakenhashes.com/agents/{id}/benchmark \
     -H "Authorization: Bearer $TOKEN"
   ```

## System Settings Reference

### Performance-Related Settings

| Setting Key | Default | Description | Optimization Range |
|------------|---------|-------------|-------------------|
| `chunk_fluctuation_percentage` | 20 | Threshold for merging small chunks | 10-30% |
| `benchmark_cache_duration_hours` | 168 | How long to cache benchmark results | 24-720 hours |
| `metrics_retention_days` | 30 | Realtime metrics retention | 7-90 days |
| `enable_aggregation` | true | Enable metrics aggregation | true/false |
| `speedtest_timeout_seconds` | 180 | Benchmark timeout | 60-600 seconds |
| `scheduler_check_interval_seconds` | 30 | Job scheduler interval | 10-60 seconds |

### Environment Variables

| Variable | Default | Description | Optimization Tips |
|----------|---------|-------------|-------------------|
| `KH_HASHLIST_BATCH_SIZE` | 1000 | Database batch insert size | 500-5000 based on hardware |
| `KH_MAX_UPLOAD_SIZE_MB` | 32 | Maximum file upload size | 32-1024 based on trust |
| `DATABASE_MAX_OPEN_CONNS` | 25 | Max database connections | 25-100 based on load |
| `DATABASE_MAX_IDLE_CONNS` | 5 | Max idle connections | 20% of max open |

## Performance Troubleshooting

### Common Bottlenecks

1. **Database connection exhaustion:**
   - Symptom: "too many connections" errors
   - Solution: Increase connection pool or use PgBouncer

2. **Slow hash imports:**
   - Symptom: Hashlist processing takes hours
   - Solution: Increase batch size, use SSD storage

3. **Agent communication delays:**
   - Symptom: Delayed job updates
   - Solution: Check network latency, adjust timeouts

4. **Memory exhaustion:**
   - Symptom: OOM errors during processing
   - Solution: Reduce batch sizes, add swap space

### Performance Checklist

- [ ] Database indexes are being used (check pg_stat_user_indexes)
- [ ] Connection pool sized appropriately for agent count
- [ ] Batch sizes optimized for hardware
- [ ] Metrics retention configured
- [ ] Storage using appropriate media (SSD/HDD)
- [ ] Network timeouts adjusted for environment
- [ ] Benchmark cache duration set appropriately
- [ ] Chunk sizes appropriate for job types

## Recommended Configurations

### Small Deployment (1-10 agents)
```bash
# Keep most defaults
export KH_HASHLIST_BATCH_SIZE=1000
export DATABASE_MAX_OPEN_CONNS=25
# Use default chunk fluctuation (20%)
```

### Medium Deployment (10-50 agents)
```bash
export KH_HASHLIST_BATCH_SIZE=2000
export DATABASE_MAX_OPEN_CONNS=50
export DATABASE_MAX_IDLE_CONNS=10
# Adjust chunk fluctuation to 15%
UPDATE system_settings SET value = '15' WHERE key = 'chunk_fluctuation_percentage';
```

### Large Deployment (50+ agents)
```bash
export KH_HASHLIST_BATCH_SIZE=5000
export DATABASE_MAX_OPEN_CONNS=100
export DATABASE_MAX_IDLE_CONNS=20
# Use PgBouncer for connection pooling
# Adjust chunk fluctuation to 10%
UPDATE system_settings SET value = '10' WHERE key = 'chunk_fluctuation_percentage';
# Reduce metrics retention
UPDATE system_settings SET value = '14' WHERE key = 'metrics_retention_days';
```

## Next Steps

1. Review current system metrics
2. Identify bottlenecks using monitoring queries
3. Apply appropriate optimizations
4. Monitor impact and adjust
5. Document environment-specific settings

For additional support, consult the [System Overview](../../getting-started/overview.md) documentation or contact the development team.