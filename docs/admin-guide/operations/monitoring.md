# System Monitoring Guide

This guide covers comprehensive monitoring strategies for KrakenHashes, including system health indicators, performance metrics, logging, and alerting configurations.

## Table of Contents

1. [System Health Indicators](#system-health-indicators)
2. [Job Monitoring and Statistics](#job-monitoring-and-statistics)
3. [Agent Performance Metrics](#agent-performance-metrics)
4. [Database Monitoring](#database-monitoring)
5. [Log Analysis and Alerting](#log-analysis-and-alerting)
6. [Performance Baselines](#performance-baselines)
7. [Monitoring Dashboards and Tools](#monitoring-dashboards-and-tools)

## System Health Indicators

### Health Check Endpoint

The system provides a basic health check endpoint for monitoring service availability:

```bash
# Check system health
curl https://localhost:31337/api/health

# Expected response
200 OK
```

### Service Status Monitoring

Monitor the following key services:

1. **Backend API Service**
   - Port: 31337 (HTTPS), 1337 (HTTP for CA cert)
   - Health endpoint: `/api/health`
   - Version endpoint: `/api/version`

2. **PostgreSQL Database**
   - Port: 5432
   - Connection pool status
   - Active connections

3. **WebSocket Service**
   - Agent connections
   - Heartbeat status
   - Connection count

### Docker Container Health

Monitor container status using Docker commands:

```bash
# Check container status
docker-compose ps

# Monitor resource usage
docker stats

# Check container logs
docker-compose logs -f backend
docker-compose logs -f postgres
docker-compose logs -f app
```

## Job Monitoring and Statistics

### Job Execution Metrics

The system tracks comprehensive job execution metrics:

1. **Job Status Distribution**
   - Pending jobs count
   - Running jobs count
   - Completed jobs count
   - Failed jobs count
   - Cancelled jobs count

2. **Job Performance Indicators**
   - Average job completion time
   - Job success rate
   - Hash cracking rate
   - Keyspace coverage

### Job Monitoring Endpoints

```bash
# List all jobs with pagination
GET /api/jobs?page=1&page_size=20

# Get specific job details
GET /api/jobs/{job_id}

# Get job statistics
GET /api/jobs/stats
```

### Job Progress Tracking

Monitor job progress through these metrics:

- **Dispatched Percentage**: Portion of keyspace distributed to agents
- **Searched Percentage**: Portion of keyspace actually processed
- **Overall Progress**: Combined metric considering rule splitting
- **Cracked Count**: Number of successfully cracked hashes
- **Total Speed**: Combined hash rate across all agents

### Enhanced Job Details View

The Job Details page now includes a comprehensive completed tasks history that provides administrators with:

#### Completed Tasks Monitoring
- **Historical Performance Data**: View hash rates achieved by each agent for completed tasks
- **Task Completion Patterns**: Analyze when and how tasks were completed across the job lifecycle
- **Keyspace Analysis**: Identify which keyspace ranges were most productive for password cracking
- **Audit Trail**: Maintain a complete record of job execution across distributed agents

#### Available Metrics
For each completed task, administrators can review:
- Agent assignment and performance metrics
- Exact keyspace ranges processed
- Completion timestamps for timeline analysis
- Number of cracks found per task
- Average hash rate achieved during execution

#### Use Cases for Administrators
- **Performance Optimization**: Compare agent efficiency across tasks to identify hardware or configuration issues
- **Resource Planning**: Analyze task distribution patterns to optimize future job configurations
- **Troubleshooting**: Identify if specific agents or keyspace ranges consistently underperform
- **Compliance Reporting**: Maintain detailed execution records for security audits

!!! note "Data Retention"
    Completed tasks are retained even after job completion, providing long-term visibility into job execution history. This data persists according to your configured retention policies.

### Database Queries for Job Monitoring

```sql
-- Active jobs by status
SELECT status, COUNT(*) as count 
FROM job_executions 
GROUP BY status;

-- Jobs with high failure rate
SELECT je.id, je.name, je.error_message,
       COUNT(jt.id) as total_tasks,
       SUM(CASE WHEN jt.status = 'failed' THEN 1 ELSE 0 END) as failed_tasks
FROM job_executions je
JOIN job_tasks jt ON je.id = jt.job_execution_id
WHERE je.status = 'failed'
GROUP BY je.id, je.name, je.error_message
HAVING SUM(CASE WHEN jt.status = 'failed' THEN 1 ELSE 0 END) > 0;

-- Job performance over time
SELECT 
    DATE_TRUNC('hour', created_at) as hour,
    COUNT(*) as jobs_created,
    AVG(EXTRACT(EPOCH FROM (completed_at - created_at))) as avg_duration_seconds
FROM job_executions
WHERE completed_at IS NOT NULL
GROUP BY hour
ORDER BY hour DESC;
```

## Agent Performance Metrics

### Agent Metrics Collection

The agent collects and reports the following metrics:

1. **System Metrics**
   - CPU usage percentage
   - Memory usage percentage
   - GPU utilization
   - GPU temperature
   - GPU memory usage

2. **Performance Metrics**
   - Hash rate per device
   - Power usage

![Device Monitoring Dashboard](../../assets/images/screenshots/device_monitoring_2.png)
*Device monitoring dashboard showing real-time temperature, utilization, fan speed, and hash rate metrics across multiple agents*

![Device Monitoring Alternative View](../../assets/images/screenshots/device_monitoring_2.png)
*Alternative view of the device monitoring interface with detailed performance graphs and timeline data*
   - Fan speed
   - Device temperature

### Agent Monitoring Endpoints

```bash
# List all agents
GET /api/admin/agents

# Get agent details with devices
GET /api/admin/agents/{agent_id}

# Get agent performance metrics
GET /api/admin/agents/{agent_id}/metrics?timeRange=1h&metrics=temperature,utilization,fanspeed,hashrate
```

### Agent Health Monitoring

Monitor agent health through:

1. **Heartbeat Status**
   - Last heartbeat timestamp
   - Connection status (active/inactive)
   - Heartbeat interval (30 seconds default)

2. **Error Tracking**
   - Last error message
   - Error frequency
   - Recovery status

## Job Scheduling Robustness (v1.2.1+)

### Agent Crash Recovery

Starting with version 1.2.1, the system includes automatic recovery from agent crashes with improved keyspace accounting:

**Problem Solved**: When an agent crashes while executing a task, the system now properly returns the unfinished work back to the pool for reassignment.

**How It Works**:
1. Agent crashes with task assigned (e.g., keyspace 0-100,000)
2. Backend detects disconnect and resets task to "pending"
3. **NEW**: `dispatched_keyspace` is decremented by the task's chunk size
4. Scheduler can now reassign the work to another agent
5. No manual intervention required

**Monitoring Recovery Operations**:

```sql
-- Check for jobs with recently reset tasks
SELECT
    je.id as job_id,
    je.name as job_name,
    je.status,
    je.dispatched_keyspace,
    je.total_keyspace,
    COUNT(CASE WHEN jt.status = 'pending'
               AND jt.keyspace_start IS NOT NULL
          THEN 1 END) as reset_tasks,
    COUNT(jt.id) as total_tasks
FROM job_executions je
JOIN job_tasks jt ON je.id = jt.job_execution_id
WHERE je.status IN ('running', 'pending')
GROUP BY je.id, je.name, je.status, je.dispatched_keyspace, je.total_keyspace
HAVING COUNT(CASE WHEN jt.status = 'pending'
                   AND jt.keyspace_start IS NOT NULL
              THEN 1 END) > 0;
```

**Expected Results:**
- Reset tasks should be reassigned within next scheduling cycle
- `dispatched_keyspace` should be less than `total_keyspace` when reset tasks exist
- No jobs should remain stuck with pending work that can't be assigned

**Validation Query**:
```sql
-- Verify keyspace accounting is correct
SELECT
    je.id,
    je.name,
    je.total_keyspace,
    je.dispatched_keyspace,
    COALESCE(SUM(CASE WHEN jt.status != 'pending'
                      THEN jt.keyspace_end - jt.keyspace_start
                      ELSE 0 END), 0) as actual_dispatched,
    (je.dispatched_keyspace - COALESCE(SUM(CASE WHEN jt.status != 'pending'
                                                  THEN jt.keyspace_end - jt.keyspace_start
                                                  ELSE 0 END), 0)) as keyspace_mismatch
FROM job_executions je
LEFT JOIN job_tasks jt ON je.id = jt.job_execution_id
WHERE je.status IN ('running', 'pending')
GROUP BY je.id, je.name, je.total_keyspace, je.dispatched_keyspace
HAVING ABS(je.dispatched_keyspace - COALESCE(SUM(CASE WHEN jt.status != 'pending'
                                                       THEN jt.keyspace_end - jt.keyspace_start
                                                       ELSE 0 END), 0)) > 1000;
```

**Interpretation:**
- `keyspace_mismatch` should be near zero (within 1000 for rounding)
- Large mismatches indicate potential accounting issues
- Empty results = healthy keyspace tracking

### Agent Crash Patterns

Track agent reliability:

```sql
-- Agents with frequent task resets (potential stability issues)
SELECT
    a.id,
    a.name,
    COUNT(CASE WHEN jt.retry_count > 0 THEN 1 END) as reset_count,
    COUNT(jt.id) as total_tasks,
    (COUNT(CASE WHEN jt.retry_count > 0 THEN 1 END)::float /
     NULLIF(COUNT(jt.id), 0) * 100) as reset_percentage
FROM agents a
LEFT JOIN job_tasks jt ON a.id = jt.agent_id
WHERE jt.created_at > NOW() - INTERVAL '7 days'
GROUP BY a.id, a.name
HAVING COUNT(CASE WHEN jt.retry_count > 0 THEN 1 END) > 5
ORDER BY reset_percentage DESC;
```

**Action Items:**
- Agents with >10% reset rate may have stability issues
- Check agent logs for crashes or disconnections
- Investigate hardware problems (overheating, power issues)
- Consider updating agent software if outdated

### Database Queries for Agent Monitoring

```sql
-- Agents with stale heartbeats
SELECT id, name, last_heartbeat, status
FROM agents
WHERE last_heartbeat < NOW() - INTERVAL '5 minutes'
  AND status = 'active';

-- Agent performance metrics
SELECT 
    a.name as agent_name,
    apm.metric_type,
    AVG(apm.value) as avg_value,
    MAX(apm.value) as max_value,
    MIN(apm.value) as min_value
FROM agents a
JOIN agent_performance_metrics apm ON a.id = apm.agent_id
WHERE apm.timestamp > NOW() - INTERVAL '1 hour'
GROUP BY a.name, apm.metric_type;

-- GPU device utilization
SELECT 
    a.name as agent_name,
    apm.device_name,
    apm.metric_type,
    AVG(apm.value) as avg_utilization
FROM agents a
JOIN agent_performance_metrics apm ON a.id = apm.agent_id
WHERE apm.metric_type = 'utilization'
  AND apm.timestamp > NOW() - INTERVAL '1 hour'
GROUP BY a.name, apm.device_name, apm.metric_type
ORDER BY avg_utilization DESC;
```

## Database Monitoring

### Connection Pool Monitoring

Monitor database connection health:

```sql
-- Active connections by state
SELECT state, COUNT(*) 
FROM pg_stat_activity 
GROUP BY state;

-- Long-running queries
SELECT 
    pid,
    now() - pg_stat_activity.query_start AS duration,
    query,
    state
FROM pg_stat_activity
WHERE (now() - pg_stat_activity.query_start) > interval '5 minutes';

-- Database size growth
SELECT 
    pg_database.datname,
    pg_size_pretty(pg_database_size(pg_database.datname)) AS size
FROM pg_database
ORDER BY pg_database_size(pg_database.datname) DESC;
```

### Table Statistics

Monitor table growth and performance:

```sql
-- Table sizes
SELECT
    schemaname AS table_schema,
    tablename AS table_name,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) AS size,
    pg_size_pretty(pg_relation_size(schemaname||'.'||tablename)) AS data_size
FROM pg_tables
WHERE schemaname = 'public'
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC;

-- Index usage
SELECT
    schemaname,
    tablename,
    indexname,
    idx_scan,
    idx_tup_read,
    idx_tup_fetch
FROM pg_stat_user_indexes
WHERE schemaname = 'public'
ORDER BY idx_scan DESC;
```

### Performance Metrics Tables

The system maintains dedicated tables for performance tracking:

1. **agent_metrics** - Real-time agent system metrics
2. **agent_performance_metrics** - Detailed performance data with aggregation
3. **job_performance_metrics** - Job execution performance tracking
4. **agent_benchmarks** - Hashcat benchmark results per agent

## Log Analysis and Alerting

### Log Configuration

Configure logging through environment variables:

```bash
# Enable debug logging
export DEBUG=true

# Set log level (DEBUG, INFO, WARNING, ERROR)
export LOG_LEVEL=INFO
```

### Log Locations

When running with Docker, logs are stored in:

```
/home/zerkereod/Programming/passwordCracking/kh-backend/logs/krakenhashes/
├── backend/      # Backend application logs
├── postgres/     # PostgreSQL logs
└── nginx/        # Nginx/frontend logs
```

### Log Format

The system uses structured logging with the following format:
```
[LEVEL] [TIMESTAMP] [FILE:LINE] [FUNCTION] MESSAGE
```

Example:
```
[INFO] [2025-08-01 15:04:05.000] [/path/to/file.go:42] [FunctionName] Processing job execution
```

### Key Log Patterns to Monitor

1. **Error Patterns**
   ```bash
   # Find all errors across logs
   grep -i "error" /home/zerkereod/Programming/passwordCracking/kh-backend/logs/krakenhashes/*/*.log
   
   # Find database connection errors
   grep -i "database.*error\|connection.*failed" logs/backend/*.log
   
   # Find agent disconnections
   grep -i "agent.*disconnect\|websocket.*close" logs/backend/*.log
   ```

2. **Performance Issues**
   ```bash
   # Find slow queries
   grep -i "slow query\|query took" logs/backend/*.log
   
   # Find memory issues
   grep -i "out of memory\|memory.*limit" logs/*/*.log
   ```

3. **Security Events**
   ```bash
   # Find authentication failures
   grep -i "auth.*fail\|login.*fail\|unauthorized" logs/backend/*.log
   
   # Find suspicious activity
   grep -i "invalid.*token\|forbidden\|suspicious" logs/backend/*.log
   ```

### Alert Configuration

Set up alerts for critical events:

1. **System Health Alerts**
   - Service down (health check fails)
   - Database connection pool exhausted
   - High error rate (>5% of requests)

2. **Performance Alerts**
   - CPU usage > 90% for 5 minutes
   - Memory usage > 85%
   - Database query time > 5 seconds
   - Job queue backlog > 100 jobs

3. **Security Alerts**
   - Multiple failed login attempts
   - Unauthorized API access attempts
   - Agent registration anomalies

## Performance Baselines

### Establishing Baselines

Monitor and document normal operating parameters:

1. **System Resource Baselines**
   - Normal CPU usage: 20-40% (idle), 60-80% (active jobs)
   - Memory usage: 2-4GB (base), +1GB per 1M hashes
   - Database connections: 10-20 (normal load)

2. **Job Performance Baselines**
   - Job creation rate: 10-50 jobs/hour
   - Average job duration: Varies by attack type
   - Hash processing rate: Device-dependent

3. **Agent Performance Baselines**
   - Heartbeat interval: 30 seconds
   - Benchmark cache duration: 24 hours
   - GPU utilization: 90-100% during jobs

### Benchmark Tracking

The system automatically tracks agent benchmarks:

```sql
-- View agent benchmarks
SELECT 
    a.name as agent_name,
    ab.attack_mode,
    ab.hash_type,
    ab.speed,
    ab.updated_at
FROM agents a
JOIN agent_benchmarks ab ON a.id = ab.agent_id
ORDER BY a.name, ab.attack_mode, ab.hash_type;
```

### Performance Degradation Detection

Monitor for performance degradation:

```sql
-- Compare current vs historical performance
WITH current_metrics AS (
    SELECT 
        agent_id,
        AVG(value) as current_avg
    FROM agent_performance_metrics
    WHERE metric_type = 'hash_rate'
      AND timestamp > NOW() - INTERVAL '1 hour'
    GROUP BY agent_id
),
historical_metrics AS (
    SELECT 
        agent_id,
        AVG(value) as historical_avg
    FROM agent_performance_metrics
    WHERE metric_type = 'hash_rate'
      AND timestamp BETWEEN NOW() - INTERVAL '1 week' AND NOW() - INTERVAL '1 day'
    GROUP BY agent_id
)
SELECT 
    a.name,
    cm.current_avg,
    hm.historical_avg,
    ((cm.current_avg - hm.historical_avg) / hm.historical_avg * 100) as percent_change
FROM agents a
JOIN current_metrics cm ON a.id = cm.agent_id
JOIN historical_metrics hm ON a.id = hm.agent_id
WHERE ABS((cm.current_avg - hm.historical_avg) / hm.historical_avg) > 0.1;
```

## Monitoring Dashboards and Tools

### Built-in Monitoring Endpoints

1. **System Status Dashboard**
   - Real-time agent status
   - Active job count
   - System resource usage
   - Recent errors

2. **Job Monitoring Dashboard**
   - Job queue status
   - Job progress tracking
   - Success/failure rates
   - Performance trends

3. **Agent Performance Dashboard**
   - Agent availability
   - Device utilization
   - Temperature monitoring
   - Hash rate tracking

### External Monitoring Integration

The system can be integrated with external monitoring tools:

1. **Prometheus Integration**
   - Export metrics via `/metrics` endpoint (if implemented)
   - Custom metric exporters
   - Alert manager integration

2. **Grafana Dashboards**
   - PostgreSQL data source
   - Custom dashboard templates
   - Alert visualization

3. **Log Aggregation**
   - ELK Stack (Elasticsearch, Logstash, Kibana)
   - Fluentd/Fluent Bit
   - Centralized log analysis

### Monitoring Best Practices

1. **Regular Health Checks**
   - Automated health check every 30 seconds
   - Alert on 3 consecutive failures
   - Include dependency checks

2. **Capacity Planning**
   - Monitor growth trends
   - Plan for peak usage
   - Scale resources proactively

3. **Performance Optimization**
   - Regular benchmark updates
   - Query optimization based on metrics
   - Resource allocation tuning

4. **Security Monitoring**
   - Audit log analysis
   - Anomaly detection
   - Access pattern monitoring

### Troubleshooting Guide

Common issues and monitoring approaches:

1. **High CPU Usage**
   - Check active job count
   - Verify agent task distribution
   - Monitor database query performance

2. **Memory Leaks**
   - Track memory usage over time
   - Identify growing processes
   - Check for unclosed connections

3. **Slow Job Processing**
   - Verify agent benchmarks
   - Check network latency
   - Monitor file I/O performance

4. **Database Performance**
   - Analyze slow queries
   - Check index usage
   - Monitor connection pool

## Maintenance and Cleanup

### Automated Cleanup Services

The system includes several cleanup services:

1. **Metrics Cleanup Service**
   - Aggregates real-time metrics to daily/weekly
   - Removes old metrics based on retention policy
   - Runs automatically on schedule

2. **Agent Cleanup Service**
   - Marks stale agents as inactive
   - Cleans up orphaned resources
   - Maintains agent health status

3. **Job Cleanup Service**
   - Archives completed jobs
   - Removes temporary files
   - Updates job statistics

### Manual Maintenance Tasks

```bash
# Force cleanup of old metrics
curl -X POST https://localhost:31337/api/admin/force-cleanup

# Vacuum database
docker exec -it krakenhashes_postgres_1 psql -U postgres -d krakenhashes -c "VACUUM ANALYZE;"

# Check database bloat
docker exec -it krakenhashes_postgres_1 psql -U postgres -d krakenhashes -c "
SELECT 
    schemaname,
    tablename,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) AS size
FROM pg_tables
WHERE schemaname = 'public'
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC
LIMIT 10;"
```

## Conclusion

Effective monitoring is crucial for maintaining a healthy KrakenHashes deployment. Regular monitoring of system health, job performance, and agent metrics ensures optimal operation and early detection of issues. Implement automated alerting for critical metrics and maintain historical data for trend analysis and capacity planning.