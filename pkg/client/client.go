package client

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	storagev1client "k8s.io/client-go/kubernetes/typed/storage/v1"
	"k8s.io/client-go/rest"
)

// Client wraps the real Kubernetes client with our interface
type Client struct {
	clientset kubernetes.Interface
}

// NewClient creates a new client wrapper from a Kubernetes clientset
func NewClient(clientset kubernetes.Interface) *Client {
	return &Client{
		clientset: clientset,
	}
}

// NewClientFromConfig creates a new client wrapper from a REST config
func NewClientFromConfig(config *rest.Config) (*Client, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return NewClient(clientset), nil
}

// CoreV1 returns the CoreV1 interface
func (c *Client) CoreV1() CoreV1Interface {
	return &coreV1Client{client: c.clientset.CoreV1()}
}

// StorageV1 returns the StorageV1 interface
func (c *Client) StorageV1() StorageV1Interface {
	return &storageV1Client{client: c.clientset.StorageV1()}
}

// coreV1Client implements CoreV1Interface
type coreV1Client struct {
	client corev1client.CoreV1Interface
}

func (c *coreV1Client) Pods(namespace string) PodInterface {
	return &podClient{client: c.client.Pods(namespace)}
}

func (c *coreV1Client) PersistentVolumes() PersistentVolumeInterface {
	return &persistentVolumeClient{client: c.client.PersistentVolumes()}
}

func (c *coreV1Client) PersistentVolumeClaims(namespace string) PersistentVolumeClaimInterface {
	return &persistentVolumeClaimClient{client: c.client.PersistentVolumeClaims(namespace)}
}

func (c *coreV1Client) Events(namespace string) EventInterface {
	return &eventClient{client: c.client.Events(namespace)}
}

func (c *coreV1Client) Nodes() NodeInterface {
	return &nodeClient{client: c.client.Nodes()}
}

// storageV1Client implements StorageV1Interface
type storageV1Client struct {
	client storagev1client.StorageV1Interface
}

func (c *storageV1Client) VolumeAttachments() VolumeAttachmentInterface {
	return &volumeAttachmentClient{client: c.client.VolumeAttachments()}
}

func (c *storageV1Client) StorageClasses() StorageClassInterface {
	return &storageClassClient{client: c.client.StorageClasses()}
}

// podClient implements PodInterface
type podClient struct {
	client corev1client.PodInterface
}

func (c *podClient) List(ctx context.Context, opts metav1.ListOptions) (*corev1.PodList, error) {
	return c.client.List(ctx, opts)
}

func (c *podClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.Pod, error) {
	return c.client.Get(ctx, name, opts)
}

func (c *podClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return c.client.Watch(ctx, opts)
}

// persistentVolumeClient implements PersistentVolumeInterface
type persistentVolumeClient struct {
	client corev1client.PersistentVolumeInterface
}

func (c *persistentVolumeClient) List(ctx context.Context, opts metav1.ListOptions) (*corev1.PersistentVolumeList, error) {
	return c.client.List(ctx, opts)
}

func (c *persistentVolumeClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.PersistentVolume, error) {
	return c.client.Get(ctx, name, opts)
}

// persistentVolumeClaimClient implements PersistentVolumeClaimInterface
type persistentVolumeClaimClient struct {
	client corev1client.PersistentVolumeClaimInterface
}

func (c *persistentVolumeClaimClient) List(ctx context.Context, opts metav1.ListOptions) (*corev1.PersistentVolumeClaimList, error) {
	return c.client.List(ctx, opts)
}

func (c *persistentVolumeClaimClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.PersistentVolumeClaim, error) {
	return c.client.Get(ctx, name, opts)
}

// eventClient implements EventInterface
type eventClient struct {
	client corev1client.EventInterface
}

func (c *eventClient) List(ctx context.Context, opts metav1.ListOptions) (*corev1.EventList, error) {
	return c.client.List(ctx, opts)
}

func (c *eventClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return c.client.Watch(ctx, opts)
}

// nodeClient implements NodeInterface
type nodeClient struct {
	client corev1client.NodeInterface
}

func (c *nodeClient) List(ctx context.Context, opts metav1.ListOptions) (*corev1.NodeList, error) {
	return c.client.List(ctx, opts)
}

func (c *nodeClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.Node, error) {
	return c.client.Get(ctx, name, opts)
}

// volumeAttachmentClient implements VolumeAttachmentInterface
type volumeAttachmentClient struct {
	client storagev1client.VolumeAttachmentInterface
}

func (c *volumeAttachmentClient) List(ctx context.Context, opts metav1.ListOptions) (*storagev1.VolumeAttachmentList, error) {
	return c.client.List(ctx, opts)
}

func (c *volumeAttachmentClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*storagev1.VolumeAttachment, error) {
	return c.client.Get(ctx, name, opts)
}

func (c *volumeAttachmentClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return c.client.Delete(ctx, name, opts)
}

// storageClassClient implements StorageClassInterface
type storageClassClient struct {
	client storagev1client.StorageClassInterface
}

func (c *storageClassClient) List(ctx context.Context, opts metav1.ListOptions) (*storagev1.StorageClassList, error) {
	return c.client.List(ctx, opts)
}

func (c *storageClassClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*storagev1.StorageClass, error) {
	return c.client.Get(ctx, name, opts)
}