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
| **Metadata** | `GET /hash-types` | List hash types |
| | `GET /workflows` | List workflows |
| | `GET /preset-jobs` | List preset jobs |

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

✅ **Available Now:**
- Client management (CRUD)
- Hashlist management (upload, list, delete)
- Agent management (vouchers, list, update)
- Metadata endpoints (hash types, workflows, presets)

⏳ **Coming Soon:**
- Job API endpoints (create, modify, monitor)
- WebSocket support for real-time updates

## Support

- **Issues**: https://github.com/ZerkerEOD/krakenhashes/issues
- **Main Documentation**: See `../../CLAUDE.md`
- **User Guide**: [USER_GUIDE.md](USER_GUIDE.md)

## Version

Current API Version: **v1.0.0**

Last Updated: 2024
