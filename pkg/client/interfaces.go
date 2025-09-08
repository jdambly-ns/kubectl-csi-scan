package client

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

//go:generate mockgen -source=interfaces.go -destination=mocks/mock_interfaces.go -package=mocks

// KubernetesClient defines the interface for Kubernetes operations used by the CSI mount detective
type KubernetesClient interface {
	CoreV1() CoreV1Interface
	StorageV1() StorageV1Interface
}

// CoreV1Interface defines the interface for Core v1 API operations
type CoreV1Interface interface {
	Pods(namespace string) PodInterface
	PersistentVolumes() PersistentVolumeInterface
	PersistentVolumeClaims(namespace string) PersistentVolumeClaimInterface
	Events(namespace string) EventInterface
	Nodes() NodeInterface
}

// StorageV1Interface defines the interface for Storage v1 API operations
type StorageV1Interface interface {
	VolumeAttachments() VolumeAttachmentInterface
	StorageClasses() StorageClassInterface
}

// PodInterface defines the interface for Pod operations
type PodInterface interface {
	List(ctx context.Context, opts metav1.ListOptions) (*corev1.PodList, error)
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.Pod, error)
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
}

// PersistentVolumeInterface defines the interface for PersistentVolume operations
type PersistentVolumeInterface interface {
	List(ctx context.Context, opts metav1.ListOptions) (*corev1.PersistentVolumeList, error)
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.PersistentVolume, error)
}

// PersistentVolumeClaimInterface defines the interface for PersistentVolumeClaim operations
type PersistentVolumeClaimInterface interface {
	List(ctx context.Context, opts metav1.ListOptions) (*corev1.PersistentVolumeClaimList, error)
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.PersistentVolumeClaim, error)
}

// EventInterface defines the interface for Event operations
type EventInterface interface {
	List(ctx context.Context, opts metav1.ListOptions) (*corev1.EventList, error)
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
}

// NodeInterface defines the interface for Node operations
type NodeInterface interface {
	List(ctx context.Context, opts metav1.ListOptions) (*corev1.NodeList, error)
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.Node, error)
}

// VolumeAttachmentInterface defines the interface for VolumeAttachment operations
type VolumeAttachmentInterface interface {
	List(ctx context.Context, opts metav1.ListOptions) (*storagev1.VolumeAttachmentList, error)
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*storagev1.VolumeAttachment, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
}

// StorageClassInterface defines the interface for StorageClass operations
type StorageClassInterface interface {
	List(ctx context.Context, opts metav1.ListOptions) (*storagev1.StorageClassList, error)
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*storagev1.StorageClass, error)
}