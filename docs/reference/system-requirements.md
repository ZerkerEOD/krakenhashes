# System Requirements

This guide provides detailed system requirements for deploying KrakenHashes across different scales, from development environments to enterprise production deployments.

## Overview

KrakenHashes is **optimized by default for 8GB RAM systems**, providing a balance between accessibility and performance. Multi-agent operations are supported at all tiers, making distributed password cracking accessible regardless of your infrastructure scale.

!!! info "Real-World Context"
    Our stress testing includes 1.75 million hash jobs with 100% crack rates to validate system stability under extreme conditions. Typical production environments see 10-30% crack rates per job, which require significantly fewer resources.

## Quick Reference Table

| Total RAM | Configuration | PostgreSQL Settings | Typical Use Case | Multi-Agent Support |
|-----------|---------------|-------------------|------------------|---------------------|
| 4GB | Minimum | 512MB / 32MB | Development, small jobs (<500k hashes) | ✅ Yes |
| **8GB** | **Default/Recommended** | **1GB / 64MB** | **Production, standard operations (1-10M hashes)** | ✅ Yes |
| 16GB | High-Volume | 4GB / 128MB | Large-scale operations, multiple concurrent jobs | ✅ Yes |
| 32GB | Enterprise | 8GB / 256MB | Enterprise multi-agent deployments | ✅ Yes |
| 64GB | Large-Scale | 16GB / 512MB | Extreme parallel processing | ✅ Yes |
| 96-128GB | Extreme | 24-32GB / 512MB-1GB | Maximum throughput operations | ✅ Yes |

!!! tip "8GB Default"
    KrakenHashes ships with PostgreSQL settings optimized for 8GB systems. No configuration changes are needed for standard deployments.

## Detailed Configuration Guides

### Minimum Configuration (4GB RAM)

**System Allocation:**
- PostgreSQL: ~1.5 GB
- Backend + Frontend + Nginx: ~1 GB
- OS + Buffer: ~1.5 GB

**PostgreSQL Settings:**
```bash
POSTGRES_SHARED_BUFFERS=512MB
POSTGRES_WORK_MEM=32MB
POSTGRES_EFFECTIVE_CACHE_SIZE=2GB
POSTGRES_MAINTENANCE_WORK_MEM=128MB
POSTGRES_MAX_CONNECTIONS=100
```

**Suitable For:**
- Development environments
- Small hash jobs (<500k hashes)
- Single or few agents
- Testing and evaluation

**Limitations:**
- May experience occasional memory pressure under load
- Retry logic will handle transient failures automatically
- Slower performance on large batch operations

---

### Default Configuration (8GB RAM) ⭐ Recommended

**System Allocation:**
- PostgreSQL: ~3.5 GB
- Backend + Frontend + Nginx: ~1 GB
- OS + Buffer: ~3.5 GB

**PostgreSQL Settings (Default):**
```bash
POSTGRES_SHARED_BUFFERS=1GB
POSTGRES_WORK_MEM=64MB
POSTGRES_EFFECTIVE_CACHE_SIZE=4GB
POSTGRES_MAINTENANCE_WORK_MEM=256MB
POSTGRES_MAX_CONNECTIONS=100
```

**Suitable For:**
- Production deployments
- Multi-agent operations
- Hash jobs up to 10M
- Standard crack rates (10-30%)
- Multiple concurrent jobs

**Performance:**
- Zero memory exhaustion errors under normal load
- Fast hash lookups with memory caching
- Smooth multi-agent coordination
- Retry logic acts as safety net only

---

### High-Volume Configuration (16GB RAM)

**System Allocation:**
- PostgreSQL: ~8 GB
- Backend + Frontend + Nginx: ~2 GB
- OS + Buffer: ~6 GB

**PostgreSQL Settings:**
```bash
POSTGRES_SHARED_BUFFERS=4GB
POSTGRES_WORK_MEM=128MB
POSTGRES_EFFECTIVE_CACHE_SIZE=8GB
POSTGRES_MAINTENANCE_WORK_MEM=1GB
POSTGRES_MAX_CONNECTIONS=100
```

**Suitable For:**
- High-volume operations (10M+ hashes)
- Multiple concurrent large jobs
- Many agents running simultaneously
- Stress testing scenarios

**Performance:**
- Entire hash tables fit in memory
- 50-100x faster hash lookups
- Minimal disk I/O
- Ideal for sustained high-throughput

---

### Enterprise Configuration (32GB RAM)

**System Allocation:**
- PostgreSQL: ~16 GB
- Backend + Frontend + Nginx: ~4 GB
- OS + Buffer: ~12 GB

**PostgreSQL Settings:**
```bash
POSTGRES_SHARED_BUFFERS=8GB
POSTGRES_WORK_MEM=256MB
POSTGRES_EFFECTIVE_CACHE_SIZE=16GB
POSTGRES_MAINTENANCE_WORK_MEM=2GB
POSTGRES_MAX_CONNECTIONS=150
```

**Suitable For:**
- Enterprise multi-team environments
- Dozens of agents
- Multiple 10M+ hash jobs concurrently
- High-frequency job submission

**Performance:**
- Maximum query performance
- Large concurrent workloads
- Extensive memory caching
- Optimal for 24/7 operations

---

### Large-Scale Configuration (64GB RAM)

**System Allocation:**
- PostgreSQL: ~32 GB
- Backend + Frontend + Nginx: ~4 GB
- OS + Buffer: ~28 GB

**PostgreSQL Settings:**
```bash
POSTGRES_SHARED_BUFFERS=16GB
POSTGRES_WORK_MEM=512MB
POSTGRES_EFFECTIVE_CACHE_SIZE=32GB
POSTGRES_MAINTENANCE_WORK_MEM=4GB
POSTGRES_MAX_CONNECTIONS=200
```

**Suitable For:**
- Massive parallel processing
- Hundreds of agents
- Continuous high-volume operations
- Very large individual jobs (50M+ hashes)

---

### Extreme Configuration (96-128GB RAM)

**System Allocation:**
- PostgreSQL: ~48-64 GB
- Backend + Frontend + Nginx: ~4 GB
- OS + Buffer: ~44-60 GB

**PostgreSQL Settings:**
```bash
POSTGRES_SHARED_BUFFERS=24GB  # or 32GB for 128GB systems
POSTGRES_WORK_MEM=1GB
POSTGRES_EFFECTIVE_CACHE_SIZE=48GB
POSTGRES_MAINTENANCE_WORK_MEM=8GB
POSTGRES_MAX_CONNECTIONS=250
```

**Suitable For:**
- Maximum throughput scenarios
- Enterprise-scale deployments
- Extremely large jobs (100M+ hashes)
- Research and development at scale

## Applying Configuration Changes

### Step 1: Edit .env File

Add or modify the PostgreSQL memory settings in your `.env` file:

```bash
# PostgreSQL Memory Configuration
POSTGRES_SHARED_BUFFERS=1GB
POSTGRES_WORK_MEM=64MB
POSTGRES_EFFECTIVE_CACHE_SIZE=4GB
POSTGRES_MAINTENANCE_WORK_MEM=256MB
POSTGRES_MAX_CONNECTIONS=100
```

!!! warning "Restart Required"
    PostgreSQL memory changes require a container restart to take effect.

### Step 2: Restart PostgreSQL

```bash
docker-compose restart postgres
```

Or for a full restart:

```bash
docker-compose down
docker-compose up -d
```

### Step 3: Verify Settings

```bash
docker exec krakenhashes-postgres psql -U krakenhashes -d krakenhashes -c "SHOW shared_buffers; SHOW work_mem;"
```

Expected output:
```
 shared_buffers
----------------
 1GB
(1 row)

 work_mem
----------
 64MB
(1 row)
```

## Performance Expectations by Tier

### Hash Lookup Performance

| Configuration | First Lookup | Subsequent Lookups | Benefit |
|--------------|--------------|-------------------|---------|
| 4GB | Disk I/O | Partially cached | Baseline |
| 8GB | Partially cached | Mostly cached | 10-20x faster |
| 16GB | Cached | Fully cached | 50-100x faster |
| 32GB+ | Fully cached | Fully cached | Maximum speed |

### Batch Processing Capability

All configurations handle 10k crack batches efficiently. The difference is in **sustained throughput** and **concurrent job capacity**:

- **4GB**: 1-2 concurrent jobs
- **8GB**: 3-5 concurrent jobs
- **16GB**: 10+ concurrent jobs
- **32GB+**: 20+ concurrent jobs

### Retry Logic Behavior

| Configuration | Expected Retries | Behavior |
|--------------|------------------|----------|
| 4GB | Occasional | 1-5% of batches under load |
| 8GB | Rare | <0.1% under normal load |
| 16GB+ | Never | Safety net only |

## Additional Considerations

### Disk Space Requirements

- **Minimum**: 20GB (OS, application, small wordlists)
- **Recommended**: 100GB+ (moderate wordlist collection)
- **Production**: 500GB - 2TB (extensive wordlist/rule libraries)

!!! info "Wordlist Size Matters"
    Disk requirements scale primarily with your wordlist and rule collection size. Hash storage is comparatively minimal (~100 bytes per hash).

### CPU Requirements

- **Minimum**: 2 cores
- **Recommended**: 4-8 cores
- **Production**: 8-16+ cores for concurrent job processing

!!! tip "CPU vs GPU"
    CPUs handle job coordination and database operations. GPUs (on agents) handle the actual password cracking. Both are important!

### Network Bandwidth

- **Agent-Backend Communication**: ~100 KB/s per agent (WebSocket)
- **Crack Transmission**: ~500 KB-1 MB per 10k crack batch
- **File Synchronization**: Depends on wordlist/rule sizes

For 10 agents running continuously:
- ~1-2 Mbps sustained
- ~10-20 Mbps peak during file sync

## Troubleshooting

### "No space left on device" Errors

**Symptom**: PostgreSQL shared memory errors in logs

**Solution**: Increase `POSTGRES_WORK_MEM`

```bash
# In .env file
POSTGRES_WORK_MEM=128MB  # or higher
```

See [PostgreSQL Tuning Guide](postgresql-tuning.md#troubleshooting) for detailed troubleshooting.

### Slow Hash Lookups

**Symptom**: Jobs take much longer than expected

**Solution**: Increase `POSTGRES_SHARED_BUFFERS`

```bash
# In .env file
POSTGRES_SHARED_BUFFERS=2GB  # or higher
```

### High Memory Usage

**Symptom**: Docker container using excessive RAM

**Solution**: This is usually correct behavior. PostgreSQL actively uses allocated memory for caching. If the host system is struggling:

1. Reduce `POSTGRES_SHARED_BUFFERS`
2. Reduce `POSTGRES_WORK_MEM`
3. Consider upgrading system RAM

## Related Documentation

- [PostgreSQL Tuning Guide](postgresql-tuning.md) - Detailed tuning instructions
- [Performance Optimization](../admin-guide/advanced/performance.md) - System-wide performance tips
- [Installation Guide](../getting-started/installation.md) - Initial setup instructions
