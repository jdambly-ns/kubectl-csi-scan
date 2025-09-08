package detect

import (
	"context"
	"fmt"
	"time"

	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/jdambly/kubectl-csi-scan/pkg/client"
	"github.com/jdambly/kubectl-csi-scan/pkg/types"
)

// VolumeAttachmentDetector implements detection via VolumeAttachment API objects
type VolumeAttachmentDetector struct {
	client       client.KubernetesClient
	targetDriver string
}

// NewVolumeAttachmentDetector creates a new VolumeAttachment detector
func NewVolumeAttachmentDetector(kubeClient client.KubernetesClient, targetDriver string) *VolumeAttachmentDetector {
	return &VolumeAttachmentDetector{
		client:       kubeClient,
		targetDriver: targetDriver,
	}
}

// Detect finds VolumeAttachment conflicts and stuck attachments
func (d *VolumeAttachmentDetector) Detect(ctx context.Context) ([]types.CSIMountIssue, error) {
	var issues []types.CSIMountIssue

	// Get all VolumeAttachments
	vas, err := d.client.StorageV1().VolumeAttachments().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list VolumeAttachments: %w", err)
	}

	// Track attachments by volume handle for conflict detection
	volumeAttachments := make(map[string][]types.VolumeAttachmentInfo)
	attachedVAs := make(map[string]types.VolumeAttachmentInfo)

	for _, va := range vas.Items {
		// Filter by driver if specified
		if d.targetDriver != "" && va.Spec.Attacher != d.targetDriver {
			continue
		}

		vaInfo := types.VolumeAttachmentInfo{
			Name:           va.Name,
			Node:           va.Spec.NodeName,
			VolumeHandle:   d.getVolumeHandle(va.Spec.Source),
			Driver:         va.Spec.Attacher, // Attacher field contains the CSI driver name
			Attached:       va.Status.Attached,
			LastTransition: va.CreationTimestamp,
		}

		if va.Status.AttachError != nil {
			vaInfo.AttachError = va.Status.AttachError.Message
		}
		if va.Status.DetachError != nil {
			vaInfo.DetachError = va.Status.DetachError.Message
		}

		volumeHandle := vaInfo.VolumeHandle
		volumeAttachments[volumeHandle] = append(volumeAttachments[volumeHandle], vaInfo)

		if va.Status.Attached {
			attachedVAs[volumeHandle] = vaInfo
		}

		// Check for attach/detach errors
		if va.Status.AttachError != nil || va.Status.DetachError != nil {
			severity := d.calculateSeverity(va, volumeAttachments[volumeHandle])
			
			// Determine issue type based on error type
			var issueType types.IssueType
			if va.Status.DetachError != nil {
				issueType = types.StuckVolumeDetachment
			} else {
				issueType = types.FailedAttachVolume
			}
			
			issue := types.CSIMountIssue{
				Type:        issueType,
				Severity:    severity,
				Node:        va.Spec.NodeName,
				Volume:      volumeHandle,
				Driver:      vaInfo.Driver,
				Description: d.formatErrorDescription(va),
				DetectedBy:  types.VolumeAttachmentMethod,
				DetectedAt:  time.Now(),
				Metadata: map[string]string{
					"volumeattachment_name": va.Name,
					"attach_error":          vaInfo.AttachError,
					"detach_error":          vaInfo.DetachError,
				},
			}
			issues = append(issues, issue)
		}

		// Check for stuck attachments (not attached after significant time)
		if !va.Status.Attached && va.Status.AttachError == nil {
			timeSinceCreation := time.Since(va.CreationTimestamp.Time)
			if timeSinceCreation > 30*time.Minute { // Consider stuck after 30 minutes
				severity := d.calculateStuckAttachmentSeverity(timeSinceCreation)
				issue := types.CSIMountIssue{
					Type:        types.StuckVolumeAttachment,
					Severity:    severity,
					Node:        va.Spec.NodeName,
					Volume:      volumeHandle,
					Driver:      vaInfo.Driver,
					Description: fmt.Sprintf("Volume stuck in attaching state for %v", timeSinceCreation.Round(time.Minute)),
					DetectedBy:  types.VolumeAttachmentMethod,
					DetectedAt:  time.Now(),
					Metadata: map[string]string{
						"volume_attachment_name": va.Name,
						"stuck_duration":        timeSinceCreation.String(),
						"created_at":           va.CreationTimestamp.Format(time.RFC3339),
						"age_hours":            fmt.Sprintf("%.1f", timeSinceCreation.Hours()),
					},
				}
				issues = append(issues, issue)
			}
		}
	}

	// Detect multiple attachments for same volume
	for volumeHandle, attachments := range volumeAttachments {
		if len(attachments) > 1 {
			// Check if multiple are attached
			attachedCount := 0
			var attachedNodes []string
			for _, attachment := range attachments {
				if attachment.Attached {
					attachedCount++
					attachedNodes = append(attachedNodes, attachment.Node)
				}
			}

			if attachedCount > 1 {
				severity := d.calculateMultiAttachSeverity(attachedCount)
				issue := types.CSIMountIssue{
					Type:        types.MultipleAttachments,
					Severity:    severity,
					Volume:      volumeHandle,
					Driver:      attachments[0].Driver,
					Description: fmt.Sprintf("Volume attached to multiple nodes: %v", attachedNodes),
					DetectedBy:  types.VolumeAttachmentMethod,
					DetectedAt:  time.Now(),
					Metadata: map[string]string{
						"attached_count": fmt.Sprintf("%d", attachedCount),
						"attached_nodes": fmt.Sprintf("%v", attachedNodes),
						"total_attachments": fmt.Sprintf("%d", len(attachments)),
					},
				}
				issues = append(issues, issue)
			}
		}
	}

	return issues, nil
}

// matchesDriver checks if the VolumeAttachmentSource matches the target driver
func (d *VolumeAttachmentDetector) matchesDriver(source storagev1.VolumeAttachmentSource, targetDriver string) bool {
	if source.PersistentVolumeName != nil {
		// We would need to fetch the PV to get the CSI driver, for now assume match
		return true
	}
	if source.InlineVolumeSpec != nil && source.InlineVolumeSpec.CSI != nil {
		return source.InlineVolumeSpec.CSI.Driver == targetDriver
	}
	return true // Conservative approach - include if uncertain
}

// getVolumeHandle extracts the volume handle from VolumeAttachmentSource
func (d *VolumeAttachmentDetector) getVolumeHandle(source storagev1.VolumeAttachmentSource) string {
	if source.PersistentVolumeName != nil {
		return *source.PersistentVolumeName
	}
	if source.InlineVolumeSpec != nil && source.InlineVolumeSpec.CSI != nil {
		return source.InlineVolumeSpec.CSI.VolumeHandle
	}
	return "unknown"
}

// getDriverName extracts the CSI driver name from VolumeAttachmentSource  
func (d *VolumeAttachmentDetector) getDriverName(source storagev1.VolumeAttachmentSource) string {
	if source.InlineVolumeSpec != nil && source.InlineVolumeSpec.CSI != nil {
		return source.InlineVolumeSpec.CSI.Driver
	}
	// For PV sources, we'd need to fetch the PV to get the driver
	// Return empty string if we can't determine driver
	if d.targetDriver != "" {
		return d.targetDriver // Use target driver as fallback when filtering
	}
	return "" // Unknown driver when not filtering
}

// calculateSeverity determines issue severity based on VolumeAttachment state
func (d *VolumeAttachmentDetector) calculateSeverity(va storagev1.VolumeAttachment, allAttachments []types.VolumeAttachmentInfo) types.IssueSeverity {
	attachedCount := 0
	for _, attachment := range allAttachments {
		if attachment.Attached {
			attachedCount++
		}
	}

	if attachedCount >= 5 {
		return types.SeverityCritical
	} else if attachedCount >= 3 {
		return types.SeverityHigh
	} else if attachedCount == 2 {
		return types.SeverityMedium
	}
	return types.SeverityLow
}

// calculateMultiAttachSeverity determines severity based on number of simultaneous attachments
func (d *VolumeAttachmentDetector) calculateMultiAttachSeverity(attachedCount int) types.IssueSeverity {
	if attachedCount >= 5 {
		return types.SeverityCritical
	} else if attachedCount >= 3 {
		return types.SeverityHigh
	}
	return types.SeverityMedium
}

// calculateStuckAttachmentSeverity determines severity based on how long attachment has been stuck
func (d *VolumeAttachmentDetector) calculateStuckAttachmentSeverity(stuckDuration time.Duration) types.IssueSeverity {
	if stuckDuration > 4*time.Hour {
		return types.SeverityCritical
	} else if stuckDuration > 2*time.Hour {
		return types.SeverityHigh
	} else if stuckDuration > 1*time.Hour {
		return types.SeverityMedium
	}
	return types.SeverityLow
}

// formatErrorDescription creates a human-readable description of the error
func (d *VolumeAttachmentDetector) formatErrorDescription(va storagev1.VolumeAttachment) string {
	var errors []string
	
	if va.Status.AttachError != nil {
		errors = append(errors, fmt.Sprintf("Attach Error: %s", va.Status.AttachError.Message))
	}
	
	if va.Status.DetachError != nil {
		errors = append(errors, fmt.Sprintf("Detach Error: %s", va.Status.DetachError.Message))
	}
	
	if len(errors) == 0 {
		return "VolumeAttachment in inconsistent state"
	}
	
	result := fmt.Sprintf("VolumeAttachment %s on node %s has errors: ", va.Name, va.Spec.NodeName)
	for i, err := range errors {
		if i > 0 {
			result += "; "
		}
		result += err
	}
	
	return result
}