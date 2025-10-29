# KrakenHashes Installation Guide

This guide covers both production and development installation of KrakenHashes.

## Production Installation

### Prerequisites

-   :material-docker:{ .lg .middle } Docker Engine 20.10+ and Docker Compose 2.0+
-   :material-memory:{ .lg .middle } 4GB RAM minimum, 8GB RAM recommended (see [System Requirements](../reference/system-requirements.md))
-   :material-harddisk:{ .lg .middle } 20GB+ disk space for hash files, wordlists, and rules
-   :material-expansion-card:{ .lg .middle } GPU recommended for optimal performance (NVIDIA, AMD, or Intel)
-   :material-linux:{ .lg .middle } Linux host (Ubuntu 20.04+, Debian 11+, RHEL 8+, or similar)

### Docker Compose v2 Requirements

KrakenHashes requires Docker Compose v2.0 or higher due to:
- Advanced environment variable interpolation syntax
- Improved health check support
- Better service dependency handling

The docker compose.yml uses syntax like `${LOG_DIR:-./logs}/postgres` which requires v2.

#### Installing Docker Compose v2

```bash
# Ubuntu/Debian
sudo apt-get update
sudo apt-get install docker compose-plugin

# CentOS/RHEL/Fedora
sudo yum install docker compose-plugin

# Manual installation (all systems)
sudo curl -L "https://github.com/docker/compose/releases/latest/download/docker compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker compose
sudo chmod +x /usr/local/bin/docker compose

# Verify installation
docker compose version
```

#### Common Issues

If you see this error:
```
ERROR: Invalid interpolation format for "postgres" option in service "services"
```

You're using the old docker compose v1. Install v2 and use `docker compose` (with a space).

### Quick Start with Docker Hub

The easiest way to run KrakenHashes is using the pre-built Docker image from Docker Hub.

#### 1. Create a docker compose.yml file

```yaml
services:
    postgres:
        image: postgres:15-alpine
        container_name: krakenhashes-postgres
        volumes:
            - postgres_data:/var/lib/postgresql/data
        environment:
            - POSTGRES_USER=krakenhashes
            - POSTGRES_PASSWORD=changeme # CHANGE THIS!
            - POSTGRES_DB=krakenhashes
        restart: unless-stopped
        healthcheck:
            test: ["CMD-SHELL", "pg_isready -U krakenhashes"]
            interval: 5s
            timeout: 5s
            retries: 5

    krakenhashes:
        image: zerkereod/krakenhashes:latest
        container_name: krakenhashes-app
        depends_on:
            postgres:
                condition: service_healthy
        ports:
            - "443:443" # Frontend HTTPS
            - "31337:31337" # Backend API HTTPS
            - "1337:1337" # Backend API HTTP
        volumes:
            - krakenhashes_data:/var/lib/krakenhashes
            - ./logs:/var/log/krakenhashes
        environment:
            - DB_HOST=postgres
            - DB_PORT=5432
            - DB_NAME=krakenhashes
            - DB_USER=krakenhashes
            - DB_PASSWORD=changeme # CHANGE THIS!
            - JWT_SECRET=your-secret-key-here # CHANGE THIS!
            - TLS_MODE=self-signed # Options: self-signed, user-provided, certbot
        restart: unless-stopped

volumes:
    postgres_data:
    krakenhashes_data:
```

#### 2. Create a .env file (optional but recommended)

```bash
# Database Configuration
DB_USER=krakenhashes
DB_PASSWORD=your-secure-password
DB_NAME=krakenhashes

# Security
JWT_SECRET=your-very-long-random-string

# TLS Configuration
TLS_MODE=self-signed

# Ports (optional, defaults shown)
FRONTEND_PORT=443
KH_HTTPS_PORT=31337
KH_PORT=1337
```

#### 3. Start KrakenHashes

```bash
# Pull the latest image
docker pull zerkereod/krakenhashes:latest

# Start the services
docker compose up -d

# Check logs
docker compose logs -f
```

#### 4. Access the Application

-   Frontend: https://localhost:443
-   Backend API: https://localhost:31337
-   Default admin credentials: admin:KrakenHashes1!

### Production Configuration

#### TLS/SSL Options

KrakenHashes supports three TLS modes:

1. **self-signed** (default) - Automatically generates self-signed certificates
2. **user-provided** - Use your own certificates
3. **certbot** - Automatically obtain Let's Encrypt certificates (tested and working)

##### Using Your Own Certificates

```yaml
krakenhashes:
    environment:
        - TLS_MODE=user-provided
    volumes:
        - ./certs/server.crt:/etc/krakenhashes/certs/server.crt:ro
        - ./certs/server.key:/etc/krakenhashes/certs/server.key:ro
        - ./certs/ca.crt:/etc/krakenhashes/certs/ca.crt:ro # Optional
```

##### Using Let's Encrypt (Certbot)

!!! warning "Important Limitation"
    Certbot cannot add IP addresses to certificates. You must access the system through the domain name for the certificate to be trusted. If you need IP access, use self-signed or user-provided certificates instead.

```yaml
krakenhashes:
    environment:
        - TLS_MODE=certbot
        - CERTBOT_EMAIL=admin@example.com
        - CERTBOT_DOMAIN=krakenhashes.example.com
```

#### Data Persistence

Important directories that should be persisted:

-   `/var/lib/krakenhashes` - Application data (binaries, wordlists, rules, hashlists)
-   `/var/log/krakenhashes` - Application logs
-   PostgreSQL data volume

#### Environment Variables

| Variable      | Default      | Description                   |
| ------------- | ------------ | ----------------------------- |
| `DB_HOST`     | localhost    | PostgreSQL hostname           |
| `DB_PORT`     | 5432         | PostgreSQL port               |
| `DB_NAME`     | krakenhashes | Database name                 |
| `DB_USER`     | krakenhashes | Database user                 |
| `DB_PASSWORD` | krakenhashes | Database password             |
| `JWT_SECRET`  | (random)     | JWT signing secret            |
| `TLS_MODE`    | self-signed  | TLS certificate mode          |
| `PUID`        | 1000         | User ID for file permissions  |
| `PGID`        | 1000         | Group ID for file permissions |

#### Logging Configuration

KrakenHashes provides comprehensive logging with configurable levels and component-specific debugging:

##### Log Levels

Set the `LOG_LEVEL` environment variable to control logging verbosity:

- `DEBUG` - Detailed debugging information (verbose)
- `INFO` - General information and status updates (default)
- `WARNING` - Warning messages that need attention
- `ERROR` - Error messages only

##### Debug Flags

Enable component-specific debugging with these environment variables:

| Flag | Description |
|------|-------------|
| `DEBUG_SQL` | Log all SQL queries and parameters |
| `DEBUG_HTTP` | Log HTTP requests and responses |
| `DEBUG_WEBSOCKET` | Log WebSocket messages |
| `DEBUG_AUTH` | Log authentication attempts and JWT validation |
| `DEBUG_JOBS` | Log job processing and scheduling |

##### Log Storage

Logs are stored in the following directory structure:

```
$HOME/krakenhashes/logs/
├── backend/      # Backend application logs
├── frontend/     # Nginx access and error logs
├── nginx/        # Nginx configuration logs
└── postgres/     # PostgreSQL database logs
```

To view logs in real-time:

```bash
# All logs
docker compose logs -f

# Specific service
docker compose logs -f backend

# Check for errors
docker compose logs | grep -i error
```

### Production Best Practices

1. **Security**

    - Always change default passwords
    - Use a strong JWT_SECRET (minimum 32 characters)
    - Use proper TLS certificates for production
    - Restrict network access to necessary ports only

2. **Backup**

    - Regular PostgreSQL backups: `docker exec krakenhashes-postgres pg_dump -U krakenhashes krakenhashes > backup.sql`
    - Backup the data volume: `/var/lib/krakenhashes`

3. **Monitoring**

    - Monitor logs in `/var/log/krakenhashes`
    - Set up health checks for the application endpoints
    - Monitor disk space for hash storage

4. **Updates**

    ```bash
    # Pull latest image
    docker pull zerkereod/krakenhashes:latest

    # Recreate container with new image
    docker compose up -d --force-recreate krakenhashes
    ```

### Troubleshooting Production Issues

#### Container won't start

```bash
# Check logs
docker compose logs krakenhashes

# Check if ports are already in use
netstat -tlnp | grep -E "443|31337|1337|5432"
```

#### Database connection issues

```bash
# Test database connectivity
docker exec krakenhashes-app nc -zv postgres 5432

# Check database logs
docker compose logs postgres
```

#### Permission issues

```bash
# Fix ownership (adjust PUID/PGID as needed)
docker exec krakenhashes-app chown -R 1000:1000 /var/lib/krakenhashes
```

---

## Development Installation

### Prerequisites

-   Docker Engine 20.10+ and Docker Compose 2.0+
-   Go 1.23.1+
-   Node.js 20+
-   Git
-   8GB+ RAM recommended for development

### Development Setup Options

#### Option 1: Docker Development Environment (Recommended)

This setup provides hot-reloading for both backend and frontend.

1. **Clone the repository**

    ```bash
    git clone https://github.com/yourusername/krakenhashes.git
    cd krakenhashes
    ```

2. **Start development environment**

    ```bash
    # Start all services with hot-reloading
    docker compose -f docker compose.dev.yml up

    # Or run in background
    docker compose -f docker compose.dev.yml up -d
    ```

3. **Access the services**

    - Frontend: http://localhost:3000 (with hot-reload)
    - Backend API: https://localhost:31337
    - PostgreSQL: localhost:5432

4. **View logs**

    ```bash
    # All services
    docker compose -f docker compose.dev.yml logs -f

    # Specific service
    docker compose -f docker compose.dev.yml logs -f backend
    ```

The development environment features:

-   **Backend**: Uses Air for Go hot-reloading
-   **Frontend**: Uses React development server with hot-reload
-   **Database**: PostgreSQL with persistent volume
-   **Volumes**: Source code mounted for live updates

#### Option 2: Local Development (Traditional)

1. **Clone the repository**

    ```bash
    git clone https://github.com/yourusername/krakenhashes.git
    cd krakenhashes
    ```

2. **Start PostgreSQL**

    ```bash
    cd backend
    docker compose up -d
    cd ..
    ```

3. **Run the backend**

    ```bash
    cd backend
    go mod download
    go run cmd/server/main.go
    ```

4. **Run the frontend**
    ```bash
    cd frontend
    npm install
    npm start
    ```

### Development Configuration

#### Backend Development

The backend uses Air for hot-reloading. Configuration is in `backend/.air.toml`:

```toml
[build]
  cmd = "go build -o ./tmp/main ./cmd/server"
  bin = "./tmp/main"
  include_ext = ["go", "tpl", "tmpl", "html"]
```

Environment variables for development:

```bash
export DB_HOST=localhost
export DB_PORT=5432
export DB_NAME=krakenhashes
export DB_USER=krakenhashes
export DB_PASSWORD=krakenhashes
export JWT_SECRET=dev_jwt_secret
export DEBUG=true
export LOG_LEVEL=DEBUG
```

#### Frontend Development

The frontend uses Create React App with environment variables:

```bash
REACT_APP_API_URL=https://localhost:31337
REACT_APP_WS_URL=wss://localhost:31337
REACT_APP_DEBUG=true
```

### Development Workflows

#### Running Tests

```bash
# Backend tests
cd backend
go test ./...
go test -v ./internal/services

# Frontend tests
cd frontend
npm test
```

#### Building for Production

```bash
# Build production Docker image
docker build -f Dockerfile.prod -t krakenhashes:local .

# Test production build locally
docker compose down
docker compose up -d
```

#### Database Migrations

```bash
# Apply migrations
cd backend
make migrate-up

# Rollback migrations
make migrate-down

# Create new migration
make migrate-create name=add_new_table
```

### Development Tools

#### Logging

View development logs with filtering:

```bash
# Backend logs
tail -f logs/backend.log | grep -i error

# Frontend logs
tail -f logs/frontend.log

# All logs
grep -i error logs/*.log
```

#### Database Access

```bash
# Connect to development database
docker exec -it krakenhashes-postgres-dev psql -U krakenhashes -d krakenhashes

# Quick query
docker exec krakenhashes-postgres-dev psql -U krakenhashes -d krakenhashes -c "SELECT * FROM users;"
```

### Switching Between Development and Production

```bash
# Stop development environment
docker compose -f docker compose.dev.yml down

# Start production environment
docker compose up -d

# Switch back to development
docker compose down
docker compose -f docker compose.dev.yml up
```

### Common Development Issues

#### Port conflicts

```bash
# Check what's using the ports
lsof -i :3000   # Frontend
lsof -i :31337  # Backend HTTPS
lsof -i :5432   # PostgreSQL
```

#### Go module issues

```bash
# Clear module cache
go clean -modcache

# Update dependencies
go mod tidy
go mod download
```

#### Frontend dependency issues

```bash
# Clear npm cache
cd frontend
rm -rf node_modules package-lock.json
npm cache clean --force
npm install
```

## Next Steps

-   Review the configuration settings in your .env file
-   Check the [Admin Documentation](../admin-guide/index.md) for system administration
-   See [User Documentation](../user-guide/index.md) for using KrakenHashes
-   Join our community for support and updates
