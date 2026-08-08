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

package endpointslice

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kubernetes-sigs/multi-network-api/pkg/podnetworkdevice/v1alpha1"
	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	coreinformers "k8s.io/client-go/informers/core/v1"
	discoveryinformers "k8s.io/client-go/informers/discovery/v1"
	clientset "k8s.io/client-go/kubernetes"
	v1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const (
	queueName = "network_kind"

	ServiceLabelPodNetworkKind      = "multinetwork.networking.k8s.io/spec.podnetwork.kind"
	ServiceLabelPodNetworkName      = "multinetwork.networking.k8s.io/spec.podnetwork.name"
	ServiceLabelPodNetworkNamespace = "multinetwork.networking.k8s.io/spec.podnetwork.namespace"

	DummyLabel = "multinetwork.networking.k8s.io/dummy"
)

type EndpointSliceController struct {
	serviceQueue workqueue.TypedRateLimitingInterface[string]

	serviceLister v1listers.ServiceLister

	endpointSliceSynced         cache.InformerSynced
	serviceSynced               cache.InformerSynced
	podNetworkDeviceCacheSynced cache.InformerSynced

	endpointSliceReconciler *EndpointSliceReconciler
}

func NewEndpointSliceController(
	endpointSliceInformer discoveryinformers.EndpointSliceInformer,
	serviceInformer coreinformers.ServiceInformer,
	podNetworkDeviceCache *v1alpha1.PodNetworkDeviceCache,
	clientset clientset.Interface,
) (*EndpointSliceController, error) {
	esc := &EndpointSliceController{
		serviceLister:               serviceInformer.Lister(),
		endpointSliceSynced:         endpointSliceInformer.Informer().HasSynced,
		serviceSynced:               serviceInformer.Informer().HasSynced,
		podNetworkDeviceCacheSynced: podNetworkDeviceCache.HasSynced,
		serviceQueue: workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.DefaultTypedControllerRateLimiter[string](),
			workqueue.TypedRateLimitingQueueConfig[string]{Name: queueName},
		),
	}

	endpointSliceReconciler, err := NewEndpointSliceReconciler(
		serviceInformer.Lister(),
		endpointSliceInformer.Lister(),
		podNetworkDeviceCache,
		clientset,
	)
	if err != nil {
		return nil, err
	}
	esc.endpointSliceReconciler = endpointSliceReconciler

	if _, err := endpointSliceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { esc.onEndpointSliceEvent(nil, obj) },
		UpdateFunc: esc.onEndpointSliceEvent,
		DeleteFunc: func(obj interface{}) { esc.onEndpointSliceEvent(obj, nil) },
	}); err != nil {
		return nil, err
	}

	if _, err := serviceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    esc.onServiceEvent,
		UpdateFunc: func(_, newObj interface{}) { esc.onServiceEvent(newObj) },
		DeleteFunc: esc.onServiceEvent,
	}); err != nil {
		return nil, err
	}

	if _, err := podNetworkDeviceCache.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { esc.onPodNetworkDeviceEvent(nil, obj) },
		UpdateFunc: esc.onPodNetworkDeviceEvent,
		DeleteFunc: func(obj interface{}) { esc.onPodNetworkDeviceEvent(obj, nil) },
	}); err != nil {
		return nil, err
	}

	return esc, nil
}

func (esc *EndpointSliceController) onEndpointSliceEvent(oldObj, newObj interface{}) {
	for _, obj := range []interface{}{oldObj, newObj} {
		if obj == nil {
			continue
		}

		endpointSlice, ok := obj.(*discoveryv1.EndpointSlice)
		if !ok {
			klog.Error(nil, "Expected EndpointSlice", "actual", fmt.Sprintf("%T", obj))
			return
		}

		service, ok := endpointSlice.Labels[discoveryv1.LabelServiceName]
		if !ok {
			return
		}

		esc.serviceQueue.Add(queueKey(endpointSlice.Namespace, service))
	}
}

func (esc *EndpointSliceController) onServiceEvent(obj interface{}) {
	service, ok := obj.(*v1.Service)
	if !ok {
		klog.Error(nil, "Expected Service", "actual", fmt.Sprintf("%T", obj))
		return
	}

	esc.serviceQueue.Add(queueKey(service.Namespace, service.Name))
}

func (esc *EndpointSliceController) onPodNetworkDeviceEvent(oldObj, newObj interface{}) {
	for _, obj := range []interface{}{oldObj, newObj} {
		if obj == nil {
			continue
		}

		podNetworkDevice, ok := obj.(*v1alpha1.PodNetworkDevice)
		if !ok {
			klog.Error(nil, "Expected PodNetworkDevice", "actual", fmt.Sprintf("%T", obj))
			return
		}

		serviceKeys, err := getServicesForPodNetworkDevices(esc.serviceLister, podNetworkDevice)
		if err != nil {
			klog.Error(nil, "Failed to get services for PodNetworkDevice", "error", err)
			return
		}

		for serviceKey := range serviceKeys {
			esc.serviceQueue.Add(serviceKey)
		}
	}
}

func (esc *EndpointSliceController) Run(ctx context.Context, workers int) error {
	if !cache.WaitForNamedCacheSyncWithContext(
		ctx,
		esc.endpointSliceSynced,
		esc.serviceSynced,
		esc.podNetworkDeviceCacheSynced,
	) {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	var wg sync.WaitGroup
	defer func() {
		klog.FromContext(ctx).Info("Shutting down endpoint slice controller")
		esc.serviceQueue.ShutDown()
		wg.Wait()
	}()

	for i := 0; i < workers; i++ {
		wg.Go(func() {
			wait.UntilWithContext(ctx, esc.runWorker, time.Second)
		})
	}

	<-ctx.Done()

	return nil
}

func (esc *EndpointSliceController) runWorker(ctx context.Context) {
	for esc.processNextWorkItem(ctx) {
	}
}

func (esc *EndpointSliceController) processNextWorkItem(ctx context.Context) bool {
	key, shutdown := esc.serviceQueue.Get()
	if shutdown {
		return false
	}
	defer esc.serviceQueue.Done(key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleErrorWithContext(ctx, err, "Work item failed", "item", key)
		return true
	}
	err = esc.endpointSliceReconciler.Reconcile(ctx, name, namespace)
	if err == nil {
		esc.serviceQueue.Forget(key)
		return true
	}

	runtime.HandleErrorWithContext(ctx, err, "Work item failed", "item", key)
	esc.serviceQueue.AddRateLimited(key)

	return true
}

func queueKey(serviceNamespace string, serviceName string) string {
	return fmt.Sprintf("%s/%s", serviceNamespace, serviceName)
}
