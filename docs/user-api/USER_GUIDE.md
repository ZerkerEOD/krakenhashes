# KrakenHashes User API - User Guide

## Overview

The KrakenHashes User API provides programmatic access to core KrakenHashes functionality, allowing you to integrate password cracking workflows into your own tools and automation.

**Base URL**: `https://your-domain.com/api/v1` (or `http://localhost:31337/api/v1` for development)

## Authentication

All API requests require two headers:

- `X-User-Email`: Your KrakenHashes account email
- `X-API-Key`: Your 64-character API key

### Getting Your API Key

1. Log into the KrakenHashes web interface
2. Navigate to **Profile Settings** → **API Keys**
3. Click **Generate API Key**
4. **Copy and save the key immediately** - it will only be shown once!

### Example Authentication

```bash
curl -X GET https://your-domain.com/api/v1/health \
  -H "X-User-Email: user@example.com" \
  -H "X-API-Key: your-64-character-api-key-here"
```

## Core Concepts

### 1. Clients

Clients represent organizations or engagements. Hashlists can optionally be associated with a client.

**Key Points:**
- Client names must be unique
- Clients can only be deleted if they have no associated hashlists
- Client assignment to hashlists may be required based on system settings
- All clients are accessible to all authenticated users

### 2. Hashlists

Hashlists are collections of password hashes to crack.

**Key Points:**
- Upload via multipart form-data
- Must specify hash type ID (Hashcat mode number)
- Processing happens in background after upload
- Check `/hash-types` endpoint for supported types
- Client assignment may be optional or required based on `require_client_for_hashlist` setting

### 3. Agents

Compute agents perform the actual cracking work.

**Key Points:**
- Register agents using voucher codes
- Vouchers can be single-use or continuous
- Agents are associated with the user who generated the voucher
- Monitor agent status and hardware via API

### 4. Jobs

Jobs define cracking tasks using preset configurations.

**Key Points:**
- Jobs link hashlists to preset job configurations
- Control priority (higher = processed first)
- Set `max_agents` to limit concurrent agent allocation
- Monitor progress via layers endpoint for increment mode jobs
- Priority maximum is configurable via `max_job_priority` system setting (default: 1000)

**Job Status Values:**
- `pending` - Job created, waiting to be scheduled
- `running` - Job is actively being processed
- `paused` - Job manually paused
- `completed` - All hashes cracked or exhausted
- `failed` - Job failed due to error

**Increment Mode:**
- `off` - Standard attack (single layer)
- `enabled` - Increment mode enabled
- `enabled_with_brain` - Increment mode with brain feature

## Common Workflows

### Workflow 1: Upload and Crack Hashes

```bash
# 1. Create a client (optional, based on system settings)
CLIENT_ID=$(curl -s -X POST https://your-domain.com/api/v1/clients \
  -H "X-User-Email: user@example.com" \
  -H "X-API-Key: your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"name":"Pentest 2024","description":"Annual pentest engagement"}' \
  | jq -r .id)

# 2. Check available hash types
curl -s -X GET "https://your-domain.com/api/v1/hash-types?enabled_only=true" \
  -H "X-User-Email: user@example.com" \
  -H "X-API-Key: your-api-key" \
  | jq '.hash_types[] | {id, name}'

# 3. Upload hashlist
HASHLIST_ID=$(curl -s -X POST https://your-domain.com/api/v1/hashlists \
  -H "X-User-Email: user@example.com" \
  -H "X-API-Key: your-api-key" \
  -F "file=@hashes.txt" \
  -F "name=Domain Hashes" \
  -F "client_id=$CLIENT_ID" \
  -F "hash_type_id=1000" \
  | jq -r .id)

# 4. Check available preset jobs
curl -s -X GET https://your-domain.com/api/v1/preset-jobs \
  -H "X-User-Email: user@example.com" \
  -H "X-API-Key: your-api-key" \
  | jq '.preset_jobs[] | {id, name}'

# 5. Create a job
JOB_ID=$(curl -s -X POST https://your-domain.com/api/v1/jobs \
  -H "X-User-Email: user@example.com" \
  -H "X-API-Key: your-api-key" \
  -H "Content-Type: application/json" \
  -d "{
    \"hashlist_id\": $HASHLIST_ID,
    \"preset_job_id\": 1,
    \"name\": \"Domain Hash Attack\",
    \"priority\": 100,
    \"max_agents\": 5
  }" \
  | jq -r .id)

# 6. Monitor job progress
curl -s -X GET "https://your-domain.com/api/v1/jobs/$JOB_ID" \
  -H "X-User-Email: user@example.com" \
  -H "X-API-Key: your-api-key" \
  | jq '{status, progress, cracked_count, total_hashes}'
```

### Workflow 2: Agent Registration

```bash
# 1. Generate a registration voucher
VOUCHER=$(curl -s -X POST https://your-domain.com/api/v1/agents/vouchers \
  -H "X-User-Email: user@example.com" \
  -H "X-API-Key: your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "is_continuous": false
  }' \
  | jq -r .code)

echo "Voucher code: $VOUCHER"

# 2. On the agent machine, register with the voucher
./agent --host your-domain.com:31337 --claim $VOUCHER

# 3. Monitor agent status
curl -s -X GET https://your-domain.com/api/v1/agents \
  -H "X-User-Email: user@example.com" \
  -H "X-API-Key: your-api-key" \
  | jq '.agents[] | {id, name, status, gpus: .hardware.gpus | length}'
```

### Workflow 3: Job Management

```bash
# List all jobs with filtering
curl -s -X GET "https://your-domain.com/api/v1/jobs?status=running&page=1&page_size=20" \
  -H "X-User-Email: user@example.com" \
  -H "X-API-Key: your-api-key" \
  | jq .

# Update job priority (boost to run sooner)
curl -s -X PATCH "https://your-domain.com/api/v1/jobs/$JOB_ID" \
  -H "X-User-Email: user@example.com" \
  -H "X-API-Key: your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"priority": 500}'

# Get job layers (for increment mode jobs)
curl -s -X GET "https://your-domain.com/api/v1/jobs/$JOB_ID/layers" \
  -H "X-User-Email: user@example.com" \
  -H "X-API-Key: your-api-key" \
  | jq '.layers[] | {id, name, status, progress}'

# Get tasks within a specific layer
curl -s -X GET "https://your-domain.com/api/v1/jobs/$JOB_ID/layers/1" \
  -H "X-User-Email: user@example.com" \
  -H "X-API-Key: your-api-key" \
  | jq '.tasks[] | {id, status, agent_name, progress}'
```

### Workflow 4: Batch Operations

```python
from krakenhashes import KrakenHashesClient
import glob
import os

client = KrakenHashesClient(
    base_url='https://your-domain.com/api/v1',
    email='user@example.com',
    api_key='your-api-key'
)

# Create a client for this batch
client_obj = client.create_client(name="Batch Upload 2024")
client_id = client_obj['id']

# Get preset job for potfile attack
preset_jobs = client.list_preset_jobs()
potfile_preset_id = next(
    p['id'] for p in preset_jobs['preset_jobs']
    if 'potfile' in p['name'].lower()
)

# Upload all hash files and create jobs
for hash_file in glob.glob('hashes/*.txt'):
    filename = os.path.basename(hash_file)
    print(f"Uploading {filename}...")

    hashlist = client.create_hashlist(
        name=filename,
        client_id=client_id,
        hash_type_id=1000,  # NTLM
        file_path=hash_file
    )
    print(f"  Created hashlist ID: {hashlist['id']}")

    # Create job for this hashlist
    job = client.create_job(
        name=f"Attack {filename}",
        hashlist_id=hashlist['id'],
        preset_job_id=potfile_preset_id,
        priority=100,
        max_agents=3
    )
    print(f"  Created job ID: {job['id']}")

print("Batch upload complete!")
```

## API Reference

### Pagination

List endpoints support pagination via query parameters:

- `page`: Page number (1-indexed, default: 1)
- `page_size`: Items per page (1-100, default: 20)

**Example:**

```bash
curl "https://your-domain.com/api/v1/clients?page=2&page_size=50" \
  -H "X-User-Email: user@example.com" \
  -H "X-API-Key: your-api-key"
```

**Response:**

```json
{
  "clients": [...],
  "page": 2,
  "page_size": 50,
  "total": 127
}
```

### Dynamic Validation

Some API validation rules are configured via system settings:

| Setting | Default | Description |
|---------|---------|-------------|
| `max_job_priority` | 1000 | Maximum allowed job priority value |
| `require_client_for_hashlist` | false | Whether `client_id` is required for hashlist uploads |
| `default_data_retention_months` | null | Default retention period for new clients |

### Error Handling

All errors return JSON with this structure:

```json
{
  "error": "Human-readable error message",
  "code": "MACHINE_READABLE_ERROR_CODE"
}
```

**Common Error Codes:**

| Code | Description | HTTP Status |
|------|-------------|-------------|
| `VALIDATION_ERROR` | Invalid request data | 400 |
| `AUTH_REQUIRED` | Missing or invalid credentials | 401 |
| `RESOURCE_ACCESS_DENIED` | Not authorized to access resource | 403 |
| `RESOURCE_NOT_FOUND` | Resource does not exist | 404 |
| `CLIENT_HAS_HASHLISTS` | Cannot delete client with hashlists | 409 |
| `HASHLIST_HAS_ACTIVE_JOBS` | Cannot delete hashlist with active jobs | 409 |
| `CLIENT_REQUIRED` | Client is required (based on system setting) | 400 |
| `INTERNAL_ERROR` | Server error | 500 |

### Rate Limiting

The User API does not currently implement rate limiting. However, be mindful of:

- File upload sizes (hashlists)
- Background processing capacity
- Database connection limits

## Best Practices

### Security

1. **Protect your API key** like a password
   - Never commit API keys to version control
   - Use environment variables: `export KRAKEN_API_KEY="..."`
   - Rotate keys periodically

2. **Use HTTPS in production**
   - API keys are sent in headers, not encrypted by default
   - Always use TLS/SSL for production deployments

3. **Scope access appropriately**
   - Each user has separate API keys
   - API keys have same permissions as user account

### Performance

1. **Use pagination** for large result sets
   ```python
   # Good: Paginate through results
   page = 1
   while True:
       result = client.list_clients(page=page, page_size=100)
       if not result['clients']:
           break
       process(result['clients'])
       page += 1

   # Bad: Try to load everything at once (may timeout)
   all_clients = client.list_clients(page_size=10000)  # Don't do this
   ```

2. **Check processing status** before proceeding
   ```python
   # Upload hashlist
   hashlist = client.create_hashlist(...)

   # Wait for processing to complete
   import time
   while True:
       status = client.get_hashlist(hashlist['id'])
       if status.get('status') == 'ready':
           break
       time.sleep(5)
   ```

3. **Reuse HTTP connections**
   ```python
   # Good: Client maintains session
   client = KrakenHashesClient(...)
   for i in range(100):
       client.create_client(...)  # Reuses connection

   # Bad: Creating new connections each time
   for i in range(100):
       client = KrakenHashesClient(...)  # New connection overhead
       client.create_client(...)
   ```

### Error Handling

Always handle errors gracefully:

```python
try:
    client.create_client(name="Test")
except requests.exceptions.HTTPError as e:
    if e.response.status_code == 400:
        error_data = e.response.json()
        if error_data.get('code') == 'VALIDATION_ERROR':
            print(f"Validation error: {error_data.get('error')}")
        else:
            raise
    elif e.response.status_code == 409:
        print("Client already exists, continuing...")
    else:
        raise
```

## Examples

### Python

See `examples/python_client.py` for a complete Python client implementation with examples.

**Quick Start:**

```bash
pip install requests
python examples/python_client.py
```

### cURL

See `examples/curl_examples.sh` for comprehensive cURL examples.

**Quick Start:**

```bash
# Edit the script to set your credentials
vim examples/curl_examples.sh

# Run examples
bash examples/curl_examples.sh
```

### Postman

Import the OpenAPI specification (`openapi.yaml`) into Postman:

1. Open Postman
2. Import → Upload File → Select `openapi.yaml`
3. Configure environment variables:
   - `baseUrl`: `https://your-domain.com/api/v1`
   - `userEmail`: Your email
   - `apiKey`: Your API key

## Troubleshooting

### Authentication Fails

**Symptom:** 401 Unauthorized errors

**Solutions:**
1. Verify headers are set correctly:
   ```bash
   curl -v https://your-domain.com/api/v1/health \
     -H "X-User-Email: user@example.com" \
     -H "X-API-Key: your-api-key"
   ```
2. Check for typos in email or API key
3. Regenerate API key if needed (invalidates old key)
4. Ensure you're using the correct base URL

### Hashlist Upload Fails

**Symptom:** 400 Bad Request on hashlist upload

**Solutions:**
1. If `CLIENT_REQUIRED` error, check if system requires client assignment:
   ```bash
   # Either provide a client_id or ask admin to disable requirement
   ```
2. Verify client exists (if provided):
   ```bash
   curl https://your-domain.com/api/v1/clients/CLIENT_ID \
     -H "X-User-Email: ..." -H "X-API-Key: ..."
   ```
3. Check hash type is valid:
   ```bash
   curl "https://your-domain.com/api/v1/hash-types?enabled_only=true" \
     -H "X-User-Email: ..." -H "X-API-Key: ..."
   ```
4. Verify file format (one hash per line, plain text)
5. Check file size limits

### Job Creation Fails

**Symptom:** 400 Bad Request on job creation

**Solutions:**
1. Verify hashlist exists and is ready:
   ```bash
   curl https://your-domain.com/api/v1/hashlists/HASHLIST_ID \
     -H "X-User-Email: ..." -H "X-API-Key: ..."
   ```
2. Verify preset job exists:
   ```bash
   curl https://your-domain.com/api/v1/preset-jobs \
     -H "X-User-Email: ..." -H "X-API-Key: ..."
   ```
3. Check priority is within allowed range (default max: 1000)
4. Ensure `max_agents` is a positive integer

### Agent Not Appearing

**Symptom:** Agent registers but doesn't appear in list

**Solutions:**
1. Verify voucher was generated by your account:
   ```bash
   curl https://your-domain.com/api/v1/agents \
     -H "X-User-Email: ..." -H "X-API-Key: ..."
   ```
2. Check agent logs for connection errors
3. Verify network connectivity to backend
4. Ensure voucher is still active

## OpenAPI Specification

The complete API specification is available in `openapi.yaml`. This can be used with:

- **Swagger UI**: Interactive API documentation
- **Postman**: Import for testing
- **Code generators**: Generate client libraries in various languages

To view the specification in Swagger UI:

```bash
docker run -p 8080:8080 \
  -e SWAGGER_JSON=/specs/openapi.yaml \
  -v $(pwd)/openapi.yaml:/specs/openapi.yaml \
  swaggerapi/swagger-ui
```

Then open: http://localhost:8080

## Support

### Documentation

- **OpenAPI Spec**: `openapi.yaml`
- **Python Examples**: `examples/python_client.py`
- **cURL Examples**: `examples/curl_examples.sh`
- **Main Docs**: `../../CLAUDE.md`

### Issues

Report issues at: https://github.com/ZerkerEOD/krakenhashes/issues

When reporting issues, include:
- API endpoint being called
- Request headers (redact API key!)
- Request body
- Response status and body
- Expected vs. actual behavior

## Future Enhancements

The following features are planned for future releases:

- **WebSocket Support**: Real-time job progress updates
- **Bulk Operations**: Batch create/update/delete endpoints
- **Export Endpoints**: Download cracked passwords
- **Statistics API**: Aggregate cracking statistics
- **Webhook Support**: Event notifications for job completion

Stay tuned for updates!
