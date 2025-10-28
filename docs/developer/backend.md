# Backend Development Guide

This guide covers the KrakenHashes backend development, including environment setup, architecture, coding patterns, and common development tasks.

## Table of Contents

1. [Development Environment Setup](#development-environment-setup)
2. [Code Structure and Architecture](#code-structure-and-architecture)
3. [Core Conventions and Patterns](#core-conventions-and-patterns)
4. [Adding New Endpoints](#adding-new-endpoints)
5. [Database Operations](#database-operations)
6. [Authentication and Authorization](#authentication-and-authorization)
7. [WebSocket Development](#websocket-development)
8. [Testing Strategies](#testing-strategies)
9. [Common Patterns and Utilities](#common-patterns-and-utilities)
10. [Debugging and Logging](#debugging-and-logging)

## Development Environment Setup

### Prerequisites

- Docker and Docker Compose (primary development method)
- Go 1.21+ (for IDE support and running tests locally)
- PostgreSQL client tools (optional, for database inspection)
- Make (for running build commands)

### Initial Setup

1. **Clone the repository**
   ```bash
   git clone <repository-url>
   cd krakenhashes
   ```

2. **Set up environment variables**
   ```bash
   # Copy the example environment file
   cp .env.example .env
   
   # Edit .env with your configuration
   # Required variables:
   DB_HOST=postgres
   DB_PORT=5432
   DB_USER=krakenhashes
   DB_PASSWORD=your-secure-password
   DB_NAME=krakenhashes
   JWT_SECRET=your-jwt-secret
   KH_TLS_MODE=self-signed
   ```

3. **Start the development environment**
   ```bash
   # Build and start all services
   docker-compose down && docker-compose up -d --build
   
   # View logs
   docker-compose logs -f backend
   ```

4. **Verify the setup**
   ```bash
   # Check backend health
   curl -k https://localhost:8443/api/status
   
   # Check database migrations
   docker-compose exec backend ls -la /app/db/migrations
   ```

### Docker Development Workflow

**Important**: Always use Docker for building and testing. Never use `go build` directly as it creates binaries in the project directory.

```bash
# Rebuild backend only
docker-compose up -d --build backend

# Run database migrations
cd backend && make migrate-up

# View structured logs
docker-compose logs backend | grep -E "ERROR|WARNING|INFO"

# Access backend container
docker-compose exec backend sh
```

## Code Structure and Architecture

The backend follows a layered architecture with clear separation of concerns:

```
backend/
├── cmd/
│   ├── server/          # Main application entry point
│   └── migrate/         # Database migration tool
├── internal/            # Private application code
│   ├── config/          # Configuration management
│   ├── db/              # Database wrapper and utilities
│   ├── handlers/        # HTTP request handlers (controllers)
│   ├── middleware/      # HTTP middleware
│   ├── models/          # Domain models and types
│   ├── repository/      # Data access layer
│   ├── services/        # Business logic layer
│   ├── websocket/       # WebSocket handlers
│   └── routes/          # Route configuration
├── pkg/                 # Public packages
│   ├── debug/           # Debug logging utilities
│   ├── jwt/             # JWT token handling
│   └── httputil/        # HTTP utilities
└── db/
    └── migrations/      # SQL migration files
```

### Key Architecture Patterns

1. **Repository Pattern**: All database access through repositories
2. **Service Layer**: Business logic separated from handlers
3. **Dependency Injection**: Dependencies passed through constructors
4. **Middleware Chain**: Composable middleware for cross-cutting concerns
5. **Context Propagation**: Request context flows through all layers

## Core Conventions and Patterns

### Database Access Pattern

The backend uses a custom DB wrapper instead of sqlx directly:

```go
// internal/db/db.go
type DB struct {
    *sql.DB
}

// Repository pattern
type UserRepository struct {
    db *db.DB
}

func NewUserRepository(db *db.DB) *UserRepository {
    return &UserRepository{db: db}
}

// Use standard database/sql methods
func (r *UserRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
    user := &models.User{}
    err := r.db.QueryRowContext(ctx, queries.GetUserByID, id).Scan(
        &user.ID,
        &user.Username,
        // ... other fields
    )
    if err == sql.ErrNoRows {
        return nil, fmt.Errorf("user not found: %s", id)
    }
    return user, err
}
```

### Service Layer Pattern

Services contain business logic and orchestrate multiple repositories:

```go
// internal/services/client/client_service.go
type ClientService struct {
    clientRepo         *repository.ClientRepository
    hashlistRepo       *repository.HashListRepository
    clientSettingsRepo *repository.ClientSettingsRepository
    retentionService   *retention.RetentionService
}

func (s *ClientService) DeleteClient(ctx context.Context, clientID uuid.UUID) error {
    // Begin transaction
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("failed to start transaction: %w", err)
    }
    defer tx.Rollback()
    
    // Business logic here...
    
    return tx.Commit()
}
```

### Error Handling

Use wrapped errors for better error tracking:

```go
// Wrap errors with context
if err != nil {
    return fmt.Errorf("failed to create user: %w", err)
}

// Custom error types
var (
    ErrNotFound = errors.New("resource not found")
    ErrUnauthorized = errors.New("unauthorized")
)

// Check error types
if errors.Is(err, repository.ErrNotFound) {
    http.Error(w, "Not found", http.StatusNotFound)
    return
}
```

## Adding New Endpoints

### Step 1: Define the Model

```go
// internal/models/example.go
package models

import (
    "time"
    "github.com/google/uuid"
)

type Example struct {
    ID          uuid.UUID  `json:"id"`
    Name        string     `json:"name"`
    Description string     `json:"description"`
    CreatedAt   time.Time  `json:"created_at"`
    UpdatedAt   time.Time  `json:"updated_at"`
}
```

**Important Pattern - Nullable String Fields**:

For fields that may be null (e.g., passwords in hash records), use `*string` instead of `string`:

```go
// internal/models/hashlist.go
type Hash struct {
    ID           uuid.UUID  `json:"id"`
    OriginalHash string     `json:"original_hash"`
    Username     *string    `json:"username,omitempty"`    // Nullable
    Domain       *string    `json:"domain,omitempty"`      // Nullable
    HashTypeID   int        `json:"hash_type_id"`
    IsCracked    bool       `json:"is_cracked"`
    Password     *string    `json:"password,omitempty"`    // Nullable - only present when cracked
    LastUpdated  time.Time  `json:"last_updated"`
}
```

**Why Use Pointers for Nullable Fields**:
- Distinguishes between empty string (`""`) and no value (`nil`)
- Matches SQL NULL semantics in the database
- Prevents invalid empty string values in uncracked hashes
- JSON marshaling with `omitempty` excludes `nil` fields from output

**When to Use `*string`**:
- Optional fields that may not be set (username, domain)
- Fields only present under certain conditions (password only when cracked)
- Fields that should be omitted from JSON when null

**When to Use `string`**:
- Required fields that always have a value
- Fields where empty string is a valid value
- Primary identifiers or keys

### Step 2: Create the Repository

```go
// internal/repository/example_repository.go
package repository

type ExampleRepository struct {
    db *db.DB
}

func NewExampleRepository(db *db.DB) *ExampleRepository {
    return &ExampleRepository{db: db}
}

func (r *ExampleRepository) Create(ctx context.Context, example *models.Example) error {
    query := `
        INSERT INTO examples (id, name, description, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5)
    `
    _, err := r.db.ExecContext(ctx, query,
        example.ID,
        example.Name,
        example.Description,
        example.CreatedAt,
        example.UpdatedAt,
    )
    return err
}

func (r *ExampleRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Example, error) {
    example := &models.Example{}
    query := `SELECT id, name, description, created_at, updated_at FROM examples WHERE id = $1`
    
    err := r.db.QueryRowContext(ctx, query, id).Scan(
        &example.ID,
        &example.Name,
        &example.Description,
        &example.CreatedAt,
        &example.UpdatedAt,
    )
    
    if err == sql.ErrNoRows {
        return nil, ErrNotFound
    }
    
    return example, err
}
```

### Step 3: Create the Service (if needed)

```go
// internal/services/example_service.go
package services

type ExampleService struct {
    repo *repository.ExampleRepository
}

func NewExampleService(repo *repository.ExampleRepository) *ExampleService {
    return &ExampleService{repo: repo}
}

func (s *ExampleService) CreateExample(ctx context.Context, name, description string) (*models.Example, error) {
    example := &models.Example{
        ID:          uuid.New(),
        Name:        name,
        Description: description,
        CreatedAt:   time.Now(),
        UpdatedAt:   time.Now(),
    }
    
    if err := s.repo.Create(ctx, example); err != nil {
        return nil, fmt.Errorf("failed to create example: %w", err)
    }
    
    return example, nil
}
```

### Step 4: Create the Handler

```go
// internal/handlers/example/handler.go
package example

type Handler struct {
    service *services.ExampleService
}

func NewHandler(service *services.ExampleService) *Handler {
    return &Handler{service: service}
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Name        string `json:"name"`
        Description string `json:"description"`
    }
    
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request", http.StatusBadRequest)
        return
    }
    
    // Get user ID from context (set by auth middleware)
    userID := r.Context().Value("user_id").(uuid.UUID)
    
    example, err := h.service.CreateExample(r.Context(), req.Name, req.Description)
    if err != nil {
        debug.Error("Failed to create example: %v", err)
        http.Error(w, "Internal server error", http.StatusInternalServerError)
        return
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(example)
}

func (h *Handler) GetByID(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    id, err := uuid.Parse(vars["id"])
    if err != nil {
        http.Error(w, "Invalid ID", http.StatusBadRequest)
        return
    }
    
    example, err := h.service.repo.GetByID(r.Context(), id)
    if err != nil {
        if errors.Is(err, repository.ErrNotFound) {
            http.Error(w, "Not found", http.StatusNotFound)
            return
        }
        debug.Error("Failed to get example: %v", err)
        http.Error(w, "Internal server error", http.StatusInternalServerError)
        return
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(example)
}
```

### Step 5: Register Routes

```go
// internal/routes/routes.go
// In SetupRoutes function:

// Initialize repository and service
exampleRepo := repository.NewExampleRepository(database)
exampleService := services.NewExampleService(exampleRepo)
exampleHandler := example.NewHandler(exampleService)

// Register routes with authentication
jwtRouter.HandleFunc("/examples", exampleHandler.Create).Methods("POST")
jwtRouter.HandleFunc("/examples/{id}", exampleHandler.GetByID).Methods("GET")
```

## Database Operations

### Creating Migrations

```bash
# Create a new migration
make migration name=add_example_table

# This creates two files:
# - db/migrations/XXXXXX_add_example_table.up.sql
# - db/migrations/XXXXXX_add_example_table.down.sql
```

Example migration:

```sql
-- XXXXXX_add_example_table.up.sql
CREATE TABLE IF NOT EXISTS examples (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX idx_examples_created_by ON examples(created_by);

-- Add trigger for updated_at
CREATE TRIGGER update_examples_updated_at BEFORE UPDATE ON examples
FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- XXXXXX_add_example_table.down.sql
DROP TRIGGER IF EXISTS update_examples_updated_at ON examples;
DROP TABLE IF EXISTS examples;
```

### Transaction Management

```go
// Use transactions for complex operations
func (s *Service) ComplexOperation(ctx context.Context) error {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("failed to begin transaction: %w", err)
    }
    defer func() {
        if err != nil {
            if rbErr := tx.Rollback(); rbErr != nil {
                debug.Error("Failed to rollback: %v", rbErr)
            }
        }
    }()
    
    // Perform operations using tx
    if err = s.repo.CreateWithTx(tx, data); err != nil {
        return err
    }
    
    if err = s.repo.UpdateWithTx(tx, id, updates); err != nil {
        return err
    }
    
    return tx.Commit()
}
```

### Query Patterns

```go
// Parameterized queries (always use placeholders)
query := `
    SELECT h.id, h.hash_value, h.is_cracked, h.plain_text
    FROM hashes h
    WHERE h.hashlist_id = $1
    AND h.created_at > $2
    ORDER BY h.created_at DESC
    LIMIT $3
`

rows, err := db.QueryContext(ctx, query, hashlistID, since, limit)
if err != nil {
    return nil, fmt.Errorf("failed to query hashes: %w", err)
}
defer rows.Close()

var hashes []models.Hash
for rows.Next() {
    var hash models.Hash
    err := rows.Scan(&hash.ID, &hash.HashValue, &hash.IsCracked, &hash.PlainText)
    if err != nil {
        return nil, fmt.Errorf("failed to scan hash: %w", err)
    }
    hashes = append(hashes, hash)
}

if err = rows.Err(); err != nil {
    return nil, fmt.Errorf("error iterating hash rows: %w", err)
}
```

## Authentication and Authorization

### JWT Authentication Flow

1. **Login**: User provides credentials → Validate → Generate JWT → Set cookie
2. **Request**: Extract token from cookie → Validate JWT → Check database → Add to context
3. **Logout**: Remove token from database → Clear cookie

### Middleware Stack

```go
// internal/middleware/auth.go
func RequireAuth(database *db.DB) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Skip for OPTIONS requests
            if r.Method == "OPTIONS" {
                next.ServeHTTP(w, r)
                return
            }
            
            // Get token from cookie
            cookie, err := r.Cookie("token")
            if err != nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
            }
            
            // Validate token
            userID, err := jwt.ValidateJWT(cookie.Value)
            if err != nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
            }
            
            // Verify token exists in database
            exists, err := database.TokenExists(cookie.Value)
            if !exists {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
            }
            
            // Add to context
            ctx := context.WithValue(r.Context(), "user_id", userID)
            ctx = context.WithValue(ctx, "user_role", role)
            r = r.WithContext(ctx)
            
            next.ServeHTTP(w, r)
        })
    }
}
```

### Role-Based Access Control

```go
// internal/middleware/admin.go
func RequireAdmin() func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            role := r.Context().Value("user_role").(string)
            
            if role != "admin" {
                http.Error(w, "Forbidden", http.StatusForbidden)
                return
            }
            
            next.ServeHTTP(w, r)
        })
    }
}

// Usage in routes
adminRouter := jwtRouter.PathPrefix("/admin").Subrouter()
adminRouter.Use(middleware.RequireAdmin())
```

### API Key Authentication (Agents)

```go
// internal/handlers/auth/api/middleware.go
func RequireAPIKey(agentService *services.AgentService) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            apiKey := r.Header.Get("X-API-Key")
            agentIDStr := r.Header.Get("X-Agent-ID")
            
            if apiKey == "" || agentIDStr == "" {
                http.Error(w, "API Key and Agent ID required", http.StatusUnauthorized)
                return
            }
            
            agent, err := agentService.GetByAPIKey(r.Context(), apiKey)
            if err != nil {
                http.Error(w, "Invalid API Key", http.StatusUnauthorized)
                return
            }
            
            ctx := context.WithValue(r.Context(), "agent_id", agent.ID)
            r = r.WithContext(ctx)
            
            next.ServeHTTP(w, r)
        })
    }
}
```

## WebSocket Development

### WebSocket Handler Pattern

```go
// internal/websocket/agent_updates.go
type AgentUpdateHandler struct {
    db           *db.DB
    agentService *services.AgentService
    upgrader     websocket.Upgrader
}

func (h *AgentUpdateHandler) HandleUpdates(w http.ResponseWriter, r *http.Request) {
    // Authenticate before upgrading
    apiKey := r.Header.Get("X-API-Key")
    agent, err := h.agentService.GetByAPIKey(r.Context(), apiKey)
    if err != nil {
        http.Error(w, "Invalid API Key", http.StatusUnauthorized)
        return
    }
    
    // Upgrade connection
    conn, err := h.upgrader.Upgrade(w, r, nil)
    if err != nil {
        debug.Error("Failed to upgrade connection: %v", err)
        return
    }
    defer conn.Close()
    
    // Configure connection
    conn.SetReadLimit(maxMessageSize)
    conn.SetReadDeadline(time.Now().Add(pongWait))
    conn.SetPongHandler(func(string) error {
        conn.SetReadDeadline(time.Now().Add(pongWait))
        return nil
    })
    
    // Start ping ticker
    ticker := time.NewTicker(pingPeriod)
    defer ticker.Stop()
    
    // Message handling loop
    for {
        messageType, message, err := conn.ReadMessage()
        if err != nil {
            if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
                debug.Error("WebSocket error: %v", err)
            }
            break
        }
        
        // Process message
        if err := h.processMessage(agent.ID, message); err != nil {
            debug.Error("Failed to process message: %v", err)
        }
    }
}
```

### Message Processing with Transactions

```go
func (h *AgentUpdateHandler) processCrackUpdate(ctx context.Context, agentID int, msg CrackUpdateMessage) error {
    tx, err := h.db.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("failed to start transaction: %w", err)
    }
    defer func() {
        if err != nil {
            tx.Rollback()
        }
    }()
    
    // Update hash status
    err = h.hashRepo.UpdateCrackStatus(tx, msg.HashID, msg.Password)
    if err != nil {
        return err
    }
    
    // Update hashlist count
    err = h.hashlistRepo.IncrementCrackedCountTx(tx, msg.HashlistID, 1)
    if err != nil {
        return err
    }
    
    return tx.Commit()
}
```

## Testing Strategies

### Unit Testing

```go
// internal/handlers/auth/handler_test.go
func TestLoginHandler(t *testing.T) {
    // Setup
    testutil.SetTestJWTSecret(t)
    db := testutil.SetupTestDB(t)
    emailService := testutil.NewMockEmailService()
    handler := NewHandler(db, emailService)
    
    // Create test user
    testUser := testutil.CreateTestUser(t, db, "testuser", "test@example.com", "password", "user")
    
    // Test successful login
    t.Run("successful login", func(t *testing.T) {
        body := map[string]string{
            "username": "testuser",
            "password": "password",
        }
        jsonBody, _ := json.Marshal(body)
        
        req := httptest.NewRequest("POST", "/api/login", bytes.NewBuffer(jsonBody))
        rr := httptest.NewRecorder()
        
        handler.Login(rr, req)
        
        assert.Equal(t, http.StatusOK, rr.Code)
        
        var resp models.LoginResponse
        json.Unmarshal(rr.Body.Bytes(), &resp)
        assert.True(t, resp.Success)
        assert.NotEmpty(t, resp.Token)
    })
}
```

### Integration Testing

```go
// internal/integration_test/auth_integration_test.go
func TestAuthenticationFlow(t *testing.T) {
    // Setup test environment
    db := testutil.SetupTestDB(t)
    router := setupTestRouter(db)
    
    // Register user
    registerResp := testutil.RegisterUser(t, router, "testuser", "test@example.com", "password")
    assert.Equal(t, http.StatusOK, registerResp.Code)
    
    // Login
    loginResp := testutil.Login(t, router, "testuser", "password")
    assert.Equal(t, http.StatusOK, loginResp.Code)
    
    // Extract token
    token := testutil.ExtractTokenFromResponse(t, loginResp)
    
    // Access protected endpoint
    req := httptest.NewRequest("GET", "/api/dashboard", nil)
    req.AddCookie(&http.Cookie{Name: "token", Value: token})
    rr := httptest.NewRecorder()
    
    router.ServeHTTP(rr, req)
    assert.Equal(t, http.StatusOK, rr.Code)
}
```

### Mock Services

```go
// internal/testutil/mocks.go
type MockEmailService struct {
    SentEmails []SentEmail
}

func (m *MockEmailService) SendMFACode(ctx context.Context, email, code string) error {
    m.SentEmails = append(m.SentEmails, SentEmail{
        To:      email,
        Subject: "MFA Code",
        Body:    code,
    })
    return nil
}
```

### Database Testing

```go
// internal/testutil/db.go
func SetupTestDB(t *testing.T) *db.DB {
    // Connect to test database
    testDB := os.Getenv("TEST_DATABASE_URL")
    if testDB == "" {
        testDB = "postgres://test:test@localhost/krakenhashes_test"
    }
    
    sqlDB, err := sql.Open("postgres", testDB)
    require.NoError(t, err)
    
    // Run migrations
    err = database.RunMigrations()
    require.NoError(t, err)
    
    // Clean up after test
    t.Cleanup(func() {
        // Truncate all tables
        tables := []string{"users", "agents", "hashlists", "hashes"}
        for _, table := range tables {
            sqlDB.Exec(fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table))
        }
        sqlDB.Close()
    })
    
    return &db.DB{DB: sqlDB}
}
```

## Common Patterns and Utilities

### Debug Logging

```go
// Use the debug package for structured logging
import "github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"

// Log levels
debug.Debug("Processing request for user: %s", userID)
debug.Info("Server starting on port %d", port)
debug.Warning("Rate limit approaching for user: %s", userID)
debug.Error("Failed to connect to database: %v", err)

// Conditional debug logging
if debug.IsDebugEnabled() {
    debug.Debug("Detailed request info: %+v", req)
}
```

### HTTP Utilities

```go
// internal/pkg/httputil/httputil.go
func WriteJSON(w http.ResponseWriter, status int, data interface{}) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    if err := json.NewEncoder(w).Encode(data); err != nil {
        debug.Error("Failed to encode JSON response: %v", err)
    }
}

func ReadJSON(r *http.Request, dest interface{}) error {
    if r.Header.Get("Content-Type") != "application/json" {
        return errors.New("content-type must be application/json")
    }
    
    decoder := json.NewDecoder(r.Body)
    decoder.DisallowUnknownFields()
    return decoder.Decode(dest)
}
```

### Context Values

```go
// pkg/jwt/context.go
type contextKey string

const (
    userIDKey   contextKey = "user_id"
    userRoleKey contextKey = "user_role"
    agentIDKey  contextKey = "agent_id"
)

func GetUserID(ctx context.Context) (uuid.UUID, bool) {
    id, ok := ctx.Value(userIDKey).(uuid.UUID)
    return id, ok
}

func GetUserRole(ctx context.Context) (string, bool) {
    role, ok := ctx.Value(userRoleKey).(string)
    return role, ok
}
```

### File Operations

```go
// Use the centralized data directory
func SaveUploadedFile(file multipart.File, filename string) error {
    dataDir := config.GetDataDir()
    destPath := filepath.Join(dataDir, "uploads", filename)
    
    // Ensure directory exists
    if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
        return fmt.Errorf("failed to create directory: %w", err)
    }
    
    // Create destination file
    dest, err := os.Create(destPath)
    if err != nil {
        return fmt.Errorf("failed to create file: %w", err)
    }
    defer dest.Close()
    
    // Copy content
    if _, err := io.Copy(dest, file); err != nil {
        return fmt.Errorf("failed to save file: %w", err)
    }
    
    return nil
}
```

### Validation Helpers

```go
// Validate request data
func ValidateCreateUserRequest(req *CreateUserRequest) error {
    if req.Username == "" {
        return errors.New("username is required")
    }

    if len(req.Username) < 3 || len(req.Username) > 50 {
        return errors.New("username must be between 3 and 50 characters")
    }

    if !emailRegex.MatchString(req.Email) {
        return errors.New("invalid email format")
    }

    if err := password.Validate(req.Password); err != nil {
        return fmt.Errorf("invalid password: %w", err)
    }

    return nil
}
```

### Working with Nullable Fields

When working with `*string` and other pointer fields, always check for `nil` before dereferencing:

```go
// Bad - will panic if password is nil
func processHash(hash *models.Hash) string {
    return *hash.Password // PANIC if hash not cracked!
}

// Good - safe nil checking
func processHash(hash *models.Hash) string {
    if hash.Password == nil {
        return "" // or handle appropriately
    }
    return *hash.Password
}

// Best - guard clause pattern
func processHash(hash *models.Hash) string {
    if hash.Password == nil {
        return ""
    }

    password := *hash.Password
    // ... process password
    return password
}
```

**Common Patterns**:

```go
// Analytics service - skip nil passwords
for _, pwd := range passwords {
    if pwd.Password == nil {
        continue // Skip entries without passwords
    }
    length := len([]rune(*pwd.Password))
    // ... process
}

// Setting nullable fields
hash := &models.Hash{
    OriginalHash: "5F4DCC3B...",
    IsCracked:    true,
}
plaintext := "password123"
hash.Password = &plaintext  // Set pointer to value

// Or inline
hash.Password = stringPtr("password123")

// Helper function for creating string pointers
func stringPtr(s string) *string {
    return &s
}
```

**Database Scanning**:

Use `sql.NullString` when scanning, then convert to `*string`:

```go
var password sql.NullString
err := row.Scan(&hash.ID, &hash.OriginalHash, &password)

if password.Valid {
    hash.Password = &password.String
} else {
    hash.Password = nil
}
```

**JSON Handling**:

The `omitempty` tag automatically excludes `nil` pointer fields:

```go
type Hash struct {
    Password *string `json:"password,omitempty"`
}

// When marshaling:
uncracked := Hash{Password: nil}
json.Marshal(uncracked) // {"id": "...", "original_hash": "..."}
// password field omitted

cracked := Hash{Password: stringPtr("test123")}
json.Marshal(cracked) // {"id": "...", "original_hash": "...", "password": "test123"}
// password field included
```

## Debugging and Logging

### Environment Variables for Debugging

```bash
# Enable debug logging
KH_DEBUG=true

# Set log level (DEBUG, INFO, WARNING, ERROR)
KH_LOG_LEVEL=DEBUG

# Enable SQL query logging
KH_LOG_SQL=true
```

### Debugging Database Queries

```go
// Log SQL queries in development
if debug.IsDebugEnabled() {
    debug.Debug("Executing query: %s with args: %v", query, args)
}

// Time query execution
start := time.Now()
rows, err := db.QueryContext(ctx, query, args...)
debug.Debug("Query executed in %v", time.Since(start))
```

### Request/Response Logging Middleware

```go
func loggingMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        
        // Wrap response writer to capture status
        wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
        
        // Log request
        debug.Info("[%s] %s %s", r.Method, r.URL.Path, r.RemoteAddr)
        
        next.ServeHTTP(wrapped, r)
        
        // Log response
        duration := time.Since(start)
        debug.Info("[%s] %s %s - %d (%v)", 
            r.Method, r.URL.Path, r.RemoteAddr, 
            wrapped.statusCode, duration)
    })
}
```

### Common Debugging Commands

```bash
# View backend logs with context
docker-compose logs backend | grep -A 5 -B 5 "ERROR"

# Monitor real-time logs
docker-compose logs -f backend | grep -E "user_id|agent_id"

# Check database state
docker-compose exec postgres psql -U krakenhashes -d krakenhashes \
  -c "SELECT * FROM users WHERE created_at > NOW() - INTERVAL '1 hour';"

# Test endpoint with curl
curl -k -X POST https://localhost:8443/api/login \
  -H "Content-Type: application/json" \
  -d '{"username":"test","password":"test"}' \
  -c cookies.txt -v

# Use saved cookies for authenticated requests
curl -k https://localhost:8443/api/dashboard \
  -b cookies.txt -v
```

## Best Practices

1. **Always use context**: Pass context through all function calls for cancellation and timeouts
2. **Handle errors explicitly**: Never ignore errors, always log or return them
3. **Use transactions**: For operations that modify multiple tables
4. **Validate input**: Validate all user input at the handler level
5. **Log appropriately**: Use debug for development, info for important events, error for failures
6. **Test thoroughly**: Write unit tests for business logic, integration tests for workflows
7. **Document APIs**: Add comments to handlers explaining request/response formats
8. **Use prepared statements**: Always use parameterized queries to prevent SQL injection
9. **Close resources**: Always close database rows, files, and connections
10. **Follow Go conventions**: Use gofmt, follow effective Go guidelines

## Troubleshooting

### Common Issues

1. **Database connection errors**
   - Check DATABASE_URL environment variable
   - Ensure PostgreSQL is running
   - Verify network connectivity in Docker

2. **Migration failures**
   - Check migration syntax
   - Ensure migrations are sequential
   - Verify database permissions

3. **Authentication issues**
   - Check JWT_SECRET is set
   - Verify token exists in database
   - Check cookie settings (secure, httpOnly)

4. **WebSocket connection failures**
   - Verify TLS certificates
   - Check CORS settings
   - Ensure proper authentication headers

5. **File upload issues**
   - Check data directory permissions
   - Verify multipart form parsing
   - Check file size limits

### Debug Mode Features

When `KH_DEBUG=true`:
- Detailed SQL query logging
- Request/response body logging
- Performance timing information
- Stack traces on errors
- WebSocket message logging

Remember to disable debug mode in production for security and performance reasons.