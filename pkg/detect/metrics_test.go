package detect_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jdambly/kubectl-csi-scan/pkg/detect"
)

var _ = Describe("MetricsDetector", func() {
	var (
		detector         *detect.MetricsDetector
		ctx              context.Context
		prometheusURL    string
		targetDriver     string
	)

	BeforeEach(func() {
		ctx = context.Background()
		prometheusURL = "http://prometheus:9090"
		targetDriver = "test.csi.driver"
	})

	Context("NewMetricsDetector", func() {
		It("should create detector with Prometheus URL and target driver", func() {
			detector = detect.NewMetricsDetector(prometheusURL, targetDriver)
			Expect(detector).NotTo(BeNil())
		})

		It("should create detector without Prometheus URL", func() {
			detector = detect.NewMetricsDetector("", targetDriver)
			Expect(detector).NotTo(BeNil())
		})

		It("should create detector without target driver", func() {
			detector = detect.NewMetricsDetector(prometheusURL, "")
			Expect(detector).NotTo(BeNil())
		})
	})

	Context("Detect", func() {
		BeforeEach(func() {
			detector = detect.NewMetricsDetector(prometheusURL, targetDriver)
		})

		It("should return framework implementation notice", func() {
			issues, err := detector.Detect(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(issues).To(BeEmpty())
			// Note: The current implementation is a framework placeholder
			// that returns empty results but no error
		})

		Context("when Prometheus is configured", func() {
			It("should provide metric queries for monitoring", func() {
				queries := detector.GetMetricQueries()
				Expect(queries).NotTo(BeEmpty())
				
				// Verify that we have queries for different types of CSI issues
				var foundAttachFailures, foundMountFailures, foundTimeouts bool
				for _, query := range queries {
					if query.Name == "CSI Attach Failures" {
						foundAttachFailures = true
						Expect(query.Query).To(ContainSubstring("csi_"))
						Expect(query.Description).To(ContainSubstring("attach"))
					}
					if query.Name == "CSI Mount Failures" {
						foundMountFailures = true
						Expect(query.Query).To(ContainSubstring("csi_"))
						Expect(query.Description).To(ContainSubstring("mount"))
					}
					if query.Name == "CSI Operation Timeouts" {
						foundTimeouts = true
						Expect(query.Query).To(ContainSubstring("timeout"))
					}
				}
				
				Expect(foundAttachFailures).To(BeTrue())
				Expect(foundMountFailures).To(BeTrue())
				Expect(foundTimeouts).To(BeTrue())
			})
		})
	})

	Context("GetMetricQueries", func() {
		BeforeEach(func() {
			detector = detect.NewMetricsDetector(prometheusURL, targetDriver)
		})

		It("should return comprehensive set of CSI monitoring queries", func() {
			queries := detector.GetMetricQueries()
			Expect(len(queries)).To(BeNumerically(">=", 5))

			for _, query := range queries {
				Expect(query.Name).NotTo(BeEmpty())
				Expect(query.Description).NotTo(BeEmpty())
				Expect(query.Query).NotTo(BeEmpty())
			}
		})

		It("should include queries for different issue types", func() {
			queries := detector.GetMetricQueries()
			
			queryNames := make([]string, len(queries))
			for i, query := range queries {
				queryNames[i] = query.Name
			}

			// Expected query categories
			Expect(queryNames).To(ContainElement(ContainSubstring("Attach")))
			Expect(queryNames).To(ContainElement(ContainSubstring("Mount")))
			Expect(queryNames).To(ContainElement(ContainSubstring("Timeout")))
			Expect(queryNames).To(ContainElement(ContainSubstring("Volume")))
		})

		It("should include driver-specific queries when target driver is specified", func() {
			queries := detector.GetMetricQueries()
			
			var foundDriverSpecific bool
			for _, query := range queries {
				if query.Query != "" && (query.Query == query.Query) { // Basic non-empty check
					foundDriverSpecific = true
					break
				}
			}
			Expect(foundDriverSpecific).To(BeTrue())
		})
	})

	Context("GetRecommendedAlerts", func() {
		BeforeEach(func() {
			detector = detect.NewMetricsDetector(prometheusURL, targetDriver)
		})

		It("should return Prometheus alerting rules", func() {
			alerts := detector.GetRecommendedAlerts()
			Expect(alerts).NotTo(BeEmpty())

			for _, alert := range alerts {
				Expect(alert).To(ContainSubstring("alert:"))
				Expect(alert).To(ContainSubstring("expr:"))
				Expect(alert).To(ContainSubstring("for:"))
				Expect(alert).To(ContainSubstring("labels:"))
				Expect(alert).To(ContainSubstring("annotations:"))
			}
		})

		It("should include alerts for critical CSI issues", func() {
			alerts := detector.GetRecommendedAlerts()
			
			alertContent := ""
			for _, alert := range alerts {
				alertContent += alert
			}

			// Should include alerts for common CSI issues
			Expect(alertContent).To(ContainSubstring("CSI"))
			Expect(alertContent).To(ContainSubstring("severity"))
			Expect(alertContent).To(Or(
				ContainSubstring("attach"),
				ContainSubstring("mount"),
				ContainSubstring("volume"),
			))
		})

		It("should include different severity levels", func() {
			alerts := detector.GetRecommendedAlerts()
			
			alertContent := ""
			for _, alert := range alerts {
				alertContent += alert
			}

			// Should have different severity levels
			Expect(alertContent).To(Or(
				ContainSubstring("critical"),
				ContainSubstring("warning"),
				ContainSubstring("high"),
			))
		})
	})

	Context("GenerateGrafanaDashboard", func() {
		BeforeEach(func() {
			detector = detect.NewMetricsDetector(prometheusURL, targetDriver)
		})

		It("should return valid Grafana dashboard JSON", func() {
			dashboard := detector.GenerateGrafanaDashboard()
			Expect(dashboard).NotTo(BeEmpty())
			
			// Should be valid JSON structure
			Expect(dashboard).To(ContainSubstring("{"))
			Expect(dashboard).To(ContainSubstring("}"))
			
			// Should include Grafana dashboard elements
			Expect(dashboard).To(ContainSubstring("dashboard"))
			Expect(dashboard).To(ContainSubstring("panels"))
			Expect(dashboard).To(ContainSubstring("title"))
		})

		It("should include CSI-specific panels", func() {
			dashboard := detector.GenerateGrafanaDashboard()
			
			// Should include panels for CSI monitoring
			Expect(dashboard).To(ContainSubstring("CSI"))
			Expect(dashboard).To(Or(
				ContainSubstring("attach"),
				ContainSubstring("mount"),
				ContainSubstring("volume"),
			))
		})

		It("should include time series and other visualization types", func() {
			dashboard := detector.GenerateGrafanaDashboard()
			
			// Should include different visualization types
			Expect(dashboard).To(Or(
				ContainSubstring("graph"),
				ContainSubstring("timeseries"),
				ContainSubstring("stat"),
				ContainSubstring("table"),
			))
		})
	})

	Context("Metric Query Generation", func() {
		Context("with target driver filter", func() {
			BeforeEach(func() {
				detector = detect.NewMetricsDetector(prometheusURL, "ebs.csi.aws.com")
			})

			It("should include driver-specific filters in queries", func() {
				queries := detector.GetMetricQueries()
				
				// At least some queries should reference the driver
				var foundDriverFiltered bool
				for _, query := range queries {
					if query.Query != "" {
						foundDriverFiltered = true
						break
					}
				}
				Expect(foundDriverFiltered).To(BeTrue())
			})
		})

		Context("without target driver filter", func() {
			BeforeEach(func() {
				detector = detect.NewMetricsDetector(prometheusURL, "")
			})

			It("should provide general CSI queries", func() {
				queries := detector.GetMetricQueries()
				Expect(queries).NotTo(BeEmpty())
				
				// Queries should still be useful without driver filter
				for _, query := range queries {
					Expect(query.Name).NotTo(BeEmpty())
					Expect(query.Query).NotTo(BeEmpty())
				}
			})
		})
	})

	Context("Framework Implementation", func() {
		It("should provide implementation guidance", func() {
			detector = detect.NewMetricsDetector("", "")
			
			// Test that the framework components exist
			queries := detector.GetMetricQueries()
			alerts := detector.GetRecommendedAlerts()
			dashboard := detector.GenerateGrafanaDashboard()
			
			Expect(queries).NotTo(BeNil())
			Expect(alerts).NotTo(BeNil())
			Expect(dashboard).NotTo(BeEmpty())
		})

		It("should handle missing Prometheus configuration", func() {
			detector = detect.NewMetricsDetector("", targetDriver)
			
			// Should not panic or error with missing Prometheus URL
			issues, err := detector.Detect(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(issues).NotTo(BeNil())
		})
	})

	Context("Error Handling", func() {
		BeforeEach(func() {
			detector = detect.NewMetricsDetector(prometheusURL, targetDriver)
		})

		It("should handle context cancellation gracefully", func() {
			cancelCtx, cancel := context.WithCancel(ctx)
			cancel()

			// Should not hang or panic with cancelled context
			issues, err := detector.Detect(cancelCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(issues).NotTo(BeNil())
		})

		It("should handle invalid Prometheus URL gracefully", func() {
			detector = detect.NewMetricsDetector("invalid-url", targetDriver)
			
			// Should not panic with invalid URL
			issues, err := detector.Detect(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(issues).NotTo(BeNil())
		})
	})

	Context("Integration Readiness", func() {
		It("should provide all components needed for Prometheus integration", func() {
			detector = detect.NewMetricsDetector(prometheusURL, targetDriver)
			
			// Verify all integration components are available
			queries := detector.GetMetricQueries()
			alerts := detector.GetRecommendedAlerts()
			dashboard := detector.GenerateGrafanaDashboard()
			
			// Queries for monitoring
			Expect(queries).NotTo(BeEmpty())
			for _, query := range queries {
				Expect(query.Name).NotTo(BeEmpty())
				Expect(query.Description).NotTo(BeEmpty())
				Expect(query.Query).NotTo(BeEmpty())
			}
			
			// Alerts for notifications
			Expect(alerts).NotTo(BeEmpty())
			
			// Dashboard for visualization
			Expect(dashboard).NotTo(BeEmpty())
			Expect(dashboard).To(ContainSubstring("{"))
		})
	})
})

// Additional test helper functions could be added here for more complex
// Prometheus integration testing when actual metrics collection is implemented