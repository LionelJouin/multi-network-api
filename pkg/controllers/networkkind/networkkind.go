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

package networkkind

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kubernetes-sigs/multi-network-api/apis/v1alpha1"
	v1alpha1client "github.com/kubernetes-sigs/multi-network-api/pkg/client/clientset/versioned/typed/apis/v1alpha1"
	v1alpha1networkkindinformers "github.com/kubernetes-sigs/multi-network-api/pkg/client/informers/externalversions/apis/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	v1apiextensionsinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const (
	queueName = "network_kind"
)

type NetworkKindController struct {
	customResourceDefinitionSynced cache.InformerSynced
	networkKindSynced              cache.InformerSynced

	networkKindQueue workqueue.TypedRateLimitingInterface[string]

	networkKindReconciler *NetworkKindReconciler
}

func NewNetworkKindController(
	networkKindInformer v1alpha1networkkindinformers.NetworkKindInformer,
	customResourceDefinitionInformer v1apiextensionsinformers.CustomResourceDefinitionInformer,
	networkKindClient v1alpha1client.NetworkKindInterface,
	client clientset.Interface,
) (*NetworkKindController, error) {

	nkc := &NetworkKindController{
		customResourceDefinitionSynced: customResourceDefinitionInformer.Informer().HasSynced,
		networkKindSynced:              networkKindInformer.Informer().HasSynced,
		networkKindQueue: workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.DefaultTypedControllerRateLimiter[string](),
			workqueue.TypedRateLimitingQueueConfig[string]{Name: queueName},
		),
	}

	var err error
	nkc.networkKindReconciler, err = NewNetworkKindReconciler(
		customResourceDefinitionInformer,
		networkKindInformer.Lister(),
		networkKindClient,
		client,
	)
	if err != nil {
		return nil, err
	}

	if _, err := networkKindInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    nkc.enqueueNetworkKind,
		UpdateFunc: func(_, newObj interface{}) { nkc.enqueueNetworkKind(newObj) },
		DeleteFunc: nkc.enqueueNetworkKind,
	}); err != nil {
		return nil, err
	}

	if _, err := customResourceDefinitionInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { nkc.onCustomResourceDefinitionEvent(nil, obj) },
		UpdateFunc: func(oldObj, newObj interface{}) { nkc.onCustomResourceDefinitionEvent(oldObj, newObj) },
		DeleteFunc: func(obj interface{}) { nkc.onCustomResourceDefinitionEvent(obj, nil) },
	}); err != nil {
		return nil, err
	}

	return nkc, nil
}

func (nkc *NetworkKindController) enqueueNetworkKind(obj interface{}) {
	networkKind, ok := obj.(*v1alpha1.NetworkKind)
	if !ok {
		klog.Error(nil, "Expected NetworkKind", "actual", fmt.Sprintf("%T", obj))
		return
	}

	nkc.networkKindQueue.Add(networkKind.Name)
}

func (nkc *NetworkKindController) onCustomResourceDefinitionEvent(oldObj, newObj interface{}) {
	var ok bool
	newName := ""
	oldName := ""

	var newCrd *apiextensionsv1.CustomResourceDefinition
	if newObj != nil {
		newCrd, ok = newObj.(*apiextensionsv1.CustomResourceDefinition)
		if !ok {
			klog.Error(nil, "Expected CustomResourceDefinition", "actual", fmt.Sprintf("%T", newObj))
			return
		}
		if newCrd != nil {
			newName = v1alpha1.GetNetworkKindName(newCrd.Spec.Group, newCrd.Spec.Names.Kind)
		}
	}

	var oldCrd *apiextensionsv1.CustomResourceDefinition
	if oldObj != nil {
		oldCrd, ok = oldObj.(*apiextensionsv1.CustomResourceDefinition)
		if !ok {
			klog.Error(nil, "Expected CustomResourceDefinition", "actual", fmt.Sprintf("%T", oldObj))
			return
		}

		if oldCrd != nil {
			oldName = v1alpha1.GetNetworkKindName(oldCrd.Spec.Group, oldCrd.Spec.Names.Kind)
		}
	}

	if newName != "" && newName != oldName {
		nkc.networkKindQueue.Add(oldName)
	}

	if newName != "" {
		nkc.networkKindQueue.Add(newName)
	}
}

func (nkc *NetworkKindController) Run(ctx context.Context, workers int) error {
	if !cache.WaitForNamedCacheSyncWithContext(ctx, nkc.networkKindSynced, nkc.customResourceDefinitionSynced) {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	var wg sync.WaitGroup
	defer func() {
		klog.FromContext(ctx).Info("Shutting down network kind controller")
		nkc.networkKindQueue.ShutDown()
		wg.Wait()
	}()

	for i := 0; i < workers; i++ {
		wg.Go(func() {
			wait.UntilWithContext(ctx, nkc.runWorker, time.Second)
		})
	}

	<-ctx.Done()

	return nil
}

func (nkc *NetworkKindController) runWorker(ctx context.Context) {
	for nkc.processNextWorkItem(ctx) {
	}
}

func (nkc *NetworkKindController) processNextWorkItem(ctx context.Context) bool {
	key, shutdown := nkc.networkKindQueue.Get()
	if shutdown {
		return false
	}
	defer nkc.networkKindQueue.Done(key)

	err := nkc.networkKindReconciler.Reconcile(ctx, key)
	if err == nil {
		nkc.networkKindQueue.Forget(key)
		return true
	}

	runtime.HandleErrorWithContext(ctx, err, "Work item failed", "item", key)
	nkc.networkKindQueue.AddRateLimited(key)

	return true
}
