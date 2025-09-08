package detect_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/jdambly/kubectl-csi-scan/pkg/client/mocks"
	"github.com/jdambly/kubectl-csi-scan/pkg/detect"
	"github.com/jdambly/kubectl-csi-scan/pkg/types"
)

var _ = Describe("VolumeAttachmentDetector", func() {
	var (
		ctrl                     *gomock.Controller
		mockClient               *mocks.MockKubernetesClient
		mockStorageV1            *mocks.MockStorageV1Interface
		mockVolumeAttachments    *mocks.MockVolumeAttachmentInterface
		detector                 *detect.VolumeAttachmentDetector
		ctx                      context.Context
		targetDriver             string
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockClient = mocks.NewMockKubernetesClient(ctrl)
		mockStorageV1 = mocks.NewMockStorageV1Interface(ctrl)
		mockVolumeAttachments = mocks.NewMockVolumeAttachmentInterface(ctrl)
		ctx = context.Background()
		targetDriver = "test.csi.driver"

		// Set up mock expectations
		mockClient.EXPECT().StorageV1().Return(mockStorageV1).AnyTimes()
		mockStorageV1.EXPECT().VolumeAttachments().Return(mockVolumeAttachments).AnyTimes()
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("NewVolumeAttachmentDetector", func() {
		It("should create detector with target driver", func() {
			detector = detect.NewVolumeAttachmentDetector(mockClient, targetDriver)
			Expect(detector).NotTo(BeNil())
		})

		It("should create detector without target driver", func() {
			detector = detect.NewVolumeAttachmentDetector(mockClient, "")
			Expect(detector).NotTo(BeNil())
		})
	})

	Context("Detect", func() {
		BeforeEach(func() {
			detector = detect.NewVolumeAttachmentDetector(mockClient, targetDriver)
		})

		Context("when no VolumeAttachments exist", func() {
			It("should return no issues", func() {
				mockVolumeAttachments.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(&storagev1.VolumeAttachmentList{}, nil)

				issues, err := detector.Detect(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(issues).To(BeEmpty())
			})
		})

		Context("when VolumeAttachments exist but no issues", func() {
			It("should return no issues for properly attached volumes", func() {
				vaList := &storagev1.VolumeAttachmentList{
					Items: []storagev1.VolumeAttachment{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "test-va-1",
							},
							Spec: storagev1.VolumeAttachmentSpec{
								Attacher: targetDriver,
								NodeName: "node-1",
								Source: storagev1.VolumeAttachmentSource{
									PersistentVolumeName: stringPtr("pv-1"),
								},
							},
							Status: storagev1.VolumeAttachmentStatus{
								Attached: true,
							},
						},
					},
				}

				mockVolumeAttachments.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(vaList, nil)

				issues, err := detector.Detect(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(issues).To(BeEmpty())
			})
		})

		Context("when stuck attachment issues exist", func() {
			It("should detect volume stuck in attaching state", func() {
				vaList := &storagev1.VolumeAttachmentList{
					Items: []storagev1.VolumeAttachment{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:              "stuck-va",
								CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
							},
							Spec: storagev1.VolumeAttachmentSpec{
								Attacher: targetDriver,
								NodeName: "node-1",
								Source: storagev1.VolumeAttachmentSource{
									PersistentVolumeName: stringPtr("stuck-pv"),
								},
							},
							Status: storagev1.VolumeAttachmentStatus{
								Attached: false, // Stuck in attaching state
							},
						},
					},
				}

				mockVolumeAttachments.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(vaList, nil)

				issues, err := detector.Detect(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(issues).To(HaveLen(1))
				Expect(issues[0].Type).To(Equal(types.StuckVolumeAttachment))
				Expect(issues[0].Volume).To(Equal("stuck-pv"))
				Expect(issues[0].Node).To(Equal("node-1"))
				Expect(issues[0].DetectedBy).To(Equal(types.VolumeAttachmentMethod))
			})

			It("should detect multiple attachments for same volume", func() {
				vaList := &storagev1.VolumeAttachmentList{
					Items: []storagev1.VolumeAttachment{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "multi-va-1",
							},
							Spec: storagev1.VolumeAttachmentSpec{
								Attacher: targetDriver,
								NodeName: "node-1",
								Source: storagev1.VolumeAttachmentSource{
									PersistentVolumeName: stringPtr("multi-pv"),
								},
							},
							Status: storagev1.VolumeAttachmentStatus{
								Attached: true,
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "multi-va-2",
							},
							Spec: storagev1.VolumeAttachmentSpec{
								Attacher: targetDriver,
								NodeName: "node-2",
								Source: storagev1.VolumeAttachmentSource{
									PersistentVolumeName: stringPtr("multi-pv"),
								},
							},
							Status: storagev1.VolumeAttachmentStatus{
								Attached: true,
							},
						},
					},
				}

				mockVolumeAttachments.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(vaList, nil)

				issues, err := detector.Detect(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(issues).To(HaveLen(1))
				Expect(issues[0].Type).To(Equal(types.MultipleAttachments))
				Expect(issues[0].Volume).To(Equal("multi-pv"))
				Expect(issues[0].DetectedBy).To(Equal(types.VolumeAttachmentMethod))
				Expect(issues[0].Description).To(ContainSubstring("multiple nodes"))
			})

			It("should detect attachment with errors", func() {
				vaList := &storagev1.VolumeAttachmentList{
					Items: []storagev1.VolumeAttachment{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "error-va",
							},
							Spec: storagev1.VolumeAttachmentSpec{
								Attacher: targetDriver,
								NodeName: "node-1",
								Source: storagev1.VolumeAttachmentSource{
									PersistentVolumeName: stringPtr("error-pv"),
								},
							},
							Status: storagev1.VolumeAttachmentStatus{
								Attached:    false,
								AttachError: &storagev1.VolumeError{
									Time:    metav1.NewTime(time.Now()),
									Message: "Failed to attach volume",
								},
							},
						},
					},
				}

				mockVolumeAttachments.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(vaList, nil)

				issues, err := detector.Detect(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(issues).To(HaveLen(1))
				Expect(issues[0].Type).To(Equal(types.FailedAttachVolume))
				Expect(issues[0].Volume).To(Equal("error-pv"))
				Expect(issues[0].Node).To(Equal("node-1"))
				Expect(issues[0].Description).To(ContainSubstring("Failed to attach volume"))
			})
		})

		Context("when detach issues exist", func() {
			It("should detect volume stuck in detaching state", func() {
				vaList := &storagev1.VolumeAttachmentList{
					Items: []storagev1.VolumeAttachment{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:              "detaching-va",
								DeletionTimestamp: &metav1.Time{Time: time.Now().Add(-1 * time.Hour)},
								Finalizers:        []string{"external-attacher/test-csi-driver"},
							},
							Spec: storagev1.VolumeAttachmentSpec{
								Attacher: targetDriver,
								NodeName: "node-1",
								Source: storagev1.VolumeAttachmentSource{
									PersistentVolumeName: stringPtr("detaching-pv"),
								},
							},
							Status: storagev1.VolumeAttachmentStatus{
								Attached: true,
								DetachError: &storagev1.VolumeError{
									Time:    metav1.NewTime(time.Now().Add(-30 * time.Minute)),
									Message: "Failed to detach volume",
								},
							},
						},
					},
				}

				mockVolumeAttachments.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(vaList, nil)

				issues, err := detector.Detect(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(issues).To(HaveLen(1))
				Expect(issues[0].Type).To(Equal(types.StuckVolumeDetachment))
				Expect(issues[0].Volume).To(Equal("detaching-pv"))
				Expect(issues[0].Node).To(Equal("node-1"))
				Expect(issues[0].Description).To(ContainSubstring("Failed to detach volume"))
			})
		})

		Context("when filtering by target driver", func() {
			It("should filter VolumeAttachments by target driver", func() {
				vaList := &storagev1.VolumeAttachmentList{
					Items: []storagev1.VolumeAttachment{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "other-driver-va",
							},
							Spec: storagev1.VolumeAttachmentSpec{
								Attacher: "other.csi.driver",
								NodeName: "node-1",
								Source: storagev1.VolumeAttachmentSource{
									PersistentVolumeName: stringPtr("other-pv"),
								},
							},
							Status: storagev1.VolumeAttachmentStatus{
								Attached: false, // Would be an issue if we weren't filtering
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:              "target-driver-va",
								CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
							},
							Spec: storagev1.VolumeAttachmentSpec{
								Attacher: targetDriver,
								NodeName: "node-1",
								Source: storagev1.VolumeAttachmentSource{
									PersistentVolumeName: stringPtr("target-pv"),
								},
							},
							Status: storagev1.VolumeAttachmentStatus{
								Attached: false, // This should be detected
							},
						},
					},
				}

				mockVolumeAttachments.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(vaList, nil)

				issues, err := detector.Detect(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(issues).To(HaveLen(1))
				Expect(issues[0].Volume).To(Equal("target-pv"))
				Expect(issues[0].Driver).To(Equal(targetDriver))
			})
		})

		Context("when no target driver specified", func() {
			BeforeEach(func() {
				detector = detect.NewVolumeAttachmentDetector(mockClient, "")
			})

			It("should detect issues from all CSI drivers", func() {
				vaList := &storagev1.VolumeAttachmentList{
					Items: []storagev1.VolumeAttachment{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:              "any-driver-va",
								CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
							},
							Spec: storagev1.VolumeAttachmentSpec{
								Attacher: "any.csi.driver",
								NodeName: "node-1",
								Source: storagev1.VolumeAttachmentSource{
									PersistentVolumeName: stringPtr("any-pv"),
								},
							},
							Status: storagev1.VolumeAttachmentStatus{
								Attached: false,
							},
						},
					},
				}

				mockVolumeAttachments.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(vaList, nil)

				issues, err := detector.Detect(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(issues).To(HaveLen(1))
				Expect(issues[0].Volume).To(Equal("any-pv"))
				Expect(issues[0].Driver).To(Equal("any.csi.driver"))
			})
		})

		Context("error handling", func() {
			BeforeEach(func() {
				detector = detect.NewVolumeAttachmentDetector(mockClient, targetDriver)
			})

			It("should handle API errors gracefully", func() {
				mockVolumeAttachments.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(nil, &testError{msg: "API server unavailable"})

				issues, err := detector.Detect(ctx)
				Expect(err).To(HaveOccurred())
				Expect(err).To(MatchError(ContainSubstring("API server unavailable")))
				Expect(issues).To(BeNil())
			})

			It("should handle context cancellation", func() {
				cancelCtx, cancel := context.WithCancel(ctx)
				cancel()

				mockVolumeAttachments.EXPECT().
					List(cancelCtx, metav1.ListOptions{}).
					Return(nil, &testError{msg: "context canceled"})

				issues, err := detector.Detect(cancelCtx)
				Expect(err).To(HaveOccurred())
				Expect(err).To(MatchError(ContainSubstring("context canceled")))
				Expect(issues).To(BeNil())
			})
		})
	})

	Context("Severity Calculation", func() {
		BeforeEach(func() {
			detector = detect.NewVolumeAttachmentDetector(mockClient, targetDriver)
		})

		It("should assign higher severity to older stuck attachments", func() {
			vaList := &storagev1.VolumeAttachmentList{
				Items: []storagev1.VolumeAttachment{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:              "very-old-va",
							CreationTimestamp: metav1.NewTime(time.Now().Add(-24 * time.Hour)),
						},
						Spec: storagev1.VolumeAttachmentSpec{
							Attacher: targetDriver,
							NodeName: "node-1",
							Source: storagev1.VolumeAttachmentSource{
								PersistentVolumeName: stringPtr("very-old-pv"),
							},
						},
						Status: storagev1.VolumeAttachmentStatus{
							Attached: false,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:              "recent-va",
							CreationTimestamp: metav1.NewTime(time.Now().Add(-1 * time.Hour)),
						},
						Spec: storagev1.VolumeAttachmentSpec{
							Attacher: targetDriver,
							NodeName: "node-2",
							Source: storagev1.VolumeAttachmentSource{
								PersistentVolumeName: stringPtr("recent-pv"),
							},
						},
						Status: storagev1.VolumeAttachmentStatus{
							Attached: false,
						},
					},
				},
			}

			mockVolumeAttachments.EXPECT().
				List(ctx, metav1.ListOptions{}).
				Return(vaList, nil)

			issues, err := detector.Detect(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(issues).To(HaveLen(2))

			// Find the very old issue
			var veryOldIssue, recentIssue *types.CSIMountIssue
			for i := range issues {
				if issues[i].Volume == "very-old-pv" {
					veryOldIssue = &issues[i]
				} else if issues[i].Volume == "recent-pv" {
					recentIssue = &issues[i]
				}
			}

			Expect(veryOldIssue).NotTo(BeNil())
			Expect(recentIssue).NotTo(BeNil())
			
			// Very old attachment should have higher severity
			Expect(veryOldIssue.Severity).To(Equal(types.SeverityCritical))
			Expect(recentIssue.Severity).To(Equal(types.SeverityMedium))
		})
	})

	Context("Metadata Extraction", func() {
		BeforeEach(func() {
			detector = detect.NewVolumeAttachmentDetector(mockClient, targetDriver)
		})

		It("should extract relevant metadata from VolumeAttachment", func() {
			vaList := &storagev1.VolumeAttachmentList{
				Items: []storagev1.VolumeAttachment{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:              "metadata-va",
							CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
							Annotations: map[string]string{
								"csi.alpha.kubernetes.io/node-id": "node-id-123",
							},
						},
						Spec: storagev1.VolumeAttachmentSpec{
							Attacher: targetDriver,
							NodeName: "node-1",
							Source: storagev1.VolumeAttachmentSource{
								PersistentVolumeName: stringPtr("metadata-pv"),
							},
						},
						Status: storagev1.VolumeAttachmentStatus{
							Attached: false,
						},
					},
				},
			}

			mockVolumeAttachments.EXPECT().
				List(ctx, metav1.ListOptions{}).
				Return(vaList, nil)

			issues, err := detector.Detect(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(issues).To(HaveLen(1))
			
			issue := issues[0]
			Expect(issue.Metadata).To(HaveKey("volume_attachment_name"))
			Expect(issue.Metadata["volume_attachment_name"]).To(Equal("metadata-va"))
			Expect(issue.Metadata).To(HaveKey("age_hours"))
			Expect(issue.DetectedAt).To(BeTemporally("~", time.Now(), time.Minute))
		})
	})

	Context("Private Function Coverage", func() {
		BeforeEach(func() {
			detector = detect.NewVolumeAttachmentDetector(mockClient, "test.csi.driver")
		})

		It("should exercise private functions through inline volume specs", func() {
			// This test exercises getDriverName, getVolumeHandle, and matchesDriver
			// functions indirectly by using inline volume specs
			vaList := &storagev1.VolumeAttachmentList{
				Items: []storagev1.VolumeAttachment{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:              "inline-csi-va",
							CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
						},
						Spec: storagev1.VolumeAttachmentSpec{
							Attacher: "test.csi.driver",
							NodeName: "node-1",
							Source: storagev1.VolumeAttachmentSource{
								InlineVolumeSpec: &corev1.PersistentVolumeSpec{
									PersistentVolumeSource: corev1.PersistentVolumeSource{
										CSI: &corev1.CSIPersistentVolumeSource{
											Driver:       "test.csi.driver",
											VolumeHandle: "inline-vol-123",
										},
									},
								},
							},
						},
						Status: storagev1.VolumeAttachmentStatus{
							Attached: false, // Trigger stuck attachment issue
						},
					},
				},
			}

			mockVolumeAttachments.EXPECT().
				List(ctx, metav1.ListOptions{}).
				Return(vaList, nil)

			issues, err := detector.Detect(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(issues).To(HaveLen(1))
			
			// Verify the private functions worked correctly
			issue := issues[0]
			Expect(issue.Volume).To(Equal("inline-vol-123")) // getVolumeHandle worked
			Expect(issue.Driver).To(Equal("test.csi.driver")) // getDriverName worked
			// matchesDriver worked (issue was detected, not filtered out)
		})

		It("should filter out volumes from different drivers using inline specs", func() {
			// This exercises the matchesDriver function to return false
			vaList := &storagev1.VolumeAttachmentList{
				Items: []storagev1.VolumeAttachment{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:              "other-driver-va",
							CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
						},
						Spec: storagev1.VolumeAttachmentSpec{
							Attacher: "other.csi.driver",
							NodeName: "node-1",
							Source: storagev1.VolumeAttachmentSource{
								InlineVolumeSpec: &corev1.PersistentVolumeSpec{
									PersistentVolumeSource: corev1.PersistentVolumeSource{
										CSI: &corev1.CSIPersistentVolumeSource{
											Driver:       "other.csi.driver",
											VolumeHandle: "other-vol-123",
										},
									},
								},
							},
						},
						Status: storagev1.VolumeAttachmentStatus{
							Attached: false, // Would be an issue if not filtered
						},
					},
				},
			}

			mockVolumeAttachments.EXPECT().
				List(ctx, metav1.ListOptions{}).
				Return(vaList, nil)

			issues, err := detector.Detect(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(issues).To(BeEmpty()) // Should be filtered out by matchesDriver
		})

		It("should handle missing inline volume spec in getDriverName", func() {
			// This exercises getDriverName with no inline spec - should use fallback
			vaList := &storagev1.VolumeAttachmentList{
				Items: []storagev1.VolumeAttachment{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:              "pv-only-va",
							CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
						},
						Spec: storagev1.VolumeAttachmentSpec{
							Attacher: "test.csi.driver",
							NodeName: "node-1",
							Source: storagev1.VolumeAttachmentSource{
								PersistentVolumeName: stringPtr("pv-only-vol"),
							},
						},
						Status: storagev1.VolumeAttachmentStatus{
							Attached: false,
						},
					},
				},
			}

			mockVolumeAttachments.EXPECT().
				List(ctx, metav1.ListOptions{}).
				Return(vaList, nil)

			issues, err := detector.Detect(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(issues).To(HaveLen(1))
			
			// Should use target driver as fallback for getDriverName
			issue := issues[0]
			Expect(issue.Driver).To(Equal("test.csi.driver"))
			Expect(issue.Volume).To(Equal("pv-only-vol"))
		})

		It("should handle detector without target driver in getDriverName", func() {
			// Create detector without target driver
			noTargetDetector := detect.NewVolumeAttachmentDetector(mockClient, "")
			
			vaList := &storagev1.VolumeAttachmentList{
				Items: []storagev1.VolumeAttachment{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:              "no-target-va",
							CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
						},
						Spec: storagev1.VolumeAttachmentSpec{
							Attacher: "any.csi.driver",
							NodeName: "node-1",
							Source: storagev1.VolumeAttachmentSource{
								PersistentVolumeName: stringPtr("no-target-vol"),
							},
						},
						Status: storagev1.VolumeAttachmentStatus{
							Attached: false,
						},
					},
				},
			}

			mockVolumeAttachments.EXPECT().
				List(ctx, metav1.ListOptions{}).
				Return(vaList, nil)

			issues, err := noTargetDetector.Detect(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(issues).To(HaveLen(1))
			
			// Driver should come from Attacher field, not getDriverName function
			issue := issues[0]
			Expect(issue.Driver).To(Equal("any.csi.driver"))
			Expect(issue.Volume).To(Equal("no-target-vol"))
		})

		It("should handle non-CSI inline volume specs", func() {
			// This exercises the conservative behavior in the private functions
			vaList := &storagev1.VolumeAttachmentList{
				Items: []storagev1.VolumeAttachment{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:              "non-csi-va",
							CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
						},
						Spec: storagev1.VolumeAttachmentSpec{
							Attacher: "test.csi.driver",
							NodeName: "node-1",
							Source: storagev1.VolumeAttachmentSource{
								InlineVolumeSpec: &corev1.PersistentVolumeSpec{
									PersistentVolumeSource: corev1.PersistentVolumeSource{
										HostPath: &corev1.HostPathVolumeSource{
											Path: "/tmp/test",
										},
									},
								},
							},
						},
						Status: storagev1.VolumeAttachmentStatus{
							Attached: false,
						},
					},
				},
			}

			mockVolumeAttachments.EXPECT().
				List(ctx, metav1.ListOptions{}).
				Return(vaList, nil)

			issues, err := detector.Detect(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(issues).To(HaveLen(1))
			
			// Conservative behavior should include this volume
			issue := issues[0]
			Expect(issue.Volume).To(Equal("unknown")) // getVolumeHandle returned "unknown"
			Expect(issue.Driver).To(Equal("test.csi.driver")) // Used target driver
		})
	})
})

