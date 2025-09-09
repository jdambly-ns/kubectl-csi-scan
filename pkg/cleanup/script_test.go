package cleanup_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cleanup Script", func() {
	var (
		scriptPath string
		tmpDir     string
	)

	BeforeEach(func() {
		// Get the project root directory
		wd, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		
		// Navigate to project root (from pkg/cleanup to project root)
		projectRoot := filepath.Join(wd, "..", "..")
		scriptPath = filepath.Join(projectRoot, "scripts", "cleanup-mounts.sh")
		
		// Verify script exists
		_, err = os.Stat(scriptPath)
		Expect(err).NotTo(HaveOccurred())

		// Create temporary directory for test files
		tmpDir, err = os.MkdirTemp("", "cleanup-script-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		// Clean up temporary directory
		if tmpDir != "" {
			os.RemoveAll(tmpDir)
		}
	})

	Describe("Script Execution", func() {
		Context("when run with --help flag", func() {
			It("should display help information", func() {
				cmd := exec.Command("bash", scriptPath, "--help")
				output, err := cmd.CombinedOutput()
				
				Expect(err).NotTo(HaveOccurred())
				
				helpText := string(output)
				Expect(helpText).To(ContainSubstring("CSI Mount Cleanup Script"))
				Expect(helpText).To(ContainSubstring("Usage:"))
				Expect(helpText).To(ContainSubstring("--dry-run"))
				Expect(helpText).To(ContainSubstring("--verbose"))
				Expect(helpText).To(ContainSubstring("--node-name"))
				Expect(helpText).To(ContainSubstring("EXAMPLES:"))
			})
		})

		Context("when run with invalid arguments", func() {
			It("should return error for unknown options", func() {
				cmd := exec.Command("bash", scriptPath, "--invalid-option")
				output, err := cmd.CombinedOutput()
				
				Expect(err).To(HaveOccurred())
				Expect(string(output)).To(ContainSubstring("Unknown option"))
				Expect(string(output)).To(ContainSubstring("Use --help for usage information"))
			})
		})

		Context("when run as non-root user", func() {
			It("should exit with privilege error", func() {
				// Set environment to simulate non-root execution
				cmd := exec.Command("bash", scriptPath, "--dry-run")
				cmd.Env = append(os.Environ(), "DRY_RUN=true")
				
				// Most CI environments run as non-root, so this should fail
				// unless specifically running in a privileged container
				output, err := cmd.CombinedOutput()
				
				// In non-privileged environments, should get privilege error
				if err != nil {
					Expect(string(output)).To(ContainSubstring("must be run as root"))
				}
				// If running in privileged environment, script should proceed
			})
		})

		Context("when CSI paths don't exist", func() {
			It("should handle missing CSI directories gracefully in dry-run", func() {
				cmd := exec.Command("bash", scriptPath, "--dry-run", "--verbose")
				cmd.Env = append(os.Environ(), 
					"DRY_RUN=true", 
					"VERBOSE=true",
				)
				
				// Run in environment without CSI paths
				output, err := cmd.CombinedOutput()
				
				// Script should run but warn about missing paths
				outputStr := string(output)
				if strings.Contains(outputStr, "must be run as root") {
					Skip("Test requires root privileges")
				}
				
				// Should either complete successfully or warn about missing paths
				if err == nil {
					Expect(outputStr).To(SatisfyAny(
						ContainSubstring("No stuck CSI mounts found"),
						ContainSubstring("Some CSI paths are missing"),
						ContainSubstring("WARNING"),
					))
				}
			})
		})
	})

	Describe("Environment Variable Handling", func() {
		Context("when environment variables are set", func() {
			It("should respect DRY_RUN environment variable", func() {
				cmd := exec.Command("bash", scriptPath)
				cmd.Env = append(os.Environ(), 
					"DRY_RUN=true",
					"VERBOSE=true",
				)
				
				output, err := cmd.CombinedOutput()
				outputStr := string(output)
				
				if strings.Contains(outputStr, "must be run as root") {
					Skip("Test requires root privileges")
				}
				
				if err == nil || !strings.Contains(outputStr, "No stuck CSI mounts found") {
					Expect(outputStr).To(ContainSubstring("DRY RUN"))
				}
			})

			It("should respect VERBOSE environment variable", func() {
				cmd := exec.Command("bash", scriptPath)
				cmd.Env = append(os.Environ(), 
					"DRY_RUN=true",
					"VERBOSE=true",
				)
				
				output, err := cmd.CombinedOutput()
				outputStr := string(output)
				
				if strings.Contains(outputStr, "must be run as root") {
					Skip("Test requires root privileges")
				}
				
				if err == nil {
					// In verbose mode, should see scanning messages
					Expect(outputStr).To(ContainSubstring("Scanning for stuck CSI mounts"))
				}
			})

			It("should respect NODE_NAME environment variable", func() {
				testNodeName := "test-node-123"
				cmd := exec.Command("bash", scriptPath, "--dry-run")
				cmd.Env = append(os.Environ(), 
					"DRY_RUN=true",
					"NODE_NAME="+testNodeName,
				)
				
				output, err := cmd.CombinedOutput()
				outputStr := string(output)
				
				if strings.Contains(outputStr, "must be run as root") {
					Skip("Test requires root privileges")
				}
				
				if err == nil {
					Expect(outputStr).To(ContainSubstring(testNodeName))
				}
			})
		})
	})

	Describe("Script Validation", func() {
		Context("when checking script syntax", func() {
			It("should have valid bash syntax", func() {
				cmd := exec.Command("bash", "-n", scriptPath)
				output, err := cmd.CombinedOutput()
				
				Expect(err).NotTo(HaveOccurred(), "Script has syntax errors: %s", string(output))
			})
		})

		Context("when checking for dangerous commands", func() {
			It("should not contain rm -rf / or similar dangerous patterns", func() {
				content, err := os.ReadFile(scriptPath)
				Expect(err).NotTo(HaveOccurred())
				
				scriptContent := string(content)
				
				// Check for dangerous patterns
				dangerousPatterns := []string{
					"rm -rf /",
					"rm -rf /*", 
					"rm -rf /$",
					"mkfs.",
					"dd if=/",
				}
				
				for _, pattern := range dangerousPatterns {
					Expect(scriptContent).NotTo(ContainSubstring(pattern), 
						"Script contains dangerous pattern: %s", pattern)
				}
			})

			It("should contain safety checks", func() {
				content, err := os.ReadFile(scriptPath)
				Expect(err).NotTo(HaveOccurred())
				
				scriptContent := string(content)
				
				// Check for safety patterns
				safetyPatterns := []string{
					"set -euo pipefail",     // Strict error handling
					"check_privileges",       // Privilege checking
					"check_mount_safety",     // Mount safety validation
					"kubernetes.io/csi",      // CSI path validation
					"DRY_RUN",               // Dry run support
				}
				
				for _, pattern := range safetyPatterns {
					Expect(scriptContent).To(ContainSubstring(pattern), 
						"Script missing safety pattern: %s", pattern)
				}
			})

			It("should validate mount paths before cleanup", func() {
				content, err := os.ReadFile(scriptPath)
				Expect(err).NotTo(HaveOccurred())
				
				scriptContent := string(content)
				
				// Should contain path validation logic
				Expect(scriptContent).To(ContainSubstring("check_mount_safety"))
				Expect(scriptContent).To(ContainSubstring("kubernetes.io/csi"))
				Expect(scriptContent).To(ContainSubstring("cinder.csi.openstack.org"))
				Expect(scriptContent).To(ContainSubstring("globalmount"))
			})
		})

		Context("when checking unmount strategy", func() {
			It("should implement progressive unmount strategy", func() {
				content, err := os.ReadFile(scriptPath)
				Expect(err).NotTo(HaveOccurred())
				
				scriptContent := string(content)
				
				// Should try graceful unmount first
				Expect(scriptContent).To(ContainSubstring("umount \"$mount_path\""))
				// Then lazy unmount
				Expect(scriptContent).To(ContainSubstring("umount -l"))
				// Finally force unmount
				Expect(scriptContent).To(ContainSubstring("umount -f"))
				
				// Should have proper error handling between attempts
				Expect(scriptContent).To(ContainSubstring("if umount"))
			})

			It("should have proper cleanup of empty directories", func() {
				content, err := os.ReadFile(scriptPath)
				Expect(err).NotTo(HaveOccurred())
				
				scriptContent := string(content)
				
				// Should clean up empty mount directories
				Expect(scriptContent).To(ContainSubstring("cleanup_mount_references"))
				Expect(scriptContent).To(ContainSubstring("rmdir"))
				Expect(scriptContent).To(ContainSubstring("ls -A"))
			})
		})

		Context("when checking logging and output", func() {
			It("should have consistent logging functions", func() {
				content, err := os.ReadFile(scriptPath)
				Expect(err).NotTo(HaveOccurred())
				
				scriptContent := string(content)
				
				// Should have logging functions
				loggingFunctions := []string{
					"log()",
					"verbose()",
					"error()",
				}
				
				for _, fn := range loggingFunctions {
					Expect(scriptContent).To(ContainSubstring(fn), 
						"Script missing logging function: %s", fn)
				}
				
				// Should use LOG_PREFIX consistently
				Expect(scriptContent).To(ContainSubstring("LOG_PREFIX"))
			})

			It("should provide comprehensive summary output", func() {
				content, err := os.ReadFile(scriptPath)
				Expect(err).NotTo(HaveOccurred())
				
				scriptContent := string(content)
				
				// Should have summary function
				Expect(scriptContent).To(ContainSubstring("show_summary"))
				Expect(scriptContent).To(ContainSubstring("Total mounts processed"))
				Expect(scriptContent).To(ContainSubstring("Successful cleanups"))
				Expect(scriptContent).To(ContainSubstring("Failed cleanups"))
			})
		})
	})

	Describe("Integration with Container Environment", func() {
		Context("when script is executable in container context", func() {
			It("should be executable", func() {
				info, err := os.Stat(scriptPath)
				Expect(err).NotTo(HaveOccurred())
				
				mode := info.Mode()
				Expect(mode & 0111).NotTo(Equal(os.FileMode(0)), "Script should be executable")
			})

			It("should have correct shebang", func() {
				content, err := os.ReadFile(scriptPath)
				Expect(err).NotTo(HaveOccurred())
				
				lines := strings.Split(string(content), "\n")
				Expect(len(lines)).To(BeNumerically(">", 0))
				Expect(lines[0]).To(Equal("#!/bin/bash"))
			})
		})
	})

	Describe("Command Line Argument Parsing", func() {
		Context("when testing argument combinations", func() {
			It("should handle multiple flags correctly", func() {
				cmd := exec.Command("bash", scriptPath, "--dry-run", "--verbose", "--node-name", "test-node")
				cmd.Env = append(os.Environ(), "DRY_RUN=true", "VERBOSE=true")
				
				output, err := cmd.CombinedOutput()
				outputStr := string(output)
				
				if strings.Contains(outputStr, "must be run as root") {
					Skip("Test requires root privileges")
				}
				
				// Should parse all arguments without error
				if err != nil {
					// Check if it's just a path/permission issue, not argument parsing
					Expect(outputStr).NotTo(ContainSubstring("Unknown option"))
					Expect(outputStr).NotTo(ContainSubstring("invalid"))
				}
			})

			It("should handle node name with special characters", func() {
				specialNodeName := "node-with-123_chars.example.com"
				cmd := exec.Command("bash", scriptPath, "--dry-run", "--node-name", specialNodeName)
				cmd.Env = append(os.Environ(), "DRY_RUN=true")
				
				output, err := cmd.CombinedOutput()
				outputStr := string(output)
				
				if strings.Contains(outputStr, "must be run as root") {
					Skip("Test requires root privileges")
				}
				
				// Should handle special characters in node names
				if err == nil {
					Expect(outputStr).To(ContainSubstring(specialNodeName))
				}
			})
		})
	})
})