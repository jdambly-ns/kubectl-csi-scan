#!/bin/bash

# Build script for kubectl-csi-scan cleanup container
# This builds the container image that can clean up stuck CSI mounts

set -euo pipefail

# Configuration
IMAGE_NAME="${IMAGE_NAME:-kubectl-csi-scan}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
REGISTRY="${REGISTRY:-}"

# Full image name
FULL_IMAGE_NAME="${IMAGE_NAME}:${IMAGE_TAG}"
if [ -n "$REGISTRY" ]; then
    FULL_IMAGE_NAME="${REGISTRY}/${FULL_IMAGE_NAME}"
fi

echo "Building container image: $FULL_IMAGE_NAME"

# Build the image
docker build -t "$FULL_IMAGE_NAME" .

echo "✅ Container build completed: $FULL_IMAGE_NAME"
echo ""
echo "Usage examples:"
echo "  # Run cleanup dry run on local Docker:"
echo "  docker run --rm --privileged -v /var/lib/kubelet:/var/lib/kubelet $FULL_IMAGE_NAME --dry-run --verbose"
echo ""
echo "  # Push to registry:"
echo "  docker push $FULL_IMAGE_NAME"
echo ""
echo "  # Use with kubectl-csi-scan:"
echo "  kubectl-csi-scan cleanup --nodes=knode57 --image=$FULL_IMAGE_NAME --dry-run"