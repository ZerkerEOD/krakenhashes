# Agent Troubleshooting Guide

This guide helps diagnose and resolve common issues with KrakenHashes agents. Use this reference when agents fail to connect, register, sync files, detect hardware, or execute jobs.

## Quick Diagnostic Commands

Before diving into specific issues, run these commands to gather diagnostic information:

```bash
# Check agent status
systemctl status krakenhashes-agent

# View recent agent logs
journalctl -u krakenhashes-agent -f --since "5 minutes ago"

# Check agent configuration
/path/to/krakenhashes-agent --version
cat ~/.krakenhashes/agent/.env

# Test connectivity to backend
curl -k https://your-backend:31337/api/health

# Verify certificate files
ls -la ~/.krakenhashes/agent/config/
openssl x509 -in ~/.krakenhashes/agent/config/client.crt -text -noout
```

## Connection Issues

### Agent Cannot Connect to Backend

**Symptoms:**
- Agent logs show "failed to connect to WebSocket server"
- Repeated connection retry attempts
- Certificate verification errors

**Common Causes:**

1. **Incorrect Backend URL Configuration**
   ```bash
   # Check agent configuration
   grep -E "KH_HOST|KH_PORT" ~/.krakenhashes/agent/.env
   
   # Test backend accessibility
   ping your-backend-host
   telnet your-backend-host 31337
   ```

2. **Certificate Issues**
   ```bash
   # Check certificate files exist
   ls -la ~/.krakenhashes/agent/config/*.crt ~/.krakenhashes/agent/config/*.key
   
   # Verify certificate validity
   openssl x509 -in ~/.krakenhashes/agent/config/client.crt -text -noout | grep -E "Valid|Subject|Issuer"
   ```

3. **Network Firewall Blocking**
   ```bash
   # Test HTTPS connectivity
   curl -k https://your-backend:31337/api/health
   
   # Test WebSocket connectivity (if nc available)
   nc -zv your-backend 31337
   ```

**Solutions:**

1. **Update Backend URL**
   ```bash
   # Edit agent configuration
   nano ~/.krakenhashes/agent/.env
   
   # Set correct values
   KH_HOST=your-backend-hostname
   KH_PORT=31337
   USE_TLS=true
   
   # Restart agent
   systemctl restart krakenhashes-agent
   ```

2. **Renew Certificates**
   ```bash
   # Stop agent
   systemctl stop krakenhashes-agent
   
   # Remove old certificates
   rm ~/.krakenhashes/agent/config/*.crt ~/.krakenhashes/agent/config/*.key
   
   # Start agent (will automatically renew certificates)
   systemctl start krakenhashes-agent
   ```

3. **Fix Network/Firewall**
   ```bash
   # Check firewall rules
   sudo ufw status
   sudo iptables -L
   
   # Open required ports
   sudo ufw allow out 31337
   sudo ufw allow out 443
   ```

### Connection Drops Frequently

**Symptoms:**
- Agent connects but disconnects after short periods
- WebSocket ping/pong timeouts
- Frequent reconnection attempts

**Causes and Solutions:**

1. **Network Instability**
   ```bash
   # Monitor network quality
   ping -c 10 your-backend-host
   
   # Check for packet loss
   mtr your-backend-host
   ```

2. **Backend Overload**
   ```bash
   # Check backend logs for resource issues
   docker-compose -f docker-compose.dev-local.yml logs backend | grep -i "error\|timeout\|overload"
   ```

3. **Aggressive Firewall/NAT**
   ```bash
   # Adjust WebSocket keepalive settings in agent config
   echo "KH_PING_PERIOD=30s" >> ~/.krakenhashes/agent/.env
   echo "KH_PONG_WAIT=60s" >> ~/.krakenhashes/agent/.env
   systemctl restart krakenhashes-agent
   ```

## Registration and Authentication Issues

### Agent Registration Fails

**Symptoms:**
- "Registration failed" errors
- "Invalid claim code" messages
- "Registration request failed" in logs

**Common Causes:**

1. **Invalid or Expired Claim Code**
   - Check admin panel for active vouchers
   - Generate new voucher if expired
   
2. **Certificate Download Issues**
   ```bash
   # Test CA certificate download
   curl -k https://your-backend:31337/ca.crt -o /tmp/ca.crt
   openssl x509 -in /tmp/ca.crt -text -noout
   ```

3. **Clock Synchronization Issues**
   ```bash
   # Check system time
   timedatectl status
   
   # Sync time if needed
   sudo ntpdate -s time.nist.gov
   # or
   sudo chrony sources -v
   ```

**Solutions:**

1. **Get Valid Claim Code**
   - Access backend admin panel
   - Go to Agent Management → Generate Voucher
   - Use the new claim code immediately

2. **Manual Registration**
   ```bash
   # Stop agent service
   systemctl stop krakenhashes-agent
   
   # Register manually
   /path/to/krakenhashes-agent --register --claim-code YOUR_CLAIM_CODE --host your-backend:31337
   
   # Start service
   systemctl start krakenhashes-agent
   ```

### Authentication Errors After Registration

**Symptoms:**
- "Failed to load API key" errors
- "Authentication failed" messages
- Agent connected but backend rejects requests

**Diagnostic Steps:**
```bash
# Check credentials files
ls -la ~/.krakenhashes/agent/config/
cat ~/.krakenhashes/agent/config/agent.key

# Verify API key format (should be UUID)
grep -E '^[0-9a-f-]{36}:[0-9]+$' ~/.krakenhashes/agent/config/agent.key
```

**Solutions:**

1. **Regenerate Credentials**
   ```bash
   # Remove existing credentials
   rm ~/.krakenhashes/agent/config/agent.key
   rm ~/.krakenhashes/agent/config/*.crt ~/.krakenhashes/agent/config/*.key
   
   # Re-register
   systemctl stop krakenhashes-agent
   /path/to/krakenhashes-agent --register --claim-code NEW_CLAIM_CODE --host your-backend:31337
   systemctl start krakenhashes-agent
   ```

2. **Fix Permissions**
   ```bash
   # Set correct ownership and permissions
   chown -R $(whoami):$(whoami) ~/.krakenhashes/agent/
   chmod 700 ~/.krakenhashes/agent/config/
   chmod 600 ~/.krakenhashes/agent/config/agent.key
   chmod 600 ~/.krakenhashes/agent/config/client.key
   chmod 644 ~/.krakenhashes/agent/config/*.crt
   ```

## Hardware Detection Issues

### No Devices Detected

**Symptoms:**
- Agent shows "0 devices detected"
- Missing GPU information in admin panel
- Hashcat fails to find OpenCL/CUDA devices

**Diagnostic Steps:**
```bash
# Check if hashcat binary exists
ls -la ~/.krakenhashes/agent/data/binaries/

# Manually test hashcat device detection
find ~/.krakenhashes/agent/data/binaries -name "hashcat*" -type f -executable | head -1 | xargs -I {} {} -I

# Check for GPU drivers
nvidia-smi           # NVIDIA
rocm-smi             # AMD
intel_gpu_top        # Intel
lspci | grep -i vga  # General
```

**Common Solutions:**

1. **Install GPU Drivers**
   ```bash
   # NVIDIA
   sudo apt update
   sudo apt install nvidia-driver-470  # or latest
   
   # AMD
   sudo apt install rocm-opencl-runtime
   
   # Intel
   sudo apt install intel-opencl-icd
   ```

2. **Install OpenCL Runtime**
   ```bash
   # Install generic OpenCL
   sudo apt install ocl-icd-opencl-dev opencl-headers
   
   # Verify OpenCL installation
   clinfo  # if available
   ```

3. **Fix Hashcat Binary Issues**
   ```bash
   # Check hashcat binary permissions
   find ~/.krakenhashes/agent/data/binaries -name "hashcat*" -type f | xargs ls -la
   
   # Make executable if needed
   find ~/.krakenhashes/agent/data/binaries -name "hashcat*" -type f | xargs chmod +x
   ```

### Devices Detected But Not Usable

**Symptoms:**
- Agent shows devices in hardware detection output
- Devices appear in the admin panel
- However, devices are not available for job execution
- Jobs fail with "no devices available" errors

**Common Cause:**

Hashcat 7.x compatibility issues with older GPU drivers. Hashcat 7.x may detect devices but fail to initialize them for compute operations with certain driver versions.

**Solutions:**

1. **Use Hashcat 6.x Binary (Recommended)**
   - Navigate to your Agent Details page in the web UI
   - Enable "Binary Override" toggle
   - Select a Hashcat 6.x version (e.g., 6.2.6 or 6.2.5) from the dropdown
   - Click "Save"
   - The agent will automatically download the binary
   - Device detection will re-run with the 6.x binary

2. **Update GPU Drivers**
   ```bash
   # NVIDIA - Use drivers 545.x or newer
   sudo apt update
   sudo apt install nvidia-driver-545  # or latest
   sudo reboot

   # AMD - Use ROCm 5.7 or newer / Adrenalin 23.12 or newer
   # Follow AMD's official ROCm installation guide

   # Verify driver installation
   nvidia-smi  # NVIDIA
   rocm-smi    # AMD
   ```

3. **Verify Driver Compatibility**
   ```bash
   # For NVIDIA - check driver version
   nvidia-smi --query-gpu=driver_version --format=csv,noheader

   # For AMD - check ROCm version
   rocminfo | grep "Agent"

   # Manually test hashcat with specific binary
   ~/.krakenhashes/agent/data/binaries/3/hashcat.bin -I  # Replace 3 with your binary version
   ```

4. **Check Agent Logs**
   ```bash
   # Look for device initialization errors
   journalctl -u krakenhashes-agent -n 100 | grep -i "device\|gpu\|opencl\|cuda"
   ```

### Partial Device Detection

**Symptoms:**
- Some GPUs detected, others missing
- Device count mismatch
- Specific GPU types not showing

**Solutions:**

1. **Mixed GPU Environment**
   ```bash
   # Ensure all necessary drivers installed
   nvidia-smi && rocm-smi && intel_gpu_top --list

   # Check for driver conflicts
   dmesg | grep -i "gpu\|nvidia\|amd\|intel" | tail -20
   ```

2. **PCIe/Power Issues**
   ```bash
   # Check PCIe slot detection
   lspci | grep -i vga
   sudo lshw -c display
   
   # Check power management
   cat /sys/class/drm/card*/device/power_state
   ```

## File Synchronization Problems

### Files Not Downloading

**Symptoms:**
- Wordlists/rules not available for jobs
- "File not found" errors during job execution
- Sync requests timing out

**Diagnostic Steps:**
```bash
# Check data directories
ls -la ~/.krakenhashes/agent/data/
ls ~/.krakenhashes/agent/data/wordlists/
ls ~/.krakenhashes/agent/data/rules/
ls ~/.krakenhashes/agent/data/binaries/

# Test file download manually
curl -k -H "X-API-Key: YOUR_API_KEY" -H "X-Agent-ID: YOUR_AGENT_ID" \
     https://your-backend:31337/api/agent/files/wordlists/rockyou.txt \
     -o /tmp/test_download.txt
```

**Common Solutions:**

1. **Fix Authentication**
   ```bash
   # Verify API key is valid
   grep -o '^[^:]*' ~/.krakenhashes/agent/config/agent.key | head -1
   
   # Test API authentication
   API_KEY=$(grep -o '^[^:]*' ~/.krakenhashes/agent/config/agent.key | head -1)
   AGENT_ID=$(grep -o '[^:]*$' ~/.krakenhashes/agent/config/agent.key)
   curl -k -H "X-API-Key: $API_KEY" -H "X-Agent-ID: $AGENT_ID" \
        https://your-backend:31337/api/agent/info
   ```

2. **Fix Directory Permissions**
   ```bash
   # Ensure agent can write to data directories
   chown -R $(whoami):$(whoami) ~/.krakenhashes/agent/data/
   chmod -R 755 ~/.krakenhashes/agent/data/
   ```

3. **Clear Corrupted Downloads**
   ```bash
   # Remove partial/corrupted files
   find ~/.krakenhashes/agent/data/ -name "*.tmp" -delete
   find ~/.krakenhashes/agent/data/ -size 0 -delete
   
   # Force re-sync
   systemctl restart krakenhashes-agent
   ```

### Binary Extraction Failures

**Symptoms:**
- Downloaded .7z files not extracted
- Hashcat binary not executable
- "No such file or directory" when running hashcat

**Solutions:**

1. **Install 7-Zip Support**
   ```bash
   sudo apt install p7zip-full
   
   # Test extraction manually
   cd ~/.krakenhashes/agent/data/binaries/
   find . -name "*.7z" | head -1 | xargs 7z t  # Test archive
   ```

2. **Fix Extraction Permissions**
   ```bash
   # Ensure extraction destination is writable
   chmod 755 ~/.krakenhashes/agent/data/binaries/
   
   # Re-extract manually if needed
   cd ~/.krakenhashes/agent/data/binaries/
   find . -name "*.7z" -exec 7z x {} \;
   ```

## Job Execution Failures

### Jobs Not Starting

**Symptoms:**
- Tasks assigned but never start
- Agent shows as idle despite task assignment
- "No enabled devices" errors

**Diagnostic Steps:**
```bash
# Check agent task status
journalctl -u krakenhashes-agent | grep -i "task\|job" | tail -10

# Verify enabled devices in backend
# (Check admin panel Agent Details page)

# Test hashcat manually
HASHCAT=$(find ~/.krakenhashes/agent/data/binaries -name "hashcat*" -type f -executable | head -1)
$HASHCAT --help
```

**Solutions:**

1. **Enable Devices**
   - Go to backend Admin Panel
   - Navigate to Agent Management
   - Select agent and enable required devices

2. **Fix Hashcat Path**
   ```bash
   # Ensure hashcat binary is executable
   find ~/.krakenhashes/agent/data/binaries -name "hashcat*" -type f | xargs chmod +x
   
   # Create symlink if needed
   HASHCAT=$(find ~/.krakenhashes/agent/data/binaries -name "hashcat*" -type f -executable | head -1)
   sudo ln -sf "$HASHCAT" /usr/local/bin/hashcat
   ```

### Jobs Crash or Stop Unexpectedly

**Symptoms:**
- Jobs start but terminate quickly
- "Process killed" messages
- Hashcat segmentation faults

**Diagnostic Steps:**
```bash
# Check system resources
free -h
df -h ~/.krakenhashes/agent/data/
ps aux | grep hashcat

# Check for OOM kills
dmesg | grep -i "killed process\|out of memory" | tail -5
journalctl -f | grep -i "oom\|memory"
```

**Solutions:**

1. **Resource Issues**
   ```bash
   # Check memory usage
   free -h
   
   # Clear cache if needed
   sudo sync
   echo 3 | sudo tee /proc/sys/vm/drop_caches
   
   # Check disk space
   df -h ~/.krakenhashes/agent/data/
   
   # Clean old files if needed
   find ~/.krakenhashes/agent/data/ -name "*.tmp" -mtime +7 -delete
   ```

2. **Driver/Hardware Issues**
   ```bash
   # Check GPU status
   nvidia-smi  # Check temperature, power, utilization
   
   # Test memory stability
   nvidia-smi --query-gpu=memory.used,memory.free,temperature.gpu --format=csv -lms 1000
   
   # Check for hardware errors
   dmesg | grep -i "error\|fault" | tail -10
   ```

### Job Progress Not Reporting

**Symptoms:**
- Jobs running but no progress updates
- Backend shows tasks as "running" indefinitely
- No crack notifications

**Solutions:**

1. **Check WebSocket Connection**
   ```bash
   # Verify agent is connected
   journalctl -u krakenhashes-agent | grep -i "websocket\|connected" | tail -5

   # Look for progress send errors
   journalctl -u krakenhashes-agent | grep -i "progress\|send.*fail" | tail -10
   ```

2. **Restart Agent Connection**
   ```bash
   # Restart agent service
   systemctl restart krakenhashes-agent

   # Monitor connection establishment
   journalctl -u krakenhashes-agent -f | grep -i "connect\|progress"
   ```

## Agent Stability and Connection Issues

### Agent Crashes During High-Volume Cracking

**Symptoms:**
- Agent crashes when thousands of hashes crack rapidly
- Panic errors related to closed channels
- Connection drops during large password discoveries
- "send on closed channel" errors

**Root Cause:**
High-volume cracking (e.g., 4,000+ cracks in seconds) can overwhelm the WebSocket message system if not properly buffered.

**Solutions Implemented (v1.2.1+):**

The system now includes automatic protections:

1. **Crack Batching System**
   - Cracks are batched in 500ms windows or 10,000-crack groups
   - Reduces message volume by 100x (8,000 messages → 80 messages)
   - See [Crack Batching System](../../reference/architecture/crack-batching-system.md) for details

2. **Increased Channel Buffers**
   ```go
   // Agent outbound buffer increased from 256 → 4,096 messages
   // Handles burst traffic during high-volume cracking
   ```

3. **Channel Monitoring**
   - Automatic warnings when buffer reaches 75% capacity
   - Critical alerts at 90% capacity
   - Graceful message dropping instead of crashes

**Monitoring for Issues:**
```bash
# Check for channel fullness warnings
journalctl -u krakenhashes-agent | grep -i "channel.*full\|fullness"

# Look for dropped messages (indicates overload)
journalctl -u krakenhashes-agent | grep -i "dropped message"

# Monitor batch sizes (should be 500-10000 cracks)
journalctl -u krakenhashes-agent | grep -i "flush.*batch"
```

**Expected Log Messages (Normal Operation):**
```
[INFO] Flushing crack batch for task abc-123: 2453 cracks
[DEBUG] Crack batch sent successfully
```

**Warning Signs:**
```
[WARNING] Outbound channel filling up (78.2%)
[ERROR] Outbound channel critically full (92.5%)
[ERROR] Dropped message - channel full (95.0%)
```

**Recovery Actions:**

If you see persistent channel fullness warnings:

1. **Check Backend Performance**
   ```bash
   # Verify backend is processing batches quickly
   docker logs krakenhashes-backend | grep -i "crack batch\|processing.*cracks"
   ```

2. **Monitor Network Bandwidth**
   ```bash
   # Ensure adequate bandwidth for WebSocket traffic
   iftop -i eth0  # or your network interface
   ```

3. **Verify Database Performance**
   ```bash
   # Check for slow crack processing queries
   # (See backend logs for timing information)
   docker logs krakenhashes-backend | grep -i "processed.*cracks.*seconds"
   ```

### Double-Close Panic Prevention

**Historical Issue (Fixed in v1.2.1):**
Agents could crash with "close of closed channel" panics during connection cleanup.

**Symptoms (if using older version):**
```
panic: close of closed channel
goroutine 123 [running]:
agent/internal/agent.(*AgentConnection).cleanup()
```

**Solution:**
Update to v1.2.1+ which includes:
- Mutex-protected channel closing
- Close-once semantics with sync.Once
- Graceful shutdown during connection cleanup

**If Still Experiencing Issues:**
```bash
# Verify agent version
/path/to/krakenhashes-agent --version

# Should show v1.2.1 or later
# If older, update agent binary
```

### Channel Overflow Protection

**How the System Protects You:**

1. **Automatic Batching**
   - Individual cracks accumulated in memory
   - Sent in bulk every 500ms or when 10k accumulated
   - Reduces network traffic and message count

2. **Buffer Monitoring**
   - System tracks outbound channel capacity
   - Warnings logged before critical levels reached
   - Allows proactive investigation

3. **Graceful Degradation**
   - If channel is full, message is dropped (not crashed)
   - Drop events are logged for investigation
   - Agent remains operational

**Performance Tuning:**

For environments with extremely high crack rates:

```bash
# Increase channel buffer (requires agent rebuild)
# Edit agent/internal/agent/connection.go:
# outbound: make(chan []byte, 8192)  // Double the default

# Or reduce batch window for more frequent smaller batches
# Edit agent/internal/jobs/hashcat_executor.go:
# crackBatchInterval: 250 * time.Millisecond  // Half the window
```

**⚠️ Warning:** Custom tuning is rarely needed. The defaults handle >99% of scenarios including extremely high-volume cracking.

### Agent Stuck State Recovery

**Symptoms:**
- Agent shows as "busy" in the admin panel but has no running task
- Agent completed a task but can't accept new work
- Backend shows agent with `current_task_id` set but task is completed
- Agent logs show "stuck in completing state" warnings

**Root Cause (GH Issue #12):**
Prior to v1.3.1, a race condition could occur where:
1. Agent completes task and sends completion message
2. Message is lost or backend doesn't process it
3. Agent remains in "completing" state indefinitely
4. Backend still shows agent as busy

**Automatic Recovery (v1.3.1+):**

The system now includes multiple automatic recovery mechanisms:

1. **Completion ACK Protocol**
   - Backend acknowledges every task completion
   - Agent waits up to 30 seconds for ACK (3 retries)
   - If no ACK received, marks completion as pending

2. **Stuck Detection**
   - Agent monitors its own state every 30 seconds
   - If stuck in "completing" state for > 2 minutes, forces recovery
   - Automatically transitions to idle and accepts new work

3. **State Sync Protocol**
   - Backend requests state sync every 5 minutes
   - Agent reports current state and any pending completions
   - Backend resolves mismatches automatically

**Manual Recovery (If Automatic Fails):**

1. **Restart the Agent**
   ```bash
   # Simple restart clears agent state
   systemctl restart krakenhashes-agent
   ```

2. **Check Agent State**
   ```bash
   # Look for stuck state warnings
   journalctl -u krakenhashes-agent | grep -i "stuck\|completing\|recovery"

   # Check for ACK timeout messages
   journalctl -u krakenhashes-agent | grep -i "ack.*timeout\|no ack received"
   ```

3. **Force Backend State Reset**
   ```bash
   # Via admin API (requires admin credentials)
   curl -k -X POST -H "Authorization: Bearer YOUR_TOKEN" \
        https://your-backend:31337/api/admin/agents/AGENT_ID/reset-state
   ```

**Diagnostic Log Messages:**

**Normal operation:**
```
[INFO] Task abc-123 completed, waiting for ACK
[INFO] ACK received for task abc-123
[INFO] Transitioning to idle state
```

**ACK timeout (triggers retry):**
```
[WARNING] No ACK received for task abc-123, retrying (attempt 2/3)
[INFO] Resending completion for task abc-123
```

**Stuck detection triggered:**
```
[WARNING] Stuck detection: Agent in COMPLETING state for 2m30s
[INFO] Force recovery initiated for task abc-123
[INFO] Marking completion as pending, transitioning to idle
```

**State sync resolution:**
```
[INFO] State sync requested by backend
[INFO] Reporting pending completion for task abc-123
[INFO] Backend confirmed task completion resolved
```

### Task Completion ACK Troubleshooting

**ACK Never Received:**

Possible causes:
- WebSocket connection dropped during completion
- Backend crashed while processing
- Network partition during ACK transmission

**Diagnostic Steps:**
```bash
# Check for WebSocket connection issues
journalctl -u krakenhashes-agent | grep -i "websocket\|connection.*closed\|disconnect"

# Check backend logs for processing errors
docker logs krakenhashes-backend | grep -i "task.*complete\|ack\|error"

# Verify network stability
ping -c 10 your-backend-host
```

**Solutions:**
1. Agent will auto-recover via stuck detection (2 min timeout)
2. Backend will resolve via state sync (5 min interval)
3. Restart agent for immediate recovery: `systemctl restart krakenhashes-agent`

**Duplicate Completion Messages:**

The system handles duplicates gracefully:
- Backend caches completions for 1 hour
- Duplicate messages receive ACK without reprocessing
- No double-counting of cracks or keyspace

**Monitoring:**
```bash
# Check for duplicate completion handling
journalctl -u krakenhashes-agent | grep -i "duplicate\|already processed"

# Backend logs show cache hits
docker logs krakenhashes-backend | grep -i "completion.*cached\|already completed"
```

### Connection Stability Best Practices

1. **Monitor Logs Proactively**
   ```bash
   # Set up log monitoring for early warning signs
   journalctl -u krakenhashes-agent -f | grep -E "WARNING|ERROR|full|dropped"
   ```

2. **Network Quality**
   - Ensure stable, low-latency connection to backend
   - Avoid Wi-Fi for production agents (use wired connections)
   - Monitor for packet loss: `mtr your-backend-host`

3. **Backend Capacity**
   - Ensure backend can process batches quickly (<5 seconds)
   - Monitor backend CPU/memory during high-volume jobs
   - Scale backend resources if consistent warnings appear

4. **Update Regularly**
   - Keep agent binary up to date for latest stability fixes
   - Review release notes for performance improvements
   - Test updates in dev environment first

## Performance Problems

### Slow Hash Rates

**Symptoms:**
- Lower than expected H/s rates
- GPU underutilization
- Benchmark speeds don't match job speeds

**Solutions:**

1. **GPU Optimization**
   ```bash
   # Check GPU power limits
   nvidia-smi -q -d POWER
   
   # Increase power limit (if supported)
   sudo nvidia-smi -pl 300  # 300W example
   
   # Set performance mode
   sudo nvidia-smi -pm 1
   ```

2. **Cooling and Throttling**
   ```bash
   # Monitor temperatures
   watch nvidia-smi
   
   # Check thermal throttling
   nvidia-smi --query-gpu=temperature.gpu,clocks_throttle_reasons.gpu_idle,clocks_throttle_reasons.applications_clocks_setting --format=csv -lms 1000
   ```

3. **Hashcat Parameters**
   ```bash
   # Add optimization flags in agent config
   echo "HASHCAT_EXTRA_PARAMS=-O -w 4" >> ~/.krakenhashes/agent/.env
   systemctl restart krakenhashes-agent
   ```

### High System Load

**Symptoms:**
- System becomes unresponsive
- Other applications slow down
- CPU usage constantly high

**Solutions:**

1. **Limit Resource Usage**
   ```bash
   # Limit hashcat workload
   echo "HASHCAT_EXTRA_PARAMS=-w 2" >> ~/.krakenhashes/agent/.env
   
   # Set CPU affinity (example: use only cores 0-3)
   systemctl edit krakenhashes-agent
   # Add:
   # [Service]
   # CPUAffinity=0-3
   ```

2. **System Tuning**
   ```bash
   # Increase file descriptor limits
   echo "* soft nofile 65536" | sudo tee -a /etc/security/limits.conf
   echo "* hard nofile 65536" | sudo tee -a /etc/security/limits.conf
   
   # Optimize memory management
   echo 'vm.swappiness=10' | sudo tee -a /etc/sysctl.conf
   sudo sysctl -p
   ```

## Error Message Reference

### Common Error Patterns

| Error Message | Cause | Solution |
|---------------|--------|----------|
| `failed to connect to WebSocket server` | Network/TLS issues | Check connectivity, renew certificates |
| `failed to load API key` | Missing/corrupt credentials | Re-register agent |
| `registration failed` | Invalid claim code | Generate new voucher |
| `failed to detect devices` | Missing drivers/OpenCL | Install GPU drivers |
| `no enabled devices` | Devices disabled in backend | Enable devices in admin panel |
| `file sync timeout` | Network/authentication issues | Check API credentials |
| `hashcat not found` | Missing/corrupt binary | Re-download binaries |
| `certificate verify failed` | Expired/invalid certificates | Renew certificates |
| `connection refused` | Backend not accessible | Check backend status |
| `permission denied` | File/directory permissions | Fix ownership/permissions |

### Debug Logging

Enable detailed logging for troubleshooting:

```bash
# Enable debug logging
echo "DEBUG=true" >> ~/.krakenhashes/agent/.env
systemctl restart krakenhashes-agent

# View detailed logs
journalctl -u krakenhashes-agent -f

# Disable debug logging after troubleshooting
sed -i '/DEBUG=true/d' ~/.krakenhashes/agent/.env
systemctl restart krakenhashes-agent
```

## Recovery Procedures

### Complete Agent Reset

When all else fails, completely reset the agent:

```bash
# Stop agent
systemctl stop krakenhashes-agent

# Backup current configuration
cp -r ~/.krakenhashes/agent ~/.krakenhashes/agent.backup.$(date +%Y%m%d)

# Remove all agent data
rm -rf ~/.krakenhashes/agent/

# Re-register with new claim code
/path/to/krakenhashes-agent --register --claim-code NEW_CLAIM_CODE --host your-backend:31337

# Start agent
systemctl start krakenhashes-agent
```

### Emergency Job Cleanup

Force cleanup of stuck hashcat processes:

```bash
# Kill all hashcat processes
pkill -f hashcat

# Clean temporary files
find ~/.krakenhashes/agent/data/ -name "*.tmp" -delete
find ~/.krakenhashes/agent/data/ -name "*.restore" -delete

# Restart agent to reset job state
systemctl restart krakenhashes-agent
```

### Certificate Recovery

Recover from certificate issues:

```bash
# Stop agent
systemctl stop krakenhashes-agent

# Download CA certificate manually
curl -k https://your-backend:31337/ca.crt -o ~/.krakenhashes/agent/config/ca.crt

# Use API key to renew client certificates
API_KEY=$(grep -o '^[^:]*' ~/.krakenhashes/agent/config/agent.key | head -1)
AGENT_ID=$(grep -o '[^:]*$' ~/.krakenhashes/agent/config/agent.key)
curl -k -X POST -H "X-API-Key: $API_KEY" -H "X-Agent-ID: $AGENT_ID" \
     https://your-backend:31337/api/agent/renew-certificates

# Start agent
systemctl start krakenhashes-agent
```

## When to Restart vs Reinstall

### Restart Agent Service
- Connection drops
- Configuration changes
- Minor authentication issues
- After enabling/disabling devices

### Restart System
- GPU driver updates
- System resource exhaustion
- Hardware changes
- Kernel updates

### Reinstall Agent
- Corrupt binary files
- Persistent authentication failures after certificate renewal
- File system permission issues that can't be resolved
- Agent binary corruption

### Complete Reset (Last Resort)
- Multiple interconnected issues
- System contamination from previous installations
- Unknown configuration corruption
- When restart and reinstall don't resolve issues

Use the diagnostic commands at the beginning of this guide to determine the appropriate recovery level.