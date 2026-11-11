# Agent Monitoring Guide

!!! info "See also"
    Server-wide health and metrics are covered in [Admin guide → System monitoring](../admin-guide/operations/monitoring.md).

This comprehensive guide covers monitoring distributed agents in KrakenHashes, including real-time metrics, health checks, performance monitoring, and troubleshooting strategies for administrators managing agent fleets.

## Table of Contents

1. [Overview](#overview)
2. [Real-time Agent Status](#real-time-agent-status)
3. [Heartbeat and Connection Monitoring](#heartbeat-and-connection-monitoring)
4. [Metrics Collection and Monitoring](#metrics-collection-and-monitoring)
5. [Device Performance Monitoring](#device-performance-monitoring)
6. [Agent Health Indicators](#agent-health-indicators)
7. [Multi-Agent Fleet Monitoring](#multi-agent-fleet-monitoring)
8. [Status Indicators and States](#status-indicators-and-states)
9. [Performance Analysis and Trends](#performance-analysis-and-trends)
10. [Troubleshooting with Monitoring Data](#troubleshooting-with-monitoring-data)
11. [Best Practices](#best-practices)

## Overview

KrakenHashes provides comprehensive monitoring capabilities for distributed agents, enabling administrators to track agent health, performance, and operational status across their entire fleet. The monitoring system includes real-time metrics collection, WebSocket-based status reporting, and detailed performance analytics.

### Key Monitoring Features

- **Real-time Agent Status**: Live connection status and heartbeat monitoring
- **Device Metrics**: GPU temperature, utilization, fan speed, and hash rate tracking
- **WebSocket Communication**: Persistent connections with automatic reconnection
- **Performance Analytics**: Historical data and trend analysis
- **Multi-Agent Dashboard**: Fleet-wide visibility and management
- **Automated Health Checks**: Connection validation and failure detection

## Real-time Agent Status

### Agent Status Overview

The system continuously monitors agent status through multiple channels:

```
Active Agents: Online and ready for work
├── Connected: WebSocket connection established
├── Idle: Available for task assignment
├── Busy: Currently executing tasks
└── Reconnecting: Temporary disconnection with recovery in progress

Inactive Agents: Not currently operational
├── Offline: No recent heartbeat or connection
├── Disabled: Administratively disabled
├── Error: Failed connection or system error
└── Pending: Newly registered, awaiting first connection
```

### Agent Connection States

The monitoring system tracks detailed connection states:

| State | Description | Color Indicator | Action Required |
|-------|-------------|----------------|----------------|
| `active` | Connected and operational | 🟢 Green | None |
| `inactive` | Disconnected or offline | 🔴 Red | Check agent service |
| `pending` | Registration in progress | 🟡 Yellow | Wait for completion |
| `disabled` | Administratively disabled | ⚫ Gray | Manual re-enable |
| `error` | System or hardware error | 🔴 Red | Investigate error |

### Agent Dashboard View

Access the agent monitoring dashboard at:
- **Frontend**: `https://your-server:31337/agents`
- **Agent Details**: `https://your-server:31337/agents/{agent_id}`

The dashboard provides:
- Real-time connection status
- Last activity timestamps  
- Device configuration and status
- Performance metrics and charts
- Task assignment history

## Heartbeat and Connection Monitoring

### WebSocket Heartbeat System

KrakenHashes uses a robust WebSocket-based heartbeat system for monitoring agent connectivity:

```
Backend ←→ Agent WebSocket Connection
├── Ping/Pong Messages: Every 54 seconds (configurable)
├── Agent Status Updates: Every 60 seconds
├── Heartbeat Timeout: 60 seconds maximum
└── Automatic Reconnection: Exponential backoff (1s to 30s)
```

### Connection Timing Configuration

The system uses configurable timing parameters:

```bash
# Backend WebSocket Settings (environment variables)
KH_WRITE_WAIT=10s      # Write operation timeout
KH_PONG_WAIT=60s       # Pong response timeout  
KH_PING_PERIOD=54s     # Ping interval

# Agent automatically fetches these from backend
# No manual configuration required
```

### Heartbeat Monitoring Queries

Monitor agent heartbeat status using database queries:

```sql
-- Agents with recent heartbeats (last 5 minutes)
SELECT 
    id,
    name,
    status,
    last_heartbeat,
    EXTRACT(EPOCH FROM (NOW() - last_heartbeat)) AS seconds_since_heartbeat
FROM agents 
WHERE last_heartbeat > NOW() - INTERVAL '5 minutes'
ORDER BY last_heartbeat DESC;

-- Agents with stale heartbeats (potential issues)
SELECT 
    id,
    name, 
    status,
    last_heartbeat,
    EXTRACT(EPOCH FROM (NOW() - last_heartbeat)) AS seconds_since_heartbeat
FROM agents
WHERE last_heartbeat < NOW() - INTERVAL '5 minutes'
  AND status = 'active'
ORDER BY last_heartbeat ASC;

-- Connection status distribution
SELECT status, COUNT(*) as count
FROM agents
GROUP BY status
ORDER BY count DESC;
```

## Metrics Collection and Monitoring

### System Metrics Collection

Agents automatically collect and report system metrics:

#### CPU and Memory Metrics
- **CPU Usage**: Overall processor utilization percentage
- **Memory Usage**: System memory utilization percentage
- **Collection Interval**: 5 seconds (configurable)
- **Data Retention**: Based on monitoring settings

#### Agent Metrics Structure
```go
type MetricsData struct {
    AgentID     int                `json:"agent_id"`
    CollectedAt time.Time          `json:"collected_at"`
    CPUs        []CPUMetrics       `json:"cpus"`
    GPUs        []GPUMetrics       `json:"gpus"`
    Memory      MemoryMetrics      `json:"memory"`
    Disk        []DiskMetrics      `json:"disk"`
    Network     []NetworkMetrics   `json:"network"`
    Process     []ProcessMetrics   `json:"process"`
}
```

### GPU Metrics Integration

GPU metrics are obtained from hashcat's JSON status output during job execution:

- **GPU Utilization**: Device usage percentage
- **GPU Temperature**: Operating temperature in Celsius
- **GPU Memory**: Memory utilization
- **Power Usage**: Power consumption in watts
- **Hash Rate**: Real-time hashing performance

### Metrics Storage and Retention

The system implements a cascading retention policy:

```
Real-time Metrics (Fine-grained)
├── Retention: 7 days (configurable)  
├── Resolution: 5-second intervals
└── Use: Recent activity monitoring

Daily Aggregates (Medium-term)
├── Retention: 30 days (configurable)
├── Resolution: Daily summaries
└── Use: Performance analysis

Weekly Aggregates (Long-term)  
├── Retention: 365 days (configurable)
├── Resolution: Weekly summaries
└── Use: Historical trends
```

### Monitoring Settings Configuration

Configure metrics retention through the admin interface:

```bash
# Access monitoring settings
https://your-server:31337/admin/monitoring

# Available settings:
- Real-time Data Retention: 7 days
- Daily Aggregates Retention: 30 days  
- Weekly Aggregates Retention: 365 days
- Enable Aggregation: true
- Aggregation Interval: daily
```

## Device Performance Monitoring

### Real-time Device Metrics

The Agent Details page provides comprehensive device monitoring with live charts:

<!-- Device Monitoring Dashboard image placeholder -->

### Available Device Charts

1. **Temperature Monitoring**
   - Real-time GPU temperature tracking
   - Temperature threshold alerts
   - Historical temperature trends
   - Multi-device comparison

2. **Utilization Tracking**
   - GPU utilization percentage
   - Device workload distribution
   - Efficiency analysis
   - Performance optimization insights

3. **Fan Speed Monitoring**
   - Cooling system performance
   - Fan curve analysis
   - Thermal management tracking
   - Hardware health indicators

4. **Hash Rate Performance**
   - Real-time hashing performance
   - Per-device contribution
   - Cumulative hash rate
   - Performance benchmarking

### Chart Configuration Options

```typescript
// Time Range Options
const timeRanges = [
    '10m',  // 10 minutes
    '20m',  // 20 minutes  
    '1h',   // 1 hour
    '5h',   // 5 hours
    '24h'   // 24 hours
];

// Metric Types
const metricTypes = [
    'temperature',  // GPU temperature in °C
    'utilization',  // GPU utilization %
    'fanspeed',     // Fan speed %
    'hashrate'      // Hash rate (varies by algorithm)
];
```

### Device Metrics API Endpoints

```bash
# Get device metrics for agent
GET /api/agents/{agent_id}/metrics?timeRange=1h&metrics=temperature,utilization,fanspeed,hashrate

# Response format
{
    "devices": [
        {
            "deviceId": 0,
            "deviceName": "NVIDIA RTX 4090",
            "metrics": {
                "temperature": [
                    {"timestamp": 1640995200000, "value": 65.0},
                    {"timestamp": 1640995205000, "value": 67.2}
                ],
                "utilization": [
                    {"timestamp": 1640995200000, "value": 98.5},
                    {"timestamp": 1640995205000, "value": 99.1}
                ]
            }
        }
    ]
}
```

### Device Enable/Disable Monitoring

Monitor device status changes through the interface:

```bash
# Update device status
PUT /api/agents/{agent_id}/devices/{device_id}
{
    "enabled": true
}

# Monitor device state changes in logs
grep -i "device.*update.*enabled" logs/backend/*.log
```

## Agent Health Indicators

### Connection Health Metrics

Monitor agent connection health through multiple indicators:

#### Connection Status Indicators
- **WebSocket State**: Connected/Disconnected
- **Last Heartbeat**: Timestamp of last communication
- **Response Time**: WebSocket ping-pong latency
- **Reconnection Attempts**: Failed connection retry count

#### System Health Indicators  
- **CPU Load**: System processor utilization
- **Memory Usage**: Available system memory
- **Disk Space**: Storage availability
- **Network Latency**: Communication delays

#### Hardware Health Indicators
- **GPU Temperature**: Thermal status and limits
- **GPU Utilization**: Device workload efficiency
- **Power Consumption**: Electrical usage monitoring
- **Fan Performance**: Cooling system operation

### Agent Status Reporting

Agents automatically report detailed status information:

```json
{
    "status": "active",
    "version": "v0.15.7",  
    "updated_at": "2025-09-11T10:30:00Z",
    "environment": {
        "os": "linux",
        "arch": "amd64",
        "hostname": "worker-01"
    },
    "os_info": {
        "platform": "linux",
        "hostname": "worker-01", 
        "os_name": "Ubuntu",
        "os_version": "22.04.3 LTS",
        "kernel_version": "Linux version 6.5.0",
        "go_version": "go1.21.0"
    }
}
```

### Health Check Queries

```sql
-- Agent health summary
SELECT 
    a.name,
    a.status,
    a.last_heartbeat,
    a.version,
    CASE 
        WHEN a.last_heartbeat > NOW() - INTERVAL '2 minutes' THEN 'Healthy'
        WHEN a.last_heartbeat > NOW() - INTERVAL '5 minutes' THEN 'Warning'
        ELSE 'Critical'
    END as health_status,
    COUNT(ad.id) as device_count,
    SUM(CASE WHEN ad.enabled THEN 1 ELSE 0 END) as enabled_devices
FROM agents a
LEFT JOIN agent_devices ad ON a.id = ad.agent_id
GROUP BY a.id, a.name, a.status, a.last_heartbeat, a.version
ORDER BY a.last_heartbeat DESC;

-- Agents with hardware issues
SELECT 
    a.name,
    ad.device_name,
    apm.metric_type,
    apm.value,
    apm.timestamp
FROM agents a
JOIN agent_devices ad ON a.id = ad.agent_id
JOIN agent_performance_metrics apm ON ad.agent_id = apm.agent_id
WHERE (apm.metric_type = 'temperature' AND apm.value > 85)
   OR (apm.metric_type = 'utilization' AND apm.value < 50)
ORDER BY apm.timestamp DESC;
```

## Multi-Agent Fleet Monitoring

### Fleet Overview Dashboard

Monitor your entire agent fleet from the main agents page:

```bash
# Access fleet monitoring
https://your-server:31337/agents

# Key fleet metrics:
- Total Agents: Active + Inactive count
- Agent Distribution: By status and location  
- Hardware Summary: Total GPU count and types
- Performance Metrics: Combined hash rates
- Health Status: Overall fleet health
```

### Fleet Status Categories

#### Active Agents
- **Online and Ready**: Available for job assignment
- **Busy**: Currently executing tasks
- **Idle**: Connected but not actively working

#### Inactive Agents  
- **Offline**: No recent heartbeat
- **Disabled**: Administratively disabled
- **Error State**: Hardware or connection issues

#### Pending Agents
- **Registering**: New agent setup in progress
- **Authenticating**: Certificate and API key validation

### Fleet-wide Monitoring Queries

```sql
-- Fleet status summary
SELECT 
    status,
    COUNT(*) as agent_count,
    ROUND(COUNT(*) * 100.0 / SUM(COUNT(*)) OVER(), 2) as percentage
FROM agents
GROUP BY status
ORDER BY agent_count DESC;

-- Fleet hardware summary
SELECT 
    ad.device_type,
    COUNT(DISTINCT a.id) as agents_with_device,
    COUNT(ad.id) as total_devices,
    SUM(CASE WHEN ad.enabled THEN 1 ELSE 0 END) as enabled_devices
FROM agents a
JOIN agent_devices ad ON a.id = ad.agent_id
GROUP BY ad.device_type
ORDER BY total_devices DESC;

-- Fleet performance overview
SELECT 
    COUNT(DISTINCT a.id) as total_agents,
    COUNT(DISTINCT CASE WHEN a.status = 'active' THEN a.id END) as active_agents,
    AVG(CASE WHEN apm.metric_type = 'utilization' THEN apm.value END) as avg_gpu_utilization,
    AVG(CASE WHEN apm.metric_type = 'temperature' THEN apm.value END) as avg_gpu_temperature
FROM agents a
LEFT JOIN agent_performance_metrics apm ON a.id = apm.agent_id
WHERE apm.timestamp > NOW() - INTERVAL '1 hour';
```

### Geographic and Organizational Fleet Monitoring

```sql
-- Agents by network location (using IP metadata)
SELECT 
    SUBSTRING(metadata->>'ipAddress', 1, 
              POSITION('.' IN metadata->>'ipAddress' || '.')) as network_prefix,
    COUNT(*) as agent_count,
    COUNT(CASE WHEN status = 'active' THEN 1 END) as active_count
FROM agents
WHERE metadata ? 'ipAddress'
GROUP BY network_prefix
ORDER BY agent_count DESC;

-- Agents by owner (team/user assignments)
SELECT 
    COALESCE(u.username, 'Unassigned') as owner,
    COUNT(a.id) as agent_count,
    COUNT(CASE WHEN a.status = 'active' THEN 1 END) as active_count
FROM agents a
LEFT JOIN users u ON a.owner_id = u.id  
GROUP BY u.username
ORDER BY agent_count DESC;
```

## Status Indicators and States

### Agent Status State Machine

```
[pending] → [active] → [inactive]
    ↓         ↓           ↓
[error]   [busy/idle]  [disabled]
    ↓         ↓           ↓
[active]  [active]   [active]
```

### Detailed Status Descriptions

| Status | Description | Typical Causes | Recovery Actions |
|--------|-------------|----------------|------------------|
| `pending` | New agent registration | First-time setup | Wait for completion |
| `active` | Fully operational | Normal state | None required |
| `busy` | Executing tasks | Job assignment | Monitor progress |
| `idle` | Connected, available | Between jobs | None required |
| `inactive` | Disconnected | Network/service issues | Restart agent service |
| `disabled` | Manually disabled | Admin action | Re-enable through UI |
| `error` | System/hardware error | Hardware failure, config error | Check logs, fix issues |

### Status Transition Triggers

#### Automatic Transitions
- **pending → active**: Successful device detection and registration
- **active → inactive**: Heartbeat timeout (>5 minutes)
- **active → error**: Device detection failure or system error
- **error → active**: Successful reconnection after error resolution

#### Manual Transitions  
- **Any → disabled**: Administrative disable action
- **disabled → active**: Administrative enable action
- **Any → active**: Force status change through API

### Visual Status Indicators

The web interface uses consistent visual indicators:

```css
/* Status indicator colors */
.status-active     { color: #4caf50; }  /* Green */
.status-inactive   { color: #f44336; }  /* Red */
.status-pending    { color: #ff9800; }  /* Orange */
.status-disabled   { color: #9e9e9e; }  /* Gray */
.status-error      { color: #f44336; }  /* Red */
```

## Performance Analysis and Trends

### Historical Performance Tracking

The system maintains comprehensive historical performance data for trend analysis:

#### Performance Metrics Database Schema

```sql
-- Agent performance metrics table structure
CREATE TABLE agent_performance_metrics (
    id SERIAL PRIMARY KEY,
    agent_id INTEGER REFERENCES agents(id),
    device_name VARCHAR(255),
    metric_type VARCHAR(50),  -- 'temperature', 'utilization', 'fanspeed', 'hashrate'
    value NUMERIC(10,2),
    timestamp TIMESTAMP DEFAULT NOW()
);

-- Benchmark results tracking
CREATE TABLE agent_benchmarks (
    id SERIAL PRIMARY KEY,
    agent_id INTEGER REFERENCES agents(id),
    attack_mode INTEGER,
    hash_type INTEGER, 
    speed BIGINT,
    device_speeds JSONB,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);
```

### Performance Analysis Queries

```sql
-- Agent performance trends over time
SELECT 
    DATE_TRUNC('hour', timestamp) as hour,
    AVG(CASE WHEN metric_type = 'utilization' THEN value END) as avg_utilization,
    AVG(CASE WHEN metric_type = 'temperature' THEN value END) as avg_temperature,
    AVG(CASE WHEN metric_type = 'hashrate' THEN value END) as avg_hashrate
FROM agent_performance_metrics
WHERE agent_id = $1
  AND timestamp > NOW() - INTERVAL '24 hours'
GROUP BY hour
ORDER BY hour;

-- Top performing agents by hash rate
SELECT 
    a.name,
    AVG(apm.value) as avg_hashrate,
    MAX(apm.value) as peak_hashrate,
    COUNT(apm.id) as measurement_count
FROM agents a
JOIN agent_performance_metrics apm ON a.id = apm.agent_id
WHERE apm.metric_type = 'hashrate'
  AND apm.timestamp > NOW() - INTERVAL '1 week'
GROUP BY a.id, a.name
ORDER BY avg_hashrate DESC
LIMIT 10;

-- Performance degradation detection
WITH recent_performance AS (
    SELECT 
        agent_id,
        AVG(value) as recent_avg
    FROM agent_performance_metrics
    WHERE metric_type = 'hashrate'
      AND timestamp > NOW() - INTERVAL '24 hours'
    GROUP BY agent_id
),
baseline_performance AS (
    SELECT 
        agent_id,
        AVG(value) as baseline_avg
    FROM agent_performance_metrics  
    WHERE metric_type = 'hashrate'
      AND timestamp BETWEEN NOW() - INTERVAL '1 week' AND NOW() - INTERVAL '2 days'
    GROUP BY agent_id
)
SELECT 
    a.name,
    r.recent_avg,
    b.baseline_avg,
    ROUND(((r.recent_avg - b.baseline_avg) / b.baseline_avg * 100), 2) as percent_change
FROM agents a
JOIN recent_performance r ON a.id = r.agent_id
JOIN baseline_performance b ON a.id = b.agent_id
WHERE ABS((r.recent_avg - b.baseline_avg) / b.baseline_avg) > 0.15  -- >15% change
ORDER BY percent_change;
```

### Benchmark Performance Tracking

Monitor agent benchmark performance over time:

```sql
-- Benchmark history for agent
SELECT 
    attack_mode,
    hash_type,
    speed,
    created_at,
    LAG(speed) OVER (PARTITION BY attack_mode, hash_type ORDER BY created_at) as previous_speed,
    speed - LAG(speed) OVER (PARTITION BY attack_mode, hash_type ORDER BY created_at) as speed_change
FROM agent_benchmarks
WHERE agent_id = $1
ORDER BY created_at DESC;

-- Fleet benchmark comparison
SELECT 
    a.name,
    ab.attack_mode,
    ab.hash_type,
    ab.speed,
    RANK() OVER (PARTITION BY ab.attack_mode, ab.hash_type ORDER BY ab.speed DESC) as rank
FROM agents a
JOIN agent_benchmarks ab ON a.id = ab.agent_id
WHERE ab.updated_at > NOW() - INTERVAL '1 week'
ORDER BY ab.attack_mode, ab.hash_type, ab.speed DESC;
```

## Troubleshooting with Monitoring Data

### Common Issues and Diagnostic Approaches

#### 1. Agent Connection Issues

**Symptoms:**
- Agent status shows "inactive" or "error"
- Missing from active agents list
- Stale heartbeat timestamps

**Diagnostic Steps:**
```sql
-- Check agent connection history
SELECT 
    id,
    name,
    status,
    last_heartbeat,
    last_error,
    EXTRACT(EPOCH FROM (NOW() - last_heartbeat)) as seconds_offline
FROM agents
WHERE name = 'problem-agent'
OR id = 123;

-- Check for recent WebSocket errors
```

**Log Analysis:**
```bash
# Check agent logs for connection issues
grep -i "connection\|websocket\|heartbeat" /path/to/agent.log

# Check backend logs for agent-related errors
grep -i "agent.*error\|websocket.*close\|connection.*failed" logs/backend/*.log

# Look for certificate or authentication issues
grep -i "certificate\|auth.*fail\|tls.*error" logs/backend/*.log
```

#### 2. Performance Degradation

**Symptoms:**
- Decreased hash rates
- Higher GPU temperatures
- Reduced utilization

**Diagnostic Queries:**
```sql
-- Performance comparison (current vs historical)
WITH current_perf AS (
    SELECT AVG(value) as current_hashrate
    FROM agent_performance_metrics
    WHERE agent_id = $1 
      AND metric_type = 'hashrate'
      AND timestamp > NOW() - INTERVAL '1 hour'
),
historical_perf AS (
    SELECT AVG(value) as historical_hashrate
    FROM agent_performance_metrics
    WHERE agent_id = $1
      AND metric_type = 'hashrate' 
      AND timestamp BETWEEN NOW() - INTERVAL '1 week' AND NOW() - INTERVAL '1 day'
)
SELECT 
    c.current_hashrate,
    h.historical_hashrate,
    ((c.current_hashrate - h.historical_hashrate) / h.historical_hashrate * 100) as percent_change
FROM current_perf c, historical_perf h;

-- Temperature analysis
SELECT 
    DATE_TRUNC('hour', timestamp) as hour,
    AVG(value) as avg_temp,
    MAX(value) as max_temp,
    COUNT(*) as measurements
FROM agent_performance_metrics
WHERE agent_id = $1
  AND metric_type = 'temperature'
  AND timestamp > NOW() - INTERVAL '24 hours'
GROUP BY hour
ORDER BY hour DESC;
```

#### 3. Hardware Health Issues  

**Symptoms:**
- High GPU temperatures (>85°C)
- Fan speed abnormalities
- Utilization inconsistencies

**Monitoring Approach:**
```sql
-- Hardware health check
SELECT 
    device_name,
    metric_type,
    value,
    timestamp,
    CASE 
        WHEN metric_type = 'temperature' AND value > 85 THEN 'CRITICAL'
        WHEN metric_type = 'temperature' AND value > 75 THEN 'WARNING'
        WHEN metric_type = 'utilization' AND value < 80 THEN 'LOW_UTIL'
        WHEN metric_type = 'fanspeed' AND value > 90 THEN 'HIGH_FAN'
        ELSE 'NORMAL'
    END as status
FROM agent_performance_metrics
WHERE agent_id = $1
  AND timestamp > NOW() - INTERVAL '1 hour'
  AND (
    (metric_type = 'temperature' AND value > 75)
    OR (metric_type = 'utilization' AND value < 80)
    OR (metric_type = 'fanspeed' AND value > 90)
  )
ORDER BY timestamp DESC;
```

#### 4. Task Assignment Issues

**Symptoms:**
- Agents remain idle during jobs
- Uneven task distribution
- Tasks stuck in "reconnect_pending"

**Diagnostic Queries:**
```sql
-- Check agent busy status and current tasks
SELECT 
    a.name,
    a.status,
    a.metadata->>'busy_status' as busy_status,
    a.metadata->>'current_task_id' as current_task,
    jt.status as task_status,
    je.name as job_name
FROM agents a
LEFT JOIN job_tasks jt ON jt.id = a.metadata->>'current_task_id'  
LEFT JOIN job_executions je ON jt.job_execution_id = je.id
WHERE a.id = $1;

-- Check for reconnect_pending tasks
SELECT 
    jt.id,
    jt.status,
    jt.agent_id,
    a.name,
    jt.keyspace_start,
    jt.keyspace_end,
    jt.updated_at
FROM job_tasks jt
JOIN agents a ON jt.agent_id = a.id
WHERE jt.status = 'reconnect_pending'
ORDER BY jt.updated_at DESC;
```

### Log Analysis Techniques

#### Structured Log Search

```bash
# Find connection events for specific agent
grep -i "agent.*123\|Agent 123" logs/backend/*.log | grep -i "connect"

# Track WebSocket message flow
grep -i "websocket\|message.*type" logs/backend/*.log | tail -50

# Monitor heartbeat activity
grep -i "heartbeat\|ping\|pong" logs/backend/*.log | tail -20

# Check for error patterns
grep -i "error\|fail\|timeout" logs/backend/*.log | grep -i "agent" | tail -10
```

#### Performance Issue Detection

```bash  
# Find performance-related messages
grep -i "performance\|slow\|timeout\|benchmark" logs/backend/*.log

# Check for resource issues
grep -i "memory\|cpu\|disk\|resource" logs/backend/*.log

# Monitor cleanup operations
grep -i "cleanup\|maintenance\|retention" logs/backend/*.log
```

### Automated Issue Detection

Set up monitoring alerts for common issues:

```bash
# Script: monitor_agents.sh
#!/bin/bash

# Check for agents with stale heartbeats
STALE_AGENTS=$(psql -t -c "SELECT COUNT(*) FROM agents WHERE last_heartbeat < NOW() - INTERVAL '5 minutes' AND status = 'active';")

if [ "$STALE_AGENTS" -gt 0 ]; then
    echo "ALERT: $STALE_AGENTS agents have stale heartbeats"
    # Send notification
fi

# Check for high GPU temperatures
HOT_GPUS=$(psql -t -c "SELECT COUNT(*) FROM agent_performance_metrics WHERE metric_type = 'temperature' AND value > 85 AND timestamp > NOW() - INTERVAL '5 minutes';")

if [ "$HOT_GPUS" -gt 0 ]; then
    echo "ALERT: $HOT_GPUS GPUs running hot (>85°C)"
    # Send notification
fi

# Check for agents with no enabled devices
NO_DEVICE_AGENTS=$(psql -t -c "SELECT COUNT(DISTINCT a.id) FROM agents a LEFT JOIN agent_devices ad ON a.id = ad.agent_id WHERE a.status = 'active' AND NOT EXISTS (SELECT 1 FROM agent_devices ad2 WHERE ad2.agent_id = a.id AND ad2.enabled = true);")

if [ "$NO_DEVICE_AGENTS" -gt 0 ]; then
    echo "ALERT: $NO_DEVICE_AGENTS active agents have no enabled devices"
    # Send notification  
fi
```

## Best Practices

### Monitoring Strategy

#### 1. Proactive Monitoring
- **Set up automated alerts** for critical metrics (heartbeat failures, high temperatures, low utilization)
- **Establish performance baselines** for each agent to detect degradation
- **Monitor trends** rather than just current values
- **Use multiple monitoring approaches** (real-time dashboard + historical analysis)

#### 2. Alert Thresholds
```bash
# Recommended alert thresholds:
Agent Heartbeat: > 5 minutes offline
GPU Temperature: > 85°C sustained  
GPU Utilization: < 50% during jobs
Connection Failures: > 3 consecutive failures
Performance Degradation: > 20% decrease from baseline
```

#### 3. Regular Health Checks
- **Daily**: Review agent status dashboard, check for offline agents
- **Weekly**: Analyze performance trends, identify degradation patterns  
- **Monthly**: Review hardware utilization, plan capacity changes
- **Quarterly**: Update performance baselines, optimize configurations

### Operational Best Practices

#### 1. Fleet Management
- **Group agents** by location, hardware type, or team assignment
- **Use naming conventions** that reflect agent purpose and location
- **Document agent configurations** including hardware specs and special parameters
- **Maintain agent inventory** with ownership and responsibility assignments

#### 2. Performance Optimization
- **Monitor benchmark results** and update when hardware changes
- **Balance task distribution** across agents based on capability
- **Track device utilization** to identify underused resources
- **Optimize extra parameters** for each agent's hardware configuration

#### 3. Maintenance Scheduling  
- **Plan maintenance windows** during low-activity periods
- **Coordinate updates** to minimize impact on running jobs
- **Test configuration changes** on non-critical agents first
- **Document maintenance activities** and their impact on performance

### Monitoring Data Retention

#### 1. Storage Management
```sql
-- Configure retention policies based on needs:
Real-time metrics: 7-14 days (high-frequency data)
Daily aggregates: 30-90 days (performance analysis)  
Weekly aggregates: 1-2 years (long-term trends)
Benchmark results: Indefinite (configuration reference)
```

#### 2. Data Cleanup Automation
- **Enable automatic aggregation** to reduce storage requirements
- **Monitor database growth** and adjust retention as needed
- **Archive historical data** for compliance or analysis needs
- **Use monitoring settings UI** to adjust retention policies

### Security and Access Control

#### 1. Monitoring Access
- **Restrict monitoring access** to appropriate administrators
- **Use role-based permissions** for different monitoring functions
- **Audit monitoring activities** and configuration changes
- **Secure monitoring endpoints** with proper authentication

#### 2. Agent Communication Security
- **Monitor certificate status** and renewal schedules
- **Track authentication failures** and suspicious activity
- **Use secure WebSocket connections** (WSS) in production
- **Regularly rotate API keys** and monitor key usage

### Disaster Recovery and Continuity

#### 1. Monitoring System Availability
- **Implement monitoring redundancy** to avoid single points of failure
- **Backup monitoring configurations** and historical data
- **Test monitoring system recovery** procedures
- **Document escalation procedures** for monitoring system failures

#### 2. Agent Recovery Procedures
- **Automate agent reconnection** with exponential backoff
- **Implement graceful degradation** when agents are offline
- **Buffer critical messages** during disconnections for recovery
- **Track task recovery** and automatic redistribution

This comprehensive monitoring guide enables administrators to effectively manage distributed KrakenHashes agent fleets, ensuring optimal performance, rapid issue detection, and reliable operation across diverse hardware configurations and network environments.