# Agent Debugging Guide

This guide covers debugging the KrakenHashes agent, including enabling debug mode, interpreting logs, and using development tools to diagnose issues.

## Quick Start - Enable Debug Mode

The fastest way to enable debug logging:

```bash
# Method 1: Command line flag
./krakenhashes-agent --debug --host backend.example.com:31337

# Method 2: Environment variable
export DEBUG=true
export LOG_LEVEL=DEBUG
./krakenhashes-agent --host backend.example.com:31337

# Method 3: Edit .env file
echo "DEBUG=true" >> .env
echo "LOG_LEVEL=DEBUG" >> .env
./krakenhashes-agent --host backend.example.com:31337
```

## Debug Configuration Options

### Command Line Flags

The agent supports several debugging-related command line flags:

```bash
./krakenhashes-agent --help

  --debug               Enable debug logging (default: false)
  --host string         Backend server host (e.g., localhost:31337)
  --tls                 Use TLS for secure communication (default: true)
  --interface string    Network interface to listen on (optional)
  --heartbeat int       Heartbeat interval in seconds (default: 5)
  --config-dir string   Configuration directory for certificates and credentials
  --data-dir string     Data directory for binaries, wordlists, rules, and hashlists
  --hashcat-params string  Extra parameters to pass to hashcat (e.g., '-O -w 3')
```

### Environment Variables

Configure debugging through environment variables in `.env` file:

```bash
# Logging Configuration
DEBUG=true                    # Enable debug logging
LOG_LEVEL=DEBUG              # Set minimum log level (DEBUG, INFO, WARNING, ERROR)

# Server Configuration
KH_HOST=backend.example.com  # Backend hostname
KH_PORT=31337               # Backend port
USE_TLS=true                # Use secure connections

# WebSocket Timing Configuration
KH_WRITE_WAIT=10s           # WebSocket write timeout
KH_PONG_WAIT=60s            # Server pong timeout
KH_PING_PERIOD=54s          # Ping interval

# File Transfer Configuration
KH_MAX_CONCURRENT_DOWNLOADS=3  # Concurrent download limit
KH_DOWNLOAD_TIMEOUT=1h         # Download timeout
KH_MAX_DOWNLOAD_RETRIES=3      # Download retry attempts

# Development Configuration
HEARTBEAT_INTERVAL=5        # Heartbeat frequency (seconds)
```

### Log Levels

The agent supports four log levels in order of severity:

1. **DEBUG** - Detailed diagnostic information
2. **INFO** - General operational messages
3. **WARNING** - Potential issues that don't stop operation
4. **ERROR** - Serious problems that may cause failures

Set the minimum level with `LOG_LEVEL` environment variable:

```bash
# Show all messages
LOG_LEVEL=DEBUG

# Show info, warnings, and errors
LOG_LEVEL=INFO

# Show only warnings and errors
LOG_LEVEL=WARNING

# Show only errors
LOG_LEVEL=ERROR
```

## Debug Output Interpretation

### Log Message Format

Debug messages follow this format:
```
[LEVEL] [TIMESTAMP] [FILE:LINE] [FUNCTION] MESSAGE
```

Example:
```
[DEBUG] [2025-01-10 15:04:05.123] [/path/to/file.go:42] [package.Function] Connecting to backend server
```

### Common Debug Messages

#### Startup and Configuration
```
[INFO] Debug logging initialized - Debug enabled: true
[INFO] Current working directory: /path/to/agent
[INFO] Loading agent configuration...
[INFO] Using config directory: /path/to/config
[INFO] Using data directory: /path/to/data
```

#### WebSocket Connection
```
[DEBUG] Starting WebSocket connection process
[INFO] Connection attempt 1 of 3
[DEBUG] WebSocket connected to wss://backend.example.com:31337/ws/agent
[INFO] Connection attempt 1 successful
```

#### Hardware Detection
```
[INFO] Detecting compute devices at startup...
[DEBUG] Found GPU: NVIDIA RTX 4090 (Device ID: 0)
[DEBUG] Found GPU: NVIDIA RTX 4080 (Device ID: 1)
[INFO] Successfully detected and sent device information to server
```

#### File Synchronization
```
[INFO] Initializing file sync with max downloads: 3, timeout: 1h0m0s, max retries: 3
[DEBUG] Scanning wordlists directory: /path/to/data/wordlists
[INFO] File sync: Found 15 local files, backend has 23 files
[DEBUG] Downloading missing file: rockyou.txt (14344391 bytes)
```

#### Job Execution
```
[INFO] Received job assignment for task: task_abc123
[DEBUG] Starting hashcat with command: hashcat -m 1000 -a 0 hashes.txt wordlist.txt
[DEBUG] Hashcat process started with PID: 12345
[INFO] Job progress: Task task_abc123, Keyspace 1000000, Hash rate 2500000 H/s
```

#### Task State Machine (v1.3.1+)
```
[DEBUG] State transition: Idle → Running (task: task_abc123)
[DEBUG] State transition: Running → Completing (task: task_abc123)
[INFO] Waiting for ACK from backend
[DEBUG] ACK received, timestamp: 2024-01-15T10:30:00Z
[DEBUG] State transition: Completing → Idle
```

#### Completion ACK Protocol
```
[INFO] Task task_abc123 completed, sending progress update
[DEBUG] Entering Completing state, starting ACK wait
[INFO] ACK wait attempt 1/3, timeout: 30s
[DEBUG] ACK received successfully
[INFO] Task completion acknowledged, transitioning to idle
```

#### Stuck Detection Recovery
```
[WARNING] Stuck detection check: state=Completing, duration=2m30s
[WARNING] Stuck timeout exceeded (2m), initiating recovery
[INFO] Force recovery: marking completion_pending for task_abc123
[DEBUG] State transition: Completing → Idle (forced)
[INFO] Agent recovered, now idle and available for work
```

## Component-Specific Debugging

### WebSocket Connection Debugging

Enable verbose WebSocket debugging:

```bash
# Add to .env file
DEBUG=true
LOG_LEVEL=DEBUG

# Monitor WebSocket messages
tail -f agent.log | grep -E "(WebSocket|WSMessage|connection)"
```

Common WebSocket issues and debugging:

```bash
# Connection timeout
[ERROR] Failed to create connection on attempt 1: dial tcp: i/o timeout

# Certificate issues
[ERROR] Failed to load CA certificate: certificate signed by unknown authority

# Authentication failures
[ERROR] WebSocket handshake failed: HTTP 401 Unauthorized
```

### File Synchronization Debugging

Monitor file sync operations:

```bash
# Filter sync-related logs
tail -f agent.log | grep -E "(sync|download|FileInfo)"

# Debug specific file types
tail -f agent.log | grep -E "(wordlist|rule|binary)"
```

File sync debug messages:
```
[DEBUG] Calculating MD5 hash for file: /path/to/wordlist.txt
[INFO] File sync: Downloading wordlist: rockyou.txt (14MB)
[WARNING] Download retry 2/3 for file: large_wordlist.txt
[ERROR] Failed to download file after 3 attempts: connection timeout
```

### Job Execution Debugging

Monitor hashcat job execution:

```bash
# Job-specific logs
tail -f agent.log | grep -E "(job|task|hashcat|progress)"

# Real-time hashcat output
tail -f agent.log | grep "hashcat_output"
```

Job debug messages:
```
[INFO] Starting hashcat executor with extra params: -O -w 3
[DEBUG] Hashcat working directory: /tmp/krakenhashes/task_abc123
[DEBUG] Hashcat stdout: Session..........: hashcat
[DEBUG] Hashcat stdout: Status...........: Running
[INFO] Job completed successfully, found 15 cracked hashes
```

### Task State Machine Debugging (v1.3.1+)

The agent uses an explicit state machine to track task status. Debug state transitions:

```bash
# Monitor state transitions
tail -f agent.log | grep -E "(state|State|transition|Transition)"

# Monitor ACK protocol
tail -f agent.log | grep -E "(ACK|ack|waiting|acknowledge)"

# Monitor stuck detection
tail -f agent.log | grep -E "(stuck|Stuck|recovery|Recovery|pending)"
```

**State Machine Debug Messages:**

Normal task lifecycle:
```
[DEBUG] TaskStateManager: TransitionTo(Running, task_abc123)
[DEBUG] TaskStateManager: state=Running, taskID=task_abc123
[DEBUG] TaskStateManager: TransitionTo(Completing, task_abc123)
[DEBUG] ACK wait started for task_abc123
[DEBUG] ACK received: task_abc123, success=true
[DEBUG] TaskStateManager: TransitionToIdle()
```

ACK timeout and retry:
```
[WARNING] ACK wait timeout for task_abc123 (attempt 1/3)
[INFO] Retrying completion message for task_abc123
[DEBUG] Resending job_progress for task_abc123
[WARNING] ACK wait timeout for task_abc123 (attempt 2/3)
[WARNING] ACK wait timeout for task_abc123 (attempt 3/3)
[WARNING] ACK retries exhausted, setting completion_pending
[DEBUG] TaskStateManager: SetCompletionPending(task_abc123)
[DEBUG] TaskStateManager: TransitionToIdle()
```

Stuck detection triggered:
```
[DEBUG] Stuck detection check: state=Completing, taskID=task_abc123, duration=2m15s
[DEBUG] Stuck detection check: state=Completing, taskID=task_abc123, duration=2m45s
[WARNING] Stuck detection: duration 2m45s exceeds timeout 2m
[INFO] Initiating force recovery for task_abc123
[DEBUG] TaskStateManager: SetCompletionPending(task_abc123)
[DEBUG] TaskStateManager: TransitionToIdle() [forced]
[INFO] Force recovery complete, agent now idle
```

**Querying Current State:**

Add diagnostic logging by checking state:
```bash
# Look for state info in logs
tail -f agent.log | grep -E "GetState|currentState|GetStateInfo"
```

**Common State Issues:**

| Issue | Log Pattern | Solution |
|-------|------------|----------|
| Stuck in Completing | `state=Completing` for >2 min | Wait for auto-recovery or restart agent |
| ACK never received | Multiple `ACK wait timeout` | Check network/backend connectivity |
| Rapid state changes | Fast Idle→Running→Idle | Check for task assignment rejections |
| Completion pending | `completion_pending=true` | Backend resolves on next state sync |

### Hardware Detection Debugging

Monitor GPU and hardware detection:

```bash
# Hardware detection logs
tail -f agent.log | grep -E "(hardware|GPU|device|monitor)"

# Device capabilities
tail -f agent.log | grep -E "(OpenCL|CUDA|compute)"
```

Hardware debug messages:
```
[DEBUG] Detecting NVIDIA GPUs using nvidia-ml-py
[INFO] Found NVIDIA GPU: GeForce RTX 4090 (12GB VRAM)
[DEBUG] GPU compute capability: 8.9
[WARNING] GPU temperature high: 85°C
```

## Development Environment Setup

### Building Debug Builds

Create debug builds with additional debugging information:

```bash
# Build with debug symbols (no optimization)
cd agent
make clean

# Set debug build flags
export GOFLAGS="-gcflags=-N -gcflags=-l"
make build

# Or build with race detection
go build -race -o debug-agent ./cmd/agent
```

### Using Go Debugger (Delve)

Install and use Delve for interactive debugging:

```bash
# Install delve
go install github.com/go-delve/delve/cmd/dlv@latest

# Build and debug
cd agent
go build -gcflags="all=-N -l" -o debug-agent ./cmd/agent

# Start debugging session
dlv exec ./debug-agent -- --debug --host localhost:31337

# Common delve commands:
# (dlv) break main.main
# (dlv) continue
# (dlv) print cfg
# (dlv) step
# (dlv) next
```

### Using pprof for Performance Profiling

Add profiling endpoints for performance analysis:

```go
// Add to main.go for profiling (development only)
import _ "net/http/pprof"

// Start profiling server (development builds only)
go func() {
    log.Println(http.ListenAndServe("localhost:6060", nil))
}()
```

Then profile the running agent:

```bash
# CPU profiling
go tool pprof http://localhost:6060/debug/pprof/profile

# Memory profiling
go tool pprof http://localhost:6060/debug/pprof/heap

# Goroutine profiling
go tool pprof http://localhost:6060/debug/pprof/goroutine
```

### Memory and Goroutine Analysis

Monitor resource usage during development:

```bash
# Monitor memory usage
while true; do
    ps -p $(pgrep krakenhashes-agent) -o pid,rss,vsz,pcpu,pmem,cmd
    sleep 5
done

# Monitor goroutines (with pprof endpoint)
curl http://localhost:6060/debug/pprof/goroutine?debug=1
```

## Common Debugging Scenarios

### 1. Agent Won't Connect to Backend

**Symptoms:**
- Connection timeout errors
- Authentication failures
- Certificate errors

**Debugging steps:**
```bash
# Enable debug logging
export DEBUG=true
export LOG_LEVEL=DEBUG

# Test network connectivity
telnet backend.example.com 31337
curl -k https://backend.example.com:31337/health

# Check certificate issues
openssl s_client -connect backend.example.com:31337

# Verify API key and agent ID
cat config/agent_credentials.json
cat config/api_key.json
```

### 2. File Sync Issues

**Symptoms:**
- Files not downloading
- Constant re-downloading
- MD5 hash mismatches

**Debugging steps:**
```bash
# Check file permissions
ls -la data/wordlists/
ls -la data/rules/

# Verify network connectivity for downloads
curl -I https://backend.example.com:31337/api/files/download/wordlist/1

# Check available disk space
df -h data/

# Manual MD5 verification
md5sum data/wordlists/rockyou.txt
```

### 3. Job Execution Problems

**Symptoms:**
- Jobs not starting
- Hashcat errors
- No progress updates

**Debugging steps:**
```bash
# Check hashcat installation
which hashcat
hashcat --version

# Test hashcat manually
hashcat -m 1000 -a 3 --stdout ?d?d?d?d | head -10

# Check GPU availability
hashcat -I

# Monitor system resources
top -p $(pgrep hashcat)
nvidia-smi  # For NVIDIA GPUs
```

### 4. High Memory Usage

**Symptoms:**
- Agent consuming excessive RAM
- System becoming slow
- Out of memory errors

**Debugging steps:**
```bash
# Enable memory profiling
export DEBUG=true
go tool pprof http://localhost:6060/debug/pprof/heap

# Check for memory leaks
# Monitor over time with:
watch "ps -p $(pgrep krakenhashes-agent) -o pid,rss,vsz"

# Reduce concurrent operations
# In .env file:
KH_MAX_CONCURRENT_DOWNLOADS=1
HEARTBEAT_INTERVAL=10
```

## Debugging Tools and Utilities

### Log Analysis Scripts

Create helper scripts for log analysis:

```bash
#!/bin/bash
# debug-helper.sh

# Show only error messages
show_errors() {
    grep "\[ERROR\]" agent.log | tail -20
}

# Show WebSocket connection events
show_websocket() {
    grep -E "(WebSocket|connection|disconnect)" agent.log | tail -20
}

# Show file sync activity
show_sync() {
    grep -E "(sync|download|upload)" agent.log | tail -20
}

# Show job execution
show_jobs() {
    grep -E "(job|task|hashcat)" agent.log | tail -20
}

# Usage: ./debug-helper.sh show_errors
$1
```

### Real-time Monitoring

Monitor agent activity in real-time:

```bash
# Multi-pane monitoring with tmux
tmux new-session -d -s agent-debug

# Pane 1: Agent output
tmux send-keys -t agent-debug "tail -f agent.log" Enter

# Pane 2: System resources
tmux split-window -v -t agent-debug
tmux send-keys -t agent-debug "htop" Enter

# Pane 3: Network connections
tmux split-window -h -t agent-debug
tmux send-keys -t agent-debug "watch 'netstat -an | grep :31337'" Enter

# Attach to session
tmux attach-session -t agent-debug
```

### Configuration Validation

Validate agent configuration:

```bash
#!/bin/bash
# validate-config.sh

echo "=== Agent Configuration Validation ==="

# Check required directories
echo "Checking directories..."
[ -d "config" ] && echo "✅ config/" || echo "❌ config/ missing"
[ -d "data" ] && echo "✅ data/" || echo "❌ data/ missing"

# Check .env file
echo "Checking .env configuration..."
if [ -f ".env" ]; then
    echo "✅ .env file exists"
    grep -q "KH_HOST=" .env && echo "✅ KH_HOST set" || echo "❌ KH_HOST missing"
    grep -q "DEBUG=" .env && echo "✅ DEBUG set" || echo "❌ DEBUG missing"
else
    echo "❌ .env file missing"
fi

# Check certificates
echo "Checking certificates..."
[ -f "config/agent.crt" ] && echo "✅ Agent certificate" || echo "❌ Agent certificate missing"
[ -f "config/ca.crt" ] && echo "✅ CA certificate" || echo "❌ CA certificate missing"

# Check API key
[ -f "config/api_key.json" ] && echo "✅ API key" || echo "❌ API key missing"

echo "=== Validation Complete ==="
```

## Automated Testing and Debugging

### Unit Tests with Debug Output

Run tests with verbose output:

```bash
cd agent

# Run all tests with verbose output
go test -v ./...

# Run specific package tests
go test -v ./internal/config
go test -v ./internal/agent
go test -v ./pkg/debug

# Run tests with race detection
go test -race -v ./...

# Generate test coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

### Integration Testing

Test agent integration with a local backend:

```bash
# Start local backend for testing
cd ../backend
docker-compose -f docker-compose.dev-local.yml up -d

# Test agent connection
cd ../agent
./krakenhashes-agent --debug --host localhost:31337
```

## Performance Profiling

### CPU Profiling

Profile CPU usage during job execution:

```bash
# Start agent with profiling
go tool pprof -http=:8080 http://localhost:6060/debug/pprof/profile?seconds=30

# During heavy computation (hashcat jobs)
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=60
```

### Memory Profiling

Identify memory usage patterns:

```bash
# Heap profiling
go tool pprof -http=:8080 http://localhost:6060/debug/pprof/heap

# Allocation profiling
go tool pprof http://localhost:6060/debug/pprof/allocs
```

### Goroutine Analysis

Monitor concurrent operations:

```bash
# Goroutine dump
curl http://localhost:6060/debug/pprof/goroutine?debug=1

# Interactive analysis
go tool pprof http://localhost:6060/debug/pprof/goroutine
```

## Logging Best Practices

### Custom Debug Messages

Add debug messages to your code:

```go
import "github.com/ZerkerEOD/krakenhashes/agent/pkg/debug"

// Different log levels
debug.Debug("Detailed diagnostic: variable=%v", someVar)
debug.Info("Operation started: %s", operation)
debug.Warning("Potential issue detected: %s", issue)
debug.Error("Critical error: %v", err)
```

### Structured Logging

Organize debug output by component:

```go
// Component-specific logging
debug.Info("[CONNECTION] WebSocket connected to %s", url)
debug.Info("[SYNC] Downloaded file: %s (%d bytes)", filename, size)
debug.Info("[JOB] Task started: %s", taskID)
debug.Info("[HARDWARE] GPU detected: %s", gpuName)
```

## Contributing and Bug Reports

### Preparing Debug Information

When reporting bugs, include:

1. **Agent version and build info:**
   ```bash
   ./krakenhashes-agent --version
   ```

2. **Complete configuration:**
   ```bash
   # Sanitized .env file (remove sensitive data)
   cat .env | sed 's/\(API_KEY\|PASSWORD\)=.*/\1=***REDACTED***/'
   ```

3. **Debug logs:**
   ```bash
   # Last 100 lines with debug enabled
   DEBUG=true LOG_LEVEL=DEBUG ./krakenhashes-agent --host backend.example.com > debug.log 2>&1
   tail -100 debug.log
   ```

4. **System information:**
   ```bash
   uname -a
   lscpu | grep -E "(Architecture|CPU|Thread)"
   nvidia-smi  # If using NVIDIA GPUs
   ```

5. **Network connectivity:**
   ```bash
   curl -I https://backend.example.com:31337/health
   ```

### Debug Build for Development

Create debug builds for development:

```bash
# Clean build with debug symbols
cd agent
make clean

# Build with debug flags
go build -gcflags="all=-N -l" -ldflags="-X main.BuildMode=debug" -o debug-agent ./cmd/agent

# Run with additional debugging
./debug-agent --debug --host backend.example.com:31337
```

This debug build includes:
- No compiler optimizations
- Full symbol information
- Additional runtime checks
- Enhanced logging

## Troubleshooting Quick Reference

| Issue | Debug Steps | Key Files |
|-------|------------|-----------|
| Won't start | Check `.env`, verify paths | `.env`, `config/` |
| Connection fails | Test network, check certs | `config/ca.crt`, `config/agent.crt` |
| Auth errors | Verify API key and agent ID | `config/api_key.json`, `config/agent_credentials.json` |
| Files not syncing | Check permissions, disk space | `data/wordlists/`, `data/rules/` |
| Jobs not running | Test hashcat, check GPU | System hashcat, `nvidia-smi` |
| High memory | Enable profiling, reduce concurrency | `.env` (set lower limits) |
| Slow performance | CPU/memory profiling | pprof endpoints |

## Advanced Debugging Techniques

### Custom Debug Builds

Build with custom debug flags:

```bash
# Build with additional debugging
go build -tags debug -gcflags="all=-N -l" ./cmd/agent

# Build with memory debugging
go build -gcflags="all=-m" ./cmd/agent

# Build with race detection (development only)
go build -race ./cmd/agent
```

### Remote Debugging

Debug agent running on remote systems:

```bash
# On remote system
dlv exec ./krakenhashes-agent --listen=:2345 --headless=true --api-version=2 -- --debug

# From local system
dlv connect remote-host:2345
```

### Container Debugging

Debug agent running in containers:

```bash
# Build debug container
docker build -f Dockerfile.debug -t agent-debug .

# Run with debug enabled
docker run -e DEBUG=true -e LOG_LEVEL=DEBUG agent-debug

# Attach debugger to container
docker exec -it <container_id> dlv attach <pid>
```

This comprehensive debugging guide should help developers and advanced users effectively debug agent issues, profile performance, and contribute to the project development.