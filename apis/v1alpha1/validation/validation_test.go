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
package validation_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/kubernetes-sigs/multi-network-api/apis/v1alpha1"
	"github.com/kubernetes-sigs/multi-network-api/apis/v1alpha1/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/dynamic-resource-allocation/api"
)

func TestValidateNetworkKind(t *testing.T) {
	tests := []struct {
		name        string
		networkKind *v1alpha1.NetworkKind
		want        field.ErrorList
	}{
		{
			name: "valid-network-kind",
			networkKind: &v1alpha1.NetworkKind{
				Spec: v1alpha1.NetworkKindSpec{
					ImplementationType: metav1.GroupKind{
						Group: "example.multinetwork.networking.x-k8s.io",
						Kind:  "ExampleNetwork",
					},
				},
			},
			want: field.ErrorList{},
		},
		{
			name: "valid-network-kind-status-CRDNotFound",
			networkKind: &v1alpha1.NetworkKind{
				Spec: v1alpha1.NetworkKindSpec{
					ImplementationType: metav1.GroupKind{
						Group: "example.multinetwork.networking.x-k8s.io",
						Kind:  "ExampleNetwork",
					},
				},
				Status: v1alpha1.NetworkKindStatus{
					Conditions: []metav1.Condition{
						{Type: v1alpha1.NetworkKindConditionImplementationTypeReady, Status: metav1.ConditionFalse, Reason: v1alpha1.NetworkKindReasonCRDNotFound, LastTransitionTime: metav1.Now(), ObservedGeneration: 0},
					},
				},
			},
			want: field.ErrorList{},
		},
		{
			name: "valid-network-kind-status-CRDNotReady",
			networkKind: &v1alpha1.NetworkKind{
				Spec: v1alpha1.NetworkKindSpec{
					ImplementationType: metav1.GroupKind{
						Group: "example.multinetwork.networking.x-k8s.io",
						Kind:  "ExampleNetwork",
					},
				},
				Status: v1alpha1.NetworkKindStatus{
					Conditions: []metav1.Condition{
						{Type: v1alpha1.NetworkKindConditionImplementationTypeReady, Status: metav1.ConditionFalse, Reason: v1alpha1.NetworkKindReasonCRDNotReady, LastTransitionTime: metav1.Now(), ObservedGeneration: 0},
					},
				},
			},
			want: field.ErrorList{},
		},
		{
			name: "valid-network-kind-status-MissingRBAC",
			networkKind: &v1alpha1.NetworkKind{
				Spec: v1alpha1.NetworkKindSpec{
					ImplementationType: metav1.GroupKind{
						Group: "example.multinetwork.networking.x-k8s.io",
						Kind:  "ExampleNetwork",
					},
				},
				Status: v1alpha1.NetworkKindStatus{
					Conditions: []metav1.Condition{
						{Type: v1alpha1.NetworkKindConditionImplementationTypeReady, Status: metav1.ConditionFalse, Reason: v1alpha1.NetworkKindReasonMissingRBAC, LastTransitionTime: metav1.Now(), ObservedGeneration: 0},
					},
				},
			},
			want: field.ErrorList{},
		},
		{
			name: "valid-network-kind-status-Compliant",
			networkKind: &v1alpha1.NetworkKind{
				Spec: v1alpha1.NetworkKindSpec{
					ImplementationType: metav1.GroupKind{
						Group: "example.multinetwork.networking.x-k8s.io",
						Kind:  "ExampleNetwork",
					},
				},
				Status: v1alpha1.NetworkKindStatus{
					Conditions: []metav1.Condition{
						{Type: v1alpha1.NetworkKindConditionImplementationTypeReady, Status: metav1.ConditionTrue, Reason: v1alpha1.NetworkKindReasonCompliant, LastTransitionTime: metav1.Now(), ObservedGeneration: 0},
					},
				},
			},
			want: field.ErrorList{},
		},
		{
			name: "invalid-network-kind-status-reason",
			networkKind: &v1alpha1.NetworkKind{
				Spec: v1alpha1.NetworkKindSpec{
					ImplementationType: metav1.GroupKind{
						Group: "example.multinetwork.networking.x-k8s.io",
						Kind:  "ExampleNetwork",
					},
				},
				Status: v1alpha1.NetworkKindStatus{
					Conditions: []metav1.Condition{
						{Type: v1alpha1.NetworkKindConditionImplementationTypeReady, Status: metav1.ConditionTrue, Reason: "InvalidReason", LastTransitionTime: metav1.Now(), ObservedGeneration: 0},
					},
				},
			},
			want: field.ErrorList{
				field.Invalid(field.NewPath("status").Child("conditions").Index(0).Child("reason"), "InvalidReason", "this reason is not valid for the condition type"),
			},
		},
		{
			name: "invalid-network-kind-status-condition-type",
			networkKind: &v1alpha1.NetworkKind{
				Spec: v1alpha1.NetworkKindSpec{
					ImplementationType: metav1.GroupKind{
						Group: "example.multinetwork.networking.x-k8s.io",
						Kind:  "ExampleNetwork",
					},
				},
				Status: v1alpha1.NetworkKindStatus{
					Conditions: []metav1.Condition{
						{Type: "InvalidConditionType", Status: metav1.ConditionTrue, Reason: v1alpha1.NetworkKindReasonCRDNotReady, LastTransitionTime: metav1.Now(), ObservedGeneration: 0},
					},
				},
			},
			want: field.ErrorList{
				field.Invalid(field.NewPath("status").Child("conditions").Index(0).Child("type"), "InvalidConditionType", "this condition type is not valid for NetworkKind status"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validation.ValidateNetworkKind(tt.networkKind)
			assertFailures(t, tt.want, got)
		})
	}
}

// AssertFailures compares the expected against the actual errors.
//
// If they differ, it also logs what the formatted errors would look
// like to a user. This can be helpful to figure out whether an error
// is informative.
func assertFailures(tb testing.TB, want, got field.ErrorList) bool {
	tb.Helper()
	if diff := cmp.Diff(want, got, cmpopts.IgnoreFields(field.Error{}, "Origin"), cmp.AllowUnexported(api.UniqueString{})); diff != "" {
		tb.Errorf("unexpected field errors (-want, +got):\n%s", diff)
		return false
	}
	return true
}
