# KrakenHashes User API Documentation

This directory contains comprehensive documentation for the KrakenHashes User API.

## Quick Start

1. **Get your API key**: Log into KrakenHashes → Profile Settings → Generate API Key
2. **Read the User Guide**: [USER_GUIDE.md](USER_GUIDE.md)
3. **Try the examples**: See `examples/` directory

## Documentation Files

- **[USER_GUIDE.md](USER_GUIDE.md)** - Complete user guide with workflows and best practices
- **[openapi.yaml](openapi.yaml)** - OpenAPI 3.0 specification
- **[examples/python_client.py](examples/python_client.py)** - Python client library and examples
- **[examples/curl_examples.sh](examples/curl_examples.sh)** - cURL command examples

## API Endpoints

### Authentication
All endpoints require `X-User-Email` and `X-API-Key` headers.

### Available Endpoints

| Category | Endpoint | Description |
|----------|----------|-------------|
| **Health** | `GET /health` | API health check |
| **Clients** | `GET /clients` | List clients |
| | `POST /clients` | Create client |
| | `GET /clients/{id}` | Get client |
| | `PATCH /clients/{id}` | Update client |
| | `DELETE /clients/{id}` | Delete client |
| **Hashlists** | `GET /hashlists` | List hashlists |
| | `POST /hashlists` | Upload hashlist |
| | `GET /hashlists/{id}` | Get hashlist |
| | `DELETE /hashlists/{id}` | Delete hashlist |
| **Agents** | `POST /agents/vouchers` | Generate voucher |
| | `GET /agents` | List agents |
| | `GET /agents/{id}` | Get agent |
| | `PATCH /agents/{id}` | Update agent |
| | `DELETE /agents/{id}` | Disable agent |
| **Jobs** | `GET /jobs` | List jobs |
| | `POST /jobs` | Create job |
| | `GET /jobs/{id}` | Get job |
| | `PATCH /jobs/{id}` | Update job |
| | `GET /jobs/{id}/layers` | Get job layers (increment mode) |
| | `GET /jobs/{id}/layers/{layer_id}` | Get tasks for a layer |
| **Metadata** | `GET /hash-types` | List hash types |
| | `GET /workflows` | List workflows |
| | `GET /preset-jobs` | List preset jobs |

## Dynamic Validation

Some API validation rules are configured via system settings:

| Setting | Default | Description |
|---------|---------|-------------|
| `max_job_priority` | 1000 | Maximum allowed job priority value |
| `require_client_for_hashlist` | false | Whether `client_id` is required for hashlist uploads |
| `default_data_retention_months` | null | Default retention period for new clients |

These settings can be adjusted by administrators through the system settings interface.

## Examples

### Quick Test

```bash
# Set your credentials
export KRAKEN_EMAIL="user@example.com"
export KRAKEN_API_KEY="your-64-character-api-key"

# Health check (no auth required)
curl http://localhost:31337/api/v1/health

# List clients
curl http://localhost:31337/api/v1/clients \
  -H "X-User-Email: $KRAKEN_EMAIL" \
  -H "X-API-Key: $KRAKEN_API_KEY"
```

### Python

```python
from python_client import KrakenHashesClient

client = KrakenHashesClient(
    base_url='http://localhost:31337/api/v1',
    email='user@example.com',
    api_key='your-api-key'
)

# Create a client
client_obj = client.create_client(name="My Client")
print(f"Created: {client_obj['id']}")

# List hash types
hash_types = client.list_hash_types(enabled_only=True)
print(f"Available hash types: {hash_types['total']}")
```

## Viewing OpenAPI Documentation

### With Swagger UI

```bash
docker run -p 8080:8080 \
  -e SWAGGER_JSON=/specs/openapi.yaml \
  -v $(pwd)/openapi.yaml:/specs/openapi.yaml \
  swaggerapi/swagger-ui
```

Open: http://localhost:8080

### With Redoc

```bash
docker run -p 8080:80 \
  -e SPEC_URL=openapi.yaml \
  -v $(pwd)/openapi.yaml:/usr/share/nginx/html/openapi.yaml \
  redocly/redoc
```

Open: http://localhost:8080

### Import to Postman

1. Open Postman
2. Import → Upload File → `openapi.yaml`
3. Set environment variables for `baseUrl`, `userEmail`, `apiKey`

## Current Status

**All User API endpoints are fully implemented:**

- Client management (CRUD with data retention settings)
- Hashlist management (upload, list, delete)
- Agent management (vouchers, list, update, delete)
- Job management (create, list, update, layers)
- Metadata endpoints (hash types, workflows, preset jobs)

**Planned Enhancements:**
- WebSocket support for real-time job status updates

## Support

- **Issues**: https://github.com/ZerkerEOD/krakenhashes/issues
- **Main Documentation**: See the [documentation site](../index.md)
- **User Guide**: [USER_GUIDE.md](USER_GUIDE.md)

## Version

Current API Version: **v1.0.0**

Last Updated: November 2025
