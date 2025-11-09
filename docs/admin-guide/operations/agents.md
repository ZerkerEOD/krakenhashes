# Agent Management Guide

This guide covers the comprehensive management of KrakenHashes agents, including registration, monitoring, scheduling, and troubleshooting.

## Table of Contents

1. [Understanding Agents](#understanding-agents)
2. [Agent Registration Process](#agent-registration-process)
3. [Managing Agent Connections](#managing-agent-connections)
4. [Monitoring Agent Health and Performance](#monitoring-agent-health-and-performance)
5. [Agent Scheduling and Availability](#agent-scheduling-and-availability)
6. [Hardware Capabilities and Benchmarks](#hardware-capabilities-and-benchmarks)
7. [Troubleshooting Agent Issues](#troubleshooting-agent-issues)

## Understanding Agents

### What are Agents?

Agents are distributed compute nodes that execute password cracking tasks using hashcat. They connect to the KrakenHashes backend server via WebSocket and receive job assignments based on their availability and capabilities.

### Agent Architecture

Each agent consists of:
- **Hardware Detection**: Automatic detection of GPUs (NVIDIA, AMD, Intel) and CPUs
- **Performance Monitoring**: Real-time tracking of resource utilization
- **File Synchronization**: Automatic download of wordlists, rules, and hashlists
- **Job Execution**: Running hashcat with specified attack parameters
- **Result Reporting**: Real-time crack updates back to the server

### Agent States

Agents can be in one of the following states:

- **`pending`**: Initial registration state, awaiting activation
- **`active`**: Connected and ready to receive jobs
- **`inactive`**: Disconnected but previously active
- **`error`**: Experiencing issues preventing normal operation
- **`disabled`**: Administratively disabled

## Agent Registration Process

### Overview

Agent registration uses a claim code (voucher) system to ensure only authorized agents can join the system.

### Creating Claim Codes

1. **Navigate to Admin Panel**
   - Go to Settings → Agent Management → Claim Codes

2. **Generate New Claim Code**
   ```
   Type: Single-use or Continuous
   Owner: Select user who will own the agents
   ```

3. **Claim Code Types**
   - **Single-use**: Can only be used once to register one agent
   - **Continuous**: Can be used multiple times (useful for auto-scaling)

### Agent Registration Steps

1. **Agent Installation**
   ```bash
   # On the agent machine
   ./krakenhashes-agent install
   ```

2. **Initial Registration**
   ```bash
   # Using the claim code
   ./krakenhashes-agent --host your-server:31337 --claim XXXX-XXXX-XXXX
   ```

3. **Certificate Download**
   - Agent automatically downloads TLS certificates
   - Stores credentials in `~/.krakenhashes/agent/`

4. **Connection Establishment**
   - Agent connects via WebSocket using API key authentication
   - Sends hardware information and capabilities

### Registration Security

- Claim codes are normalized (uppercase, no hyphens)
- API keys are generated using cryptographically secure random bytes
- TLS certificates ensure encrypted communication
- Agent ID and API key must match for authentication

![Agent Management Interface](../../assets/images/screenshots/agent_management.png)
*Agent Management interface showing active claim vouchers with different types and currently registered agents with their hardware configurations*

![Agent Details](../../assets/images/screenshots/agent_details.png)
*Detailed agent view displaying system information, hardware configuration, and GPU device management with enable/disable controls*

## Managing Agent Connections

### WebSocket Communication

Agents maintain persistent WebSocket connections for:
- Real-time job assignments
- Status updates
- Crack result reporting
- Heartbeat monitoring

### Connection Parameters

```yaml
# Environment variables for connection tuning
KH_WRITE_WAIT: "10s"      # Write timeout
KH_PONG_WAIT: "60s"       # Time to wait for pong response
KH_PING_PERIOD: "54s"     # Ping interval (must be < pong wait)
```

### Monitoring Connected Agents

1. **Dashboard View**
   - Real-time agent status on main dashboard
   - Shows connected/disconnected agents
   - Current task assignments

2. **Agent List Page**
   - Detailed view of all agents
   - Filter by status, owner, or team
   - Last heartbeat timestamps

### Managing Agent Settings

```json
// PUT /api/agents/{id}
{
  "isEnabled": true,
  "ownerId": "user-uuid",
  "extraParameters": "--custom-charset1=?l?u?d",
  "binaryVersionId": 3,
  "binaryOverride": true
}
```

### Agent Binary Version Override

Users can configure their agents to use a specific hashcat binary version instead of the job-level or system default binary.

#### Configuring Binary Override

1. **Via Agent Details Page**
   - Navigate to your agent's detail page
   - Scroll to the "Binary Version Override" section
   - Enable "Override Binary" toggle
   - Select desired binary version from dropdown
   - Click "Save"

2. **Via API**
   ```json
   // PUT /api/agents/{id}
   {
     "binaryVersionId": 3,
     "binaryOverride": true
   }
   ```

#### Binary Selection Hierarchy

When an agent needs to execute a job or benchmark, the system uses this priority order:

1. **Agent Override** (highest priority) - Binary specified in agent settings
2. **Job Binary** - Binary specified for the specific job execution
3. **System Default** (lowest priority) - Active default binary for the system

#### Use Cases

- **Testing New Versions**: Test new hashcat releases on specific agents before wider deployment
- **Compatibility Issues**: Work around driver or hardware compatibility problems
- **Performance Optimization**: Use specific binary versions that perform better on certain hardware
- **Gradual Rollouts**: Migrate agents to new versions incrementally

#### Hashcat Version Compatibility Note

⚠️ **Important**: Hashcat 7.x may detect devices but fail to recognize them as usable compute devices depending on GPU driver versions. If your agent shows devices in the hardware detection but they are not available for job execution, it is recommended to use Hashcat 6.x binaries (such as 6.2.6 or 6.2.5) as they have better compatibility with older driver versions.

#### Important Notes

- Agent binary override affects device detection, benchmarks, and job execution
- The preferred binary is automatically downloaded to the agent if not present
- If the preferred binary becomes unavailable, the system falls back to the next priority level

### Disabling/Enabling Agents

- Disabled agents remain connected but don't receive jobs
- Useful for maintenance or troubleshooting
- Preserves agent configuration and history

## Monitoring Agent Health and Performance

### Real-time Metrics

Agents report metrics every 30 seconds:

```json
{
  "cpu_usage": 45.2,
  "memory_usage": 62.8,
  "gpu_utilization": 98.5,
  "gpu_temp": 72.0,
  "gpu_metrics": {
    "device_0": {
      "temperature": 72,
      "utilization": 98.5,
      "memory_used": 8192,
      "fan_speed": 85
    }
  }
}
```

### Performance Monitoring Dashboard

1. **Agent Detail Page**
   - Historical performance graphs
   - Temperature trends
   - Utilization patterns
   - Hash rate performance

2. **Metrics Time Ranges**
   - 1 hour (default)
   - 24 hours
   - 7 days
   - 30 days

### Device Management

Each agent can have multiple devices (GPUs):

```json
// GET /api/agents/{id}/devices
[
  {
    "id": 1,
    "device_index": 0,
    "device_type": "GPU",
    "device_name": "NVIDIA GeForce RTX 4090",
    "is_enabled": true,
    "capabilities": {
      "compute_capability": "8.9",
      "memory": 24576
    }
  }
]
```

### Enabling/Disabling Devices

```json
// PUT /api/agents/{id}/devices/{deviceId}
{
  "enabled": false  // Disable specific GPU
}
```

### Runtime Selection

Each physical GPU device supports multiple compute backends (runtimes). Administrators can select which runtime to use per device.

**Available Runtimes:**
- **CUDA**: NVIDIA GPUs (optimal performance)
- **HIP**: AMD GPUs (modern Radeon cards)
- **OpenCL**: Universal (all vendors)

**Update Runtime:**
```json
// PATCH /api/agents/{id}/devices/{deviceId}/runtime
{
  "runtime": "HIP"
}
```

**Response:**
```json
{
  "message": "Device runtime updated successfully"
}
```

**Device Structure with Runtime Options:**
```json
{
  "id": 1,
  "device_id": 0,  // Physical device index
  "device_type": "GPU",
  "device_name": "AMD Radeon RX 7700S",
  "enabled": true,
  "selected_runtime": "HIP",
  "runtime_options": [
    {
      "backend": "HIP",
      "device_id": 1,      // Hashcat device ID
      "processors": 16,
      "clock": 2208,
      "memory_total": 8176,
      "memory_free": 8064,
      "pci_address": "03:00.0"
    },
    {
      "backend": "OpenCL",
      "device_id": 3,      // Hashcat device ID
      "processors": 16,
      "clock": 2208,
      "memory_total": 8176,
      "memory_free": 8064,
      "pci_address": "03:00.0"
    }
  ]
}
```

**Important Notes:**
- Changes take effect immediately for new jobs
- Each physical GPU can only run under one runtime at a time
- Runtime selection affects benchmark and job execution performance

## Agent Scheduling and Availability

### Scheduling Overview

Agents support weekly scheduling to optimize resource usage and costs.

### Configuring Agent Schedules

1. **Enable Scheduling**
   ```json
   // PUT /api/agents/{id}/scheduling-enabled
   {
     "enabled": true,
     "timezone": "America/New_York"
   }
   ```

2. **Set Daily Schedules**
   ```json
   // POST /api/agents/{id}/schedules
   {
     "dayOfWeek": 1,  // Monday (0=Sunday, 6=Saturday)
     "startTimeUTC": "22:00",
     "endTimeUTC": "06:00",
     "timezone": "America/New_York",
     "isActive": true
   }
   ```

### Schedule Features

- **UTC Storage**: All times stored in UTC for consistency
- **Timezone Display**: Shown in user's local timezone
- **Overnight Support**: Schedules can span midnight
- **Bulk Updates**: Update entire week at once

### Schedule Validation

- Start and end times must be different
- Day of week must be 0-6
- Times in HH:MM format
- Automatic handling of daylight saving time

### Availability Considerations

When scheduling is enabled:
- Agents only receive jobs during scheduled hours
- Running jobs continue to completion
- Agents remain connected outside schedule
- Heartbeat monitoring continues

## Hardware Capabilities and Benchmarks

### Hardware Detection

Agents automatically detect:

```json
{
  "hardware": {
    "cpus": [
      {
        "model": "AMD Ryzen 9 7950X",
        "cores": 16,
        "threads": 32,
        "frequency": 4.5
      }
    ],
    "gpus": [
      {
        "vendor": "NVIDIA",
        "model": "GeForce RTX 4090",
        "memory": 24576,
        "driver": "545.29.06"
      }
    ]
  }
}
```

### Benchmark System

Agents can run benchmarks for different hash types:

```sql
-- Benchmark results stored per agent
agent_benchmarks (
  agent_id,
  attack_mode,     -- 0=dictionary, 3=bruteforce, etc.
  hash_type,       -- 0=MD5, 1000=NTLM, etc.
  speed,           -- Hashes per second
  created_at
)
```

### Performance Metrics

Key metrics tracked:
- **Hash Rate**: Speed for each hash type
- **GPU Temperature**: Thermal monitoring
- **GPU Utilization**: Processing efficiency
- **Memory Usage**: VRAM consumption
- **Power Consumption**: Wattage tracking

### Consecutive Failure Tracking

Agents track consecutive task failures:
- Increments on task failure
- Resets on successful completion
- Can trigger automatic disabling
- Helps identify problematic agents

## Agent Disconnection and Task Recovery

### Understanding Disconnection Scenarios

KrakenHashes handles three distinct agent disconnection scenarios, each with specific recovery mechanisms:

#### 1. Backend Restart (Planned or Unplanned)
When the backend server restarts while agents have running tasks:

**What Happens:**
- Tasks transition to `reconnect_pending` state
- Agents continue processing and cache crack results locally
- Agent attempts reconnection with exponential backoff
- Upon reconnection, agent reports current task status

**Recovery Process:**
1. Agent reconnects and sends `current_task_status` message
2. Backend validates the task belongs to this agent
3. Task transitions from `reconnect_pending` back to `running`
4. Cached crack results are processed
5. Job continues without losing progress

**Grace Period:** Configured via Admin Panel → Settings → Job Execution → Reconnect Grace Period (default: 5 minutes)

#### 2. Graceful Agent Shutdown
When an agent is properly stopped (SIGTERM, Ctrl+C, or service stop):

**What Happens:**
- Agent sends `agent_shutdown` notification to backend
- Backend immediately marks task as `pending` for reassignment
- No retry count increment (not a failure)
- Task becomes available for other agents

**Recovery Process:**
- Task is immediately available for reassignment
- No grace period applies
- Original agent can claim new tasks upon restart

#### 3. Agent Crash or Network Failure
When an agent disconnects unexpectedly (crash, network loss, power failure):

**What Happens:**
- Backend detects lost WebSocket connection
- Task enters `reconnect_pending` state
- Grace period timer starts

**Recovery Process:**

If agent reconnects within grace period:
- Agent reports it has no running task
- Backend marks task as `pending` for reassignment
- Agent becomes available for new tasks

If grace period expires:
- Task automatically transitions to `pending`
- Available for any agent to claim
- Retry count may increment based on configuration

### Task State Transitions

```
running → reconnect_pending → running (agent reconnects with task)
running → reconnect_pending → pending (agent reconnects without task)
running → reconnect_pending → pending (grace period expires)
running → pending (graceful shutdown)
```

### Monitoring Disconnection Events

#### Key Metrics to Watch
- **Reconnection frequency**: High frequency indicates network issues
- **Grace period utilization**: Tasks recovering vs. timing out
- **Task reassignment rate**: How often tasks move between agents
- **Cached data volume**: Amount of data agents cache during disconnections

#### Log Indicators

Backend logs to monitor:
```
INFO: Agent X: Task status - HasTask: true, TaskID: xxx
INFO: Task can be recovered [task_id=xxx, status=reconnect_pending]
INFO: Successfully recovered task
```

Agent logs to monitor:
```
INFO: Sending current task status to backend
INFO: Successfully sent current task status - HasTask: true
INFO: Connection state: disconnected
INFO: Reconnection attempt X - Waiting Ys before retry
```

### Configuring Recovery Behavior

#### Reconnect Grace Period
Adjust based on your environment:
- **Stable networks**: 3-5 minutes
- **Cloud environments**: 5-10 minutes  
- **Unreliable networks**: 10-15 minutes
- **Maintenance windows**: 15-30 minutes

#### Best Practices
1. **Set grace period based on recovery time**: Consider how long it takes to:
   - Restart backend services
   - Complete rolling updates
   - Recover from network outages

2. **Monitor grace period effectiveness**:
   - Track successful recoveries vs. timeouts
   - Adjust if seeing excessive reassignments
   - Consider agent network stability

3. **Plan maintenance windows**:
   - Increase grace period before maintenance
   - Notify users of extended recovery time
   - Monitor agent reconnections post-maintenance

4. **Handle chronic disconnections**:
   - Identify agents with frequent disconnections
   - Check network path and stability
   - Consider dedicated network routes for critical agents

### Troubleshooting Recovery Issues

#### Agent Not Recovering Task After Reconnection
**Symptoms**: Agent reconnects but task is reassigned

**Common Causes**:
- Task ID mismatch between agent and backend
- Database constraint violations (check backend logs)
- Agent started fresh without cached state

**Solutions**:
1. Check agent has persistent storage for state
2. Verify task ID in agent and backend logs match
3. Review backend logs for "Failed to recover task" errors
4. Ensure database migrations are up to date

#### Tasks Stuck in Reconnect_Pending
**Symptoms**: Tasks remain in reconnect_pending after grace period

**Common Causes**:
- Backend service not running task cleanup
- Database lock preventing state transition
- Incorrect grace period configuration

**Solutions**:
1. Verify job cleanup service is running
2. Check database for locks on job_tasks table
3. Manually transition stuck tasks if needed:
   ```sql
   UPDATE job_tasks 
   SET status = 'pending', updated_at = NOW() 
   WHERE status = 'reconnect_pending' 
   AND updated_at < NOW() - INTERVAL '10 minutes';
   ```

#### Excessive Task Reassignments
**Symptoms**: Tasks frequently moving between agents

**Common Causes**:
- Grace period too short
- Network instability
- Agents crashing frequently

**Solutions**:
1. Increase reconnect grace period
2. Investigate network stability
3. Check agent system resources and logs
4. Consider agent health checks

## Troubleshooting Agent Issues

### Common Connection Issues

1. **Agent Won't Connect**
   - Check TLS certificates are valid
   - Verify API key hasn't expired
   - Ensure firewall allows WebSocket (port 8443)
   - Check agent logs for detailed errors

2. **Frequent Disconnections**
   - Review ping/pong timeout settings
   - Check network stability
   - Monitor agent system resources
   - Verify no proxy interference

### Authentication Problems

1. **Invalid API Key**
   ```bash
   # Regenerate API key
   ./krakenhashes-agent reregister --claim-code NEW-CODE
   ```

2. **Certificate Issues**
   - Check certificate expiration
   - Verify CA certificate is trusted
   - Ensure certificate matches server hostname

### Performance Issues

1. **Low Hash Rates**
   - Check GPU driver versions
   - Monitor thermal throttling
   - Verify power settings
   - Review extra parameters

2. **High Failure Rate**
   - Check hashcat binary compatibility
   - Verify file synchronization
   - Review job parameters
   - Monitor system stability

### Debugging Tools

1. **Agent Logs**
   ```bash
   # View agent logs
   tail -f ~/.krakenhashes/agent/logs/agent.log
   ```

2. **Server-side Monitoring**
   ```sql
   -- Check agent errors
   SELECT id, name, last_error, consecutive_failures
   FROM agents
   WHERE last_error IS NOT NULL
   ORDER BY updated_at DESC;
   ```

3. **WebSocket Messages**
   - Enable debug logging for detailed messages
   - Monitor heartbeat intervals
   - Check message acknowledgments

### Recovery Procedures

1. **Reset Agent State**
   ```sql
   -- Clear error state
   UPDATE agents 
   SET status = 'inactive', 
       last_error = NULL,
       consecutive_failures = 0
   WHERE id = ?;
   ```

2. **Force Reconnection**
   - Restart agent service
   - Clear local cache
   - Verify network connectivity

3. **Complete Re-registration**
   - Generate new claim code
   - Remove agent from database
   - Perform fresh registration

### Monitoring Best Practices

1. **Set Up Alerts**
   - Agent offline > 5 minutes
   - Consecutive failures > 3
   - Temperature > 85°C
   - Low hash rates

2. **Regular Maintenance**
   - Update agent software
   - Clean GPU fans
   - Check thermal paste
   - Update drivers

3. **Capacity Planning**
   - Monitor job queue depth
   - Track agent utilization
   - Plan for peak loads
   - Consider scheduling optimization

## Advanced Topics

### Agent Clustering

- Group agents by capability
- Assign specialized workloads
- Balance load across regions
- Implement failover strategies

### Security Hardening

- Rotate API keys periodically
- Implement IP whitelisting
- Use dedicated agent VLANs
- Monitor for anomalous behavior

### Integration Points

- Export metrics to monitoring systems
- Webhook notifications for events
- API automation for scaling
- Custom scheduling algorithms

## Conclusion

Effective agent management is crucial for maintaining a high-performance distributed cracking system. Regular monitoring, proper scheduling, and proactive troubleshooting ensure optimal resource utilization and job completion rates.

For additional support or advanced configurations, consult the system administrator documentation or contact the development team.