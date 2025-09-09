FROM alpine:3.20

# Install necessary packages for mount operations and Kubernetes utilities
RUN apk add --no-cache \
    bash \
    util-linux \
    findmnt \
    lsblk \
    curl \
    jq \
    coreutils

# Create non-root user for security
RUN addgroup -g 1000 cleanup && \
    adduser -D -u 1000 -G cleanup cleanup

# Copy the cleanup script
COPY scripts/cleanup-mounts.sh /usr/local/bin/cleanup-mounts.sh
RUN chmod +x /usr/local/bin/cleanup-mounts.sh

# Set the working directory
WORKDIR /app

# Switch to non-root user (will be overridden to root when running with privileged containers)
USER cleanup

# Default command
ENTRYPOINT ["/usr/local/bin/cleanup-mounts.sh"]