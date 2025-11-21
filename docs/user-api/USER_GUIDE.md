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

Clients represent organizations or engagements. Each hashlist must be associated with a client.

**Key Points:**
- Client names must be unique
- Clients can only be deleted if they have no associated hashlists
- All operations are scoped to your user account

### 2. Hashlists

Hashlists are collections of password hashes to crack.

**Key Points:**
- Upload via multipart form-data
- Must specify hash type (Hashcat mode number)
- Processing happens in background after upload
- Check `/hash-types` endpoint for supported types

### 3. Agents

Compute agents perform the actual cracking work.

**Key Points:**
- Register agents using voucher codes
- Vouchers can be single-use or continuous
- Agents are associated with the user who generated the voucher
- Monitor agent status and hardware via API

### 4. Jobs

Jobs define cracking tasks (workflows or preset-based).

**Note:** Job endpoints require additional service dependencies and are not yet available in the User API. Use the web interface for job management.

## Common Workflows

### Workflow 1: Upload and Crack Hashes

```bash
# 1. Create a client
CLIENT_ID=$(curl -s -X POST https://your-domain.com/api/v1/clients \
  -H "X-User-Email: user@example.com" \
  -H "X-API-Key: your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"name":"Pentest 2024","description":"Annual pentest engagement"}' \
  | jq -r .id)

# 2. Check available hash types
curl -s -X GET https://your-domain.com/api/v1/hash-types?enabled_only=true \
  -H "X-User-Email: user@example.com" \
  -H "X-API-Key: your-api-key" \
  | jq '.hash_types[] | {id, name}'

# 3. Upload hashlist
curl -X POST https://your-domain.com/api/v1/hashlists \
  -H "X-User-Email: user@example.com" \
  -H "X-API-Key: your-api-key" \
  -F "file=@hashes.txt" \
  -F "name=Domain Hashes" \
  -F "client_id=$CLIENT_ID" \
  -F "hash_type=1000"

# 4. Check hashlist status
curl -s -X GET https://your-domain.com/api/v1/hashlists?client_id=$CLIENT_ID \
  -H "X-User-Email: user@example.com" \
  -H "X-API-Key: your-api-key" \
  | jq .

# 5. Create and manage jobs via web interface (Job API endpoints coming soon)
```

### Workflow 2: Agent Registration

```bash
# 1. Generate a registration voucher
VOUCHER=$(curl -s -X POST https://your-domain.com/api/v1/agents/vouchers \
  -H "X-User-Email: user@example.com" \
  -H "X-API-Key: your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "expires_in": 604800,
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

### Workflow 3: Batch Operations

```python
from krakenhashes import KrakenHashesClient
import glob

client = KrakenHashesClient(
    base_url='https://your-domain.com/api/v1',
    email='user@example.com',
    api_key='your-api-key'
)

# Create a client for this batch
client_obj = client.create_client(name="Batch Upload 2024")
client_id = client_obj['id']

# Upload all hash files from a directory
for hash_file in glob.glob('hashes/*.txt'):
    filename = os.path.basename(hash_file)
    print(f"Uploading {filename}...")

    hashlist = client.create_hashlist(
        name=filename,
        client_id=client_id,
        hash_type=1000,  # NTLM
        file_path=hash_file
    )
    print(f"  Created hashlist ID: {hashlist['id']}")

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
| `INVALID_REQUEST` | Malformed request body | 400 |
| `CLIENT_NOT_FOUND` | Client does not exist or not owned by user | 404 |
| `HASHLIST_NOT_FOUND` | Hashlist does not exist or not owned by user | 404 |
| `AGENT_NOT_FOUND` | Agent does not exist or not owned by user | 404 |
| `INVALID_CREDENTIALS` | Missing or invalid API key | 401 |
| `CLIENT_HAS_HASHLISTS` | Cannot delete client with hashlists | 400 |
| `DUPLICATE_CLIENT_NAME` | Client name already exists | 400 |

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
       if status.get('processing_complete'):
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
        if error_data.get('code') == 'DUPLICATE_CLIENT_NAME':
            print("Client already exists, continuing...")
        else:
            raise
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
1. Verify client exists and you own it:
   ```bash
   curl https://your-domain.com/api/v1/clients/CLIENT_ID \
     -H "X-User-Email: ..." -H "X-API-Key: ..."
   ```
2. Check hash type is valid:
   ```bash
   curl https://your-domain.com/api/v1/hash-types?enabled_only=true \
     -H "X-User-Email: ..." -H "X-API-Key: ..."
   ```
3. Verify file format (one hash per line, plain text)
4. Check file size limits

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
4. Ensure voucher hasn't expired

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

- **Job API Endpoints**: Create, modify, and monitor jobs
- **WebSocket Support**: Real-time job progress updates
- **Bulk Operations**: Batch create/update/delete endpoints
- **Export Endpoints**: Download cracked passwords
- **Statistics API**: Aggregate cracking statistics
- **Webhook Support**: Event notifications for job completion

Stay tuned for updates!
