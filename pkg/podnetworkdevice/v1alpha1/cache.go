/*
Copyright (c) 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/kubernetes-sigs/multi-network-api/apis/v1alpha1"
	v1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	coreinformers "k8s.io/client-go/informers/core/v1"
	resourceinformers "k8s.io/client-go/informers/resource/v1"
	v1listers "k8s.io/client-go/listers/core/v1"
	v1resourcelisters "k8s.io/client-go/listers/resource/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/dynamic-resource-allocation/structured"
	"k8s.io/klog/v2"
)

const (
	queueName = "pod_network_device_cache"

	claimDriverPoolIndex = "claimDriverPool"
	deviceIDSliceIndex   = "deviceIDSlice"
)

// PodNetworkDeviceCache watches Pods, ResourceClaims, and ResourceSlices
// and maintains a precomputed joined view of pods with their network devices.
// It exposes an informer-like interface via a synthetic SharedIndexInformer
// backed by a ListerWatcher.
type PodNetworkDeviceCache struct {
	informer cache.SharedIndexInformer
	watcher  *watch.Broadcaster

	podLister           v1listers.PodLister
	resourceClaimLister v1resourcelisters.ResourceClaimLister

	resourceClaimIndexer cache.Indexer
	resourceSliceIndexer cache.Indexer

	podSynced           cache.InformerSynced
	resourceClaimSynced cache.InformerSynced
	resourceSliceSynced cache.InformerSynced

	queue workqueue.TypedRateLimitingInterface[string]

	mu       sync.RWMutex
	snapshot map[string]*PodNetworkDevice
}

// NewPodNetworkDeviceCache creates a PodNetworkDeviceCache. The source informers must not have
// been started yet (indexers must be added before start).
func NewPodNetworkDeviceCache(
	podInformer coreinformers.PodInformer,
	resourceClaimInformer resourceinformers.ResourceClaimInformer,
	resourceSliceInformer resourceinformers.ResourceSliceInformer,
) (*PodNetworkDeviceCache, error) {
	pndc := &PodNetworkDeviceCache{
		podLister:            podInformer.Lister(),
		resourceClaimLister:  resourceClaimInformer.Lister(),
		resourceClaimIndexer: resourceClaimInformer.Informer().GetIndexer(),
		resourceSliceIndexer: resourceSliceInformer.Informer().GetIndexer(),
		podSynced:            podInformer.Informer().HasSynced,
		resourceClaimSynced:  resourceClaimInformer.Informer().HasSynced,
		resourceSliceSynced:  resourceSliceInformer.Informer().HasSynced,
		queue: workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.DefaultTypedControllerRateLimiter[string](),
			workqueue.TypedRateLimitingQueueConfig[string]{Name: queueName},
		),
	}

	pndc.watcher = watch.NewBroadcaster(1000, watch.DropIfChannelFull)

	pndc.informer = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
				return nil, nil
			},
			WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
				return pndc.watcher.Watch()
			},
		},
		&PodNetworkDevice{},
		0,
		cache.Indexers{},
	)

	if _, err := podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { pndc.onPodEvent(nil, obj) },
		UpdateFunc: pndc.onPodEvent,
		DeleteFunc: func(obj interface{}) { pndc.onPodEvent(obj, nil) },
	}); err != nil {
		return nil, fmt.Errorf("failed to add Pod event handler: %w", err)
	}

	if _, err := resourceClaimInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { pndc.onResourceClaimEvent(nil, obj) },
		UpdateFunc: pndc.onResourceClaimEvent,
		DeleteFunc: func(obj interface{}) { pndc.onResourceClaimEvent(obj, nil) },
	}); err != nil {
		return nil, fmt.Errorf("failed to add ResourceClaim event handler: %w", err)
	}

	if _, err := resourceSliceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { pndc.onResourceSliceEvent(nil, obj) },
		UpdateFunc: pndc.onResourceSliceEvent,
		DeleteFunc: func(obj interface{}) { pndc.onResourceSliceEvent(obj, nil) },
	}); err != nil {
		return nil, fmt.Errorf("failed to add ResourceSlice event handler: %w", err)
	}

	return pndc, nil
}

func (pndc *PodNetworkDeviceCache) runWorker(ctx context.Context) {
	for pndc.processNextWorkItem(ctx) {
	}
}

func (pndc *PodNetworkDeviceCache) processNextWorkItem(ctx context.Context) bool {
	key, shutdown := pndc.queue.Get()
	if shutdown {
		return false
	}
	defer pndc.queue.Done(key)

	if err := pndc.syncPodNetworkDevice(key); err != nil {
		klog.FromContext(ctx).Error(err, "Failed to sync pod network device", "key", key)
		pndc.queue.AddRateLimited(key)
		return true
	}

	pndc.queue.Forget(key)
	return true
}

// Run starts the cache. It blocks until ctx is cancelled.
func (pndc *PodNetworkDeviceCache) Run(ctx context.Context, workers int) error {
	if !cache.WaitForNamedCacheSyncWithContext(
		ctx,
		pndc.podSynced,
		pndc.resourceClaimSynced,
		pndc.resourceSliceSynced,
	) {
		return fmt.Errorf("failed to wait for source informer caches to sync")
	}

	go pndc.informer.RunWithContext(ctx)

	if !cache.WaitForCacheSync(ctx.Done(), pndc.informer.HasSynced) {
		return fmt.Errorf("failed to wait for synthetic informer cache to sync")
	}

	var wg sync.WaitGroup
	defer func() {
		klog.FromContext(ctx).Info("Shutting down pod network device cache")
		pndc.queue.ShutDown()
		wg.Wait()
		pndc.watcher.Shutdown()
	}()

	for i := 0; i < workers; i++ {
		wg.Go(func() {
			wait.UntilWithContext(ctx, pndc.runWorker, time.Second)
		})
	}

	<-ctx.Done()
	return nil
}

// Informer returns the underlying SharedIndexInformer.
func (pndc *PodNetworkDeviceCache) Informer() cache.SharedIndexInformer {
	return pndc.informer
}

// AddEventHandler registers an event handler with the synthetic informer.
func (pndc *PodNetworkDeviceCache) AddEventHandler(handler cache.ResourceEventHandlerFuncs) (cache.ResourceEventHandlerRegistration, error) {
	return pndc.informer.AddEventHandler(handler)
}

// HasSynced returns true once the cache has completed its initial sync.
func (pndc *PodNetworkDeviceCache) HasSynced() bool {
	return pndc.informer.HasSynced()
}

// Get returns the PodNetworkDevices for a specific pod.
func (pndc *PodNetworkDeviceCache) Get(namespace, name string) (*PodNetworkDevice, error) {
	key := name
	if namespace != "" {
		key = namespace + "/" + name
	}

	obj, exists, err := pndc.informer.GetStore().GetByKey(key)
	if err != nil {
		return nil, fmt.Errorf("error getting pod network device for pod %s/%s: %w", namespace, name, err)
	}
	if !exists {
		return nil, fmt.Errorf("pod network device for pod %s/%s not found", namespace, name)
	}

	pnd, ok := obj.(*PodNetworkDevice)
	if !ok {
		return nil, fmt.Errorf("unexpected type for pod network device for pod %s/%s", namespace, name)
	}

	return pnd, nil
}

// List returns all PodNetworkDevices matching the given options.
func (pndc *PodNetworkDeviceCache) List() []*PodNetworkDevice {
	result := []*PodNetworkDevice{}

	for _, obj := range pndc.informer.GetStore().List() {
		pnd, ok := obj.(*PodNetworkDevice)
		if !ok {
			continue
		}

		result = append(result, pnd)
	}

	return result
}

// syncPodNetworkDevice recomputes the PodNetworkDevice for a specific pod and updates the cache and informer.
func (pndc *PodNetworkDeviceCache) syncPodNetworkDevice(podKey string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(podKey)
	if err != nil {
		return fmt.Errorf("invalid key %q: %w", podKey, err)
	}

	pod, err := pndc.podLister.Pods(namespace).Get(name)
	if errors.IsNotFound(err) {
		pndc.mu.Lock()
		old, existed := pndc.snapshot[podKey]
		delete(pndc.snapshot, podKey)
		pndc.mu.Unlock()
		if existed {
			pndc.watcher.Action(watch.Deleted, old)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get pod %s: %w", podKey, err)
	}

	newEntry := pndc.buildPodNetworkDevice(pod)

	pndc.mu.Lock()
	old, existed := pndc.snapshot[podKey]
	if len(newEntry.Devices) > 0 {
		pndc.snapshot[podKey] = newEntry
	} else {
		delete(pndc.snapshot, podKey)
	}
	pndc.mu.Unlock()

	switch {
	case !existed && len(newEntry.Devices) > 0:
		pndc.watcher.Action(watch.Added, newEntry)
	case existed && len(newEntry.Devices) == 0:
		pndc.watcher.Action(watch.Deleted, old)
	case existed && len(newEntry.Devices) > 0:
		if !reflect.DeepEqual(old.Devices, newEntry.Devices) {
			pndc.watcher.Action(watch.Modified, newEntry)
		}
	}

	return nil
}

// buildPodNetworkDevice constructs a PodNetworkDevice for the given pod.
func (pndc *PodNetworkDeviceCache) buildPodNetworkDevice(pod *v1.Pod) *PodNetworkDevice {
	result := &PodNetworkDevice{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PodNetworkDevices",
			APIVersion: "multinetwork.networking.k8s.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			UID:       pod.UID,
			Labels:    pod.Labels,
		},
		Devices: make(map[string]*Device),
	}

	for _, claimStatus := range pod.Status.ResourceClaimStatuses {
		claimName := claimStatus.Name // todo
		if claimStatus.ResourceClaimName != nil {
			claimName = *claimStatus.ResourceClaimName
		}

		claim, err := pndc.resourceClaimLister.ResourceClaims(pod.Namespace).Get(claimName)
		if err != nil {
			continue
		}

		for i, allocDevice := range claim.Status.Devices {
			if allocDevice.NetworkData == nil {
				continue
			}

			sliceDevice := pndc.findSliceDevice(allocDevice.Driver, allocDevice.Pool, allocDevice.Device)

			dev := &Device{
				AllocatedDeviceStatus: &claim.Status.Devices[i],
				Device:                sliceDevice,
				PodNetworkRef:         &PodNetworkRef{},
			}

			if sliceDevice == nil {
				continue
			}
			attr, ok := sliceDevice.Attributes[v1alpha1.StandardDeviceAttributeNetworkKind]
			if !ok || attr.StringValue == nil {
				continue
			}
			dev.PodNetworkRef.Kind = *attr.StringValue

			attr, ok = sliceDevice.Attributes[v1alpha1.StandardDeviceAttributePodNetwork]
			if !ok || attr.StringValue == nil {
				continue
			}
			dev.PodNetworkRef.Name = *attr.StringValue

			attr, ok = sliceDevice.Attributes[v1alpha1.StandardDeviceAttributePodNetworkNamespace]
			if ok && attr.StringValue == nil { // namespace of PodNetwork is optional
				continue
			}
			dev.PodNetworkRef.Namespace = attr.StringValue

			deviceID := structured.MakeSharedDeviceID(
				structured.MakeDeviceID(allocDevice.Driver, allocDevice.Pool, allocDevice.Device),
				(*types.UID)(allocDevice.ShareID))
			result.Devices[deviceID.String()] = dev
		}
	}

	return result
}

func (pndc *PodNetworkDeviceCache) findSliceDevice(driver, pool, deviceName string) *resourcev1.Device {
	key := structured.MakeDeviceID(driver, pool, deviceName).String()
	objs, err := pndc.resourceSliceIndexer.ByIndex(deviceIDSliceIndex, key)
	if err != nil || len(objs) == 0 {
		return nil
	}

	slice, ok := objs[0].(*resourcev1.ResourceSlice)
	if !ok {
		return nil
	}

	for i, dev := range slice.Spec.Devices {
		if dev.Name == deviceName {
			return &slice.Spec.Devices[i]
		}
	}

	return nil
}

func (pndc *PodNetworkDeviceCache) onPodEvent(oldObj, newObj interface{}) {
	for _, obj := range []interface{}{oldObj, newObj} {
		if obj == nil {
			continue
		}
		pod, ok := obj.(*v1.Pod)
		if !ok {
			continue
		}
		if len(pod.Status.ResourceClaimStatuses) == 0 {
			continue
		}

		key := queueKey(pod.Namespace, pod.Name)
		pndc.queue.Add(key)
	}
}

func (pndc *PodNetworkDeviceCache) onResourceClaimEvent(oldObj, newObj interface{}) {
	for _, obj := range []interface{}{oldObj, newObj} {
		if obj == nil {
			continue
		}
		claim, ok := obj.(*resourcev1.ResourceClaim)
		if !ok {
			continue
		}
		if len(claim.Status.Devices) == 0 {
			continue
		}
		if len(claim.Status.ReservedFor) != 1 {
			continue
		}
		if claim.Status.ReservedFor[0].Resource != "pods" {
			continue
		}
		key := queueKey(claim.Namespace, claim.Status.ReservedFor[0].Name)
		pndc.queue.Add(key)
	}
}

func (pndc *PodNetworkDeviceCache) onResourceSliceEvent(oldObj, newObj interface{}) {
	for _, obj := range []interface{}{oldObj, newObj} {
		if obj == nil {
			continue
		}
		slice, ok := obj.(*resourcev1.ResourceSlice)
		if !ok {
			continue
		}
		claims, err := pndc.resourceClaimIndexer.ByIndex(
			claimDriverPoolIndex,
			claimDriverPoolKey(slice.Spec.Driver, slice.Spec.Pool.Name))
		if err != nil {
			continue
		}
		for _, objClaim := range claims {
			pndc.onResourceClaimEvent(nil, objClaim)
		}
	}
}

// claimDriverPoolIndexFunc indexes ResourceClaims by the driver/pool pairs
// of their allocated devices that have network data.
func claimDriverPoolIndexFunc(obj interface{}) ([]string, error) {
	claim, ok := obj.(*resourcev1.ResourceClaim)
	if !ok {
		return nil, nil
	}

	keys := sets.New[string]()
	for _, device := range claim.Status.Devices {
		if device.NetworkData == nil {
			continue
		}
		keys.Insert(device.Driver + "/" + device.Pool)
	}
	return sets.List(keys), nil
}

// deviceIDSliceIndexFunc indexes ResourceSlices by Device IDs
// for each device they contain.
func deviceIDSliceIndexFunc(obj interface{}) ([]string, error) {
	slice, ok := obj.(*resourcev1.ResourceSlice)
	if !ok {
		return nil, nil
	}

	keys := make([]string, 0, len(slice.Spec.Devices))
	for _, dev := range slice.Spec.Devices {
		keys = append(keys, deviceIDKey(slice.Spec.Driver, slice.Spec.Pool.Name, dev.Name))
	}
	return keys, nil
}

func queueKey(podNamespace string, podName string) string {
	return fmt.Sprintf("%s/%s", podNamespace, podName)
}

func deviceIDKey(driver, pool, device string) string {
	return structured.MakeDeviceID(driver, pool, device).String()
}

func claimDriverPoolKey(driver, pool string) string {
	return fmt.Sprintf("%s/%s", driver, pool)
}
