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

```bash
# Build the plugin
go build -o kubectl-csi_scan cmd/main.go

# Install to PATH (make it available as kubectl csi-scan)
sudo mv kubectl-csi_scan /usr/local/bin/
```

## Usage

```bash
# Detect all CSI mount issues
kubectl csi-scan detect

# Check specific detection method
kubectl csi-scan detect --method=volumeattachments
kubectl csi-scan detect --method=cross-node-pvc
kubectl csi-scan detect --method=events
kubectl csi-scan detect --method=metrics

# Check specific CSI driver
kubectl csi-scan detect --driver=cinder.csi.openstack.org

# Output detailed analysis
kubectl csi-scan detect --output=detailed

# Generate cleanup recommendations
kubectl csi-scan detect --recommend-cleanup
```

## Background

This tool was developed to address production issues where:
- Pods get stuck in Init state due to mount failures
- "GetDeviceMountRefs check failed" errors prevent volume detachment
- Multi-Attach errors block workload mobility across nodes
- 25%+ of cluster nodes affected by stuck CSI volumes

Based on Kubernetes source code analysis identifying the root cause in volume mount reference cleanup within the CSI plugin ecosystem.
