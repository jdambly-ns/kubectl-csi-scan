package cleanup

import (
	"bytes"
	"context"
	"fmt"
	"text/template"
	"time"

	"github.com/rs/zerolog/log"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

// CleanupJobConfig holds configuration for a cleanup job
type CleanupJobConfig struct {
	NodeName        string
	DryRun          bool
	Verbose         bool
	Image           string
	ImagePullPolicy string
	Namespace       string
	ServiceAccount  string
}

// CleanupJobManager manages Kubernetes cleanup jobs
type CleanupJobManager struct {
	client    kubernetes.Interface
	namespace string
}

// NewCleanupJobManager creates a new cleanup job manager
func NewCleanupJobManager(client kubernetes.Interface, namespace string) *CleanupJobManager {
	return &CleanupJobManager{
		client:    client,
		namespace: namespace,
	}
}

// CreateCleanupJob creates a cleanup job for the specified node
func (m *CleanupJobManager) CreateCleanupJob(ctx context.Context, config CleanupJobConfig) (string, error) {
	// Generate job manifest from template
	manifest, err := m.generateJobManifest(config)
	if err != nil {
		return "", fmt.Errorf("failed to generate job manifest: %w", err)
	}

	// Parse the manifest into Kubernetes objects
	objects, err := m.parseManifest(manifest)
	if err != nil {
		return "", fmt.Errorf("failed to parse job manifest: %w", err)
	}

	// Create the objects in the cluster
	var jobName string
	for _, obj := range objects {
		switch resource := obj.(type) {
		case *corev1.ServiceAccount:
			err = m.createServiceAccount(ctx, resource)
		case *batchv1.Job:
			jobName = resource.Name
			err = m.createJob(ctx, resource)
		}
		
		if err != nil {
			return "", err
		}
	}

	return jobName, nil
}

// WaitForJobs waits for all specified jobs to complete
func (m *CleanupJobManager) WaitForJobs(ctx context.Context, jobNames []string) error {
	log.Info().Strs("jobs", jobNames).Msg("waiting for cleanup jobs to complete")

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for jobs to complete")
		case <-ticker.C:
			allComplete, err := m.checkJobsStatus(ctx, jobNames)
			if err != nil {
				return fmt.Errorf("failed to check job status: %w", err)
			}
			
			if allComplete {
				log.Info().Msg("all cleanup jobs completed successfully")
				return nil
			}
		}
	}
}

// generateJobManifest generates a job manifest from the template
func (m *CleanupJobManager) generateJobManifest(config CleanupJobConfig) (string, error) {
	// Template data for manifest generation
	templateData := struct {
		NodeName        string
		DryRun          bool
		Verbose         bool
		Image           string
		ImagePullPolicy string
		Namespace       string
		ServiceAccount  string
	}{
		NodeName:        config.NodeName,
		DryRun:          config.DryRun,
		Verbose:         config.Verbose,
		Image:           config.Image,
		ImagePullPolicy: config.ImagePullPolicy,
		Namespace:       config.Namespace,
		ServiceAccount:  config.ServiceAccount,
	}

	// Job template
	jobTemplate := `apiVersion: batch/v1
kind: Job
metadata:
  name: csi-mount-cleanup-{{.NodeName}}
  namespace: {{.Namespace}}
  labels:
    app: kubectl-csi-scan
    component: cleanup-job
    node: {{.NodeName}}
    kubectl-csi-scan/managed: "true"
  annotations:
    kubectl-csi-scan/created-by: kubectl-csi-scan
    kubectl-csi-scan/node: {{.NodeName}}
    kubectl-csi-scan/dry-run: "{{.DryRun}}"
spec:
  backoffLimit: 0
  completions: 1
  parallelism: 1
  ttlSecondsAfterFinished: 3600
  template:
    metadata:
      labels:
        app: kubectl-csi-scan
        component: cleanup-job
        node: {{.NodeName}}
    spec:
      restartPolicy: Never
      nodeSelector:
        kubernetes.io/hostname: {{.NodeName}}
      tolerations:
      - operator: Exists
        effect: NoSchedule
      - operator: Exists
        effect: NoExecute
      - operator: Exists
        effect: PreferNoSchedule
      hostNetwork: true
      hostPID: true
      priorityClassName: system-node-critical
      serviceAccountName: {{.ServiceAccount}}
      containers:
      - name: csi-mount-cleanup
        image: {{.Image}}
        imagePullPolicy: {{.ImagePullPolicy}}
        securityContext:
          privileged: true
          runAsUser: 0
          runAsGroup: 0
        env:
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        - name: DRY_RUN
          value: "{{.DryRun}}"
        - name: VERBOSE
          value: "{{.Verbose}}"
        volumeMounts:
        - name: kubelet-dir
          mountPath: /var/lib/kubelet
          mountPropagation: Bidirectional
        - name: host-proc
          mountPath: /host/proc
          readOnly: true
        - name: host-sys
          mountPath: /host/sys
          readOnly: true
        resources:
          requests:
            memory: "64Mi"
            cpu: "100m"
          limits:
            memory: "256Mi"
            cpu: "500m"
        command: ["/usr/local/bin/cleanup-mounts.sh"]
        args: 
        {{- if .DryRun}}
        - "--dry-run"
        {{- end}}
        {{- if .Verbose}}
        - "--verbose"
        {{- end}}
      volumes:
      - name: kubelet-dir
        hostPath:
          path: /var/lib/kubelet
          type: Directory
      - name: host-proc
        hostPath:
          path: /proc
          type: Directory
      - name: host-sys
        hostPath:
          path: /sys
          type: Directory
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{.ServiceAccount}}
  namespace: {{.Namespace}}
  labels:
    app: kubectl-csi-scan
    component: cleanup-service-account`

	tmpl, err := template.New("cleanup-job").Parse(jobTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse job template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, templateData); err != nil {
		return "", fmt.Errorf("failed to execute job template: %w", err)
	}

	return buf.String(), nil
}

// parseManifest parses a YAML manifest into Kubernetes objects
func (m *CleanupJobManager) parseManifest(manifest string) ([]interface{}, error) {
	var objects []interface{}
	
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader([]byte(manifest)), 4096)
	
	for {
		var obj interface{}
		err := decoder.Decode(&obj)
		if err != nil {
			break
		}
		
		// Convert to proper Kubernetes types
		switch objMap := obj.(type) {
		case map[string]interface{}:
			kind, ok := objMap["kind"].(string)
			if !ok {
				continue
			}
			
			switch kind {
			case "Job":
				job := &batchv1.Job{}
				data, err := yaml.Marshal(objMap)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal job: %w", err)
				}
				if err := utilyaml.Unmarshal(data, job); err != nil {
					return nil, fmt.Errorf("failed to unmarshal job: %w", err)
				}
				objects = append(objects, job)
				
			case "ServiceAccount":
				sa := &corev1.ServiceAccount{}
				data, err := yaml.Marshal(objMap)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal service account: %w", err)
				}
				if err := utilyaml.Unmarshal(data, sa); err != nil {
					return nil, fmt.Errorf("failed to unmarshal service account: %w", err)
				}
				objects = append(objects, sa)
			}
		}
	}
	
	return objects, nil
}

// createServiceAccount creates a service account if it doesn't exist
func (m *CleanupJobManager) createServiceAccount(ctx context.Context, sa *corev1.ServiceAccount) error {
	_, err := m.client.CoreV1().ServiceAccounts(m.namespace).Get(ctx, sa.Name, metav1.GetOptions{})
	if err == nil {
		// Service account already exists
		log.Debug().Str("service_account", sa.Name).Msg("service account already exists")
		return nil
	}

	_, err = m.client.CoreV1().ServiceAccounts(m.namespace).Create(ctx, sa, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create service account %s: %w", sa.Name, err)
	}

	log.Info().Str("service_account", sa.Name).Msg("created service account")
	return nil
}

// createJob creates a cleanup job
func (m *CleanupJobManager) createJob(ctx context.Context, job *batchv1.Job) error {
	_, err := m.client.BatchV1().Jobs(m.namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create job %s: %w", job.Name, err)
	}

	log.Info().Str("job", job.Name).Str("node", job.Spec.Template.Spec.NodeSelector["kubernetes.io/hostname"]).Msg("created cleanup job")
	return nil
}

// checkJobsStatus checks if all jobs have completed
func (m *CleanupJobManager) checkJobsStatus(ctx context.Context, jobNames []string) (bool, error) {
	allComplete := true
	
	for _, jobName := range jobNames {
		job, err := m.client.BatchV1().Jobs(m.namespace).Get(ctx, jobName, metav1.GetOptions{})
		if err != nil {
			return false, fmt.Errorf("failed to get job %s: %w", jobName, err)
		}

		// Check job status
		if job.Status.Succeeded == 0 && job.Status.Failed == 0 {
			// Job is still running
			allComplete = false
			log.Debug().Str("job", jobName).Msg("job still running")
			continue
		}

		if job.Status.Failed > 0 {
			// Job failed
			log.Error().Str("job", jobName).Int32("failures", job.Status.Failed).Msg("cleanup job failed")
			return false, fmt.Errorf("cleanup job %s failed", jobName)
		}

		if job.Status.Succeeded > 0 {
			// Job completed successfully
			log.Info().Str("job", jobName).Msg("cleanup job completed successfully")
		}
	}

	return allComplete, nil
}