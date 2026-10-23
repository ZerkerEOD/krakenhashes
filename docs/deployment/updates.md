# Update Procedures Guide

This guide covers the procedures for updating KrakenHashes deployments, including pre-update checks, update processes, and rollback procedures.

> **Upgrading from 2.x to 3.0?** Read [Upgrading to 3.0.0 from 2.x](#upgrading-to-300-from-2x) below before you start. Always review the release notes and test updates in a non-production environment first.

!!! tip "Agents can update themselves"
    Agents installed via the launcher update **automatically** once the server is upgraded — you
    don't have to re-download and replace each binary by hand. The server-side rollout limit and
    other controls live under Admin Settings. See [Agent Auto-Update](../agent-guide/auto-update.md).
    Standalone agents (installed without the launcher) are still updated manually as described in the
    Agent Update Process below.

## Table of Contents

- [Upgrading to 3.0.0 from 2.x](#upgrading-to-300-from-2x)
- [Pre-Update Checklist](#pre-update-checklist)
- [Updating Docker Deployments](#updating-docker-deployments)
- [Database Migration Procedures](#database-migration-procedures)
- [Agent Update Process](#agent-update-process)
- [Enabling New Features After an Update](#enabling-new-features-after-an-update)
- [Rollback Procedures](#rollback-procedures)
- [Version Compatibility](#version-compatibility)
- [Post-Update Verification](#post-update-verification)
- [Troubleshooting](#troubleshooting)

## Upgrading to 3.0.0 from 2.x

3.0.0 is a major release. Existing deployments upgrade in place, but a few things need attention.

### Before you start

- **Back up the database.** Several 3.0 migrations rewrite data and one of them cannot be reversed (see below). Rolling back means restoring this backup: the 2.1.x backend refuses to start against a database that is ahead of its migrations.
- **Plan for migration time on large `hashes` tables.** Two migrations hold a write lock on `hashes` for as long as they run: one rebuilds three indexes (needed to import long Kerberos hashes), the other backfills a new `cracked_at` column on every cracked row. On tens of millions of hashes expect minutes, not seconds; the backend is not reachable until they finish. Run `VACUUM ANALYZE hashes;` afterwards.
- **The `$HEX[...]` cleanup is irreversible by design.** Passwords stored as `$HEX[...]` literals are decoded to their real bytes. The down migration is a no-op.
- **Decide what to do with 2.x agents** (next section).

### Agents

- **2.x agents keep working** against a 3.0 backend, on their API key. The backend recognises the shared bootstrap certificate they present and does not treat it as an mTLS identity. They show as "Update available" on the Agent page.
- **Auto-update cannot update them.** A 2.x binary does not understand the update command, so 3.0 deliberately never sends it to agents below 3.0.0. The recommended fix is to move each one onto the [launcher](../agent-guide/auto-update.md), which swaps the binary and keeps it current from then on; you can also replace the binary by hand (download from the Agent page or the GitHub release). Once a 3.x agent connects it migrates to per-agent mTLS automatically ([details](../reference/architecture/agent-mtls.md)).
- **Port 1337 (plain HTTP) is still needed, for agents only.** It serves the CA certificate and certificate renewal. 3.0 moved the browser's CA download to the HTTPS origin (`/ca.crt`), so users no longer need 1337, but agents do.
- **Agents that dial the backend on a public IP:** certificate addresses (SANs) are now managed in Admin → Certificates and public IPs are not accepted. Your current certificate is untouched until you reissue it; before reissuing, make sure agents reach the backend by hostname or a private/VPN address.

### Configuration

- **No new required settings.** `diff .env.example .env` shows the new optional ones: `KH_SHARE_DIR`/`KH_SHARE_DIR_HOST` and `KH_MIGRATION_DRAIN_FLOOR_SECONDS` (network-share storage), `KH_ENCRYPTION_KEY`/`KH_ALLOW_EPHEMERAL_KEY`, `KH_INSTANCE_LOCK_WAIT`/`KH_ALLOW_MULTIPLE_INSTANCES`, `KH_MIGRATE_ALLOW_AHEAD` (dev only).
- **Encryption key.** If `KH_ENCRYPTION_KEY` is unset, the backend generates one at `$KH_CONFIG_DIR/secrets/encryption.key` and refuses to start if it cannot read or create that file. `SSO_ENCRYPTION_KEY` is still honoured; never set both to different values or existing SSO secrets become unreadable.
- **One backend per database.** The backend now takes an instance lock on the database. Blue/green or rolling deployments that briefly run two backends need `KH_ALLOW_MULTIPLE_INSTANCES=true`.
- **Custom nginx configurations** (only if you mount your own): add the `map $http_upgrade $connection_upgrade` block from `docker/nginx/nginx.conf` and the `location = /ca.crt` block from the site templates, or nginx fails to start.

### Removed or changed

- **Rule splitting is gone**, replaced by keyspace splitting. The `rule_split_*`, `chunk_fluctuation_percentage` and `speedtest_timeout_seconds` settings and the preset-job "use rule splitting" flag are removed by migration. The agent-side port-swap and auth-mode settings are removed too.
- **Stricter Multi-Team access checks.** 37 endpoints that did not check team membership now do and return 404 for resources outside the caller's teams. Users who relied on that gap lose access to those resources; grant team membership instead.
- **Custom job creation returns `202 Accepted`** with status `preparing` instead of `201` when keyspace calculation runs in the background. API clients that waited for `201` should accept both.
- **`max_agents` can be clamped.** Once an admin sets `max_agents_limit`, requests above it are rejected with `400`. The default (0) keeps 2.x behaviour.
- **`GET /api/dashboard` was replaced** by the `/api/dashboard/{stats,recent-cracks,agent-health,attention,crack-trend}` endpoints.
- **Remember Me** now only pre-fills the username; it no longer extends the session.
- **Navigation moved.** Admin settings live in one hub with autosave, user settings pages are grouped, and the old routes redirect.

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
git checkout tags/v3.0.0  # Replace with target version
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

### Rehearsing Migrations First

Migrations always run on backend startup; there is no switch to turn that off. To see what a release will do to your data before upgrading production, rehearse it against a copy:

1. **Restore your backup into a scratch database**
```bash
createdb -U krakenhashes krakenhashes_rehearsal
psql -U krakenhashes -d krakenhashes_rehearsal < backup_YYYYMMDD_HHMMSS.sql
```

2. **Run the new release's migrations against it**
```bash
cd backend
DATABASE_URL=postgres://krakenhashes:password@localhost/krakenhashes_rehearsal?sslmode=disable make migrate-up
```
This also tells you how long the slow migrations take on your data.

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

The backend image ships the matching agent and launcher binaries, so once the server is upgraded the new agent version is available from the Agent page.

### Launcher-managed agents (automatic)

Agents installed through the launcher update themselves: the backend sends an update command to each idle, out-of-date agent (a few at a time, never one that is running a job), the launcher swaps the binary, health-checks the new version and rolls back if it fails. Watch progress on the Agent page or in the backend log.

The rollout limits live under Admin → Settings → Agents. See [Agent Auto-Update](../agent-guide/auto-update.md).

### Standalone agents (manual)

Agents that run without the launcher, and any 2.x agent, are updated by hand:

```bash
# On each agent machine
systemctl stop krakenhashes-agent      # or: systemctl --user stop krakenhashes-agent
# Download the new binary from Admin → Agents → Download, or from the GitHub release,
# and replace the old one.
chmod +x krakenhashes-agent
systemctl start krakenhashes-agent
```

Or reinstall the agent through the launcher so this is the last manual update.

### Checking agent versions

The Agent page lists each agent's version and flags those below the version the server expects. The expected version is also available without logging in:

```bash
curl -sk https://your-server:31337/api/public/agent/version
```

## Enabling New Features After an Update

New capabilities usually ship **opt-in and off by default**, so an update by itself changes nothing about how the system behaves — you adopt a feature deliberately, after the upgrade is healthy. That is what keeps updates safe and non-disruptive, but it also means a new feature won't appear to "just work" until you turn it on.

### How to find what's new

1. **Release notes** — the authoritative list of what changed, including any new opt-in features and the steps to enable them.
2. **`diff .env.example .env`** — the Pre-Update Checklist step (above) surfaces new environment variables. New *optional* variables are typically commented out or empty by default; copy the ones you want into your `.env`.
3. **Admin → System Settings** — most runtime features are toggled here, not in `.env`. After an upgrade, scan the settings tabs for new sections.

### Where features get enabled

| Enablement path | Examples | Notes |
|-----------------|----------|-------|
| Admin UI (no restart) | Per-agent settings, storage backend, scheduling modes | Takes effect live. |
| New `.env` variable (+ restart) | New mount paths, tuning knobs | Add to `.env`, then `docker-compose up -d` to apply. |
| Both | Network share storage | A compose/`.env` mount **plus** an Admin toggle + migration. |

### Example: adopting Network Share Storage

The network-share storage backend and per-agent storage tiers are a good model of the opt-in pattern. After upgrading to a version that includes them, nothing changes until you choose to use them:

1. **New env var** — `diff .env.example .env` shows `KH_SHARE_DIR_HOST` (and `KH_SHARE_DIR`, `KH_MIGRATION_DRAIN_FLOOR_SECONDS`). Leave them unset to keep local-disk behavior.
2. **Mount + bind** — mount your SMB/NFS share on the host, set `KH_SHARE_DIR_HOST` to that path, and restart so the compose bind takes effect.
3. **Enable + migrate** — go to **Admin → System Settings → Storage**, validate the mount, and run the one-time migration to move wordlists/rules onto the share. The migration is resumable and reversible.
4. **Per-agent tiers (optional)** — assign on-prem agents the `network_direct` or `on_demand` tier as needed.

Full details: [Storage Architecture](../admin-guide/resource-management/storage.md#network-share-storage) and the [Network Share Storage architecture](../reference/architecture/network-share-storage.md) reference. The same "check release notes → review `.env.example` diff → enable in Admin" flow applies to any opt-in feature a release introduces.

### Agent mTLS migrates automatically

Agent mutual-TLS authentication is the exception to the opt-in pattern: it turns itself on. After you upgrade a **self-signed** deployment, agents migrate from API-key to per-agent-certificate (mTLS) auth **automatically, on their next connection** — over the existing agent port, with no new port, no `.env` variable, and no Admin toggle. New agents use mTLS from registration; existing agents renew their certificate once and reconnect. API-key and mTLS agents interoperate throughout, so the migration is invisible and needs nothing from you.

- **Self-signed mode only.** In `provided`/`certbot` TLS modes there is no internal CA to issue agent certificates, so agents stay on API-key auth and nothing changes.
- **Revoke vs disable.** A new **Revoke** action (Admin → Agents → the agent's menu) is the hard kill: it invalidates an agent's credentials, severs its connection, and moves it to a Revoked list; bringing it back needs a new claim code. **Disable** is unchanged — it only pauses scheduling and the agent's credentials stay valid.

Full details: [Agent mTLS Authentication](../reference/architecture/agent-mtls.md).

!!! note "Database columns for a new feature arrive with the migration"
    Opt-in features that need new tables/columns get them automatically when migrations run on backend startup (above) — the schema is ready even while the feature stays off. You only *enable* the behavior; you don't run any extra schema step.

## Rollback Procedures

### Quick Rollback (Docker)

1. **Stop Current Services**
```bash
docker-compose down
```

2. **Restore Previous Version**
```bash
# Checkout previous version
git checkout tags/v2.1.2  # The version you upgraded from

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

The backend, frontend and database always ship together in one image, so only the agent version can differ from the server.

| Server release | Agents that can connect | Notes |
|----------------|-------------------------|-------|
| 3.0.x          | 3.0.x (mTLS), 2.1.x (API key) | 2.1.x agents work but cannot auto-update or use mTLS, loopback, slow-hash mode or network-share tiers. |
| 2.1.x          | 2.1.x, 2.0.x            | |
| 2.0.x          | 2.0.x, 1.x ≥ 1.1.0      | The 1.1.0 WebSocket protocol change is the floor. |
| 1.x            | 1.x ≥ 1.1.0             | |

> **Rule of thumb:** run agents at the same major version as the server. An older agent generally still connects, but new features are negotiated per feature and silently unavailable to it. A newer agent than the server is not supported.

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