# Docker Initialization Guide

!!! info "See also"
    Running agents in Docker is a separate page: [Agent guide → Docker deployment](../../agent-guide/docker-deployment.md).

## Overview
This guide explains how to initialize and run KrakenHashes using Docker. KrakenHashes runs as a single container that includes the frontend, backend, and PostgreSQL database.

## Prerequisites
- Docker 20.10.0 or higher
- Docker Compose v2.0.0 or higher
- At least 4GB of free disk space

## Environment Configuration
The application uses a single `.env` file for all configuration. Copy the `.env.example` file:

```bash
cp .env.example .env
```

### Important Environment Variables

#### Logging Configuration
- `DEBUG`: Set to 'true' or '1' to enable debug output
- `LOG_LEVEL`: Controls message verbosity (DEBUG, INFO, WARNING, ERROR)
- `DEBUG_SQL`: Enable SQL query logging
- `DEBUG_HTTP`: Enable HTTP request/response logging
- `DEBUG_WEBSOCKET`: Enable WebSocket message logging
- `DEBUG_AUTH`: Enable authentication debugging
- `DEBUG_JOBS`: Enable job processing debugging

#### Log File Locations
All logs are written to both stdout/stderr and files within the container:
- `/var/log/krakenhashes/`: Base log directory mounted from host
  - `backend/`: Backend service logs
  - `frontend/`: Frontend service logs
  - `nginx/`: Nginx access and error logs
  - `postgres/`: PostgreSQL logs

## Starting the Service

1. Build and start the service:
   ```bash
   docker-compose up --build
   ```

2. Verify the service is running:
   ```bash
   docker-compose ps
   ```

## Debugging

### Viewing Logs
1. Real-time container logs:
   ```bash
   docker-compose logs -f
   ```

2. Access log files directly from host machine:
   ```bash
   ls /var/log/krakenhashes/
   ```

### Common Issues
1. Database Connection Issues
   - Check PostgreSQL logs in `/var/log/krakenhashes/postgres/`
   - Verify database credentials in .env
   - Ensure database port is not in use

2. Certificate Issues
   - Verify TLS configuration in .env
   - Check certificate paths
   - Ensure proper permissions on certificate files

## Maintenance

### Database Backups
PostgreSQL data is persisted in a named volume. To backup:
```bash
docker-compose exec krakenhashes pg_dump -U krakenhashes > backup.sql
```

### Log Rotation
Logs are automatically rotated using logrotate with the following policy:
- Maximum size: 100MB
- Retention: 30 days
- Compression: enabled

## Security Notes
1. Change default passwords in .env
2. Secure the log directory permissions
3. Regular security updates
4. Monitor log files for suspicious activity 