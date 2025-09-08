package detect_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/jdambly/kubectl-csi-scan/pkg/client/mocks"
	"github.com/jdambly/kubectl-csi-scan/pkg/detect"
	"github.com/jdambly/kubectl-csi-scan/pkg/types"
)

var _ = Describe("EventsDetector", func() {
	var (
		ctrl              *gomock.Controller
		mockClient        *mocks.MockKubernetesClient
		mockCoreV1        *mocks.MockCoreV1Interface
		mockEvents        *mocks.MockEventInterface
		detector          *detect.EventsDetector
		ctx               context.Context
		targetDriver      string
		lookbackDuration  time.Duration
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockClient = mocks.NewMockKubernetesClient(ctrl)
		mockCoreV1 = mocks.NewMockCoreV1Interface(ctrl)
		mockEvents = mocks.NewMockEventInterface(ctrl)
		ctx = context.Background()
		targetDriver = "test.csi.driver"
		lookbackDuration = 2 * time.Hour

		// Set up mock expectations
		mockClient.EXPECT().CoreV1().Return(mockCoreV1).AnyTimes()
		mockCoreV1.EXPECT().Events("").Return(mockEvents).AnyTimes()
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("NewEventsDetector", func() {
		It("should create detector with target driver and lookback", func() {
			detector = detect.NewEventsDetector(mockClient, targetDriver, lookbackDuration)
			Expect(detector).NotTo(BeNil())
		})

		It("should create detector with default lookback duration", func() {
			detector = detect.NewEventsDetector(mockClient, targetDriver, 0)
			Expect(detector).NotTo(BeNil())
		})

		It("should create detector without target driver", func() {
			detector = detect.NewEventsDetector(mockClient, "", lookbackDuration)
			Expect(detector).NotTo(BeNil())
		})
	})

	Context("Detect", func() {
		BeforeEach(func() {
			detector = detect.NewEventsDetector(mockClient, targetDriver, lookbackDuration)
		})

		Context("when no events exist", func() {
			It("should return no issues", func() {
				mockEvents.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(&corev1.EventList{}, nil)

				issues, err := detector.Detect(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(issues).To(BeEmpty())
			})
		})

		Context("when events exist but are too old", func() {
			It("should filter out old events", func() {
				oldTime := time.Now().Add(-24 * time.Hour)
				eventList := &corev1.EventList{
					Items: []corev1.Event{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "old-event",
								Namespace: "default",
							},
							Type:          "Warning",
							Reason:        "FailedAttachVolume",
							Message:       "Multi-Attach error for volume pvc-123",
							LastTimestamp: metav1.NewTime(oldTime),
							EventTime:     metav1.NewMicroTime(oldTime),
							Source: corev1.EventSource{
								Component: "attachdetach-controller",
							},
							InvolvedObject: corev1.ObjectReference{
								Kind: "Pod",
								Name: "test-pod",
							},
							Count: 1,
						},
					},
				}

				mockEvents.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(eventList, nil)

				issues, err := detector.Detect(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(issues).To(BeEmpty())
			})
		})

		Context("when Multi-Attach errors are detected", func() {
			It("should detect Multi-Attach error events", func() {
				recentTime := time.Now().Add(-30 * time.Minute)
				eventList := &corev1.EventList{
					Items: []corev1.Event{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "multi-attach-event",
								Namespace: "default",
							},
							Type:          "Warning",
							Reason:        "FailedAttachVolume",
							Message:       "Multi-Attach error for volume pvc-123",
							LastTimestamp: metav1.NewTime(recentTime),
							EventTime:     metav1.NewMicroTime(recentTime),
							Source: corev1.EventSource{
								Component: "attachdetach-controller",
							},
							InvolvedObject: corev1.ObjectReference{
								Kind: "Pod",
								Name: "test-pod",
							},
							Count: 3,
						},
					},
				}

				mockEvents.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(eventList, nil)

				issues, err := detector.Detect(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(issues).To(HaveLen(1))
				Expect(issues[0].Type).To(Equal(types.MultiAttachError))
				Expect(issues[0].Volume).To(Equal("pvc-123"))
				Expect(issues[0].Namespace).To(Equal("default"))
				Expect(issues[0].DetectedBy).To(Equal(types.EventsMethod))
				Expect(issues[0].Description).To(ContainSubstring("Multi-Attach error detected"))
				Expect(issues[0].Metadata).To(HaveKeyWithValue("count", "3"))
				Expect(issues[0].Metadata).To(HaveKeyWithValue("event_reason", "FailedAttachVolume"))
			})
		})

		Context("when FailedAttachVolume events are detected", func() {
			It("should detect FailedAttachVolume events", func() {
				recentTime := time.Now().Add(-15 * time.Minute)
				eventList := &corev1.EventList{
					Items: []corev1.Event{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "failed-attach-event",
								Namespace: "kube-system",
							},
							Type:          "Warning",
							Reason:        "FailedAttachVolume",
							Message:       "AttachVolume.Attach failed for volume \"pv-456\" : rpc error: code = Aborted",
							LastTimestamp: metav1.NewTime(recentTime),
							EventTime:     metav1.NewMicroTime(recentTime),
							Source: corev1.EventSource{
								Component: "attachdetach-controller",
							},
							InvolvedObject: corev1.ObjectReference{
								Kind: "Pod",
								Name: "worker-pod",
							},
							Count: 5,
						},
					},
				}

				mockEvents.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(eventList, nil)

				issues, err := detector.Detect(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(issues).To(HaveLen(1))
				Expect(issues[0].Type).To(Equal(types.FailedAttachVolume))
				Expect(issues[0].Volume).To(Equal("pv-456"))
				Expect(issues[0].Namespace).To(Equal("kube-system"))
				Expect(issues[0].DetectedBy).To(Equal(types.EventsMethod))
				Expect(issues[0].Severity).To(Equal(types.SeverityMedium))
				Expect(issues[0].Description).To(ContainSubstring("Failed to attach volume"))
			})
		})

		Context("when FailedMount events are detected", func() {
			It("should detect GetDeviceMountRefs related mount failures", func() {
				recentTime := time.Now().Add(-45 * time.Minute)
				eventList := &corev1.EventList{
					Items: []corev1.Event{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "failed-mount-event",
								Namespace: "production",
							},
							Type:          "Warning",
							Reason:        "FailedMount",
							Message:       "MountVolume.SetUp failed for volume \"pvc-789\" : GetDeviceMountRefs returned error",
							LastTimestamp: metav1.NewTime(recentTime),
							EventTime:     metav1.NewMicroTime(recentTime),
							Source: corev1.EventSource{
								Component: "kubelet",
							},
							InvolvedObject: corev1.ObjectReference{
								Kind: "Pod",
								Name: "app-pod",
							},
							Count: 7,
						},
					},
				}

				mockEvents.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(eventList, nil)

				issues, err := detector.Detect(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(issues).To(HaveLen(1))
				Expect(issues[0].Type).To(Equal(types.StuckMountReference))
				Expect(issues[0].Volume).To(Equal("pvc-789"))
				Expect(issues[0].Namespace).To(Equal("production"))
				Expect(issues[0].DetectedBy).To(Equal(types.EventsMethod))
				Expect(issues[0].Severity).To(Equal(types.SeverityHigh))
				Expect(issues[0].Description).To(ContainSubstring("Mount reference cleanup failure"))
			})

			It("should detect general FailedMount events", func() {
				recentTime := time.Now().Add(-20 * time.Minute)
				eventList := &corev1.EventList{
					Items: []corev1.Event{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "general-mount-failure",
								Namespace: "default",
							},
							Type:          "Warning",
							Reason:        "FailedMount",
							Message:       "MountVolume.SetUp failed for volume \"pvc-general\" : mount failed",
							LastTimestamp: metav1.NewTime(recentTime),
							EventTime:     metav1.NewMicroTime(recentTime),
							Source: corev1.EventSource{
								Component: "kubelet",
							},
							InvolvedObject: corev1.ObjectReference{
								Kind: "Pod",
								Name: "general-pod",
							},
							Count: 2,
						},
					},
				}

				mockEvents.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(eventList, nil)

				issues, err := detector.Detect(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(issues).To(HaveLen(1))
				Expect(issues[0].Type).To(Equal(types.CSIOperationFailure))
				Expect(issues[0].Volume).To(Equal("pvc-general"))
				Expect(issues[0].Namespace).To(Equal("default"))
				Expect(issues[0].DetectedBy).To(Equal(types.EventsMethod))
				Expect(issues[0].Description).To(ContainSubstring("Failed to mount volume"))
			})
		})

		Context("when filtering by target driver", func() {
			It("should filter events by target driver name in message", func() {
				recentTime := time.Now().Add(-30 * time.Minute)
				eventList := &corev1.EventList{
					Items: []corev1.Event{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "other-driver-event",
								Namespace: "default",
							},
							Type:          "Warning",
							Reason:        "FailedAttachVolume",
							Message:       "AttachVolume.Attach failed for volume: other.csi.driver error",
							LastTimestamp: metav1.NewTime(recentTime),
							EventTime:     metav1.NewMicroTime(recentTime),
							Source: corev1.EventSource{
								Component: "attachdetach-controller",
							},
							InvolvedObject: corev1.ObjectReference{
								Kind: "Pod",
								Name: "other-pod",
							},
							Count: 1,
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "target-driver-event",
								Namespace: "default",
							},
							Type:          "Warning",
							Reason:        "FailedAttachVolume",
							Message:       "AttachVolume.Attach failed for volume: test.csi.driver error",
							LastTimestamp: metav1.NewTime(recentTime),
							EventTime:     metav1.NewMicroTime(recentTime),
							Source: corev1.EventSource{
								Component: "attachdetach-controller",
							},
							InvolvedObject: corev1.ObjectReference{
								Kind: "Pod",
								Name: "target-pod",
							},
							Count: 1,
						},
					},
				}

				mockEvents.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(eventList, nil)

				issues, err := detector.Detect(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(issues).To(HaveLen(1))
				Expect(issues[0].Description).To(ContainSubstring("test.csi.driver"))
			})

			It("should include CSI-related events when target driver is specified", func() {
				recentTime := time.Now().Add(-30 * time.Minute)
				eventList := &corev1.EventList{
					Items: []corev1.Event{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "csi-event",
								Namespace: "default",
							},
							Type:          "Warning",
							Reason:        "CSIVolumeExpansionFailed",
							Message:       "CSI volume expansion failed",
							LastTimestamp: metav1.NewTime(recentTime),
							EventTime:     metav1.NewMicroTime(recentTime),
							Source: corev1.EventSource{
								Component: "external-resizer",
							},
							InvolvedObject: corev1.ObjectReference{
								Kind: "PersistentVolumeClaim",
								Name: "test-pvc",
							},
							Count: 1,
						},
					},
				}

				mockEvents.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(eventList, nil)

				issues, err := detector.Detect(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(issues).To(HaveLen(1))
				Expect(issues[0].Type).To(Equal(types.CSIOperationFailure))
			})
		})

		Context("when no target driver specified", func() {
			BeforeEach(func() {
				detector = detect.NewEventsDetector(mockClient, "", lookbackDuration)
			})

			It("should detect issues from all volume-related events", func() {
				recentTime := time.Now().Add(-30 * time.Minute)
				eventList := &corev1.EventList{
					Items: []corev1.Event{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "any-volume-event",
								Namespace: "default",
							},
							Type:          "Warning",
							Reason:        "VolumeResizeFailed",
							Message:       "Volume resize failed for any driver",
							LastTimestamp: metav1.NewTime(recentTime),
							EventTime:     metav1.NewMicroTime(recentTime),
							Source: corev1.EventSource{
								Component: "external-resizer",
							},
							InvolvedObject: corev1.ObjectReference{
								Kind: "PersistentVolumeClaim",
								Name: "any-pvc",
							},
							Count: 1,
						},
					},
				}

				mockEvents.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(eventList, nil)

				issues, err := detector.Detect(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(issues).To(HaveLen(1))
				Expect(issues[0].Type).To(Equal(types.CSIOperationFailure))
			})
		})

		Context("GetRecentEvents", func() {
			BeforeEach(func() {
				detector = detect.NewEventsDetector(mockClient, targetDriver, lookbackDuration)
			})

			It("should return recent volume-related events", func() {
				recentTime := time.Now().Add(-30 * time.Minute)
				eventList := &corev1.EventList{
					Items: []corev1.Event{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "volume-event-1",
								Namespace: "default",
							},
							Type:          "Warning",
							Reason:        "FailedMount",
							Message:       "Mount failed",
							LastTimestamp: metav1.NewTime(recentTime),
							EventTime:     metav1.NewMicroTime(recentTime),
							Source: corev1.EventSource{
								Component: "kubelet",
							},
							InvolvedObject: corev1.ObjectReference{
								Kind: "Pod",
								Name: "test-pod-1",
							},
							Count: 1,
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "volume-event-2",
								Namespace: "kube-system",
							},
							Type:          "Normal",
							Reason:        "SuccessfulAttachVolume",
							Message:       "AttachVolume.Attach succeeded",
							LastTimestamp: metav1.NewTime(recentTime),
							EventTime:     metav1.NewMicroTime(recentTime),
							Source: corev1.EventSource{
								Component: "attachdetach-controller",
							},
							InvolvedObject: corev1.ObjectReference{
								Kind: "Pod",
								Name: "test-pod-2",
							},
							Count: 1,
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "non-volume-event",
								Namespace: "default",
							},
							Type:          "Warning",
							Reason:        "Failed",
							Message:       "Some other failure",
							LastTimestamp: metav1.NewTime(recentTime),
							EventTime:     metav1.NewMicroTime(recentTime),
							Source: corev1.EventSource{
								Component: "scheduler",
							},
							InvolvedObject: corev1.ObjectReference{
								Kind: "Pod",
								Name: "test-pod-3",
							},
							Count: 1,
						},
					},
				}

				mockEvents.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(eventList, nil)

				events, err := detector.GetRecentEvents(ctx, 10)
				Expect(err).NotTo(HaveOccurred())
				Expect(events).To(HaveLen(2))

				var mountEvent, attachEvent *types.EventInfo
				for i := range events {
					if events[i].Reason == "FailedMount" {
						mountEvent = &events[i]
					} else if events[i].Reason == "SuccessfulAttachVolume" {
						attachEvent = &events[i]
					}
				}

				Expect(mountEvent).NotTo(BeNil())
				Expect(mountEvent.Type).To(Equal("Warning"))
				Expect(mountEvent.Namespace).To(Equal("default"))
				Expect(mountEvent.Object).To(Equal("Pod/test-pod-1"))

				Expect(attachEvent).NotTo(BeNil())
				Expect(attachEvent.Type).To(Equal("Normal"))
				Expect(attachEvent.Namespace).To(Equal("kube-system"))
				Expect(attachEvent.Object).To(Equal("Pod/test-pod-2"))
			})

			It("should limit results to maxResults", func() {
				recentTime := time.Now().Add(-30 * time.Minute)
				var eventItems []corev1.Event
				for i := 0; i < 10; i++ {
					event := corev1.Event{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("volume-event-%d", i),
							Namespace: "default",
						},
						Type:          "Warning",
						Reason:        "FailedMount",
						Message:       "Mount failed",
						LastTimestamp: metav1.NewTime(recentTime),
						EventTime:     metav1.NewMicroTime(recentTime),
						Source: corev1.EventSource{
							Component: "kubelet",
						},
						InvolvedObject: corev1.ObjectReference{
							Kind: "Pod",
							Name: fmt.Sprintf("test-pod-%d", i),
						},
						Count: 1,
					}
					eventItems = append(eventItems, event)
				}

				eventList := &corev1.EventList{Items: eventItems}

				mockEvents.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(eventList, nil)

				events, err := detector.GetRecentEvents(ctx, 5)
				Expect(err).NotTo(HaveOccurred())
				Expect(events).To(HaveLen(5))
			})
		})

		Context("error handling", func() {
			BeforeEach(func() {
				detector = detect.NewEventsDetector(mockClient, targetDriver, lookbackDuration)
			})

			It("should handle Events API errors gracefully", func() {
				mockEvents.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(nil, &testError{msg: "Events API error"})

				issues, err := detector.Detect(ctx)
				Expect(err).To(HaveOccurred())
				Expect(err).To(MatchError(ContainSubstring("Events API error")))
				Expect(issues).To(BeNil())
			})

			It("should handle context cancellation", func() {
				cancelCtx, cancel := context.WithCancel(ctx)
				cancel()

				mockEvents.EXPECT().
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
			detector = detect.NewEventsDetector(mockClient, targetDriver, lookbackDuration)
		})

		DescribeTable("event severity calculation",
			func(count int32, message string, expectedSeverity types.IssueSeverity) {
				recentTime := time.Now().Add(-30 * time.Minute)
				eventList := &corev1.EventList{
					Items: []corev1.Event{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "severity-test-event",
								Namespace: "default",
							},
							Type:          "Warning",
							Reason:        "FailedAttachVolume",
							Message:       message,
							LastTimestamp: metav1.NewTime(recentTime),
							EventTime:     metav1.NewMicroTime(recentTime),
							Source: corev1.EventSource{
								Component: "attachdetach-controller",
							},
							InvolvedObject: corev1.ObjectReference{
								Kind: "Pod",
								Name: "test-pod",
							},
							Count: count,
						},
					},
				}

				mockEvents.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(eventList, nil)

				issues, err := detector.Detect(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(issues).To(HaveLen(1))
				Expect(issues[0].Severity).To(Equal(expectedSeverity))
			},
			Entry("single occurrence", int32(1), "AttachVolume failed", types.SeverityLow),
			Entry("few occurrences", int32(3), "AttachVolume failed", types.SeverityMedium),
			Entry("many occurrences", int32(8), "AttachVolume failed", types.SeverityHigh),
			Entry("very many occurrences", int32(15), "AttachVolume failed", types.SeverityCritical),
			Entry("Multi-Attach error", int32(2), "Multi-Attach error for volume", types.SeverityHigh),
			Entry("GetDeviceMountRefs error", int32(1), "GetDeviceMountRefs returned error", types.SeverityHigh),
		)
	})

	Context("Volume and Driver Extraction", func() {
		BeforeEach(func() {
			detector = detect.NewEventsDetector(mockClient, "", lookbackDuration)
		})

		DescribeTable("volume extraction from messages",
			func(message, expectedVolume string) {
				recentTime := time.Now().Add(-30 * time.Minute)
				eventList := &corev1.EventList{
					Items: []corev1.Event{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "extraction-test",
								Namespace: "default",
							},
							Type:          "Warning",
							Reason:        "FailedAttachVolume",
							Message:       message,
							LastTimestamp: metav1.NewTime(recentTime),
							EventTime:     metav1.NewMicroTime(recentTime),
							Source: corev1.EventSource{
								Component: "attachdetach-controller",
							},
							InvolvedObject: corev1.ObjectReference{
								Kind: "Pod",
								Name: "test-pod",
							},
							Count: 1,
						},
					},
				}

				mockEvents.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(eventList, nil)

				issues, err := detector.Detect(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(issues).To(HaveLen(1))
				Expect(issues[0].Volume).To(Equal(expectedVolume))
			},
			Entry("PVC pattern", "AttachVolume.Attach failed for volume pvc-123abc", "pvc-123abc"),
			Entry("quoted volume", "AttachVolume failed for volume \"my-volume\"", "my-volume"),
			Entry("complex message", "Failed to mount volume \"pvc-456def\" on node", "pvc-456def"),
			Entry("no volume found", "Some generic error message", "unknown"),
		)

		DescribeTable("driver extraction from messages",
			func(message, expectedDriver string) {
				recentTime := time.Now().Add(-30 * time.Minute)
				eventList := &corev1.EventList{
					Items: []corev1.Event{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "driver-extraction-test",
								Namespace: "default",
							},
							Type:          "Warning",
							Reason:        "FailedAttachVolume",
							Message:       message,
							LastTimestamp: metav1.NewTime(recentTime),
							EventTime:     metav1.NewMicroTime(recentTime),
							Source: corev1.EventSource{
								Component: "attachdetach-controller",
							},
							InvolvedObject: corev1.ObjectReference{
								Kind: "Pod",
								Name: "test-pod",
							},
							Count: 1,
						},
					},
				}

				mockEvents.EXPECT().
					List(ctx, metav1.ListOptions{}).
					Return(eventList, nil)

				issues, err := detector.Detect(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(issues).To(HaveLen(1))
				Expect(issues[0].Driver).To(Equal(expectedDriver))
			},
			Entry("EBS CSI driver", "AttachVolume failed: ebs.csi.aws.com error", "ebs.csi.aws.com"),
			Entry("Ceph RBD driver", "Mount failed: rook-ceph.rbd.csi.ceph.com timeout", "rook-ceph.rbd.csi.ceph.com"),
			Entry("Azure disk driver", "Volume error: disk.csi.azure.com unavailable", "disk.csi.azure.com"),
			Entry("unknown driver", "Some generic volume error", "unknown"),
		)
	})
})