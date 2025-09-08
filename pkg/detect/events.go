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
			Volume:      d.extractVolumeFromMessage(event.Message),
			Namespace:   event.Namespace,
			Driver:      d.extractDriverFromMessage(event.Message),
			Description: fmt.Sprintf("Multi-Attach error detected: %s", event.Message),
			DetectedBy:  types.EventsMethod,
			DetectedAt:  time.Now(),
			Metadata: map[string]string{
				"event_reason":    event.Reason,
				"event_type":      event.Type,
				"involved_object": fmt.Sprintf("%s/%s", event.InvolvedObject.Kind, event.InvolvedObject.Name),
				"event_time":      eventTime.Format(time.RFC3339),
				"source_component": event.Source.Component,
				"count":           fmt.Sprintf("%d", event.Count),
			},
		}
	}

	// Failed attach volume errors
	if event.Reason == "FailedAttachVolume" && event.Type == "Warning" {
		return &types.CSIMountIssue{
			Type:        types.FailedAttachVolume,
			Severity:    d.calculateEventSeverity(event),
			Volume:      d.extractVolumeFromMessage(event.Message),
			Namespace:   event.Namespace,
			Driver:      d.extractDriverFromMessage(event.Message),
			Description: fmt.Sprintf("Failed to attach volume: %s", event.Message),
			DetectedBy:  types.EventsMethod,
			DetectedAt:  time.Now(),
			Metadata: map[string]string{
				"event_reason":     event.Reason,
				"event_type":       event.Type,
				"involved_object":  fmt.Sprintf("%s/%s", event.InvolvedObject.Kind, event.InvolvedObject.Name),
				"event_time":       eventTime.Format(time.RFC3339),
				"source_component": event.Source.Component,
				"count":            fmt.Sprintf("%d", event.Count),
			},
		}
	}

	// Failed mount errors
	if event.Reason == "FailedMount" && event.Type == "Warning" {
		// Check for GetDeviceMountRefs related errors
		if strings.Contains(event.Message, "GetDeviceMountRefs") {
			return &types.CSIMountIssue{
				Type:        types.StuckMountReference,
				Severity:    d.calculateEventSeverity(event),
				Volume:      d.extractVolumeFromMessage(event.Message),
				Namespace:   event.Namespace,
				Driver:      d.extractDriverFromMessage(event.Message),
				Description: fmt.Sprintf("Mount reference cleanup failure: %s", event.Message),
				DetectedBy:  types.EventsMethod,
				DetectedAt:  time.Now(),
				Metadata: map[string]string{
					"event_reason":     event.Reason,
					"event_type":       event.Type,
					"involved_object":  fmt.Sprintf("%s/%s", event.InvolvedObject.Kind, event.InvolvedObject.Name),
					"event_time":       eventTime.Format(time.RFC3339),
					"source_component": event.Source.Component,
					"count":            fmt.Sprintf("%d", event.Count),
				},
			}
		}

		return &types.CSIMountIssue{
			Type:        types.CSIOperationFailure,
			Severity:    d.calculateEventSeverity(event),
			Volume:      d.extractVolumeFromMessage(event.Message),
			Namespace:   event.Namespace,
			Driver:      d.extractDriverFromMessage(event.Message),
			Description: fmt.Sprintf("Failed to mount volume: %s", event.Message),
			DetectedBy:  types.EventsMethod,
			DetectedAt:  time.Now(),
			Metadata: map[string]string{
				"event_reason":     event.Reason,
				"event_type":       event.Type,
				"involved_object":  fmt.Sprintf("%s/%s", event.InvolvedObject.Kind, event.InvolvedObject.Name),
				"event_time":       eventTime.Format(time.RFC3339),
				"source_component": event.Source.Component,
				"count":            fmt.Sprintf("%d", event.Count),
			},
		}
	}

	// Other CSI-related warning events
	if event.Type == "Warning" && (strings.Contains(event.Message, "CSI") || strings.Contains(event.Reason, "Volume")) {
		return &types.CSIMountIssue{
			Type:        types.CSIOperationFailure,
			Severity:    d.calculateEventSeverity(event),
			Volume:      d.extractVolumeFromMessage(event.Message),
			Namespace:   event.Namespace,
			Driver:      d.extractDriverFromMessage(event.Message),
			Description: fmt.Sprintf("CSI operation issue: %s", event.Message),
			DetectedBy:  types.EventsMethod,
			DetectedAt:  time.Now(),
			Metadata: map[string]string{
				"event_reason":     event.Reason,
				"event_type":       event.Type,
				"involved_object":  fmt.Sprintf("%s/%s", event.InvolvedObject.Kind, event.InvolvedObject.Name),
				"event_time":       eventTime.Format(time.RFC3339),
				"source_component": event.Source.Component,
				"count":            fmt.Sprintf("%d", event.Count),
			},
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
	// Look for PVC patterns
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

	// Look for volume handle patterns
	if strings.Contains(message, "volume") && strings.Contains(message, "\"") {
		start := strings.Index(message, "volume \"")
		if start != -1 {
			start += 8 // len("volume \"")
			end := strings.Index(message[start:], "\"")
			if end != -1 {
				return message[start : start+end]
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