# Environment Variables Reference

This document provides a comprehensive reference for all environment variables used in the KrakenHashes system.

## Table of Contents

- [Backend Server](#backend-server)
- [Frontend Application](#frontend-application)
- [Agent](#agent)
- [Docker & Deployment](#docker--deployment)
- [Database](#database)
- [Authentication & Security](#authentication--security)
- [TLS/SSL Configuration](#tlsssl-configuration)
- [Logging & Debugging](#logging--debugging)
- [WebSocket Configuration](#websocket-configuration)

## Backend Server

### Core Configuration

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `KH_HOST` | string | `localhost` (or `0.0.0.0` in Docker) | No | Host address for the server to bind to |
| `KH_HTTPS_PORT` | integer | `31337` | No | Port for HTTPS API server |
| `KH_HTTP_PORT` | integer | `1337` | No | Port for HTTP server (CA certificate distribution) |
| `KH_IN_DOCKER` | boolean | `false` | No | Set to `TRUE` when running in Docker container |

### Data & Storage

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `KH_CONFIG_DIR` | string | `~/.krakenhashes` | No | Base directory for configuration files |
| `KH_DATA_DIR` | string | `~/.krakenhashes-data` | No | Base directory for mutable data (uploads, binaries, etc.) |
| `KH_HASHLIST_BATCH_SIZE` | integer | `1000` | No | Maximum number of hashes to process in one database batch |
| `KH_MAX_UPLOAD_SIZE_MB` | integer | `32` | No | Maximum file upload size in megabytes |
| `KH_HASH_UPLOAD_DIR` | string | `{KH_DATA_DIR}/hashlist_uploads` | No | Directory for storing uploaded hashlists |

### Directory Structure

The backend automatically creates the following subdirectories under `KH_DATA_DIR`:
- `binaries/` - Executable files (hashcat, john, etc.)
- `wordlists/` - Wordlist files with subdirectories:
  - `general/` - Common wordlists
  - `specialized/` - Domain-specific wordlists
  - `targeted/` - Client/project-specific wordlists
  - `custom/` - User-created wordlists
- `rules/` - Rule files with subdirectories:
  - `hashcat/` - Hashcat-compatible rules
  - `john/` - John the Ripper rules
  - `custom/` - User-created rules
- `hashlists/` - Hash files and crack results

## Frontend Application

### API Configuration

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `REACT_APP_API_URL` | string | `https://localhost:31337` | Yes | HTTPS API endpoint URL |
| `REACT_APP_HTTP_API_URL` | string | `http://localhost:1337` | No | HTTP API endpoint URL (for CA cert download) |
| `REACT_APP_WS_URL` | string | `wss://localhost:31337` | Yes | WebSocket endpoint URL |
| `REACT_APP_VERSION` | string | (from versions.json) | No | Frontend version (set during build) |

### Development Server

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `HTTPS` | boolean | `true` | No | Enable HTTPS for development server |
| `SSL_CRT_FILE` | string | - | No | Path to SSL certificate for dev server |
| `SSL_KEY_FILE` | string | - | No | Path to SSL key for dev server |
| `HOST` | string | `0.0.0.0` | No | Development server host |
| `PORT` | integer | `3000` | No | Development server port |
| `NODE_ENV` | string | `development` | No | Node environment |
| `BROWSER` | string | `none` | No | Browser launch behavior |

### Debug Configuration

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `REACT_APP_DEBUG` | boolean | `false` | No | Enable debug mode in React app |
| `REACT_APP_DEBUG_REDUX` | boolean | `false` | No | Enable Redux debugging |

## Agent

### Core Configuration

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `KH_DATA_DIR` | string | `{executable_dir}/data` | No | Base directory for agent data |
| `KH_CONFIG_DIR` | string | `{executable_dir}/config` | No | Directory for agent configuration files |
| `HASHCAT_EXTRA_PARAMS` | string | - | No | Extra parameters to pass to hashcat (e.g., `-O -w 3`) |

The agent creates the same directory structure as the backend under its data directory.

## Docker & Deployment

### Container Configuration

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `PUID` | integer | `1000` | No | User ID for file permissions |
| `PGID` | integer | `1000` | No | Group ID for file permissions |
| `TZ` | string | `UTC` | No | Container timezone |

### Volume Mounts

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `LOG_DIR` | string | `/var/log/krakenhashes` | No | Base directory for log files |
| `KH_CONFIG_DIR_HOST` | string | `/etc/krakenhashes` | No | Host path for config directory |
| `KH_DATA_DIR_HOST` | string | `/var/lib/krakenhashes` | No | Host path for data directory |

### Port Mappings

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `FRONTEND_PORT` | integer | `443` | No | Host port for frontend (nginx) |

## Database

### Connection Settings

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `DB_HOST` | string | `localhost` | Yes | Database host |
| `DB_PORT` | integer | `5432` | Yes | Database port |
| `DB_NAME` | string | `krakenhashes` | Yes | Database name |
| `DB_USER` | string | `krakenhashes` | Yes | Database username |
| `DB_PASSWORD` | string | `krakenhashes` | Yes | Database password |
| `DB_ARGUMENTS` | string | `sslmode=disable` | No | Additional connection arguments (e.g., `sslmode=require&connect_timeout=5`) |

The backend builds the connection string dynamically from these variables.

## Authentication & Security

### JWT Configuration

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `JWT_SECRET` | string | - | Yes | Secret key for JWT token signing |
| `JWT_EXPIRATION` | string | `24h` | No | JWT token expiration time |
| `DEFAULT_ADMIN_ID` | string | - | No | User ID of the default admin |

### CORS Configuration

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `CORS_ALLOWED_ORIGIN` | string | `https://localhost:443` | No | Allowed CORS origin |
| `ALLOWED_ORIGINS` | string | `*` | No | Comma-separated list of allowed origins |

## TLS/SSL Configuration

### Certificate Mode

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `KH_TLS_MODE` | string | `self-signed` | No | TLS mode: `self-signed`, `provided`, or `certbot` |
| `KH_CERTS_DIR` | string | `{KH_CONFIG_DIR}/certs` | No | Directory for storing certificates |

### Certificate Details

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `KH_ADDITIONAL_DNS_NAMES` | string | - | No | Comma-separated additional DNS names for certificates |
| `KH_ADDITIONAL_IP_ADDRESSES` | string | - | No | Comma-separated additional IP addresses for certificates |
| `KH_KEY_SIZE` | integer | `4096` | No | RSA key size (2048 or 4096) |
| `KH_SERVER_CERT_VALIDITY` | integer | `365` | No | Server certificate validity in days |
| `KH_CA_CERT_VALIDITY` | integer | `3650` | No | CA certificate validity in days |

### Self-Signed CA Configuration

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `KH_CA_COUNTRY` | string | `US` | No | CA certificate country code |
| `KH_CA_ORGANIZATION` | string | `KrakenHashes` | No | CA organization name |
| `KH_CA_ORGANIZATIONAL_UNIT` | string | `KrakenHashes CA` | No | CA organizational unit |
| `KH_CA_COMMON_NAME` | string | `KrakenHashes Root CA` | No | CA common name |

### User-Provided Certificates

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `KH_CERT_FILE` | string | `{KH_CERTS_DIR}/server.crt` | Yes* | Path to certificate file |
| `KH_KEY_FILE` | string | `{KH_CERTS_DIR}/server.key` | Yes* | Path to private key file |
| `KH_CA_FILE` | string | `{KH_CERTS_DIR}/ca.crt` | No | Path to CA certificate file |

\* Required when `KH_TLS_MODE=provided`

### Let's Encrypt (Certbot) Configuration

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `KH_CERTBOT_DOMAIN` | string | - | Yes* | Domain name for Let's Encrypt |
| `KH_CERTBOT_EMAIL` | string | - | Yes* | Email for Let's Encrypt notifications |
| `KH_CERTBOT_STAGING` | boolean | `false` | No | Use Let's Encrypt staging server |
| `KH_CERTBOT_AUTO_RENEW` | boolean | `true` | No | Enable automatic renewal |
| `KH_CERTBOT_RENEW_HOOK` | string | - | No | Custom hook script after renewal |
| `CLOUDFLARE_API_TOKEN` | string | - | Yes** | Cloudflare API token for DNS-01 challenge |

\* Required when `KH_TLS_MODE=certbot`
\** Required for DNS-01 challenge with Cloudflare

## Logging & Debugging

### Debug Flags

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `DEBUG` | boolean | `false` | No | Enable global debug output |
| `LOG_LEVEL` | string | `INFO` | No | Log level: `DEBUG`, `INFO`, `WARNING`, `ERROR` |
| `DEBUG_SQL` | boolean | `false` | No | Enable SQL query logging |
| `DEBUG_HTTP` | boolean | `false` | No | Enable HTTP request/response logging |
| `DEBUG_WEBSOCKET` | boolean | `false` | No | Enable WebSocket message logging |
| `DEBUG_AUTH` | boolean | `false` | No | Enable authentication debugging |
| `DEBUG_JOBS` | boolean | `false` | No | Enable job processing debugging |

### Log Directories

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `BACKEND_LOG_DIR` | string | `${LOG_DIR}/backend` | No | Backend log directory |
| `FRONTEND_LOG_DIR` | string | `${LOG_DIR}/frontend` | No | Frontend log directory |
| `NGINX_LOG_DIR` | string | `${LOG_DIR}/nginx` | No | Nginx log directory |
| `POSTGRES_LOG_DIR` | string | `${LOG_DIR}/postgres` | No | PostgreSQL log directory |

### Nginx Logging

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `NGINX_ACCESS_LOG_LEVEL` | string | `info` | No | Nginx access log level |
| `NGINX_ERROR_LOG_LEVEL` | string | `warn` | No | Nginx error log level |
| `NGINX_CLIENT_MAX_BODY_SIZE` | string | `50M` | No | Maximum client body size |

## WebSocket Configuration

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `KH_WRITE_WAIT` | duration | `10s` | No | Time allowed to write messages |
| `KH_PONG_WAIT` | duration | `60s` | No | Time to wait for pong response |
| `KH_PING_PERIOD` | duration | `54s` | No | How often to send pings |

Duration format: `10s`, `5m`, `1h`, etc.

## Environment Variable Priority

1. **Explicit environment variables** take precedence
2. **Docker environment files** (`.env`) are loaded next
3. **Default values** are used as fallback

## Best Practices

1. **Security**: Never commit sensitive values (passwords, JWT secrets) to version control
2. **Production**: Always set strong values for `JWT_SECRET`, `DB_PASSWORD`, and certificate configurations
3. **Development**: Use `.env` files for local development configuration
4. **Docker**: Mount configuration directories to persist data between container restarts
5. **Paths**: Use absolute paths for file and directory configurations
6. **Validation**: The backend validates critical environment variables on startup

## Example Configurations

### Minimal Development Setup

```bash
# .env
DB_HOST=localhost
DB_PORT=5432
DB_NAME=krakenhashes
DB_USER=krakenhashes
DB_PASSWORD=krakenhashes
DB_ARGUMENTS=sslmode=disable
JWT_SECRET=dev-secret-change-in-production
DEBUG=true
```

### Production Docker Setup

```bash
# .env.production
PUID=1000
PGID=1000
DB_HOST=postgres
DB_PASSWORD=strong-random-password
JWT_SECRET=very-long-random-secret
KH_TLS_MODE=certbot
KH_CERTBOT_DOMAIN=kraken.example.com
KH_CERTBOT_EMAIL=admin@example.com
CLOUDFLARE_API_TOKEN=your-cloudflare-api-token
DEBUG=false
LOG_LEVEL=WARNING
```

### Agent Configuration

```bash
# Agent environment
KH_DATA_DIR=/opt/krakenhashes-agent/data
KH_CONFIG_DIR=/opt/krakenhashes-agent/config
HASHCAT_EXTRA_PARAMS=-O -w 3
```