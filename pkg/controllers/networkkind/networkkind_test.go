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
	"sync"
	"testing"
	"time"

	"github.com/kubernetes-sigs/multi-network-api/apis/v1alpha1"
	networkKindFake "github.com/kubernetes-sigs/multi-network-api/pkg/client/clientset/versioned/fake"
	networkKindInformers "github.com/kubernetes-sigs/multi-network-api/pkg/client/informers/externalversions"
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
// It records the networkKindNames that have to be reconciled.
type fakeReconciler struct {
	mu               sync.Mutex
	networkKindNames []string
}

func (f *fakeReconciler) Reconcile(_ context.Context, networkKindName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.networkKindNames = append(f.networkKindNames, networkKindName)
	return nil
}

func (f *fakeReconciler) getNetworkKindNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]string, len(f.networkKindNames))
	copy(result, f.networkKindNames)
	return result
}

func newController(
	ctx context.Context,
	t *testing.T,
	initialNetworkKindObjects []runtime.Object,
	initialCRDObjects []runtime.Object,
) (*fake.Clientset, *networkKindFake.Clientset, *apiextensionsFake.Clientset, *NetworkKindController, *fakeReconciler) {
	fakeKubeClient := fake.NewSimpleClientset()
	fakeMultiNetworkClient := networkKindFake.NewSimpleClientset(initialNetworkKindObjects...)
	fakeApiExtensionsClient := apiextensionsFake.NewSimpleClientset(initialCRDObjects...)

	networkKindInformerFactory := networkKindInformers.NewSharedInformerFactory(fakeMultiNetworkClient, 0)
	apiextensionsInformerFactory := apiextensionsinformers.NewSharedInformerFactory(fakeApiExtensionsClient, 0)

	controller, err := NewNetworkKindController(
		networkKindInformerFactory.Multinetwork().V1alpha1().NetworkKinds(),
		apiextensionsInformerFactory.Apiextensions().V1().CustomResourceDefinitions(),
		fakeMultiNetworkClient.MultinetworkV1alpha1().NetworkKinds(),
		fakeKubeClient,
	)
	if err != nil {
		t.Fatal("NewNetworkKindController failed:", err)
	}

	fr := &fakeReconciler{}
	controller.networkKindReconciler = fr

	networkKindInformerFactory.Start(ctx.Done())
	apiextensionsInformerFactory.Start(ctx.Done())
	networkKindInformerFactory.WaitForCacheSync(ctx.Done())
	apiextensionsInformerFactory.WaitForCacheSync(ctx.Done())

	return fakeKubeClient, fakeMultiNetworkClient, fakeApiExtensionsClient, controller, fr
}

func TestEventHandlers(t *testing.T) {
	type object interface {
		runtime.Object
		metav1.Object
	}

	tests := []struct {
		name                      string
		initialNetworkKindObjects []runtime.Object
		initialCRDObjects         []runtime.Object
		createObjects             []object
		updateObjects             []object
		deleteObjects             []object
		expectedNetworkKindNames  []string
	}{
		{
			name:                      "no event",
			initialNetworkKindObjects: []runtime.Object{},
			initialCRDObjects:         []runtime.Object{},
			createObjects:             []object{},
			updateObjects:             []object{},
			deleteObjects:             []object{},
			expectedNetworkKindNames:  []string{},
		},
		{
			name: "initial NetworkKind",
			initialNetworkKindObjects: []runtime.Object{
				&v1alpha1.NetworkKind{ObjectMeta: metav1.ObjectMeta{Name: "my-network-kind"}},
			},
			expectedNetworkKindNames: []string{"my-network-kind"},
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
			expectedNetworkKindNames: []string{"example-com-mynetwork"},
		},
		{
			name: "create NetworkKind",
			createObjects: []object{
				&v1alpha1.NetworkKind{ObjectMeta: metav1.ObjectMeta{Name: "my-network-kind"}},
			},
			expectedNetworkKindNames: []string{"my-network-kind"},
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
			expectedNetworkKindNames: []string{"example-com-mynetwork"},
		},
		{
			name: "delete NetworkKind",
			initialNetworkKindObjects: []runtime.Object{
				&v1alpha1.NetworkKind{ObjectMeta: metav1.ObjectMeta{Name: "my-network-kind"}},
			},
			deleteObjects: []object{
				&v1alpha1.NetworkKind{ObjectMeta: metav1.ObjectMeta{Name: "my-network-kind"}},
			},
			expectedNetworkKindNames: []string{"my-network-kind"},
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
			deleteObjects: []object{
				&apiextensionsv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{Name: "mynetworks.example.com"},
					Spec: apiextensionsv1.CustomResourceDefinitionSpec{
						Group: "example.com",
						Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: "MyNetwork"},
					},
				},
			},
			expectedNetworkKindNames: []string{"example-com-mynetwork"},
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
			updateObjects: []object{
				&apiextensionsv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{Name: "mynetworks.example.com"},
					Spec: apiextensionsv1.CustomResourceDefinitionSpec{
						Group: "new.example.com",
						Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: "NewNetwork"},
					},
				},
			},
			expectedNetworkKindNames: []string{"old-example-com-oldnetwork", "new-example-com-newnetwork"},
		},
		{
			name: "multiple initial NetworkKinds",
			initialNetworkKindObjects: []runtime.Object{
				&v1alpha1.NetworkKind{ObjectMeta: metav1.ObjectMeta{Name: "nk-a"}},
				&v1alpha1.NetworkKind{ObjectMeta: metav1.ObjectMeta{Name: "nk-b"}},
			},
			expectedNetworkKindNames: []string{"nk-a", "nk-b"},
		},
		{
			name: "initial NetworkKind and CustomResourceDefinition",
			initialNetworkKindObjects: []runtime.Object{
				&v1alpha1.NetworkKind{ObjectMeta: metav1.ObjectMeta{Name: "my-network-kind"}},
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
			expectedNetworkKindNames: []string{"my-network-kind", "example-com-mynetwork"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			_, fakeMultiNetworkClient, fakeApiExtensionsClient, networkKindController, fr := newController(
				ctx,
				t,
				tt.initialNetworkKindObjects,
				tt.initialCRDObjects,
			)

			go func() {
				_ = networkKindController.Run(ctx, 1)
			}()

			// Helper function to perform create, update, and delete actions on objects
			action := func(obj object, actionType string) {
				var gvr *schema.GroupVersionResource
				var objectTracker k8stesting.ObjectTracker

				switch obj.(type) {
				case *v1alpha1.NetworkKind:
					r := schema.GroupVersionResource{Group: v1alpha1.GroupVersion.Group, Version: v1alpha1.GroupVersion.Version, Resource: "networkkinds"}
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
				return len(fr.getNetworkKindNames()) >= len(tt.expectedNetworkKindNames), nil
			}); err != nil {
				t.Fatalf("timed out waiting for %d reconciled keys, got: %v", len(tt.expectedNetworkKindNames), fr.getNetworkKindNames())
			}

			actualKeys := fr.getNetworkKindNames()
			for _, expected := range tt.expectedNetworkKindNames {
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
