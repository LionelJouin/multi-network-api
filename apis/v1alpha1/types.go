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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:resource:shortName=nk,scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Group",type=string,JSONPath=`.spec.implementationType.group`
// +kubebuilder:printcolumn:name="Kind",type=string,JSONPath=`.spec.implementationType.kind`
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PodNetworkKind describes a kind of pod networks implemented
// by a specific controller and represented by implementation-defined objects.
type PodNetworkKind struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// spec is the desired state of the PodNetworkKind.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Spec PodNetworkKindSpec `json:"spec"`
	// status is the current state of the PodNetworkKind.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Status PodNetworkKindStatus `json:"status"`
}

// PodNetworkKindSpec describes how pod network objects of this kind are identified.
type PodNetworkKindSpec struct {
	// ImplementationType identifies the API type of the pod network objects
	// belonging to this PodNetworkKind.
	// The ImplementationType may reference either a namespace-scoped or a cluster-scoped resource type.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.ImplementationType is immutable"
	ImplementationType metav1.GroupKind `json:"implementationType"`

	// DefaultPodNetworkKind indicates whether this PodNetworkKind requests to be the default for the cluster.
	// If true, this PodNetworkKind is requesting to be the default for the cluster.
	// If false, this PodNetworkKind is not requesting to be the default for the cluster.
	// If not specified, the default is false.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.DefaultPodNetworkKind is immutable"
	// +optional
	DefaultPodNetworkKind *bool `json:"defaultPodNetworkKind,omitempty"`
}

// PodNetworkKindStatus describes the observed state of the PodNetworkKind.
type PodNetworkKindStatus struct {
	// conditions is the list of conditions for this PodNetworkKind.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// DefaultPodNetworkKind indicates whether this PodNetworkKind has been selected to be the default for the cluster.
	// When multiple PodNetworkKinds have spec.DefaultPodNetworkKind set to true, the candidate selected as default
	// is the oldest PodNetworkKind determined by metadata.creationTimestamp. If timestamps are equal, the
	// candidate selected as default is the one with the first name in the list sorted in alphabetical order.
	// If another PodNetworkKind is already the active default PodNetworkKind, this field will be false even if
	// spec.DefaultPodNetworkKind is true, and a condition will be set to indicate that the default PodNetworkKind
	// is already set.
	// +optional
	DefaultPodNetworkKind *bool `json:"defaultPodNetworkKind,omitempty"`
}

// Well-known condition types for PodNetworkKinds.
const (
	// PodNetworkKindConditionImplementationTypeReady indicates whether the CRD referenced by the
	// PodNetworkKind's ImplementationType exists and is ready in the cluster.
	PodNetworkKindConditionImplementationTypeReady = "ImplementationTypeReady"

	// PodNetworkKindConditionDefaultPodNetworkKind indicates whether this PodNetworkKind has been selected
	// to be the default for the cluster.
	// This condition is added only when the spec.DefaultPodNetworkKind is true.
	PodNetworkKindConditionDefaultPodNetworkKind = "DefaultPodNetworkKind"
)

// Well-known condition reasons for PodNetworkKinds.
const (
	// PodNetworkKindReasonCRDNotFound in the ImplementationTypeReady condition indicates
	// that the CRD referenced by the PodNetworkKind's ImplementationType does not exist
	// in the cluster.
	PodNetworkKindReasonCRDNotFound string = "CRDNotFound"
	// PodNetworkKindReasonCRDNotReady in the ImplementationTypeReady condition indicates
	// that the CRD referenced by the PodNetworkKind's ImplementationType exists but is not
	// yet ready.
	// Ready means that the CRD conditions status are reported in the following way for each types:
	// - Established: True
	// - NamesAccepted: True
	// - NonStructuralSchema: False (or non-existent)
	// - Terminating: False (or non-existent)
	// - KubernetesAPIApprovalPolicyConformant: True (or non-existent)
	// For more details, refer to the Kubernetes API documentation for CRD condition types:
	// https://github.com/kubernetes/apiextensions-apiserver/blob/v0.37.0/pkg/apis/apiextensions/v1/types.go#L305
	PodNetworkKindReasonCRDNotReady string = "CRDNotReady"
	// PodNetworkKindReasonCRDMissingCategories in the ImplementationTypeReady condition indicates
	// that the CRD referenced by the PodNetworkKind's ImplementationType exists but is missing
	// required categories.
	PodNetworkKindReasonCRDMissingCategories string = "CRDMissingCategories"
	// PodNetworkKindReasonMissingRBAC in the ImplementationTypeReady condition indicates
	// that the necessary RBAC permissions are missing for the CRD referenced by the
	// PodNetworkKind's ImplementationType.
	PodNetworkKindReasonMissingRBAC string = "MissingRBAC"
	// PodNetworkKindReasonTypeConflict in the ImplementationTypeReady condition indicates
	// that another PodNetworkKind with the same type already exists in the cluster.
	PodNetworkKindReasonTypeConflict string = "TypeConflict"
	// PodNetworkKindReasonCompliant in the ImplementationTypeReady condition indicates
	// that the CRD referenced by the PodNetworkKind's ImplementationType exists and is ready.
	PodNetworkKindReasonCompliant string = "Compliant"

	// PodNetworkKindReasonDefaultPodNetworkKindSet in the DefaultPodNetworkKind condition indicates
	// that this PodNetworkKind has been selected to be the default for the cluster.
	PodNetworkKindReasonDefaultPodNetworkKindSet string = "DefaultPodNetworkKindSet"
	// PodNetworkKindReasonDefaultPodNetworkKindAlreadySet in the DefaultPodNetworkKind condition indicates
	// that this PodNetworkKind cannot be set as the default for the cluster because another
	// PodNetworkKind is already set as the default.
	PodNetworkKindReasonDefaultPodNetworkKindAlreadySet string = "DefaultPodNetworkKindAlreadySet"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PodNetworkKindList is a list of PodNetworkKind resources.
type PodNetworkKindList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []PodNetworkKind `json:"items"`
}
