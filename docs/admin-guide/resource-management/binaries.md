# Binary Management

KrakenHashes provides a comprehensive binary management system for hashcat and other password cracking tools. This document explains how to manage binaries, track versions, and ensure secure distribution to agents.

## Overview

The binary management system allows administrators to:
- Upload and manage multiple versions of hashcat binaries
- Track different binary types and compression formats
- Verify binary integrity with MD5 checksums
- Automatically extract and prepare binaries for server-side execution
- Distribute binaries securely to agents
- Maintain an audit trail of all binary operations

## Understanding Binary Management

### Architecture

The binary management system consists of several components:

1. **Binary Storage**: Files are stored in `<data_dir>/binaries/<version_id>/`
2. **Local Extraction**: Server-side binaries are extracted to `<data_dir>/binaries/local/<version_id>/`
3. **Version Tracking**: Database tracks all binary versions with metadata
4. **Distribution**: Agents download binaries via secure API endpoints
5. **Verification**: Automatic integrity checking with MD5 hashes

### Binary Types

Currently supported binary types:
- `hashcat` - Hashcat password cracking tool
- `john` - John the Ripper (future support)

### Compression Types

Supported compression formats:
- `7z` - 7-Zip archive format
- `zip` - ZIP archive format
- `tar.gz` - Gzip-compressed TAR archive
- `tar.xz` - XZ-compressed TAR archive

## Uploading New Binaries

### Via Admin API

To add a new binary version, use the admin API endpoint:

```http
POST /api/admin/binary
Authorization: Bearer <admin_token>
Content-Type: application/json

{
  "binary_type": "hashcat",
  "compression_type": "7z",
  "source_url": "https://github.com/hashcat/hashcat/releases/download/v6.2.6/hashcat-6.2.6.7z",
  "file_name": "hashcat-6.2.6.7z"
}
```

The system will:
1. Download the binary from the specified URL
2. Calculate and store the MD5 hash
3. Verify the download integrity
4. Extract the binary for server-side use
5. Mark the version as active and verified

### Upload Process

When a binary is uploaded:

1. **Download Phase**: The system downloads the binary with retry logic (up to 3 attempts)
2. **Verification Phase**: MD5 hash is calculated and stored
3. **Storage Phase**: Binary is saved to `<data_dir>/binaries/<version_id>/`
4. **Extraction Phase**: Archive is extracted to `<data_dir>/binaries/local/<version_id>/`
5. **Status Update**: Version status is set to `verified`

## Version Management and Tracking

### Database Schema

Binary versions are tracked in the `binary_versions` table with the following fields:

| Field | Type | Description |
|-------|------|-------------|
| `id` | SERIAL | Unique version identifier |
| `binary_type` | ENUM | Type of binary (hashcat, john) |
| `compression_type` | ENUM | Compression format |
| `source_url` | TEXT | Original download URL |
| `file_name` | VARCHAR(255) | Stored filename |
| `md5_hash` | VARCHAR(32) | MD5 checksum |
| `file_size` | BIGINT | File size in bytes |
| `created_at` | TIMESTAMP | Creation timestamp |
| `created_by` | UUID | User who added the version |
| `is_active` | BOOLEAN | Whether version is active |
| `last_verified_at` | TIMESTAMP | Last verification time |
| `verification_status` | VARCHAR(50) | Status: pending, verified, failed, deleted |

### Verification Status

Binary versions can have the following statuses:
- `pending` - Initial state, download/verification in progress
- `verified` - Successfully downloaded and verified
- `failed` - Download or verification failed
- `deleted` - Binary has been deleted

### Listing Versions

To list all binary versions:

```http
GET /api/admin/binary?type=hashcat&active=true
Authorization: Bearer <admin_token>
```

Query parameters:
- `type` - Filter by binary type
- `active` - Filter by active status (true/false)
- `status` - Filter by verification status

### Getting Latest Version

Agents can retrieve the latest active version:

```http
GET /api/binary/latest?type=hashcat
X-API-Key: <agent_api_key>
```

## Platform-Specific Considerations

### Linux

The system automatically handles Linux-specific binary names:
- Checks for both `hashcat` and `hashcat.bin`
- Sets executable permissions (0750) on extracted binaries

### Windows

- Looks for `hashcat.exe` in extracted archives
- Handles Windows-specific path separators

### Archive Extraction

The extraction process intelligently handles common archive structures:
- Single directory archives: Contents are moved to the target directory
- Multi-file archives: All files are extracted as-is
- Nested structures: Properly flattened during extraction

## Binary Synchronization to Agents

### Agent Download Process

Agents download binaries through the following process:

1. **Version Check**: Agent queries for the latest active version
2. **Download Request**: Agent requests binary download by version ID
3. **Authentication**: API key authentication is required
4. **Streaming Download**: Binary is streamed to the agent
5. **Local Verification**: Agent verifies MD5 hash after download

**Note**: Agents can be configured with a preferred binary version override. When an agent has a binary override set, it will download and use that specific version instead of the latest active version. This allows for per-agent binary selection based on hardware compatibility, testing requirements, or performance optimization.

### API Endpoints for Agents

```http
# Get latest version metadata
GET /api/binary/latest?type=hashcat
X-API-Key: <agent_api_key>

# Download specific version
GET /api/binary/download/{version_id}
X-API-Key: <agent_api_key>
```

### Synchronization Protocol

The agent file sync system (`agent/internal/sync/sync.go`) handles:
- Concurrent downloads with configurable limits
- Retry logic for failed downloads
- Local caching to avoid re-downloads
- Integrity verification with MD5 hashes

### Per-Agent Binary Overrides

Users can configure individual agents to use specific binary versions, overriding system defaults.

#### Configuration

Agent binary overrides are set via the Agent Details page or API:

```json
// PUT /api/agents/{id}
{
  "binaryVersionId": 3,
  "binaryOverride": true
}
```

#### Selection Priority

When determining which binary to use for an agent:

1. **Agent Override**: If enabled, the agent uses its configured binary version
2. **Job Binary**: For specific job executions, use the job's binary version
3. **System Default**: Use the active default binary

#### Impact on Agent Operations

Agent binary overrides affect:
- **Device Detection**: The agent uses the preferred binary to detect GPU/CPU capabilities
- **Benchmarks**: Performance benchmarks run with the preferred binary for accurate metrics
- **Job Execution**: Tasks execute using the agent's preferred binary (unless job specifies otherwise)

#### Version Compatibility

⚠️ **Hashcat 7.x Compatibility Note**: Hashcat version 7.x may detect GPU devices but fail to recognize them as usable for job execution, particularly with older GPU driver versions. If you experience device detection issues where devices appear in hardware detection but are not available for jobs:

- Use Hashcat 6.x binaries (e.g., 6.2.6, 6.2.5) which have better driver compatibility
- Configure affected agents to use 6.x binaries via the binary override feature
- Update GPU drivers to the latest version before trying 7.x binaries

#### Automatic Synchronization

When an agent binary override is set or changed:
1. Backend sends a `config_update` WebSocket message with the preferred binary version
2. Agent receives the preference and updates its configuration
3. If the binary isn't already downloaded, the file sync system downloads it
4. Device detection runs with the preferred binary after download completes

## Updating and Replacing Binaries

### Adding a New Version

To add a new version of hashcat:

1. Upload the new version via the admin API
2. The system automatically downloads and verifies it
3. Previous versions remain available but can be deactivated

### Deactivating Old Versions

```http
DELETE /api/admin/binary/{version_id}
Authorization: Bearer <admin_token>
```

This will:
- Mark the version as inactive (`is_active = false`)
- Set verification status to `deleted`
- Remove the binary file from disk
- Preserve the database record for audit purposes

### Version Verification

To manually verify a binary's integrity:

```http
POST /api/admin/binary/{version_id}/verify
Authorization: Bearer <admin_token>
```

This will:
- Check if the file exists on disk
- Recalculate the MD5 hash
- Compare with stored hash
- Update verification status and timestamp

## Best Practices and Security

### Security Considerations

1. **Source URLs**: Only download binaries from trusted sources
   - Official hashcat releases: https://github.com/hashcat/hashcat/releases
   - Verify SSL certificates for download sources

2. **Hash Verification**: Always verify MD5 hashes after download
   - The system automatically calculates and stores hashes
   - Manual verification can be triggered via API

3. **Access Control**: Binary management requires admin privileges
   - Only administrators can add/remove binaries
   - Agents have read-only access for downloads

4. **File Permissions**: Extracted binaries have restricted permissions (0750)
   - Only the application user can execute binaries
   - Group members have read access

### Operational Best Practices

1. **Version Testing**: Test new binary versions before deployment
   - Upload to a test environment first
   - Verify extraction and execution work correctly
   - Check compatibility with existing jobs

2. **Retention Policy**: Maintain a reasonable number of versions
   - Keep at least 2-3 recent versions for rollback
   - Delete very old versions to save storage space
   - Archive important versions externally

3. **Monitoring**: Regular verification of binary integrity
   - Schedule periodic verification checks
   - Monitor download failures in logs
   - Track agent synchronization success rates

4. **Documentation**: Document version changes
   - Note any breaking changes between versions
   - Track performance improvements
   - Document known issues with specific versions

### Storage Management

1. **Disk Space**: Monitor available disk space
   - Binary archives can be large (100MB+)
   - Extracted binaries double the storage requirement
   - Plan for growth with multiple versions

2. **Cleanup**: Regular cleanup of old versions
   - Delete inactive versions after confirming they're not needed
   - Remove failed download attempts
   - Clean up orphaned extraction directories

## Troubleshooting

### Common Issues

1. **Download Failures**
   - Check network connectivity to source URL
   - Verify SSL/TLS certificates are valid
   - Check firewall rules for outbound HTTPS
   - Review logs for specific error messages

2. **Extraction Failures**
   - Ensure required tools are installed (7z, unzip, tar)
   - Check disk space for extraction
   - Verify archive isn't corrupted
   - Check file permissions on data directory

3. **Verification Failures**
   - File may be corrupted during download
   - Source file may have changed
   - Disk errors could cause corruption
   - Try re-downloading the binary

### Log Locations

Binary management logs are written to:
- Backend logs: Check for `[Binary Manager]` entries
- Download attempts: Look for HTTP client errors
- Extraction logs: Command output is logged
- Verification results: Hash comparison details

### Manual Recovery

If automated processes fail:

1. **Manual Download**: Download binary to a temporary location
2. **Manual Upload**: Place in `<data_dir>/binaries/<version_id>/`
3. **Update Database**: Set correct hash and file size
4. **Manual Extraction**: Extract to local directory
5. **Verify Permissions**: Ensure correct file permissions

## Version.json File

The `versions.json` file in the repository root tracks component versions:

```json
{
    "backend": "0.1.0",
    "frontend": "0.1.0",
    "agent": "0.1.0",
    "api": "0.1.0",
    "database": "0.1.0"
}
```

This file is used for:
- Build-time version embedding
- API version compatibility checks
- Component version tracking
- Release management

Note: This tracks KrakenHashes component versions, not binary tool versions.

## API Reference

### Admin Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/admin/binary` | Add new binary version |
| GET | `/api/admin/binary` | List all versions |
| GET | `/api/admin/binary/{id}` | Get specific version |
| DELETE | `/api/admin/binary/{id}` | Delete/deactivate version |
| POST | `/api/admin/binary/{id}/verify` | Verify binary integrity |

### Agent Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/binary/latest` | Get latest active version |
| GET | `/api/binary/download/{id}` | Download binary file |

## Future Enhancements

Planned improvements to the binary management system:

1. **Automatic Updates**: Check for new releases periodically
2. **Version Channels**: Support for stable/beta/nightly channels
3. **Platform Detection**: Automatic platform-specific binary selection
4. **Signature Verification**: GPG signature verification for downloads
5. **Delta Updates**: Differential updates for minor versions
6. **Binary Caching**: CDN integration for faster agent downloads
7. **Performance Metrics**: Track binary performance across versions