# Agent Installation

## Overview

This guide covers installing and setting up KrakenHashes agents on various platforms.

## System Requirements

### Minimum Requirements
- 4GB RAM
- 10GB free disk space (You need enough disk space to cover all wordlists)
- Linux (Ubuntu 20.04+, Debian 11+, RHEL 8+, or similar)
- Network connectivity to backend server

### GPU Requirements (Optional but Recommended)
- NVIDIA: CUDA 11.0+ compatible GPU with 4GB+ VRAM
- AMD: ROCm compatible GPU or OpenCL support
- Intel: OpenCL compatible GPU

## Installation

The agent is distributed as a standalone binary that runs from your home directory. No root access is required for basic installation.

!!! tip "Recommended: install via the launcher for automatic updates"
    The fastest way to install — and the way to get **automatic updates** — is the **Install an
    Agent** panel on the Agent Management page, which generates ready-to-copy commands for your
    platform (install as a service, run in a terminal, or download the binary). Service installs use
    the **launcher**, which keeps the agent up to date on its own. The manual steps below set up a
    standalone agent that you update yourself. See [Auto-Update](auto-update.md) for the full model.

### Step 1: Create Agent Directory

```bash
# Create a directory for the agent in your home folder
mkdir ~/krakenhashes-agent
cd ~/krakenhashes-agent
```

### Step 2: Download the Agent Binary

Download the appropriate binary for your platform from the [GitHub Releases](https://github.com/ZerkerEOD/krakenhashes/releases) page or directly from your KrakenHashes server:

#### Option 1: Download from GitHub Releases

```bash
# For Linux AMD64 (most common)
wget https://github.com/ZerkerEOD/krakenhashes/releases/latest/download/krakenhashes-agent-linux-amd64 -O krakenhashes-agent

# For Linux ARM64
wget https://github.com/ZerkerEOD/krakenhashes/releases/latest/download/krakenhashes-agent-linux-arm64 -O krakenhashes-agent

# For macOS AMD64
wget https://github.com/ZerkerEOD/krakenhashes/releases/latest/download/krakenhashes-agent-darwin-amd64 -O krakenhashes-agent

# For macOS ARM64 (Apple Silicon)
wget https://github.com/ZerkerEOD/krakenhashes/releases/latest/download/krakenhashes-agent-darwin-arm64 -O krakenhashes-agent

# Make the binary executable
chmod +x krakenhashes-agent
```

#### Option 2: Download from KrakenHashes Server

The KrakenHashes web UI provides convenient download options with ready-to-use commands. Navigate to the Agent Downloads page in your installation to access:

- **Direct Download Buttons**: Click to download the agent binary for your platform
- **Copy URL**: Get the direct download URL
- **curl Command**: One-click copy of curl command with SSL bypass flags for self-signed certificates
- **wget Command**: One-click copy of wget command with SSL bypass flags for self-signed certificates

Example commands (replace `your-server` with your actual server URL):

```bash
# Using curl (Linux AMD64)
curl -k -o krakenhashes-agent https://your-server:31337/api/public/agent/download/linux/amd64
chmod +x krakenhashes-agent

# Using wget (Linux AMD64)
wget --no-check-certificate -O krakenhashes-agent https://your-server:31337/api/public/agent/download/linux/amd64
chmod +x krakenhashes-agent
```

**Note:** The `-k` (curl) and `--no-check-certificate` (wget) flags bypass SSL certificate validation, which is necessary when using self-signed certificates. All downloads save as `krakenhashes-agent` regardless of platform.

### Step 3: Verify Installation

```bash
# Verify the binary is executable
ls -la ~/krakenhashes-agent/krakenhashes-agent
# Should show executable permissions (x)

# Check available options
./krakenhashes-agent -help

# Test connectivity (without registering)
./krakenhashes-agent -host your-server:31337 -debug
# This will show connection attempts even without a claim code
```

### Optional: Set up as a Service

For automatic startup and easier management, see the [Systemd Service Setup](systemd-setup.md) guide. This allows the agent to run in the background and start automatically on boot.

## Initial Configuration

The agent supports two configuration methods:

### Method 1: Automatic Configuration (Recommended)

The agent automatically creates a `.env` configuration file on first run. From the agent directory:

```bash
cd ~/krakenhashes-agent

# Run with your server details and claim code
./krakenhashes-agent -host your-server:31337 -claim YOUR_CLAIM_CODE

# The agent will create:
# - .env configuration file
# - config/ directory for certificates
# - data/ directory for files
```

### Method 2: Manual .env File Creation

You can manually create a `.env` file before running the agent:

1. Create a `.env` file in `~/krakenhashes-agent/.env`:

```bash
cd ~/krakenhashes-agent
nano .env  # or your preferred editor
```

2. Add the following configuration:

```bash
# KrakenHashes Agent Configuration

# Server Configuration
KH_HOST=your-server.example.com  # Backend server hostname
KH_PORT=31337                    # Backend server port
USE_TLS=true                     # Use TLS for secure communication
LISTEN_INTERFACE=                # Network interface to bind to
HEARTBEAT_INTERVAL=5             # Heartbeat interval in seconds

# Agent Configuration
KH_CLAIM_CODE=YOUR-CLAIM-CODE-HERE  # Your claim code from Admin UI

# Directory Configuration
KH_CONFIG_DIR=./config  # Configuration directory
KH_DATA_DIR=./data      # Data directory

# WebSocket Timing Configuration
KH_WRITE_WAIT=10s   # Timeout for writing messages
KH_PONG_WAIT=60s    # Timeout for receiving pong
KH_PING_PERIOD=54s  # Ping interval

# File Transfer Configuration
KH_MAX_CONCURRENT_DOWNLOADS=3  # Max concurrent downloads
KH_DOWNLOAD_TIMEOUT=1h         # Download timeout

# Hashcat Configuration
HASHCAT_EXTRA_PARAMS=  # Extra hashcat parameters (see note below)

# Logging Configuration
DEBUG=false            # Enable debug logging
LOG_LEVEL=INFO        # Log level
```

3. Replace the placeholder values with your actual configuration
4. Run the agent: `cd ~/krakenhashes-agent && ./krakenhashes-agent`

**Important Note on HASHCAT_EXTRA_PARAMS:**
- Parameters configured via the frontend (per-agent settings) take precedence
- The .env file parameters are only used as a fallback
- Best practice: Configure parameters via the frontend UI for centralized management

### Post-Configuration

After the first run, the agent will use the `.env` file for all configuration. You can edit this file manually if needed:

```bash
# View/edit the generated configuration
cat .env
nano .env  # or your preferred editor

# Note: After successful registration, the KH_CLAIM_CODE will be automatically commented out
```

## Agent Registration

### Step 1: Generate a Claim Code

In the KrakenHashes Admin UI:
1. Navigate to **Agents → Manage Vouchers**
2. Click **"Create Voucher"**
3. Choose voucher type (one-time or continuous)
4. Copy the generated code

### Step 2: Register the Agent

From your agent directory, run the agent with your claim code:

```bash
cd ~/krakenhashes-agent

# Register and run the agent
./krakenhashes-agent -host your-server:31337 -claim YOUR_CLAIM_CODE

# With debug output (helpful for troubleshooting)
./krakenhashes-agent -host your-server:31337 -claim YOUR_CLAIM_CODE -debug
```

The agent will:
- Connect to the backend server
- Register using the claim code
- Generate certificates and API keys
- Create a `.env` file with your configuration
- Automatically comment out the claim code after successful registration

### Step 3: Running the Agent

After registration, simply run:

```bash
cd ~/krakenhashes-agent
./krakenhashes-agent
```

The agent will use the `.env` file created during registration. You don't need to specify the claim code again.

For automatic startup, see the [Systemd Service Setup](systemd-setup.md) guide.

## GPU Driver Installation
The following is only to help but not a full list or updated each time. Please review what drivers you need for your distribution.

### NVIDIA GPUs

```bash
# Ubuntu/Debian
sudo apt-get update
sudo apt-get install -y nvidia-driver-525 nvidia-cuda-toolkit

# RHEL/CentOS/Rocky
sudo dnf config-manager --add-repo https://developer.download.nvidia.com/compute/cuda/repos/rhel8/x86_64/cuda-rhel8.repo
sudo dnf install -y nvidia-driver cuda
```

### AMD GPUs

```bash
# Install ROCm
wget -q -O - https://repo.radeon.com/rocm/rocm.gpg.key | sudo apt-key add -
echo 'deb [arch=amd64] https://repo.radeon.com/rocm/apt/debian/ ubuntu main' | sudo tee /etc/apt/sources.list.d/rocm.list
sudo apt update
sudo apt install rocm-dev
```

## Verification

1. **Check agent status**:
   
   For manual run, check if the process is running:
   ```bash
   ps aux | grep krakenhashes-agent
   ```
   
   For systemd service:
   ```bash
   # User service
   systemctl --user status krakenhashes-agent
   
   # System service  
   sudo systemctl status krakenhashes-agent
   ```

2. **View logs**:
   
   For manual run, check the terminal output or log files in the agent directory.
   
   For systemd service:
   ```bash
   # User service
   journalctl --user -u krakenhashes-agent -f
   
   # System service
   sudo journalctl -u krakenhashes-agent -f
   ```

3. **Verify in Web UI**:
   - Navigate to Agents section
   - Confirm agent appears as "Online"
   - Check detected devices

## Next Steps

- [Configure the agent](configuration.md)
- [Set up scheduling](scheduling.md)
- [Learn about file synchronization](file-sync.md)