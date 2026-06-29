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
package validation

import (
	"github.com/kubernetes-sigs/multi-network-api/apis/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/set"
)

var (
	conditionReasonsForTypes = map[string]set.Set[string]{
		v1alpha1.NetworkKindConditionImplementationTypeReady: set.New[string](
			v1alpha1.NetworkKindReasonCRDNotFound,
			v1alpha1.NetworkKindReasonCRDNotReady,
			v1alpha1.NetworkKindReasonMissingRBAC,
			v1alpha1.NetworkKindReasonCompliant,
		),
	}
)

// ValidateNetworkKind validates a NetworkKind.
func ValidateNetworkKind(networkKind *v1alpha1.NetworkKind) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, validateNetworkKindSpec(&networkKind.Spec, field.NewPath("spec"))...)
	allErrs = append(allErrs, validateNetworkKindStatus(&networkKind.Status, field.NewPath("status"))...)
	return allErrs
}

func validateNetworkKindSpec(networkKindSpec *v1alpha1.NetworkKindSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, validateImplementationType(&networkKindSpec.ImplementationType, fldPath.Child("implementationType"))...)
	return allErrs
}

func validateImplementationType(implementationType *metav1.GroupKind, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if implementationType.Group == "" {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("group"), implementationType.Group, "must not be empty"))
	}
	if implementationType.Kind == "" {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("kind"), implementationType.Kind, "must not be empty"))
	}
	return allErrs
}

func validateNetworkKindStatus(networkKindStatus *v1alpha1.NetworkKindStatus, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, validateConditions(networkKindStatus.Conditions, fldPath.Child("conditions"))...)
	return allErrs
}

func validateConditions(conditions []metav1.Condition, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, metav1validation.ValidateConditions(conditions, fldPath)...)

	visitedConditionTypes := map[string]struct{}{}
	for i, condition := range conditions {
		validReasons, ok := conditionReasonsForTypes[condition.Type]
		if !ok {
			allErrs = append(allErrs, field.Invalid(fldPath.Index(i).Child("type"), condition.Type, "this condition type is not valid for NetworkKind status"))
			continue
		}
		if !validReasons.Has(condition.Reason) {
			allErrs = append(allErrs, field.Invalid(fldPath.Index(i).Child("reason"), condition.Reason, "this reason is not valid for the condition type"))
			continue
		}
		if _, exists := visitedConditionTypes[condition.Type]; exists {
			allErrs = append(allErrs, field.Duplicate(fldPath.Index(i).Child("type"), condition.Type))
		}
		visitedConditionTypes[condition.Type] = struct{}{}
	}

	return allErrs
}
