package detect

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/jdambly/kubectl-csi-scan/pkg/client"
	"github.com/jdambly/kubectl-csi-scan/pkg/types"
)

// EventsDetector implements detection via Kubernetes events analysis
type EventsDetector struct {
	client       client.KubernetesClient
	targetDriver string
	lookbackDuration time.Duration
}

// NewEventsDetector creates a new events detector
func NewEventsDetector(kubeClient client.KubernetesClient, targetDriver string, lookbackDuration time.Duration) *EventsDetector {
	if lookbackDuration == 0 {
		lookbackDuration = 1 * time.Hour // Default to 1 hour lookback
	}
	
	return &EventsDetector{
		client:           kubeClient,
		targetDriver:     targetDriver,
		lookbackDuration: lookbackDuration,
	}
}

// Detect finds CSI-related issues from Kubernetes events
func (d *EventsDetector) Detect(ctx context.Context) ([]types.CSIMountIssue, error) {
	var issues []types.CSIMountIssue

	// Get events from all namespaces
	events, err := d.client.CoreV1().Events("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list events: %w", err)
	}

	cutoffTime := time.Now().Add(-d.lookbackDuration)

	for _, event := range events.Items {
		// Skip old events
		if event.LastTimestamp.Time.Before(cutoffTime) && event.EventTime.Time.Before(cutoffTime) {
			continue
		}

		// Filter by driver if specified
		if d.targetDriver != "" && !d.eventMatchesDriver(event, d.targetDriver) {
			continue
		}

		// Analyze event for CSI mount issues
		if issue := d.analyzeEvent(event); issue != nil {
			issues = append(issues, *issue)
		}
	}

	return issues, nil
}

// eventMatchesDriver checks if an event is related to the target CSI driver
func (d *EventsDetector) eventMatchesDriver(event corev1.Event, targetDriver string) bool {
	// Check message content for driver name
	if strings.Contains(event.Message, targetDriver) {
		return true
	}

	// Check if message contains any CSI driver names other than the target driver
	// Build a list of known CSI driver patterns
	knownDrivers := []string{
		"cinder.csi.openstack.org",
		"rook-ceph.rbd.csi.ceph.com", 
		"rook-ceph.cephfs.csi.ceph.com",
		"ebs.csi.aws.com",
		"disk.csi.azure.com",
		"pd.csi.storage.gke.io",
	}
	
	// Add target driver to known drivers if not already present
	if targetDriver != "" {
		found := false
		for _, known := range knownDrivers {
			if known == targetDriver {
				found = true
				break
			}
		}
		if !found {
			knownDrivers = append(knownDrivers, targetDriver)
		}
	}
	
	// Check if message contains any other CSI driver - if so, exclude it
	for _, driver := range knownDrivers {
		if driver != targetDriver && strings.Contains(event.Message, driver) {
			return false
		}
	}

	// Check if message looks like it's about a specific CSI driver but not ours
	// Look for pattern "driver.name.domain" in the message
	if strings.Contains(event.Message, ".csi.") {
		// If it mentions a CSI driver pattern but not our target driver, exclude it
		return false
	}

	// Include important volume events that don't mention specific drivers
	importantEvents := []string{
		"Multi-Attach error",
		"GetDeviceMountRefs",
		"FailedAttachVolume", 
		"FailedMount",
	}
	
	for _, important := range importantEvents {
		if strings.Contains(event.Message, important) || event.Reason == important {
			return true
		}
	}

	// Include events that mention CSI
	if strings.Contains(event.Reason, "CSI") || strings.Contains(event.Message, "CSI") {
		return true
	}

	return false
}

// analyzeEvent examines an individual event for CSI mount issues
func (d *EventsDetector) analyzeEvent(event corev1.Event) *types.CSIMountIssue {
	eventTime := event.LastTimestamp.Time
	if eventTime.IsZero() {
		eventTime = event.EventTime.Time
	}

	// Multi-Attach errors
	if strings.Contains(event.Message, "Multi-Attach error") {
		return &types.CSIMountIssue{
			Type:        types.MultiAttachError,
			Severity:    d.calculateEventSeverity(event),
			Node:        d.getNodeForDisplay(event),
			Volume:      d.extractVolumeFromMessage(event.Message),
			PVC:         d.getPVCForDisplay(event),
			Namespace:   event.Namespace,
			Driver:      d.extractDriverFromMessage(event.Message),
			Description: fmt.Sprintf("Multi-Attach error detected: %s", event.Message),
			DetectedBy:  types.EventsMethod,
			DetectedAt:  time.Now(),
			Metadata:    d.buildEventMetadata(event, eventTime),
		}
	}

	// Failed attach volume errors
	if event.Reason == "FailedAttachVolume" && event.Type == "Warning" {
		return &types.CSIMountIssue{
			Type:        types.FailedAttachVolume,
			Severity:    d.calculateEventSeverity(event),
			Node:        d.getNodeForDisplay(event),
			Volume:      d.extractVolumeFromMessage(event.Message),
			PVC:         d.getPVCForDisplay(event),
			Namespace:   event.Namespace,
			Driver:      d.extractDriverFromMessage(event.Message),
			Description: fmt.Sprintf("Failed to attach volume: %s", event.Message),
			DetectedBy:  types.EventsMethod,
			DetectedAt:  time.Now(),
			Metadata:    d.buildEventMetadata(event, eventTime),
		}
	}

	// Failed mount errors
	if event.Reason == "FailedMount" && event.Type == "Warning" {
		// Check for GetDeviceMountRefs related errors
		if strings.Contains(event.Message, "GetDeviceMountRefs") {
			return &types.CSIMountIssue{
				Type:        types.StuckMountReference,
				Severity:    d.calculateEventSeverity(event),
				Node:        d.getNodeForDisplay(event),
				Volume:      d.extractVolumeFromMessage(event.Message),
				PVC:         d.getPVCForDisplay(event),
				Namespace:   event.Namespace,
				Driver:      d.extractDriverFromMessage(event.Message),
				Description: fmt.Sprintf("Mount reference cleanup failure: %s", event.Message),
				DetectedBy:  types.EventsMethod,
				DetectedAt:  time.Now(),
				Metadata:    d.buildEventMetadata(event, eventTime),
			}
		}

		return &types.CSIMountIssue{
			Type:        types.CSIOperationFailure,
			Severity:    d.calculateEventSeverity(event),
			Node:        d.getNodeForDisplay(event),
			Volume:      d.extractVolumeFromMessage(event.Message),
			PVC:         d.getPVCForDisplay(event),
			Namespace:   event.Namespace,
			Driver:      d.extractDriverFromMessage(event.Message),
			Description: fmt.Sprintf("Failed to mount volume: %s", event.Message),
			DetectedBy:  types.EventsMethod,
			DetectedAt:  time.Now(),
			Metadata:    d.buildEventMetadata(event, eventTime),
		}
	}

	// Other CSI-related warning events - be much more specific
	if event.Type == "Warning" && d.isCSIRelatedEvent(event) {
		return &types.CSIMountIssue{
			Type:        types.CSIOperationFailure,
			Severity:    d.calculateEventSeverity(event),
			Node:        d.getNodeForDisplay(event),
			Volume:      d.extractVolumeFromMessage(event.Message),
			PVC:         d.getPVCForDisplay(event),
			Namespace:   event.Namespace,
			Driver:      d.extractDriverFromMessage(event.Message),
			Description: fmt.Sprintf("CSI operation issue: %s", event.Message),
			DetectedBy:  types.EventsMethod,
			DetectedAt:  time.Now(),
			Metadata:    d.buildEventMetadata(event, eventTime),
		}
	}

	return nil
}

// calculateEventSeverity determines severity based on event characteristics
func (d *EventsDetector) calculateEventSeverity(event corev1.Event) types.IssueSeverity {
	// Higher severity for specific critical errors (check first)
	if strings.Contains(event.Message, "Multi-Attach error") {
		return types.SeverityHigh
	}

	if strings.Contains(event.Message, "GetDeviceMountRefs") {
		return types.SeverityHigh
	}

	// Higher severity for frequently occurring events
	if event.Count >= 10 {
		return types.SeverityCritical
	} else if event.Count >= 7 {
		return types.SeverityHigh
	} else if event.Count >= 3 {
		return types.SeverityMedium
	}

	return types.SeverityLow
}

// extractVolumeFromMessage attempts to extract volume information from event message
func (d *EventsDetector) extractVolumeFromMessage(message string) string {
	// Look for PVC patterns (most common for CSI volumes)
	if strings.Contains(message, "pvc-") {
		parts := strings.Fields(message)
		for _, part := range parts {
			if strings.HasPrefix(part, "pvc-") {
				// Remove quotes and other punctuation
				part = strings.Trim(part, "\"',.()[]")
				return part
			}
		}
	}

	// Look for volume handle patterns with quotes
	volumePatterns := []string{
		"volume \"",
		"Volume \"",
		"volumeHandle \"",
		"volumeId \"",
		"volume_id \"",
	}
	
	for _, pattern := range volumePatterns {
		if strings.Contains(message, pattern) {
			start := strings.Index(message, pattern)
			if start != -1 {
				start += len(pattern)
				end := strings.Index(message[start:], "\"")
				if end != -1 {
					volumeHandle := message[start : start+end]
					if volumeHandle != "" && volumeHandle != "unknown" {
						return volumeHandle
					}
				}
			}
		}
	}
	
	// Look for volume names after specific keywords without quotes
	volumeKeywords := []string{"volume", "Volume", "volumeHandle", "volumeId"}
	words := strings.Fields(message)
	
	for i, word := range words {
		for _, keyword := range volumeKeywords {
			if strings.EqualFold(word, keyword) && i+1 < len(words) {
				nextWord := strings.Trim(words[i+1], "\"',.()[]:")
				if nextWord != "" && nextWord != "unknown" && !strings.Contains(nextWord, " ") {
					return nextWord
				}
			}
		}
	}
	
	// Look for any word that looks like a volume handle (contains pvc- or is a long alphanumeric string)
	for _, word := range words {
		cleanWord := strings.Trim(word, "\"',.()[]:")
		
		// Handle malformed cases like "volumes=[volume-name"
		if strings.Contains(cleanWord, "volumes=[") {
			// Extract everything after "volumes=["
			parts := strings.Split(cleanWord, "volumes=[")
			if len(parts) > 1 {
				volumePart := parts[1]
				// Remove any trailing brackets or punctuation
				volumePart = strings.Trim(volumePart, "[]()\"',.")
				if volumePart != "" && volumePart != "unknown" {
					return volumePart
				}
			}
		}
		
		if strings.HasPrefix(cleanWord, "pvc-") {
			return cleanWord
		}
		// Look for long alphanumeric strings that might be volume handles
		if len(cleanWord) > 10 && strings.ContainsAny(cleanWord, "0123456789") && strings.ContainsAny(cleanWord, "abcdefghijklmnopqrstuvwxyz") {
			// Exclude common kubernetes token patterns
			if !strings.Contains(cleanWord, "kube-api-access-") && !strings.Contains(cleanWord, "default-token-") {
				return cleanWord
			}
		}
	}

	return "unknown"
}

// extractDriverFromMessage attempts to extract CSI driver name from event message
func (d *EventsDetector) extractDriverFromMessage(message string) string {
	// Look for common CSI driver patterns
	csiDrivers := []string{
		"cinder.csi.openstack.org",
		"rook-ceph.rbd.csi.ceph.com",
		"rook-ceph.cephfs.csi.ceph.com",
		"ebs.csi.aws.com",
		"disk.csi.azure.com",
		"pd.csi.storage.gke.io",
	}

	for _, driver := range csiDrivers {
		if strings.Contains(message, driver) {
			return driver
		}
	}

	// If target driver is specified and not found in common list, check for it
	if d.targetDriver != "" && strings.Contains(message, d.targetDriver) {
		return d.targetDriver
	}

	return "unknown"
}

// extractNodeFromEvent attempts to extract node information from the event
func (d *EventsDetector) extractNodeFromEvent(event corev1.Event) string {
	// If the involved object is a Node, return its name
	if event.InvolvedObject.Kind == "Node" {
		return event.InvolvedObject.Name
	}
	
	// Try to extract from source host (for events reported by kubelet)
	if event.Source.Host != "" {
		return event.Source.Host
	}
	
	// Try to extract node name from the message for volume-related events
	nodePatterns := []string{
		"node \"",
		"Node \"",
		" node ",
		" Node ",
	}
	
	for _, pattern := range nodePatterns {
		if strings.Contains(event.Message, pattern) {
			// Find the start of the node name
			start := strings.Index(event.Message, pattern)
			if start != -1 {
				start += len(pattern)
				
				// Handle quoted node names
				if strings.HasSuffix(pattern, "\"") {
					end := strings.Index(event.Message[start:], "\"")
					if end != -1 {
						return event.Message[start : start+end]
					}
				} else {
					// Handle space-separated node names
					words := strings.Fields(event.Message[start:])
					if len(words) > 0 {
						// Clean up punctuation
						nodeName := strings.Trim(words[0], "\"',.()[]:")
						if nodeName != "" {
							return nodeName
						}
					}
				}
			}
		}
	}
	
	return ""
}

// extractPVCFromEvent attempts to extract PVC information from the event
func (d *EventsDetector) extractPVCFromEvent(event corev1.Event) string {
	// If the involved object is a PVC, return its name (this is the primary case)
	if event.InvolvedObject.Kind == "PersistentVolumeClaim" {
		return event.InvolvedObject.Name
	}
	
	// For Pod events, try to extract PVC name from the message
	if event.InvolvedObject.Kind == "Pod" {
		// Try to extract PVC name from the message
		pvcPatterns := []string{
			"pvc \"",
			"PVC \"",
			"persistentvolumeclaim \"",
			"PersistentVolumeClaim \"",
			"claim \"",
		}
		
		for _, pattern := range pvcPatterns {
			if strings.Contains(event.Message, pattern) {
				start := strings.Index(event.Message, pattern)
				if start != -1 {
					start += len(pattern)
					end := strings.Index(event.Message[start:], "\"")
					if end != -1 {
						return event.Message[start : start+end]
					}
				}
			}
		}
		
		// Look for PVC name patterns without quotes
		pvcWords := []string{"pvc", "PVC", "claim"}
		words := strings.Fields(event.Message)
		
		for i, word := range words {
			for _, pvcWord := range pvcWords {
				if strings.EqualFold(word, pvcWord) && i+1 < len(words) {
					// Next word might be the PVC name
					nextWord := strings.Trim(words[i+1], "\"',.()[]:")
					if nextWord != "" && !strings.Contains(nextWord, " ") {
						return nextWord
					}
				}
			}
		}
	}
	
	return ""
}

// getNodeForDisplay determines what to show in the Node column for the event
func (d *EventsDetector) getNodeForDisplay(event corev1.Event) string {
	// If the involved object is a Node, show its name
	if event.InvolvedObject.Kind == "Node" {
		return event.InvolvedObject.Name
	}
	
	// For volume-related events on pods, try to extract the node
	if event.InvolvedObject.Kind == "Pod" {
		// Try to extract from source host (most reliable for pod events)
		if event.Source.Host != "" {
			return event.Source.Host
		}
		
		// Try to extract node name from the message
		return d.extractNodeFromEvent(event)
	}
	
	// For other object types, check source host
	if event.Source.Host != "" {
		return event.Source.Host
	}
	
	return ""
}

// getPVCForDisplay determines what to show in the PVC column for the event
func (d *EventsDetector) getPVCForDisplay(event corev1.Event) string {
	// If the involved object is a PVC, show it in format "name" or "namespace/name" if needed
	if event.InvolvedObject.Kind == "PersistentVolumeClaim" {
		pvcName := event.InvolvedObject.Name
		// If the PVC is not in the same namespace as the event, show the full reference
		if event.InvolvedObject.Namespace != "" && event.InvolvedObject.Namespace != event.Namespace {
			return fmt.Sprintf("%s/%s", event.InvolvedObject.Namespace, pvcName)
		}
		return pvcName
	}
	
	// For Pod events, try to extract the PVC name from the message
	if event.InvolvedObject.Kind == "Pod" {
		return d.extractPVCFromEvent(event)
	}
	
	// For other object types, don't show anything in PVC column
	return ""
}

// isCSIRelatedEvent checks if an event is specifically related to CSI operations
func (d *EventsDetector) isCSIRelatedEvent(event corev1.Event) bool {
	// Explicit CSI mentions in message or reason
	if strings.Contains(event.Message, "CSI") || strings.Contains(event.Reason, "CSI") {
		return true
	}
	
	// Specific CSI driver patterns
	csiDriverPatterns := []string{
		".csi.",
		"csi.openstack.org",
		"csi.ceph.com",
		"csi.aws.com", 
		"csi.azure.com",
		"csi.storage.gke.io",
	}
	
	for _, pattern := range csiDriverPatterns {
		if strings.Contains(event.Message, pattern) {
			return true
		}
	}
	
	// Specific volume-related errors that are likely CSI-related
	csiVolumeReasons := []string{
		"VolumeBindingFailed",
		"ProvisioningFailed", 
		"VolumeFailedMount",
		"VolumeFailedUnmount",
		"VolumeResizeFailed",
		"VolumeResizing",
	}
	
	for _, reason := range csiVolumeReasons {
		if event.Reason == reason {
			return true
		}
	}
	
	// Check if event mentions persistent volume operations with storage class
	if (strings.Contains(event.Message, "StorageClass") || strings.Contains(event.Message, "storageclass")) &&
		(strings.Contains(event.Message, "PersistentVolume") || strings.Contains(event.Message, "volume")) {
		return true
	}
	
	// Exclude common non-CSI events
	excludePatterns := []string{
		"kube-api-access-",
		"default-token-",
		"configmap",
		"secret",
		"serviceaccount",
	}
	
	for _, pattern := range excludePatterns {
		if strings.Contains(event.Message, pattern) {
			return false
		}
	}
	
	return false
}

// GetRecentEvents returns recent events that might be relevant to CSI mount issues
func (d *EventsDetector) GetRecentEvents(ctx context.Context, maxResults int) ([]types.EventInfo, error) {
	events, err := d.client.CoreV1().Events("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list events: %w", err)
	}

	var relevantEvents []types.EventInfo
	cutoffTime := time.Now().Add(-d.lookbackDuration)

	for _, event := range events.Items {
		// Skip old events
		eventTime := event.LastTimestamp.Time
		if eventTime.IsZero() {
			eventTime = event.EventTime.Time
		}
		if eventTime.Before(cutoffTime) {
			continue
		}

		// Filter for volume-related events
		if d.eventMatchesDriver(event, d.targetDriver) || d.isVolumeRelatedEvent(event) {
			relevantEvents = append(relevantEvents, types.EventInfo{
				Type:      event.Type,
				Reason:    event.Reason,
				Message:   event.Message,
				Object:    fmt.Sprintf("%s/%s", event.InvolvedObject.Kind, event.InvolvedObject.Name),
				Namespace: event.Namespace,
				Time:      metav1.Time{Time: eventTime},
			})

			if len(relevantEvents) >= maxResults {
				break
			}
		}
	}

	return relevantEvents, nil
}

// buildEventMetadata creates comprehensive metadata from Kubernetes event
func (d *EventsDetector) buildEventMetadata(event corev1.Event, eventTime time.Time) map[string]string {
	metadata := map[string]string{
		// Full event message - this is what the user specifically requested
		"full_event_message": event.Message,
		
		// Event details
		"event_reason":       event.Reason,
		"event_type":         event.Type,
		"event_time":         eventTime.Format(time.RFC3339),
		"count":              fmt.Sprintf("%d", event.Count),
		
		// Involved object details
		"involved_object_kind":       event.InvolvedObject.Kind,
		"involved_object_name":       event.InvolvedObject.Name,
		"involved_object_namespace":  event.InvolvedObject.Namespace,
		"involved_object_uid":        string(event.InvolvedObject.UID),
		"involved_object":            fmt.Sprintf("%s/%s", event.InvolvedObject.Kind, event.InvolvedObject.Name),
		
		// Event metadata
		"event_namespace":     event.Namespace,
		"source_component":    event.Source.Component,
		"source_host":         event.Source.Host,
		"reporting_controller": event.ReportingController,
		"reporting_instance":   event.ReportingInstance,
	}

	// Add resource version if available
	if event.InvolvedObject.ResourceVersion != "" {
		metadata["involved_object_resource_version"] = event.InvolvedObject.ResourceVersion
	}

	// Add API version if available
	if event.InvolvedObject.APIVersion != "" {
		metadata["involved_object_api_version"] = event.InvolvedObject.APIVersion
	}

	// Add field path if available (useful for pod-specific events)
	if event.InvolvedObject.FieldPath != "" {
		metadata["involved_object_field_path"] = event.InvolvedObject.FieldPath
	}

	// For pod events, extract additional pod-specific metadata
	if event.InvolvedObject.Kind == "Pod" {
		metadata["pod_name"] = event.InvolvedObject.Name
		metadata["pod_namespace"] = event.InvolvedObject.Namespace
		if event.InvolvedObject.Namespace == "" {
			metadata["pod_namespace"] = event.Namespace
		}
	}

	// For PVC events, extract PVC-specific metadata
	if event.InvolvedObject.Kind == "PersistentVolumeClaim" {
		metadata["pvc_name"] = event.InvolvedObject.Name
		metadata["pvc_namespace"] = event.InvolvedObject.Namespace
		if event.InvolvedObject.Namespace == "" {
			metadata["pvc_namespace"] = event.Namespace
		}
	}

	// For Node events, extract node-specific metadata
	if event.InvolvedObject.Kind == "Node" {
		metadata["node_name"] = event.InvolvedObject.Name
	}

	// Try to extract additional volume-specific information from the message
	if volumeHandle := d.extractVolumeFromMessage(event.Message); volumeHandle != "unknown" {
		metadata["extracted_volume_handle"] = volumeHandle
	}

	if driver := d.extractDriverFromMessage(event.Message); driver != "unknown" {
		metadata["extracted_csi_driver"] = driver
	}

	return metadata
}

// isVolumeRelatedEvent checks if an event is related to volume operations
func (d *EventsDetector) isVolumeRelatedEvent(event corev1.Event) bool {
	volumeKeywords := []string{
		"Volume", "Mount", "Attach", "PVC", "PV", "CSI",
		"volume", "mount", "attach", "pvc", "pv", "csi",
	}

	for _, keyword := range volumeKeywords {
		if strings.Contains(event.Reason, keyword) || strings.Contains(event.Message, keyword) {
			return true
		}
	}

	return false
}