# kubectl-csi-scan

A kubectl plugin to detect and analyze CSI mount cleanup issues in Kubernetes clusters.

## Purpose

This plugin implements the detection methods identified in investigation of cinder CSI mount cleanup failures (Netskope SYS-24741/SYS-24675). It provides multiple detection approaches to identify stuck volume attachments and mount references without relying on log scraping.

## Detection Methods

1. **VolumeAttachment API Inspection** - Most reliable, checks for conflicting attachment states
2. **Cross-Node PVC Analysis** - Identifies volumes that appear attached to multiple nodes  
3. **Kubernetes Events Monitoring** - Detects Multi-Attach and FailedAttachVolume events
4. **Prometheus Metrics Queries** - Monitors CSI operation failures and timeouts

## Installation

### Using Make (Recommended)

```bash
# Build the plugin
make build

# Install to system PATH 
make install

# Or build for multiple platforms
make build-all
```

### Manual Build

```bash
# Build the plugin
go build -o kubectl-csi_mount_detective cmd/main.go

# Install to PATH (make it available as kubectl csi-scan)
sudo mv kubectl-csi_mount_detective /usr/local/bin/
```

## Usage

### Basic Detection

```bash
# Detect all CSI mount issues using all methods
kubectl csi-scan detect

# Check specific detection method
kubectl csi-scan detect --method=volumeattachments
kubectl csi-scan detect --method=cross-node-pvc
kubectl csi-scan detect --method=events
kubectl csi-scan detect --method=metrics

# Check specific CSI driver
kubectl csi-scan detect --driver=cinder.csi.openstack.org
kubectl csi-scan detect --driver=ebs.csi.aws.com

# Filter by severity level
kubectl csi-scan detect --min-severity=high
kubectl csi-scan detect --min-severity=critical
```

### Output Formats

```bash
# Default table output
kubectl csi-scan detect

# JSON output for programmatic use
kubectl csi-scan detect --output=json

# Detailed markdown-style report
kubectl csi-scan detect --output=detailed

# Generate cleanup recommendations
kubectl csi-scan detect --recommend-cleanup
```

### Analysis and Metrics

```bash
# Get detailed cluster analysis
kubectl csi-scan analyze

# Generate Prometheus metrics queries
kubectl csi-scan metrics

# Get recent CSI-related events
kubectl csi-scan detect --method=events --lookback=2h
```

### Advanced Usage

```bash
# Combine multiple methods with specific driver
kubectl csi-scan detect --method=volumeattachments,events --driver=cinder.csi.openstack.org

# Get high-severity issues with cleanup recommendations
kubectl csi-scan detect --min-severity=high --recommend-cleanup --output=detailed

# Export results for further analysis
kubectl csi-scan detect --output=json > csi-issues.json
```

## Development

### Prerequisites

- Go 1.24+
- kubectl with cluster access
- Make (optional, for using Makefile commands)

### Development Commands

```bash
# Initial setup (installs linting tools)
make dev-setup

# Build and test during development
make build
make test

# Run tests with coverage
make test-coverage

# Run tests in watch mode for development
make test-watch

# Validate tests and check mocks
make test-validate

# Run linting
make lint

# Format code
make fmt

# Update dependencies
make mod-update

# Run full CI pipeline locally
make ci
```

### Testing

The project uses Ginkgo v2 for BDD-style testing with Gomega matchers:

```bash
# Run all tests
make test

# Run tests with coverage report (generates coverage.html)
make test-coverage

# Run tests with coverage threshold validation (requires ≥80%)
make test-coverage-threshold

# Run integration tests (requires cluster access)
make test-integration

# Generate test mocks
make generate-mocks
```

**Test Coverage**: Currently at **85.2%** (exceeds 80% minimum requirement)

### Project Structure

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
├── README.md               # This file
└── CLAUDE.md               # Developer guidance
```

### Key Dependencies

- **Kubernetes**: client-go v0.28.0 for cluster API access
- **CLI Framework**: spf13/cobra for command structure  
- **Testing**: Ginkgo v2 and Gomega for BDD-style testing
- **Logging**: zerolog for structured logging
- **Mocking**: go.uber.org/mock for test mocking

## Output Examples

### Table Output (Default)

```
SEVERITY  TYPE                    NODE      VOLUME                PVC             DRIVER                    DETECTED_BY       AGE
high      stuck-volume-attachment node-1    pvc-abc123           data-claim      cinder.csi.openstack.org volumeattachments 2h30m
critical  multiple-attachments    node-1,2  pvc-def456           cache-volume    ebs.csi.aws.com          volumeattachments 4h15m
medium    failed-attach-volume    node-3    pvc-789xyz           logs-pvc        disk.csi.azure.com       events           45m
```

### JSON Output

```json
{
  "issues": [
    {
      "type": "stuck-volume-attachment",
      "severity": "high",
      "node": "node-1",
      "volume": "pvc-abc123",
      "pvc": "data-claim",
      "driver": "cinder.csi.openstack.org",
      "description": "Volume stuck in attaching state for 2h30m",
      "detected_by": "volumeattachments",
      "detected_at": "2025-09-08T15:30:00Z",
      "metadata": {
        "volume_attachment_name": "csi-abc123",
        "stuck_duration": "2h30m",
        "age_hours": "2.5"
      }
    }
  ],
  "summary": {
    "total_issues": 1,
    "by_severity": {"high": 1},
    "by_type": {"stuck-volume-attachment": 1},
    "methods_used": ["volumeattachments"]
  }
}
```

## Issue Types Detected

- **stuck-volume-attachment**: Volume stuck in attaching state for >30 minutes
- **stuck-volume-detachment**: Volume stuck in detaching state with finalizers
- **multiple-attachments**: Volume attached to multiple nodes simultaneously
- **failed-attach-volume**: AttachVolume operation failed with errors
- **failed-detach-volume**: DetachVolume operation failed with errors
- **multi-attach-error**: Multi-Attach error events detected
- **cross-node-pvc-usage**: PVC used by pods on multiple nodes
- **high-node-pvc-usage**: Node has excessive PVC attachments

## Severity Levels

- **Critical**: >4 hours stuck, or >5 simultaneous attachments
- **High**: >2 hours stuck, or >3 simultaneous attachments  
- **Medium**: >1 hour stuck, or moderate attachment issues
- **Low**: Recent issues or minor attachment problems

## Troubleshooting

### Common Issues

**Permission denied when accessing cluster:**
```bash
# Ensure kubectl is configured correctly
kubectl auth can-i list volumeattachments
kubectl auth can-i list events
```

**No issues detected but problems persist:**
```bash
# Try different detection methods
kubectl csi-scan detect --method=events --lookback=24h
kubectl csi-scan detect --method=cross-node-pvc

# Check all drivers (don't filter by specific driver)
kubectl csi-scan detect --driver=""
```

**Plugin not found:**
```bash
# Ensure binary name is correct
ls -la /usr/local/bin/kubectl-csi_mount_detective

# Or use direct binary execution
./kubectl-csi_mount_detective detect
```

## Background

This tool was developed to address production issues where:
- Pods get stuck in Init state due to mount failures
- "GetDeviceMountRefs check failed" errors prevent volume detachment  
- Multi-Attach errors block workload mobility across nodes
- 25%+ of cluster nodes affected by stuck CSI volumes

Based on Kubernetes source code analysis identifying the root cause in volume mount reference cleanup within the CSI plugin ecosystem.

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Run tests (`make test`)
4. Ensure code passes linting (`make lint`)
5. Commit changes (`git commit -m 'Add amazing feature'`)
6. Push to branch (`git push origin feature/amazing-feature`)
7. Open a Pull Request

### Code Quality Requirements

- Maintain test coverage ≥80%
- All tests must pass
- Code must pass golangci-lint checks
- Follow existing code patterns and conventions
