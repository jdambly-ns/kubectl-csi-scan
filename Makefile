# kubectl-csi-scan Makefile

# Variables
BINARY_NAME=kubectl-csi_scan
PACKAGE=github.com/jdambly/kubectl-csi-scan
VERSION?=v0.1.0
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Go build flags
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.gitCommit=$(GIT_COMMIT)"

# Default target
.PHONY: all
all: clean lint test build

# Build the binary
.PHONY: build
build:
	@echo "Building $(BINARY_NAME)..."
	go build $(LDFLAGS) -o $(BINARY_NAME) cmd/main.go

# Build for multiple platforms
.PHONY: build-all
build-all: clean
	@echo "Building for multiple platforms..."
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-linux-amd64 cmd/main.go
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-amd64 cmd/main.go
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-arm64 cmd/main.go
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-windows-amd64.exe cmd/main.go

# Install to system PATH
.PHONY: install
install: build
	@echo "Installing $(BINARY_NAME) to /usr/local/bin..."
	sudo mv $(BINARY_NAME) /usr/local/bin/

# Run unit tests
.PHONY: test
test:
	@echo "Running unit tests..."
	ginkgo -r --randomize-all --randomize-suites --fail-on-pending --cover --trace

# Run tests with coverage
.PHONY: test-coverage
test-coverage:
	@echo "Running tests with coverage..."
	ginkgo -r --randomize-all --randomize-suites --fail-on-pending --cover --trace --coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run tests with coverage and fail if below threshold
.PHONY: test-coverage-threshold
test-coverage-threshold:
	@echo "Running tests with coverage threshold..."
	ginkgo -r --randomize-all --randomize-suites --fail-on-pending --cover --trace --coverprofile=coverage.out
	go tool cover -func=coverage.out | grep total | awk '{print $$3}' | sed 's/%//' | awk '{if ($$1 < 80) {print "Coverage " $$1 "% is below 80% threshold"; exit 1} else {print "Coverage " $$1 "% meets threshold"}}'

# Run tests in watch mode for development
.PHONY: test-watch
test-watch:
	@echo "Running tests in watch mode..."
	ginkgo watch -r --randomize-all --randomize-suites --fail-on-pending

# Run integration tests (requires cluster access)
.PHONY: test-integration
test-integration:
	@echo "Running integration tests..."
	ginkgo -r --randomize-all --randomize-suites --fail-on-pending --focus="Integration" --trace

# Generate test mocks
.PHONY: generate-mocks
generate-mocks:
	@echo "Generating test mocks..."
	go generate ./pkg/client/...

# Setup test dependencies
.PHONY: test-setup
test-setup:
	@echo "Setting up test dependencies..."
	go install github.com/onsi/ginkgo/v2/ginkgo@latest
	go install go.uber.org/mock/mockgen@latest
	go mod download

# Validate test structure and dependencies
.PHONY: test-validate
test-validate:
	@echo "Validating test structure..."
	@echo "Checking for test files..."
	@find . -name "*_test.go" -type f | wc -l | xargs -I {} echo "Found {} test files"
	@echo "Checking for Ginkgo test suites..."
	@find . -name "suite_test.go" -type f | wc -l | xargs -I {} echo "Found {} test suites"
	@echo "Checking mock generation..."
	@find . -name "mock_*.go" -type f | wc -l | xargs -I {} echo "Found {} mock files"
	ginkgo -r --dry-run

# Lint the code
.PHONY: lint
lint:
	@echo "Running linters..."
	golangci-lint run

# Format the code
.PHONY: fmt
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning build artifacts..."
	rm -f $(BINARY_NAME)
	rm -rf bin/
	rm -f coverage.out coverage.html

# Initialize go modules
.PHONY: mod-init
mod-init:
	go mod init $(PACKAGE)
	go mod tidy

# Update dependencies
.PHONY: mod-update
mod-update:
	go mod tidy
	go mod download

# Generate examples
.PHONY: examples
examples: build
	@echo "Generating example outputs..."
	mkdir -p examples
	./$(BINARY_NAME) detect --help > examples/detect-help.txt
	./$(BINARY_NAME) metrics > examples/prometheus-queries.txt
	./$(BINARY_NAME) metrics --generate-alerts > examples/prometheus-alerts.yaml
	./$(BINARY_NAME) metrics --generate-dashboard > examples/grafana-dashboard.json

# Demo mode (requires kubectl access)
.PHONY: demo
demo: build
	@echo "Running demo detection..."
	@echo "1. VolumeAttachment detection:"
	./$(BINARY_NAME) detect --method=volumeattachments --output=table || echo "No cluster access"
	@echo "\n2. Sample Prometheus queries:"
	./$(BINARY_NAME) metrics | head -20

# Development setup
.PHONY: dev-setup
dev-setup: test-setup
	@echo "Setting up development environment..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go mod download

# CI/QA target for continuous integration
.PHONY: ci
ci: lint test-coverage-threshold
	@echo "CI checks completed successfully"

# Docker build
.PHONY: docker-build
docker-build:
	@echo "Building Docker image..."
	docker build -t $(BINARY_NAME):$(VERSION) .
	docker tag $(BINARY_NAME):$(VERSION) $(BINARY_NAME):latest

# Help
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  all           - Clean, lint, test, and build (default)"
	@echo "  build         - Build the binary"
	@echo "  build-all     - Build for multiple platforms"
	@echo "  install       - Install to system PATH"
	@echo "  test          - Run tests"
	@echo "  test-coverage - Run tests with coverage"
	@echo "  lint          - Run linters"
	@echo "  fmt           - Format code"
	@echo "  clean         - Clean build artifacts"
	@echo "  ci            - Run CI checks (lint + coverage threshold)"
	@echo "  mod-init      - Initialize go modules"
	@echo "  mod-update    - Update dependencies"
	@echo "  examples      - Generate example outputs"
	@echo "  demo          - Run demo detection"
	@echo "  dev-setup     - Set up development environment"
	@echo "  docker-build  - Build Docker image"
	@echo "  help          - Show this help"