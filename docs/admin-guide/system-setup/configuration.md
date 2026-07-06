# KrakenHashes System Configuration Guide

This guide provides comprehensive documentation for all configuration options available in the KrakenHashes system, including environment variables, configuration files, and settings for all components.

## Table of Contents

- [Overview](#overview)
- [Environment Variables](#environment-variables)
  - [Core System Settings](#core-system-settings)
  - [Database Configuration](#database-configuration)
  - [Backend Configuration](#backend-configuration)
  - [Frontend Configuration](#frontend-configuration)
  - [Agent Configuration](#agent-configuration)
  - [TLS/SSL Configuration](#tlsssl-configuration)
  - [Debug and Logging](#debug-and-logging)
  - [Docker-Specific Settings](#docker-specific-settings)
- [Configuration Files](#configuration-files)
- [Common Configuration Scenarios](#common-configuration-scenarios)

## Overview

KrakenHashes uses environment variables for all runtime configuration. Configuration can be provided through:

1. **Environment Variables**: Direct system environment variables
2. **`.env` Files**: Local environment files (development)
3. **Docker Compose**: Environment variables in docker-compose.yml
4. **Kubernetes ConfigMaps**: For production deployments

### Configuration Hierarchy

The system follows this precedence order (highest to lowest):
1. Runtime environment variables
2. Docker Compose environment settings
3. `.env` file values
4. Default values in code

## Configuration Types

KrakenHashes uses two types of configuration:

### 1. Environment Variables (System Configuration)
These settings control core system behavior and are set at deployment time. They configure infrastructure elements like database connections, ports, and file paths.

### 2. Admin Panel Settings (Runtime Configuration)
These settings can be changed through the web interface without restarting services. They control operational behavior like job execution, chunking, and agent coordination.

!!! tip "Configuration Best Practice"
    Use environment variables for infrastructure settings that rarely change.
    Use admin panel settings for operational parameters that need frequent adjustment.

## Admin Panel Settings

Several configuration options are available through the Admin Panel UI rather than environment variables. These settings can be changed at runtime without restarting services.

### Available Admin Panel Settings

- **[Job Execution Settings](../operations/job-settings.md)**: Control job chunking, agent behavior, and task distribution
  - Default chunk duration
  - Reconnect grace period
  - Progress reporting intervals
  - Rule splitting configuration
  
- **[Data Retention Settings](../operations/data-retention.md)**: Configure automatic data cleanup
  - Hashlist retention periods
  - Job history retention
  - Metrics retention

- **[Agent Scheduling](../operations/scheduling.md)**: Define when agents are available
  - Daily schedules per agent
  - Global scheduling enable/disable

### Accessing Admin Panel Settings

1. Log in as an administrator
2. Navigate to the **Admin Panel**
3. Click **Settings** in the navigation menu
4. Select the appropriate settings category

Changes to admin panel settings take effect immediately without requiring service restarts.

## Environment Variables

### Core System Settings

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `PUID` | User ID for file permissions | `1000` | `1001` |
| `PGID` | Group ID for file permissions | `1000` | `1001` |
| `TZ` | Timezone | `UTC` | `America/New_York` |
| `KH_IN_DOCKER` | Whether running in Docker | `false` | `true` |

### Database Configuration

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `DB_HOST` | PostgreSQL host | `localhost` | `postgres` |
| `DB_PORT` | PostgreSQL port | `5432` | `5432` |
| `DB_NAME` | Database name | `krakenhashes` | `krakenhashes_prod` |
| `DB_USER` | Database username | `krakenhashes` | `khuser` |
| `DB_PASSWORD` | Database password | `krakenhashes` | `secure_password` |
| `DB_ARGUMENTS` | Additional connection arguments | `sslmode=disable` | `sslmode=require&connect_timeout=5` |

### Backend Configuration

#### Server Settings

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `KH_HOST` | Backend host binding | `localhost` (or `0.0.0.0` in Docker) | `0.0.0.0` |
| `KH_HTTPS_PORT` | HTTPS API port | `31337` | `8443` |
| `KH_HTTP_PORT` | HTTP port (CA certificate) | `1337` | `8080` |
| `KH_CONFIG_DIR` | Configuration directory | `~/.krakenhashes` | `/etc/krakenhashes` |
| `KH_DATA_DIR` | Data storage directory | `~/.krakenhashes-data` | `/var/lib/krakenhashes` |
| `KH_CERTS_DIR` | Certificate directory | `{KH_CONFIG_DIR}/certs` | `/etc/krakenhashes/certs` |

#### File Handling

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `KH_HASHLIST_BATCH_SIZE` | Max hashes per DB batch | `1000` | `5000` |
| `KH_MAX_UPLOAD_SIZE_MB` | Max file upload size (MB) | `32` | `100` |
| `KH_HASH_UPLOAD_DIR` | Hash upload directory | `{KH_DATA_DIR}/hashlist_uploads` | `/var/lib/krakenhashes/uploads` |

#### JWT Authentication

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `JWT_SECRET` | JWT signing secret | - | `your-secret-key` |
| `JWT_EXPIRATION` | Token expiration time | `24h` | `7d` |
| `DEFAULT_ADMIN_ID` | Default admin user ID | - | `uuid-here` |

#### WebSocket Configuration

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `KH_WRITE_WAIT` | Time allowed to write messages | `4s` | `10s` |
| `KH_PONG_WAIT` | Time to wait for pong response | `10s` | `30s` |
| `KH_PING_PERIOD` | How often to send pings | `6s` | `15s` |

### Frontend Configuration

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `REACT_APP_API_URL` | HTTPS API endpoint | `https://localhost:31337` | `https://api.example.com` |
| `REACT_APP_HTTP_API_URL` | HTTP API endpoint | `http://localhost:1337` | `http://api.example.com:8080` |
| `REACT_APP_WS_URL` | WebSocket endpoint | `wss://localhost:31337` | `wss://api.example.com` |
| `FRONTEND_PORT` | Frontend HTTPS port | `443` | `3000` |
| `PORT` | Development server port | `3000` | `3001` |
| `NODE_ENV` | Node environment | `development` | `production` |
| `HTTPS` | Enable HTTPS in dev server | `true` | `false` |
| `SSL_CRT_FILE` | Dev server SSL certificate | - | `../certs/server.crt` |
| `SSL_KEY_FILE` | Dev server SSL key | - | `../certs/server.key` |

### Agent Configuration

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `KH_CONFIG_DIR` | Agent config directory | `{executable_dir}/config` | `/opt/krakenhashes/config` |
| `KH_DATA_DIR` | Agent data directory | `{executable_dir}/data` | `/opt/krakenhashes/data` |
| `HASHCAT_EXTRA_PARAMS` | Extra hashcat parameters | - | `-O -w 3` |

### TLS/SSL Configuration

#### General TLS Settings

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `KH_TLS_MODE` | TLS provider mode | `self-signed` | `certbot`, `provided` |
| `KH_CERT_KEY_SIZE` | RSA key size (bits) | `4096` | `2048` |
| `KH_CERT_VALIDITY_DAYS` | Server cert validity (days) | `365` | `730` |
| `KH_CA_VALIDITY_DAYS` | CA cert validity (days) | `3650` | `7300` |

#### Certificate Details

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `KH_CA_COUNTRY` | CA country code | `US` | `UK` |
| `KH_CA_ORGANIZATION` | CA organization | `KrakenHashes` | `YourOrg` |
| `KH_CA_ORGANIZATIONAL_UNIT` | CA organizational unit | `KrakenHashes CA` | `IT Department` |
| `KH_CA_COMMON_NAME` | CA common name | `KrakenHashes Root CA` | `YourOrg Root CA` |
| `KH_ADDITIONAL_DNS_NAMES` | Additional DNS names (comma-separated) | - | `localhost,app.local,*.example.com` |
| `KH_ADDITIONAL_IP_ADDRESSES` | Additional IP addresses (comma-separated) | - | `192.168.1.100,10.0.0.5` |

#### Provided Certificate Mode

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `KH_CERT_FILE` | Path to certificate file | `{KH_CERTS_DIR}/server.crt` | `/etc/ssl/server.crt` |
| `KH_KEY_FILE` | Path to private key file | `{KH_CERTS_DIR}/server.key` | `/etc/ssl/server.key` |
| `KH_CA_FILE` | Path to CA certificate | `{KH_CERTS_DIR}/ca.crt` | `/etc/ssl/ca.crt` |

#### Certbot Mode (Let's Encrypt)

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `KH_CERTBOT_DOMAIN` | Domain for certificate | - | `kraken.example.com` |
| `KH_CERTBOT_EMAIL` | Email for notifications | - | `admin@example.com` |
| `KH_CERTBOT_STAGING` | Use staging server | `false` | `true` |
| `KH_CERTBOT_AUTO_RENEW` | Enable auto-renewal | `true` | `false` |
| `KH_CERTBOT_RENEW_HOOK` | Post-renewal hook script | - | `/opt/scripts/reload.sh` |
| `CLOUDFLARE_API_TOKEN` | Cloudflare API token for DNS-01 | - | `your-api-token` |

### Debug and Logging

#### General Debug Settings

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `DEBUG` | Enable debug mode | `false` | `true` |
| `LOG_LEVEL` | Logging level | `INFO` | `DEBUG`, `WARNING`, `ERROR` |

#### Component-Specific Debug Flags

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `DEBUG_SQL` | Enable SQL query logging | `false` | `true` |
| `DEBUG_HTTP` | Enable HTTP request/response logging | `false` | `true` |
| `DEBUG_WEBSOCKET` | Enable WebSocket message logging | `false` | `true` |
| `DEBUG_AUTH` | Enable authentication debugging | `false` | `true` |
| `DEBUG_JOBS` | Enable job processing debugging | `false` | `true` |

#### Frontend Debug Settings

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `REACT_APP_DEBUG` | Enable frontend debugging | `false` | `true` |
| `REACT_APP_DEBUG_REDUX` | Enable Redux debugging | `false` | `true` |

#### Log Directories

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `LOG_DIR` | Base log directory | `/var/log/krakenhashes` | `/logs` |
| `BACKEND_LOG_DIR` | Backend logs | `${LOG_DIR}/backend` | `/logs/backend` |
| `FRONTEND_LOG_DIR` | Frontend logs | `${LOG_DIR}/frontend` | `/logs/frontend` |
| `NGINX_LOG_DIR` | Nginx logs | `${LOG_DIR}/nginx` | `/logs/nginx` |
| `POSTGRES_LOG_DIR` | PostgreSQL logs | `${LOG_DIR}/postgres` | `/logs/postgres` |

### Docker-Specific Settings

#### Volume Mounts

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `KH_CONFIG_DIR_HOST` | Host config directory | `/etc/krakenhashes` | `./config` |
| `KH_DATA_DIR_HOST` | Host data directory | `/var/lib/krakenhashes` | `./data` |

#### Nginx Configuration

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `NGINX_ACCESS_LOG_LEVEL` | Nginx access log level | `info` | `debug` |
| `NGINX_ERROR_LOG_LEVEL` | Nginx error log level | `warn` | `error` |
| `NGINX_CLIENT_MAX_BODY_SIZE` | Max request body size | `50M` | `100M` |

#### CORS Configuration

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `CORS_ALLOWED_ORIGIN` | Allowed CORS origins | `https://localhost:443` | `https://app.example.com` |

## Configuration Files

### Directory Structure

The system creates and uses the following directory structure:

```
${KH_CONFIG_DIR}/
├── certs/              # TLS certificates
│   ├── ca.crt         # CA certificate
│   ├── ca.key         # CA private key
│   ├── server.crt     # Server certificate
│   └── server.key     # Server private key
└── config/            # Application configuration

${KH_DATA_DIR}/
├── binaries/          # Hashcat and other tools
├── wordlists/         # Wordlist files
│   ├── general/       # General purpose wordlists
│   ├── specialized/   # Domain-specific wordlists
│   ├── targeted/      # Target-specific wordlists
│   └── custom/        # User-uploaded wordlists
├── rules/             # Rule files
│   ├── hashcat/       # Hashcat rule files
│   ├── john/          # John the Ripper rules
│   └── custom/        # Custom rule files
├── hashlists/         # Hash files
└── hashlist_uploads/  # Temporary upload directory
```

### Configuration File Locations

- **Backend**: No configuration files - all settings via environment variables
- **Frontend**: `.env` file in frontend directory (development only)
- **Agent**: Configuration stored in `${KH_CONFIG_DIR}/agent.json` (auto-generated)
- **Docker**: `.env` file in project root for docker-compose

## Common Configuration Scenarios

### Development Setup

```bash
# .env file for development
DEBUG=true
LOG_LEVEL=DEBUG
KH_TLS_MODE=self-signed
JWT_SECRET=dev-secret-key
DB_HOST=localhost
DB_PASSWORD=dev-password
```

### Production with Let's Encrypt

```bash
# .env file for production
DEBUG=false
LOG_LEVEL=INFO
KH_TLS_MODE=certbot
KH_CERTBOT_DOMAIN=kraken.example.com
KH_CERTBOT_EMAIL=admin@example.com
CLOUDFLARE_API_TOKEN=your-token
JWT_SECRET=secure-random-secret
DB_PASSWORD=strong-password
```

### High-Security Environment

```bash
# .env file for high security
KH_TLS_MODE=provided
KH_CERT_FILE=/etc/ssl/certs/server.crt
KH_KEY_FILE=/etc/ssl/private/server.key
KH_CA_FILE=/etc/ssl/certs/ca-bundle.crt
KH_CERT_KEY_SIZE=4096
JWT_SECRET=very-long-secure-secret
DEBUG=false
DEBUG_SQL=false
DEBUG_HTTP=false
```

### Agent Configuration

```bash
# Agent environment
KH_CONFIG_DIR=/opt/krakenhashes/config
KH_DATA_DIR=/opt/krakenhashes/data
HASHCAT_EXTRA_PARAMS=-O -w 3
```

### Docker Production Deployment

```bash
# Production docker-compose override
PUID=1000
PGID=1000
KH_CONFIG_DIR_HOST=/opt/krakenhashes/config
KH_DATA_DIR_HOST=/data/krakenhashes
LOG_DIR=/var/log/krakenhashes
NGINX_CLIENT_MAX_BODY_SIZE=100M
```

## Best Practices

1. **Security**:
   - Always use strong, unique values for `JWT_SECRET` in production
   - Never commit `.env` files with secrets to version control
   - Use environment-specific configurations

2. **File Permissions**:
   - Set `PUID` and `PGID` to match your host user for proper permissions
   - Ensure certificate files have restricted permissions (600 or 640)

3. **TLS/SSL**:
   - Use Let's Encrypt (`certbot` mode) for production
   - Self-signed certificates only for development
   - Always include all required DNS names and IP addresses

4. **Performance**:
   - Adjust `KH_HASHLIST_BATCH_SIZE` based on available memory
   - Configure `NGINX_CLIENT_MAX_BODY_SIZE` for expected file sizes
   - Set appropriate WebSocket timeouts for your network

5. **Logging**:
   - Use `INFO` level for production
   - Enable component-specific debugging only when needed
   - Regularly rotate log files

6. **Database**:
   - Use strong passwords in production
   - Consider using SSL for database connections in production
   - Regular backups of the PostgreSQL data volume

## Troubleshooting

### Common Issues

1. **Certificate Errors**:
   - Check `KH_ADDITIONAL_DNS_NAMES` includes all hostnames
   - Verify certificate paths are correct
   - Ensure proper file permissions

2. **Database Connection**:
   - Verify database credentials
   - Check network connectivity between containers
   - Ensure PostgreSQL is healthy before backend starts

3. **File Upload Issues**:
   - Check `KH_MAX_UPLOAD_SIZE_MB` setting
   - Verify `NGINX_CLIENT_MAX_BODY_SIZE` is sufficient
   - Ensure data directories have proper permissions

4. **WebSocket Disconnections**:
   - Adjust timeout values (`KH_PONG_WAIT`, `KH_PING_PERIOD`)
   - Check for proxy/firewall interference
   - Verify WebSocket URL configuration