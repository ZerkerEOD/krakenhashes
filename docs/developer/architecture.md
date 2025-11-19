# KrakenHashes System Architecture

## Table of Contents

1. [Overview](#overview)
2. [High-Level Architecture](#high-level-architecture)
3. [Backend Architecture](#backend-architecture)
4. [Frontend Architecture](#frontend-architecture)
5. [Agent Architecture](#agent-architecture)
6. [Communication Protocols](#communication-protocols)
7. [Database Schema](#database-schema)
8. [Security Architecture](#security-architecture)
9. [File Storage Architecture](#file-storage-architecture)
10. [Deployment Architecture](#deployment-architecture)

## Overview

KrakenHashes is a distributed password cracking management system designed to orchestrate and manage hashcat operations across multiple compute agents. The system follows a client-server architecture with a centralized backend, web-based frontend, and distributed agent nodes.

### Key Components

- **Backend Server** (Go): REST API server managing job orchestration, user authentication, and agent coordination
- **Frontend** (React/TypeScript): Web UI for system management and monitoring
- **Agent** (Go): Distributed compute nodes executing hashcat jobs
- **PostgreSQL Database**: Persistent storage for system data
- **File Storage**: Centralized storage for binaries, wordlists, rules, and hashlists

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                            Frontend (React)                              │
│                         Material-UI Components                           │
│                      React Query + TypeScript                            │
└────────────────────────────────┬────────────────────────────────────────┘
                                 │ HTTPS/REST API
                                 │ WebSocket
┌────────────────────────────────▼────────────────────────────────────────┐
│                          Backend Server (Go)                             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌────────────┐ │
│  │   Handlers   │  │   Services   │  │ Repositories │  │ Middleware │ │
│  │  (HTTP/WS)   │  │ (Business)   │  │   (Data)     │  │   (Auth)   │ │
│  └──────────────┘  └──────────────┘  └──────────────┘  └────────────┘ │
└────────────────────────────────┬────────────────────────────────────────┘
                                 │ SQL
                                 │
┌────────────────────────────────▼────────────────────────────────────────┐
│                         PostgreSQL Database                              │
│                    (Users, Agents, Jobs, Hashlists)                     │
└─────────────────────────────────────────────────────────────────────────┘
                                 
┌─────────────────────────────────────────────────────────────────────────┐
│                          Agent Nodes (Go)                                │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌────────────┐ │
│  │   Hardware   │  │     Job      │  │     Sync     │  │ Heartbeat  │ │
│  │  Detection   │  │  Execution   │  │   Manager    │  │  Manager   │ │
│  └──────────────┘  └──────────────┘  └──────────────┘  └────────────┘ │
└─────────────────────────────────────────────────────────────────────────┘
```

## Backend Architecture

### Layered Architecture

The backend follows a clean layered architecture with clear separation of concerns:

#### 1. **Presentation Layer** (`internal/handlers/`)
- HTTP request handlers organized by domain
- WebSocket handlers for real-time communication
- Request validation and response formatting

**Key Packages:**
- `admin/` - Administrative functions (users, clients, settings)
- `agent/` - Agent management and registration
- `auth/` - Authentication and authorization
- `hashlist/` - Hashlist management
- `jobs/` - Job execution and monitoring
- `websocket/` - WebSocket connection handling

#### 2. **Service Layer** (`internal/services/`)
- Business logic implementation
- Transaction management
- Cross-cutting concerns (scheduling, monitoring)

**Key Services:**
- `AgentService` - Agent lifecycle management
- `JobExecutionService` - Job orchestration
- `JobSchedulingService` - Task distribution with modular architecture (v1.1+)
  - **Modular Components:**
    - `job_scheduling_service.go` (600+ lines) - Main scheduling coordinator
    - `job_scheduling_benchmark_planning.go` (684 lines) - Parallel benchmark planning and execution
    - `job_scheduling_task_assignment.go` (710 lines) - Parallel task assignment with two-phase execution
  - **Performance Characteristics:**
    - Parallel benchmarking: 96% improvement (15 agents: 450s → 12s)
    - Parallel task assignment: 95% improvement (15 agents: 450s → 20s)
    - Goroutine-based concurrent operations for scalability
  - **Key Features:**
    - Priority-based allocation with overflow control (FIFO/round-robin modes)
    - Round-robin benchmark distribution
    - Two-phase task planning (sequential planning, parallel execution)
    - Forced benchmark agent prioritization
- `ClientService` - Customer management
- `RetentionService` - Automated data purging with secure deletion
- `WebSocketService` - Real-time communication hub
- `HashlistSyncService` - File synchronization to agents
- `MetricsCleanupService` - Agent metrics pruning

#### 3. **Repository Layer** (`internal/repository/`)
- Database access abstraction
- SQL query execution
- Data mapping

**Key Repositories:**
- `UserRepository` - User account management
- `AgentRepository` - Agent registration and status
- `HashlistRepository` - Hashlist storage
- `JobExecutionRepository` - Job tracking
- `JobTaskRepository` - Task management

#### 4. **Infrastructure Layer**
- Database connections (`internal/database/`)
- File storage (`internal/binary/`, `internal/wordlist/`, `internal/rule/`)
- External integrations (email providers)
- TLS/SSL management (`internal/tls/`)

### Design Patterns

1. **Repository Pattern**: All database operations go through repository interfaces
2. **Service Layer Pattern**: Business logic separated from data access
3. **Middleware Pattern**: Cross-cutting concerns (auth, logging, CORS)
4. **Hub Pattern**: Central WebSocket hub for agent connections
5. **Factory Pattern**: TLS provider creation, GPU detector creation

### Key Backend Features

- **JWT Authentication**: Access/refresh token pattern
- **Multi-Factor Authentication**: TOTP, email, backup codes
- **Role-Based Access Control**: user, admin, agent, system roles
- **Job Scheduling**: Dynamic task distribution with chunking
- **File Synchronization**: Agent-backend file sync
- **Monitoring**: System metrics and heartbeat management
- **Data Retention**: Configurable retention policies
- **Accurate Keyspace Tracking**: Captures real keyspace from hashcat `progress[1]` values for precise progress reporting

## Frontend Architecture

### Component Structure

The frontend uses React with TypeScript and follows a component-based architecture:

#### 1. **Pages** (`src/pages/`)
- Top-level route components
- Page-specific business logic
- Component composition

**Key Pages:**
- `Dashboard` - System overview
- `AgentManagement` - Agent monitoring
- `Jobs/` - Job execution interface
- `AdminSettings/` - System configuration
- `Login` - Authentication

#### 2. **Components** (`src/components/`)
- Reusable UI components
- Domain-specific components
- Common UI patterns

**Component Categories:**
- `admin/` - Administrative UI components
- `agent/` - Agent-related components
- `auth/` - Authentication components
- `common/` - Shared components
- `hashlist/` - Hashlist management UI

#### 3. **Services** (`src/services/`)
- API communication layer
- HTTP request handling
- Response transformation

**Key Services:**
- `api.ts` - Base API configuration
- `auth.ts` - Authentication API
- `jobSettings.ts` - Job configuration
- `systemSettings.ts` - System settings

#### 4. **State Management**
- **React Context**: Authentication state (`AuthContext`)
- **React Query**: Server state management with caching
- **Local State**: Component-specific state with hooks

#### 5. **Type System** (`src/types/`)
- TypeScript interfaces and types
- API response types
- Domain models

### Frontend Technologies

- **React 18**: Component framework
- **TypeScript**: Type safety
- **Material-UI**: Component library
- **React Query**: Data fetching and caching
- **React Router**: Client-side routing
- **Axios**: HTTP client

## Agent Architecture

### Core Modules

#### 1. **Agent Core** (`internal/agent/`)
- WebSocket connection management
- Registration with claim codes
- Heartbeat maintenance
- Message routing

#### 2. **Hardware Detection** (`internal/hardware/`)
- GPU detection (NVIDIA, AMD, Intel)
- System resource monitoring
- Hashcat availability checking
- Device capability reporting

**GPU Detectors:**
- `gpu/nvidia.go` - NVIDIA GPU detection
- `gpu/amd.go` - AMD GPU detection
- `gpu/intel.go` - Intel GPU detection
- `gpu/detector.go` - Detection orchestration

#### 3. **Job Execution** (`internal/jobs/`)
- Hashcat process management
- Job progress tracking
- Output parsing
- Error handling

#### 4. **File Synchronization** (`internal/sync/`)
- Binary synchronization
- Wordlist management
- Rule file handling
- Hashlist retrieval

#### 5. **Metrics Collection** (`internal/metrics/`)
- System resource monitoring
- GPU utilization tracking
- Performance metrics reporting

### Agent Lifecycle

1. **Registration Phase**
   - Claim code validation
   - API key generation
   - Certificate exchange
   - Initial synchronization

2. **Active Phase**
   - Heartbeat maintenance
   - Job reception and execution
   - Progress reporting
   - File synchronization

3. **Execution Phase**
   - Task assignment reception
   - Hashcat process spawning
   - Progress monitoring
   - Result reporting

## Communication Protocols

### REST API

The system uses RESTful APIs for standard CRUD operations:

**Endpoint Structure:**
```
/api/v1/auth/*         - Authentication endpoints
/api/v1/admin/*        - Administrative functions
/api/v1/agents/*       - Agent management
/api/v1/hashlists/*    - Hashlist operations
/api/v1/jobs/*         - Job management
/api/v1/wordlists/*    - Wordlist management
/api/v1/rules/*        - Rule file management
```

**Authentication:**
- JWT Bearer tokens
- API key authentication (agents)
- Refresh token rotation

### WebSocket Protocol

Real-time communication uses WebSocket with JSON message format:

**Message Structure:**
```json
{
  "type": "message_type",
  "payload": { ... },
  "timestamp": "2025-01-01T00:00:00Z"
}
```

**Agent → Server Messages:**
- `heartbeat` - Keep-alive signal
- `task_status` - Task execution status
- `job_progress` - Job progress updates
- `benchmark_result` - Benchmark results
- `hardware_info` - Hardware capabilities
- `hashcat_output` - Hashcat output streams
- `device_update` - Device status changes

**Server → Agent Messages:**
- `task_assignment` - New task assignment
- `job_stop` - Stop job execution
- `benchmark_request` - Request benchmark
- `config_update` - Configuration changes
- `file_sync_request` - File sync command
- `force_cleanup` - Force cleanup command

### File Transfer Protocol

File synchronization uses HTTP(S) with the following endpoints:

- `GET /api/v1/sync/binaries/:name` - Download binaries
- `GET /api/v1/sync/wordlists/:id` - Download wordlists
- `GET /api/v1/sync/rules/:id` - Download rules
- `GET /api/v1/sync/hashlists/:id` - Download hashlists

## Database Schema

### Core Tables

#### User Management
- `users` - User accounts with roles and preferences
- `auth_tokens` - JWT refresh tokens
- `mfa_methods` - Multi-factor authentication settings
- `mfa_backup_codes` - MFA recovery codes

#### Agent Management
- `agents` - Registered compute agents
- `agent_devices` - GPU/compute devices per agent
- `agent_schedules` - Agent availability schedules
- `agent_hashlists` - Agent-hashlist assignments

#### Job Management
- `job_workflows` - Attack strategy definitions
- `preset_jobs` - Predefined job templates
- `job_executions` - Active job instances
- `job_tasks` - Individual task assignments
- `performance_metrics` - Task performance data

#### Data Management
- `hashlists` - Password hash collections
- `hashes` - Individual password hashes
- `clients` - Customer/engagement tracking
- `wordlists` - Dictionary files
- `rules` - Rule files for mutations

#### System Management
- `vouchers` - Agent registration codes
- `binary_versions` - Hashcat binary versions
- `system_settings` - Global configuration
- `client_settings` - Per-client settings

### Key Relationships

```
users ─────────┬──── agents (owner_id)
               ├──── hashlists (created_by)
               └──── job_executions (created_by)

agents ────────┬──── agent_devices
               ├──── agent_schedules
               └──── job_tasks

hashlists ─────┬──── hashes
               ├──── job_executions
               └──── clients

job_workflows ──┬─── preset_jobs
                └─── job_executions ──── job_tasks
```

## Security Architecture

### Authentication & Authorization

#### Multi-Layer Authentication
1. **User Authentication**
   - Username/password with bcrypt hashing
   - JWT access/refresh token pattern
   - Session management with token rotation
   - Session-token binding with CASCADE delete for security

2. **Multi-Factor Authentication**
   - TOTP (Time-based One-Time Passwords)
   - Email-based verification
   - Backup codes for recovery
   - Configurable MFA policies

3. **Agent Authentication**
   - Claim code registration
   - API key authentication
   - Certificate-based trust

#### Session Security Architecture

KrakenHashes implements a database-backed session-token relationship to ensure true session termination:

- **Foreign Key Binding**: `active_sessions.token_id` → `tokens.id` with ON DELETE CASCADE
- **Atomic Revocation**: Deleting a token automatically removes associated sessions
- **True Logout**: Session termination immediately invalidates JWT authentication
- **Security Enforcement**: Auth middleware validates both token existence and session validity
- **No Orphaned Tokens**: Database constraints prevent tokens from persisting after logout

This architecture prevents a critical vulnerability where session termination would only remove UI state but leave JWT tokens active in the database.

#### Role-Based Access Control (RBAC)

**Roles:**
- `user` - Standard user access
- `admin` - Administrative privileges
- `agent` - Agent-specific operations
- `system` - System-level operations

**Middleware Chain:**
```go
AuthMiddleware → RoleMiddleware → ResourceMiddleware → Handler
```

### Transport Security

#### TLS/SSL Configuration

**Supported Modes:**
1. **Self-Signed Certificates**
   - Automatic generation with CA
   - Configurable validity periods
   - SAN extension support

2. **Provided Certificates**
   - Custom certificate installation
   - Certificate chain validation

3. **Let's Encrypt (Certbot)**
   - Automatic certificate renewal
   - ACME protocol support

**Certificate Features:**
- RSA 2048/4096 bit keys
- Multiple DNS names and IP addresses
- Proper certificate chain delivery
- Browser-compatible extensions

### Data Security

1. **Password Storage**
   - bcrypt with configurable cost factor
   - No plaintext storage

2. **Token Security**
   - Short-lived access tokens (15 minutes)
   - Refresh token rotation
   - Secure token storage

3. **File Access Control**
   - Path sanitization
   - Directory restrictions
   - User-based access control

4. **API Security**
   - Rate limiting
   - Request validation
   - CORS configuration

## File Storage Architecture

### Directory Structure

```
/data/krakenhashes/
├── binaries/         # Hashcat binaries
│   ├── hashcat-linux-x64/
│   ├── hashcat-windows-x64/
│   └── hashcat-darwin-x64/
├── wordlists/        # Dictionary files
│   ├── general/      # Common wordlists
│   ├── specialized/  # Domain-specific
│   ├── targeted/     # Custom lists
│   └── custom/       # User uploads
├── rules/            # Mutation rules
│   ├── hashcat/      # Hashcat rules
│   ├── john/         # John rules
│   └── custom/       # Custom rules
└── hashlists/        # Hash files
    └── {client_id}/  # Per-client storage
```

### Storage Management

- **Upload Processing**: Files are uploaded to temporary storage, processed, then moved to permanent locations
- **Deduplication**: Files are tracked by MD5 hash to prevent duplicates
- **Synchronization**: Agent sync service ensures agents have required files
- **Cleanup**: Automated retention policies remove expired data

## Data Lifecycle Management

### Retention System

The system implements comprehensive data lifecycle management with automated retention policies:

#### Backend Retention Service
- **Automatic Purging**: Runs daily at midnight and on startup
- **Client-Specific Policies**: Each client can have custom retention periods
- **Secure Deletion Process**:
  1. Transaction-based database cleanup
  2. Secure file overwriting with random data
  3. PostgreSQL VACUUM to prevent recovery
  4. Comprehensive audit logging

#### Agent Cleanup Service
- **3-Day Retention**: Temporary files removed after 3 days
- **Automatic Cleanup**: Runs every 6 hours
- **File Types Managed**:
  - Hashlist files (after inactivity)
  - Rule chunks (temporary segments)
  - Chunk ID tracking files
- **Preserved Files**: Base rules, wordlists, and binaries

#### Potfile Exclusion
!!! warning "Important"
    The potfile (`/var/lib/krakenhashes/wordlists/custom/potfile.txt`) containing plaintext passwords is **NOT** managed by the retention system. It requires separate manual management for compliance with data protection regulations.

### Data Security

#### Secure Deletion
- Files overwritten with random data before removal
- VACUUM ANALYZE on PostgreSQL tables
- Prevention of WAL (Write-Ahead Log) recovery
- Transaction safety for atomic operations

#### Audit Trail
- All deletion operations logged
- Retention compliance tracking
- Last purge timestamp recording

1. **File Organization**
   - Client-based isolation
   - Category-based grouping
   - Version tracking

2. **Synchronization**
   - Delta-based updates
   - Checksum verification
   - Compression support

3. **Retention Policies**
   - Configurable retention periods
   - Automatic cleanup
   - Archive support

## Deployment Architecture

### Docker-Based Deployment

```yaml
Services:
- backend    # Go backend server
- postgres   # PostgreSQL database
- app        # Nginx + React frontend

Networks:
- krakenhashes_default # Internal network

Volumes:
- postgres_data     # Database persistence
- kh_config        # Configuration files
- kh_data          # Application data
- kh_logs          # Log files
```

### Production Considerations

1. **Scalability**
   - Horizontal agent scaling
   - Database connection pooling
   - Load balancer ready

2. **Monitoring**
   - Health check endpoints
   - Metrics collection
   - Log aggregation

3. **Backup & Recovery**
   - Database backups
   - File system snapshots
   - Configuration backup

4. **High Availability**
   - Database replication support
   - Stateless backend design
   - Agent failover handling

### Environment Configuration

**Key Environment Variables:**
```bash
# Database
DB_HOST, DB_PORT, DB_USER, DB_PASSWORD, DB_NAME

# Security
JWT_SECRET, JWT_REFRESH_SECRET

# TLS/SSL
KH_TLS_MODE, KH_CERT_KEY_SIZE

# Directories
KH_CONFIG_DIR, KH_DATA_DIR

# Ports
KH_HTTP_PORT, KH_HTTPS_PORT
```

## Conclusion

KrakenHashes implements a robust distributed architecture designed for scalability, security, and maintainability. The system's modular design allows for independent scaling of components while maintaining clear separation of concerns throughout the stack.