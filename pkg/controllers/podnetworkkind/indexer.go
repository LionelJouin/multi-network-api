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
	"github.com/kubernetes-sigs/multi-network-api/apis/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	// groupKindCustomResourceDefinitionIndex is the lookup name for the index function
	// which indexes by groupkind CustomResourceDefinition.
	groupKindCustomResourceDefinitionIndex = "groupKind"

	// implementationTypePodNetworkKindIndex is the lookup name for the index function
	// which indexes PodNetworkKind by its implementationType GroupKind.
	implementationTypePodNetworkKindIndex = "implementationType"
)

func AddCustomResourceDefinitionIndexers(indexer cache.Indexer) error {
	return indexer.AddIndexers(cache.Indexers{
		groupKindCustomResourceDefinitionIndex: func(obj interface{}) ([]string, error) {
			nk, ok := obj.(*apiextensionsv1.CustomResourceDefinition)
			if !ok {
				return nil, nil
			}
			return []string{v1alpha1.GroupKind(nk.Spec.Group, nk.Spec.Names.Kind)}, nil
		},
	})
}

func AddPodNetworkKindIndexers(indexer cache.Indexer) error {
	return indexer.AddIndexers(cache.Indexers{
		implementationTypePodNetworkKindIndex: func(obj interface{}) ([]string, error) {
			pnk, ok := obj.(*v1alpha1.PodNetworkKind)
			if !ok {
				return nil, nil
			}
			return []string{v1alpha1.GroupKind(pnk.Spec.ImplementationType.Group, pnk.Spec.ImplementationType.Kind)}, nil
		},
	})
}
