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

KrakenHashes supports two methods for adding binaries: downloading from a URL or direct file upload.

### Via URL Download

To add a new binary version by downloading from a URL, use the admin API endpoint:

```http
POST /api/admin/binary
Authorization: Bearer <admin_token>
Content-Type: application/json

{
  "binary_type": "hashcat",
  "compression_type": "7z",
  "source_url": "https://github.com/hashcat/hashcat/releases/download/v6.2.6/hashcat-6.2.6.7z",
  "file_name": "hashcat-6.2.6.7z",
  "version": "6.2.6",
  "description": "Official hashcat 6.2.6 release"
}
```

The system will:
1. Download the binary from the specified URL
2. Calculate and store the MD5 hash
3. Verify the download integrity
4. Extract the binary for server-side use
5. Mark the version as active and verified

### Via Direct Upload

For custom-compiled binaries or when URL download isn't available, use the multipart upload endpoint:

```http
POST /api/admin/binary/upload
Authorization: Bearer <admin_token>
Content-Type: multipart/form-data

binary_type: hashcat
compression_type: 7z
version: 7.1.2+338
description: Custom build with additional patches
file: <binary_archive_file>
```

**Form fields:**
| Field | Required | Description |
|-------|----------|-------------|
| `file` | Yes | The binary archive file |
| `binary_type` | Yes | Type of binary (hashcat, john) |
| `compression_type` | Yes | Archive format (7z, zip, tar.gz, tar.xz) |
| `version` | No | Version string for identification |
| `description` | No | Human-readable description |

The system will:
1. Receive and store the uploaded file
2. Calculate and store the MD5 hash
3. Extract the binary for server-side use
4. Mark the version as active and verified

**Use cases for direct upload:**
- Custom-compiled hashcat builds with specific optimizations
- Pre-release or beta versions not yet on GitHub
- Patched versions for specific hardware compatibility
- Internal builds with custom modifications

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

| Field | Type | Nullable | Default | Description |
|-------|------|----------|---------|-------------|
| `id` | SERIAL | No | auto | Unique version identifier |
| `binary_type` | ENUM | No | - | Type of binary (hashcat, john) |
| `compression_type` | ENUM | No | - | Compression format |
| `source_type` | VARCHAR(50) | No | 'url' | Source type: 'url' or 'upload' |
| `source_url` | TEXT | Yes | NULL | Original download URL (NULL for uploads) |
| `file_name` | VARCHAR(255) | No | - | Stored filename |
| `md5_hash` | VARCHAR(32) | No | - | MD5 checksum |
| `file_size` | BIGINT | No | - | File size in bytes |
| `version` | VARCHAR(100) | Yes | NULL | Version string (e.g., "6.2.6", "7.1.2+338") |
| `description` | TEXT | Yes | NULL | Human-readable description |
| `created_at` | TIMESTAMP | Yes | now() | Creation timestamp |
| `created_by` | UUID | No | - | User who added the version |
| `is_active` | BOOLEAN | Yes | true | Whether version is active |
| `is_default` | BOOLEAN | Yes | false | Whether this is the default version |
| `last_verified_at` | TIMESTAMP | Yes | NULL | Last verification time |
| `verification_status` | VARCHAR(50) | Yes | 'pending' | Status: pending, verified, failed, deleted |

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

## Binary Version Patterns

KrakenHashes uses a pattern-based system for specifying binary versions in jobs and agents. Instead of selecting specific binary IDs, you specify patterns that match version families.

### Pattern Types

| Pattern | Example | Description |
|---------|---------|-------------|
| `default` | `"default"` | Matches any binary version (wildcard) |
| Major Wildcard | `"7.x"` | Matches any v7 binary (7.0.0, 7.1.2, 7.2.0, etc.) |
| Minor Wildcard | `"7.1.x"` | Matches any v7.1 binary (7.1.0, 7.1.2, 7.1.5, etc.) |
| Exact | `"7.1.2"` | Matches exactly v7.1.2 (any suffix like 7.1.2-custom) |
| Exact with Suffix | `"7.1.2-NTLMv3"` | Matches only v7.1.2-NTLMv3 specifically |

### Pattern Resolution

When a pattern needs to resolve to an actual binary for download:

1. **Exact patterns**: Find the binary with matching version string
2. **Suffix patterns**: Find the binary with exact version and suffix match
3. **Wildcards**: Find the newest binary matching the pattern
   - `"7.x"` resolves to newest v7.x.x binary available
   - `"7.1.x"` resolves to newest v7.1.x binary available

### Available Patterns API

To see which patterns are available and their resolved binaries:

```http
GET /api/binary/patterns
Authorization: Bearer <token>
```

Response:
```json
{
  "patterns": [
    {"pattern": "default", "resolved_id": 5, "resolved_version": "7.1.2"},
    {"pattern": "7.x", "resolved_id": 5, "resolved_version": "7.1.2"},
    {"pattern": "6.x", "resolved_id": 3, "resolved_version": "6.2.6"},
    {"pattern": "7.1.2-NTLMv3", "resolved_id": 7, "resolved_version": "7.1.2-NTLMv3"}
  ]
}
```

### Using Patterns

**In Jobs**: When creating a job, specify `binary_version` as a pattern:
```json
{
  "binary_version": "7.x",
  "attack_mode": 0,
  "wordlist_ids": ["4"]
}
```

**In Agents**: Configure an agent's binary version pattern:
```json
// PUT /api/admin/agents/{id}/settings
{
  "binaryVersion": "6.x"
}
```

**In Preset Jobs**: Select a pattern when creating preset jobs.

For detailed compatibility rules and scheduling behavior, see [Binary Version Patterns Architecture](../../reference/architecture/binary-version-patterns.md).

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

### Per-Agent Binary Version Patterns

Users can configure individual agents to use specific binary version patterns. This determines which jobs the agent can run and which binary it downloads.

#### Configuration

Agent binary version patterns are set via the Agent Details page or API:

```json
// PUT /api/admin/agents/{id}/settings
{
  "binaryVersion": "7.x"
}
```

Pattern examples:
- `"default"` - Agent can run any job, uses whatever binary is needed
- `"7.x"` - Agent runs v7 jobs only, downloads newest v7 binary
- `"6.x"` - Agent runs v6 jobs only, for driver compatibility
- `"7.1.2-NTLMv3"` - Agent runs only jobs requiring this specific build

For complete pattern syntax and compatibility rules, see [Binary Version Patterns](../../reference/architecture/binary-version-patterns.md).

#### Pattern Resolution Priority

When determining which binary to use for an agent:

1. **Agent Pattern**: Agent uses its configured binary version pattern
2. **Job Pattern**: Job execution's binary version pattern
3. **System Default**: Active default binary

Wildcard patterns (e.g., `"7.x"`) resolve to the newest matching binary available.

#### Impact on Agent Operations

Agent binary version patterns affect:
- **Job Compatibility**: Agents only receive jobs with compatible binary patterns
- **Device Detection**: The agent uses the resolved binary to detect GPU/CPU capabilities
- **Benchmarks**: Performance benchmarks run with the resolved binary for accurate metrics
- **Job Execution**: Tasks execute using the resolved binary

#### Version Compatibility

⚠️ **Hashcat 7.x Compatibility Note**: Hashcat version 7.x may detect GPU devices but fail to recognize them as usable for job execution, particularly with older GPU driver versions. If you experience device detection issues where devices appear in hardware detection but are not available for jobs:

- Use Hashcat 6.x binaries (e.g., 6.2.6, 6.2.5) which have better driver compatibility
- Configure affected agents to use `"6.x"` binary version pattern
- Update GPU drivers to the latest version before trying 7.x binaries

#### Automatic Synchronization

When an agent's binary version pattern is set or changed:
1. Backend sends a `config_update` WebSocket message with the new pattern
2. Agent receives the pattern and updates its configuration
3. Pattern is resolved to an actual binary (newest matching version)
4. If the binary isn't already downloaded, the file sync system downloads it
5. Device detection runs with the resolved binary after download completes

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
| POST | `/api/admin/binary` | Add new binary version via URL download |
| POST | `/api/admin/binary/upload` | Add new binary version via direct upload |
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