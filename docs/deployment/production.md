# Production Best Practices Guide

This guide provides comprehensive recommendations for deploying and operating KrakenHashes in production environments. Following these practices ensures security, reliability, performance, and compliance for your password cracking infrastructure.

## Table of Contents

1. [Infrastructure Requirements and Recommendations](#infrastructure-requirements-and-recommendations)
2. [Security Hardening Checklist](#security-hardening-checklist)
3. [Performance Optimization](#performance-optimization)
4. [High Availability Setup](#high-availability-setup)
5. [Monitoring and Alerting](#monitoring-and-alerting)
6. [Backup and Disaster Recovery](#backup-and-disaster-recovery)
7. [Compliance Considerations](#compliance-considerations)

## Infrastructure Requirements and Recommendations

### Hardware Requirements

#### Backend Server (Minimum)
- **CPU**: 8 cores (16+ recommended for large deployments)
- **RAM**: 16GB (32GB+ recommended)
- **Storage**: 
  - System: 50GB SSD
  - Data: 500GB+ SSD (scales with hashlist/wordlist size)
  - Database: 100GB+ SSD with high IOPS
- **Network**: 1Gbps connection minimum

#### Backend Server (Recommended)
- **CPU**: 16-32 cores (AMD EPYC or Intel Xeon)
- **RAM**: 64GB ECC memory
- **Storage**:
  - System: 2x 250GB SSD in RAID 1
  - Data: 2TB+ NVMe SSD array (RAID 10)
  - Database: Dedicated 500GB+ enterprise SSD
- **Network**: 10Gbps connection for agent communication

#### Agent Hardware (Per Agent)
- **CPU**: 8+ cores for hashcat coordination
- **RAM**: 16GB minimum (32GB+ for large wordlists)
- **GPU**: NVIDIA RTX 3090/4090 or better
- **Storage**: 250GB SSD for local caching
- **Network**: 1Gbps stable connection

### Network Architecture

```
Internet
    │
    ├─── Firewall/WAF
    │         │
    │    Load Balancer (Optional for HA)
    │         │
    │    ┌────┴────┐
    │    │ Backend │ ←── Port 31337 (HTTPS API)
    │    │ Server  │ ←── Port 1337 (HTTP CA cert)
    │    └────┬────┘
    │         │
    ├─────────┴──── PostgreSQL (Port 5432)
    │
    └─── Agent Network ←── WebSocket connections
```

### Firewall Rules

#### Inbound Rules
```
# Public Access
443/tcp  → Load Balancer       # HTTPS (if using reverse proxy)
31337/tcp → Backend Server      # HTTPS API
1337/tcp  → Backend Server      # CA Certificate endpoint

# Internal Only
5432/tcp  → PostgreSQL          # Database (restrict to backend only)
22/tcp    → All Servers         # SSH (restrict source IPs)
```

#### Outbound Rules
```
443/tcp   → Internet            # Updates, external services
80/tcp    → Internet            # Package updates
53/udp    → DNS Servers         # DNS resolution
123/udp   → NTP Servers         # Time synchronization
```

### Storage Recommendations

#### File System Layout
```
/var/lib/krakenhashes/          # Main data directory
├── binaries/                   # Hashcat binaries (10GB)
├── wordlists/                  # Wordlist storage (100GB+)
│   ├── general/               
│   ├── specialized/           
│   ├── targeted/              
│   └── custom/                
├── rules/                      # Rule files (10GB)
│   ├── hashcat/               
│   ├── john/                  
│   └── custom/                
├── hashlists/                  # User hashlists (100GB+)
└── hashlist_uploads/           # Temporary uploads (50GB)

/var/lib/postgresql/            # Database files (separate disk)
/var/log/krakenhashes/          # Application logs
/backup/krakenhashes/           # Backup storage (separate disk/NAS)
```

#### Storage Best Practices
- Use separate disks/volumes for database, data, and backups
- Implement RAID for redundancy (RAID 10 recommended)
- Monitor disk usage and set alerts at 80% capacity
- Use SSD/NVMe for database and frequently accessed data
- For large wordlist/rule corpora, use the built-in [network share storage](../admin-guide/resource-management/storage.md#network-share-storage) backend (SMB/NFS) so servers and select agents share one copy instead of each holding the full set. Hashlists, binaries, and uploads stay on local disk.

## Security Hardening Checklist

### Authentication and Access Control

#### 1. Strong Authentication Configuration
```bash
# Configure in Admin Panel → Authentication Settings

✓ Minimum Password Length: 20 characters
✓ Require Uppercase: Enabled
✓ Require Lowercase: Enabled
✓ Require Numbers: Enabled
✓ Require Special Characters: Enabled
✓ Maximum Failed Login Attempts: 3
✓ Account Lockout Duration: 30 minutes
✓ JWT Token Expiry: 15 minutes (production)
```

#### 2. Multi-Factor Authentication (MFA)
```bash
✓ Require MFA for All Users: Enabled
✓ Allowed Methods:
  - Email Authentication: Enabled (backup only)
  - Authenticator Apps: Enabled (primary)
✓ Email Code Validity: 5 minutes
✓ Code Cooldown Period: 2 minutes
✓ Maximum Code Attempts: 3
✓ Number of Backup Codes: 10
```

#### 3. User Account Security
- Enforce unique usernames (no shared accounts)
- Implement role-based access control (RBAC)
- Regular access reviews (quarterly)
- Disable default accounts
- Audit privileged account usage

### Network Security

#### 1. TLS/SSL Configuration
```bash
# Production TLS Setup (use provided certificates)
export KH_TLS_MODE=provided
export KH_TLS_CERT_PATH=/etc/krakenhashes/certs/server.crt
export KH_TLS_KEY_PATH=/etc/krakenhashes/certs/server.key
export KH_TLS_CA_PATH=/etc/krakenhashes/certs/ca.crt

# Or use Let's Encrypt
export KH_TLS_MODE=certbot
export KH_CERTBOT_DOMAIN=krakenhashes.example.com
export KH_CERTBOT_EMAIL=admin@example.com
```

#### 2. Network Hardening
```bash
# Disable unnecessary services
systemctl disable avahi-daemon
systemctl disable cups
systemctl disable bluetooth

# Configure iptables/firewalld
firewall-cmd --permanent --add-service=https
firewall-cmd --permanent --add-port=31337/tcp
firewall-cmd --permanent --add-port=1337/tcp
firewall-cmd --permanent --remove-service=ssh # Use specific source IPs
firewall-cmd --reload

# Enable fail2ban for SSH and API endpoints
apt-get install fail2ban
```

#### 3. API Security
- Implement rate limiting (100 requests/minute per IP)
- Enable CORS with specific allowed origins
- Validate all input data
- Use prepared statements for database queries
- Implement request signing for agent communication

### Database Security

#### 1. PostgreSQL Hardening
```sql
-- Restrict connections
ALTER SYSTEM SET listen_addresses = 'localhost';
ALTER SYSTEM SET max_connections = 200;

-- Enable SSL
ALTER SYSTEM SET ssl = on;
ALTER SYSTEM SET ssl_cert_file = '/etc/postgresql/server.crt';
ALTER SYSTEM SET ssl_key_file = '/etc/postgresql/server.key';

-- Configure authentication
-- Edit pg_hba.conf
hostssl krakenhashes krakenhashes 127.0.0.1/32 scram-sha-256
hostssl krakenhashes krakenhashes ::1/128 scram-sha-256

-- Set strong passwords
ALTER USER krakenhashes WITH PASSWORD 'use-a-very-strong-password-here';
ALTER USER postgres WITH PASSWORD 'another-very-strong-password';

-- Revoke unnecessary permissions
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
```

#### 2. Database Access Control
- Use application-specific database users
- Implement least privilege principle
- Regular password rotation (90 days)
- Audit database access logs
- Encrypt sensitive columns (future feature)

### File System Security

#### 1. Directory Permissions
```bash
# Set proper ownership
chown -R krakenhashes:krakenhashes /var/lib/krakenhashes
chown -R postgres:postgres /var/lib/postgresql

# Set restrictive permissions
chmod 750 /var/lib/krakenhashes
chmod 750 /var/lib/krakenhashes/binaries
chmod 750 /var/lib/krakenhashes/wordlists
chmod 750 /var/lib/krakenhashes/rules
chmod 750 /var/lib/krakenhashes/hashlists
chmod 1777 /var/lib/krakenhashes/hashlist_uploads  # Sticky bit for uploads

# Protect configuration
chmod 600 /etc/krakenhashes/config.env
chmod 700 /etc/krakenhashes/certs
chmod 600 /etc/krakenhashes/certs/*
```

#### 2. File Integrity Monitoring
```bash
# Install AIDE or similar
apt-get install aide
aide --init
aide --check

# Monitor critical files
/usr/local/bin/krakenhashes
/etc/krakenhashes/*
/var/lib/krakenhashes/binaries/*
```

### Container Security (Docker Deployments)

#### 1. Docker Hardening
```yaml
# docker-compose.yml security additions
services:
  backend:
    security_opt:
      - no-new-privileges:true
    read_only: true
    tmpfs:
      - /tmp
      - /var/run
    cap_drop:
      - ALL
    cap_add:
      - NET_BIND_SERVICE
    user: "1000:1000"
```

#### 2. Image Security
```bash
# Scan images for vulnerabilities
docker scan krakenhashes/backend:latest

# Use specific versions, not 'latest'
image: krakenhashes/backend:v0.1.0-alpha

# Sign images
export DOCKER_CONTENT_TRUST=1
```

## Performance Optimization

### Database Optimization

#### 1. PostgreSQL Tuning
```sql
-- Memory settings (for 64GB RAM server)
ALTER SYSTEM SET shared_buffers = '16GB';
ALTER SYSTEM SET effective_cache_size = '48GB';
ALTER SYSTEM SET maintenance_work_mem = '2GB';
ALTER SYSTEM SET work_mem = '256MB';

-- Connection pooling
ALTER SYSTEM SET max_connections = 200;
ALTER SYSTEM SET max_prepared_transactions = 200;

-- Write performance
ALTER SYSTEM SET checkpoint_completion_target = 0.9;
ALTER SYSTEM SET wal_buffers = '16MB';
ALTER SYSTEM SET default_statistics_target = 100;

-- Query optimization
ALTER SYSTEM SET random_page_cost = 1.1;  -- For SSD
ALTER SYSTEM SET effective_io_concurrency = 200;  -- For SSD
```

#### 2. Index Optimization
```sql
-- Analyze query patterns and create appropriate indexes
CREATE INDEX CONCURRENTLY idx_hashes_hashlist_cracked 
    ON hashes(hashlist_id, is_cracked);
    
CREATE INDEX CONCURRENTLY idx_job_tasks_status 
    ON job_tasks(job_execution_id, status);
    
CREATE INDEX CONCURRENTLY idx_agent_performance_metrics_lookup 
    ON agent_performance_metrics(agent_id, metric_type, timestamp);

-- Regular maintenance
VACUUM ANALYZE;
REINDEX CONCURRENTLY;
```

### Application Performance

#### 1. Backend Optimization
```bash
# Environment variables for performance
export GOMAXPROCS=16                    # Match CPU cores
export KH_HASHLIST_BATCH_SIZE=5000      # Increase for better throughput
export KH_DB_MAX_OPEN_CONNS=50          # Database connection pool
export KH_DB_MAX_IDLE_CONNS=25
export KH_WEBSOCKET_BUFFER_SIZE=8192    # Larger WebSocket buffers
```

#### 2. Caching Strategy
- Implement Redis for session caching
- Cache agent benchmark results (24 hours)
- Cache frequently accessed wordlist metadata
- Use CDN for static assets

#### 3. Job Processing Optimization
```bash
# Configure job chunking for optimal performance
Rule Splitting Threshold: 10000         # Split large rule files
Keyspace Chunk Size: 1 hour            # Balanced chunk duration
Maximum Concurrent Jobs: 100            # Per-agent limit
Task Dispatch Batch Size: 10            # Parallel task dispatch
```

### Network Optimization

#### 1. WebSocket Tuning
```nginx
# Nginx configuration for WebSocket
location /ws {
    proxy_pass https://backend:31337;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 3600s;
    proxy_send_timeout 3600s;
    proxy_buffer_size 64k;
    proxy_buffers 16 32k;
}
```

#### 2. Load Balancing
```nginx
upstream krakenhashes_backend {
    least_conn;
    server backend1:31337 max_fails=3 fail_timeout=30s;
    server backend2:31337 max_fails=3 fail_timeout=30s;
    keepalive 32;
}
```

## High Availability Setup

### Architecture Overview

```
                    ┌─────────────┐
                    │ Load Balancer│
                    │   (HAProxy)  │
                    └──────┬───────┘
                           │
                ┌──────────┴──────────┐
                │                     │
         ┌──────▼──────┐      ┌──────▼──────┐
         │  Backend 1  │      │  Backend 2  │
         │  (Active)   │      │  (Active)   │
         └──────┬──────┘      └──────┬──────┘
                │                     │
         ┌──────▼──────────────────▼──────┐
         │     PostgreSQL Cluster          │
         │   (Primary + Replica)           │
         └─────────────────────────────────┘
```

### Database High Availability

#### 1. PostgreSQL Replication Setup
```bash
# On Primary
postgresql.conf:
wal_level = replica
max_wal_senders = 3
wal_keep_segments = 64
synchronous_commit = on
synchronous_standby_names = 'replica1'

# On Replica
recovery.conf:
standby_mode = 'on'
primary_conninfo = 'host=primary port=5432 user=replicator'
trigger_file = '/tmp/postgresql.trigger'
```

#### 2. Connection Pooling with PgBouncer
```ini
# pgbouncer.ini
[databases]
krakenhashes = host=primary port=5432 dbname=krakenhashes

[pgbouncer]
pool_mode = transaction
max_client_conn = 1000
default_pool_size = 50
min_pool_size = 10
reserve_pool_size = 5
reserve_pool_timeout = 3
server_lifetime = 3600
server_idle_timeout = 600
```

### Application High Availability

#### 1. Backend Clustering
```yaml
# docker-compose-ha.yml
services:
  backend1:
    image: krakenhashes/backend:v0.1.0
    environment:
      - KH_CLUSTER_MODE=true
      - KH_NODE_ID=backend1
      - KH_CLUSTER_PEERS=backend2:31337
    volumes:
      - shared-data:/var/lib/krakenhashes

  backend2:
    image: krakenhashes/backend:v0.1.0
    environment:
      - KH_CLUSTER_MODE=true
      - KH_NODE_ID=backend2
      - KH_CLUSTER_PEERS=backend1:31337
    volumes:
      - shared-data:/var/lib/krakenhashes
```

#### 2. Load Balancer Configuration
```
# haproxy.cfg
global
    maxconn 4096
    log stdout local0
    
defaults
    mode http
    timeout connect 5000ms
    timeout client 50000ms
    timeout server 50000ms
    option httplog
    
frontend krakenhashes_front
    bind *:443 ssl crt /etc/ssl/krakenhashes.pem
    default_backend krakenhashes_back
    
backend krakenhashes_back
    balance leastconn
    option httpchk GET /api/health
    server backend1 backend1:31337 check ssl verify none
    server backend2 backend2:31337 check ssl verify none
```

### Storage High Availability

#### 1. Distributed File System
```bash
# GlusterFS setup for shared storage
gluster volume create krakenhashes-data replica 2 \
    server1:/data/gluster/krakenhashes \
    server2:/data/gluster/krakenhashes
    
gluster volume start krakenhashes-data
mount -t glusterfs server1:/krakenhashes-data /var/lib/krakenhashes
```

#### 2. Object Storage Integration
```bash
# S3-compatible storage for large files
export KH_STORAGE_TYPE=s3
export KH_S3_ENDPOINT=https://s3.example.com
export KH_S3_BUCKET=krakenhashes-data
export KH_S3_ACCESS_KEY=your-access-key
export KH_S3_SECRET_KEY=your-secret-key
```

## Monitoring and Alerting

### Metrics Collection

#### 1. Prometheus Integration
```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'krakenhashes'
    static_configs:
      - targets: ['backend1:31337', 'backend2:31337']
    metrics_path: '/metrics'
    scheme: 'https'
    tls_config:
      insecure_skip_verify: true
```

#### 2. Key Metrics to Monitor
```yaml
# System Metrics
- CPU usage per core
- Memory usage and available
- Disk I/O and latency
- Network throughput and errors

# Application Metrics
- Request rate and latency
- Error rate by endpoint
- Active WebSocket connections
- Database connection pool usage

# Business Metrics
- Jobs queued/running/completed
- Hash crack rate
- Agent utilization
- Storage usage growth
```

### Alerting Configuration

#### 1. Critical Alerts (Immediate Response)
```yaml
groups:
  - name: critical
    rules:
      - alert: ServiceDown
        expr: up{job="krakenhashes"} == 0
        for: 2m
        
      - alert: DatabaseDown
        expr: pg_up == 0
        for: 1m
        
      - alert: HighErrorRate
        expr: rate(http_requests_total{status=~"5.."}[5m]) > 0.05
        for: 5m
        
      - alert: DiskSpaceCritical
        expr: disk_free_percentage < 10
        for: 5m
```

#### 2. Warning Alerts (Business Hours)
```yaml
groups:
  - name: warnings
    rules:
      - alert: HighCPUUsage
        expr: cpu_usage_percentage > 80
        for: 15m
        
      - alert: MemoryPressure
        expr: memory_available_percentage < 20
        for: 10m
        
      - alert: SlowQueries
        expr: pg_slow_queries_rate > 10
        for: 10m
        
      - alert: AgentDisconnections
        expr: rate(agent_disconnections[5m]) > 5
        for: 5m
```

### Logging Strategy

#### 1. Centralized Logging
```yaml
# Fluentd configuration
<source>
  @type tail
  path /var/log/krakenhashes/backend/*.log
  pos_file /var/log/fluentd/krakenhashes.pos
  tag krakenhashes.backend
  <parse>
    @type json
  </parse>
</source>

<match krakenhashes.**>
  @type elasticsearch
  host elasticsearch.example.com
  port 9200
  logstash_format true
  logstash_prefix krakenhashes
</match>
```

#### 2. Log Retention Policy
- Application logs: 30 days
- Security logs: 90 days
- Audit logs: 365 days
- Performance metrics: 90 days aggregated

### Dashboard Setup

#### 1. Grafana Dashboards
- System Overview Dashboard
- Job Performance Dashboard
- Agent Monitoring Dashboard
- Security Events Dashboard
- Database Performance Dashboard

#### 2. Real-time Monitoring
```bash
# Custom monitoring endpoints
GET /api/admin/metrics/realtime
GET /api/admin/health/detailed
GET /api/admin/agents/status
GET /api/admin/jobs/statistics
```

## Backup and Disaster Recovery

### Backup Strategy

#### 1. Automated Backup Schedule
```cron
# Database backups - every 4 hours
0 */4 * * * /usr/local/bin/krakenhashes-db-backup.sh

# File system backups - daily at 2 AM
0 2 * * * /usr/local/bin/krakenhashes-file-backup.sh

# Configuration backups - weekly
0 3 * * 0 /usr/local/bin/krakenhashes-config-backup.sh

# Off-site sync - daily at 4 AM
0 4 * * * /usr/local/bin/krakenhashes-offsite-sync.sh
```

#### 2. Backup Verification
```bash
#!/bin/bash
# Automated backup verification
BACKUP_DIR="/backup/krakenhashes"
LOG_FILE="/var/log/krakenhashes-backup-verify.log"

# Verify latest backups
for backup_type in postgres files config; do
    latest=$(find $BACKUP_DIR/$backup_type -name "*.gz" -mtime -1 | head -1)
    if [ -z "$latest" ]; then
        echo "ERROR: No recent $backup_type backup found" >> $LOG_FILE
        # Send alert
    fi
done
```

### Disaster Recovery Plan

#### 1. RTO/RPO Targets
- **Database RTO**: 30 minutes
- **Database RPO**: 4 hours
- **Full System RTO**: 2 hours
- **Full System RPO**: 24 hours

#### 2. Recovery Procedures
```bash
# Quick recovery checklist
1. Assess damage and determine recovery scope
2. Provision replacement infrastructure
3. Restore database from latest backup
4. Restore file system data
5. Update DNS/load balancer configuration
6. Verify system functionality
7. Communicate with users
```

#### 3. Disaster Recovery Testing
- Monthly backup restoration tests
- Quarterly full DR drills
- Annual infrastructure failover test
- Document lessons learned

## Compliance Considerations

### Data Protection

#### 1. GDPR Compliance
- Implement right to erasure (data deletion)
- Maintain data processing records
- Encrypt personal data at rest and in transit
- Implement data retention policies
- Regular privacy impact assessments

#### 2. Data Retention Policy
```sql
-- Automated data retention
DELETE FROM hashes 
WHERE is_cracked = true 
  AND cracked_at < NOW() - INTERVAL '90 days'
  AND hashlist_id IN (
    SELECT id FROM hashlists 
    WHERE retention_days = 90
  );

DELETE FROM job_executions 
WHERE completed_at < NOW() - INTERVAL '180 days';

DELETE FROM agent_performance_metrics 
WHERE timestamp < NOW() - INTERVAL '90 days';
```

### Security Compliance

#### 1. Access Control Compliance
- Implement least privilege access
- Regular access reviews
- Multi-factor authentication
- Session management controls
- Audit trail for all admin actions

#### 2. Audit Logging
```sql
-- Audit table structure
CREATE TABLE audit_logs (
    id SERIAL PRIMARY KEY,
    user_id INTEGER,
    action VARCHAR(100),
    resource_type VARCHAR(50),
    resource_id INTEGER,
    details JSONB,
    ip_address INET,
    user_agent TEXT,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Index for efficient querying
CREATE INDEX idx_audit_logs_user_action 
    ON audit_logs(user_id, action, created_at);
```

### Industry Standards

#### 1. Password Handling
- Never store plaintext passwords
- Use bcrypt with cost factor 12+
- Implement password history
- Enforce password complexity
- Regular password policy reviews

#### 2. Cryptographic Standards
- TLS 1.2 minimum (prefer TLS 1.3)
- Strong cipher suites only
- 2048-bit RSA minimum (prefer 4096-bit)
- Regular certificate rotation
- Hardware security module (HSM) for keys

### Compliance Reporting

#### 1. Regular Reports
- Monthly security metrics
- Quarterly compliance audits
- Annual penetration testing
- Incident response reports
- User access reviews

#### 2. Documentation Requirements
- System architecture diagrams
- Data flow documentation
- Security policies and procedures
- Incident response plan
- Business continuity plan

## Operational Best Practices

### Change Management

#### 1. Deployment Process
```bash
# Pre-deployment checklist
- [ ] Code review completed
- [ ] Security scan passed
- [ ] Performance testing done
- [ ] Backup taken
- [ ] Rollback plan ready
- [ ] Maintenance window scheduled
- [ ] User notification sent
```

#### 2. Version Control
- Tag all production releases
- Maintain detailed changelogs
- Document breaking changes
- Test upgrade paths
- Keep rollback scripts ready

### Capacity Planning

#### 1. Growth Monitoring
```sql
-- Monitor growth trends
SELECT 
    DATE_TRUNC('month', created_at) as month,
    COUNT(*) as hashlists_created,
    SUM(total_hashes) as total_hashes_added
FROM hashlists
GROUP BY month
ORDER BY month;
```

#### 2. Scaling Triggers
- CPU usage > 70% sustained
- Memory usage > 80%
- Storage usage > 75%
- Response time > 2 seconds
- Queue depth > 1000 jobs

### Maintenance Windows

#### 1. Scheduled Maintenance
- Weekly: Log rotation, temp file cleanup
- Monthly: Database optimization, index rebuilds
- Quarterly: Security updates, certificate renewal
- Annually: Major version upgrades

#### 2. Emergency Maintenance
- Critical security patches: Immediate
- Data corruption: Immediate
- Performance degradation: Within 4 hours
- Non-critical bugs: Next maintenance window

## Summary

This production deployment guide provides a comprehensive framework for operating KrakenHashes at scale. Key takeaways:

1. **Security First**: Implement defense in depth with multiple security layers
2. **High Availability**: Design for failure with redundancy at every level
3. **Performance**: Optimize continuously based on monitoring data
4. **Compliance**: Maintain audit trails and follow data protection regulations
5. **Automation**: Automate routine tasks to reduce human error
6. **Documentation**: Keep all procedures documented and up to date

Regular review and updates of these practices ensure your KrakenHashes deployment remains secure, performant, and reliable as your organization's needs evolve.