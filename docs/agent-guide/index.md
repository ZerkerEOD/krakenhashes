# Agent Guide

## Overview

Agents are the computational workhorses of KrakenHashes, responsible for executing password cracking jobs using hashcat. This guide covers all aspects of agent deployment, configuration, and management.

## Quick Start

1. **Download the agent binary** from the Releases page
2. **Generate a claim code** in the Admin UI
3. **Register the agent**:
   ```bash
   ./krakenhashes-agent -claim YOUR_CLAIM_CODE -host IP:31337
   ```
4. **Start the agent**:
   ```bash
   ./krakenhashes-agent
   ```

## Guide Contents

### Setup and Configuration
- [Installation](installation.md) - Installing and setting up agents
- [Auto-Update](auto-update.md) - Self-updating agents and the launcher
- [Configuration](configuration.md) - Agent configuration options
- [Systemd Service Setup](systemd-setup.md) - Running agents as systemd services
- [File Synchronization](file-sync.md) - How agents sync files with the backend

### Operations
- [Scheduling](scheduling.md) - Configure working hours and availability
- [Device Management](device-management.md) - GPU and device configuration
- [Monitoring](monitoring.md) - Performance metrics and health checks

### Troubleshooting
- [Common Issues](troubleshooting.md) - Solutions to frequent problems
- [Logs and Debugging](debugging.md) - Finding and understanding agent logs

## Key Concepts

### Agent Registration

Agents use a claim code system for secure registration:
- **One-time codes**: Single use, automatically deactivated
- **Continuous codes**: Can register multiple agents
- **API keys**: Generated during registration for ongoing authentication

### Device Support

KrakenHashes agents support multiple device types:
- NVIDIA GPUs (CUDA)
- AMD GPUs (OpenCL)
- Intel GPUs (OpenCL)
- CPU-based cracking (fallback)

### File Management

Agents automatically manage required files:
- Wordlists are downloaded on-demand
- Rules are cached locally
- Hashcat binaries are auto-updated
- All files are verified using checksums

### Security

Agent security features:
- TLS encrypted communication
- API key authentication
- No inbound connections required
- Certificate validation

## Next Steps

- [Install your first agent](installation.md)
- [Configure agent scheduling](scheduling.md)
- [Learn about file synchronization](file-sync.md)