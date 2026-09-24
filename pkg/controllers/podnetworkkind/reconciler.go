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
	"github.com/kubernetes-sigs/multi-network-api/pkg/ruleswatcher"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	v1apiextensionsinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/ptr"
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
	ruleWatcher                     ruleswatcher.Interface
	clientset                       clientset.Interface
}

func NewPodNetworkKindReconciler(
	customResourceDefinitionInformer v1apiextensionsinformers.CustomResourceDefinitionInformer,
	podNetworkKindLister v1alpha1networkkindlisters.PodNetworkKindLister,
	podNetworkKindClient v1alpha1client.PodNetworkKindInterface,
	ruleWatcher ruleswatcher.Interface,
	clientset clientset.Interface,
) (*PodNetworkKindReconciler, error) {
	nkr := &PodNetworkKindReconciler{
		customResourceDefinitionIndexer: customResourceDefinitionInformer.Informer().GetIndexer(),
		podNetworkKindLister:            podNetworkKindLister,
		podNetworkKindClient:            podNetworkKindClient,
		ruleWatcher:                     ruleWatcher,
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

	pnkr.setImplementationTypeReadyCondition(podNetworkKind, crd)

	err = pnkr.setDefaultPodNetworkKind(podNetworkKind)
	if err != nil {
		return fmt.Errorf("failed to set default pod network kind: %w", err)
	}

	if hasStatusChanged(oldNetworkKind, podNetworkKind) {
		_, err := pnkr.podNetworkKindClient.UpdateStatus(ctx, podNetworkKind, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update PodNetworkKind status: %w", err)
		}
	}

	return nil
}

func (pnkr *PodNetworkKindReconciler) setImplementationTypeReadyCondition(podNetworkKind *v1alpha1.PodNetworkKind, crd *apiextensionsv1.CustomResourceDefinition) {
	condition := metav1.Condition{
		Type:               v1alpha1.PodNetworkKindConditionImplementationTypeReady,
		Status:             metav1.ConditionTrue,
		Message:            "",
		ObservedGeneration: podNetworkKind.ObjectMeta.Generation,
		Reason:             v1alpha1.PodNetworkKindReasonCompliant,
	}

	if crd == nil {
		condition.Status = metav1.ConditionFalse
		condition.Message = "CRD not found"
		condition.Reason = v1alpha1.PodNetworkKindReasonCRDNotFound
		meta.SetStatusCondition(&podNetworkKind.Status.Conditions, condition)
		return
	}

	ready, message := crdReady(crd)
	if !ready {
		condition.Status = metav1.ConditionFalse
		condition.Message = message
		condition.Reason = v1alpha1.PodNetworkKindReasonCRDNotReady
		meta.SetStatusCondition(&podNetworkKind.Status.Conditions, condition)
		return
	}

	categoriesExisting, message := crdCategory(crd)
	if !categoriesExisting {
		condition.Status = metav1.ConditionFalse
		condition.Message = message
		condition.Reason = v1alpha1.PodNetworkKindReasonCRDMissingCategories
		meta.SetStatusCondition(&podNetworkKind.Status.Conditions, condition)
		return
	}

	allowed := pnkr.hasPermissions(crd)
	if !allowed {
		condition.Status = metav1.ConditionFalse
		condition.Message = "Insufficient permissions to access the Custom Resources"
		condition.Reason = v1alpha1.PodNetworkKindReasonMissingRBAC
		meta.SetStatusCondition(&podNetworkKind.Status.Conditions, condition)
		return
	}

	meta.SetStatusCondition(&podNetworkKind.Status.Conditions, condition)
}

func (pnkr *PodNetworkKindReconciler) setDefaultPodNetworkKind(podNetworkKind *v1alpha1.PodNetworkKind) error {
	if podNetworkKind.Spec.DefaultPodNetworkKind == nil || !*podNetworkKind.Spec.DefaultPodNetworkKind {
		podNetworkKind.Status.DefaultPodNetworkKind = nil
		meta.RemoveStatusCondition(&podNetworkKind.Status.Conditions, v1alpha1.PodNetworkKindConditionDefaultPodNetworkKind)
		return nil
	}

	allKinds, err := pnkr.podNetworkKindLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("failed to list PodNetworkKinds: %w", err)
	}

	var candidates []*v1alpha1.PodNetworkKind
	foundCurrent := false
	for _, k := range allKinds {
		if k.Spec.DefaultPodNetworkKind != nil && *k.Spec.DefaultPodNetworkKind {
			if k.Name == podNetworkKind.Name {
				candidates = append(candidates, podNetworkKind)
				foundCurrent = true
			} else {
				candidates = append(candidates, k)
			}
		}
	}
	if !foundCurrent {
		candidates = append(candidates, podNetworkKind)
	}

	best := selectDefaultPodNetworkKind(candidates)
	if best != nil && best.Name == podNetworkKind.Name {
		podNetworkKind.Status.DefaultPodNetworkKind = ptr.To(true)
		meta.SetStatusCondition(&podNetworkKind.Status.Conditions, metav1.Condition{
			Type:               v1alpha1.PodNetworkKindConditionDefaultPodNetworkKind,
			Status:             metav1.ConditionTrue,
			Reason:             v1alpha1.PodNetworkKindReasonDefaultPodNetworkKindSet,
			Message:            "",
			ObservedGeneration: podNetworkKind.Generation,
		})
	} else {
		podNetworkKind.Status.DefaultPodNetworkKind = ptr.To(false)
		meta.SetStatusCondition(&podNetworkKind.Status.Conditions, metav1.Condition{
			Type:               v1alpha1.PodNetworkKindConditionDefaultPodNetworkKind,
			Status:             metav1.ConditionFalse,
			Reason:             v1alpha1.PodNetworkKindReasonDefaultPodNetworkKindAlreadySet,
			Message:            "Another PodNetworkKind is already set as default",
			ObservedGeneration: podNetworkKind.Generation,
		})
	}

	return nil
}

func selectDefaultPodNetworkKind(candidates []*v1alpha1.PodNetworkKind) *v1alpha1.PodNetworkKind {
	if len(candidates) == 0 {
		return nil
	}

	best := candidates[0]
	for _, c := range candidates[1:] {
		if isOlderOrFirstAlphabetically(c, best) {
			best = c
		}
	}
	return best
}

func isOlderOrFirstAlphabetically(a, b *v1alpha1.PodNetworkKind) bool {
	if a.CreationTimestamp.Before(&b.CreationTimestamp) {
		return true
	}
	if b.CreationTimestamp.Before(&a.CreationTimestamp) {
		return false
	}
	return a.Name < b.Name
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
	if (oldPodNetworkKind.Status.DefaultPodNetworkKind == nil) != (newPodNetworkKind.Status.DefaultPodNetworkKind == nil) {
		return true
	}
	if oldPodNetworkKind.Status.DefaultPodNetworkKind != nil && newPodNetworkKind.Status.DefaultPodNetworkKind != nil {
		if *oldPodNetworkKind.Status.DefaultPodNetworkKind != *newPodNetworkKind.Status.DefaultPodNetworkKind {
			return true
		}
	}

	if len(oldPodNetworkKind.Status.Conditions) != len(newPodNetworkKind.Status.Conditions) {
		return true
	}

	for _, oldCondition := range oldPodNetworkKind.Status.Conditions {
		newCondition := findCondition(newPodNetworkKind.Status.Conditions, oldCondition.Type)
		if newCondition == nil ||
			oldCondition.Status != newCondition.Status ||
			oldCondition.Reason != newCondition.Reason ||
			oldCondition.Message != newCondition.Message {
			return true
		}
	}
	return false
}

func findCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

func (pnkr *PodNetworkKindReconciler) hasPermissions(crd *apiextensionsv1.CustomResourceDefinition) bool {
	return pnkr.ruleWatcher.HasPermission("list", crd.Spec.Group, crd.Spec.Names.Plural)
}
