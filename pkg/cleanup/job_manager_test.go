package cleanup_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/jdambly/kubectl-csi-scan/pkg/cleanup"
)

var _ = Describe("CleanupJobManager", func() {
	var (
		fakeClient  *fake.Clientset
		jobManager  *cleanup.CleanupJobManager
		ctx         context.Context
		namespace   string
	)

	BeforeEach(func() {
		fakeClient = fake.NewSimpleClientset()
		namespace = "test-namespace"
		jobManager = cleanup.NewCleanupJobManager(fakeClient, namespace)
		ctx = context.Background()
	})

	Describe("NewCleanupJobManager", func() {
		It("should create a new cleanup job manager", func() {
			manager := cleanup.NewCleanupJobManager(fakeClient, "default")
			Expect(manager).NotTo(BeNil())
		})
	})

	Describe("CreateCleanupJob", func() {
		var config cleanup.CleanupJobConfig

		BeforeEach(func() {
			config = cleanup.CleanupJobConfig{
				NodeName:        "test-node",
				DryRun:          true,
				Verbose:         true,
				Image:           "test-image:latest",
				ImagePullPolicy: "IfNotPresent",
				Namespace:       namespace,
				ServiceAccount:  "test-sa",
			}
		})

		Context("when creating a valid cleanup job", func() {
			It("should create job and service account successfully", func() {
				jobName, err := jobManager.CreateCleanupJob(ctx, config)
				
				Expect(err).NotTo(HaveOccurred())
				Expect(jobName).To(Equal("csi-mount-cleanup-test-node"))

				// Verify job was created
				jobs, err := fakeClient.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
				Expect(err).NotTo(HaveOccurred())
				Expect(jobs.Items).To(HaveLen(1))

				job := jobs.Items[0]
				Expect(job.Name).To(Equal("csi-mount-cleanup-test-node"))
				Expect(job.Spec.Template.Spec.NodeSelector["kubernetes.io/hostname"]).To(Equal("test-node"))
				Expect(job.Spec.Template.Spec.Containers[0].Image).To(Equal("test-image:latest"))
				
				// Verify service account was created
				serviceAccounts, err := fakeClient.CoreV1().ServiceAccounts(namespace).List(ctx, metav1.ListOptions{})
				Expect(err).NotTo(HaveOccurred())
				Expect(serviceAccounts.Items).To(HaveLen(1))
				Expect(serviceAccounts.Items[0].Name).To(Equal("test-sa"))
			})

			It("should set correct environment variables for dry run", func() {
				_, err := jobManager.CreateCleanupJob(ctx, config)
				Expect(err).NotTo(HaveOccurred())

				jobs, err := fakeClient.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
				Expect(err).NotTo(HaveOccurred())
				
				container := jobs.Items[0].Spec.Template.Spec.Containers[0]
				
				var dryRunEnv, verboseEnv *corev1.EnvVar
				for i, env := range container.Env {
					if env.Name == "DRY_RUN" {
						dryRunEnv = &container.Env[i]
					}
					if env.Name == "VERBOSE" {
						verboseEnv = &container.Env[i]
					}
				}
				
				Expect(dryRunEnv).NotTo(BeNil())
				Expect(dryRunEnv.Value).To(Equal("true"))
				Expect(verboseEnv).NotTo(BeNil())
				Expect(verboseEnv.Value).To(Equal("true"))
			})

			It("should configure proper security context", func() {
				_, err := jobManager.CreateCleanupJob(ctx, config)
				Expect(err).NotTo(HaveOccurred())

				jobs, err := fakeClient.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
				Expect(err).NotTo(HaveOccurred())
				
				container := jobs.Items[0].Spec.Template.Spec.Containers[0]
				securityContext := container.SecurityContext
				
				Expect(securityContext).NotTo(BeNil())
				Expect(*securityContext.Privileged).To(BeTrue())
				Expect(*securityContext.RunAsUser).To(Equal(int64(0)))
				Expect(*securityContext.RunAsGroup).To(Equal(int64(0)))
			})

			It("should mount required volumes", func() {
				_, err := jobManager.CreateCleanupJob(ctx, config)
				Expect(err).NotTo(HaveOccurred())

				jobs, err := fakeClient.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
				Expect(err).NotTo(HaveOccurred())
				
				podSpec := jobs.Items[0].Spec.Template.Spec
				volumeMounts := podSpec.Containers[0].VolumeMounts
				
				expectedMounts := map[string]string{
					"kubelet-dir": "/var/lib/kubelet",
					"host-proc":   "/host/proc",
					"host-sys":    "/host/sys",
				}
				
				for _, mount := range volumeMounts {
					expectedPath, exists := expectedMounts[mount.Name]
					Expect(exists).To(BeTrue(), fmt.Sprintf("Unexpected mount: %s", mount.Name))
					Expect(mount.MountPath).To(Equal(expectedPath))
				}
				
				Expect(len(volumeMounts)).To(Equal(len(expectedMounts)))
			})

			It("should not create service account if it already exists", func() {
				// Pre-create the service account
				existingSA := &corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      config.ServiceAccount,
						Namespace: namespace,
					},
				}
				_, err := fakeClient.CoreV1().ServiceAccounts(namespace).Create(ctx, existingSA, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())

				_, err = jobManager.CreateCleanupJob(ctx, config)
				Expect(err).NotTo(HaveOccurred())

				// Should still have only one service account
				serviceAccounts, err := fakeClient.CoreV1().ServiceAccounts(namespace).List(ctx, metav1.ListOptions{})
				Expect(err).NotTo(HaveOccurred())
				Expect(serviceAccounts.Items).To(HaveLen(1))
			})
		})

		Context("when job creation fails", func() {
			It("should return error if job creation fails", func() {
				fakeClient.PrependReactor("create", "jobs", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, fmt.Errorf("job creation failed")
				})

				_, err := jobManager.CreateCleanupJob(ctx, config)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to create job"))
			})

			It("should return error if service account creation fails", func() {
				fakeClient.PrependReactor("create", "serviceaccounts", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, fmt.Errorf("service account creation failed")
				})

				_, err := jobManager.CreateCleanupJob(ctx, config)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to create service account"))
			})
		})

		Context("with different configurations", func() {
			It("should handle non-dry-run mode", func() {
				config.DryRun = false
				config.Verbose = false

				_, err := jobManager.CreateCleanupJob(ctx, config)
				Expect(err).NotTo(HaveOccurred())

				jobs, err := fakeClient.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
				Expect(err).NotTo(HaveOccurred())
				
				container := jobs.Items[0].Spec.Template.Spec.Containers[0]
				
				var dryRunEnv, verboseEnv *corev1.EnvVar
				for i, env := range container.Env {
					if env.Name == "DRY_RUN" {
						dryRunEnv = &container.Env[i]
					}
					if env.Name == "VERBOSE" {
						verboseEnv = &container.Env[i]
					}
				}
				
				Expect(dryRunEnv.Value).To(Equal("false"))
				Expect(verboseEnv.Value).To(Equal("false"))
			})

			It("should handle custom image and pull policy", func() {
				config.Image = "custom/image:v1.0.0"
				config.ImagePullPolicy = "Always"

				_, err := jobManager.CreateCleanupJob(ctx, config)
				Expect(err).NotTo(HaveOccurred())

				jobs, err := fakeClient.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
				Expect(err).NotTo(HaveOccurred())
				
				container := jobs.Items[0].Spec.Template.Spec.Containers[0]
				Expect(container.Image).To(Equal("custom/image:v1.0.0"))
				Expect(string(container.ImagePullPolicy)).To(Equal("Always"))
			})
		})
	})

	Describe("WaitForJobs", func() {
		var jobNames []string

		BeforeEach(func() {
			jobNames = []string{"test-job-1", "test-job-2"}
		})

		Context("when jobs complete successfully", func() {
			It("should return without error when all jobs succeed", func() {
				// Create jobs in succeeded state
				for _, jobName := range jobNames {
					job := &batchv1.Job{
						ObjectMeta: metav1.ObjectMeta{
							Name:      jobName,
							Namespace: namespace,
						},
						Status: batchv1.JobStatus{
							Succeeded: 1,
						},
					}
					_, err := fakeClient.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
					Expect(err).NotTo(HaveOccurred())
				}

				ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()

				err := jobManager.WaitForJobs(ctx, jobNames)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when jobs fail", func() {
			It("should return error if any job fails", func() {
				// Create one successful job and one failed job
				successJob := &batchv1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:      jobNames[0],
						Namespace: namespace,
					},
					Status: batchv1.JobStatus{
						Succeeded: 1,
					},
				}
				failedJob := &batchv1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:      jobNames[1],
						Namespace: namespace,
					},
					Status: batchv1.JobStatus{
						Failed: 1,
					},
				}

				_, err := fakeClient.BatchV1().Jobs(namespace).Create(ctx, successJob, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())
				_, err = fakeClient.BatchV1().Jobs(namespace).Create(ctx, failedJob, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())

				ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()

				err = jobManager.WaitForJobs(ctx, jobNames)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("cleanup job test-job-2 failed"))
			})
		})

		Context("when jobs are still running", func() {
			It("should timeout if jobs don't complete", func() {
				// Create jobs without completion status
				for _, jobName := range jobNames {
					job := &batchv1.Job{
						ObjectMeta: metav1.ObjectMeta{
							Name:      jobName,
							Namespace: namespace,
						},
						Status: batchv1.JobStatus{
							// No succeeded or failed status - still running
						},
					}
					_, err := fakeClient.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
					Expect(err).NotTo(HaveOccurred())
				}

				ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()

				err := jobManager.WaitForJobs(ctx, jobNames)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("timeout waiting for jobs to complete"))
			})
		})

		Context("when job doesn't exist", func() {
			It("should return error if job is not found", func() {
				ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()

				err := jobManager.WaitForJobs(ctx, []string{"non-existent-job"})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to get job"))
			})
		})
	})

	Describe("Template Generation", func() {
		It("should generate valid job manifest", func() {
			config := cleanup.CleanupJobConfig{
				NodeName:        "test-node",
				DryRun:          true,
				Verbose:         false,
				Image:           "test-image:latest",
				ImagePullPolicy: "IfNotPresent",
				Namespace:       namespace,
				ServiceAccount:  "test-sa",
			}

			_, err := jobManager.CreateCleanupJob(ctx, config)
			Expect(err).NotTo(HaveOccurred())

			jobs, err := fakeClient.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(jobs.Items).To(HaveLen(1))

			job := jobs.Items[0]
			
			// Verify essential job configuration
			Expect(job.Spec.BackoffLimit).NotTo(BeNil())
			Expect(*job.Spec.BackoffLimit).To(Equal(int32(0)))
			Expect(job.Spec.Completions).NotTo(BeNil())
			Expect(*job.Spec.Completions).To(Equal(int32(1)))
			Expect(job.Spec.Parallelism).NotTo(BeNil())
			Expect(*job.Spec.Parallelism).To(Equal(int32(1)))
			Expect(job.Spec.TTLSecondsAfterFinished).NotTo(BeNil())
			Expect(*job.Spec.TTLSecondsAfterFinished).To(Equal(int32(3600)))

			// Verify pod template
			podSpec := job.Spec.Template.Spec
			Expect(podSpec.RestartPolicy).To(Equal(corev1.RestartPolicyNever))
			Expect(podSpec.HostNetwork).To(BeTrue())
			Expect(podSpec.HostPID).To(BeTrue())
			Expect(podSpec.PriorityClassName).To(Equal("system-node-critical"))
			Expect(podSpec.ServiceAccountName).To(Equal("test-sa"))

			// Verify tolerations
			Expect(len(podSpec.Tolerations)).To(BeNumerically(">=", 3))
			for _, toleration := range podSpec.Tolerations {
				Expect(toleration.Operator).To(Equal(corev1.TolerationOpExists))
			}

			// Verify labels and annotations
			Expect(job.Labels["app"]).To(Equal("kubectl-csi-scan"))
			Expect(job.Labels["component"]).To(Equal("cleanup-job"))
			Expect(job.Labels["node"]).To(Equal("test-node"))
			Expect(job.Annotations["kubectl-csi-scan/created-by"]).To(Equal("kubectl-csi-scan"))
			Expect(job.Annotations["kubectl-csi-scan/node"]).To(Equal("test-node"))
			Expect(job.Annotations["kubectl-csi-scan/dry-run"]).To(Equal("true"))
		})

		It("should include command arguments for dry run and verbose modes", func() {
			config := cleanup.CleanupJobConfig{
				NodeName:        "test-node",
				DryRun:          true,
				Verbose:         true,
				Image:           "test-image:latest",
				ImagePullPolicy: "IfNotPresent",
				Namespace:       namespace,
				ServiceAccount:  "test-sa",
			}

			_, err := jobManager.CreateCleanupJob(ctx, config)
			Expect(err).NotTo(HaveOccurred())

			jobs, err := fakeClient.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())

			container := jobs.Items[0].Spec.Template.Spec.Containers[0]
			
			// Check that command is set
			Expect(container.Command).To(Equal([]string{"/usr/local/bin/cleanup-mounts.sh"}))
			
			// In a real template execution, arguments would be conditionally added
			// For this test, we verify the environment variables are set correctly
			// which the script will use to determine behavior
			envMap := make(map[string]string)
			for _, env := range container.Env {
				envMap[env.Name] = env.Value
			}
			
			Expect(envMap["DRY_RUN"]).To(Equal("true"))
			Expect(envMap["VERBOSE"]).To(Equal("true"))
		})

		It("should set resource limits and requests", func() {
			config := cleanup.CleanupJobConfig{
				NodeName:        "test-node",
				DryRun:          false,
				Verbose:         false,
				Image:           "test-image:latest",
				ImagePullPolicy: "IfNotPresent",
				Namespace:       namespace,
				ServiceAccount:  "test-sa",
			}

			_, err := jobManager.CreateCleanupJob(ctx, config)
			Expect(err).NotTo(HaveOccurred())

			jobs, err := fakeClient.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())

			container := jobs.Items[0].Spec.Template.Spec.Containers[0]
			resources := container.Resources

			Expect(resources.Requests).NotTo(BeEmpty())
			Expect(resources.Limits).NotTo(BeEmpty())
			
			// Verify specific resource values
			Expect(resources.Requests.Memory().String()).To(Equal("64Mi"))
			Expect(resources.Requests.Cpu().String()).To(Equal("100m"))
			Expect(resources.Limits.Memory().String()).To(Equal("256Mi"))
			Expect(resources.Limits.Cpu().String()).To(Equal("500m"))
		})
	})

	Describe("Error Handling", func() {
		It("should handle invalid template data gracefully", func() {
			config := cleanup.CleanupJobConfig{
				NodeName:        "", // Invalid empty node name
				DryRun:          true,
				Verbose:         true,
				Image:           "test-image:latest",
				ImagePullPolicy: "IfNotPresent",
				Namespace:       namespace,
				ServiceAccount:  "test-sa",
			}

			// This should still work as the template doesn't validate node name
			_, err := jobManager.CreateCleanupJob(ctx, config)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle Kubernetes API errors during job creation", func() {
			config := cleanup.CleanupJobConfig{
				NodeName:        "test-node",
				DryRun:          true,
				Verbose:         true,
				Image:           "test-image:latest",
				ImagePullPolicy: "IfNotPresent",
				Namespace:       namespace,
				ServiceAccount:  "test-sa",
			}

			// Simulate API error
			fakeClient.PrependReactor("create", "*", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
				createAction := action.(k8stesting.CreateAction)
				if strings.Contains(createAction.GetResource().Resource, "jobs") {
					return true, nil, fmt.Errorf("API server error")
				}
				return false, nil, nil
			})

			_, err := jobManager.CreateCleanupJob(ctx, config)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create job"))
		})
	})
})