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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/kubernetes-sigs/multi-network-api/apis/v1alpha1"
	networkKindFake "github.com/kubernetes-sigs/multi-network-api/pkg/client/clientset/versioned/fake"
	networkKindInformers "github.com/kubernetes-sigs/multi-network-api/pkg/client/informers/externalversions"
	authv1 "k8s.io/api/authorization/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsFake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	apiextensionsinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func newReconciler(
	ctx context.Context,
	t *testing.T,
	initialNetworkKindObjects []runtime.Object,
	initialCRDObjects []runtime.Object,
) (*fake.Clientset, *networkKindFake.Clientset, *apiextensionsFake.Clientset, *NetworkKindReconciler) {
	fakeKubeClient := fake.NewSimpleClientset()
	fakeMultiNetworkClient := networkKindFake.NewSimpleClientset(initialNetworkKindObjects...)
	fakeApiExtensionsClient := apiextensionsFake.NewSimpleClientset(initialCRDObjects...)

	networkKindInformerFactory := networkKindInformers.NewSharedInformerFactory(fakeMultiNetworkClient, 0)
	apiextensionsInformerFactory := apiextensionsinformers.NewSharedInformerFactory(fakeApiExtensionsClient, 0)

	reconciler, err := NewNetworkKindReconciler(
		apiextensionsInformerFactory.Apiextensions().V1().CustomResourceDefinitions(),
		networkKindInformerFactory.Multinetwork().V1alpha1().NetworkKinds().Lister(),
		fakeMultiNetworkClient.MultinetworkV1alpha1().NetworkKinds(),
		fakeKubeClient,
	)
	if err != nil {
		t.Fatal("NewNetworkKindReconciler failed:", err)
	}

	networkKindInformerFactory.Start(ctx.Done())
	apiextensionsInformerFactory.Start(ctx.Done())
	networkKindInformerFactory.WaitForCacheSync(ctx.Done())
	apiextensionsInformerFactory.WaitForCacheSync(ctx.Done())

	return fakeKubeClient, fakeMultiNetworkClient, fakeApiExtensionsClient, reconciler
}

func TestNetworkKindReconciler_Reconcile(t *testing.T) {
	networkKindObj := func() *v1alpha1.NetworkKind {
		return &v1alpha1.NetworkKind{
			ObjectMeta: metav1.ObjectMeta{Name: "example-com-mynetwork"},
			Spec: v1alpha1.NetworkKindSpec{
				ImplementationType: metav1.GroupKind{Group: "example.com", Kind: "MyNetwork"},
			},
		}
	}

	readyCRD := func() *apiextensionsv1.CustomResourceDefinition {
		return &apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{Name: "mynetworks.example.com"},
			Spec: apiextensionsv1.CustomResourceDefinitionSpec{
				Group: "example.com",
				Names: apiextensionsv1.CustomResourceDefinitionNames{
					Kind:       "MyNetwork",
					Plural:     "mynetworks",
					Categories: []string{"podnetwork", "podnetworks"},
				},
			},
			Status: apiextensionsv1.CustomResourceDefinitionStatus{
				Conditions: []apiextensionsv1.CustomResourceDefinitionCondition{
					{Type: apiextensionsv1.Established, Status: apiextensionsv1.ConditionTrue},
					{Type: apiextensionsv1.NamesAccepted, Status: apiextensionsv1.ConditionTrue},
				},
			},
		}
	}

	allowSelfSubjectAccessReview := func(fakeKubeClient *fake.Clientset) {
		fakeKubeClient.PrependReactor("create", "selfsubjectaccessreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, &authv1.SelfSubjectAccessReview{
				Status: authv1.SubjectAccessReviewStatus{Allowed: true},
			}, nil
		})
	}

	tests := []struct {
		name                      string
		initialNetworkKindObjects []runtime.Object
		initialCRDObjects         []runtime.Object
		fakeKubeClientSetup       func(*fake.Clientset)
		networkKindName           string
		wantErr                   bool
		expectedNetworkKind       *v1alpha1.NetworkKind
	}{
		{
			name:            "NetworkKind not found",
			networkKindName: "nonexistent",
		},
		{
			name:                      "CRD not found",
			initialNetworkKindObjects: []runtime.Object{networkKindObj()},
			networkKindName:           "example-com-mynetwork",
			expectedNetworkKind: func() *v1alpha1.NetworkKind {
				nk := networkKindObj()
				nk.Status.Conditions = []metav1.Condition{{
					Type:    v1alpha1.NetworkKindConditionImplementationTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.NetworkKindReasonCRDNotFound,
					Message: "CRD not found",
				}}
				return nk
			}(),
		},
		{
			name:                      "CRD not established",
			initialNetworkKindObjects: []runtime.Object{networkKindObj()},
			initialCRDObjects: []runtime.Object{func() *apiextensionsv1.CustomResourceDefinition {
				crd := readyCRD()
				crd.Status.Conditions = []apiextensionsv1.CustomResourceDefinitionCondition{
					{Type: apiextensionsv1.Established, Status: apiextensionsv1.ConditionFalse},
					{Type: apiextensionsv1.NamesAccepted, Status: apiextensionsv1.ConditionTrue},
				}
				return crd
			}()},
			networkKindName: "example-com-mynetwork",
			expectedNetworkKind: func() *v1alpha1.NetworkKind {
				nk := networkKindObj()
				nk.Status.Conditions = []metav1.Condition{{
					Type:    v1alpha1.NetworkKindConditionImplementationTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.NetworkKindReasonCRDNotReady,
					Message: "CRD is not established",
				}}
				return nk
			}(),
		},
		{
			name:                      "CRD missing mandatory categories",
			initialNetworkKindObjects: []runtime.Object{networkKindObj()},
			initialCRDObjects: []runtime.Object{func() *apiextensionsv1.CustomResourceDefinition {
				crd := readyCRD()
				crd.Spec.Names.Categories = nil
				return crd
			}()},
			networkKindName: "example-com-mynetwork",
			expectedNetworkKind: func() *v1alpha1.NetworkKind {
				nk := networkKindObj()
				nk.Status.Conditions = []metav1.Condition{{
					Type:    v1alpha1.NetworkKindConditionImplementationTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.NetworkKindReasonCRDNotReady,
					Message: "CRD is missing mandatory category podnetwork",
				}}
				return nk
			}(),
		},
		{
			name:                      "missing RBAC",
			initialNetworkKindObjects: []runtime.Object{networkKindObj()},
			initialCRDObjects:         []runtime.Object{readyCRD()},
			networkKindName:           "example-com-mynetwork",
			expectedNetworkKind: func() *v1alpha1.NetworkKind {
				nk := networkKindObj()
				nk.Status.Conditions = []metav1.Condition{{
					Type:    v1alpha1.NetworkKindConditionImplementationTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.NetworkKindReasonMissingRBAC,
					Message: "Insufficient permissions to access the Custom Resources",
				}}
				return nk
			}(),
		},
		{
			name:                      "compliant",
			initialNetworkKindObjects: []runtime.Object{networkKindObj()},
			initialCRDObjects:         []runtime.Object{readyCRD()},
			fakeKubeClientSetup:       allowSelfSubjectAccessReview,
			networkKindName:           "example-com-mynetwork",
			expectedNetworkKind: func() *v1alpha1.NetworkKind {
				nk := networkKindObj()
				nk.Status.Conditions = []metav1.Condition{{
					Type:    v1alpha1.NetworkKindConditionImplementationTypeReady,
					Status:  metav1.ConditionTrue,
					Reason:  v1alpha1.NetworkKindReasonCompliant,
					Message: "",
				}}
				return nk
			}(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			fakeKubeClient, fakeMultiNetworkClient, _, nkr := newReconciler(ctx, t, tt.initialNetworkKindObjects, tt.initialCRDObjects)

			if tt.fakeKubeClientSetup != nil {
				tt.fakeKubeClientSetup(fakeKubeClient)
			}

			err := nkr.Reconcile(ctx, tt.networkKindName)
			if (err != nil) != tt.wantErr {
				t.Errorf("Reconcile() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.expectedNetworkKind != nil {
				nk, getErr := fakeMultiNetworkClient.MultinetworkV1alpha1().NetworkKinds().Get(ctx, tt.networkKindName, metav1.GetOptions{})
				if getErr != nil {
					t.Fatalf("failed to get NetworkKind: %v", getErr)
				}
				if diff := cmp.Diff(tt.expectedNetworkKind, nk,
					cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime"),
				); diff != "" {
					t.Errorf("NetworkKind mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}
