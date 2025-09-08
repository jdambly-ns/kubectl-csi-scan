package detect

import (
	"context"
	"fmt"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/jdambly/kubectl-csi-scan/pkg/client"
	"github.com/jdambly/kubectl-csi-scan/pkg/types"
)

// Detector coordinates multiple detection methods
type Detector struct {
	client                 client.KubernetesClient
	volumeAttachmentDetector *VolumeAttachmentDetector
	crossNodePVCDetector     *CrossNodePVCDetector
	eventsDetector          *EventsDetector
	metricsDetector         *MetricsDetector
	options                 types.DetectionOptions
}

// NewDetector creates a new multi-method detector
func NewDetector(kubeClient client.KubernetesClient, options types.DetectionOptions) *Detector {
	detector := &Detector{
		client:  kubeClient,
		options: options,
	}

	// Initialize detection methods based on options
	for _, method := range options.Methods {
		switch method {
		case types.VolumeAttachmentMethod:
			detector.volumeAttachmentDetector = NewVolumeAttachmentDetector(kubeClient, options.TargetDriver)
		case types.CrossNodePVCMethod:
			detector.crossNodePVCDetector = NewCrossNodePVCDetector(kubeClient, options.TargetDriver)
		case types.EventsMethod:
			detector.eventsDetector = NewEventsDetector(kubeClient, options.TargetDriver, 1*time.Hour)
		case types.MetricsMethod:
			detector.metricsDetector = NewMetricsDetector("", options.TargetDriver) // Prometheus URL would be configured
		}
	}

	return detector
}

// DetectAll runs all configured detection methods and returns consolidated results
func (d *Detector) DetectAll(ctx context.Context) (*types.DetectionResult, error) {
	var allIssues []types.CSIMountIssue
	var methodsUsed []types.DetectionMethod

	// Run VolumeAttachment detection
	if d.volumeAttachmentDetector != nil {
		issues, err := d.volumeAttachmentDetector.Detect(ctx)
		if err != nil {
			return nil, fmt.Errorf("VolumeAttachment detection failed: %w", err)
		}
		allIssues = append(allIssues, issues...)
		methodsUsed = append(methodsUsed, types.VolumeAttachmentMethod)
	}

	// Run cross-node PVC detection
	if d.crossNodePVCDetector != nil {
		issues, err := d.crossNodePVCDetector.Detect(ctx)
		if err != nil {
			return nil, fmt.Errorf("cross-node PVC detection failed: %w", err)
		}
		allIssues = append(allIssues, issues...)
		methodsUsed = append(methodsUsed, types.CrossNodePVCMethod)
	}

	// Run events detection
	if d.eventsDetector != nil {
		issues, err := d.eventsDetector.Detect(ctx)
		if err != nil {
			return nil, fmt.Errorf("events detection failed: %w", err)
		}
		allIssues = append(allIssues, issues...)
		methodsUsed = append(methodsUsed, types.EventsMethod)
	}

	// Run metrics detection
	if d.metricsDetector != nil {
		issues, err := d.metricsDetector.Detect(ctx)
		if err != nil {
			return nil, fmt.Errorf("metrics detection failed: %w", err)
		}
		allIssues = append(allIssues, issues...)
		methodsUsed = append(methodsUsed, types.MetricsMethod)
	}

	// Filter by minimum severity
	filteredIssues := d.filterBySeverity(allIssues, d.options.MinSeverity)

	// Generate summary
	summary := d.generateSummary(filteredIssues, methodsUsed)

	// Generate recommendations if requested
	var recommendations []string
	if d.options.RecommendCleanup {
		recommendations = d.generateRecommendations(filteredIssues)
	}

	return &types.DetectionResult{
		Summary:         summary,
		Issues:          filteredIssues,
		Recommendations: recommendations,
		GeneratedAt:     time.Now(),
	}, nil
}

// filterBySeverity filters issues based on minimum severity level
func (d *Detector) filterBySeverity(issues []types.CSIMountIssue, minSeverity types.IssueSeverity) []types.CSIMountIssue {
	if minSeverity == "" {
		return issues
	}

	severityOrder := map[types.IssueSeverity]int{
		types.SeverityLow:      1,
		types.SeverityMedium:   2,
		types.SeverityHigh:     3,
		types.SeverityCritical: 4,
	}

	minLevel := severityOrder[minSeverity]
	var filtered []types.CSIMountIssue

	for _, issue := range issues {
		if severityOrder[issue.Severity] >= minLevel {
			filtered = append(filtered, issue)
		}
	}

	return filtered
}

// generateSummary creates a summary of detected issues
func (d *Detector) generateSummary(issues []types.CSIMountIssue, methodsUsed []types.DetectionMethod) types.DetectionSummary {
	summary := types.DetectionSummary{
		TotalIssues:      len(issues),
		IssuesBySeverity: make(map[types.IssueSeverity]int),
		IssuesByType:     make(map[types.IssueType]int),
		MethodsUsed:      methodsUsed,
	}

	nodeSet := make(map[string]bool)
	driverSet := make(map[string]bool)

	for _, issue := range issues {
		// Count by severity
		summary.IssuesBySeverity[issue.Severity]++

		// Count by type
		summary.IssuesByType[issue.Type]++

		// Track affected nodes
		if issue.Node != "" {
			nodeSet[issue.Node] = true
		}

		// Track affected drivers
		if issue.Driver != "" {
			driverSet[issue.Driver] = true
		}
	}

	// Convert sets to slices
	for node := range nodeSet {
		summary.AffectedNodes = append(summary.AffectedNodes, node)
	}
	for driver := range driverSet {
		summary.AffectedDrivers = append(summary.AffectedDrivers, driver)
	}

	// Sort for consistent output
	sort.Strings(summary.AffectedNodes)
	sort.Strings(summary.AffectedDrivers)

	return summary
}

// generateRecommendations creates cleanup and remediation recommendations
func (d *Detector) generateRecommendations(issues []types.CSIMountIssue) []string {
	var recommendations []string

	// Track types of issues found
	hasVolumeAttachmentConflicts := false
	hasMultipleAttachments := false
	hasStuckMountReferences := false
	hasCSIOperationFailures := false

	affectedNodes := make(map[string]bool)
	affectedDrivers := make(map[string]bool)

	for _, issue := range issues {
		switch issue.Type {
		case types.VolumeAttachmentConflict:
			hasVolumeAttachmentConflicts = true
		case types.MultipleAttachments:
			hasMultipleAttachments = true
		case types.StuckMountReference:
			hasStuckMountReferences = true
		case types.CSIOperationFailure:
			hasCSIOperationFailures = true
		}

		if issue.Node != "" {
			affectedNodes[issue.Node] = true
		}
		if issue.Driver != "" {
			affectedDrivers[issue.Driver] = true
		}
	}

	// General recommendations
	recommendations = append(recommendations, "## Immediate Actions")

	if hasVolumeAttachmentConflicts || hasMultipleAttachments {
		recommendations = append(recommendations,
			"1. **Check VolumeAttachment objects**: kubectl get volumeattachments -o wide",
			"2. **Identify conflicting attachments**: Look for volumes attached to multiple nodes",
			"3. **Force detach if safe**: Delete stuck VolumeAttachment objects for volumes not in use",
		)
	}

	if hasStuckMountReferences {
		recommendations = append(recommendations,
			"4. **Check mount references on affected nodes**:",
			"   - Run: mount | grep csi",
			"   - Look for multiple mount points to same volume",
			"   - Safely unmount unused references: umount <path>",
		)
	}

	if hasCSIOperationFailures {
		recommendations = append(recommendations,
			"5. **Review CSI driver logs**:",
			"   - Check kubelet logs: journalctl -u kubelet",
			"   - Check CSI driver pods: kubectl logs -n kube-system <csi-pod>",
		)
	}

	// Node-specific recommendations
	if len(affectedNodes) > 0 {
		recommendations = append(recommendations, "\n## Affected Nodes")
		recommendations = append(recommendations, fmt.Sprintf("Priority nodes for cleanup: %v", getSortedKeys(affectedNodes)))
		
		if len(affectedNodes) > 10 {
			recommendations = append(recommendations, "⚠️  **High Impact**: More than 10 nodes affected - consider automated cleanup")
		}
	}

	// Driver-specific recommendations
	if len(affectedDrivers) > 0 {
		recommendations = append(recommendations, "\n## Driver-Specific Actions")
		for driver := range affectedDrivers {
			switch driver {
			case "cinder.csi.openstack.org":
				recommendations = append(recommendations,
					fmt.Sprintf("**%s**:", driver),
					"- Consider upgrading cinder CSI driver to latest version",
					"- Check OpenStack Cinder service health",
					"- Review volume attachment limits in OpenStack",
				)
			case "rook-ceph.rbd.csi.ceph.com", "rook-ceph.cephfs.csi.ceph.com":
				recommendations = append(recommendations,
					fmt.Sprintf("**%s**:", driver),
					"- Check Ceph cluster health: kubectl -n rook-ceph exec -it deploy/rook-ceph-tools -- ceph status",
					"- Review Rook operator logs",
					"- Verify network connectivity to Ceph cluster",
				)
			default:
				recommendations = append(recommendations,
					fmt.Sprintf("**%s**:", driver),
					"- Check CSI driver pods are healthy",
					"- Review driver-specific documentation for troubleshooting",
				)
			}
		}
	}

	// Long-term recommendations
	recommendations = append(recommendations,
		"\n## Long-term Solutions",
		"1. **Monitoring**: Set up Prometheus alerts for CSI operation failures",
		"2. **Automation**: Deploy automated cleanup scripts for recurring issues",
		"3. **Upgrades**: Keep CSI drivers updated to latest stable versions",
		"4. **Documentation**: Document cleanup procedures for operations team",
	)

	// Safety warnings
	recommendations = append(recommendations,
		"\n## ⚠️  Safety Warnings",
		"- **Always verify pods are not using volumes before force detaching**",
		"- **Test cleanup procedures in non-production first**",
		"- **Backup important data before making changes**",
		"- **Coordinate with application teams before cleanup**",
	)

	return recommendations
}

// getSortedKeys returns sorted slice of map keys
func getSortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// GetDetailedAnalysis provides additional detailed analysis for debugging
func (d *Detector) GetDetailedAnalysis(ctx context.Context) (*DetailedAnalysis, error) {
	analysis := &DetailedAnalysis{}

	// Get VolumeAttachment details if available
	if d.volumeAttachmentDetector != nil {
		vas, err := d.client.StorageV1().VolumeAttachments().List(ctx, metav1.ListOptions{})
		if err == nil {
			analysis.VolumeAttachmentCount = len(vas.Items)
			for _, va := range vas.Items {
				if va.Status.Attached {
					analysis.AttachedVolumeCount++
				}
				if va.Status.AttachError != nil || va.Status.DetachError != nil {
					analysis.VolumeAttachmentErrors++
				}
			}
		}
	}

	// Get node PVC usage if available
	if d.crossNodePVCDetector != nil {
		nodeUsage, err := d.crossNodePVCDetector.GetNodePVCUsage(ctx)
		if err == nil {
			analysis.NodePVCUsage = nodeUsage
		}
	}

	// Get recent events if available
	if d.eventsDetector != nil {
		events, err := d.eventsDetector.GetRecentEvents(ctx, 50)
		if err == nil {
			analysis.RecentEvents = events
		}
	}

	// Get metric queries if available
	if d.metricsDetector != nil {
		analysis.MetricQueries = d.metricsDetector.GetMetricQueries()
		analysis.RecommendedAlerts = d.metricsDetector.GetRecommendedAlerts()
	}

	return analysis, nil
}

// DetailedAnalysis contains additional analysis information
type DetailedAnalysis struct {
	VolumeAttachmentCount  int                      `json:"volumeAttachmentCount"`
	AttachedVolumeCount    int                      `json:"attachedVolumeCount"`
	VolumeAttachmentErrors int                      `json:"volumeAttachmentErrors"`
	NodePVCUsage          []types.NodePVCUsage     `json:"nodePVCUsage"`
	RecentEvents          []types.EventInfo        `json:"recentEvents"`
	MetricQueries         []types.MetricQuery      `json:"metricQueries"`
	RecommendedAlerts     []string                 `json:"recommendedAlerts"`
}