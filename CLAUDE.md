# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) and to contributors when working with code in this repository.

## Project Overview

-   Backend server (Go) - REST API and job management
-   Agent system (Go) - Distributed hashcat execution
-   Frontend (React/TypeScript) - Web UI
-   PostgreSQL database with migrations
-   Analytics system - Comprehensive password analysis with domain filtering

## Versioning Strategy

### Semantic Versioning

KrakenHashes follows semantic versioning (major.minor.patch):

-   **Major**: Breaking changes, incompatible API changes
-   **Minor**: New features, backwards-compatible functionality
-   **Patch**: Bug fixes, backwards-compatible fixes

### Version Management Rules

#### Pre-1.0.0 (Development Phase - Completed)

-   **Unified Versioning**: All components (backend, frontend, agent, api, database) maintained the same version number
-   When any component changed, all components incremented to the same version
-   The `release` key tracked the overall release version and matched all component versions

#### Post-1.0.0 (Current Phase)

-   **Release-Tracked Versioning**: The `release` key tracks the overall GitHub release version
-   Components can version independently based on their changes
-   The release version increments when a new GitHub release is created
-   Example: Release 1.2.0 might contain Backend 1.3.0, Frontend 1.2.0, Agent 1.2.1
-   Git tags should match the `release` version (e.g., tag `v1.2.0` for release "1.2.0")

### Version Update Process

When updating versions on the master branch:

1. **Check the last git tag** using `git describe --tags --abbrev=0` to determine the current version
2. **Determine version increment type** (major, minor, or patch) based on the changes
3. **Update versions.json** with new version numbers
4. **Update component-specific version files** if they exist
5. **Update CLAUDE.md** version references if needed
6. **Create git tag** with format `v{version}` (e.g., `v0.11.1`)

#### versions.json Structure

```json
{
    "release": "1.0.0",
    "backend": "1.0.0",
    "frontend": "1.0.0",
    "agent": "1.0.0",
    "api": "1.0.0",
    "database": "1.0.0"
}
```

**Important Notes**:
-   The `release` key tracks the overall GitHub release version and should match git tags
-   Component versions can match the release version or differ based on independent changes
-   Always keep versions.json synchronized with actual component versions
-   This file is the source of truth for version tracking across the project
-   Git release tags should match the `release` version (e.g., `v1.0.0`)

## Common Development Commands

### Agent Development

**For the agent, always use the Makefile to build. This ensures clean builds and proper binary organization.**

```bash
cd agent
make clean && make build-all  # Clean and build for all platforms
make clean && make build       # Clean and build for current platform only
make clean && make linux-amd64 # Clean and build for specific platform

# The binaries will be in ../bin/agent/
```

### Mock Agents for Testing

**Mock agents simulate GPU work without requiring real hardware, enabling:**
- Testing scheduling algorithms
- Integration testing on CI/CD servers
- Development without GPU access
- Simulating multiple agents on one machine

#### Running Mock Agents

Use the `--test-mode` flag to enable mock mode:

```bash
# Start a single mock agent
./agent --host localhost:31337 --claim VOUCHER_CODE --test-mode

# Start multiple mock agents for testing (background processes)
./agent --host localhost:31337 --claim CODE1 --test-mode &
./agent --host localhost:31337 --claim CODE2 --test-mode &
./agent --host localhost:31337 --claim CODE3 --test-mode &

# Or use via .env file
echo "TEST_MODE=true" >> .env
./agent --host localhost:31337 --claim VOUCHER_CODE
```

#### Mock Agent Configuration

Configure mock agent behavior via environment variables:

```bash
# Job completion speed (default: 120 seconds)
MOCK_PROGRESS_SPEED=60  # Complete jobs in 60 seconds

# Crack rate percentage (default: 5%)
MOCK_CRACK_RATE=10  # Crack 10% of hashes

# Simulated hash rate (default: 1 GH/s)
MOCK_HASH_RATE=2000000000  # 2 GH/s

# Number of fake GPUs (default: 2)
MOCK_GPU_COUNT=4  # Report 4 GPUs

# GPU vendor (default: nvidia)
MOCK_GPU_VENDOR=amd  # Options: nvidia, amd, intel

# GPU model (optional, auto-selected based on vendor)
MOCK_GPU_MODEL="AMD Radeon RX 7900 XTX"

# GPU memory in MB (default: 24576 = 24GB)
MOCK_GPU_MEMORY_MB=32768  # 32GB
```

#### Testing Priority-Based Scheduling

Example workflow to test the scheduling system:

```bash
# 1. Start backend
docker-compose -f docker-compose.dev-local.yml up -d backend postgres

# 2. Create 5 vouchers via frontend admin panel

# 3. Start 5 mock agents with different configurations
MOCK_GPU_COUNT=2 MOCK_HASH_RATE=1000000000 ./agent --host localhost:31337 --claim CODE1 --test-mode &
MOCK_GPU_COUNT=4 MOCK_HASH_RATE=2000000000 ./agent --host localhost:31337 --claim CODE2 --test-mode &
MOCK_GPU_COUNT=1 MOCK_HASH_RATE=500000000  ./agent --host localhost:31337 --claim CODE3 --test-mode &
MOCK_GPU_COUNT=2 MOCK_HASH_RATE=1500000000 ./agent --host localhost:31337 --claim CODE4 --test-mode &
MOCK_GPU_COUNT=3 MOCK_HASH_RATE=1800000000 ./agent --host localhost:31337 --claim CODE5 --test-mode &

# 4. Create jobs with different priorities and max_agents settings
# 5. Observe agent allocation in real-time via frontend
# 6. Verify priority-based allocation behavior

# 7. Kill all mock agents when done
pkill -f "agent.*test-mode"
```

#### Mock Mode Behavior

Mock agents:
- ✅ Register and connect via WebSocket (real)
- ✅ Report fake GPU devices (configurable)
- ✅ Accept task assignments (real)
- ✅ Send progress updates (simulated with timers)
- ✅ Generate random cracks (configurable rate)
- ✅ Complete jobs successfully (configurable speed)
- ✅ Support benchmarks (instant fake results)
- ✅ Respond to stop commands (real)
- ❌ No actual hashcat execution
- ❌ No real GPU usage

This enables full integration testing of the backend scheduling system without requiring actual GPU hardware.

### Docker Development (Primary Method for Backend)

**IMPORTANT: Always use docker-compose.dev-local.yml for development. Never use the default docker-compose.yml as it will break the development setup.**

```bash
# Build and run all services (ALWAYS use -f docker-compose.dev-local.yml)
docker-compose down; docker-compose -f docker-compose.dev-local.yml up -d --build

# View logs
docker-compose -f docker-compose.dev-local.yml logs -f backend    # Follow backend logs
docker-compose -f docker-compose.dev-local.yml logs -f postgres   # Follow database logs
docker-compose -f docker-compose.dev-local.yml logs -f app        # Follow nginx/frontend logs

# Stop services
docker-compose -f docker-compose.dev-local.yml down               # Stop containers
docker-compose -f docker-compose.dev-local.yml down -v            # Stop and remove volumes (full reset)

# Rebuild specific service
docker-compose -f docker-compose.dev-local.yml up -d --build backend  # Rebuild only backend
```

### Frontend Development (Standalone)

```bash
cd frontend
npm install            # Install dependencies
npm start             # Start dev server (port 3000)
npm run build         # Production build
npm test              # Run tests
```

### Database Migrations

```bash
cd backend
make migrate-up        # Apply migrations
make migrate-down      # Rollback one migration
# Migrations are auto-applied on startup in Docker
```

## Architecture Overview

### Backend Structure

-   `cmd/server/` - Main entry point
-   `internal/handlers/` - HTTP request handlers organized by domain (auth, admin, agent, etc.)
-   `internal/repository/` - Database access layer with repository pattern
-   `internal/services/` - Business logic layer
-   `internal/models/` - Domain models and types
-   `internal/middleware/` - Auth, CORS, logging middleware
-   `internal/websocket/` - Real-time agent communication
-   `db/migrations/` - Sequential SQL migrations

### Key Backend Patterns

1. **Repository Pattern**: All database access through repositories (e.g., `UserRepository`, `HashlistRepository`)
    - Use `*db.DB` wrapper, not `*sqlx.DB` directly
    - Use standard `database/sql` methods: `Query()`, `QueryRow()`, `Exec()`
    - Manual row scanning with `rows.Scan()` instead of sqlx struct tags
    - Transactions use `db.Begin()` not `db.Beginx()`
2. **Service Layer**: Business logic in services (e.g., `ClientService`, `RetentionService`)
3. **JWT Authentication**: Access/refresh token pattern with MFA support
4. **WebSocket Hub**: Central hub pattern for agent connections
5. **File Management**: Centralized storage in `/data/krakenhashes/` with subdirectories for binaries, wordlists, rules, hashlists
6. **Job Update System**: Automatic keyspace recalculation when wordlists/rules/potfile change
   - Forward-only updates (no deficit tracking)
   - Only affects undispatched work
   - See `docs/reference/architecture/job-update-system.md` for details
7. **Accurate Keyspace Tracking**: Progress values captured from hashcat for precise progress reporting
   - Captures `progress[1]` values from benchmarks and first progress updates
   - Transitions from estimated to actual keyspace values
   - Uses `avg_rule_multiplier` to improve future estimates
   - See `docs/reference/architecture/benchmark-workflow.md` for details
8. **Analytics System**: Pre-calculated analytics reports with domain filtering
   - 13 comprehensive analysis sections (length, complexity, patterns, etc.)
   - Domain-based filtering for multi-domain environments
   - Automatic domain extraction from NetNTLMv2, NTLM, Kerberos hashes
   - Pre-calculated analytics stored as JSONB for instant access
   - See `docs/user-guide/analytics-reports.md` for full details
9. **Priority-Based Scheduling**: Intelligent agent allocation with priority-aware max_agents
   - Higher priority jobs override max_agents limits and take ALL available agents
   - Same priority jobs respect max_agents up to their configured limits
   - Overflow agents (beyond max_agents at same priority) use configurable allocation mode:
     - **FIFO mode (default)**: Oldest job gets all extra agents
     - **Round-robin mode**: Distribute evenly across jobs
   - Smart interruption: Only interrupts as many tasks as needed
     - Lowest priority jobs interrupted first
     - Within same priority: newest jobs interrupted first (FIFO)
   - Jobs without pending work don't block agent allocation
   - System setting: `agent_overflow_allocation_mode` = `"fifo"` | `"round_robin"`

### Agent Architecture

-   `internal/hardware/` - GPU detection (NVIDIA, AMD, Intel)
-   `internal/metrics/` - System monitoring and reporting
-   `internal/sync/` - File synchronization with backend
-   WebSocket-based communication with heartbeat
-   Claim code registration system

### Frontend Patterns

-   Material-UI components throughout
-   React Query for data fetching with caching
-   Context-based authentication state
-   Service layer for API calls (`services/api.ts`)
-   TypeScript interfaces in `types/` directory

## Key Configuration

### Environment Variables

```bash
# Backend
DATABASE_URL=postgres://user:pass@localhost/dbname
JWT_SECRET=your-secret
SERVER_PORT=8080

# TLS/SSL Configuration
KH_TLS_MODE=self-signed                    # Options: self-signed, provided, certbot
KH_ADDITIONAL_DNS_NAMES=localhost,app.local # Additional DNS names for certificates
KH_ADDITIONAL_IP_ADDRESSES=192.168.1.100   # Additional IP addresses for certificates
KH_CERT_KEY_SIZE=4096                      # RSA key size (2048 or 4096)
KH_CERT_VALIDITY_DAYS=365                  # Server certificate validity
KH_CA_VALIDITY_DAYS=3650                   # CA certificate validity (10 years)

# Certbot Configuration (when KH_TLS_MODE=certbot)
KH_CERTBOT_DOMAIN=krakenhashes.example.com # Domain for certificate
KH_CERTBOT_EMAIL=admin@example.com         # Email for ACME account
KH_CERTBOT_CHALLENGE_TYPE=http-01          # Options: dns-01, http-01 (leave empty for auto-detect)
KH_CERTBOT_SERVER=                         # Custom ACME server URL (default: Let's Encrypt)
KH_CERTBOT_STAGING=false                   # Use Let's Encrypt staging environment
KH_CERTBOT_AUTO_RENEW=true                 # Enable automatic certificate renewal
KH_CERTBOT_RENEW_HOOK=                     # Script to run after renewal
KH_CERTBOT_EXTRA_ARGS=                     # Additional certbot arguments
KH_CERTBOT_CUSTOM_CA_CERT=                 # Path to custom CA cert for internal ACME servers

# DNS Provider Credentials (for dns-01 challenge)
CLOUDFLARE_API_TOKEN=                      # Cloudflare API token (required for dns-01 with Cloudflare)

# Frontend
REACT_APP_API_URL=http://localhost:8080
```

### Important Files

-   `docker-compose.yml` - Full stack configuration
-   `backend/internal/config/config.go` - Server configuration
-   `agent/internal/config/config.go` - Agent configuration
-   `versions.json` - Component version tracking (source of truth for all versions)
-   `docs/SSL_TLS_SETUP.md` - SSL/TLS certificate installation guide
-   `docs/SSL_TLS_CERTBOT_CONFIGURATION.md` - Certbot and ACME configuration guide

## Testing Approach

### Backend Testing

-   Unit tests alongside code (`*_test.go`)
-   Integration tests in `integration_test/` subdirectories
-   Mock repositories for service testing
-   Test database for integration tests

### Running Specific Tests

```bash
# Backend
go test -v ./internal/repository -run TestUserRepository
go test -v ./internal/services/...

# Frontend
npm test -- --testNamePattern="Login"
```

## Database Schema

Key tables (94 migrations total):

-   `users` - User accounts with MFA settings
-   `agents` - Registered compute agents
-   `hashlists` - Password hash collections
-   `hashes` - Individual hashes with crack status
-   `clients` - Customer/engagement tracking
-   `job_workflows` - Attack strategy definitions
-   `job_executions` - Job instances with accurate keyspace tracking
-   `job_tasks` - Individual task assignments
-   `wordlists`, `rules` - Attack resources
-   `auth_tokens` - JWT refresh tokens
-   `vouchers` - Agent registration codes

## Security Considerations

1. **Authentication**: JWT with refresh tokens, MFA support (TOTP, email, backup codes)
2. **Authorization**: Role-based (user, admin, agent, system)
3. **Agent Auth**: API key + claim code registration
4. **TLS**: Configurable modes, certificate validation
    - Self-signed certificates with proper extensions for browser compatibility
    - Full certificate chain delivery for proper validation
    - See `docs/SSL_TLS_SETUP.md` for certificate installation instructions
5. **File Access**: Sanitized paths, directory restrictions

## Current Status (v1.0+)

KrakenHashes has reached v1.0+ with core functionality complete:

-   ✅ Hashcat integration fully implemented
-   ✅ Job execution system with accurate keyspace tracking
-   ✅ Agent distribution and management
-   ✅ Progress tracking with hashcat `progress[1]` values
-   🔄 Team management features (planned for v2.0)
-   ⚠️ Production ready with ongoing feature development

## Development Tips

1. Check `internal/models/errors.go` for standard error types
2. Use existing middleware from `internal/middleware/`
3. Follow repository pattern for new database operations
4. WebSocket messages use JSON with `type` field for routing
5. Frontend API calls should use the service layer
6. Database migrations must include both up and down scripts
7. Agent-backend communication uses WebSocket with heartbeat

## Git Commit Guidelines

When asked to commit and push changes, generate descriptive commit messages based on features in areas organized by their primary directory (backend, frontend, agent, docs, etc.). The commit message should be in markdown format and include:

1. **Primary area prefix**: `feat(backend):`, `feat(frontend):`, `feat(agent):`, `feat(docs):`, etc.
2. **Descriptive summary**: Brief overview of what was implemented
3. **Detailed breakdown**: Bulleted list of specific features/changes by component
4. **Impact notes**: Testing, security, or performance implications

### Commit Message Template:

```markdown
feat(area): descriptive summary of main feature

## Components Added/Modified

-   **Component 1**: Description of changes
-   **Component 2**: Description of changes

## Key Features

-   Feature 1 with brief description
-   Feature 2 with brief description

## Testing/Security/Performance Notes

-   Testing coverage added/improved
-   Security enhancements implemented
-   Performance optimizations made
```

After generating the commit message, wait for approval before committing and pushing changes.

## Frontend UI Layout Standards

### Page Layout Consistency

To ensure consistent margins and responsive behavior across all pages:

1. **Use Box instead of Container**: All page components should use `Box` with padding instead of `Container` components with maxWidth constraints

    ```tsx
    // ✅ Correct - responsive margins that adapt to screen size
    return <Box sx={{ p: 3 }}>{/* Page content */}</Box>;

    // ❌ Incorrect - creates large fixed margins on wide screens
    return <Container maxWidth="lg">{/* Page content */}</Container>;
    ```

2. **Avoid maxWidth constraints**: Do not use `maxWidth` on root page elements as it creates artificial compression

    ```tsx
    // ✅ Correct
    <Box sx={{ p: 3 }}>

    // ❌ Incorrect
    <Box sx={{ p: 3, maxWidth: 800, margin: '0 auto' }}>
    ```

3. **Consistent padding**: Use `sx={{ p: 3 }}` (24px) as the standard padding for all pages

4. **Uniform Button Placement**: Primary action buttons should be positioned in the top-right corner of management pages

    ```tsx
    // ✅ Correct - title/description on left, button on right
    <Box sx={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", mb: 3 }}>
        <Box>
            <Typography variant="h4" component="h1" gutterBottom>
                Page Title
            </Typography>
            <Typography variant="body1" color="text.secondary">
                Page description text
            </Typography>
        </Box>
        <Button variant="contained" startIcon={<AddIcon />}>
            Primary Action
        </Button>
    </Box>
    ```

5. **Title Naming Convention**: Use singular form for management page titles
    - ✅ "Wordlist Management" (not "Wordlists Management")
    - ✅ "Rule Management" (not "Rules Management")
    - ✅ "Agent Management" (not "Agents Management")
    - ✅ "Preset Job Management" (not "Preset Jobs Management")

This pattern ensures that content flows naturally across all screen sizes without unnecessary constraints, matching the responsive behavior of the /jobs page, while maintaining consistent visual hierarchy across all management interfaces.
