# Get version from versions.json
VERSION := $(shell jq -r .backend ../versions.json)
LDFLAGS := -X github.com/ZerkerEOD/krakenhashes/backend/internal/version.Version=$(VERSION)

.PHONY: build
build:
	@echo "Building backend version $(VERSION)..."
	go build -ldflags="$(LDFLAGS)" -o ../bin/server/krakenhashes-server ./cmd/server

.PHONY: run
run: build
	@echo "Running backend version $(VERSION)..."
	../bin/server/krakenhashes-server

.PHONY: clean
clean:
	rm -rf ../bin/server/

.PHONY: test
test:
	go test -v ./...

.PHONY: test-auth
test-auth:
	@echo "Running authentication tests..."
	@JWT_SECRET=test-secret go test -v -count=1 ./pkg/jwt ./pkg/password ./internal/handlers/auth ./internal/integration_test

.PHONY: test-integration
test-integration:
	@echo "Running integration tests..."
	@JWT_SECRET=test-secret go test -v -count=1 ./internal/integration_test

.PHONY: test-unit
test-unit:
	@echo "Running unit tests..."
	@go test -v -count=1 -short ./...

# Creates the test database. SetupTestDB does this automatically too; this
# target exists so the failure is a clear message rather than a test error.
.PHONY: test-db
test-db:
	@echo "Ensuring test database exists..."
	@psql "$${TEST_DATABASE_URL:-postgres://krakenhashes:krakenhashes@localhost:5432/postgres?sslmode=disable}" \
		-tAc "SELECT 1 FROM pg_database WHERE datname='krakenhashes_test'" | grep -q 1 \
		|| psql "postgres://krakenhashes:krakenhashes@localhost:5432/postgres?sslmode=disable" \
			-c "CREATE DATABASE krakenhashes_test"
	@echo "Test database ready."

# DB-backed tests share one database and TRUNCATE globally, so they must run
# serially: -p 1 across packages, and no t.Parallel() within them.
.PHONY: test-cloud
test-cloud:
	@echo "Running cloud provisioning tests..."
	@JWT_SECRET=test-secret KH_ENCRYPTION_KEY=test-encryption-key-32-bytes-ok! \
		go test -race -p 1 -count=1 \
		./internal/repository/... ./internal/services/cloud/... ./internal/handlers/admin/cloud/...

.PHONY: test-coverage
test-coverage:
	@echo "Running tests with coverage..."
	@go test -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

.PHONY: version
version:
	@echo "Backend version: $(VERSION)" 