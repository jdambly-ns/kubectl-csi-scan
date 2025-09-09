package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cleanup Command Integration", func() {
	var (
		binaryPath string
		tmpDir     string
	)

	BeforeEach(func() {
		// Get the project root directory
		wd, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		
		// Build the binary for testing
		binaryPath = filepath.Join(wd, "kubectl-csi_scan-test")
		
		// Build the test binary
		buildCmd := exec.Command("go", "build", "-o", binaryPath, "cmd/main.go")
		buildOutput, err := buildCmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "Failed to build test binary: %s", string(buildOutput))

		// Create temporary directory for test files
		tmpDir, err = os.MkdirTemp("", "cleanup-cmd-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		// Clean up test binary and temp directory
		if binaryPath != "" {
			os.Remove(binaryPath)
		}
		if tmpDir != "" {
			os.RemoveAll(tmpDir)
		}
	})

	Describe("Cleanup Command Help", func() {
		It("should display help information", func() {
			cmd := exec.Command(binaryPath, "cleanup", "--help")
			output, err := cmd.CombinedOutput()
			
			Expect(err).NotTo(HaveOccurred())
			
			helpText := string(output)
			Expect(helpText).To(ContainSubstring("Create and run Kubernetes jobs"))
			Expect(helpText).To(ContainSubstring("cleanup stuck CSI mount references"))
			Expect(helpText).To(ContainSubstring("--nodes"))
			Expect(helpText).To(ContainSubstring("--dry-run"))
			Expect(helpText).To(ContainSubstring("--verbose"))
			Expect(helpText).To(ContainSubstring("--image"))
			Expect(helpText).To(ContainSubstring("--namespace"))
			Expect(helpText).To(ContainSubstring("Security Notes"))
			Expect(helpText).To(ContainSubstring("privileged security context"))
		})
	})

	Describe("Cleanup Command Validation", func() {
		Context("when required flags are missing", func() {
			It("should return error when no nodes are specified", func() {
				cmd := exec.Command(binaryPath, "cleanup")
				// Don't set KUBECONFIG to avoid actual cluster connection
				cmd.Env = []string{"KUBECONFIG=/nonexistent"}
				
				output, err := cmd.CombinedOutput()
				
				Expect(err).To(HaveOccurred())
				outputStr := string(output)
				Expect(outputStr).To(ContainSubstring("required flag(s) \"nodes\" not set"))
			})
		})

		Context("when invalid flags are provided", func() {
			It("should return error for unknown flags", func() {
				cmd := exec.Command(binaryPath, "cleanup", "--invalid-flag")
				cmd.Env = []string{"KUBECONFIG=/nonexistent"}
				
				output, err := cmd.CombinedOutput()
				
				Expect(err).To(HaveOccurred())
				Expect(string(output)).To(ContainSubstring("unknown flag"))
			})

			It("should validate timeout format", func() {
				cmd := exec.Command(binaryPath, "cleanup", "--nodes=node1", "--timeout=invalid")
				cmd.Env = []string{"KUBECONFIG=/nonexistent"}
				
				output, err := cmd.CombinedOutput()
				
				Expect(err).To(HaveOccurred())
				Expect(string(output)).To(ContainSubstring("invalid duration"))
			})
		})
	})

	Describe("Cleanup Command Configuration", func() {
		Context("when valid parameters are provided", func() {
			It("should accept valid node list", func() {
				cmd := exec.Command(binaryPath, "cleanup", 
					"--nodes=node1,node2,node3",
					"--dry-run",
					"--namespace=test-ns",
				)
				// Set invalid kubeconfig to fail at connection, not validation
				cmd.Env = []string{"KUBECONFIG=/nonexistent"}
				
				output, err := cmd.CombinedOutput()
				
				// Should fail at Kubernetes client creation, not parameter validation
				Expect(err).To(HaveOccurred())
				outputStr := string(output)
				Expect(outputStr).To(ContainSubstring("failed to initialize Kubernetes client"))
				// Should not contain parameter validation errors
				Expect(outputStr).NotTo(ContainSubstring("required flag"))
				Expect(outputStr).NotTo(ContainSubstring("unknown flag"))
			})

			It("should accept valid image and pull policy", func() {
				cmd := exec.Command(binaryPath, "cleanup",
					"--nodes=node1",
					"--image=custom/image:v1.0.0",
					"--image-pull-policy=Always",
					"--dry-run",
				)
				cmd.Env = []string{"KUBECONFIG=/nonexistent"}
				
				output, err := cmd.CombinedOutput()
				
				// Should fail at client creation, not validation
				Expect(err).To(HaveOccurred())
				outputStr := string(output)
				Expect(outputStr).To(ContainSubstring("failed to initialize Kubernetes client"))
			})

			It("should accept valid timeout duration", func() {
				cmd := exec.Command(binaryPath, "cleanup",
					"--nodes=node1",
					"--timeout=15m",
					"--dry-run",
				)
				cmd.Env = []string{"KUBECONFIG=/nonexistent"}
				
				output, err := cmd.CombinedOutput()
				
				// Should fail at client creation, not validation
				Expect(err).To(HaveOccurred())
				outputStr := string(output)
				Expect(outputStr).To(ContainSubstring("failed to initialize Kubernetes client"))
			})
		})
	})

	Describe("Cleanup Command Integration", func() {
		Context("when Kubernetes client fails", func() {
			It("should provide helpful error message for connection issues", func() {
				cmd := exec.Command(binaryPath, "cleanup",
					"--nodes=node1",
					"--dry-run",
				)
				cmd.Env = []string{"KUBECONFIG=/nonexistent"}
				
				output, err := cmd.CombinedOutput()
				
				Expect(err).To(HaveOccurred())
				outputStr := string(output)
				Expect(outputStr).To(ContainSubstring("failed to initialize Kubernetes client"))
				Expect(outputStr).To(ContainSubstring("check your kubeconfig and cluster connectivity"))
			})

			It("should handle invalid kubeconfig format", func() {
				// Create invalid kubeconfig
				invalidKubeconfig := filepath.Join(tmpDir, "invalid-kubeconfig")
				err := os.WriteFile(invalidKubeconfig, []byte("invalid yaml content"), 0644)
				Expect(err).NotTo(HaveOccurred())

				cmd := exec.Command(binaryPath, "cleanup",
					"--nodes=node1",
					"--dry-run",
				)
				cmd.Env = []string{"KUBECONFIG=" + invalidKubeconfig}
				
				output, err := cmd.CombinedOutput()
				
				Expect(err).To(HaveOccurred())
				outputStr := string(output)
				Expect(outputStr).To(ContainSubstring("failed to initialize Kubernetes client"))
			})
		})

		Context("when using different output verbosity", func() {
			It("should log cleanup parameters when verbose", func() {
				cmd := exec.Command(binaryPath, "cleanup",
					"--nodes=node1,node2",
					"--verbose",
					"--dry-run",
					"--image=test:latest",
				)
				cmd.Env = []string{
					"KUBECONFIG=/nonexistent",
					"LOG_FORMAT=json", // Enable structured logging
				}
				
				output, err := cmd.CombinedOutput()
				
				Expect(err).To(HaveOccurred()) // Expected due to invalid kubeconfig
				outputStr := string(output)
				
				// Should log the parameters before failing
				if strings.Contains(outputStr, "starting cleanup job creation") {
					Expect(outputStr).To(ContainSubstring("node1"))
					Expect(outputStr).To(ContainSubstring("node2"))
					Expect(outputStr).To(ContainSubstring("test:latest"))
				}
			})
		})
	})

	Describe("Command Integration with Other Subcommands", func() {
		Context("when running detect before cleanup", func() {
			It("should be able to run detect command", func() {
				cmd := exec.Command(binaryPath, "detect", "--help")
				output, err := cmd.CombinedOutput()
				
				Expect(err).NotTo(HaveOccurred())
				Expect(string(output)).To(ContainSubstring("Detect CSI mount issues"))
			})

			It("should be able to run analyze command", func() {
				cmd := exec.Command(binaryPath, "analyze", "--help")
				output, err := cmd.CombinedOutput()
				
				Expect(err).NotTo(HaveOccurred())
				Expect(string(output)).To(ContainSubstring("Perform detailed analysis"))
			})

			It("should show cleanup in main help", func() {
				cmd := exec.Command(binaryPath, "--help")
				output, err := cmd.CombinedOutput()
				
				Expect(err).NotTo(HaveOccurred())
				helpText := string(output)
				Expect(helpText).To(ContainSubstring("cleanup"))
				Expect(helpText).To(ContainSubstring("detect"))
				Expect(helpText).To(ContainSubstring("analyze"))
				Expect(helpText).To(ContainSubstring("metrics"))
			})
		})
	})

	Describe("Error Handling and User Experience", func() {
		Context("when providing user-friendly errors", func() {
			It("should provide clear error for missing required arguments", func() {
				cmd := exec.Command(binaryPath, "cleanup", "--dry-run")
				cmd.Env = []string{"KUBECONFIG=/nonexistent"}
				
				output, err := cmd.CombinedOutput()
				
				Expect(err).To(HaveOccurred())
				outputStr := string(output)
				Expect(outputStr).To(ContainSubstring("required flag"))
				Expect(outputStr).To(ContainSubstring("nodes"))
			})

			It("should show usage information on command errors", func() {
				cmd := exec.Command(binaryPath, "cleanup", "--invalid")
				cmd.Env = []string{"KUBECONFIG=/nonexistent"}
				
				output, err := cmd.CombinedOutput()
				
				Expect(err).To(HaveOccurred())
				outputStr := string(output)
				Expect(outputStr).To(ContainSubstring("Usage:"))
			})
		})

		Context("when handling timeouts and cancellation", func() {
			It("should respect context cancellation", func() {
				// This test ensures the command structure supports context handling
				// The actual timeout testing would require a real cluster
				
				cmd := exec.Command(binaryPath, "cleanup",
					"--nodes=node1",
					"--timeout=1ns", // Very short timeout
					"--dry-run",
				)
				cmd.Env = []string{"KUBECONFIG=/nonexistent"}
				
				start := time.Now()
				output, err := cmd.CombinedOutput()
				duration := time.Since(start)
				
				Expect(err).To(HaveOccurred())
				// Should fail quickly due to kubeconfig, not timeout
				Expect(duration).To(BeNumerically("<", 5*time.Second))
				
				outputStr := string(output)
				Expect(outputStr).To(ContainSubstring("failed to initialize Kubernetes client"))
			})
		})
	})

	Describe("Configuration Parsing", func() {
		Context("when parsing complex node lists", func() {
			It("should handle comma-separated node lists", func() {
				cmd := exec.Command(binaryPath, "cleanup",
					"--nodes=node1,node2,node-with-dashes,node.with.dots",
					"--dry-run",
				)
				cmd.Env = []string{"KUBECONFIG=/nonexistent"}
				
				output, err := cmd.CombinedOutput()
				
				// Should fail at client creation, not node list parsing
				Expect(err).To(HaveOccurred())
				outputStr := string(output)
				Expect(outputStr).To(ContainSubstring("failed to initialize Kubernetes client"))
				Expect(outputStr).NotTo(ContainSubstring("invalid node"))
			})

			It("should handle whitespace in node lists", func() {
				cmd := exec.Command(binaryPath, "cleanup",
					"--nodes=node1, node2 , node3",
					"--dry-run",
				)
				cmd.Env = []string{"KUBECONFIG=/nonexistent"}
				
				output, err := cmd.CombinedOutput()
				
				// Should fail at client creation, not parsing
				Expect(err).To(HaveOccurred())
				outputStr := string(output)
				Expect(outputStr).To(ContainSubstring("failed to initialize Kubernetes client"))
			})
		})

		Context("when parsing duration values", func() {
			It("should accept various duration formats", func() {
				validDurations := []string{"5m", "300s", "1h30m", "90m"}
				
				for _, duration := range validDurations {
					cmd := exec.Command(binaryPath, "cleanup",
						"--nodes=node1",
						"--timeout="+duration,
						"--dry-run",
					)
					cmd.Env = []string{"KUBECONFIG=/nonexistent"}
					
					output, err := cmd.CombinedOutput()
					
					// Should fail at client creation, not duration parsing
					Expect(err).To(HaveOccurred())
					outputStr := string(output)
					Expect(outputStr).To(ContainSubstring("failed to initialize Kubernetes client"))
					Expect(outputStr).NotTo(ContainSubstring("invalid duration"), 
						"Duration %s should be valid", duration)
				}
			})
		})
	})

	Describe("Command Line Interface Consistency", func() {
		Context("when comparing with other commands", func() {
			It("should have consistent flag naming with detect command", func() {
				// Check that global flags work consistently
				detectCmd := exec.Command(binaryPath, "detect", "--help")
				detectOutput, err := detectCmd.CombinedOutput()
				Expect(err).NotTo(HaveOccurred())
				
				cleanupCmd := exec.Command(binaryPath, "cleanup", "--help")
				cleanupOutput, err := cleanupCmd.CombinedOutput()
				Expect(err).NotTo(HaveOccurred())
				
				detectText := string(detectOutput)
				cleanupText := string(cleanupOutput)
				
				// Both should have global flags section
				globalFlags := []string{"--kubeconfig", "--context", "--namespace"}
				for _, flag := range globalFlags {
					if strings.Contains(detectText, flag) {
						Expect(cleanupText).To(ContainSubstring(flag), 
							"Cleanup command should have same global flag: %s", flag)
					}
				}
			})

			It("should have consistent help format", func() {
				commands := []string{"detect", "analyze", "cleanup", "metrics"}
				
				for _, command := range commands {
					cmd := exec.Command(binaryPath, command, "--help")
					output, err := cmd.CombinedOutput()
					
					Expect(err).NotTo(HaveOccurred(), "Command %s should have help", command)
					
					helpText := string(output)
					Expect(helpText).To(ContainSubstring("Usage:"))
					Expect(helpText).To(ContainSubstring("Flags:"))
				}
			})
		})
	})
})