# Update Procedures Guide

This guide covers the procedures for updating KrakenHashes deployments, including pre-update checks, update processes, and rollback procedures.

> **⚠️ IMPORTANT**: KrakenHashes is currently in **v0.1.0-alpha**. Breaking changes are expected between versions. Always review release notes and test updates in a non-production environment first.

!!! tip "Agents can update themselves"
    Agents installed via the launcher update **automatically** once the server is upgraded — you
    don't have to re-download and replace each binary by hand. The server-side rollout limit and
    other controls live under Admin Settings. See [Agent Auto-Update](../agent-guide/auto-update.md).
    Standalone agents (installed without the launcher) are still updated manually as described in the
    Agent Update Process below.

## Table of Contents

- [Pre-Update Checklist](#pre-update-checklist)
- [Updating Docker Deployments](#updating-docker-deployments)
- [Database Migration Procedures](#database-migration-procedures)
- [Agent Update Process](#agent-update-process)
- [Rollback Procedures](#rollback-procedures)
- [Version Compatibility](#version-compatibility)
- [Post-Update Verification](#post-update-verification)
- [Troubleshooting](#troubleshooting)

## Pre-Update Checklist

Before beginning any update, complete the following checklist:

### 1. Review Release Notes
- [ ] Check the [release notes](https://github.com/yourusername/krakenhashes/releases) for breaking changes
- [ ] Review migration scripts included in the release
- [ ] Identify any configuration changes required
- [ ] Note any new environment variables or removed features

### 2. Backup Current System
```bash
# Backup database
docker-compose exec postgres pg_dump -U krakenhashes krakenhashes > backup_$(date +%Y%m%d_%H%M%S).sql

# Backup configuration files
cp -r /opt/krakenhashes/.env backup/.env.$(date +%Y%m%d_%H%M%S)
cp -r /opt/kh-backend/config backup/config_$(date +%Y%m%d_%H%M%S)

# Backup data directory
tar -czf backup/data_$(date +%Y%m%d_%H%M%S).tar.gz /opt/kh-backend/data
```

### 3. Check System Health
```bash
# Check service status
docker-compose ps

# Verify no active jobs
docker-compose exec backend curl -s http://localhost:8080/api/v1/health

# Check agent connections
docker-compose logs backend | grep -i "agent.*connected" | tail -n 20
```

### 4. Document Current Version
```bash
# Record current versions
docker-compose exec backend /app/krakenhashes --version > current_version.txt
docker-compose exec postgres psql -U krakenhashes -c "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1;"
```

### 5. Plan Maintenance Window
- [ ] Notify users of planned downtime
- [ ] Schedule update during low-activity period
- [ ] Prepare rollback plan
- [ ] Assign responsible personnel

## Updating Docker Deployments

### Standard Update Process

1. **Stop Current Services**
```bash
cd /opt/krakenhashes
docker-compose down
```

2. **Pull Latest Code**
```bash
git fetch origin
git checkout tags/v0.2.0  # Replace with target version
# OR for latest development
git checkout master
git pull origin master
```

3. **Review Configuration Changes**
```bash
# Check for new environment variables
diff .env.example .env

# Apply any new required variables
nano .env
```

4. **Build and Start Services**
```bash
# Build new images
docker-compose build --no-cache

# Start services
docker-compose up -d

# Monitor startup
docker-compose logs -f
```

### Incremental Updates (Development)

For development environments with frequent updates:

```bash
# Quick rebuild and restart
docker-compose down
git pull origin master
docker-compose up -d --build backend
docker-compose logs -f backend
```

## Database Migration Procedures

### Automatic Migrations

Migrations are automatically applied on backend startup. Monitor the process:

```bash
# Watch migration logs
docker-compose logs backend | grep -i migration

# Verify migration status
docker-compose exec postgres psql -U krakenhashes -d krakenhashes -c "SELECT version, dirty FROM schema_migrations ORDER BY version DESC LIMIT 5;"
```

### Manual Migration Control

For production environments requiring manual migration control:

1. **Disable Auto-Migration**
```bash
# In .env, set:
AUTO_MIGRATE=false
```

2. **Apply Migrations Manually**
```bash
cd backend

# View pending migrations
make migrate-status

# Apply all pending migrations
make migrate-up

# Apply specific version
migrate -path db/migrations -database "$DATABASE_URL" goto 20240115120000
```

3. **Verify Migration Success**
```bash
# Check migration history
docker-compose exec postgres psql -U krakenhashes -d krakenhashes -c "SELECT * FROM schema_migrations;"

# Test critical tables
docker-compose exec postgres psql -U krakenhashes -d krakenhashes -c "\dt"
```

### Handling Failed Migrations

If a migration fails:

1. **Check Migration Status**
```bash
# Check if migration is dirty
docker-compose exec postgres psql -U krakenhashes -d krakenhashes -c "SELECT * FROM schema_migrations WHERE dirty = true;"
```

2. **Fix Dirty Migration**
```bash
# Force version (use with caution)
cd backend
migrate -path db/migrations -database "$DATABASE_URL" force 20240115120000

# Then retry
make migrate-up
```

## Agent Update Process

### Coordinated Agent Updates

1. **Prepare New Agent Binary**
```bash
# New agent binaries are typically included in backend updates
# Verify new version is available
docker-compose exec backend ls -la /data/krakenhashes/binaries/
```

2. **Notify Connected Agents**
```bash
# Agents will receive update notifications via WebSocket
# Monitor agent update status in backend logs
docker-compose logs -f backend | grep -i "agent.*update"
```

3. **Manual Agent Update** (if auto-update fails)
```bash
# On each agent machine
cd /path/to/agent
./update.sh  # If provided

# Or manually:
systemctl stop krakenhashes-agent
wget https://your-server/api/v1/binaries/agent/latest -O krakenhashes-agent
chmod +x krakenhashes-agent
systemctl start krakenhashes-agent
```

### Agent Compatibility Check

Before updating:
```bash
# Check agent versions
docker-compose exec backend curl -s http://localhost:8080/api/v1/agents | jq '.[] | {id, version, last_seen}'

# Verify compatibility matrix in release notes
```

## Rollback Procedures

### Quick Rollback (Docker)

1. **Stop Current Services**
```bash
docker-compose down
```

2. **Restore Previous Version**
```bash
# Checkout previous version
git checkout tags/v0.1.0  # Previous version

# Restore configuration
cp backup/.env.20240115_120000 .env

# Rebuild and start
docker-compose up -d --build
```

3. **Restore Database** (if schema changed)
```bash
# Stop backend to prevent connections
docker-compose stop backend

# Restore database
docker-compose exec -T postgres psql -U krakenhashes -d krakenhashes < backup_20240115_120000.sql

# Restart backend
docker-compose start backend
```

### Rollback with Data Preservation

For rollbacks that need to preserve new data:

1. **Export New Data**
```bash
# Export specific tables with new data
docker-compose exec postgres pg_dump -U krakenhashes -t job_executions -t hashes --data-only krakenhashes > new_data.sql
```

2. **Perform Rollback**
Follow standard rollback procedure

3. **Reimport Preserved Data**
```bash
# Carefully reimport compatible data
docker-compose exec -T postgres psql -U krakenhashes -d krakenhashes < new_data.sql
```

## Version Compatibility

### Compatibility Matrix

| Component | Backend | Agent | Frontend | Database Schema |
|-----------|---------|-------|----------|-----------------|
| v0.1.0    | 0.1.0   | 0.1.0 | 0.1.0    | 19              |
| v0.2.0    | 0.2.0   | 0.1.0-0.2.0 | 0.2.0 | 22         |
| v1.0.0    | 1.0.0   | 1.0.0 | 1.0.0    | 30              |

> **Note**: During alpha, assume all components must be updated together unless release notes specify otherwise.

### Checking Compatibility

```bash
# Check all component versions
docker-compose exec backend /app/krakenhashes --version
docker-compose exec backend curl -s http://localhost:8080/api/v1/system/info

# Check schema version
docker-compose exec postgres psql -U krakenhashes -c "SELECT MAX(version) FROM schema_migrations;"
```

## Post-Update Verification

### 1. System Health Checks

```bash
# Backend health
curl -s https://localhost:8443/api/v1/health | jq .

# Database connectivity
docker-compose exec backend curl -s http://localhost:8080/api/v1/system/db-check

# Frontend accessibility
curl -I https://localhost:8443
```

### 2. Functional Verification

```bash
# Test authentication
curl -X POST https://localhost:8443/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"your-password"}'

# Check agent connectivity
docker-compose logs backend | grep -i "websocket.*agent" | tail -n 10

# Verify job creation (with auth token)
curl -X GET https://localhost:8443/api/v1/jobs \
  -H "Authorization: Bearer $TOKEN"
```

### 3. Data Integrity Checks

```sql
-- Connect to database
docker-compose exec postgres psql -U krakenhashes -d krakenhashes

-- Check critical tables
SELECT COUNT(*) FROM users;
SELECT COUNT(*) FROM agents;
SELECT COUNT(*) FROM hashlists;
SELECT COUNT(*) FROM job_executions WHERE status = 'running';

-- Verify migrations
SELECT * FROM schema_migrations ORDER BY version DESC LIMIT 5;
```

### 4. Performance Verification

```bash
# Check resource usage
docker stats --no-stream

# Monitor logs for errors
docker-compose logs --tail=100 backend | grep -i error

# Check response times
time curl -s https://localhost:8443/api/v1/health
```

## Troubleshooting

### Common Update Issues

#### 1. Migration Failures
```bash
# Check migration logs
docker-compose logs backend | grep -E "(migration|migrate)"

# Reset dirty migration
cd backend
migrate -path db/migrations -database "$DATABASE_URL" force VERSION_NUMBER
```

#### 2. Container Start Failures
```bash
# Check detailed logs
docker-compose logs backend
docker-compose logs postgres

# Verify file permissions
ls -la /opt/kh-backend/data

# Check disk space
df -h
```

#### 3. Agent Connection Issues
```bash
# Restart agent connections
docker-compose restart backend

# Check WebSocket logs
docker-compose logs backend | grep -i websocket

# Verify agent API keys are still valid
docker-compose exec postgres psql -U krakenhashes -c "SELECT * FROM agents WHERE active = true;"
```

#### 4. Frontend Loading Issues
```bash
# Clear browser cache and cookies
# Rebuild frontend
docker-compose up -d --build app

# Check nginx logs
docker-compose logs app
```

### Emergency Procedures

If the system becomes unresponsive:

1. **Preserve Logs**
```bash
docker-compose logs > emergency_logs_$(date +%Y%m%d_%H%M%S).txt
```

2. **Force Stop**
```bash
docker-compose down -v
```

3. **Clean Start**
```bash
docker system prune -f
docker-compose up -d --build
```

4. **Contact Support**
- Provide emergency logs
- Document steps leading to failure
- Note any error messages

## Best Practices

1. **Always Test Updates**
   - Use staging environment
   - Test core functionality
   - Verify agent connectivity

2. **Schedule Wisely**
   - Update during maintenance windows
   - Avoid updates during active cracking jobs
   - Coordinate with users

3. **Document Everything**
   - Record version changes
   - Note configuration modifications
   - Log any issues encountered

4. **Monitor Post-Update**
   - Watch logs for 24 hours
   - Check performance metrics
   - Gather user feedback

5. **Maintain Backups**
   - Keep multiple backup versions
   - Test restore procedures regularly
   - Store backups securely

## Conclusion

Updating KrakenHashes requires careful planning and execution, especially during the alpha phase. Always prioritize data safety and system availability. When in doubt, test in a non-production environment first.

For additional support or questions about specific update scenarios, consult the [documentation](https://docs.krakenhashes.com) or contact the development team.