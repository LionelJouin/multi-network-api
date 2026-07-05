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

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/cache"
)

type PodNetwork struct {
	client             dynamic.ResourceInterface
	podNetworkInformer informers.GenericInformer
}

func NewPodNetwork(
	group string,
	kind string,
	mapper *restmapper.DeferredDiscoveryRESTMapper,
	dynamicClient dynamic.Interface,
) (*PodNetwork, error) {
	gk := schema.GroupKind{Group: group, Kind: kind}

	mapping, err := mapper.RESTMapping(gk)
	if err != nil {
		return nil, err
	}

	resourceInterface := dynamicClient.Resource(mapping.Resource)
	factory := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 0)

	podNetworkInformer := factory.ForResource(mapping.Resource)

	pn := &PodNetwork{
		client:             resourceInterface,
		podNetworkInformer: podNetworkInformer,
	}

	return pn, nil
}

func (pn *PodNetwork) Start(ctx context.Context) {
	pn.podNetworkInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) {},
		UpdateFunc: func(old, new interface{}) {},
		DeleteFunc: func(obj interface{}) {},
	})
}
