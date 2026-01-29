# Docker Deployment

This guide covers deploying the KrakenHashes agent as a Docker container with GPU support.

## Overview

The containerized agent provides the same functionality as the bare-metal installation:

- Connects to your KrakenHashes backend server
- Downloads hashcat from the backend (not bundled in image)
- Syncs wordlists, rules, and hashlists
- Executes cracking jobs with GPU acceleration

**Advantages of container deployment:**

- Simplified dependency management
- Easy updates via image pulls
- Consistent environment across different hosts
- Ideal for container-only infrastructure

## Prerequisites

### NVIDIA GPUs

1. **NVIDIA Drivers** installed on the host system
2. **NVIDIA Container Toolkit** installed

#### Installing NVIDIA Container Toolkit

```bash
# Add NVIDIA repository
distribution=$(. /etc/os-release;echo $ID$VERSION_ID)
curl -s -L https://nvidia.github.io/nvidia-docker/gpgkey | sudo apt-key add -
curl -s -L https://nvidia.github.io/nvidia-docker/$distribution/nvidia-docker.list | \
    sudo tee /etc/apt/sources.list.d/nvidia-docker.list

# Install
sudo apt-get update
sudo apt-get install -y nvidia-container-toolkit

# Configure Docker runtime
sudo nvidia-ctk runtime configure --runtime=docker

# Restart Docker
sudo systemctl restart docker

# Verify installation
docker run --rm --gpus all nvidia/cuda:12.2-base-ubuntu22.04 nvidia-smi
```

### AMD GPUs

1. **AMD GPU** with ROCm support - see [ROCm Compatibility Matrix](https://rocm.docs.amd.com/en/latest/compatibility/compatibility-matrix.html)
2. **ROCm Drivers** installed on the host

#### Installing ROCm Drivers

Follow the official guide: [ROCm Quick Start](https://rocm.docs.amd.com/en/latest/deploy/linux/quick_start.html)

Verify installation:

```bash
rocm-smi
```

## Quick Start

### 1. Create Directory

```bash
mkdir -p ~/krakenhashes-agent
cd ~/krakenhashes-agent
```

### 2. Download Configuration Files

**For NVIDIA GPUs:**

```bash
curl -o docker-compose.yml https://raw.githubusercontent.com/ZerkerEOD/krakenhashes/master/agent/docker-compose.yml
curl -o .env https://raw.githubusercontent.com/ZerkerEOD/krakenhashes/master/agent/.env.example
```

**For AMD GPUs:**

```bash
curl -o docker-compose.yml https://raw.githubusercontent.com/ZerkerEOD/krakenhashes/master/agent/docker-compose.rocm.yml
curl -o .env https://raw.githubusercontent.com/ZerkerEOD/krakenhashes/master/agent/.env.example
```

### 3. Configure Environment

Edit `.env` with your settings:

```bash
nano .env
```

**Required settings:**

```bash
# Your KrakenHashes backend server
KH_HOST=your-server.example.com
KH_PORT=31337

# Claim code from Admin UI -> Agents -> Manage Vouchers
KH_CLAIM_CODE=YOUR-CLAIM-CODE-HERE
```

### 4. Create Voucher

1. Log into your KrakenHashes web interface as admin
2. Navigate to **Agents** → **Manage Vouchers**
3. Click **Create Voucher**
4. Copy the generated code to your `.env` file

### 5. Start the Agent

```bash
docker-compose up -d
```

### 6. Verify Registration

```bash
# Check logs
docker-compose logs -f

# Agent should show:
# - Successful connection to backend
# - Registration complete
# - Device detection
# - File sync in progress
```

The agent should appear in the KrakenHashes web UI under **Agents** with status **Online**.

### 7. Remove Claim Code (Optional)

After successful registration, remove the claim code from `.env`:

```bash
# Edit .env and remove or comment out KH_CLAIM_CODE
nano .env
```

The agent stores its credentials in a Docker volume and doesn't need the claim code after initial registration.

## GPU Configuration

### NVIDIA: Use All GPUs (Default)

```yaml
deploy:
  resources:
    reservations:
      devices:
        - driver: nvidia
          count: all
          capabilities: [gpu, compute, utility]
```

### NVIDIA: Specific Number of GPUs

```yaml
deploy:
  resources:
    reservations:
      devices:
        - driver: nvidia
          count: 2 # Use exactly 2 GPUs
          capabilities: [gpu, compute, utility]
```

### NVIDIA: Specific GPUs by ID

First, list your GPUs:

```bash
nvidia-smi -L
# GPU 0: NVIDIA GeForce RTX 4090 (UUID: GPU-abc123...)
# GPU 1: NVIDIA GeForce RTX 4090 (UUID: GPU-def456...)
```

Then specify by index:

```yaml
deploy:
  resources:
    reservations:
      devices:
        - driver: nvidia
          device_ids: ["0", "2"] # Use GPU 0 and GPU 2
          capabilities: [gpu, compute, utility]
```

Or by UUID (stable across reboots):

```yaml
deploy:
  resources:
    reservations:
      devices:
        - driver: nvidia
          device_ids: ["GPU-abc123-1234-5678-90ab-cdef12345678"]
          capabilities: [gpu, compute, utility]
```

### AMD ROCm Configuration

```yaml
devices:
  - /dev/kfd:/dev/kfd
  - /dev/dri:/dev/dri
group_add:
  - video
  - render
```

## Managing the Agent

### View Logs

```bash
docker-compose logs -f
```

### Stop Agent

```bash
docker-compose down
```

### Restart Agent

```bash
docker-compose restart
```

### Update Agent

```bash
docker-compose pull
docker-compose up -d
```

## Data Persistence

The container uses two Docker volumes:

| Volume                        | Path         | Contents                                       |
| ----------------------------- | ------------ | ---------------------------------------------- |
| `krakenhashes_agent_config`   | `/app/config` | Certificates, API keys, credentials           |
| `krakenhashes_agent_data`     | `/app/data`   | Hashcat, wordlists, rules, hashlists          |

### Backup Volumes

```bash
# Backup config
docker run --rm \
  -v krakenhashes_agent_config:/config \
  -v $(pwd):/backup \
  alpine tar czf /backup/agent_config_backup.tar.gz /config

# Backup data
docker run --rm \
  -v krakenhashes_agent_data:/data \
  -v $(pwd):/backup \
  alpine tar czf /backup/agent_data_backup.tar.gz /data
```

### Restore Volumes

```bash
# Restore config
docker run --rm \
  -v krakenhashes_agent_config:/config \
  -v $(pwd):/backup \
  alpine tar xzf /backup/agent_config_backup.tar.gz -C /

# Restore data
docker run --rm \
  -v krakenhashes_agent_data:/data \
  -v $(pwd):/backup \
  alpine tar xzf /backup/agent_data_backup.tar.gz -C /
```

### Reset Agent (Fresh Start)

```bash
# Stop container
docker-compose down

# Remove volumes (WARNING: loses all data including credentials)
docker volume rm krakenhashes_agent_config krakenhashes_agent_data

# Re-add claim code to .env and start fresh
docker-compose up -d
```

## Environment Variables

| Variable                     | Required | Default  | Description                                   |
| ---------------------------- | -------- | -------- | --------------------------------------------- |
| `KH_HOST`                    | Yes      | -        | Backend server hostname                       |
| `KH_PORT`                    | No       | `31337`  | Backend server port                           |
| `KH_CLAIM_CODE`              | First run| -        | Registration claim code                       |
| `USE_TLS`                    | No       | `true`   | Enable TLS for backend connection             |
| `DEBUG`                      | No       | `false`  | Enable debug logging                          |
| `LOG_LEVEL`                  | No       | `INFO`   | Log level (DEBUG, INFO, WARNING, ERROR)       |
| `HASHCAT_EXTRA_PARAMS`       | No       | -        | Extra hashcat parameters (e.g., `-O -w 3`)    |
| `NVIDIA_VISIBLE_DEVICES`     | No       | `all`    | GPUs to expose (NVIDIA only)                  |

## Troubleshooting

### GPU Not Detected

**NVIDIA:**

```bash
# Verify drivers on host
nvidia-smi

# Verify Container Toolkit
docker run --rm --gpus all nvidia/cuda:12.2-base-ubuntu22.04 nvidia-smi

# Check Docker runtime configuration
cat /etc/docker/daemon.json
```

**AMD:**

```bash
# Verify ROCm on host
rocm-smi

# Check device permissions
ls -la /dev/kfd /dev/dri

# Verify user is in video/render groups
groups
```

### Connection Failed

```bash
# Enable debug mode
DEBUG=true docker-compose up

# Check connectivity to backend
curl -k https://YOUR_SERVER:31337/api/health

# Verify TLS setting matches backend
# If backend uses self-signed cert, USE_TLS=true is correct
```

### Registration Failed

1. Verify claim code is valid and unused
2. Check backend server is reachable
3. Ensure port 31337 (or your configured port) is open
4. Review logs for specific error message:

```bash
docker-compose logs 2>&1 | grep -i "registration\|claim\|error"
```

### File Sync Issues

```bash
# Check sync status in logs
docker-compose logs | grep -i "sync"

# Verify data volume is mounted
docker-compose exec krakenhashes-agent ls -la /app/data

# Manual sync info
docker-compose exec krakenhashes-agent ls -la /app/data/binaries
```

### Container Keeps Restarting

```bash
# Check exit code
docker-compose ps

# View recent logs
docker-compose logs --tail=100

# Check health status
docker inspect krakenhashes-agent | jq '.[0].State.Health'
```

## Running Multiple Agents

To run multiple agents on the same host (e.g., different GPU sets):

```yaml
# docker-compose.multi.yml
services:
  agent-gpu0:
    image: zerkereod/krakenhashes-agent-nvidia:latest
    container_name: krakenhashes-agent-gpu0
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              device_ids: ["0"]
              capabilities: [gpu]
    environment:
      - KH_HOST=${KH_HOST}
      - KH_CLAIM_CODE=${KH_CLAIM_CODE_GPU0}
    volumes:
      - agent0_config:/app/config
      - agent0_data:/app/data

  agent-gpu1:
    image: zerkereod/krakenhashes-agent-nvidia:latest
    container_name: krakenhashes-agent-gpu1
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              device_ids: ["1"]
              capabilities: [gpu]
    environment:
      - KH_HOST=${KH_HOST}
      - KH_CLAIM_CODE=${KH_CLAIM_CODE_GPU1}
    volumes:
      - agent1_config:/app/config
      - agent1_data:/app/data

volumes:
  agent0_config:
  agent0_data:
  agent1_config:
  agent1_data:
```

Each agent needs its own claim code and volumes.

## Docker Images

Two separate repositories are available:

| Repository                              | GPU    | Description                   |
| --------------------------------------- | ------ | ----------------------------- |
| `zerkereod/krakenhashes-agent-nvidia`   | NVIDIA | CUDA-enabled agent image      |
| `zerkereod/krakenhashes-agent-amd`      | AMD    | ROCm-enabled agent image      |

### Available Tags

| Tag       | Description                                |
| --------- | ------------------------------------------ |
| `:latest` | Latest stable release                      |
| `:X.Y.Z`  | Specific version (e.g., `:1.0.5`)          |
| `:dev`    | Development build from master branch       |

## See Also

- [Installation Guide](installation.md) - Bare-metal agent installation
- [Configuration](configuration.md) - All configuration options
- [Troubleshooting](troubleshooting.md) - General troubleshooting guide
- [Device Management](device-management.md) - Managing GPU devices
