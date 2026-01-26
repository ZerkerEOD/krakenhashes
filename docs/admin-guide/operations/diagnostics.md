# System Diagnostics

The Diagnostics page provides administrators with tools for troubleshooting, debugging, and generating support packages. It enables remote management of debug settings, log viewing, and collection of sanitized diagnostic data.

## Accessing Diagnostics

Navigate to **Admin Menu → Diagnostics** from the main navigation.

!!! info "Admin Access Required"
    The Diagnostics page requires administrator privileges. Regular users cannot access this functionality.

## System Information Panel

The System Information panel displays real-time server health metrics:

### Runtime Information

| Metric | Description |
|--------|-------------|
| Go Version | Go runtime version powering the backend |
| OS/Architecture | Operating system and CPU architecture |
| CPUs | Number of available CPU cores |
| Goroutines | Current number of active Go routines |
| Connected Agents | Number of agents currently connected via WebSocket |

### Memory Statistics

| Metric | Description |
|--------|-------------|
| Allocated | Currently allocated memory (MB) |
| Heap Allocated | Memory allocated on the heap (MB) |
| System Memory | Total memory obtained from OS (MB) |
| Heap Objects | Number of allocated heap objects |
| GC Cycles | Number of completed garbage collection cycles |

### Database Information

| Metric | Description |
|--------|-------------|
| Version | PostgreSQL version |
| Size | Total database size |
| Open Connections | Current open database connections |
| In Use | Connections currently in use |
| Idle | Idle connections in pool |
| Max Open | Maximum allowed connections |

## Server Debug Mode

Server debug mode controls the logging verbosity for the backend service.

### Enabling Server Debug Mode

1. Locate the **Server Debug Mode** toggle in the diagnostics panel
2. Click the toggle to enable/disable debug logging
3. Confirm the action in the dialog that appears

!!! warning "Connection Interruption"
    Enabling or disabling server debug mode triggers an nginx reload to apply logging configuration changes. Active connections will be briefly interrupted (~1-2 seconds).

### Debug Log Levels

When debug mode is enabled, the system captures detailed logging:

| Level | Description |
|-------|-------------|
| DEBUG | Detailed diagnostic information for developers |
| INFO | General operational messages (default when debug enabled) |
| WARNING | Potential issues that don't stop operation |
| ERROR | Serious problems that may cause failures |

!!! note "Runtime Only"
    Server debug mode is a runtime setting and resets when the server restarts. For persistent debug mode, set `DEBUG=true` and `LOG_LEVEL=DEBUG` environment variables.

## Agent Debug Management

The Agent Debug Management table shows all registered agents and their debug status.

### Agent Status Columns

| Column | Description |
|--------|-------------|
| Agent ID | Unique agent identifier |
| Debug Enabled | Whether debug logging is active |
| Level | Current log level (DEBUG, INFO, WARNING, ERROR) |
| File Logging | Whether logs are being written to disk |
| Log File Size | Size of the agent's log file |
| Buffer Count | Number of log entries in memory buffer |
| Buffer Capacity | Maximum buffer size |
| Last Updated | When the status was last reported |

### Toggling Agent Debug Mode

#### Per-Agent Toggle

1. Find the agent in the table
2. Click the toggle switch in the **Debug Enabled** column
3. The agent receives the command via WebSocket and updates immediately

#### Bulk Toggle (All Agents)

Use the **Enable All** or **Disable All** buttons above the table to toggle debug mode for all connected agents simultaneously.

### Viewing Agent Logs

1. Click the **View** button next to an agent
2. In the dialog that opens:
   - Select a **Log Level Filter** (ALL, DEBUG, INFO, WARNING, ERROR)
   - Choose **Hours Back** (1-168 hours) to retrieve
3. Click **Refresh** to fetch the latest logs

!!! tip "Log Truncation"
    Large log outputs are automatically truncated to prevent UI performance issues. The full logs are included in downloaded diagnostic packages.

### Purging Agent Logs

1. Click the **Purge** button next to an agent
2. Confirm the purge action in the dialog
3. Both the memory buffer and file logs are cleared

!!! warning "Irreversible Action"
    Purging logs cannot be undone. Ensure you've downloaded any needed logs before purging.

## Server Log Management

The Server Log Management section displays statistics for server-side logs and provides purge functionality.

### Log Statistics

The panel shows file counts and total sizes for each log category:

| Category | Files | Description |
|----------|-------|-------------|
| Backend | `backend/*.log*` | Application server logs |
| Nginx | `nginx/*.log*` | Web server access and error logs |
| PostgreSQL | `postgres/*.log*` | Database logs (if configured) |

### Purging Server Logs

Click **Purge** next to a category to clear those logs:

- **Backend**: Clears application server logs
- **Nginx**: Clears web server logs
- **PostgreSQL**: Clears database logs
- **Purge All**: Clears all server logs

!!! note "How Purging Works"
    - Active log files (`.log`) are **truncated** to preserve file handles
    - Rotated backup files (`.log.1`, `.log.2.gz`, etc.) are **deleted**
    - No service restart required - logging continues seamlessly

## Downloading Diagnostic Package

The diagnostic package is a ZIP file containing sanitized system information for troubleshooting.

### Package Contents

Every diagnostic package includes:

| File | Contents |
|------|----------|
| `system_info.json` | Server runtime info, memory stats, database metrics |
| `database_export.json` | Sanitized export of diagnostic database tables |
| `server_logs/backend/` | Backend application logs |
| `agents/debug_status.json` | Debug status for all registered agents |

### Optional Inclusions

Configure additional contents with the checkboxes:

| Option | Description |
|--------|-------------|
| Include Agent Logs | Fetch logs from all connected agents via WebSocket |
| Include Nginx Logs | Include nginx access and error logs |
| Include PostgreSQL Logs | Include database logs (if available) |
| Hours Back | How far back to retrieve agent logs (1-168 hours) |

### Sensitive Data Warning

When including agent logs or nginx logs, a warning dialog appears explaining that logs may contain:

- IP addresses from requests
- Request paths and parameters
- Timing information
- User agent strings
- Other operational data

You must acknowledge this warning before proceeding.

### Data Sanitization

The diagnostic package automatically sanitizes sensitive information:

| Data Type | Sanitization |
|-----------|--------------|
| Home paths | `/home/username/` → `/home/[USER]/` |
| JWT tokens | Full token → `[REDACTED:jwt_token]` |
| API keys | Header values → `[REDACTED:api_key:len=N]` |
| Database fields | Sensitive columns → `[REDACTED:field:len=N]` |
| Hostnames | Machine names → `[REDACTED:hostname]` |
| Agent paths | Full paths → relative paths (e.g., `logs/agent.log`) |

!!! success "Safe to Share"
    Diagnostic packages are designed to be safe for sharing with developers for support purposes. Sensitive data is automatically redacted.

### Downloading

1. Configure the desired options
2. Click **Download Diagnostics**
3. Wait for the package to be generated (progress shown for agent log collection)
4. The ZIP file downloads automatically with timestamp in filename

## Troubleshooting Tips

### Debug Mode Not Persisting After Restart

Server debug mode is a runtime-only setting. For persistent debug logging:

1. Edit your environment configuration (`.env` or `docker-compose.yml`)
2. Set `DEBUG=true` and `LOG_LEVEL=DEBUG`
3. Restart the server

### Agent Logs Not Appearing

If an agent's logs aren't showing:

- Verify the agent is connected (check **Connected Agents** count)
- Ensure debug mode is enabled on the agent
- Check that the agent has sufficient disk space for file logging
- Confirm the agent is running a version that supports remote log retrieval (v1.4.0+)

### Nginx Logs Empty or Not Updating

- Nginx logs are captured via supervisord stdout/stderr capture
- After purging, log files exist but may be empty until new requests arrive
- Verify nginx is running: check Docker container logs
- Debug mode must be enabled for verbose nginx logging

### Large Diagnostic Package Downloads Timing Out

For environments with many agents or large log files:

- Reduce the **Hours Back** setting
- Disable **Include Agent Logs** if not needed
- Download logs in smaller batches

## Security Considerations

!!! warning "Handle With Care"
    While diagnostic packages are sanitized, they still contain operational data that could be useful to attackers. Follow these guidelines:

1. **Delete After Use**: Remove diagnostic packages once troubleshooting is complete
2. **Secure Transfer**: Use encrypted channels when sharing diagnostic packages
3. **Access Control**: Only administrators can access the diagnostics page
4. **Audit Trail**: Diagnostic downloads may be logged for security auditing
5. **Review Contents**: Before sharing, verify the sanitization meets your security requirements

## API Endpoints

For automation and scripting, the diagnostics API endpoints are available:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/admin/diagnostics/system-info` | GET | Get system information |
| `/api/admin/diagnostics/agents` | GET | Get all agent debug statuses |
| `/api/admin/diagnostics/agents/{id}/debug` | POST | Toggle agent debug mode |
| `/api/admin/diagnostics/agents/{id}/logs` | GET | Get agent logs |
| `/api/admin/diagnostics/agents/{id}/logs` | DELETE | Purge agent logs |
| `/api/admin/diagnostics/server/debug` | GET/POST | Get/toggle server debug mode |
| `/api/admin/diagnostics/logs/stats` | GET | Get server log statistics |
| `/api/admin/diagnostics/logs/{dir}` | DELETE | Purge server logs |
| `/api/admin/diagnostics/download` | GET | Download diagnostic package |

All endpoints require admin authentication via JWT token.
