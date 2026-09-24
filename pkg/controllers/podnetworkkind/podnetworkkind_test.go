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
	"sync"
	"testing"
	"time"

	"github.com/kubernetes-sigs/multi-network-api/apis/v1alpha1"
	podNetworkKindFake "github.com/kubernetes-sigs/multi-network-api/pkg/client/clientset/versioned/fake"
	podNetworkKindInformers "github.com/kubernetes-sigs/multi-network-api/pkg/client/informers/externalversions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsFake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	apiextensionsinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// fakeReconciler is a simple implementation of the reconciler interface for testing purposes.
// It records the podNetworkKindNames that have to be reconciled.
type fakeReconciler struct {
	mu                  sync.Mutex
	podNetworkKindNames []string
}

func (f *fakeReconciler) Reconcile(_ context.Context, podNetworkKindName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.podNetworkKindNames = append(f.podNetworkKindNames, podNetworkKindName)
	return nil
}

func (f *fakeReconciler) getPodNetworkKindNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]string, len(f.podNetworkKindNames))
	copy(result, f.podNetworkKindNames)
	return result
}

func newController(
	ctx context.Context,
	t *testing.T,
	initialPodNetworkKindObjects []runtime.Object,
	initialCRDObjects []runtime.Object,
) (*fake.Clientset, *podNetworkKindFake.Clientset, *apiextensionsFake.Clientset, *PodNetworkKindController, *fakeReconciler) {
	fakeKubeClient := fake.NewSimpleClientset()
	fakeMultiNetworkClient := podNetworkKindFake.NewSimpleClientset(initialPodNetworkKindObjects...)
	fakeApiExtensionsClient := apiextensionsFake.NewSimpleClientset(initialCRDObjects...)

	podNetworkKindInformerFactory := podNetworkKindInformers.NewSharedInformerFactory(fakeMultiNetworkClient, 0)
	apiextensionsInformerFactory := apiextensionsinformers.NewSharedInformerFactory(fakeApiExtensionsClient, 0)

	controller, err := NewPodNetworkKindController(
		podNetworkKindInformerFactory.Multinetwork().V1alpha1().PodNetworkKinds(),
		apiextensionsInformerFactory.Apiextensions().V1().CustomResourceDefinitions(),
		fakeMultiNetworkClient.MultinetworkV1alpha1().PodNetworkKinds(),
		fakeKubeClient,
	)
	if err != nil {
		t.Fatal("NewPodNetworkKindController failed:", err)
	}

	fr := &fakeReconciler{}
	controller.podNetworkKindReconciler = fr

	podNetworkKindInformerFactory.Start(ctx.Done())
	apiextensionsInformerFactory.Start(ctx.Done())
	podNetworkKindInformerFactory.WaitForCacheSync(ctx.Done())
	apiextensionsInformerFactory.WaitForCacheSync(ctx.Done())

	return fakeKubeClient, fakeMultiNetworkClient, fakeApiExtensionsClient, controller, fr
}

func TestEventHandlers(t *testing.T) {
	type object interface {
		runtime.Object
		metav1.Object
	}

	tests := []struct {
		name                               string
		initialPodNetworkKindObjects       []runtime.Object
		initialCRDObjects                  []runtime.Object
		initialExpectedPodNetworkKindNames []string
		createObjects                      []object
		updateObjects                      []object
		deleteObjects                      []object
		expectedPodNetworkKindNames        []string
	}{
		{
			name:                         "no event",
			initialPodNetworkKindObjects: []runtime.Object{},
			initialCRDObjects:            []runtime.Object{},
			createObjects:                []object{},
			updateObjects:                []object{},
			deleteObjects:                []object{},
			expectedPodNetworkKindNames:  []string{},
		},
		{
			name: "initial PodNetworkKind",
			initialPodNetworkKindObjects: []runtime.Object{
				&v1alpha1.PodNetworkKind{ObjectMeta: metav1.ObjectMeta{Name: "my-network-kind"}},
			},
			expectedPodNetworkKindNames: []string{"my-network-kind"},
		},
		{
			name: "initial CustomResourceDefinition",
			initialCRDObjects: []runtime.Object{
				&apiextensionsv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{Name: "mynetworks.example.com"},
					Spec: apiextensionsv1.CustomResourceDefinitionSpec{
						Group: "example.com",
						Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: "MyNetwork"},
					},
				},
			},
			expectedPodNetworkKindNames: []string{"example-com-mynetwork"},
		},
		{
			name: "create PodNetworkKind",
			createObjects: []object{
				&v1alpha1.PodNetworkKind{ObjectMeta: metav1.ObjectMeta{Name: "my-network-kind"}},
			},
			expectedPodNetworkKindNames: []string{"my-network-kind"},
		},
		{
			name: "create CustomResourceDefinition",
			createObjects: []object{
				&apiextensionsv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{Name: "mynetworks.example.com"},
					Spec: apiextensionsv1.CustomResourceDefinitionSpec{
						Group: "example.com",
						Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: "MyNetwork"},
					},
				},
			},
			expectedPodNetworkKindNames: []string{"example-com-mynetwork"},
		},
		{
			name: "delete PodNetworkKind",
			initialPodNetworkKindObjects: []runtime.Object{
				&v1alpha1.PodNetworkKind{ObjectMeta: metav1.ObjectMeta{Name: "my-network-kind"}},
			},
			initialExpectedPodNetworkKindNames: []string{"my-network-kind"},
			deleteObjects: []object{
				&v1alpha1.PodNetworkKind{ObjectMeta: metav1.ObjectMeta{Name: "my-network-kind"}},
			},
			expectedPodNetworkKindNames: []string{"my-network-kind"},
		},
		{
			name: "delete CustomResourceDefinition",
			initialCRDObjects: []runtime.Object{
				&apiextensionsv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{Name: "mynetworks.example.com"},
					Spec: apiextensionsv1.CustomResourceDefinitionSpec{
						Group: "example.com",
						Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: "MyNetwork"},
					},
				},
			},
			initialExpectedPodNetworkKindNames: []string{"example-com-mynetwork"},
			deleteObjects: []object{
				&apiextensionsv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{Name: "mynetworks.example.com"},
					Spec: apiextensionsv1.CustomResourceDefinitionSpec{
						Group: "example.com",
						Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: "MyNetwork"},
					},
				},
			},
			expectedPodNetworkKindNames: []string{"example-com-mynetwork"},
		},
		{
			name: "update CustomResourceDefinition enqueues both old and new names",
			initialCRDObjects: []runtime.Object{
				&apiextensionsv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{Name: "mynetworks.example.com"},
					Spec: apiextensionsv1.CustomResourceDefinitionSpec{
						Group: "old.example.com",
						Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: "OldNetwork"},
					},
				},
			},
			initialExpectedPodNetworkKindNames: []string{"old-example-com-oldnetwork"},
			updateObjects: []object{
				&apiextensionsv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{Name: "mynetworks.example.com"},
					Spec: apiextensionsv1.CustomResourceDefinitionSpec{
						Group: "new.example.com",
						Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: "NewNetwork"},
					},
				},
			},
			expectedPodNetworkKindNames: []string{"old-example-com-oldnetwork", "new-example-com-newnetwork"},
		},
		{
			name: "multiple initial PodNetworkKinds",
			initialPodNetworkKindObjects: []runtime.Object{
				&v1alpha1.PodNetworkKind{ObjectMeta: metav1.ObjectMeta{Name: "nk-a"}},
				&v1alpha1.PodNetworkKind{ObjectMeta: metav1.ObjectMeta{Name: "nk-b"}},
			},
			expectedPodNetworkKindNames: []string{"nk-a", "nk-b"},
		},
		{
			name: "initial PodNetworkKind and CustomResourceDefinition",
			initialPodNetworkKindObjects: []runtime.Object{
				&v1alpha1.PodNetworkKind{ObjectMeta: metav1.ObjectMeta{Name: "my-network-kind"}},
			},
			initialCRDObjects: []runtime.Object{
				&apiextensionsv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{Name: "mynetworks.example.com"},
					Spec: apiextensionsv1.CustomResourceDefinitionSpec{
						Group: "example.com",
						Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: "MyNetwork"},
					},
				},
			},
			expectedPodNetworkKindNames: []string{"my-network-kind", "example-com-mynetwork"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			_, fakeMultiNetworkClient, fakeApiExtensionsClient, podNetworkKindController, fr := newController(
				ctx,
				t,
				tt.initialPodNetworkKindObjects,
				tt.initialCRDObjects,
			)

			go func() {
				_ = podNetworkKindController.Run(ctx, 1)
			}()

			if len(tt.initialExpectedPodNetworkKindNames) > 0 {
				if err := wait.PollUntilContextTimeout(ctx, 1*time.Millisecond, 2*time.Second, true, func(ctx context.Context) (bool, error) {
					return len(fr.getPodNetworkKindNames()) >= len(tt.initialExpectedPodNetworkKindNames), nil
				}); err != nil {
					t.Fatalf("timed out waiting for initial reconciliation, got: %v, want at least: %v", fr.getPodNetworkKindNames(), tt.initialExpectedPodNetworkKindNames)
				}
			}

			// Helper function to perform create, update, and delete actions on objects
			action := func(obj object, actionType string) {
				var gvr *schema.GroupVersionResource
				var objectTracker k8stesting.ObjectTracker

				switch obj.(type) {
				case *v1alpha1.PodNetworkKind:
					r := schema.GroupVersionResource{Group: v1alpha1.GroupVersion.Group, Version: v1alpha1.GroupVersion.Version, Resource: "podnetworkkinds"}
					gvr = &r
					objectTracker = fakeMultiNetworkClient.Tracker()
				case *apiextensionsv1.CustomResourceDefinition:
					r := apiextensionsv1.SchemeGroupVersion.WithResource("customresourcedefinitions")
					gvr = &r
					objectTracker = fakeApiExtensionsClient.Tracker()
				}

				if gvr == nil || objectTracker == nil {
					t.Fatal("gvr or objectTracker is nil")
				}

				var err error
				switch actionType {
				case "create":
					err = objectTracker.Create(*gvr, obj, obj.GetNamespace(), metav1.CreateOptions{})
				case "update":
					err = objectTracker.Update(*gvr, obj, obj.GetNamespace(), metav1.UpdateOptions{})
				case "delete":
					err = objectTracker.Delete(*gvr, obj.GetNamespace(), obj.GetName(), metav1.DeleteOptions{})
				}

				if err != nil {
					t.Fatal("action failed:", err)
				}
			}

			for _, object := range tt.createObjects {
				action(object, "create")
			}
			for _, object := range tt.updateObjects {
				action(object, "update")
			}
			for _, object := range tt.deleteObjects {
				action(object, "delete")
			}

			if err := wait.PollUntilContextTimeout(ctx, 1*time.Millisecond, 2*time.Second, true, func(ctx context.Context) (bool, error) {
				return len(fr.getPodNetworkKindNames()) >= len(tt.expectedPodNetworkKindNames), nil
			}); err != nil {
				t.Fatalf("timed out waiting for %d reconciled keys, got: %v", len(tt.expectedPodNetworkKindNames), fr.getPodNetworkKindNames())
			}

			actualKeys := fr.getPodNetworkKindNames()
			for _, expected := range tt.expectedPodNetworkKindNames {
				found := false
				for _, actual := range actualKeys {
					if actual == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected key %q not found in reconciled keys: %v", expected, actualKeys)
				}
			}
		})
	}
}
