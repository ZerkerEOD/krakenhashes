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