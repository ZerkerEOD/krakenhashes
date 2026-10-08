# Docker Compose Deployment Guide

⚠️ **Version Requirement:** Docker Compose v2.0+ is required. Use `docker compose` (space), not `docker-compose` (hyphen).

This guide covers deploying KrakenHashes using Docker Compose, including configuration options, network setup, volume management, and common customizations.

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Configuration Options](#configuration-options)
- [Network Setup](#network-setup)
- [Volume Management](#volume-management)
- [Deployment Steps](#deployment-steps)
- [Scaling Considerations](#scaling-considerations)
- [Common Customizations](#common-customizations)
- [Troubleshooting](#troubleshooting)
- [Security Considerations](#security-considerations)

## Overview

KrakenHashes uses Docker Compose to orchestrate multiple services:

- **PostgreSQL**: Database backend for storing hashlists, jobs, and system data
- **KrakenHashes App**: Combined backend API and frontend served through nginx

The Docker Compose setup provides:
- Automatic service dependency management
- Health checks for service readiness
- Persistent data storage through Docker volumes
- Environment-based configuration
- Isolated networking

## Prerequisites

Before deploying with Docker Compose:

1. **Docker Engine**: Version 20.10 or higher
2. **Docker Compose**: Version 2.0 or higher (included with Docker Desktop)
3. **System Requirements**:
   - 4GB RAM minimum (8GB recommended)
   - 10GB free disk space
   - Linux, macOS, or Windows with WSL2

4. **Network Ports**:
   - 443 (HTTPS frontend)
   - 1337 (HTTP API)
   - 31337 (HTTPS API)
   - 5432 (PostgreSQL - optional, can be internal only)

## Configuration Options

### Environment Variables

Create a `.env` file in the project root with the following variables:

```bash
# Database Configuration
DB_USER=krakenhashes
DB_PASSWORD=your-secure-password
DB_NAME=krakenhashes

# Port Configuration
FRONTEND_PORT=443
KH_PORT=1337
KH_HTTPS_PORT=31337

# Directory Configuration
LOG_DIR=/var/log/krakenhashes
KH_CONFIG_DIR_HOST=/etc/krakenhashes
KH_DATA_DIR_HOST=/var/lib/krakenhashes

# User/Group IDs (for file permissions)
PUID=1000
PGID=1000

# TLS Configuration
KH_TLS_MODE=self-signed
KH_CERT_KEY_SIZE=4096
KH_CERT_VALIDITY_DAYS=365
KH_CA_VALIDITY_DAYS=3650

# Network Share Storage (optional; see below)
# KH_SHARE_DIR_HOST=/mnt/krakenhashes-share   # Host path of the mounted SMB/NFS share
```

!!! tip "Optional: keep wordlists/rules on a network share"
    The compose file includes a bind mount for an operator-mounted network share. Setting
    `KH_SHARE_DIR_HOST` to the host path of a mounted SMB/NFS share makes it available to the
    backend at `KH_SHARE_DIR` (`/var/lib/krakenhashes-share`). Nothing changes until you enable it
    from **Admin → System Settings → Storage** and migrate; see
    [Network Share Storage](../admin-guide/resource-management/storage.md#network-share-storage).

### Service-Specific Configuration

#### PostgreSQL Service

The PostgreSQL service is configured with:
- Alpine-based image for smaller footprint
- Health checks using `pg_isready`
- Persistent data storage in Docker volume
- Configurable credentials via environment variables

#### KrakenHashes Application

The main application container includes:
- Multi-stage build for optimized image size
- Combined backend and frontend services
- nginx reverse proxy for frontend
- TLS/SSL support with multiple modes
- File storage for binaries, wordlists, and hashlists

## Network Setup

### Default Network

Docker Compose creates an isolated bridge network `krakenhashes-net`:

```yaml
networks:
  krakenhashes-net:
    driver: bridge
```

This provides:
- Service discovery by container name
- Isolation from other Docker networks
- Internal DNS resolution

### Service Communication

- Backend connects to PostgreSQL using hostname `postgres`
- All services communicate over the internal network
- Only required ports are exposed to the host

### Custom Network Configuration

To use an existing network or customize settings:

```yaml
networks:
  krakenhashes-net:
    external: true
    name: my-existing-network
```

Or with custom subnet:

```yaml
networks:
  krakenhashes-net:
    driver: bridge
    ipam:
      config:
        - subnet: 172.20.0.0/16
          gateway: 172.20.0.1
```

## Volume Management

### Persistent Volumes

KrakenHashes uses named volumes for data persistence:

1. **postgres_data**: PostgreSQL database files
2. **krakenhashes_data**: Application data (wordlists, rules, hashlists)

### Volume Locations

Default volume storage locations:
- Docker managed: `/var/lib/docker/volumes/`
- Named volumes:
  - `krakenhashes_postgres_data`
  - `krakenhashes_app_data`

### Bind Mounts

The compose file uses bind mounts for:
- Logs: `${LOG_DIR:-/var/log/krakenhashes}`
- Config: `${KH_CONFIG_DIR_HOST:-/etc/krakenhashes}`
- Data: `${KH_DATA_DIR_HOST:-/var/lib/krakenhashes}`
- Network share (optional): `${KH_SHARE_DIR_HOST:-/mnt/krakenhashes-share}` → `/var/lib/krakenhashes-share`

### Network Share Storage (optional)

For large deployments, wordlists and rules can live on a network share instead of the local data
volume. You mount the SMB/NFS share on the host yourself (KrakenHashes stores no share
credentials), set `KH_SHARE_DIR_HOST` to that host path, and the compose file binds it into the
backend at `KH_SHARE_DIR`. It remains inactive (local-disk behavior) until enabled and migrated
from **Admin → System Settings → Storage**. Leaving `KH_SHARE_DIR_HOST` unset binds a harmless
empty default and keeps local storage. Full details, including per-agent storage tiers and the
migration flow, are in [Storage Architecture](../admin-guide/resource-management/storage.md#network-share-storage).

### Backup Strategy

To backup volumes:

```bash
# Backup PostgreSQL data
docker run --rm -v krakenhashes_postgres_data:/data \
  -v $(pwd):/backup alpine tar czf /backup/postgres-backup.tar.gz -C /data .

# Backup application data
docker run --rm -v krakenhashes_app_data:/data \
  -v $(pwd):/backup alpine tar czf /backup/app-backup.tar.gz -C /data .
```

## Deployment Steps

### Initial Deployment

1. **Clone the repository**:
   ```bash
   git clone https://github.com/yourusername/krakenhashes.git
   cd krakenhashes
   ```

2. **Create environment file**:
   ```bash
   cp .env.example .env
   # Edit .env with your configuration
   ```

3. **Create required directories**:
   ```bash
   sudo mkdir -p /var/log/krakenhashes
   sudo mkdir -p /etc/krakenhashes
   sudo mkdir -p /var/lib/krakenhashes
   sudo chown -R 1000:1000 /var/log/krakenhashes
   sudo chown -R 1000:1000 /etc/krakenhashes
   sudo chown -R 1000:1000 /var/lib/krakenhashes
   ```

4. **Build and start services**:
   ```bash
   docker-compose up -d --build
   ```

5. **Verify deployment**:
   ```bash
   docker-compose ps
   docker-compose logs -f
   ```

### Updating Deployment

1. **Pull latest changes**:
   ```bash
   git pull origin main
   ```

2. **Rebuild and restart**:
   ```bash
   docker-compose down
   docker-compose up -d --build
   ```

3. **Check migration status**:
   ```bash
   docker-compose logs krakenhashes | grep -i migration
   ```

## Scaling Considerations

### Horizontal Scaling

While the current setup runs as a single instance, you can prepare for scaling:

1. **Database Scaling**:
   - Use external PostgreSQL for production
   - Consider connection pooling with PgBouncer
   - Implement read replicas for reporting

2. **Application Scaling**:
   - Use external load balancer (nginx, HAProxy)
   - Share file storage (NFS, S3-compatible)
   - Implement Redis for session storage

### Resource Limits

Add resource constraints to prevent container resource exhaustion:

```yaml
services:
  krakenhashes:
    deploy:
      resources:
        limits:
          cpus: '2'
          memory: 4G
        reservations:
          cpus: '1'
          memory: 2G
```

## Common Customizations

### Development Mode

For development, uncomment restart policies and expose additional ports:

```yaml
services:
  postgres:
    restart: unless-stopped
    ports:
      - "5432:5432"  # Direct database access
  
  krakenhashes:
    restart: unless-stopped
    environment:
      - DEBUG=true
      - LOG_LEVEL=debug
```

### Production Optimizations

1. **Remove unnecessary port exposures**:
   ```yaml
   services:
     postgres:
       # Remove ports section for internal-only access
   ```

2. **Enable restart policies**:
   ```yaml
   restart: always
   ```

3. **Use specific image tags**:
   ```yaml
   image: postgres:15.4-alpine
   ```

### Custom TLS Certificates

To use your own certificates:

1. Place certificates in `/etc/krakenhashes/certs/`
2. Set environment variables:
   ```bash
   KH_TLS_MODE=provided
   KH_TLS_CERT_PATH=/etc/krakenhashes/certs/server.crt
   KH_TLS_KEY_PATH=/etc/krakenhashes/certs/server.key
   ```

### External Database

To use an external PostgreSQL instance:

1. Remove the postgres service from docker-compose.yml
2. Update environment variables:
   ```bash
   DB_HOST=your-database-host.com
   DB_PORT=5432
   DB_NAME=krakenhashes
   DB_USER=krakenhashes
   DB_PASSWORD=your-password
   ```

## Troubleshooting

### Common Issues

1. **Container fails to start**:
   ```bash
   # Check logs
   docker-compose logs krakenhashes
   
   # Check health status
   docker-compose ps
   ```

2. **Database connection errors**:
   ```bash
   # Test database connectivity
   docker-compose exec krakenhashes pg_isready -h postgres -U krakenhashes
   
   # Check PostgreSQL logs
   docker-compose logs postgres
   ```

3. **Permission issues**:
   ```bash
   # Fix ownership
   sudo chown -R 1000:1000 /var/lib/krakenhashes
   sudo chown -R 1000:1000 /var/log/krakenhashes
   ```

4. **Port conflicts**:
   ```bash
   # Check port usage
   sudo netstat -tlnp | grep -E '(443|1337|31337|5432)'
   
   # Change ports in .env file
   FRONTEND_PORT=8443
   KH_PORT=8337
   ```

### Debug Mode

Enable debug logging:

```bash
# In .env file
LOG_LEVEL=debug
DEBUG=true

# Restart services
docker-compose restart krakenhashes
```

### Health Checks

Monitor service health:

```bash
# Check all services
docker-compose ps

# Detailed health info
docker inspect krakenhashes-postgres | jq '.[0].State.Health'
```

## Security Considerations

### Network Security

1. **Firewall Rules**:
   - Only expose necessary ports
   - Use firewall to restrict access
   - Consider VPN for administrative access

2. **TLS/SSL**:
   - Always use HTTPS in production
   - Regularly update certificates
   - Use strong cipher suites

### Container Security

1. **Run as non-root**:
   - Containers use UID/GID 1000 by default
   - Avoid running as root user

2. **Image Security**:
   ```bash
   # Scan for vulnerabilities
   docker scan krakenhashes:latest
   ```

3. **Secrets Management**:
   - Use Docker secrets for sensitive data
   - Rotate database passwords regularly
   - Never commit .env files to version control

### Backup and Recovery

1. **Regular Backups**:
   ```bash
   # Automated backup script
   #!/bin/bash
   BACKUP_DIR="/backup/krakenhashes/$(date +%Y%m%d)"
   mkdir -p $BACKUP_DIR
   
   # Backup database
   docker-compose exec -T postgres pg_dump -U krakenhashes > $BACKUP_DIR/database.sql
   
   # Backup volumes
   docker run --rm -v krakenhashes_app_data:/data \
     -v $BACKUP_DIR:/backup alpine \
     tar czf /backup/app-data.tar.gz -C /data .
   ```

2. **Test Recovery**:
   - Regularly test backup restoration
   - Document recovery procedures
   - Keep multiple backup generations

### Monitoring

Implement monitoring for:
- Container health and restarts
- Resource usage (CPU, memory, disk)
- Application logs and errors
- Database performance
- TLS certificate expiration

Consider using:
- Prometheus + Grafana for metrics
- ELK stack for log aggregation
- Uptime monitoring services