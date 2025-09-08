# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

kubectl-csi-scan is a Go-based kubectl plugin that detects and analyzes CSI mount cleanup issues in Kubernetes clusters. It implements multiple detection methods to identify stuck volume attachments, mount references, and CSI operation failures without relying on log scraping.

**Note**: The binary name is `kubectl-csi_mount_detective` (as seen in cmd/main.go:46), but the project is referred to as kubectl-csi-scan.

## Development Commands

### Build and Development
```bash
# Build the binary
make build

# Build for multiple platforms
make build-all

# Install to system PATH
make install

# Development setup (installs linting tools)
make dev-setup
```

### Testing and Quality
```bash
# Run tests using Ginkgo framework
make test

# Run tests with coverage report (generates coverage.html)
make test-coverage

# Run tests with coverage threshold validation (fails if <80%)
make test-coverage-threshold

# Run tests in watch mode for development
make test-watch

# Run integration tests (requires cluster access)
make test-integration

# Generate test mocks
make generate-mocks

# Validate test structure and check mock generation
make test-validate

# Lint code using golangci-lint
make lint

# Format code
make fmt
```

### Dependencies
```bash
# Update dependencies
make mod-update

# Download dependencies
go mod download
```

### Examples and Demo
```bash
# Generate example outputs (help text, queries, alerts, dashboard)
make examples

# Run demo (requires kubectl cluster access)
make demo

# Run CI/QA pipeline locally
make ci
```

## Code Architecture

### Core Components

1. **Entry Point**: `cmd/main.go`
   - CLI interface using cobra framework
   - Subcommands: `detect`, `analyze`, `metrics`
   - Global kubernetes client configuration
   - Uses zerolog for structured logging with console output for development

2. **Detection Framework**: `pkg/detect/detector.go`
   - Coordinates multiple detection methods
   - Provides unified result aggregation
   - Handles filtering and recommendation generation
   - Supports context-based timeouts (2-minute default)

3. **Detection Methods** (all in `pkg/detect/`):
   - `volumeattachments.go`: VolumeAttachment API analysis (most reliable)
   - `crossnodepvc.go`: Cross-node PVC usage analysis
   - `events.go`: Kubernetes events monitoring (1-hour default window)
   - `metrics.go`: Prometheus metrics queries

4. **Type Definitions**: `pkg/types/types.go`
   - Core data structures for issues, detection options, and results
   - Issue severity levels: low, medium, high, critical
   - Detection method constants and issue type enums
   - Rich metadata support for detailed analysis

5. **Client Abstraction**: `pkg/client/`
   - `interfaces.go`: Kubernetes client interface definitions for testing
   - `client.go`: Concrete client implementation
   - `mocks/`: Generated mocks using go.uber.org/mock

### Detection Methods

The tool implements four primary detection approaches:

1. **VolumeAttachment API Inspection**: Checks for conflicting attachment states (most reliable method)
2. **Cross-Node PVC Analysis**: Identifies volumes attached to multiple nodes  
3. **Kubernetes Events Monitoring**: Detects Multi-Attach and FailedAttachVolume events
4. **Prometheus Metrics Queries**: Monitors CSI operation failures and timeouts

### Plugin Design Pattern

- Follows kubectl plugin naming convention (`kubectl-csi_scan`)
- Uses kubernetes client-go for cluster API access
- Each detection method is implemented as a separate detector with common interface
- Results are aggregated and filtered through the main detector coordinator

## Project Structure

```
├── cmd/main.go              # CLI entry point and command definitions
├── pkg/
│   ├── client/              # Kubernetes client abstractions and interfaces
│   │   ├── interfaces.go    # Client interface definitions for testing
│   │   └── mocks/           # Generated mocks for testing
│   ├── detect/              # Detection method implementations
│   │   ├── detector.go      # Main coordinator and result aggregation
│   │   ├── volumeattachments.go
│   │   ├── crossnodepvc.go
│   │   ├── events.go
│   │   ├── metrics.go
│   │   └── *_test.go        # Ginkgo test files for each detector
│   └── types/
│       └── types.go         # Core type definitions and constants
├── Makefile                 # Build, test, and development commands
├── go.mod                   # Go module definition
├── README.md               # User documentation
└── CLAUDE.md               # Developer guidance (this file)
```

## Key Dependencies

- **Kubernetes**: Uses client-go v0.28.0 for cluster API access
- **CLI Framework**: spf13/cobra for command structure
- **Testing Framework**: Ginkgo v2 and Gomega for BDD-style testing
- **Logging**: zerolog for structured logging
- **Mock Generation**: go.uber.org/mock for test mocking
- **Go Version**: Requires Go 1.24+

## Development Workflow

1. **Initial Setup**: Use `make dev-setup` to install linting tools and dependencies
2. **Development**: Run `make build` to compile during development
3. **Testing**: Use `make test` to run the test suite, `make test-watch` for development
4. **Quality**: Run `make lint` before committing changes
5. **Integration**: Use `make demo` to test against a live cluster (requires kubectl access)
6. **CI**: Run `make ci` to execute full CI pipeline locally (lint + coverage threshold)

### Testing Strategy

- **Unit Tests**: Ginkgo BDD-style tests with Gomega matchers
- **Mocking**: Uses go.uber.org/mock for Kubernetes client mocking
- **Coverage**: 80% minimum threshold enforced by CI
- **Integration**: Requires live Kubernetes cluster access for full validation

## Testing Requirements

The tool requires a Kubernetes cluster for integration testing. Detection methods interact with:
- VolumeAttachment objects (storage/v1 API)
- Pods and PVCs (core/v1 API)  
- Events (core/v1 API)
- Prometheus metrics (when configured)

## CLI Command Structure

The main entry point creates three subcommands:
- **detect**: Primary detection command with configurable methods, output formats, and filtering
- **analyze**: Detailed analysis including cluster statistics and recommendations  
- **metrics**: Generate Prometheus queries, alerting rules, and Grafana dashboards

### Output Formats
- `table`: Human-readable tabular output (default)
- `json`: Structured JSON for programmatic use
- `detailed`: Markdown-style detailed report
- CLI provides progress feedback and summary statistics

## Error Handling

The codebase implements comprehensive error handling:
- Client connection errors with helpful messages
- Context-based timeouts for all operations
- Validation of CLI parameters with user-friendly error messages
- Graceful handling of missing cluster permissions