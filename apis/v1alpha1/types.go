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

// NetworkKind describes a kind of pod networks implemented
// by a specific controller and represented by implementation-defined objects.
type NetworkKind struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// spec is the desired state of the NetworkKind.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Spec NetworkKindSpec `json:"spec"`
	// status is the current state of the NetworkKind.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Status NetworkKindStatus `json:"status"`
}

// NetworkKindSpec describes how pod network objects of this kind are identified.
type NetworkKindSpec struct {
	// ImplementationType identifies the API type of the pod network objects
	// belonging to this NetworkKind.
	// The ImplementationType may reference either a namespace-scoped or a cluster-scoped resource type.
	//
	// +required
	ImplementationType metav1.GroupKind `json:"implementationType"`
}

// NetworkKindStatus describes the observed state of the NetworkKind.
type NetworkKindStatus struct {
	// conditions is the list of conditions for this NetworkKind.
	// +optional
	// +listType=map
	// +listMapKey=type
	//
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// Well-known condition types for NetworkKinds.
const (
	// NetworkKindConditionImplementationTypeReady indicates whether the CRD referenced by the
	// NetworkKind's ImplementationType exists and is ready in the cluster.
	NetworkKindConditionImplementationTypeReady = "ImplementationTypeReady"
)

// Well-known condition reasons for NetworkKinds.
const (
	// NetworkKindReasonCRDNotFound in the ImplementationTypeReady condition indicates
	// that the CRD referenced by the NetworkKind's ImplementationType does not exist
	// in the cluster.
	NetworkKindReasonCRDNotFound string = "CRDNotFound"
	// NetworkKindReasonCRDNotReady in the ImplementationTypeReady condition indicates
	// that the CRD referenced by the NetworkKind's ImplementationType exists but is not
	// yet ready.
	// Ready means that the CRD has been established, its names have been accepted and it contains
	// the required categories.
	NetworkKindReasonCRDNotReady string = "CRDNotReady"
	// NetworkKindReasonMissingRBAC in the ImplementationTypeReady condition indicates
	// that the necessary RBAC permissions are missing for the CRD referenced by the
	// NetworkKind's ImplementationType.
	NetworkKindReasonMissingRBAC string = "MissingRBAC"
	// NetworkKindReasonCompliant in the ImplementationTypeReady condition indicates
	// that the CRD referenced by the NetworkKind's ImplementationType exists and is ready.
	NetworkKindReasonCompliant string = "Compliant"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NetworkKindList is a list of NetworkKind resources.
type NetworkKindList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []NetworkKind `json:"items"`
}
