# KrakenHashes Quick Start Guide

Get KrakenHashes up and running in under 5 minutes!

## Prerequisites

- Docker Engine 19.03.0+ and Docker Compose v2.0+ installed
- 4GB RAM minimum
- Linux-based system recommended

### Verify Docker Compose Version

```bash
# Check your version - should be 2.0 or higher
docker compose version

# If you have the old docker-compose v1, you'll need to upgrade
```

⚠️ **Important:** This project requires Docker Compose v2 (plugin). The legacy `docker-compose` command will not work.

## 1. Download configuration template

```bash
# Create a directory for KrakenHashes
mkdir krakenhashes && cd krakenhashes

# Download the environment template
wget https://raw.githubusercontent.com/ZerkerEOD/krakenhashes/master/.env.example
cp .env.example .env

# Edit the .env file and change these at minimum:
# - DB_PASSWORD (from default)
# - JWT_SECRET (from default)
# - PUID/PGID (to match your user: run 'id -u' and 'id -g')
```

## 2. Download docker-compose.yml

```bash
# Download the docker-compose configuration
wget https://raw.githubusercontent.com/ZerkerEOD/krakenhashes/master/docker-compose.yml
```

This downloads the latest production docker-compose configuration which includes:

- PostgreSQL with optimized memory settings
- HTTPS (443) and agent (31337) ports
- HTTP port (1337) for non-TLS connections
- Proper volume mounts for data persistence
- Network isolation

## 3. Start KrakenHashes

```bash
# Start the application
docker compose up -d

# Wait for initialization (about 30 seconds)
docker compose logs -f krakenhashes
```

## 4. Access the Application

Open your browser and navigate to:

-   **https://localhost** (redirects to port 443)

**Note:** You'll see a certificate warning because we're using self-signed certificates. This is normal for local development.

## 5. First Login

1. Log in with the default admin credentials:
   - **Username:** `admin`
   - **Password:** `KrakenHashes1!`

2. **Important:** Change the admin password immediately after first login for security

## 6. Quick Test

1. **Upload hashcat binary** (required first):

    - Navigate to Admin → Binaries
    - Click "Upload Binary"
    - Upload your hashcat binary archive (download from https://hashcat.net/hashcat/)
    - The archive contains binaries for all platforms (Linux, Windows, macOS)

    !!! note "System Initialization"
        After uploading your first binary, the system automatically:
        - Verifies the binary integrity
        - Creates the potfile preset job (if not already exists)
        - Makes the binary available to agents

2. **Upload a wordlist**:

    - Navigate to Admin → Wordlists
    - Click "Upload Wordlist"
    - Upload a small wordlist file (e.g., rockyou.txt or a test file)

3. **Create a test hashlist**:

    - Navigate to Hashlists
    - Click "Create Hashlist"
    - Add a few test hashes (e.g., MD5 hashes like `5f4dcc3b5aa765d61d8327deb882cf99` for "password")

4. **Create a job** (when agents are connected):
    - Navigate to your hashlist from the Hashlists page
    - Click on your hashlist to view its details
    - Click "Create Job" from the hashlist management page
    - Choose a preset job template or configure custom settings
    - Start the job

**Note**: Jobs require at least one connected agent to execute. Without agents, jobs will remain in pending status.

## Common Tasks

### View Logs

Log files are stored in `$HOME/krakenhashes/logs/` by default. You can view them in several ways:

```bash
# Live logs from Docker
docker compose logs -f

# Backend logs only
docker compose logs -f krakenhashes

# Check stored log files
tail -f $HOME/krakenhashes/logs/backend/*.log

# Check for errors across all logs
grep -i error $HOME/krakenhashes/logs/*/*.log
```

For debugging, you can enable verbose logging by setting `LOG_LEVEL=DEBUG` in your `.env` file.

### Stop the Application

```bash
docker compose down
```

### Update to Latest Version

```bash
# Pull latest image
docker pull zerkereod/krakenhashes:latest

# Restart with new image
docker compose up -d
```

### Backup Database

```bash
docker exec krakenhashes-postgres pg_dump -U krakenhashes krakenhashes > backup.sql
```

## Troubleshooting

### Cannot access the web interface

1. Check if containers are running: `docker compose ps`
2. Check logs for errors: `docker compose logs`
3. Ensure ports 443, 31337, and 1337 are not in use: `netstat -tlnp | grep -E "443|31337|1337"`

### Database connection errors

1. Ensure PostgreSQL is healthy: `docker compose ps`
2. Check database logs: `docker compose logs postgres`
3. Verify environment variables match in both services

### Certificate warnings

This is normal with self-signed certificates. For production, see the [Installation Guide](installation.md) for proper TLS setup.

## Next Steps

-   **For Users**: Read [Understanding Jobs and Workflows](../user-guide/jobs-workflows.md)
-   **For Admins**: Review the full [Installation Guide](installation.md) for production setup
-   **For Developers**: See [Development Setup](installation.md#development-installation)

## Getting Help

-   Check the [full documentation](../index.md)
-   Report issues on [GitHub](https://github.com/ZerkerEOD/krakenhashes/issues)
-   Join our community chat (Discord): [KrakenHashes](https://discord.gg/taafA9cSFV)
