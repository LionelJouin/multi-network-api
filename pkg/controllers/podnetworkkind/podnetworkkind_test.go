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
	"sync"
	"testing"
	"time"

	"github.com/kubernetes-sigs/multi-network-api/apis/v1alpha1"
	podNetworkKindFake "github.com/kubernetes-sigs/multi-network-api/pkg/client/clientset/versioned/fake"
	podNetworkKindInformers "github.com/kubernetes-sigs/multi-network-api/pkg/client/informers/externalversions"
	"github.com/kubernetes-sigs/multi-network-api/pkg/ruleswatcher"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsFake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	apiextensionsinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/ptr"
)

// fakeReconciler is a simple implementation of the reconciler interface for testing purposes.
// It records the podNetworkKindNames that have to be reconciled.
type fakeReconciler struct {
	mu                  sync.Mutex
	podNetworkKindNames []string
	reconcileErr        error
}

func (f *fakeReconciler) Reconcile(_ context.Context, podNetworkKindName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.podNetworkKindNames = append(f.podNetworkKindNames, podNetworkKindName)
	return f.reconcileErr
}

func (f *fakeReconciler) setReconcileErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reconcileErr = err
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

	ruleWatcher := ruleswatcher.New(fakeKubeClient)
	_ = ruleWatcher.Sync(ctx)

	controller, err := NewPodNetworkKindController(
		podNetworkKindInformerFactory.Multinetwork().V1alpha1().PodNetworkKinds(),
		apiextensionsInformerFactory.Apiextensions().V1().CustomResourceDefinitions(),
		fakeMultiNetworkClient.MultinetworkV1alpha1().PodNetworkKinds(),
		ruleWatcher,
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
			name: "update PodNetworkKind",
			initialPodNetworkKindObjects: []runtime.Object{
				&v1alpha1.PodNetworkKind{ObjectMeta: metav1.ObjectMeta{Name: "my-network-kind"}},
			},
			initialExpectedPodNetworkKindNames: []string{"my-network-kind"},
			updateObjects: []object{
				&v1alpha1.PodNetworkKind{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "my-network-kind",
						Labels: map[string]string{"updated": "true"},
					},
				},
			},
			expectedPodNetworkKindNames: []string{"my-network-kind", "my-network-kind"},
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
			expectedPodNetworkKindNames: []string{"my-network-kind", "my-network-kind"},
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
			expectedPodNetworkKindNames: []string{"example-com-mynetwork", "example-com-mynetwork"},
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
			expectedPodNetworkKindNames: []string{"old-example-com-oldnetwork", "old-example-com-oldnetwork", "new-example-com-newnetwork"},
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
		{
			name: "create PodNetworkKind requesting default enqueues existing default candidate",
			initialPodNetworkKindObjects: []runtime.Object{
				&v1alpha1.PodNetworkKind{
					ObjectMeta: metav1.ObjectMeta{Name: "pnk-a"},
					Spec: v1alpha1.PodNetworkKindSpec{
						DefaultPodNetworkKind: ptr.To(true),
					},
				},
			},
			initialExpectedPodNetworkKindNames: []string{"pnk-a"},
			createObjects: []object{
				&v1alpha1.PodNetworkKind{
					ObjectMeta: metav1.ObjectMeta{Name: "pnk-b"},
					Spec: v1alpha1.PodNetworkKindSpec{
						DefaultPodNetworkKind: ptr.To(true),
					},
				},
			},
			expectedPodNetworkKindNames: []string{"pnk-a", "pnk-b", "pnk-a"},
		},
		{
			name: "delete PodNetworkKind requesting default enqueues remaining default candidate",
			initialPodNetworkKindObjects: []runtime.Object{
				&v1alpha1.PodNetworkKind{
					ObjectMeta: metav1.ObjectMeta{Name: "pnk-a"},
					Spec: v1alpha1.PodNetworkKindSpec{
						DefaultPodNetworkKind: ptr.To(true),
					},
				},
				&v1alpha1.PodNetworkKind{
					ObjectMeta: metav1.ObjectMeta{Name: "pnk-b"},
					Spec: v1alpha1.PodNetworkKindSpec{
						DefaultPodNetworkKind: ptr.To(true),
					},
				},
			},
			initialExpectedPodNetworkKindNames: []string{"pnk-a", "pnk-b"},
			deleteObjects: []object{
				&v1alpha1.PodNetworkKind{
					ObjectMeta: metav1.ObjectMeta{Name: "pnk-a"},
					Spec: v1alpha1.PodNetworkKindSpec{
						DefaultPodNetworkKind: ptr.To(true),
					},
				},
			},
			expectedPodNetworkKindNames: []string{"pnk-a", "pnk-b", "pnk-b"},
		},
		{
			name: "update PodNetworkKind requesting default enqueues all default candidates",
			initialPodNetworkKindObjects: []runtime.Object{
				&v1alpha1.PodNetworkKind{
					ObjectMeta: metav1.ObjectMeta{Name: "pnk-a"},
					Spec: v1alpha1.PodNetworkKindSpec{
						DefaultPodNetworkKind: ptr.To(true),
					},
				},
				&v1alpha1.PodNetworkKind{
					ObjectMeta: metav1.ObjectMeta{Name: "pnk-b"},
					Spec: v1alpha1.PodNetworkKindSpec{
						DefaultPodNetworkKind: ptr.To(true),
					},
				},
			},
			initialExpectedPodNetworkKindNames: []string{"pnk-a", "pnk-b"},
			updateObjects: []object{
				&v1alpha1.PodNetworkKind{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "pnk-b",
						Labels: map[string]string{"foo": "bar"},
					},
					Spec: v1alpha1.PodNetworkKindSpec{
						DefaultPodNetworkKind: ptr.To(true),
					},
				},
			},
			expectedPodNetworkKindNames: []string{"pnk-a", "pnk-b", "pnk-a", "pnk-b"},
		},
		{
			name: "create PodNetworkKind with default false does not enqueue other candidates",
			initialPodNetworkKindObjects: []runtime.Object{
				&v1alpha1.PodNetworkKind{
					ObjectMeta: metav1.ObjectMeta{Name: "pnk-a"},
					Spec: v1alpha1.PodNetworkKindSpec{
						DefaultPodNetworkKind: ptr.To(true),
					},
				},
			},
			initialExpectedPodNetworkKindNames: []string{"pnk-a"},
			createObjects: []object{
				&v1alpha1.PodNetworkKind{
					ObjectMeta: metav1.ObjectMeta{Name: "pnk-non-default"},
					Spec: v1alpha1.PodNetworkKindSpec{
						DefaultPodNetworkKind: ptr.To(false),
					},
				},
			},
			expectedPodNetworkKindNames: []string{"pnk-a", "pnk-non-default"},
		},
		{
			name: "update CustomResourceDefinition without GroupKind change enqueues name",
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
			updateObjects: []object{
				&apiextensionsv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "mynetworks.example.com",
						Labels: map[string]string{"updated": "true"},
					},
					Spec: apiextensionsv1.CustomResourceDefinitionSpec{
						Group: "example.com",
						Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: "MyNetwork"},
					},
				},
			},
			expectedPodNetworkKindNames: []string{"example-com-mynetwork", "example-com-mynetwork"},
		},
		{
			name: "multiple CRDs created enqueues all corresponding names",
			createObjects: []object{
				&apiextensionsv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{Name: "netas.a.com"},
					Spec: apiextensionsv1.CustomResourceDefinitionSpec{
						Group: "a.com",
						Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: "NetA"},
					},
				},
				&apiextensionsv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{Name: "netbs.b.com"},
					Spec: apiextensionsv1.CustomResourceDefinitionSpec{
						Group: "b.com",
						Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: "NetB"},
					},
				},
			},
			expectedPodNetworkKindNames: []string{"a-com-neta", "b-com-netb"},
		},
		{
			name: "create both PodNetworkKind and CustomResourceDefinition with different names",
			createObjects: []object{
				&v1alpha1.PodNetworkKind{ObjectMeta: metav1.ObjectMeta{Name: "my-pnk"}},
				&apiextensionsv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{Name: "mynetworks.example.com"},
					Spec: apiextensionsv1.CustomResourceDefinitionSpec{
						Group: "example.com",
						Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: "MyNetwork"},
					},
				},
			},
			expectedPodNetworkKindNames: []string{"my-pnk", "example-com-mynetwork"},
		},
		{
			name: "delete CRD and PodNetworkKind together",
			initialPodNetworkKindObjects: []runtime.Object{
				&v1alpha1.PodNetworkKind{ObjectMeta: metav1.ObjectMeta{Name: "my-pnk"}},
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
			initialExpectedPodNetworkKindNames: []string{"my-pnk", "example-com-mynetwork"},
			deleteObjects: []object{
				&v1alpha1.PodNetworkKind{ObjectMeta: metav1.ObjectMeta{Name: "my-pnk"}},
				&apiextensionsv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{Name: "mynetworks.example.com"},
					Spec: apiextensionsv1.CustomResourceDefinitionSpec{
						Group: "example.com",
						Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: "MyNetwork"},
					},
				},
			},
			expectedPodNetworkKindNames: []string{"my-pnk", "example-com-mynetwork", "my-pnk", "example-com-mynetwork"},
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

func TestNewPodNetworkKindController(t *testing.T) {
	t.Run("successful controller initialization", func(t *testing.T) {
		fakeKubeClient := fake.NewSimpleClientset()
		fakeMultiNetworkClient := podNetworkKindFake.NewSimpleClientset()
		fakeApiExtensionsClient := apiextensionsFake.NewSimpleClientset()

		podNetworkKindInformerFactory := podNetworkKindInformers.NewSharedInformerFactory(fakeMultiNetworkClient, 0)
		apiextensionsInformerFactory := apiextensionsinformers.NewSharedInformerFactory(fakeApiExtensionsClient, 0)

		ruleWatcher := ruleswatcher.New(fakeKubeClient)

		controller, err := NewPodNetworkKindController(
			podNetworkKindInformerFactory.Multinetwork().V1alpha1().PodNetworkKinds(),
			apiextensionsInformerFactory.Apiextensions().V1().CustomResourceDefinitions(),
			fakeMultiNetworkClient.MultinetworkV1alpha1().PodNetworkKinds(),
			ruleWatcher,
			fakeKubeClient,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if controller == nil {
			t.Fatal("expected controller to be non-nil")
		}
		if controller.podNetworkKindQueue == nil {
			t.Fatal("expected podNetworkKindQueue to be non-nil")
		}
		if controller.podNetworkKindReconciler == nil {
			t.Fatal("expected podNetworkKindReconciler to be non-nil")
		}
	})

	t.Run("error when adding indexer fails", func(t *testing.T) {
		fakeKubeClient := fake.NewSimpleClientset()
		fakeMultiNetworkClient := podNetworkKindFake.NewSimpleClientset()
		fakeApiExtensionsClient := apiextensionsFake.NewSimpleClientset()

		podNetworkKindInformerFactory := podNetworkKindInformers.NewSharedInformerFactory(fakeMultiNetworkClient, 0)
		apiextensionsInformerFactory := apiextensionsinformers.NewSharedInformerFactory(fakeApiExtensionsClient, 0)

		crdInformer := apiextensionsInformerFactory.Apiextensions().V1().CustomResourceDefinitions()
		err := crdInformer.Informer().GetIndexer().AddIndexers(cache.Indexers{
			groupKindCustomResourceDefinitionIndex: func(obj interface{}) ([]string, error) {
				return nil, nil
			},
		})
		if err != nil {
			t.Fatalf("failed to add indexer: %v", err)
		}

		ruleWatcher := ruleswatcher.New(fakeKubeClient)

		controller, err := NewPodNetworkKindController(
			podNetworkKindInformerFactory.Multinetwork().V1alpha1().PodNetworkKinds(),
			crdInformer,
			fakeMultiNetworkClient.MultinetworkV1alpha1().PodNetworkKinds(),
			ruleWatcher,
			fakeKubeClient,
		)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if controller != nil {
			t.Fatal("expected controller to be nil on error")
		}
	})
}

func TestEnqueuePodNetworkKind(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	_, _, _, controller, _ := newController(ctx, t, nil, nil)

	t.Run("valid PodNetworkKind", func(t *testing.T) {
		pnk := &v1alpha1.PodNetworkKind{
			ObjectMeta: metav1.ObjectMeta{Name: "my-network-kind"},
		}
		controller.enqueuePodNetworkKind(pnk)

		if controller.podNetworkKindQueue.Len() != 1 {
			t.Fatalf("expected queue length 1, got %d", controller.podNetworkKindQueue.Len())
		}
		key, _ := controller.podNetworkKindQueue.Get()
		if key != "my-network-kind" {
			t.Errorf("expected key %q, got %q", "my-network-kind", key)
		}
		controller.podNetworkKindQueue.Done(key)
	})

	t.Run("invalid object type", func(t *testing.T) {
		controller.enqueuePodNetworkKind("not-a-pod-network-kind")
		if controller.podNetworkKindQueue.Len() != 0 {
			t.Fatalf("expected queue length 0, got %d", controller.podNetworkKindQueue.Len())
		}
	})

	t.Run("DeletedFinalStateUnknown with valid PodNetworkKind", func(t *testing.T) {
		pnk := &v1alpha1.PodNetworkKind{
			ObjectMeta: metav1.ObjectMeta{Name: "tombstone-network-kind"},
		}
		tombstone := cache.DeletedFinalStateUnknown{
			Key: "tombstone-network-kind",
			Obj: pnk,
		}
		controller.enqueuePodNetworkKind(tombstone)

		if controller.podNetworkKindQueue.Len() != 1 {
			t.Fatalf("expected queue length 1, got %d", controller.podNetworkKindQueue.Len())
		}
		key, _ := controller.podNetworkKindQueue.Get()
		if key != "tombstone-network-kind" {
			t.Errorf("expected key %q, got %q", "tombstone-network-kind", key)
		}
		controller.podNetworkKindQueue.Done(key)
	})

	t.Run("DeletedFinalStateUnknown with invalid Obj", func(t *testing.T) {
		tombstone := cache.DeletedFinalStateUnknown{
			Key: "bad-tombstone",
			Obj: "not-a-pod-network-kind",
		}
		controller.enqueuePodNetworkKind(tombstone)
		if controller.podNetworkKindQueue.Len() != 0 {
			t.Fatalf("expected queue length 0, got %d", controller.podNetworkKindQueue.Len())
		}
	})
}

func TestOnCustomResourceDefinitionEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	_, _, _, controller, _ := newController(ctx, t, nil, nil)

	t.Run("both nil", func(t *testing.T) {
		controller.onCustomResourceDefinitionEvent(nil, nil)
		if controller.podNetworkKindQueue.Len() != 0 {
			t.Fatalf("expected queue length 0, got %d", controller.podNetworkKindQueue.Len())
		}
	})

	t.Run("create event (nil old, non-nil new)", func(t *testing.T) {
		crd := &apiextensionsv1.CustomResourceDefinition{
			Spec: apiextensionsv1.CustomResourceDefinitionSpec{
				Group: "example.com",
				Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: "MyNetwork"},
			},
		}
		controller.onCustomResourceDefinitionEvent(nil, crd)
		if controller.podNetworkKindQueue.Len() != 1 {
			t.Fatalf("expected queue length 1, got %d", controller.podNetworkKindQueue.Len())
		}
		key, _ := controller.podNetworkKindQueue.Get()
		expected := v1alpha1.GetPodNetworkKindName("example.com", "MyNetwork")
		if key != expected {
			t.Errorf("expected key %q, got %q", expected, key)
		}
		controller.podNetworkKindQueue.Done(key)
	})

	t.Run("delete event (non-nil old, nil new)", func(t *testing.T) {
		crd := &apiextensionsv1.CustomResourceDefinition{
			Spec: apiextensionsv1.CustomResourceDefinitionSpec{
				Group: "example.com",
				Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: "MyNetwork"},
			},
		}
		controller.onCustomResourceDefinitionEvent(crd, nil)
		if controller.podNetworkKindQueue.Len() != 1 {
			t.Fatalf("expected queue length 1, got %d", controller.podNetworkKindQueue.Len())
		}
		key, _ := controller.podNetworkKindQueue.Get()
		expected := v1alpha1.GetPodNetworkKindName("example.com", "MyNetwork")
		if key != expected {
			t.Errorf("expected key %q, got %q", expected, key)
		}
		controller.podNetworkKindQueue.Done(key)
	})

	t.Run("update event (non-nil old and new)", func(t *testing.T) {
		oldCRD := &apiextensionsv1.CustomResourceDefinition{
			Spec: apiextensionsv1.CustomResourceDefinitionSpec{
				Group: "old.example.com",
				Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: "OldKind"},
			},
		}
		newCRD := &apiextensionsv1.CustomResourceDefinition{
			Spec: apiextensionsv1.CustomResourceDefinitionSpec{
				Group: "new.example.com",
				Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: "NewKind"},
			},
		}
		controller.onCustomResourceDefinitionEvent(oldCRD, newCRD)
		if controller.podNetworkKindQueue.Len() != 2 {
			t.Fatalf("expected queue length 2, got %d", controller.podNetworkKindQueue.Len())
		}
		key1, _ := controller.podNetworkKindQueue.Get()
		controller.podNetworkKindQueue.Done(key1)
		key2, _ := controller.podNetworkKindQueue.Get()
		controller.podNetworkKindQueue.Done(key2)

		expectedOld := v1alpha1.GetPodNetworkKindName("old.example.com", "OldKind")
		expectedNew := v1alpha1.GetPodNetworkKindName("new.example.com", "NewKind")
		keys := []string{key1, key2}
		if !((keys[0] == expectedOld && keys[1] == expectedNew) || (keys[0] == expectedNew && keys[1] == expectedOld)) {
			t.Errorf("expected keys [%s, %s], got %v", expectedOld, expectedNew, keys)
		}
	})

	t.Run("invalid object type", func(t *testing.T) {
		controller.onCustomResourceDefinitionEvent("invalid-old", nil)
		if controller.podNetworkKindQueue.Len() != 0 {
			t.Fatalf("expected queue length 0, got %d", controller.podNetworkKindQueue.Len())
		}

		controller.onCustomResourceDefinitionEvent(nil, "invalid-new")
		if controller.podNetworkKindQueue.Len() != 0 {
			t.Fatalf("expected queue length 0, got %d", controller.podNetworkKindQueue.Len())
		}
	})

	t.Run("DeletedFinalStateUnknown with valid CRD", func(t *testing.T) {
		crd := &apiextensionsv1.CustomResourceDefinition{
			Spec: apiextensionsv1.CustomResourceDefinitionSpec{
				Group: "example.com",
				Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: "TombstoneNet"},
			},
		}
		tombstone := cache.DeletedFinalStateUnknown{
			Key: "example-com-tombstonenet",
			Obj: crd,
		}
		controller.onCustomResourceDefinitionEvent(tombstone, nil)
		if controller.podNetworkKindQueue.Len() != 1 {
			t.Fatalf("expected queue length 1, got %d", controller.podNetworkKindQueue.Len())
		}
		key, _ := controller.podNetworkKindQueue.Get()
		expected := v1alpha1.GetPodNetworkKindName("example.com", "TombstoneNet")
		if key != expected {
			t.Errorf("expected key %q, got %q", expected, key)
		}
		controller.podNetworkKindQueue.Done(key)
	})

	t.Run("DeletedFinalStateUnknown with invalid Obj", func(t *testing.T) {
		tombstone := cache.DeletedFinalStateUnknown{
			Key: "bad-crd-tombstone",
			Obj: "not-a-crd",
		}
		controller.onCustomResourceDefinitionEvent(tombstone, nil)
		if controller.podNetworkKindQueue.Len() != 0 {
			t.Fatalf("expected queue length 0, got %d", controller.podNetworkKindQueue.Len())
		}
	})
}

func TestRun(t *testing.T) {
	t.Run("cache sync failure", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		_, _, _, controller, _ := newController(ctx, t, nil, nil)
		controller.podNetworkKindSynced = func() bool { return false }

		canceledCtx, cancelImmediate := context.WithCancel(context.Background())
		cancelImmediate()

		err := controller.Run(canceledCtx, 1)
		if err == nil {
			t.Fatal("expected error waiting for caches to sync, got nil")
		}
		expectedMsg := "failed to wait for caches to sync"
		if err.Error() != expectedMsg {
			t.Errorf("expected error %q, got %q", expectedMsg, err.Error())
		}
	})

	t.Run("worker execution and shutdown", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())

		_, _, _, controller, fr := newController(ctx, t, nil, nil)

		runErrCh := make(chan error, 1)
		go func() {
			runErrCh <- controller.Run(ctx, 2)
		}()

		controller.podNetworkKindQueue.Add("test-item")

		if err := wait.PollUntilContextTimeout(ctx, 1*time.Millisecond, 2*time.Second, true, func(ctx context.Context) (bool, error) {
			names := fr.getPodNetworkKindNames()
			return len(names) == 1 && names[0] == "test-item", nil
		}); err != nil {
			t.Fatalf("timed out waiting for worker to process item, got: %v", fr.getPodNetworkKindNames())
		}

		cancel()

		select {
		case err := <-runErrCh:
			if err != nil {
				t.Fatalf("unexpected error from Run: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for Run to exit after context cancellation")
		}

		if !controller.podNetworkKindQueue.ShuttingDown() {
			t.Error("expected queue to be shut down")
		}
	})
}

func TestProcessNextWorkItem(t *testing.T) {
	t.Run("queue shutdown returns false", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		_, _, _, controller, _ := newController(ctx, t, nil, nil)
		controller.podNetworkKindQueue.ShutDown()

		result := controller.processNextWorkItem(ctx)
		if result != false {
			t.Errorf("expected false on shutdown, got %v", result)
		}
	})

	t.Run("reconcile succeeds", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		_, _, _, controller, fr := newController(ctx, t, nil, nil)
		fr.setReconcileErr(nil)

		controller.podNetworkKindQueue.Add("success-pnk")

		result := controller.processNextWorkItem(ctx)
		if result != true {
			t.Errorf("expected true, got %v", result)
		}

		names := fr.getPodNetworkKindNames()
		if len(names) != 1 || names[0] != "success-pnk" {
			t.Errorf("expected reconciler to be called with %q, got: %v", "success-pnk", names)
		}

		if controller.podNetworkKindQueue.NumRequeues("success-pnk") != 0 {
			t.Errorf("expected NumRequeues to be 0 after Forget, got %d", controller.podNetworkKindQueue.NumRequeues("success-pnk"))
		}
		if controller.podNetworkKindQueue.Len() != 0 {
			t.Errorf("expected queue to be empty, got length %d", controller.podNetworkKindQueue.Len())
		}
	})

	t.Run("reconcile returns error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		_, _, _, controller, fr := newController(ctx, t, nil, nil)
		expectedErr := fmt.Errorf("reconciliation error")
		fr.setReconcileErr(expectedErr)

		controller.podNetworkKindQueue.Add("error-pnk")

		result := controller.processNextWorkItem(ctx)
		if result != true {
			t.Errorf("expected true, got %v", result)
		}

		names := fr.getPodNetworkKindNames()
		if len(names) != 1 || names[0] != "error-pnk" {
			t.Errorf("expected reconciler to be called with %q, got: %v", "error-pnk", names)
		}

		if controller.podNetworkKindQueue.NumRequeues("error-pnk") != 1 {
			t.Errorf("expected NumRequeues to be 1 after AddRateLimited, got %d", controller.podNetworkKindQueue.NumRequeues("error-pnk"))
		}
	})
}

func TestEnqueueAllPodNetworkKinds(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	initialPNK1 := &v1alpha1.PodNetworkKind{ObjectMeta: metav1.ObjectMeta{Name: "pnk-1"}}
	initialPNK2 := &v1alpha1.PodNetworkKind{ObjectMeta: metav1.ObjectMeta{Name: "pnk-2"}}

	_, _, _, controller, _ := newController(ctx, t, []runtime.Object{initialPNK1, initialPNK2}, nil)

	// Clear items from queue
	for controller.podNetworkKindQueue.Len() > 0 {
		item, _ := controller.podNetworkKindQueue.Get()
		controller.podNetworkKindQueue.Done(item)
	}

	controller.enqueueAllPodNetworkKinds()

	if controller.podNetworkKindQueue.Len() != 2 {
		t.Fatalf("expected queue length 2, got %d", controller.podNetworkKindQueue.Len())
	}
}

func TestEnqueueAllDefaultCandidates(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	initialPNK1 := &v1alpha1.PodNetworkKind{
		ObjectMeta: metav1.ObjectMeta{Name: "pnk-default-1"},
		Spec: v1alpha1.PodNetworkKindSpec{
			DefaultPodNetworkKind: ptr.To(true),
		},
	}
	initialPNK2 := &v1alpha1.PodNetworkKind{
		ObjectMeta: metav1.ObjectMeta{Name: "pnk-non-default"},
		Spec: v1alpha1.PodNetworkKindSpec{
			DefaultPodNetworkKind: ptr.To(false),
		},
	}
	initialPNK3 := &v1alpha1.PodNetworkKind{
		ObjectMeta: metav1.ObjectMeta{Name: "pnk-default-2"},
		Spec: v1alpha1.PodNetworkKindSpec{
			DefaultPodNetworkKind: ptr.To(true),
		},
	}

	_, _, _, controller, _ := newController(ctx, t, []runtime.Object{initialPNK1, initialPNK2, initialPNK3}, nil)

	// Clear items from queue
	for controller.podNetworkKindQueue.Len() > 0 {
		item, _ := controller.podNetworkKindQueue.Get()
		controller.podNetworkKindQueue.Done(item)
	}

	controller.enqueueAllDefaultCandidates()

	if controller.podNetworkKindQueue.Len() != 2 {
		t.Fatalf("expected queue length 2, got %d", controller.podNetworkKindQueue.Len())
	}
}
