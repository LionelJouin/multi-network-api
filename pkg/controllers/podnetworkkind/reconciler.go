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

type PodNetworkKindReconciler struct {
	customResourceDefinitionIndexer cache.Indexer
	podNetworkKindLister            v1alpha1networkkindlisters.PodNetworkKindLister
	podNetworkKindClient            v1alpha1client.PodNetworkKindInterface
	clientset                       clientset.Interface
}

func NewPodNetworkKindReconciler(
	customResourceDefinitionInformer v1apiextensionsinformers.CustomResourceDefinitionInformer,
	podNetworkKindLister v1alpha1networkkindlisters.PodNetworkKindLister,
	podNetworkKindClient v1alpha1client.PodNetworkKindInterface,
	clientset clientset.Interface,
) (*PodNetworkKindReconciler, error) {
	nkr := &PodNetworkKindReconciler{
		customResourceDefinitionIndexer: customResourceDefinitionInformer.Informer().GetIndexer(),
		podNetworkKindLister:            podNetworkKindLister,
		podNetworkKindClient:            podNetworkKindClient,
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
		return nil, fmt.Errorf("failed to add indexer for PodNetworkKind: %w", err)
	}

	return nkr, nil
}

func (pnkr *PodNetworkKindReconciler) Reconcile(ctx context.Context, podNetworkKindName string) error {
	podNetworkKind, err := pnkr.podNetworkKindLister.Get(podNetworkKindName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get PodNetworkKind: %w", err)
	}
	oldNetworkKind := podNetworkKind.DeepCopy()

	objs, err := pnkr.customResourceDefinitionIndexer.ByIndex(
		groupKindCustomResourceDefinitionIndex,
		v1alpha1.GroupKind(podNetworkKind.Spec.ImplementationType.Group, podNetworkKind.Spec.ImplementationType.Kind),
	)
	if err != nil {
		return fmt.Errorf("failed to get CustomResourceDefinition: %w", err)
	}

	var crd *apiextensionsv1.CustomResourceDefinition
	if len(objs) == 1 {
		var ok bool
		crd, ok = objs[0].(*apiextensionsv1.CustomResourceDefinition)
		if !ok {
			return fmt.Errorf("unexpected type for CustomResourceDefinition: %T", objs[0])
		}
	}

	err = pnkr.setConditions(ctx, podNetworkKind, crd)
	if err != nil {
		return fmt.Errorf("failed to set conditions: %w", err)
	}

	if hasStatusChanged(oldNetworkKind, podNetworkKind) {
		_, err := pnkr.podNetworkKindClient.UpdateStatus(ctx, podNetworkKind, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update PodNetworkKind status: %w", err)
		}
	}

	return nil
}

func (pnkr *PodNetworkKindReconciler) setConditions(ctx context.Context, podNetworkKind *v1alpha1.PodNetworkKind, crd *apiextensionsv1.CustomResourceDefinition) error {
	podNetworkKind.Status.Conditions = []metav1.Condition{
		{
			Type:               v1alpha1.PodNetworkKindConditionImplementationTypeReady,
			Status:             metav1.ConditionTrue,
			Message:            "",
			ObservedGeneration: podNetworkKind.ObjectMeta.Generation,
			LastTransitionTime: metav1.Now(),
			Reason:             v1alpha1.PodNetworkKindReasonCompliant,
		},
	}

	if crd == nil {
		podNetworkKind.Status.Conditions[0].Status = metav1.ConditionFalse
		podNetworkKind.Status.Conditions[0].Message = "CRD not found"
		podNetworkKind.Status.Conditions[0].Reason = v1alpha1.PodNetworkKindReasonCRDNotFound
		return nil
	}

	ready, message := crdReady(crd)
	if !ready {
		podNetworkKind.Status.Conditions[0].Status = metav1.ConditionFalse
		podNetworkKind.Status.Conditions[0].Message = message
		podNetworkKind.Status.Conditions[0].Reason = v1alpha1.PodNetworkKindReasonCRDNotReady
		return nil
	}

	categoriesExisting, message := crdCategory(crd)
	if !categoriesExisting {
		podNetworkKind.Status.Conditions[0].Status = metav1.ConditionFalse
		podNetworkKind.Status.Conditions[0].Message = message
		podNetworkKind.Status.Conditions[0].Reason = v1alpha1.PodNetworkKindReasonCRDMissingCategories
		return nil
	}

	allowed, err := pnkr.hasPermissions(ctx, crd)
	if err != nil {
		return err
	}
	if !allowed {
		podNetworkKind.Status.Conditions[0].Status = metav1.ConditionFalse
		podNetworkKind.Status.Conditions[0].Message = "Insufficient permissions to access the Custom Resources"
		podNetworkKind.Status.Conditions[0].Reason = v1alpha1.PodNetworkKindReasonMissingRBAC
		return nil
	}

	return nil
}

// crdCategory checks if the CRD has all the mandatory categories.
// It returns a boolean indicating if the CRD is compliant and a message describing any missing categories.
func crdCategory(crd *apiextensionsv1.CustomResourceDefinition) (bool, string) {
	crdCategories := set.New(crd.Spec.Names.Categories...)
	for _, category := range v1alpha1.Categories {
		if !crdCategories.Has(category) {
			return false, fmt.Sprintf("CRD is missing mandatory category %s", category)
		}
	}

	return true, ""
}

// crdReady checks if the CRD is established and its names are accepted.
// It returns a boolean indicating if the CRD is ready and a message describing any issues.
func crdReady(crd *apiextensionsv1.CustomResourceDefinition) (bool, string) {
	// conditionTypes keeps track of the CRD conditions that need to be satisfied.
	conditionTypes := set.New(apiextensionsv1.Established, apiextensionsv1.NamesAccepted)

	for _, condition := range crd.Status.Conditions {
		switch condition.Type {
		case apiextensionsv1.Established:
			if condition.Status != apiextensionsv1.ConditionTrue {
				return false, "CRD is not established"
			}
			conditionTypes.Delete(apiextensionsv1.Established)
		case apiextensionsv1.NamesAccepted:
			if condition.Status != apiextensionsv1.ConditionTrue {
				return false, "CRD names are not accepted"
			}
			conditionTypes.Delete(apiextensionsv1.NamesAccepted)
		case apiextensionsv1.NonStructuralSchema:
			if condition.Status != apiextensionsv1.ConditionTrue {
				return false, "CRD has a structural schema issue"
			}
		case apiextensionsv1.Terminating:
			if condition.Status != apiextensionsv1.ConditionTrue {
				return false, "CRD is terminating"
			}
		case apiextensionsv1.KubernetesAPIApprovalPolicyConformant:
			if condition.Status != apiextensionsv1.ConditionFalse {
				return false, "CRD is not conformant with the Kubernetes API approval policy"
			}
		}
	}

	if conditionTypes.Len() > 0 {
		return false, fmt.Sprintf("CRD is missing conditions: %v", conditionTypes.UnsortedList())
	}

	return true, ""
}

func hasStatusChanged(oldPodNetworkKind *v1alpha1.PodNetworkKind, newPodNetworkKind *v1alpha1.PodNetworkKind) bool {
	if len(oldPodNetworkKind.Status.Conditions) != len(newPodNetworkKind.Status.Conditions) {
		return true
	}

	for i, oldCondition := range oldPodNetworkKind.Status.Conditions {
		newCondition := newPodNetworkKind.Status.Conditions[i]
		if oldCondition.Type != newCondition.Type ||
			oldCondition.Status != newCondition.Status ||
			oldCondition.Reason != newCondition.Reason ||
			oldCondition.Message != newCondition.Message {
			return true
		}
	}
	return false
}

func (pnkr *PodNetworkKindReconciler) hasPermissions(ctx context.Context, crd *apiextensionsv1.CustomResourceDefinition) (bool, error) {
	review := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Group:    crd.Spec.Group,
				Resource: crd.Spec.Names.Plural,
				Verb:     "list",
			},
		},
	}

	result, err := pnkr.clientset.AuthorizationV1().
		SelfSubjectAccessReviews().
		Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to create SelfSubjectAccessReview: %w", err)
	}

	return result.Status.Allowed, nil
}
