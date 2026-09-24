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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/kubernetes-sigs/multi-network-api/apis/v1alpha1"
	podNetworkKindFake "github.com/kubernetes-sigs/multi-network-api/pkg/client/clientset/versioned/fake"
	podNetworkKindInformers "github.com/kubernetes-sigs/multi-network-api/pkg/client/informers/externalversions"
	authv1 "k8s.io/api/authorization/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsFake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	apiextensionsinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
)

func newReconciler(
	ctx context.Context,
	t *testing.T,
	initialPodNetworkKindObjects []runtime.Object,
	initialCRDObjects []runtime.Object,
) (*fake.Clientset, *podNetworkKindFake.Clientset, *apiextensionsFake.Clientset, *PodNetworkKindReconciler) {
	fakeKubeClient := fake.NewSimpleClientset()
	fakeMultiNetworkClient := podNetworkKindFake.NewSimpleClientset(initialPodNetworkKindObjects...)
	fakeApiExtensionsClient := apiextensionsFake.NewSimpleClientset(initialCRDObjects...)

	networkKindInformerFactory := podNetworkKindInformers.NewSharedInformerFactory(fakeMultiNetworkClient, 0)
	apiextensionsInformerFactory := apiextensionsinformers.NewSharedInformerFactory(fakeApiExtensionsClient, 0)

	reconciler, err := NewPodNetworkKindReconciler(
		apiextensionsInformerFactory.Apiextensions().V1().CustomResourceDefinitions(),
		networkKindInformerFactory.Multinetwork().V1alpha1().PodNetworkKinds().Lister(),
		fakeMultiNetworkClient.MultinetworkV1alpha1().PodNetworkKinds(),
		fakeKubeClient,
	)
	if err != nil {
		t.Fatal("NewPodNetworkKindReconciler failed:", err)
	}

	networkKindInformerFactory.Start(ctx.Done())
	apiextensionsInformerFactory.Start(ctx.Done())
	networkKindInformerFactory.WaitForCacheSync(ctx.Done())
	apiextensionsInformerFactory.WaitForCacheSync(ctx.Done())

	return fakeKubeClient, fakeMultiNetworkClient, fakeApiExtensionsClient, reconciler
}

func TestPodNetworkKindReconciler_Reconcile(t *testing.T) {
	podNetworkKindObj := func() *v1alpha1.PodNetworkKind {
		return &v1alpha1.PodNetworkKind{
			ObjectMeta: metav1.ObjectMeta{Name: "example-com-mynetwork"},
			Spec: v1alpha1.PodNetworkKindSpec{
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
		name                         string
		initialPodNetworkKindObjects []runtime.Object
		initialCRDObjects            []runtime.Object
		fakeKubeClientSetup          func(*fake.Clientset)
		fakeMultiNetworkClientSetup  func(*podNetworkKindFake.Clientset)
		customReconcilerSetup        func(*PodNetworkKindReconciler)
		podNetworkKindName           string
		wantErr                      bool
		expectedPodNetworkKind       *v1alpha1.PodNetworkKind
	}{
		{
			name:               "PodNetworkKind not found",
			podNetworkKindName: "nonexistent",
		},
		{
			name:                         "CRD not found",
			initialPodNetworkKindObjects: []runtime.Object{podNetworkKindObj()},
			podNetworkKindName:           "example-com-mynetwork",
			expectedPodNetworkKind: func() *v1alpha1.PodNetworkKind {
				nk := podNetworkKindObj()
				nk.Status.Conditions = []metav1.Condition{{
					Type:    v1alpha1.PodNetworkKindConditionImplementationTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.PodNetworkKindReasonCRDNotFound,
					Message: "CRD not found",
				}}
				return nk
			}(),
		},
		{
			name:                         "CRD not established",
			initialPodNetworkKindObjects: []runtime.Object{podNetworkKindObj()},
			initialCRDObjects: []runtime.Object{func() *apiextensionsv1.CustomResourceDefinition {
				crd := readyCRD()
				crd.Status.Conditions = []apiextensionsv1.CustomResourceDefinitionCondition{
					{Type: apiextensionsv1.Established, Status: apiextensionsv1.ConditionFalse},
					{Type: apiextensionsv1.NamesAccepted, Status: apiextensionsv1.ConditionTrue},
				}
				return crd
			}()},
			podNetworkKindName: "example-com-mynetwork",
			expectedPodNetworkKind: func() *v1alpha1.PodNetworkKind {
				nk := podNetworkKindObj()
				nk.Status.Conditions = []metav1.Condition{{
					Type:    v1alpha1.PodNetworkKindConditionImplementationTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.PodNetworkKindReasonCRDNotReady,
					Message: "CRD is not established",
				}}
				return nk
			}(),
		},
		{
			name:                         "CRD names not accepted",
			initialPodNetworkKindObjects: []runtime.Object{podNetworkKindObj()},
			initialCRDObjects: []runtime.Object{func() *apiextensionsv1.CustomResourceDefinition {
				crd := readyCRD()
				crd.Status.Conditions = []apiextensionsv1.CustomResourceDefinitionCondition{
					{Type: apiextensionsv1.Established, Status: apiextensionsv1.ConditionTrue},
					{Type: apiextensionsv1.NamesAccepted, Status: apiextensionsv1.ConditionFalse},
				}
				return crd
			}()},
			podNetworkKindName: "example-com-mynetwork",
			expectedPodNetworkKind: func() *v1alpha1.PodNetworkKind {
				nk := podNetworkKindObj()
				nk.Status.Conditions = []metav1.Condition{{
					Type:    v1alpha1.PodNetworkKindConditionImplementationTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.PodNetworkKindReasonCRDNotReady,
					Message: "CRD names are not accepted",
				}}
				return nk
			}(),
		},
		{
			name:                         "CRD missing Established condition",
			initialPodNetworkKindObjects: []runtime.Object{podNetworkKindObj()},
			initialCRDObjects: []runtime.Object{func() *apiextensionsv1.CustomResourceDefinition {
				crd := readyCRD()
				crd.Status.Conditions = []apiextensionsv1.CustomResourceDefinitionCondition{
					{Type: apiextensionsv1.NamesAccepted, Status: apiextensionsv1.ConditionTrue},
				}
				return crd
			}()},
			podNetworkKindName: "example-com-mynetwork",
			expectedPodNetworkKind: func() *v1alpha1.PodNetworkKind {
				nk := podNetworkKindObj()
				nk.Status.Conditions = []metav1.Condition{{
					Type:    v1alpha1.PodNetworkKindConditionImplementationTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.PodNetworkKindReasonCRDNotReady,
					Message: "CRD is missing conditions: [Established]",
				}}
				return nk
			}(),
		},
		{
			name:                         "CRD has structural schema issue",
			initialPodNetworkKindObjects: []runtime.Object{podNetworkKindObj()},
			initialCRDObjects: []runtime.Object{func() *apiextensionsv1.CustomResourceDefinition {
				crd := readyCRD()
				crd.Status.Conditions = append(crd.Status.Conditions, apiextensionsv1.CustomResourceDefinitionCondition{
					Type:   apiextensionsv1.NonStructuralSchema,
					Status: apiextensionsv1.ConditionFalse,
				})
				return crd
			}()},
			podNetworkKindName: "example-com-mynetwork",
			expectedPodNetworkKind: func() *v1alpha1.PodNetworkKind {
				nk := podNetworkKindObj()
				nk.Status.Conditions = []metav1.Condition{{
					Type:    v1alpha1.PodNetworkKindConditionImplementationTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.PodNetworkKindReasonCRDNotReady,
					Message: "CRD has a structural schema issue",
				}}
				return nk
			}(),
		},
		{
			name:                         "CRD is terminating",
			initialPodNetworkKindObjects: []runtime.Object{podNetworkKindObj()},
			initialCRDObjects: []runtime.Object{func() *apiextensionsv1.CustomResourceDefinition {
				crd := readyCRD()
				crd.Status.Conditions = append(crd.Status.Conditions, apiextensionsv1.CustomResourceDefinitionCondition{
					Type:   apiextensionsv1.Terminating,
					Status: apiextensionsv1.ConditionFalse,
				})
				return crd
			}()},
			podNetworkKindName: "example-com-mynetwork",
			expectedPodNetworkKind: func() *v1alpha1.PodNetworkKind {
				nk := podNetworkKindObj()
				nk.Status.Conditions = []metav1.Condition{{
					Type:    v1alpha1.PodNetworkKindConditionImplementationTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.PodNetworkKindReasonCRDNotReady,
					Message: "CRD is terminating",
				}}
				return nk
			}(),
		},
		{
			name:                         "CRD not conformant with API approval policy",
			initialPodNetworkKindObjects: []runtime.Object{podNetworkKindObj()},
			initialCRDObjects: []runtime.Object{func() *apiextensionsv1.CustomResourceDefinition {
				crd := readyCRD()
				crd.Status.Conditions = append(crd.Status.Conditions, apiextensionsv1.CustomResourceDefinitionCondition{
					Type:   apiextensionsv1.KubernetesAPIApprovalPolicyConformant,
					Status: apiextensionsv1.ConditionTrue,
				})
				return crd
			}()},
			podNetworkKindName: "example-com-mynetwork",
			expectedPodNetworkKind: func() *v1alpha1.PodNetworkKind {
				nk := podNetworkKindObj()
				nk.Status.Conditions = []metav1.Condition{{
					Type:    v1alpha1.PodNetworkKindConditionImplementationTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.PodNetworkKindReasonCRDNotReady,
					Message: "CRD is not conformant with the Kubernetes API approval policy",
				}}
				return nk
			}(),
		},
		{
			name:                         "CRD missing mandatory categories",
			initialPodNetworkKindObjects: []runtime.Object{podNetworkKindObj()},
			initialCRDObjects: []runtime.Object{func() *apiextensionsv1.CustomResourceDefinition {
				crd := readyCRD()
				crd.Spec.Names.Categories = nil
				return crd
			}()},
			podNetworkKindName: "example-com-mynetwork",
			expectedPodNetworkKind: func() *v1alpha1.PodNetworkKind {
				nk := podNetworkKindObj()
				nk.Status.Conditions = []metav1.Condition{{
					Type:    v1alpha1.PodNetworkKindConditionImplementationTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.PodNetworkKindReasonCRDMissingCategories,
					Message: "CRD is missing mandatory category podnetwork",
				}}
				return nk
			}(),
		},
		{
			name:                         "missing RBAC",
			initialPodNetworkKindObjects: []runtime.Object{podNetworkKindObj()},
			initialCRDObjects:            []runtime.Object{readyCRD()},
			podNetworkKindName:           "example-com-mynetwork",
			expectedPodNetworkKind: func() *v1alpha1.PodNetworkKind {
				nk := podNetworkKindObj()
				nk.Status.Conditions = []metav1.Condition{{
					Type:    v1alpha1.PodNetworkKindConditionImplementationTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.PodNetworkKindReasonMissingRBAC,
					Message: "Insufficient permissions to access the Custom Resources",
				}}
				return nk
			}(),
		},
		{
			name:                         "SSAR error",
			initialPodNetworkKindObjects: []runtime.Object{podNetworkKindObj()},
			initialCRDObjects:            []runtime.Object{readyCRD()},
			fakeKubeClientSetup: func(fakeKubeClient *fake.Clientset) {
				fakeKubeClient.PrependReactor("create", "selfsubjectaccessreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, fmt.Errorf("SSAR server error")
				})
			},
			podNetworkKindName: "example-com-mynetwork",
			wantErr:            true,
		},
		{
			name:                         "compliant",
			initialPodNetworkKindObjects: []runtime.Object{podNetworkKindObj()},
			initialCRDObjects:            []runtime.Object{readyCRD()},
			fakeKubeClientSetup:          allowSelfSubjectAccessReview,
			podNetworkKindName:           "example-com-mynetwork",
			expectedPodNetworkKind: func() *v1alpha1.PodNetworkKind {
				nk := podNetworkKindObj()
				nk.Status.Conditions = []metav1.Condition{{
					Type:    v1alpha1.PodNetworkKindConditionImplementationTypeReady,
					Status:  metav1.ConditionTrue,
					Reason:  v1alpha1.PodNetworkKindReasonCompliant,
					Message: "",
				}}
				return nk
			}(),
		},
		{
			name: "status has not changed",
			initialPodNetworkKindObjects: []runtime.Object{func() *v1alpha1.PodNetworkKind {
				nk := podNetworkKindObj()
				nk.Status.Conditions = []metav1.Condition{{
					Type:               v1alpha1.PodNetworkKindConditionImplementationTypeReady,
					Status:             metav1.ConditionTrue,
					Reason:             v1alpha1.PodNetworkKindReasonCompliant,
					Message:            "",
					ObservedGeneration: nk.Generation,
				}}
				return nk
			}()},
			initialCRDObjects:   []runtime.Object{readyCRD()},
			fakeKubeClientSetup: allowSelfSubjectAccessReview,
			fakeMultiNetworkClientSetup: func(fakeMultiNetworkClient *podNetworkKindFake.Clientset) {
				fakeMultiNetworkClient.PrependReactor("update", "podnetworkkinds", func(action k8stesting.Action) (bool, runtime.Object, error) {
					t.Errorf("UpdateStatus should not have been called when status did not change")
					return true, nil, nil
				})
			},
			podNetworkKindName: "example-com-mynetwork",
			expectedPodNetworkKind: func() *v1alpha1.PodNetworkKind {
				nk := podNetworkKindObj()
				nk.Status.Conditions = []metav1.Condition{{
					Type:               v1alpha1.PodNetworkKindConditionImplementationTypeReady,
					Status:             metav1.ConditionTrue,
					Reason:             v1alpha1.PodNetworkKindReasonCompliant,
					Message:            "",
					ObservedGeneration: nk.Generation,
				}}
				return nk
			}(),
		},
		{
			name:                         "failed status update",
			initialPodNetworkKindObjects: []runtime.Object{podNetworkKindObj()},
			initialCRDObjects:            []runtime.Object{readyCRD()},
			fakeKubeClientSetup:          allowSelfSubjectAccessReview,
			fakeMultiNetworkClientSetup: func(fakeMultiNetworkClient *podNetworkKindFake.Clientset) {
				fakeMultiNetworkClient.PrependReactor("update", "podnetworkkinds", func(action k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, fmt.Errorf("update failed")
				})
			},
			podNetworkKindName: "example-com-mynetwork",
			wantErr:            true,
		},
		{
			name:                         "unexpected object type in CRD indexer",
			initialPodNetworkKindObjects: []runtime.Object{podNetworkKindObj()},
			customReconcilerSetup: func(nkr *PodNetworkKindReconciler) {
				nkr.customResourceDefinitionIndexer = cache.NewIndexer(
					func(obj interface{}) (string, error) { return "test-key", nil },
					cache.Indexers{
						groupKindCustomResourceDefinitionIndex: func(obj interface{}) ([]string, error) {
							return []string{v1alpha1.GroupKind("example.com", "MyNetwork")}, nil
						},
					},
				)
				_ = nkr.customResourceDefinitionIndexer.Add(&v1alpha1.PodNetworkKind{
					ObjectMeta: metav1.ObjectMeta{Name: "unexpected-object"},
				})
			},
			podNetworkKindName: "example-com-mynetwork",
			wantErr:            true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			fakeKubeClient, fakeMultiNetworkClient, _, nkr := newReconciler(ctx, t, tt.initialPodNetworkKindObjects, tt.initialCRDObjects)

			if tt.fakeKubeClientSetup != nil {
				tt.fakeKubeClientSetup(fakeKubeClient)
			}
			if tt.fakeMultiNetworkClientSetup != nil {
				tt.fakeMultiNetworkClientSetup(fakeMultiNetworkClient)
			}
			if tt.customReconcilerSetup != nil {
				tt.customReconcilerSetup(nkr)
			}

			err := nkr.Reconcile(ctx, tt.podNetworkKindName)
			if (err != nil) != tt.wantErr {
				t.Errorf("Reconcile() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.expectedPodNetworkKind != nil {
				nk, getErr := fakeMultiNetworkClient.MultinetworkV1alpha1().PodNetworkKinds().Get(ctx, tt.podNetworkKindName, metav1.GetOptions{})
				if getErr != nil {
					t.Fatalf("failed to get PodNetworkKind: %v", getErr)
				}
				if diff := cmp.Diff(tt.expectedPodNetworkKind, nk,
					cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime"),
				); diff != "" {
					t.Errorf("PodNetworkKind mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestCrdCategory(t *testing.T) {
	tests := []struct {
		name        string
		categories  []string
		wantOk      bool
		wantMessage string
	}{
		{
			name:        "all required categories present",
			categories:  []string{"podnetwork", "podnetworks"},
			wantOk:      true,
			wantMessage: "",
		},
		{
			name:        "all required plus extra categories",
			categories:  []string{"all", "podnetwork", "podnetworks", "custom"},
			wantOk:      true,
			wantMessage: "",
		},
		{
			name:        "missing podnetworks",
			categories:  []string{"podnetwork"},
			wantOk:      false,
			wantMessage: "CRD is missing mandatory category podnetworks",
		},
		{
			name:        "missing podnetwork",
			categories:  []string{"podnetworks"},
			wantOk:      false,
			wantMessage: "CRD is missing mandatory category podnetwork",
		},
		{
			name:        "nil categories",
			categories:  nil,
			wantOk:      false,
			wantMessage: "CRD is missing mandatory category podnetwork",
		},
		{
			name:        "empty categories",
			categories:  []string{},
			wantOk:      false,
			wantMessage: "CRD is missing mandatory category podnetwork",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			crd := &apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Names: apiextensionsv1.CustomResourceDefinitionNames{
						Categories: tt.categories,
					},
				},
			}
			ok, msg := crdCategory(crd)
			if ok != tt.wantOk {
				t.Errorf("crdCategory() ok = %v, wantOk %v", ok, tt.wantOk)
			}
			if msg != tt.wantMessage {
				t.Errorf("crdCategory() message = %q, wantMessage %q", msg, tt.wantMessage)
			}
		})
	}
}

func TestCrdReady(t *testing.T) {
	readyConditions := []apiextensionsv1.CustomResourceDefinitionCondition{
		{Type: apiextensionsv1.Established, Status: apiextensionsv1.ConditionTrue},
		{Type: apiextensionsv1.NamesAccepted, Status: apiextensionsv1.ConditionTrue},
	}

	tests := []struct {
		name        string
		conditions  []apiextensionsv1.CustomResourceDefinitionCondition
		wantOk      bool
		wantMessage string
	}{
		{
			name:        "ready with Established and NamesAccepted true",
			conditions:  readyConditions,
			wantOk:      true,
			wantMessage: "",
		},
		{
			name: "Established condition false",
			conditions: []apiextensionsv1.CustomResourceDefinitionCondition{
				{Type: apiextensionsv1.Established, Status: apiextensionsv1.ConditionFalse},
				{Type: apiextensionsv1.NamesAccepted, Status: apiextensionsv1.ConditionTrue},
			},
			wantOk:      false,
			wantMessage: "CRD is not established",
		},
		{
			name: "NamesAccepted condition false",
			conditions: []apiextensionsv1.CustomResourceDefinitionCondition{
				{Type: apiextensionsv1.Established, Status: apiextensionsv1.ConditionTrue},
				{Type: apiextensionsv1.NamesAccepted, Status: apiextensionsv1.ConditionFalse},
			},
			wantOk:      false,
			wantMessage: "CRD names are not accepted",
		},
		{
			name: "NonStructuralSchema condition not true",
			conditions: []apiextensionsv1.CustomResourceDefinitionCondition{
				{Type: apiextensionsv1.Established, Status: apiextensionsv1.ConditionTrue},
				{Type: apiextensionsv1.NamesAccepted, Status: apiextensionsv1.ConditionTrue},
				{Type: apiextensionsv1.NonStructuralSchema, Status: apiextensionsv1.ConditionFalse},
			},
			wantOk:      false,
			wantMessage: "CRD has a structural schema issue",
		},
		{
			name: "Terminating condition not true",
			conditions: []apiextensionsv1.CustomResourceDefinitionCondition{
				{Type: apiextensionsv1.Established, Status: apiextensionsv1.ConditionTrue},
				{Type: apiextensionsv1.NamesAccepted, Status: apiextensionsv1.ConditionTrue},
				{Type: apiextensionsv1.Terminating, Status: apiextensionsv1.ConditionFalse},
			},
			wantOk:      false,
			wantMessage: "CRD is terminating",
		},
		{
			name: "KubernetesAPIApprovalPolicyConformant not false",
			conditions: []apiextensionsv1.CustomResourceDefinitionCondition{
				{Type: apiextensionsv1.Established, Status: apiextensionsv1.ConditionTrue},
				{Type: apiextensionsv1.NamesAccepted, Status: apiextensionsv1.ConditionTrue},
				{Type: apiextensionsv1.KubernetesAPIApprovalPolicyConformant, Status: apiextensionsv1.ConditionTrue},
			},
			wantOk:      false,
			wantMessage: "CRD is not conformant with the Kubernetes API approval policy",
		},
		{
			name:        "missing Established and NamesAccepted conditions",
			conditions:  []apiextensionsv1.CustomResourceDefinitionCondition{},
			wantOk:      false,
			wantMessage: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			crd := &apiextensionsv1.CustomResourceDefinition{
				Status: apiextensionsv1.CustomResourceDefinitionStatus{
					Conditions: tt.conditions,
				},
			}
			ok, msg := crdReady(crd)
			if ok != tt.wantOk {
				t.Errorf("crdReady() ok = %v, wantOk %v", ok, tt.wantOk)
			}
			if tt.wantMessage != "" && msg != tt.wantMessage {
				t.Errorf("crdReady() message = %q, wantMessage %q", msg, tt.wantMessage)
			}
			if !tt.wantOk && tt.wantMessage == "" && len(msg) == 0 {
				t.Errorf("crdReady() expected non-empty message on failure")
			}
		})
	}
}

func TestHasStatusChanged(t *testing.T) {
	cond := func(t string, s metav1.ConditionStatus, r, m string) metav1.Condition {
		return metav1.Condition{Type: t, Status: s, Reason: r, Message: m}
	}

	tests := []struct {
		name     string
		oldPNK   *v1alpha1.PodNetworkKind
		newPNK   *v1alpha1.PodNetworkKind
		expected bool
	}{
		{
			name:     "identical empty conditions",
			oldPNK:   &v1alpha1.PodNetworkKind{},
			newPNK:   &v1alpha1.PodNetworkKind{},
			expected: false,
		},
		{
			name: "identical single condition",
			oldPNK: &v1alpha1.PodNetworkKind{
				Status: v1alpha1.PodNetworkKindStatus{
					Conditions: []metav1.Condition{cond("Ready", metav1.ConditionTrue, "Compliant", "All good")},
				},
			},
			newPNK: &v1alpha1.PodNetworkKind{
				Status: v1alpha1.PodNetworkKindStatus{
					Conditions: []metav1.Condition{cond("Ready", metav1.ConditionTrue, "Compliant", "All good")},
				},
			},
			expected: false,
		},
		{
			name: "different lengths",
			oldPNK: &v1alpha1.PodNetworkKind{
				Status: v1alpha1.PodNetworkKindStatus{
					Conditions: []metav1.Condition{},
				},
			},
			newPNK: &v1alpha1.PodNetworkKind{
				Status: v1alpha1.PodNetworkKindStatus{
					Conditions: []metav1.Condition{cond("Ready", metav1.ConditionTrue, "Compliant", "")},
				},
			},
			expected: true,
		},
		{
			name: "different Type",
			oldPNK: &v1alpha1.PodNetworkKind{
				Status: v1alpha1.PodNetworkKindStatus{
					Conditions: []metav1.Condition{cond("Ready", metav1.ConditionTrue, "Compliant", "")},
				},
			},
			newPNK: &v1alpha1.PodNetworkKind{
				Status: v1alpha1.PodNetworkKindStatus{
					Conditions: []metav1.Condition{cond("Other", metav1.ConditionTrue, "Compliant", "")},
				},
			},
			expected: true,
		},
		{
			name: "different Status",
			oldPNK: &v1alpha1.PodNetworkKind{
				Status: v1alpha1.PodNetworkKindStatus{
					Conditions: []metav1.Condition{cond("Ready", metav1.ConditionTrue, "Compliant", "")},
				},
			},
			newPNK: &v1alpha1.PodNetworkKind{
				Status: v1alpha1.PodNetworkKindStatus{
					Conditions: []metav1.Condition{cond("Ready", metav1.ConditionFalse, "Compliant", "")},
				},
			},
			expected: true,
		},
		{
			name: "different Reason",
			oldPNK: &v1alpha1.PodNetworkKind{
				Status: v1alpha1.PodNetworkKindStatus{
					Conditions: []metav1.Condition{cond("Ready", metav1.ConditionTrue, "Compliant", "")},
				},
			},
			newPNK: &v1alpha1.PodNetworkKind{
				Status: v1alpha1.PodNetworkKindStatus{
					Conditions: []metav1.Condition{cond("Ready", metav1.ConditionTrue, "OtherReason", "")},
				},
			},
			expected: true,
		},
		{
			name: "different Message",
			oldPNK: &v1alpha1.PodNetworkKind{
				Status: v1alpha1.PodNetworkKindStatus{
					Conditions: []metav1.Condition{cond("Ready", metav1.ConditionTrue, "Compliant", "msg1")},
				},
			},
			newPNK: &v1alpha1.PodNetworkKind{
				Status: v1alpha1.PodNetworkKindStatus{
					Conditions: []metav1.Condition{cond("Ready", metav1.ConditionTrue, "Compliant", "msg2")},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasStatusChanged(tt.oldPNK, tt.newPNK)
			if got != tt.expected {
				t.Errorf("hasStatusChanged() = %v, want %v", got, tt.expected)
			}
		})
	}
}
