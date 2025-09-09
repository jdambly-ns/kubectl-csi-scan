# CSI Mount Cleanup Examples

This document provides examples of how to use the kubectl-csi-scan cleanup functionality to address stuck CSI mount issues.

## Prerequisites

1. Built and pushed the cleanup container image
2. Cluster admin access (cleanup jobs require privileged containers)
3. Identified problematic nodes using the detection commands

## Basic Usage

### 1. Detect Issues First

```bash
# Find nodes with CSI mount issues
kubectl csi-mount-detective detect --method=volumeattachments

# Get detailed analysis
kubectl csi-mount-detective analyze
```

### 2. Dry Run Cleanup

Always start with a dry run to see what would be cleaned up:

```bash
# Dry run on specific nodes
kubectl csi-mount-detective cleanup --nodes=knode57,knode55 --dry-run --verbose

# Dry run with custom image
kubectl csi-mount-detective cleanup \
  --nodes=knode57 \
  --image=myregistry/kubectl-csi-scan:v1.0.0 \
  --dry-run
```

### 3. Perform Actual Cleanup

```bash
# Cleanup on identified problematic nodes
kubectl csi-mount-detective cleanup --nodes=knode57,knode55

# Cleanup with verbose logging for troubleshooting
kubectl csi-mount-detective cleanup --nodes=knode57 --verbose
```

## Advanced Usage

### Custom Namespace and Service Account

```bash
# Use custom namespace for cleanup jobs
kubectl csi-mount-detective cleanup \
  --nodes=knode57 \
  --namespace=kube-system \
  --service-account=csi-cleanup-sa

# Extend timeout for slow operations
kubectl csi-mount-detective cleanup \
  --nodes=knode57,knode55,knode50 \
  --timeout=15m
```

### Container Image Management

```bash
# Build and tag the cleanup image
./build-container.sh

# Push to your registry
export REGISTRY=myregistry.example.com
export IMAGE_TAG=v1.0.0
./build-container.sh
docker push myregistry.example.com/kubectl-csi-scan:v1.0.0

# Use the custom image
kubectl csi-mount-detective cleanup \
  --nodes=knode57 \
  --image=myregistry.example.com/kubectl-csi-scan:v1.0.0
```

## Monitoring and Verification

### Check Cleanup Job Status

```bash
# List cleanup jobs
kubectl get jobs -l app=kubectl-csi-scan

# Check job logs
kubectl logs job/csi-mount-cleanup-knode57

# Monitor job progress
kubectl get jobs -l app=kubectl-csi-scan -w
```

### Verify Cleanup Results

```bash
# Re-run detection to verify issues are resolved
kubectl csi-mount-detective detect --method=volumeattachments

# Check VolumeAttachment status
kubectl get volumeattachments

# Check for Multi-Attach events
kubectl get events --field-selector reason=FailedAttachVolume
```

## Workflow Integration

### Complete Cleanup Workflow

```bash
#!/bin/bash
set -euo pipefail

echo "1. Detecting CSI mount issues..."
NODES=$(kubectl csi-mount-detective detect --method=volumeattachments --output=json | \
        jq -r '.issues[].node' | sort -u | tr '\n' ',' | sed 's/,$//')

if [ -z "$NODES" ]; then
    echo "No issues detected"
    exit 0
fi

echo "2. Found issues on nodes: $NODES"
echo "3. Running dry-run cleanup..."
kubectl csi-mount-detective cleanup --nodes="$NODES" --dry-run

echo "4. Proceed with cleanup? (y/N)"
read -r CONFIRM
if [ "$CONFIRM" = "y" ] || [ "$CONFIRM" = "Y" ]; then
    echo "5. Performing cleanup..."
    kubectl csi-mount-detective cleanup --nodes="$NODES"
    
    echo "6. Verifying cleanup..."
    sleep 30
    kubectl csi-mount-detective detect --method=volumeattachments
else
    echo "Cleanup cancelled"
fi
```

## Troubleshooting

### Common Issues

1. **Permission Errors**: Ensure the service account has proper RBAC permissions
2. **Image Pull Errors**: Verify the container image is accessible from worker nodes
3. **Node Selector Issues**: Confirm node names match exactly (case-sensitive)
4. **Timeout Issues**: Increase timeout for nodes with many stuck mounts

### Debug Commands

```bash
# Check service account permissions
kubectl auth can-i create jobs --as=system:serviceaccount:default:kubectl-csi-scan-cleanup

# Verify node labels
kubectl get nodes --show-labels

# Check if image is pullable
kubectl run test-image --image=kubectl-csi-scan:latest --rm -it --restart=Never -- /bin/sh

# Get detailed job status
kubectl describe job csi-mount-cleanup-knode57
```

## Safety Considerations

1. **Always use dry-run first** to understand what will be cleaned up
2. **Test on non-production clusters** before using in production
3. **Backup critical data** before running cleanup on storage nodes
4. **Monitor cleanup progress** and be prepared to intervene if needed
5. **Verify results** after cleanup to ensure proper resolution

The cleanup functionality is designed to be safe and conservative, but it operates with privileged access to node filesystems. Use appropriate caution in production environments.