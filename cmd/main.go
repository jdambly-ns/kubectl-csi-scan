package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"

	"github.com/jdambly/kubectl-csi-scan/pkg/client"
	"github.com/jdambly/kubectl-csi-scan/pkg/detect"
	"github.com/jdambly/kubectl-csi-scan/pkg/types"
)

var (
	configFlags = genericclioptions.NewConfigFlags(true)
)

func main() {
	// Configure structured logging
	zerolog.TimeFieldFormat = time.RFC3339
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	
	// Pretty console output for development
	if os.Getenv("LOG_FORMAT") != "json" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}
	
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		// CLI error messages to stderr are appropriate
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kubectl-csi_mount_detective",
		Short: "Detect and analyze CSI mount cleanup issues in Kubernetes clusters",
		Long: `A kubectl plugin to detect CSI mount cleanup issues using multiple detection methods:
- VolumeAttachment API inspection for stuck attachments
- Cross-node PVC usage analysis for multi-attach issues  
- Kubernetes events monitoring for mount failures
- Prometheus metrics queries for operation failures

This tool was developed to address production issues where CSI volumes get stuck
in attached state, preventing proper pod scheduling and volume cleanup.`,
		SilenceUsage: true,
	}

	// Add global flags
	configFlags.AddFlags(cmd.PersistentFlags())

	// Add subcommands
	cmd.AddCommand(newDetectCmd())
	cmd.AddCommand(newAnalyzeCmd())
	cmd.AddCommand(newMetricsCmd())

	return cmd
}

func newDetectCmd() *cobra.Command {
	var (
		methods         []string
		targetDriver    string
		outputFormat    string
		recommendCleanup bool
		minSeverity     string
	)

	cmd := &cobra.Command{
		Use:   "detect",
		Short: "Detect CSI mount issues using specified methods",
		Long: `Detect CSI mount cleanup issues using one or more detection methods.

Available methods:
- volumeattachments: Check VolumeAttachment API objects for conflicts
- cross-node-pvc: Analyze PVC usage across multiple nodes  
- events: Monitor Kubernetes events for mount failures
- metrics: Query Prometheus metrics for operation failures

Examples:
  # Detect all issues using all methods
  kubectl csi-mount-detective detect

  # Use specific detection method
  kubectl csi-mount-detective detect --method=volumeattachments

  # Target specific CSI driver
  kubectl csi-mount-detective detect --driver=cinder.csi.openstack.org

  # Get cleanup recommendations
  kubectl csi-mount-detective detect --recommend-cleanup

  # Filter by severity level
  kubectl csi-mount-detective detect --min-severity=high`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDetect(methods, targetDriver, outputFormat, recommendCleanup, minSeverity)
		},
	}

	cmd.Flags().StringSliceVar(&methods, "method", []string{"volumeattachments", "cross-node-pvc", "events"}, 
		"Detection methods to use (volumeattachments,cross-node-pvc,events,metrics)")
	cmd.Flags().StringVar(&targetDriver, "driver", "", 
		"Target CSI driver to analyze (e.g., cinder.csi.openstack.org)")
	cmd.Flags().StringVar(&outputFormat, "output", "table", 
		"Output format (table,json,yaml,detailed)")
	cmd.Flags().BoolVar(&recommendCleanup, "recommend-cleanup", false, 
		"Generate cleanup recommendations")
	cmd.Flags().StringVar(&minSeverity, "min-severity", "", 
		"Minimum severity level to report (low,medium,high,critical)")

	return cmd
}

func newAnalyzeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Perform detailed analysis of cluster state",
		Long: `Perform detailed analysis of cluster state including:
- VolumeAttachment statistics
- Node PVC usage patterns
- Recent relevant events
- Recommended Prometheus queries

This provides deeper insights for troubleshooting and monitoring setup.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAnalyze()
		},
	}

	return cmd
}

func newMetricsCmd() *cobra.Command {
	var (
		generateAlerts    bool
		generateDashboard bool
		outputFile        string
	)

	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Generate Prometheus queries and alerts for CSI monitoring",
		Long: `Generate Prometheus queries, alerting rules, and Grafana dashboards
for monitoring CSI mount issues.

This helps set up proactive monitoring to detect issues before they impact applications.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMetrics(generateAlerts, generateDashboard, outputFile)
		},
	}

	cmd.Flags().BoolVar(&generateAlerts, "generate-alerts", false, 
		"Generate Prometheus alerting rules")
	cmd.Flags().BoolVar(&generateDashboard, "generate-dashboard", false, 
		"Generate Grafana dashboard JSON")
	cmd.Flags().StringVar(&outputFile, "output-file", "", 
		"Write output to file instead of stdout")

	return cmd
}

func runDetect(methods []string, targetDriver, outputFormat string, recommendCleanup bool, minSeverity string) error {
	// Validate input parameters
	if err := validateDetectFlags(methods, outputFormat, minSeverity); err != nil {
		return err
	}

	log.Info().
		Strs("methods", methods).
		Str("driver", targetDriver).
		Str("format", outputFormat).
		Bool("recommend_cleanup", recommendCleanup).
		Str("min_severity", minSeverity).
		Msg("starting detection process")

	// Build Kubernetes client
	kubeClient, err := buildKubernetesClient()
	if err != nil {
		log.Error().Err(err).Msg("failed to build Kubernetes client")
		return newClientError(err)
	}

	// Parse detection methods
	var detectionMethods []types.DetectionMethod
	for _, method := range methods {
		switch method {
		case "volumeattachments":
			detectionMethods = append(detectionMethods, types.VolumeAttachmentMethod)
		case "cross-node-pvc":
			detectionMethods = append(detectionMethods, types.CrossNodePVCMethod)
		case "events":
			detectionMethods = append(detectionMethods, types.EventsMethod)
		case "metrics":
			detectionMethods = append(detectionMethods, types.MetricsMethod)
		default:
			return fmt.Errorf("unknown detection method: %s", method)
		}
	}

	// Parse minimum severity
	var minSev types.IssueSeverity
	if minSeverity != "" {
		switch strings.ToLower(minSeverity) {
		case "low":
			minSev = types.SeverityLow
		case "medium":
			minSev = types.SeverityMedium
		case "high":
			minSev = types.SeverityHigh
		case "critical":
			minSev = types.SeverityCritical
		default:
			return fmt.Errorf("unknown severity level: %s", minSeverity)
		}
	}

	// Create detector
	options := types.DetectionOptions{
		Methods:          detectionMethods,
		TargetDriver:     targetDriver,
		OutputFormat:     outputFormat,
		RecommendCleanup: recommendCleanup,
		MinSeverity:      minSev,
	}

	detector := detect.NewDetector(client.NewClient(kubeClient), options)

	// Add progress feedback
	fmt.Fprintf(os.Stderr, "Analyzing cluster state using %d detection methods...\n", len(detectionMethods))
	
	// Run detection with improved context handling
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := detector.DetectAll(ctx)
	if err != nil {
		log.Error().Err(err).Msg("detection process failed")
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("detection timed out after 2 minutes - try reducing scope with --driver flag or --method selection")
		}
		return newDetectionError("general", err)
	}

	log.Info().
		Int("issues_found", len(result.Issues)).
		Msg("detection completed successfully")

	// Add success feedback
	if len(result.Issues) == 0 {
		fmt.Fprintf(os.Stderr, "✅ No CSI mount issues detected\n")
	} else {
		fmt.Fprintf(os.Stderr, "⚠️  Found %d issues\n", len(result.Issues))
	}

	// Output results
	return outputResult(result, outputFormat)
}

func runAnalyze() error {
	kubeClient, err := buildKubernetesClient()
	if err != nil {
		return fmt.Errorf("failed to build Kubernetes client: %w", err)
	}

	// Create detector with all methods
	options := types.DetectionOptions{
		Methods: []types.DetectionMethod{
			types.VolumeAttachmentMethod,
			types.CrossNodePVCMethod,
			types.EventsMethod,
			types.MetricsMethod,
		},
	}

	detector := detect.NewDetector(client.NewClient(kubeClient), options)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	analysis, err := detector.GetDetailedAnalysis(ctx)
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}

	// Output detailed analysis
	data, err := json.MarshalIndent(analysis, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal analysis: %w", err)
	}

	fmt.Println(string(data))
	return nil
}

func runMetrics(generateAlerts, generateDashboard bool, outputFile string) error {
	metricsDetector := detect.NewMetricsDetector("", "")

	var output strings.Builder

	if generateAlerts {
		output.WriteString("# Prometheus Alerting Rules for CSI Mount Issues\n")
		output.WriteString("groups:\n")
		output.WriteString("- name: csi-mount-detective\n")
		output.WriteString("  rules:\n")
		
		alerts := metricsDetector.GetRecommendedAlerts()
		for _, alert := range alerts {
			output.WriteString(alert)
			output.WriteString("\n")
		}
	}

	if generateDashboard {
		if generateAlerts {
			output.WriteString("\n---\n\n")
		}
		output.WriteString("# Grafana Dashboard JSON\n")
		output.WriteString(metricsDetector.GenerateGrafanaDashboard())
		output.WriteString("\n")
	}

	if !generateAlerts && !generateDashboard {
		// Default: show metric queries
		output.WriteString("# Prometheus Queries for CSI Mount Detection\n\n")
		queries := metricsDetector.GetMetricQueries()
		for _, query := range queries {
			output.WriteString(fmt.Sprintf("## %s\n", query.Name))
			output.WriteString(fmt.Sprintf("# %s\n", query.Description))
			output.WriteString(fmt.Sprintf("%s\n\n", query.Query))
		}
	}

	result := output.String()

	if outputFile != "" {
		return os.WriteFile(outputFile, []byte(result), 0644)
	}

	fmt.Print(result)
	return nil
}

func buildKubernetesClient() (kubernetes.Interface, error) {
	config, err := configFlags.ToRESTConfig()
	if err != nil {
		return nil, err
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func outputResult(result *types.DetectionResult, format string) error {
	switch format {
	case "json":
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))

	case "table":
		return outputTable(result)

	case "detailed":
		return outputDetailed(result)

	default:
		return fmt.Errorf("unknown output format: %s", format)
	}

	return nil
}

func outputTable(result *types.DetectionResult) error {
	// Simple output with full names - no truncation
	if len(result.Issues) == 0 {
		fmt.Printf("No CSI mount issues detected\n")
		return nil
	}

	fmt.Printf("Total Issues: %d\n\n", result.Summary.TotalIssues)

	// Affected Nodes
	if len(result.Summary.AffectedNodes) > 0 {
		fmt.Printf("AFFECTED NODES:\n")
		for _, node := range result.Summary.AffectedNodes {
			fmt.Printf("  %s\n", node)
		}
		fmt.Printf("\n")
	}

	// Group issues by detection method for clearer output
	volumeAttachmentIssues := []types.CSIMountIssue{}
	crossNodePVCIssues := []types.CSIMountIssue{}
	eventIssues := []types.CSIMountIssue{}
	otherIssues := []types.CSIMountIssue{}

	for _, issue := range result.Issues {
		switch issue.DetectedBy {
		case types.VolumeAttachmentMethod:
			volumeAttachmentIssues = append(volumeAttachmentIssues, issue)
		case types.CrossNodePVCMethod:
			crossNodePVCIssues = append(crossNodePVCIssues, issue)
		case types.EventsMethod:
			eventIssues = append(eventIssues, issue)
		default:
			otherIssues = append(otherIssues, issue)
		}
	}

	// Volume Attachment Issues (show Node and Volume)
	if len(volumeAttachmentIssues) > 0 {
		fmt.Printf("VOLUME ATTACHMENT ISSUES:\n")
		fmt.Printf("%-20s %s\n", "NODE", "VOLUME")
		fmt.Printf("%-20s %s\n", "----", "------")
		for _, issue := range volumeAttachmentIssues {
			node := issue.Node
			if node == "" {
				node = "-"
			}
			volume := issue.Volume
			if volume == "" {
				volume = "-"
			}
			fmt.Printf("%-20s %s\n", node, volume)
		}
		fmt.Printf("\n")
	}

	// Cross-Node PVC Issues (show PVC and affected nodes from metadata)
	if len(crossNodePVCIssues) > 0 {
		fmt.Printf("CROSS-NODE PVC ISSUES:\n")
		fmt.Printf("%-50s %s\n", "PVC", "AFFECTED NODES")
		fmt.Printf("%-50s %s\n", "---", "--------------")
		for _, issue := range crossNodePVCIssues {
			pvc := issue.PVC
			if pvc == "" {
				pvc = "-"
			}
			
			// Extract nodes from metadata if available
			nodes := "-"
			if nodesStr, exists := issue.Metadata["nodes"]; exists {
				nodes = nodesStr
			} else if issue.Node != "" {
				nodes = issue.Node
			}
			
			fmt.Printf("%-50s %s\n", pvc, nodes)
		}
		fmt.Printf("\n")
	}

	// Event-based Issues
	if len(eventIssues) > 0 {
		fmt.Printf("EVENT-BASED ISSUES:\n")
		fmt.Printf("%-15s %-40s %-15s %-35s %s\n", "NAMESPACE", "OBJECT", "NODE", "VOLUME", "MESSAGE")
		fmt.Printf("%-15s %-40s %-15s %-35s %s\n", "---------", "------", "----", "------", "-------")
		for _, issue := range eventIssues {
			// Extract namespace
			namespace := issue.Namespace
			if namespace == "" {
				namespace = "-"
			}
			
			// Extract involved object from metadata
			object := "-"
			if involvedObject, exists := issue.Metadata["involved_object"]; exists {
				object = involvedObject
			}
			
			// Extract node
			node := issue.Node
			if node == "" {
				node = "-"
			}
			
			// Extract volume
			volume := issue.Volume
			if volume == "" {
				volume = "-"
			}
			
			// Extract the full event message from metadata - no truncation
			message := issue.Description
			if fullMessage, exists := issue.Metadata["full_event_message"]; exists {
				message = fullMessage
			}
			
			fmt.Printf("%-15s %-40s %-15s %-35s %s\n", namespace, object, node, volume, message)
		}
		fmt.Printf("\n")
	}

	// Other Issues
	if len(otherIssues) > 0 {
		fmt.Printf("OTHER ISSUES:\n")
		fmt.Printf("%-20s %-30s %s\n", "NODE", "PVC", "VOLUME")
		fmt.Printf("%-20s %-30s %s\n", "----", "---", "------")
		for _, issue := range otherIssues {
			node := issue.Node
			if node == "" {
				node = "-"
			}
			pvc := issue.PVC
			if pvc == "" {
				pvc = "-"
			}
			volume := issue.Volume
			if volume == "" {
				volume = "-"
			}
			fmt.Printf("%-20s %-30s %s\n", node, pvc, volume)
		}
	}

	return nil
}

func outputDetailed(result *types.DetectionResult) error {
	fmt.Printf("# CSI Mount Detective - Detailed Report\n\n")
	fmt.Printf("**Generated:** %s\n\n", result.GeneratedAt.Format(time.RFC3339))

	// Summary section
	fmt.Printf("## Summary\n\n")
	fmt.Printf("- **Total Issues:** %d\n", result.Summary.TotalIssues)
	fmt.Printf("- **Methods Used:** %v\n", result.Summary.MethodsUsed)
	
	if len(result.Summary.IssuesBySeverity) > 0 {
		fmt.Printf("- **Issues by Severity:**\n")
		for severity, count := range result.Summary.IssuesBySeverity {
			fmt.Printf("  - %s: %d\n", severity, count)
		}
	}

	if len(result.Summary.AffectedNodes) > 0 {
		fmt.Printf("- **Affected Nodes:** %v\n", result.Summary.AffectedNodes)
	}

	if len(result.Summary.AffectedDrivers) > 0 {
		fmt.Printf("- **Affected Drivers:** %v\n", result.Summary.AffectedDrivers)
	}

	// Detailed issues
	if len(result.Issues) > 0 {
		fmt.Printf("\n## Detailed Issues\n\n")
		
		for i, issue := range result.Issues {
			fmt.Printf("### Issue %d: %s\n\n", i+1, issue.Type)
			fmt.Printf("- **Severity:** %s\n", issue.Severity)
			fmt.Printf("- **Description:** %s\n", issue.Description)
			fmt.Printf("- **Detected By:** %s\n", issue.DetectedBy)
			fmt.Printf("- **Detected At:** %s\n", issue.DetectedAt.Format(time.RFC3339))
			
			if issue.Node != "" {
				fmt.Printf("- **Node:** %s\n", issue.Node)
			}
			if issue.Volume != "" {
				fmt.Printf("- **Volume:** %s\n", issue.Volume)
			}
			if issue.PVC != "" {
				fmt.Printf("- **PVC:** %s\n", issue.PVC)
			}
			if issue.Driver != "" {
				fmt.Printf("- **Driver:** %s\n", issue.Driver)
			}
			
			if len(issue.Metadata) > 0 {
				fmt.Printf("- **Metadata:**\n")
				for key, value := range issue.Metadata {
					fmt.Printf("  - %s: %s\n", key, value)
				}
			}
			fmt.Printf("\n")
		}
	}

	// Recommendations
	if len(result.Recommendations) > 0 {
		fmt.Printf("## Recommendations\n\n")
		for _, rec := range result.Recommendations {
			fmt.Printf("%s\n", rec)
		}
	}

	return nil
}

// validateDetectFlags validates input parameters for the detect command
func validateDetectFlags(methods []string, outputFormat, minSeverity string) error {
	// Validate output format
	validFormats := map[string]bool{
		"table": true, "json": true, "yaml": true, "detailed": true,
	}
	if !validFormats[outputFormat] {
		return newValidationError("output format", outputFormat, []string{"table", "json", "yaml", "detailed"})
	}
	
	// Validate methods
	validMethods := map[string]bool{
		"volumeattachments": true, "cross-node-pvc": true, "events": true, "metrics": true,
	}
	for _, method := range methods {
		if !validMethods[method] {
			return newValidationError("detection method", method, []string{"volumeattachments", "cross-node-pvc", "events", "metrics"})
		}
	}
	
	// Validate severity if provided
	if minSeverity != "" {
		validSeverities := map[string]bool{
			"low": true, "medium": true, "high": true, "critical": true,
		}
		if !validSeverities[strings.ToLower(minSeverity)] {
			return newValidationError("severity level", minSeverity, []string{"low", "medium", "high", "critical"})
		}
	}
	
	return nil
}

// newClientError creates a user-friendly error for Kubernetes client issues
func newClientError(err error) error {
	return fmt.Errorf("failed to initialize Kubernetes client - check your kubeconfig and cluster connectivity: %w", err)
}

// newValidationError creates a consistent validation error message
func newValidationError(field, value string, validOptions []string) error {
	return fmt.Errorf("invalid %s '%s' - must be one of: %s", field, value, strings.Join(validOptions, ", "))
}

// newDetectionError creates a user-friendly error for detection failures
func newDetectionError(method string, err error) error {
	return fmt.Errorf("detection method '%s' failed - check cluster permissions and connectivity: %w", method, err)
}