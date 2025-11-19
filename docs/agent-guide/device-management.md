# Device Management

The KrakenHashes agent provides comprehensive device detection, management, and optimization capabilities for password cracking workloads. This guide covers everything you need to know about configuring and optimizing hardware resources.

## Overview

The agent uses hashcat's built-in device detection capabilities to identify and manage compute devices. This approach ensures compatibility with hashcat's device handling and provides accurate performance characteristics for each device.

### Key Features

- **Automatic device detection** using hashcat's `-I` flag
- **Multi-GPU support** with intelligent device allocation
- **Cross-platform compatibility** (Windows, Linux, macOS)
- **Backend optimization** with priority-based device selection
- **Real-time monitoring** during job execution
- **Alias filtering** to prevent duplicate device entries

## Supported Hardware

### NVIDIA GPUs

**Supported backends:**
- CUDA (Primary)
- OpenCL (Fallback)

**Requirements:**
- NVIDIA driver 450.80.02 or newer
- CUDA toolkit 11.0 or newer (for CUDA backend)
- OpenCL 1.2 or newer (for OpenCL backend)

**Installation:**
```bash
# Ubuntu/Debian
sudo apt-get install nvidia-driver nvidia-cuda-toolkit

# Verify installation
nvidia-smi
nvidia-settings --version
```

**Optimal configuration:**
- Use CUDA backend when available (higher performance)
- Enable GPU boost for maximum clock speeds
- Ensure adequate power supply (750W+ for high-end cards)
- Monitor temperatures (keep below 83°C for optimal performance)

### AMD GPUs

**Supported backends:**
- HIP (Primary for modern cards)
- OpenCL (Universal)

**Requirements:**
- AMD Radeon Software 22.7.1 or newer
- ROCm 5.2 or newer (for HIP backend)
- OpenCL 2.0 or newer

**Installation:**
```bash
# Ubuntu/Debian - ROCm installation
wget https://repo.radeon.com/amdgpu-install/5.4/ubuntu/jammy/amdgpu-install_5.4.50400-1_all.deb
sudo dpkg -i amdgpu-install_5.4.50400-1_all.deb
sudo amdgpu-install --usecase=rocm

# Verify installation
rocm-smi
clinfo | grep AMD
```

**Optimal configuration:**
- Use HIP backend for RX 6000/7000 series and newer
- Use OpenCL for older cards (RX 500/Vega series)
- Enable GPU memory overclocking for hash-heavy algorithms
- Monitor junction temperatures (keep below 110°C)

### Intel GPUs

**Supported backends:**
- OpenCL
- Level Zero (Arc series)

**Requirements:**
- Intel Graphics Driver 30.0.101.1404 or newer
- Intel GPU tools for monitoring
- OpenCL runtime

**Installation:**
```bash
# Ubuntu/Debian
sudo apt-get install intel-gpu-tools intel-opencl-icd

# Verify installation
intel_gpu_top
clinfo | grep Intel
```

**Notes:**
- Intel Arc GPUs provide competitive hash rates for certain algorithms
- Integrated graphics can be used for light workloads
- Limited hashcat optimization compared to NVIDIA/AMD

### CPU Processing

**Supported:**
- All x86-64 processors
- ARM processors (limited algorithm support)

**Requirements:**
- Modern multi-core processor
- Sufficient system memory (8GB+ recommended)

**Optimal configuration:**
- Enable all CPU cores for maximum throughput
- Ensure adequate cooling for sustained workloads
- Consider CPU-only for specific algorithms (bcrypt, scrypt)

## Device Detection

### Automatic Detection

The agent automatically detects devices on startup using hashcat's device enumeration:

```bash
./agent -debug  # Enable debug output to see detection process
```

**Detection process:**
1. Locates latest hashcat binary in data directory
2. Executes `hashcat -I` command
3. Parses device information and capabilities
4. Filters aliases and invalid devices
5. Stores device configuration for job allocation

### Device Properties

Each detected device includes:

```json
{
  "device_id": 1,
  "device_name": "NVIDIA GeForce RTX 4090",
  "device_type": "GPU",
  "enabled": true,
  "processors": 128,
  "clock": 2520,
  "memory_total": 24576,
  "memory_free": 23552,
  "pci_address": "01:00.0",
  "backend": "CUDA",
  "is_alias": false
}
```

### Backend Priority

When multiple backends are available for the same device, the agent uses this default priority order:

1. **CUDA** (NVIDIA optimized - best performance)
2. **HIP** (AMD optimized - modern cards)
3. **OpenCL** (Universal fallback - widest compatibility)

This ensures optimal performance by selecting the most efficient backend for each device. However, users can override this selection per device using the runtime selection feature (see below).

## GPU Runtime Selection

Modern GPUs often support multiple compute backends (runtimes) such as CUDA, HIP, and OpenCL. KrakenHashes allows you to select which runtime to use for each physical GPU.

### Understanding Physical Devices and Runtimes

When an agent detects devices, it groups them by physical GPU:
- **Physical Device**: One entry per GPU hardware
- **Runtime Options**: Multiple backends available for that GPU
- **Selected Runtime**: The active backend used for job execution

Example: An AMD Radeon RX 7700S might show:
- Physical Device ID: 0
- Runtime Options:
  - HIP #1 (16 cores, 2208 MHz, 8176 MB)
  - OpenCL #3 (16 cores, 2208 MHz, 8176 MB)
- Selected Runtime: HIP (default)

### Changing Runtime

**Via Web Interface:**
1. Navigate to Agent Details page
2. Locate the "Hardware Configuration" section
3. Find the device you want to modify
4. Use the "Runtime" dropdown to select CUDA, HIP, or OpenCL
5. Changes take effect immediately for new jobs

**Via API:**
```bash
curl -X PATCH http://localhost:8080/api/agents/{agent_id}/devices/{device_id}/runtime \
  -H "Content-Type: application/json" \
  -d '{"runtime": "OpenCL"}'
```

### Runtime Selection Strategy

Default priority when multiple runtimes are available:
1. **CUDA** - NVIDIA GPUs (best performance)
2. **HIP** - AMD GPUs (optimized for modern cards)
3. **OpenCL** - Universal fallback (widest compatibility)

You can override this default by manually selecting a different runtime for any device.

### Hashcat Version Compatibility

Different hashcat versions have varying device detection capabilities:

**Versions 6.2.6+**
- Explicit alias information ("Alias of #X")
- More accurate device grouping
- Recommended for most users

**Versions 6.2.3-6.2.5**
- No alias information
- Uses positional matching for grouping
- Works correctly but less explicit

**All Tested Versions**: 6.2.3, 6.2.4, 6.2.5, 6.2.6, 7.1.2, 7.1.2-47+

### Important Notes

- Changing runtime takes effect immediately for new jobs
- Each physical GPU can only run under one runtime at a time
- Some hashcat algorithms perform better on specific runtimes
- Runtime selection affects benchmark results

## Multi-GPU Configuration

### Automatic Load Balancing

The agent automatically distributes workload across available GPUs:

- **Equal distribution** for identical GPU models
- **Proportional allocation** based on compute capability
- **Dynamic adjustment** based on real-time performance

### Manual Device Selection

You can manually enable/disable specific devices:

```bash
# Through the web interface:
# Agent Details → Device Management → Toggle device status

# Or via API:
curl -X PUT http://localhost:8080/api/agents/{agent_id}/devices/{device_id} \
  -H "Content-Type: application/json" \
  -d '{"enabled": false}'
```

### Optimal Multi-GPU Setups

**Recommended configurations:**

1. **Identical GPUs**: Best performance and load balancing
   ```
   4x RTX 4090 → ~400 GH/s MD5
   8x RTX 3080 → ~640 GH/s MD5
   ```

2. **Mixed GPUs**: Group similar performance tiers
   ```
   2x RTX 4090 + 2x RTX 4080 → Separate job allocation
   ```

3. **CPU + GPU hybrid**: Use CPU for specific algorithms
   ```
   GPU: Fast hashes (MD5, SHA1, NTLM)
   CPU: Slow hashes (bcrypt, scrypt, Argon2)
   ```

## Performance Optimization

### GPU Optimization

**Memory optimization:**
```bash
# Enable optimized kernels (uses more VRAM but faster)
-O

# Set workload tuning
-w 1  # Low power usage
-w 2  # Default
-w 3  # High performance (recommended)
-w 4  # Insane performance (may cause instability)
```

**Hashcat performance flags:**
```bash
# Example optimization for RTX 4090
--gpu-loops 1024 --gpu-accel 128 --gpu-threads 1024
```

### Thermal Management

**Temperature monitoring:**
- NVIDIA: Use `nvidia-smi` or built-in monitoring
- AMD: Use `rocm-smi` or `amdgpu-pro`
- Intel: Use `intel_gpu_top`

**Thermal limits:**
- NVIDIA: 83°C (optimal), 91°C (maximum)
- AMD: 110°C (junction), 95°C (edge)
- Intel: 100°C (throttling)

**Cooling recommendations:**
- Ensure adequate case ventilation
- Monitor ambient temperature (keep below 25°C)
- Consider undervolting for 24/7 operations
- Use custom fan curves for sustained workloads

### Power Management

**Power considerations:**
```bash
# Check GPU power limits
nvidia-smi -q -d POWER     # NVIDIA
rocm-smi --showpower       # AMD
```

**Power optimization:**
- Ensure adequate PSU capacity (add 20% headroom)
- Enable power limit increases where possible
- Monitor power consumption during long jobs
- Consider efficiency curves for different algorithms

## Device Allocation Strategies

### Job-Based Allocation

**Strategy selection:**
1. **Round-robin**: Distribute tasks evenly across devices
2. **Performance-based**: Allocate based on device capability
3. **Memory-based**: Consider VRAM requirements
4. **Thermal-aware**: Avoid overheated devices

### Workload Distribution

**Hash type considerations:**
- **Fast hashes** (MD5, SHA1): Use all available GPUs
- **Medium hashes** (SHA256, SHA512): Balance GPU count vs. memory
- **Slow hashes** (bcrypt, scrypt): May benefit from CPU processing
- **Memory-hard** (Argon2): Requires high VRAM devices

**Example allocations:**
```bash
# Large wordlist + fast hash = all devices
hashcat -m 0 -a 0 hashes.txt wordlist.txt -d 1,2,3,4

# Complex rules + medium hash = subset of devices
hashcat -m 1000 -a 0 hashes.txt wordlist.txt -r rules.txt -d 1,2

# Brute force + slow hash = single high-end device
hashcat -m 3200 -a 3 hashes.txt ?a?a?a?a?a?a -d 1
```

## Benchmarking and Capabilities

### Hashcat Benchmarking

**Run benchmarks:**
```bash
# Full benchmark suite
hashcat -b

# Specific algorithm benchmark
hashcat -b -m 1000  # NTLM benchmark

# Device-specific benchmark
hashcat -b -d 1     # Benchmark only device 1
```

**Interpreting results:**
```
Speed.#1.........:   123.4 GH/s (95.2ms) @ Accel:512 Loops:1024 Thr:64 Vec:1
```
- `123.4 GH/s`: Hash rate (Giga-hashes per second)
- `95.2ms`: Kernel execution time
- `Accel:512`: Acceleration factor
- `Loops:1024`: Iteration loops
- `Thr:64`: Thread count
- `Vec:1`: Vector width

### Performance Baselines

**Expected performance (RTX 4090):**
- MD5: ~100 GH/s
- SHA1: ~35 GH/s
- NTLM: ~180 GH/s
- SHA256: ~15 GH/s
- bcrypt: ~150 KH/s

**Expected performance (RX 7900 XTX):**
- MD5: ~75 GH/s
- SHA1: ~25 GH/s
- NTLM: ~130 GH/s
- SHA256: ~12 GH/s
- bcrypt: ~120 KH/s

## Hardware Requirements

### Minimum Requirements

**System specifications:**
- CPU: 4 cores, 2.5GHz
- RAM: 8GB
- Storage: 100GB available space
- GPU: Any OpenCL 1.2 compatible device
- Network: 100 Mbps for file synchronization

### Recommended Requirements

**High-performance setup:**
- CPU: 16+ cores, 3.0GHz+
- RAM: 32GB+ (64GB for large hashlists)
- Storage: 1TB+ NVMe SSD
- GPU: RTX 4080/4090 or RX 7900 XT/XTX
- Network: 1 Gbps for large file transfers
- PSU: 1000W+ 80+ Gold rated

### Enterprise Requirements

**Large-scale deployment:**
- CPU: 32+ cores server processor
- RAM: 128GB+ ECC memory
- Storage: 10TB+ enterprise SSD array
- GPU: Multiple high-end cards with NVLink/Infinity Cache
- Network: 10 Gbps with redundancy
- Power: Redundant 1600W+ PSUs
- Cooling: Dedicated server room cooling

## Driver Requirements

### NVIDIA Drivers

**Recommended versions:**
- Production: Latest stable driver (535.x+)
- Development: Latest beta driver for new features
- Enterprise: Long-term support versions (470.x LTS)

**Installation verification:**
```bash
nvidia-smi
nvcc --version  # CUDA compiler
nvidia-settings --version
```

### AMD Drivers

**Recommended versions:**
- ROCm: 5.4+ for HIP support
- AMDGPU-PRO: 23.20+ for OpenCL
- Mesa: 23.0+ for open-source stack

**Installation verification:**
```bash
rocm-smi
rocminfo | grep "Agent"
clinfo | grep AMD
```

### Intel Drivers

**Recommended versions:**
- Graphics Driver: 30.0.101.1404+
- OpenCL Runtime: 22.43+
- Level Zero: 1.8+ (Arc series)

**Installation verification:**
```bash
intel_gpu_top
clinfo | grep Intel
vainfo | grep "Driver version"
```

## Troubleshooting Device Issues

### Common Issues

**Device not detected:**
1. Verify driver installation
2. Check hardware compatibility
3. Ensure proper PCI Express connection
4. Verify power supply adequacy
5. Test with hashcat directly: `hashcat -I`

**Poor performance:**
1. Check thermal throttling
2. Verify power limits
3. Update drivers
4. Check for conflicting processes
5. Validate hashcat parameters

**System instability:**
1. Reduce workload tuning (`-w 2` instead of `-w 3`)
2. Lower GPU clocks and memory speeds
3. Improve cooling and power delivery
4. Check for hardware defects
5. Verify system memory integrity

### Debug Commands

**Hardware diagnostics:**
```bash
# System information
lspci | grep -i vga
lshw -c display

# GPU status
nvidia-smi -l 1        # NVIDIA monitoring
rocm-smi -l            # AMD monitoring
intel_gpu_top          # Intel monitoring

# Temperature monitoring
sensors                # System sensors
nvidia-smi dmon        # NVIDIA detailed monitoring
```

**Hashcat diagnostics:**
```bash
# Device information
hashcat -I

# Test device functionality
hashcat -t

# Benchmark specific device
hashcat -b -d 1

# Debug mode
hashcat --debug-mode=1 -m 1000 hash.txt wordlist.txt
```

### Performance Troubleshooting

**If hash rates are lower than expected:**

1. **Check thermal throttling:**
   ```bash
   nvidia-smi dmon -s pucvmet -c 60  # Monitor for 60 seconds
   ```

2. **Verify power limits:**
   ```bash
   nvidia-smi -q -d POWER
   ```

3. **Test with different parameters:**
   ```bash
   # Conservative settings
   hashcat -w 2 -O -m 1000 hash.txt wordlist.txt
   
   # Aggressive settings (may cause instability)
   hashcat -w 4 -O -m 1000 hash.txt wordlist.txt
   ```

4. **Check for competing processes:**
   ```bash
   ps aux | grep -E "(hashcat|john|nvidia|rocm)"
   nvidia-smi pmon  # Process monitoring
   ```

## Best Practices

### Hardware Selection

1. **Match workload to hardware:**
   - Fast hashes: High core count GPUs (RTX 4090, RX 7900 XTX)
   - Slow hashes: High memory bandwidth (RTX 3090, RX 6900 XT)
   - Mixed workloads: Balanced systems with CPU + GPU

2. **Consider total cost of ownership:**
   - Power consumption over lifetime
   - Cooling requirements and costs
   - Maintenance and replacement cycles
   - Performance per dollar ratios

### Configuration Management

1. **Document hardware configurations:**
   - GPU models, VRAM, clock speeds
   - Driver versions and update schedules
   - Optimal hashcat parameters for each device
   - Thermal and power limit settings

2. **Monitor performance trends:**
   - Track hash rates over time
   - Monitor for degradation or throttling
   - Log hardware errors and failures
   - Schedule preventive maintenance

3. **Implement redundancy:**
   - Deploy multiple agents for high availability
   - Use mixed hardware to avoid single points of failure
   - Maintain spare hardware for critical operations
   - Implement proper backup and recovery procedures

### Security Considerations

1. **Physical security:**
   - Secure hardware from unauthorized access
   - Monitor for tampering or theft
   - Implement proper access controls
   - Use hardware-based attestation where possible

2. **Driver security:**
   - Keep drivers updated for security patches
   - Verify driver signatures and authenticity
   - Monitor for driver-level exploits
   - Use enterprise driver branches when available

3. **Performance isolation:**
   - Isolate cracking workloads from other processes
   - Use dedicated hardware for sensitive operations
   - Monitor for unauthorized resource usage
   - Implement resource quotas and limits

By following this comprehensive guide, you'll be able to effectively configure, optimize, and manage hardware resources for maximum password cracking performance while maintaining system stability and security.