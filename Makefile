.PHONY: build build-docker clean test

# Version information
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S_UTC')
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Go build flags
LDFLAGS := -s -w \
	-X 'cfui/version.Version=$(VERSION)' \
	-X 'cfui/version.BuildTime=$(BUILD_TIME)' \
	-X 'cfui/version.GitCommit=$(GIT_COMMIT)'

# Build binary
build:
	@echo "Building cfui $(VERSION)..."
	@echo "  Version:    $(VERSION)"
	@echo "  Build Time: $(BUILD_TIME)"
	@echo "  Git Commit: $(GIT_COMMIT)"
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o cfui .
	@echo "Build complete: ./cfui"

# Build Docker image
build-docker:
	@echo "Building Docker image with version $(VERSION)..."
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		-t cfui:$(VERSION) \
		-t cfui:latest \
		.
	@echo "Docker image built: cfui:$(VERSION)"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -f cfui
	@echo "Clean complete"

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Run locally
run: build
	@echo "Starting cfui..."
	./cfui

# Show version info
version:
	@echo "Version:    $(VERSION)"
	@echo "Build Time: $(BUILD_TIME)"
	@echo "Git Commit: $(GIT_COMMIT)"
