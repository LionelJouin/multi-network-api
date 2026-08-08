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

package v1alpha1_test

import (
	"testing"

	"github.com/kubernetes-sigs/multi-network-api/pkg/podnetworkdevice/v1alpha1"
	"k8s.io/utils/ptr"
)

func TestPodNetworkRef_IsEqual(t *testing.T) {
	tests := []struct {
		name           string
		podNetworkRefA *v1alpha1.PodNetworkRef
		podNetworkRefB *v1alpha1.PodNetworkRef
		want           bool
	}{
		{
			name:           "both nil",
			podNetworkRefA: nil,
			podNetworkRefB: nil,
			want:           true,
		},
		{
			name:           "one nil, one not nil",
			podNetworkRefA: &v1alpha1.PodNetworkRef{Kind: "kindA", Name: "nameA"},
			podNetworkRefB: nil,
			want:           false,
		},
		{
			name:           "different kind",
			podNetworkRefA: &v1alpha1.PodNetworkRef{Kind: "kindA", Name: "nameA"},
			podNetworkRefB: &v1alpha1.PodNetworkRef{Kind: "kindB", Name: "nameA"},
			want:           false,
		},
		{
			name:           "different name",
			podNetworkRefA: &v1alpha1.PodNetworkRef{Kind: "kindA", Name: "nameA"},
			podNetworkRefB: &v1alpha1.PodNetworkRef{Kind: "kindA", Name: "nameB"},
			want:           false,
		},
		{
			name:           "different namespace",
			podNetworkRefA: &v1alpha1.PodNetworkRef{Kind: "kindA", Name: "nameA", Namespace: ptr.To("namespaceA")},
			podNetworkRefB: &v1alpha1.PodNetworkRef{Kind: "kindA", Name: "nameA", Namespace: ptr.To("namespaceB")},
			want:           false,
		},
		{
			name:           "equal with both nil namespace",
			podNetworkRefA: &v1alpha1.PodNetworkRef{Kind: "kindA", Name: "nameA"},
			podNetworkRefB: &v1alpha1.PodNetworkRef{Kind: "kindA", Name: "nameA"},
			want:           true,
		},
		{
			name:           "one nil namespace, one not nil",
			podNetworkRefA: &v1alpha1.PodNetworkRef{Kind: "kindA", Name: "nameA", Namespace: ptr.To("namespaceA")},
			podNetworkRefB: &v1alpha1.PodNetworkRef{Kind: "kindA", Name: "nameA"},
			want:           false,
		},
		{
			name:           "equal with namespace",
			podNetworkRefA: &v1alpha1.PodNetworkRef{Kind: "kindA", Name: "nameA", Namespace: ptr.To("namespaceA")},
			podNetworkRefB: &v1alpha1.PodNetworkRef{Kind: "kindA", Name: "nameA", Namespace: ptr.To("namespaceA")},
			want:           true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.podNetworkRefA.IsEqual(tt.podNetworkRefB)
			if got != tt.want {
				t.Errorf("IsEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}
