# =============================================================================
# KrakenHashes Agent - NVIDIA CUDA Support
# =============================================================================
# This Dockerfile builds a containerized KrakenHashes agent with NVIDIA GPU
# support via CUDA runtime. Hashcat is NOT bundled - it syncs from the backend
# server during normal operation, identical to bare-metal agent installations.
#
# Prerequisites:
#   - NVIDIA drivers installed on host
#   - NVIDIA Container Toolkit installed on host
#
# Build:
#   GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o krakenhashes-agent ./cmd/agent
#   docker build -t krakenhashes-agent:local .
#
# Run:
#   docker run --gpus all -e KH_HOST=your-server -e KH_CLAIM_CODE=xxx krakenhashes-agent:local
# =============================================================================

ARG CUDA_VERSION=13.1.1
ARG UBUNTU_VERSION=22.04

# Using devel image for CUDA toolkit (nvrtc) required by hashcat for native CUDA support
# CUDA is faster than OpenCL for password cracking
FROM nvidia/cuda:${CUDA_VERSION}-devel-ubuntu${UBUNTU_VERSION}

ARG AGENT_VERSION=dev

# Labels for container metadata
LABEL org.opencontainers.image.title="KrakenHashes Agent" \
      org.opencontainers.image.description="GPU-enabled agent for distributed password cracking with NVIDIA CUDA support" \
      org.opencontainers.image.version="${AGENT_VERSION}" \
      org.opencontainers.image.source="https://github.com/ZerkerEOD/krakenhashes" \
      org.opencontainers.image.vendor="ZerkerEOD" \
      org.opencontainers.image.licenses="MIT"

# Install runtime dependencies required by hashcat
# These packages are needed when hashcat is downloaded from the backend:
#   - ca-certificates: TLS certificate validation for backend connection
#   - pciutils: GPU device detection (lspci)
#   - ocl-icd-libopencl1: OpenCL ICD loader
#   - p7zip-full: Extract hashcat archives (7z format)
# Note: We don't install pocl-opencl-icd as it's CPU-only and conflicts with GPU detection
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    pciutils \
    ocl-icd-libopencl1 \
    p7zip-full \
    && rm -rf /var/lib/apt/lists/*

# Create NVIDIA OpenCL ICD file
# The NVIDIA Container Toolkit mounts libnvidia-opencl.so from the host,
# but we need an ICD file to tell OpenCL applications to use it
RUN mkdir -p /etc/OpenCL/vendors && \
    echo "libnvidia-opencl.so.1" > /etc/OpenCL/vendors/nvidia.icd

# Create non-root user for security
# Using UID/GID 1000 to match common host user IDs
RUN groupadd -g 1000 krakenhashes && \
    useradd -u 1000 -g krakenhashes -d /home/krakenhashes -m -s /bin/bash krakenhashes

# Create directory structure
# /app/config - Agent configuration, certificates, credentials
# /app/data   - Hashcat binaries, wordlists, rules, hashlists (synced from backend)
RUN mkdir -p /app/config /app/data && \
    chown -R krakenhashes:krakenhashes /app

# Copy pre-built agent binary
# Binary must be built for linux/amd64 before docker build:
#   GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o krakenhashes-agent ./cmd/agent
COPY --chown=krakenhashes:krakenhashes krakenhashes-agent /app/krakenhashes-agent
RUN chmod +x /app/krakenhashes-agent

# Set working directory and switch to non-root user
WORKDIR /app
USER krakenhashes

# Environment defaults
# These can be overridden via docker run -e or docker-compose environment
ENV KH_CONFIG_DIR=/app/config \
    KH_DATA_DIR=/app/data \
    USE_TLS=true \
    DEBUG=false \
    LOG_LEVEL=INFO \
    # NVIDIA Container Toolkit environment variables
    NVIDIA_VISIBLE_DEVICES=all \
    NVIDIA_DRIVER_CAPABILITIES=compute,utility

# Health check - verify agent process is running
# start-period allows time for registration and hashcat sync on first run
HEALTHCHECK --interval=30s --timeout=5s --start-period=60s --retries=3 \
    CMD pgrep -f krakenhashes-agent || exit 1

# Entry point - run the agent
# All configuration via environment variables
ENTRYPOINT ["/app/krakenhashes-agent"]
