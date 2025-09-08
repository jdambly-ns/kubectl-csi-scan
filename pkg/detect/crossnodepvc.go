package detect

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/jdambly/kubectl-csi-scan/pkg/client"
	"github.com/jdambly/kubectl-csi-scan/pkg/types"
)

// CrossNodePVCDetector implements detection via cross-node PVC usage analysis
type CrossNodePVCDetector struct {
	client       client.KubernetesClient
	targetDriver string
}

// NewCrossNodePVCDetector creates a new cross-node PVC detector
func NewCrossNodePVCDetector(kubeClient client.KubernetesClient, targetDriver string) *CrossNodePVCDetector {
	return &CrossNodePVCDetector{
		client:       kubeClient,
		targetDriver: targetDriver,
	}
}

// Detect finds PVCs that appear to be used across multiple nodes
func (d *CrossNodePVCDetector) Detect(ctx context.Context) ([]types.CSIMountIssue, error) {
	var issues []types.CSIMountIssue

	// Get all pods across all namespaces
	pods, err := d.client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	// Track PVC usage: pvcKey (namespace/name) -> map[nodeName]podCount
	pvcNodeUsage := make(map[string]map[string]int)
	pvcNamespaces := make(map[string]string) // pvcKey -> namespace
	pvcDrivers := make(map[string]string)    // pvcKey -> driver (if determinable)

	for _, pod := range pods.Items {
		if pod.Spec.NodeName == "" {
			continue // Skip unscheduled pods
		}

		// Check each volume in the pod
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim != nil {
				pvcKey := fmt.Sprintf("%s/%s", pod.Namespace, volume.PersistentVolumeClaim.ClaimName)
				pvcNamespaces[pvcKey] = pod.Namespace

				// Initialize maps if needed
				if pvcNodeUsage[pvcKey] == nil {
					pvcNodeUsage[pvcKey] = make(map[string]int)
				}

				// Count usage on this node
				pvcNodeUsage[pvcKey][pod.Spec.NodeName]++

				// Try to determine driver from PVC if we haven't yet
				if _, exists := pvcDrivers[pvcKey]; !exists {
					driver, err := d.getPVCDriver(ctx, pod.Namespace, volume.PersistentVolumeClaim.ClaimName)
					if err == nil && driver != "" {
						pvcDrivers[pvcKey] = driver
					}
				}
			}
		}
	}

	// Analyze usage patterns for potential issues
	for pvcKey, nodeUsage := range pvcNodeUsage {
		// Filter by driver if specified
		if d.targetDriver != "" {
			if driver, exists := pvcDrivers[pvcKey]; exists && !strings.Contains(driver, d.targetDriver) {
				continue
			}
		}

		nodeCount := len(nodeUsage)
		totalUsage := 0
		for _, count := range nodeUsage {
			totalUsage += count
		}

		// Detect suspicious patterns
		if nodeCount > 1 {
			// PVC used on multiple nodes - potential ReadWriteOnce violation
			severity := d.calculateCrossNodeSeverity(nodeCount, totalUsage)
			
			var nodeList []string
			for node, count := range nodeUsage {
				nodeList = append(nodeList, fmt.Sprintf("%s(%d)", node, count))
			}

			issue := types.CSIMountIssue{
				Type:        types.MultipleAttachments,
				Severity:    severity,
				PVC:         pvcKey,
				Namespace:   pvcNamespaces[pvcKey],
				Driver:      pvcDrivers[pvcKey],
				Description: fmt.Sprintf("PVC used on %d nodes: %v (total %d pod references)", nodeCount, nodeList, totalUsage),
				DetectedBy:  types.CrossNodePVCMethod,
				DetectedAt:  time.Now(),
				Metadata: map[string]string{
					"node_count":    fmt.Sprintf("%d", nodeCount),
					"total_usage":   fmt.Sprintf("%d", totalUsage),
					"nodes":         strings.Join(nodeList, ","),
				},
			}
			issues = append(issues, issue)
		} else if totalUsage > 10 {
			// High usage on single node - potential mount leak
			node := ""
			for n := range nodeUsage {
				node = n
				break
			}

			issue := types.CSIMountIssue{
				Type:        types.StuckMountReference,
				Severity:    d.calculateHighUsageSeverity(totalUsage),
				Node:        node,
				PVC:         pvcKey,
				Namespace:   pvcNamespaces[pvcKey],
				Driver:      pvcDrivers[pvcKey],
				Description: fmt.Sprintf("High PVC usage on single node: %d references to %s", totalUsage, pvcKey),
				DetectedBy:  types.CrossNodePVCMethod,
				DetectedAt:  time.Now(),
				Metadata: map[string]string{
					"usage_count": fmt.Sprintf("%d", totalUsage),
					"node":        node,
				},
			}
			issues = append(issues, issue)
		}
	}

	return issues, nil
}

// getPVCDriver attempts to determine the CSI driver for a PVC
func (d *CrossNodePVCDetector) getPVCDriver(ctx context.Context, namespace, pvcName string) (string, error) {
	// Get the PVC
	pvc, err := d.client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	// Get the bound PV if it exists
	if pvc.Spec.VolumeName != "" {
		pv, err := d.client.CoreV1().PersistentVolumes().Get(ctx, pvc.Spec.VolumeName, metav1.GetOptions{})
		if err != nil {
			return "", err
		}

		// Check if it's a CSI volume
		if pv.Spec.CSI != nil {
			return pv.Spec.CSI.Driver, nil
		}
	}

	// Check storage class for CSI provisioner
	if pvc.Spec.StorageClassName != nil {
		sc, err := d.client.StorageV1().StorageClasses().Get(ctx, *pvc.Spec.StorageClassName, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		
		// Storage class provisioner often matches CSI driver name
		return sc.Provisioner, nil
	}

	return "", fmt.Errorf("unable to determine driver for PVC %s/%s", namespace, pvcName)
}

// calculateCrossNodeSeverity determines severity based on cross-node usage
func (d *CrossNodePVCDetector) calculateCrossNodeSeverity(nodeCount, totalUsage int) types.IssueSeverity {
	if nodeCount >= 5 || totalUsage >= 20 {
		return types.SeverityCritical
	} else if nodeCount >= 3 || totalUsage >= 15 {
		return types.SeverityHigh
	} else if nodeCount == 2 || totalUsage >= 10 {
		return types.SeverityMedium
	}
	return types.SeverityLow
}

// calculateHighUsageSeverity determines severity based on usage count on single node
func (d *CrossNodePVCDetector) calculateHighUsageSeverity(usage int) types.IssueSeverity {
	if usage >= 20 {
		return types.SeverityCritical
	} else if usage >= 15 {
		return types.SeverityHigh
	} else if usage >= 10 {
		return types.SeverityMedium
	}
	return types.SeverityLow
}

// GetNodePVCUsage returns detailed PVC usage statistics per node
func (d *CrossNodePVCDetector) GetNodePVCUsage(ctx context.Context) ([]types.NodePVCUsage, error) {
	pods, err := d.client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	// Track usage per node
	nodeUsage := make(map[string]map[string]int) // node -> pvc -> count

	for _, pod := range pods.Items {
		if pod.Spec.NodeName == "" {
			continue
		}

		if nodeUsage[pod.Spec.NodeName] == nil {
			nodeUsage[pod.Spec.NodeName] = make(map[string]int)
		}

		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim != nil {
				pvcKey := fmt.Sprintf("%s/%s", pod.Namespace, volume.PersistentVolumeClaim.ClaimName)
				nodeUsage[pod.Spec.NodeName][pvcKey]++
			}
		}
	}

	// Convert to result format
	var result []types.NodePVCUsage
	for node, pvcCounts := range nodeUsage {
		total := 0
		for _, count := range pvcCounts {
			total += count
		}

		result = append(result, types.NodePVCUsage{
			Node:      node,
			PVCCounts: pvcCounts,
			Total:     total,
		})
	}

	return result, nil
}