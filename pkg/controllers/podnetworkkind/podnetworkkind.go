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

package podnetworkkind

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kubernetes-sigs/multi-network-api/apis/v1alpha1"
	v1alpha1client "github.com/kubernetes-sigs/multi-network-api/pkg/client/clientset/versioned/typed/apis/v1alpha1"
	v1alpha1networkkindinformers "github.com/kubernetes-sigs/multi-network-api/pkg/client/informers/externalversions/apis/v1alpha1"
	v1alpha1networkkindlisters "github.com/kubernetes-sigs/multi-network-api/pkg/client/listers/apis/v1alpha1"
	"github.com/kubernetes-sigs/multi-network-api/pkg/ruleswatcher"
	authv1 "k8s.io/api/authorization/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	v1apiextensionsinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions/apiextensions/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const (
	queueName = "pod_network_kind"
)

type reconciler interface {
	Reconcile(ctx context.Context, podNetworkKindName string) error
}

type PodNetworkKindController struct {
	customResourceDefinitionSynced cache.InformerSynced
	podNetworkKindSynced           cache.InformerSynced
	ruleWatcherSynced              cache.InformerSynced

	podNetworkKindLister v1alpha1networkkindlisters.PodNetworkKindLister

	podNetworkKindQueue workqueue.TypedRateLimitingInterface[string]

	podNetworkKindReconciler reconciler
}

func NewPodNetworkKindController(
	podNetworkKindInformer v1alpha1networkkindinformers.PodNetworkKindInformer,
	customResourceDefinitionInformer v1apiextensionsinformers.CustomResourceDefinitionInformer,
	networkKindClient v1alpha1client.PodNetworkKindInterface,
	ruleWatcher ruleswatcher.Interface,
	client clientset.Interface,
) (*PodNetworkKindController, error) {

	nkc := &PodNetworkKindController{
		customResourceDefinitionSynced: customResourceDefinitionInformer.Informer().HasSynced,
		podNetworkKindSynced:           podNetworkKindInformer.Informer().HasSynced,
		ruleWatcherSynced:              ruleWatcher.HasSynced,
		podNetworkKindLister:           podNetworkKindInformer.Lister(),
		podNetworkKindQueue: workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.DefaultTypedControllerRateLimiter[string](),
			workqueue.TypedRateLimitingQueueConfig[string]{Name: queueName},
		),
	}

	var err error
	nkc.podNetworkKindReconciler, err = NewPodNetworkKindReconciler(
		customResourceDefinitionInformer,
		podNetworkKindInformer.Lister(),
		networkKindClient,
		ruleWatcher,
		client,
	)
	if err != nil {
		return nil, err
	}

	if _, err := podNetworkKindInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    nkc.onPodNetworkKindEvent,
		UpdateFunc: func(oldObj, newObj interface{}) { nkc.onPodNetworkKindEvent(newObj) },
		DeleteFunc: nkc.onPodNetworkKindEvent,
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

	ruleWatcher.AddEventHandler(func(oldRules, newRules []authv1.ResourceRule) {
		nkc.enqueueAllPodNetworkKinds()
	})

	return nkc, nil
}

func (pnkc *PodNetworkKindController) onPodNetworkKindEvent(obj interface{}) {
	pnkc.enqueuePodNetworkKind(obj)

	pnk, ok := obj.(*v1alpha1.PodNetworkKind)
	if ok && pnk.Spec.DefaultPodNetworkKind != nil && *pnk.Spec.DefaultPodNetworkKind {
		pnkc.enqueueAllDefaultCandidates()
	}
}

func (pnkc *PodNetworkKindController) enqueueAllDefaultCandidates() {
	networkKinds, err := pnkc.podNetworkKindLister.List(labels.Everything())
	if err != nil {
		klog.Error(err, "Failed to list PodNetworkKinds")
		return
	}

	for _, nk := range networkKinds {
		if nk.Spec.DefaultPodNetworkKind != nil && *nk.Spec.DefaultPodNetworkKind {
			pnkc.podNetworkKindQueue.Add(nk.Name)
		}
	}
}

func (pnkc *PodNetworkKindController) enqueueAllPodNetworkKinds() {
	networkKinds, err := pnkc.podNetworkKindLister.List(labels.Everything())
	if err != nil {
		klog.Error(err, "Failed to list PodNetworkKinds")
		return
	}

	for _, nk := range networkKinds {
		pnkc.podNetworkKindQueue.Add(nk.Name)
	}
}

func (pnkc *PodNetworkKindController) enqueuePodNetworkKind(obj interface{}) {
	var networkKind *v1alpha1.PodNetworkKind
	switch t := obj.(type) {
	case *v1alpha1.PodNetworkKind:
		networkKind = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		networkKind, ok = t.Obj.(*v1alpha1.PodNetworkKind)
		if !ok {
			klog.Error(nil, "Expected PodNetworkKind in tombstone", "actual", fmt.Sprintf("%T", t.Obj))
			return
		}
	default:
		klog.Error(nil, "Expected PodNetworkKind", "actual", fmt.Sprintf("%T", obj))
		return
	}

	pnkc.podNetworkKindQueue.Add(networkKind.Name)
}

func (pnkc *PodNetworkKindController) onCustomResourceDefinitionEvent(oldObj, newObj interface{}) {
	for _, obj := range []interface{}{oldObj, newObj} {
		if obj == nil {
			continue
		}

		var crd *apiextensionsv1.CustomResourceDefinition
		switch t := obj.(type) {
		case *apiextensionsv1.CustomResourceDefinition:
			crd = t
		case cache.DeletedFinalStateUnknown:
			var ok bool
			crd, ok = t.Obj.(*apiextensionsv1.CustomResourceDefinition)
			if !ok {
				klog.Error(nil, "Expected CustomResourceDefinition in tombstone", "actual", fmt.Sprintf("%T", t.Obj))
				return
			}
		default:
			klog.Error(nil, "Expected CustomResourceDefinition", "actual", fmt.Sprintf("%T", obj))
			return
		}

		name := v1alpha1.GetPodNetworkKindName(crd.Spec.Group, crd.Spec.Names.Kind)
		pnkc.podNetworkKindQueue.Add(name)
	}
}

func (pnkc *PodNetworkKindController) Run(ctx context.Context, workers int) error {
	if !cache.WaitForNamedCacheSyncWithContext(ctx, pnkc.podNetworkKindSynced, pnkc.customResourceDefinitionSynced, pnkc.ruleWatcherSynced) {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	var wg sync.WaitGroup
	defer func() {
		klog.FromContext(ctx).Info("Shutting down network kind controller")
		pnkc.podNetworkKindQueue.ShutDown()
		wg.Wait()
	}()

	for i := 0; i < workers; i++ {
		wg.Go(func() {
			wait.UntilWithContext(ctx, pnkc.runWorker, time.Second)
		})
	}

	<-ctx.Done()

	return nil
}

func (pnkc *PodNetworkKindController) runWorker(ctx context.Context) {
	for pnkc.processNextWorkItem(ctx) {
	}
}

func (pnkc *PodNetworkKindController) processNextWorkItem(ctx context.Context) bool {
	key, shutdown := pnkc.podNetworkKindQueue.Get()
	if shutdown {
		return false
	}
	defer pnkc.podNetworkKindQueue.Done(key)

	err := pnkc.podNetworkKindReconciler.Reconcile(ctx, key)
	if err == nil {
		pnkc.podNetworkKindQueue.Forget(key)
		return true
	}

	runtime.HandleErrorWithContext(ctx, err, "Work item failed", "item", key)
	pnkc.podNetworkKindQueue.AddRateLimited(key)

	return true
}
