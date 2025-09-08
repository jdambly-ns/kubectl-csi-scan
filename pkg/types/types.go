package types

import (
	"time"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DetectionMethod represents the different detection approaches
type DetectionMethod string

const (
	VolumeAttachmentMethod DetectionMethod = "volumeattachments"
	CrossNodePVCMethod     DetectionMethod = "cross-node-pvc"
	EventsMethod          DetectionMethod = "events"
	MetricsMethod         DetectionMethod = "metrics"
)

// CSIMountIssue represents a detected CSI mount problem
type CSIMountIssue struct {
	Type          IssueType     `json:"type"`
	Severity      IssueSeverity `json:"severity"`
	Node          string        `json:"node"`
	Volume        string        `json:"volume,omitempty"`
	PVC           string        `json:"pvc,omitempty"`
	Namespace     string        `json:"namespace,omitempty"`
	Driver        string        `json:"driver,omitempty"`
	Description   string        `json:"description"`
	DetectedBy    DetectionMethod `json:"detectedBy"`
	DetectedAt    time.Time     `json:"detectedAt"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// IssueType categorizes the type of mount issue
type IssueType string

const (
	VolumeAttachmentConflict IssueType = "volume-attachment-conflict"
	StuckVolumeAttachment   IssueType = "stuck-volume-attachment"
	StuckVolumeDetachment   IssueType = "stuck-volume-detachment"
	MultipleAttachments     IssueType = "multiple-attachments"
	MultiAttachError        IssueType = "multi-attach-error"
	FailedAttachVolume      IssueType = "failed-attach-volume"
	StuckMountReference     IssueType = "stuck-mount-reference"
	CSIOperationFailure     IssueType = "csi-operation-failure"
)

// IssueSeverity indicates the impact level
type IssueSeverity string

const (
	SeverityCritical IssueSeverity = "critical" // 5+ conflicts or blocking critical services
	SeverityHigh     IssueSeverity = "high"     // 3-4 conflicts or widespread impact
	SeverityMedium   IssueSeverity = "medium"   // 2 conflicts or limited impact
	SeverityLow      IssueSeverity = "low"      // 1 conflict or isolated issue
)

// VolumeAttachmentInfo contains details about volume attachment conflicts
type VolumeAttachmentInfo struct {
	Name           string            `json:"name"`
	Node           string            `json:"node"`
	VolumeHandle   string            `json:"volumeHandle"`
	Driver         string            `json:"driver"`
	Attached       bool              `json:"attached"`
	AttachError    string            `json:"attachError,omitempty"`
	DetachError    string            `json:"detachError,omitempty"`
	LastTransition metav1.Time       `json:"lastTransition"`
}

// NodePVCUsage tracks PVC usage across nodes
type NodePVCUsage struct {
	Node      string            `json:"node"`
	PVCCounts map[string]int    `json:"pvcCounts"` // pvc -> count
	Total     int               `json:"total"`
}

// EventInfo represents relevant Kubernetes events
type EventInfo struct {
	Type      string      `json:"type"`
	Reason    string      `json:"reason"`
	Message   string      `json:"message"`
	Object    string      `json:"object"`
	Namespace string      `json:"namespace"`
	Time      metav1.Time `json:"time"`
}

// MetricQuery represents a Prometheus query for CSI issues
type MetricQuery struct {
	Name        string `json:"name"`
	Query       string `json:"query"`
	Description string `json:"description"`
}

// DetectionOptions configures the detection process
type DetectionOptions struct {
	Methods        []DetectionMethod `json:"methods"`
	TargetDriver   string           `json:"targetDriver,omitempty"`
	OutputFormat   string           `json:"outputFormat"`  // json, yaml, table, detailed
	RecommendCleanup bool           `json:"recommendCleanup"`
	MinSeverity    IssueSeverity    `json:"minSeverity"`
}

// DetectionResult contains all findings from the detection process
type DetectionResult struct {
	Summary       DetectionSummary  `json:"summary"`
	Issues        []CSIMountIssue   `json:"issues"`
	Recommendations []string        `json:"recommendations,omitempty"`
	GeneratedAt   time.Time         `json:"generatedAt"`
}

// DetectionSummary provides high-level statistics
type DetectionSummary struct {
	TotalIssues      int                        `json:"totalIssues"`
	IssuesBySeverity map[IssueSeverity]int      `json:"issuesBySeverity"`
	IssuesByType     map[IssueType]int          `json:"issuesByType"`
	AffectedNodes    []string                   `json:"affectedNodes"`
	AffectedDrivers  []string                   `json:"affectedDrivers"`
	MethodsUsed      []DetectionMethod          `json:"methodsUsed"`
}