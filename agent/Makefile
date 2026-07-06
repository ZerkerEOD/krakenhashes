# Makefile for KrakenHashes Agent
# Supports building for Windows, Linux, and macOS (Intel and Apple Silicon)

# Variables
BINARY_NAME = krakenhashes-agent
LAUNCHER_BINARY = krakenhashes-launcher
BUILD_DIR = ../bin
AGENT_BUILD_DIR = $(BUILD_DIR)/agent
LAUNCHER_BUILD_DIR = $(BUILD_DIR)/launcher
GO = go
GOFLAGS = -v
CGO_ENABLED = 0

# Version information
VERSION = $(shell jq -r .agent ../versions.json 2>/dev/null || echo "dev")
LAUNCHER_VERSION = $(shell jq -r '.launcher // .agent' ../versions.json 2>/dev/null || echo "dev")
BUILD_TIME = $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
COMMIT = $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")

# Build flags
LDFLAGS = -ldflags "-s -w \
	-X github.com/ZerkerEOD/krakenhashes/agent/internal/version.Version=$(VERSION) \
	-X main.BuildTime=$(BUILD_TIME) \
	-X main.Commit=$(COMMIT)"

# Launcher build flags (version injected into cmd/launcher main.Version)
LAUNCHER_LDFLAGS = -ldflags "-s -w \
	-X main.Version=$(LAUNCHER_VERSION)"

# Default target
.PHONY: all
all: build

# Build for current platform
.PHONY: build
build:
	@echo "Building $(BINARY_NAME) for current platform..."
	@mkdir -p $(AGENT_BUILD_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(AGENT_BUILD_DIR)/$(BINARY_NAME) ./cmd/agent
	@echo "✅ Build complete: $(AGENT_BUILD_DIR)/$(BINARY_NAME)"

# Build for all platforms (agent + launcher)
.PHONY: build-all
build-all: windows linux darwin darwin-arm64 launchers

# Launcher (auto-updating supervisor) — current platform
.PHONY: launcher
launcher:
	@echo "Building $(LAUNCHER_BINARY) for current platform..."
	@mkdir -p $(LAUNCHER_BUILD_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) $(LAUNCHER_LDFLAGS) -o $(LAUNCHER_BUILD_DIR)/$(LAUNCHER_BINARY) ./cmd/launcher
	@echo "✅ Build complete: $(LAUNCHER_BUILD_DIR)/$(LAUNCHER_BINARY)"

# Launcher — all platforms (mirrors the agent platform matrix)
.PHONY: launchers
launchers:
	@echo "Building $(LAUNCHER_BINARY) for all platforms..."
	@for plat in windows/amd64 windows/386 windows/arm64 linux/amd64 linux/386 linux/arm64 linux/arm darwin/amd64 darwin/arm64; do \
		os=$${plat%/*}; arch=$${plat#*/}; ext=""; \
		if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		mkdir -p $(LAUNCHER_BUILD_DIR)/$$os/$$arch; \
		echo "Building launcher $$os/$$arch..."; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) $(LAUNCHER_LDFLAGS) \
			-o $(LAUNCHER_BUILD_DIR)/$$os/$$arch/$(LAUNCHER_BINARY)$$ext ./cmd/launcher || exit 1; \
	done
	@echo "✅ Launcher builds complete: $(LAUNCHER_BUILD_DIR)/<os>/<arch>/$(LAUNCHER_BINARY)"

# Windows builds
.PHONY: windows
windows: windows-amd64 windows-386 windows-arm64

.PHONY: windows-amd64
windows-amd64:
	@echo "Building for Windows (amd64)..."
	@mkdir -p $(AGENT_BUILD_DIR)/windows/amd64
	GOOS=windows GOARCH=amd64 CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) $(LDFLAGS) \
		-o $(AGENT_BUILD_DIR)/windows/amd64/$(BINARY_NAME).exe ./cmd/agent
	@echo "✅ Built: $(AGENT_BUILD_DIR)/windows/amd64/$(BINARY_NAME).exe"

.PHONY: windows-386
windows-386:
	@echo "Building for Windows (386)..."
	@mkdir -p $(AGENT_BUILD_DIR)/windows/386
	GOOS=windows GOARCH=386 CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) $(LDFLAGS) \
		-o $(AGENT_BUILD_DIR)/windows/386/$(BINARY_NAME).exe ./cmd/agent
	@echo "✅ Built: $(AGENT_BUILD_DIR)/windows/386/$(BINARY_NAME).exe"

.PHONY: windows-arm64
windows-arm64:
	@echo "Building for Windows (arm64)..."
	@mkdir -p $(AGENT_BUILD_DIR)/windows/arm64
	GOOS=windows GOARCH=arm64 CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) $(LDFLAGS) \
		-o $(AGENT_BUILD_DIR)/windows/arm64/$(BINARY_NAME).exe ./cmd/agent
	@echo "✅ Built: $(AGENT_BUILD_DIR)/windows/arm64/$(BINARY_NAME).exe"

# Linux builds
.PHONY: linux
linux: linux-amd64 linux-386 linux-arm64 linux-arm

.PHONY: linux-amd64
linux-amd64:
	@echo "Building for Linux (amd64)..."
	@mkdir -p $(AGENT_BUILD_DIR)/linux/amd64
	GOOS=linux GOARCH=amd64 CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) $(LDFLAGS) \
		-o $(AGENT_BUILD_DIR)/linux/amd64/$(BINARY_NAME) ./cmd/agent
	@echo "✅ Built: $(AGENT_BUILD_DIR)/linux/amd64/$(BINARY_NAME)"

.PHONY: linux-386
linux-386:
	@echo "Building for Linux (386)..."
	@mkdir -p $(AGENT_BUILD_DIR)/linux/386
	GOOS=linux GOARCH=386 CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) $(LDFLAGS) \
		-o $(AGENT_BUILD_DIR)/linux/386/$(BINARY_NAME) ./cmd/agent
	@echo "✅ Built: $(AGENT_BUILD_DIR)/linux/386/$(BINARY_NAME)"

.PHONY: linux-arm64
linux-arm64:
	@echo "Building for Linux (arm64)..."
	@mkdir -p $(AGENT_BUILD_DIR)/linux/arm64
	GOOS=linux GOARCH=arm64 CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) $(LDFLAGS) \
		-o $(AGENT_BUILD_DIR)/linux/arm64/$(BINARY_NAME) ./cmd/agent
	@echo "✅ Built: $(AGENT_BUILD_DIR)/linux/arm64/$(BINARY_NAME)"

.PHONY: linux-arm
linux-arm:
	@echo "Building for Linux (arm)..."
	@mkdir -p $(AGENT_BUILD_DIR)/linux/arm
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) $(LDFLAGS) \
		-o $(AGENT_BUILD_DIR)/linux/arm/$(BINARY_NAME) ./cmd/agent
	@echo "✅ Built: $(AGENT_BUILD_DIR)/linux/arm/$(BINARY_NAME)"

# macOS builds
.PHONY: darwin
darwin: darwin-amd64 darwin-arm64

.PHONY: darwin-amd64
darwin-amd64:
	@echo "Building for macOS (Intel)..."
	@mkdir -p $(AGENT_BUILD_DIR)/darwin/amd64
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) $(LDFLAGS) \
		-o $(AGENT_BUILD_DIR)/darwin/amd64/$(BINARY_NAME) ./cmd/agent
	@echo "✅ Built: $(AGENT_BUILD_DIR)/darwin/amd64/$(BINARY_NAME)"

.PHONY: darwin-arm64
darwin-arm64:
	@echo "Building for macOS (Apple Silicon)..."
	@mkdir -p $(AGENT_BUILD_DIR)/darwin/arm64
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) $(LDFLAGS) \
		-o $(AGENT_BUILD_DIR)/darwin/arm64/$(BINARY_NAME) ./cmd/agent
	@echo "✅ Built: $(AGENT_BUILD_DIR)/darwin/arm64/$(BINARY_NAME)"

# Create release archives
.PHONY: release
release: build-all
	@echo "Creating release archives..."
	@mkdir -p $(AGENT_BUILD_DIR)/releases
	
	# Windows releases
	cd $(AGENT_BUILD_DIR)/windows/amd64 && zip -q ../../releases/$(BINARY_NAME)-$(VERSION)-windows-amd64.zip $(BINARY_NAME).exe
	cd $(AGENT_BUILD_DIR)/windows/386 && zip -q ../../releases/$(BINARY_NAME)-$(VERSION)-windows-386.zip $(BINARY_NAME).exe
	cd $(AGENT_BUILD_DIR)/windows/arm64 && zip -q ../../releases/$(BINARY_NAME)-$(VERSION)-windows-arm64.zip $(BINARY_NAME).exe
	
	# Linux releases
	cd $(AGENT_BUILD_DIR)/linux/amd64 && tar czf ../../releases/$(BINARY_NAME)-$(VERSION)-linux-amd64.tar.gz $(BINARY_NAME)
	cd $(AGENT_BUILD_DIR)/linux/386 && tar czf ../../releases/$(BINARY_NAME)-$(VERSION)-linux-386.tar.gz $(BINARY_NAME)
	cd $(AGENT_BUILD_DIR)/linux/arm64 && tar czf ../../releases/$(BINARY_NAME)-$(VERSION)-linux-arm64.tar.gz $(BINARY_NAME)
	cd $(AGENT_BUILD_DIR)/linux/arm && tar czf ../../releases/$(BINARY_NAME)-$(VERSION)-linux-arm.tar.gz $(BINARY_NAME)
	
	# macOS releases
	cd $(AGENT_BUILD_DIR)/darwin/amd64 && tar czf ../../releases/$(BINARY_NAME)-$(VERSION)-darwin-amd64.tar.gz $(BINARY_NAME)
	cd $(AGENT_BUILD_DIR)/darwin/arm64 && tar czf ../../releases/$(BINARY_NAME)-$(VERSION)-darwin-arm64.tar.gz $(BINARY_NAME)
	
	@echo "✅ Release archives created in $(AGENT_BUILD_DIR)/releases/"

# Development targets
.PHONY: run
run: build
	$(AGENT_BUILD_DIR)/$(BINARY_NAME)

.PHONY: test
test:
	$(GO) test -v ./...

.PHONY: test-coverage
test-coverage:
	$(GO) test -v -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

.PHONY: lint
lint:
	golangci-lint run

.PHONY: fmt
fmt:
	$(GO) fmt ./...

.PHONY: vet
vet:
	$(GO) vet ./...

# Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(AGENT_BUILD_DIR)
	@rm -f coverage.out coverage.html
	@echo "✅ Clean complete"

# Install dependencies
.PHONY: deps
deps:
	@echo "Installing dependencies..."
	$(GO) mod download
	$(GO) mod tidy
	@echo "✅ Dependencies installed"

# Show version info
.PHONY: version
version:
	@echo "Version: $(VERSION)"
	@echo "Commit: $(COMMIT)"
	@echo "Build Time: $(BUILD_TIME)"

# Help
.PHONY: help
help:
	@echo "KrakenHashes Agent Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make [target]"
	@echo ""
	@echo "Build targets:"
	@echo "  build          - Build for current platform"
	@echo "  build-all      - Build for all supported platforms"
	@echo "  windows        - Build for all Windows architectures"
	@echo "  linux          - Build for all Linux architectures"
	@echo "  darwin         - Build for all macOS architectures"
	@echo "  release        - Build all platforms and create release archives"
	@echo ""
	@echo "Specific platform targets:"
	@echo "  windows-amd64  - Build for Windows x64"
	@echo "  windows-386    - Build for Windows x86"
	@echo "  windows-arm64  - Build for Windows ARM64"
	@echo "  linux-amd64    - Build for Linux x64"
	@echo "  linux-386      - Build for Linux x86"
	@echo "  linux-arm64    - Build for Linux ARM64"
	@echo "  linux-arm      - Build for Linux ARM"
	@echo "  darwin-amd64   - Build for macOS Intel"
	@echo "  darwin-arm64   - Build for macOS Apple Silicon"
	@echo ""
	@echo "Development targets:"
	@echo "  run            - Build and run the agent"
	@echo "  test           - Run tests"
	@echo "  test-coverage  - Run tests with coverage report"
	@echo "  lint           - Run linter (requires golangci-lint)"
	@echo "  fmt            - Format code"
	@echo "  vet            - Run go vet"
	@echo "  deps           - Download and tidy dependencies"
	@echo ""
	@echo "Other targets:"
	@echo "  clean          - Remove build artifacts"
	@echo "  version        - Show version information"
	@echo "  help           - Show this help message"

# Default target
.DEFAULT_GOAL := help