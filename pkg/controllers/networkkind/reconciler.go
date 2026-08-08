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

	"github.com/kubernetes-sigs/multi-network-api/apis/v1alpha1"
	v1alpha1client "github.com/kubernetes-sigs/multi-network-api/pkg/client/clientset/versioned/typed/apis/v1alpha1"
	v1alpha1networkkindlisters "github.com/kubernetes-sigs/multi-network-api/pkg/client/listers/apis/v1alpha1"
	authv1 "k8s.io/api/authorization/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	v1apiextensionsinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/set"
)

const (
	// groupKindCustomResourceDefinitionIndex is the lookup name for the index function
	// which indexes by groupkind CustomResourceDefinition.
	groupKindCustomResourceDefinitionIndex = "groupKind"
)

type NetworkKindReconciler struct {
	customResourceDefinitionIndexer cache.Indexer
	networkKindLister               v1alpha1networkkindlisters.NetworkKindLister
	networkKindClient               v1alpha1client.NetworkKindInterface
	clientset                       clientset.Interface
}

func NewNetworkKindReconciler(
	customResourceDefinitionInformer v1apiextensionsinformers.CustomResourceDefinitionInformer,
	networkKindLister v1alpha1networkkindlisters.NetworkKindLister,
	networkKindClient v1alpha1client.NetworkKindInterface,
	clientset clientset.Interface,
) (*NetworkKindReconciler, error) {
	nkr := &NetworkKindReconciler{
		customResourceDefinitionIndexer: customResourceDefinitionInformer.Informer().GetIndexer(),
		networkKindLister:               networkKindLister,
		networkKindClient:               networkKindClient,
		clientset:                       clientset,
	}

	err := nkr.customResourceDefinitionIndexer.AddIndexers(cache.Indexers{
		groupKindCustomResourceDefinitionIndex: func(obj interface{}) ([]string, error) {
			nk, ok := obj.(*apiextensionsv1.CustomResourceDefinition)
			if !ok {
				return nil, nil
			}

			return []string{v1alpha1.GroupKind(nk.Spec.Group, nk.Spec.Names.Kind)}, nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to add indexer for NetworkKind: %w", err)
	}

	return nkr, nil
}

func (nkr *NetworkKindReconciler) Reconcile(ctx context.Context, networkKindName string) error {
	networkKind, err := nkr.networkKindLister.Get(networkKindName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get NetworkKind: %w", err)
	}
	oldNetworkKind := networkKind.DeepCopy()

	objs, err := nkr.customResourceDefinitionIndexer.ByIndex(
		groupKindCustomResourceDefinitionIndex,
		v1alpha1.GroupKind(networkKind.Spec.ImplementationType.Group, networkKind.Spec.ImplementationType.Kind),
	)
	if err != nil {
		return fmt.Errorf("failed to get CustomResourceDefinition: %w", err)
	}

	var crd *apiextensionsv1.CustomResourceDefinition
	if len(objs) == 1 {
		var ok bool
		crd, ok = objs[0].(*apiextensionsv1.CustomResourceDefinition)
		if !ok {
			return fmt.Errorf("Unexpected type for CustomResourceDefinition: %T", objs[0])
		}
	}

	err = nkr.setConditions(ctx, networkKind, crd)
	if err != nil {
		return fmt.Errorf("failed to set conditions: %w", err)
	}

	if hasStatusChanged(oldNetworkKind, networkKind) {
		_, err := nkr.networkKindClient.UpdateStatus(ctx, networkKind, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update NetworkKind status: %w", err)
		}
	}

	return nil
}

func (nkr *NetworkKindReconciler) setConditions(ctx context.Context, networkKind *v1alpha1.NetworkKind, crd *apiextensionsv1.CustomResourceDefinition) error {
	networkKind.Status.Conditions = []metav1.Condition{
		{
			Type:               v1alpha1.NetworkKindConditionImplementationTypeReady,
			Status:             metav1.ConditionTrue,
			Message:            "",
			ObservedGeneration: networkKind.ObjectMeta.Generation,
			LastTransitionTime: metav1.Now(),
			Reason:             v1alpha1.NetworkKindReasonCompliant,
		},
	}

	if crd == nil {
		networkKind.Status.Conditions[0].Status = metav1.ConditionFalse
		networkKind.Status.Conditions[0].Message = "CRD not found"
		networkKind.Status.Conditions[0].Reason = v1alpha1.NetworkKindReasonCRDNotFound
		return nil
	}

	ready, message := crdReady(crd)
	if !ready {
		networkKind.Status.Conditions[0].Status = metav1.ConditionFalse
		networkKind.Status.Conditions[0].Message = message
		networkKind.Status.Conditions[0].Reason = v1alpha1.NetworkKindReasonCRDNotReady
		return nil
	}

	allowed, err := nkr.hasPermissions(ctx, crd)
	if err != nil {
		return err
	}
	if !allowed {
		networkKind.Status.Conditions[0].Status = metav1.ConditionFalse
		networkKind.Status.Conditions[0].Message = "Insufficient permissions to access the Custom Resources"
		networkKind.Status.Conditions[0].Reason = v1alpha1.NetworkKindReasonMissingRBAC
		return nil
	}

	return nil
}

func crdReady(crd *apiextensionsv1.CustomResourceDefinition) (bool, string) {
	conditionTypes := set.New(apiextensionsv1.Established, apiextensionsv1.NamesAccepted)
	for _, condition := range crd.Status.Conditions {
		if condition.Type == apiextensionsv1.Established {
			if condition.Status != apiextensionsv1.ConditionTrue {
				return false, "CRD is not established"
			}
			conditionTypes.Delete(apiextensionsv1.Established)
		}
		if condition.Type == apiextensionsv1.NamesAccepted {
			if condition.Status != apiextensionsv1.ConditionTrue {
				return false, "CRD names are not accepted"
			}
			conditionTypes.Delete(apiextensionsv1.NamesAccepted)
		}
	}
	if conditionTypes.Len() > 0 {
		return false, fmt.Sprintf("CRD is missing conditions: %v", conditionTypes.UnsortedList())
	}

	crdCategories := set.New(crd.Spec.Names.Categories...)
	for _, category := range v1alpha1.Categories {
		if !crdCategories.Has(category) {
			return false, fmt.Sprintf("CRD is missing mandatory category %s", category)
		}
	}

	return true, ""
}

func hasStatusChanged(oldNetworkKind *v1alpha1.NetworkKind, newNetworkKind *v1alpha1.NetworkKind) bool {
	if len(oldNetworkKind.Status.Conditions) != len(newNetworkKind.Status.Conditions) {
		return true
	}

	for i, oldCondition := range oldNetworkKind.Status.Conditions {
		newCondition := newNetworkKind.Status.Conditions[i]
		if oldCondition.Type != newCondition.Type ||
			oldCondition.Status != newCondition.Status ||
			oldCondition.Reason != newCondition.Reason ||
			oldCondition.Message != newCondition.Message {
			return true
		}
	}
	return false
}

func (nkr *NetworkKindReconciler) hasPermissions(ctx context.Context, crd *apiextensionsv1.CustomResourceDefinition) (bool, error) {
	review := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Group:    crd.Spec.Group,
				Resource: crd.Spec.Names.Plural,
				Verb:     "list",
			},
		},
	}

	result, err := nkr.clientset.AuthorizationV1().
		SelfSubjectAccessReviews().
		Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to create SelfSubjectAccessReview: %w", err)
	}

	return result.Status.Allowed, nil
}
