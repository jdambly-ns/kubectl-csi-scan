#!/bin/bash

# CSI Mount Cleanup Script
# This script safely removes stuck CSI mount references that prevent volume detachment
# Based on the analysis in SYS-24741

set -euo pipefail

# Configuration
SCRIPT_NAME="csi-mount-cleanup"
LOG_PREFIX="[$SCRIPT_NAME]"
DRY_RUN="${DRY_RUN:-false}"
VERBOSE="${VERBOSE:-false}"
NODE_NAME="${NODE_NAME:-$(hostname)}"

# CSI mount paths to check
CSI_PLUGIN_PATH="/var/lib/kubelet/plugins/kubernetes.io/csi"
CSI_PV_PATH="/var/lib/kubelet/plugins/kubernetes.io/csi/pv"
CINDER_CSI_DRIVER="cinder.csi.openstack.org"

log() {
    echo "$LOG_PREFIX $*" >&2
}

verbose() {
    if [ "$VERBOSE" = "true" ]; then
        log "$@"
    fi
}

error() {
    log "ERROR: $*" >&2
    exit 1
}

check_privileges() {
    if [ "$(id -u)" -ne 0 ]; then
        error "This script must be run as root to perform mount operations"
    fi
}

check_paths() {
    local missing_paths=()
    
    if [ ! -d "$CSI_PLUGIN_PATH" ]; then
        missing_paths+=("$CSI_PLUGIN_PATH")
    fi
    
    if [ ! -d "$CSI_PV_PATH" ]; then
        missing_paths+=("$CSI_PV_PATH")
    fi
    
    if [ ${#missing_paths[@]} -gt 0 ]; then
        log "WARNING: Some CSI paths are missing: ${missing_paths[*]}"
        log "This node may not have CSI volumes or paths may be different"
    fi
}

find_stuck_mounts() {
    local stuck_mounts=()
    
    verbose "Scanning for stuck CSI mounts..."
    
    # Find cinder CSI globalmount directories that are still mounted
    if [ -d "$CSI_PV_PATH" ]; then
        while IFS= read -r -d '' mount_dir; do
            if mountpoint -q "$mount_dir" 2>/dev/null; then
                verbose "Found mounted directory: $mount_dir"
                stuck_mounts+=("$mount_dir")
            fi
        done < <(find "$CSI_PV_PATH" -name "globalmount" -type d -print0 2>/dev/null || true)
    fi
    
    # Find plugin-specific mount directories
    if [ -d "$CSI_PLUGIN_PATH" ]; then
        while IFS= read -r -d '' mount_dir; do
            if [[ "$mount_dir" == *"$CINDER_CSI_DRIVER"* ]] && mountpoint -q "$mount_dir" 2>/dev/null; then
                verbose "Found cinder CSI mount: $mount_dir"
                stuck_mounts+=("$mount_dir")
            fi
        done < <(find "$CSI_PLUGIN_PATH" -name "globalmount" -type d -print0 2>/dev/null || true)
    fi
    
    printf '%s\n' "${stuck_mounts[@]}"
}

check_mount_safety() {
    local mount_path="$1"
    
    # Check if mount path contains expected CSI patterns
    if [[ "$mount_path" != *"kubernetes.io/csi"* ]]; then
        log "WARNING: Mount path does not contain expected CSI pattern: $mount_path"
        return 1
    fi
    
    # Check if it's a cinder CSI mount or PV globalmount
    if [[ "$mount_path" == *"$CINDER_CSI_DRIVER"* ]] || [[ "$mount_path" == *"/pv/"*"/globalmount" ]]; then
        return 0
    fi
    
    log "WARNING: Mount path does not match expected patterns: $mount_path"
    return 1
}

cleanup_mount() {
    local mount_path="$1"
    
    if ! check_mount_safety "$mount_path"; then
        log "Skipping unsafe mount path: $mount_path"
        return 1
    fi
    
    log "Processing mount: $mount_path"
    
    if [ "$DRY_RUN" = "true" ]; then
        log "DRY RUN: Would unmount $mount_path"
        return 0
    fi
    
    # Attempt graceful unmount first
    if umount "$mount_path" 2>/dev/null; then
        log "Successfully unmounted: $mount_path"
        return 0
    fi
    
    # Try lazy unmount if graceful fails
    if umount -l "$mount_path" 2>/dev/null; then
        log "Successfully lazy unmounted: $mount_path"
        return 0
    fi
    
    # Force unmount as last resort
    if umount -f "$mount_path" 2>/dev/null; then
        log "Successfully force unmounted: $mount_path"
        return 0
    fi
    
    log "ERROR: Failed to unmount $mount_path"
    return 1
}

cleanup_mount_references() {
    local mount_path="$1"
    local parent_dir
    
    parent_dir=$(dirname "$mount_path")
    
    # Remove the globalmount directory if it's empty after unmounting
    if [ -d "$mount_path" ] && [ -z "$(ls -A "$mount_path" 2>/dev/null)" ]; then
        if [ "$DRY_RUN" = "true" ]; then
            log "DRY RUN: Would remove empty directory $mount_path"
        else
            rmdir "$mount_path" 2>/dev/null && log "Removed empty directory: $mount_path"
        fi
    fi
    
    # Clean up parent PV directory if it's empty
    if [ -d "$parent_dir" ] && [[ "$parent_dir" == *"/pv/"* ]] && [ -z "$(ls -A "$parent_dir" 2>/dev/null)" ]; then
        if [ "$DRY_RUN" = "true" ]; then
            log "DRY RUN: Would remove empty PV directory $parent_dir"
        else
            rmdir "$parent_dir" 2>/dev/null && log "Removed empty PV directory: $parent_dir"
        fi
    fi
}

show_summary() {
    local total_processed="$1"
    local successful_cleanups="$2"
    
    log "=== Cleanup Summary ==="
    log "Node: $NODE_NAME"
    log "Total mounts processed: $total_processed"
    log "Successful cleanups: $successful_cleanups"
    log "Failed cleanups: $((total_processed - successful_cleanups))"
    
    if [ "$DRY_RUN" = "true" ]; then
        log "DRY RUN MODE: No actual changes made"
    fi
}

main() {
    log "Starting CSI mount cleanup on node: $NODE_NAME"
    
    if [ "$DRY_RUN" = "true" ]; then
        log "Running in DRY RUN mode - no changes will be made"
    fi
    
    check_privileges
    check_paths
    
    local stuck_mounts
    mapfile -t stuck_mounts < <(find_stuck_mounts)
    
    if [ ${#stuck_mounts[@]} -eq 0 ]; then
        log "No stuck CSI mounts found on this node"
        return 0
    fi
    
    log "Found ${#stuck_mounts[@]} stuck mount(s) to process"
    
    local successful_cleanups=0
    local total_processed=0
    
    for mount_path in "${stuck_mounts[@]}"; do
        total_processed=$((total_processed + 1))
        
        if cleanup_mount "$mount_path"; then
            successful_cleanups=$((successful_cleanups + 1))
            cleanup_mount_references "$mount_path"
        fi
    done
    
    show_summary "$total_processed" "$successful_cleanups"
    
    if [ "$successful_cleanups" -gt 0 ]; then
        log "Cleanup completed. Recommend verifying volume attachment status in Kubernetes"
    fi
}

# Handle script arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        --verbose)
            VERBOSE=true
            shift
            ;;
        --node-name)
            NODE_NAME="$2"
            shift 2
            ;;
        --help)
            cat << EOF
CSI Mount Cleanup Script

Usage: $0 [OPTIONS]

OPTIONS:
    --dry-run       Show what would be done without making changes
    --verbose       Enable verbose logging
    --node-name     Specify the node name (default: hostname)
    --help          Show this help message

ENVIRONMENT VARIABLES:
    DRY_RUN         Set to 'true' to enable dry run mode
    VERBOSE         Set to 'true' to enable verbose logging
    NODE_NAME       Override the node name

EXAMPLES:
    # Dry run to see what would be cleaned up
    $0 --dry-run --verbose
    
    # Perform actual cleanup
    $0
    
    # Cleanup with custom node name
    $0 --node-name knode57

This script removes stuck CSI mount references that prevent proper volume
detachment in Kubernetes clusters with cinder CSI driver issues.
EOF
            exit 0
            ;;
        *)
            error "Unknown option: $1. Use --help for usage information."
            ;;
    esac
done

# Run the main function
main "$@"