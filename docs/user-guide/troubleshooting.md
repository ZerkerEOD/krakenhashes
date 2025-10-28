# KrakenHashes Troubleshooting Guide

This guide helps you resolve common issues when using KrakenHashes. If your issue isn't covered here, please contact support with relevant error messages and logs.

## Table of Contents

1. [Installation Issues](#installation-issues)
2. [Login and Authentication Problems](#login-and-authentication-problems)
3. [Job Creation and Execution Issues](#job-creation-and-execution-issues)
4. [Agent Connection Problems](#agent-connection-problems)
5. [Performance Issues](#performance-issues)
6. [File Upload Errors](#file-upload-errors)
7. [Results Not Appearing](#results-not-appearing)
8. [How to Check Logs and Get Help](#how-to-check-logs-and-get-help)

## Installation Issues

### Docker Compose Fails to Start

**Error**: `docker-compose: command not found`
- **Solution**: Install Docker Compose following the official documentation for your OS

**Error**: `Cannot connect to the Docker daemon`
- **Solution**: 
  - Ensure Docker is running: `sudo systemctl start docker`
  - Add your user to the docker group: `sudo usermod -aG docker $USER`
  - Log out and back in for group changes to take effect

**Error**: `Port 8080 is already in use`
- **Solution**:
  - Check what's using the port: `sudo lsof -i :8080`
  - Stop the conflicting service or change KrakenHashes ports in `docker-compose.yml`

### Database Migration Failures

**Error**: `pq: password authentication failed for user`
- **Solution**: 
  - Check your `DATABASE_URL` environment variable
  - Ensure PostgreSQL credentials match in `docker-compose.yml`
  - Try resetting with: `docker-compose down -v` then `docker-compose up -d`

**Error**: `migration failed: table already exists`
- **Solution**:
  - Reset migrations: `docker-compose exec backend make migrate-down`
  - Clean database: `docker-compose down -v`
  - Restart: `docker-compose up -d`

### SSL/TLS Certificate Issues

**Error**: `NET::ERR_CERT_AUTHORITY_INVALID`
- **Solution**:
  - For self-signed certificates, follow the installation guide in `docs/SSL_TLS_SETUP.md`
  - Import the CA certificate to your browser/OS trust store
  - For production, use proper certificates with `KH_TLS_MODE=provided`

## Login and Authentication Problems

### Cannot Log In

**Error**: `Invalid credentials`
- **Solution**:
  - Verify username and password are correct
  - Check if account is active (admin can verify)
  - Try resetting password through admin

**Error**: `Token expired`
- **Solution**:
  - Clear browser cookies/localStorage
  - Log in again
  - If persistent, check system time synchronization

### Multi-Factor Authentication Issues

**Error**: `Invalid TOTP code`
- **Solution**:
  - Ensure device time is synchronized
  - Verify you're using the correct authenticator app
  - Try backup codes if available
  - Contact admin to reset MFA

**Error**: `Email verification code not received`
- **Solution**:
  - Check spam/junk folder
  - Verify email address is correct in profile
  - Check backend logs for SMTP errors
  - Contact admin to check email configuration

### Session Timeout

**Issue**: Logged out unexpectedly
- **Solution**:
  - Check JWT token expiration settings
  - Enable "Remember Me" during login
  - Check for network connectivity issues

## Job Creation and Execution Issues

### Cannot Create Job

**Error**: `No hashlist selected`
- **Solution**:
  - Upload a hashlist first via Hashlists page
  - Ensure hashlist contains valid hashes
  - Check hashlist format matches selected hash type

**Error**: `No available agents`
- **Solution**:
  - Verify at least one agent is connected
  - Check agent status on Agents page
  - Ensure agents have required capabilities

**Error**: `Invalid workflow configuration`
- **Solution**:
  - Verify all required fields are filled
  - Check attack mode parameters are valid
  - Ensure selected wordlists/rules exist

### Job Stuck in Pending

**Issue**: Job never starts
- **Causes**:
  - No agents available with required capabilities
  - Agent offline or disconnected
  - Resource constraints on agent
- **Solution**:
  - Check agent status and capabilities
  - Verify agent GPU requirements match job
  - Check agent logs for errors

**Note (v1.2.1+)**: If an agent crashes while working on your job, the system automatically returns the unfinished work to the pool and reassigns it to another available agent. No manual intervention is required.

### Job Failed Immediately

**Error**: `Hashcat execution failed`
- **Solution**:
  - Check job logs for specific hashcat errors
  - Verify hash format matches selected type
  - Ensure wordlists/rules are accessible
  - Check agent has sufficient disk space

## Agent Connection Problems

### Agent Won't Connect

**Error**: `websocket: bad handshake`
- **Solution**:
  - Verify backend URL in agent config
  - Check firewall allows WebSocket connections
  - Ensure SSL certificates are trusted by agent

**Error**: `Invalid API key`
- **Solution**:
  - Regenerate API key from agent settings
  - Update agent configuration with new key
  - Restart agent after configuration change

**Error**: `Claim code invalid or expired`
- **Solution**:
  - Generate new claim code from UI
  - Use claim code within 15 minutes
  - Ensure claim code hasn't been used already

### Agent Keeps Disconnecting

**Issue**: Frequent reconnections
- **Causes**:
  - Network instability
  - Firewall/proxy timeout settings
  - Backend overload
- **Solution**:
  - Check network connectivity
  - Increase WebSocket timeout settings
  - Monitor backend resource usage

**Note (v1.2.1+)**: Agent disconnections are now handled gracefully. If an agent crashes during high-volume cracking, the crack batching system prevents data loss and automatically recovers the connection when the agent reconnects. Your job progress will be preserved.

### Agent Not Detecting GPUs

**Error**: `No GPUs detected`
- **Solution**:
  - Verify GPU drivers are installed:
    - NVIDIA: `nvidia-smi`
    - AMD: `rocm-smi`
    - Intel: Check oneAPI installation
  - Run agent with sudo if needed
  - Check GPU is not in exclusive mode

## Performance Issues

### Slow Hash Cracking

**Issue**: Lower than expected hash rates
- **Causes**:
  - Thermal throttling
  - Incorrect workload tuning
  - CPU bottleneck
- **Solution**:
  - Monitor GPU temperature
  - Adjust workload profile in job settings
  - Ensure adequate cooling
  - Check hashcat benchmark mode

### High Memory Usage

**Issue**: System running out of memory
- **Solution**:
  - Reduce wordlist buffer size
  - Split large hashlists
  - Use rule-based attacks instead of large wordlists
  - Monitor with `docker stats`

### Database Performance

**Issue**: Slow query responses
- **Solution**:
  - Check database indexes are created
  - Monitor with `docker-compose logs postgres`
  - Consider increasing PostgreSQL resources
  - Clean up old completed jobs

## File Upload Errors

### Upload Fails

**Error**: `Request entity too large`
- **Solution**:
  - Check file size limits in nginx config
  - Split large files into smaller chunks
  - Use compression for text files

**Error**: `Invalid file format`
- **Solution**:
  - Verify file format matches expected type:
    - Hashlists: One hash per line
    - Wordlists: Plain text, one word per line
    - Rules: Hashcat rule format
  - Remove any special characters or BOM

**Error**: `Permission denied`
- **Solution**:
  - Check backend data directory permissions
  - Ensure Docker volumes are writable
  - Verify disk space available

### Files Not Appearing

**Issue**: Uploaded files not visible
- **Solution**:
  - Refresh the page
  - Check upload completed successfully
  - Verify file processing logs
  - Check file storage directory

## Results Not Appearing

### Cracked Passwords Not Showing

**Issue**: Job shows progress but no results
- **Causes**:
  - Results not yet synced
  - Database write issues
  - Display filtering
- **Solution**:
  - Wait for sync interval (usually 30 seconds)
  - Check job logs for errors
  - Verify database connectivity
  - Check results filter settings

### Export Not Working

**Error**: `Export failed`
- **Solution**:
  - Check browser download permissions
  - Try different export format
  - Verify results exist to export
  - Check browser console for errors

### Statistics Incorrect

**Issue**: Progress/statistics don't match
- **Solution**:
  - Force refresh the page
  - Check for duplicate hashes
  - Verify job status is updated
  - Review calculation logic in logs

## How to Check Logs and Get Help

### Accessing Logs

#### Docker Logs
```bash
# View all logs
docker-compose logs

# View specific service logs
docker-compose logs -f backend    # Backend logs
docker-compose logs -f postgres   # Database logs
docker-compose logs -f app        # Frontend/nginx logs

# Save logs to file
docker-compose logs backend > backend.log
```

#### Log File Locations
- Backend: `/home/zerkereod/Programming/passwordCracking/kh-backend/logs/krakenhashes/backend/`
- PostgreSQL: `/home/zerkereod/Programming/passwordCracking/kh-backend/logs/krakenhashes/postgres/`
- Nginx: `/home/zerkereod/Programming/passwordCracking/kh-backend/logs/krakenhashes/nginx/`

#### Agent Logs
```bash
# On agent machine
tail -f /var/log/krakenhashes-agent.log

# Or check systemd
journalctl -u krakenhashes-agent -f
```

### What to Include When Reporting Issues

1. **Error Messages**
   - Exact error text
   - Screenshot if UI issue
   - Time when error occurred

2. **Environment Information**
   - KrakenHashes version
   - Browser type and version
   - Operating system
   - Docker version

3. **Steps to Reproduce**
   - What you were trying to do
   - Exact steps taken
   - Expected vs actual behavior

4. **Relevant Logs**
   - Error entries from logs
   - Stack traces if available
   - Related warning messages

### Getting Help

1. **Check Documentation**
   - Review relevant guides in `/docs`
   - Check CLAUDE.md for development info
   - Review API documentation

2. **Search Known Issues**
   - Check GitHub issues
   - Review release notes
   - Search error messages

3. **Contact Support**
   - Email: support@krakenhashes.com
   - Include issue report with details above
   - Provide job IDs if relevant

### Debug Mode

Enable debug logging for more details:
```bash
# Backend
export LOG_LEVEL=debug

# Agent
krakenhashes-agent --debug

# Frontend
# Open browser developer console
```

### Common Quick Fixes

1. **Restart Services**
   ```bash
   docker-compose restart backend
   ```

2. **Clear Browser Cache**
   - Hard refresh: Ctrl+Shift+R (Cmd+Shift+R on Mac)
   - Clear site data in browser settings

3. **Reset Database Connection**
   ```bash
   docker-compose restart postgres backend
   ```

4. **Verify Connectivity**
   ```bash
   # Test backend
   curl -k https://localhost:8080/api/v1/health

   # Test database
   docker-compose exec postgres pg_isready
   ```

## Prevention Tips

1. **Regular Maintenance**
   - Monitor disk space
   - Clean old job data
   - Update regularly
   - Backup database

2. **Performance Monitoring**
   - Use `docker stats`
   - Monitor agent resources
   - Set up alerting

3. **Security Best Practices**
   - Keep software updated
   - Use strong passwords
   - Enable MFA
   - Regular security audits