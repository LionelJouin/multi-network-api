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
	"cmp"
	"context"
	"reflect"
	"slices"
	"testing"
	"time"

	multinetworkv1alpha1 "github.com/kubernetes-sigs/multi-network-api/apis/v1alpha1"
	v1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
)

func newController(
	ctx context.Context,
	t *testing.T,
	initialObjects []runtime.Object,
) (*fake.Clientset, *PodNetworkDeviceCache) {
	fakeKubeClient := fake.NewSimpleClientset(initialObjects...)

	networkKindInformerFactory := informers.NewSharedInformerFactory(fakeKubeClient, 0)

	podNetworkDeviceCache, err := NewPodNetworkDeviceCache(
		networkKindInformerFactory.Core().V1().Pods(),
		networkKindInformerFactory.Resource().V1().ResourceClaims(),
		networkKindInformerFactory.Resource().V1().ResourceSlices(),
	)
	if err != nil {
		t.Fatal("NewPodNetworkDeviceCache failed:", err)
	}

	networkKindInformerFactory.Start(ctx.Done())
	networkKindInformerFactory.WaitForCacheSync(ctx.Done())

	return fakeKubeClient, podNetworkDeviceCache
}

func waitForCachePopulated(ctx context.Context, t *testing.T, cache *PodNetworkDeviceCache, expectedCount int) {
	t.Helper()
	if err := wait.PollUntilContextTimeout(ctx, 1*time.Millisecond, 2*time.Second, true, func(ctx context.Context) (bool, error) {
		return len(cache.List(ctx)) == expectedCount, nil
	}); err != nil {
		t.Fatalf("timed out waiting for cache to populate, got %d items, want %d", len(cache.List(ctx)), expectedCount)
	}
}

func TestEventHandlers(t *testing.T) {
	type object interface {
		runtime.Object
		metav1.Object
	}

	networkKind := "my-network-kind"
	podNetworkName := "my-pod-network"
	podNetworkNamespace := "default"

	sliceDevice := resourcev1.Device{
		Name: "dev-0",
		Attributes: map[resourcev1.QualifiedName]resourcev1.DeviceAttribute{
			multinetworkv1alpha1.StandardDeviceAttributeNetworkKind:         {StringValue: &networkKind},
			multinetworkv1alpha1.StandardDeviceAttributePodNetwork:          {StringValue: &podNetworkName},
			multinetworkv1alpha1.StandardDeviceAttributePodNetworkNamespace: {StringValue: &podNetworkNamespace},
		},
	}

	sliceDeviceNoNS := resourcev1.Device{
		Name: "dev-0",
		Attributes: map[resourcev1.QualifiedName]resourcev1.DeviceAttribute{
			multinetworkv1alpha1.StandardDeviceAttributeNetworkKind: {StringValue: &networkKind},
			multinetworkv1alpha1.StandardDeviceAttributePodNetwork:  {StringValue: &podNetworkName},
		},
	}

	sliceDeviceNoKind := resourcev1.Device{
		Name: "dev-0",
		Attributes: map[resourcev1.QualifiedName]resourcev1.DeviceAttribute{
			multinetworkv1alpha1.StandardDeviceAttributePodNetwork:          {StringValue: &podNetworkName},
			multinetworkv1alpha1.StandardDeviceAttributePodNetworkNamespace: {StringValue: &podNetworkNamespace},
		},
	}

	sliceDeviceNoPodNetwork := resourcev1.Device{
		Name: "dev-0",
		Attributes: map[resourcev1.QualifiedName]resourcev1.DeviceAttribute{
			multinetworkv1alpha1.StandardDeviceAttributeNetworkKind:         {StringValue: &networkKind},
			multinetworkv1alpha1.StandardDeviceAttributePodNetworkNamespace: {StringValue: &podNetworkNamespace},
		},
	}

	sliceDevice2 := resourcev1.Device{
		Name: "dev-1",
		Attributes: map[resourcev1.QualifiedName]resourcev1.DeviceAttribute{
			multinetworkv1alpha1.StandardDeviceAttributeNetworkKind:         {StringValue: &networkKind},
			multinetworkv1alpha1.StandardDeviceAttributePodNetwork:          {StringValue: &podNetworkName},
			multinetworkv1alpha1.StandardDeviceAttributePodNetworkNamespace: {StringValue: &podNetworkNamespace},
		},
	}

	allocatedDeviceStatus := resourcev1.AllocatedDeviceStatus{
		Driver: "test-driver", Pool: "test-pool", Device: "dev-0", NetworkData: &resourcev1.NetworkDeviceData{},
	}

	allocatedDeviceStatusNoND := resourcev1.AllocatedDeviceStatus{
		Driver: "test-driver", Pool: "test-pool", Device: "dev-0",
	}

	allocatedDeviceStatus2 := resourcev1.AllocatedDeviceStatus{
		Driver: "test-driver", Pool: "test-pool", Device: "dev-1", NetworkData: &resourcev1.NetworkDeviceData{},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default", UID: "test-pod-uid"},
		Status: v1.PodStatus{
			ResourceClaimStatuses: []v1.PodResourceClaimStatus{
				{Name: "test-claim", ResourceClaimName: ptr.To("test-claim")},
			},
		},
	}

	makeSlice := func(devices ...resourcev1.Device) *resourcev1.ResourceSlice {
		return &resourcev1.ResourceSlice{
			ObjectMeta: metav1.ObjectMeta{Name: "test-slice"},
			Spec: resourcev1.ResourceSliceSpec{
				Driver:  "test-driver",
				Pool:    resourcev1.ResourcePool{Name: "test-pool"},
				Devices: devices,
			},
		}
	}

	makeClaim := func(devices ...resourcev1.AllocatedDeviceStatus) *resourcev1.ResourceClaim {
		return &resourcev1.ResourceClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "test-claim", Namespace: "default"},
			Status: resourcev1.ResourceClaimStatus{
				Devices: devices,
				ReservedFor: []resourcev1.ResourceClaimConsumerReference{
					{Resource: "pods", Name: "test-pod", UID: "test-pod-uid"},
				},
			},
		}
	}

	makeExpectedPND := func(devices map[string]*Device) *PodNetworkDevice {
		return &PodNetworkDevice{
			TypeMeta:   metav1.TypeMeta{Kind: "PodNetworkDevices", APIVersion: "multinetwork.networking.k8s.io/v1alpha1"},
			ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default", UID: "test-pod-uid"},
			Pod:        pod,
			Devices:    devices,
		}
	}

	tests := []struct {
		name                      string
		initialObjects            []runtime.Object
		createObjects             []object
		updateObjects             []object
		deleteObjects             []object
		expectedPodNetworkDevices []*PodNetworkDevice
	}{
		{
			name:           "pod with one allocated network device",
			initialObjects: []runtime.Object{makeSlice(sliceDevice), makeClaim(allocatedDeviceStatus), pod},
			expectedPodNetworkDevices: []*PodNetworkDevice{
				makeExpectedPND(map[string]*Device{
					"test-driver/test-pool/dev-0": {
						AllocatedDeviceStatus: &allocatedDeviceStatus,
						Device:                &sliceDevice,
						PodNetworkRef:         &PodNetworkRef{Kind: networkKind, Name: podNetworkName, Namespace: &podNetworkNamespace},
					},
				}),
			},
		},
		{
			name: "pod with no resource claim statuses",
			initialObjects: []runtime.Object{
				&v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test-pod-no-claims", Namespace: "default"}},
			},
		},
		{
			name:           "device without NetworkData is skipped",
			initialObjects: []runtime.Object{makeSlice(sliceDevice), makeClaim(allocatedDeviceStatusNoND), pod},
		},
		{
			name:           "device without matching resource slice is skipped",
			initialObjects: []runtime.Object{makeClaim(allocatedDeviceStatus), pod},
		},
		{
			name:           "device missing networkKind attribute is skipped",
			initialObjects: []runtime.Object{makeSlice(sliceDeviceNoKind), makeClaim(allocatedDeviceStatus), pod},
		},
		{
			name:           "device missing podNetwork attribute is skipped",
			initialObjects: []runtime.Object{makeSlice(sliceDeviceNoPodNetwork), makeClaim(allocatedDeviceStatus), pod},
		},
		{
			name:           "pod network namespace is optional",
			initialObjects: []runtime.Object{makeSlice(sliceDeviceNoNS), makeClaim(allocatedDeviceStatus), pod},
			expectedPodNetworkDevices: []*PodNetworkDevice{
				makeExpectedPND(map[string]*Device{
					"test-driver/test-pool/dev-0": {
						AllocatedDeviceStatus: &allocatedDeviceStatus,
						Device:                &sliceDeviceNoNS,
						PodNetworkRef:         &PodNetworkRef{Kind: networkKind, Name: podNetworkName},
					},
				}),
			},
		},
		{
			name:           "multiple devices on one pod",
			initialObjects: []runtime.Object{makeSlice(sliceDevice, sliceDevice2), makeClaim(allocatedDeviceStatus, allocatedDeviceStatus2), pod},
			expectedPodNetworkDevices: []*PodNetworkDevice{
				makeExpectedPND(map[string]*Device{
					"test-driver/test-pool/dev-0": {
						AllocatedDeviceStatus: &allocatedDeviceStatus,
						Device:                &sliceDevice,
						PodNetworkRef:         &PodNetworkRef{Kind: networkKind, Name: podNetworkName, Namespace: &podNetworkNamespace},
					},
					"test-driver/test-pool/dev-1": {
						AllocatedDeviceStatus: &allocatedDeviceStatus2,
						Device:                &sliceDevice2,
						PodNetworkRef:         &PodNetworkRef{Kind: networkKind, Name: podNetworkName, Namespace: &podNetworkNamespace},
					},
				}),
			},
		},
		{
			name:           "pod deletion removes pod network device",
			initialObjects: []runtime.Object{makeSlice(sliceDevice), makeClaim(allocatedDeviceStatus), pod},
			deleteObjects:  []object{pod},
		},
		{
			name:           "resource claim creation triggers sync",
			initialObjects: []runtime.Object{makeSlice(sliceDevice), pod},
			createObjects:  []object{makeClaim(allocatedDeviceStatus)},
			expectedPodNetworkDevices: []*PodNetworkDevice{
				makeExpectedPND(map[string]*Device{
					"test-driver/test-pool/dev-0": {
						AllocatedDeviceStatus: &allocatedDeviceStatus,
						Device:                &sliceDevice,
						PodNetworkRef:         &PodNetworkRef{Kind: networkKind, Name: podNetworkName, Namespace: &podNetworkNamespace},
					},
				}),
			},
		},
		{
			name:           "resource slice creation triggers sync",
			initialObjects: []runtime.Object{makeClaim(allocatedDeviceStatus), pod},
			createObjects:  []object{makeSlice(sliceDevice)},
			expectedPodNetworkDevices: []*PodNetworkDevice{
				makeExpectedPND(map[string]*Device{
					"test-driver/test-pool/dev-0": {
						AllocatedDeviceStatus: &allocatedDeviceStatus,
						Device:                &sliceDevice,
						PodNetworkRef:         &PodNetworkRef{Kind: networkKind, Name: podNetworkName, Namespace: &podNetworkNamespace},
					},
				}),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			fakeKubeClient, networkDeviceCache := newController(ctx, t, tt.initialObjects)

			go func() {
				_ = networkDeviceCache.Run(ctx, 1)
			}()

			waitForCachePopulated(ctx, t, networkDeviceCache, len(tt.deleteObjects))

			action := func(obj object, actionType string) {
				var gvr *schema.GroupVersionResource
				objectTracker := fakeKubeClient.Tracker()

				switch obj.(type) {
				case *v1.Pod:
					r := v1.SchemeGroupVersion.WithResource("pods")
					gvr = &r
				case *resourcev1.ResourceClaim:
					r := resourcev1.SchemeGroupVersion.WithResource("resourceclaims")
					gvr = &r
				case *resourcev1.ResourceSlice:
					r := resourcev1.SchemeGroupVersion.WithResource("resourceslices")
					gvr = &r
				}

				if gvr == nil {
					t.Fatal("gvr is nil")
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

			waitForCachePopulated(ctx, t, networkDeviceCache, len(tt.expectedPodNetworkDevices))

			actual := networkDeviceCache.List(ctx)
			expected := tt.expectedPodNetworkDevices
			if expected == nil {
				expected = []*PodNetworkDevice{}
			}
			if !reflect.DeepEqual(actual, expected) {
				t.Errorf("unexpected PodNetworkDevices\n got: %+v\nwant: %+v", actual, expected)
			}
		})
	}
}

func TestPodNetworkDeviceCache_List(t *testing.T) {
	networkKind := "my-network-kind"
	podNetworkName := "my-pod-network"
	podNetworkNamespace := "default"

	otherNetworkKind := "other-network-kind"
	otherPodNetworkName := "other-pod-network"

	sliceDevice := resourcev1.Device{
		Name: "dev-0",
		Attributes: map[resourcev1.QualifiedName]resourcev1.DeviceAttribute{
			multinetworkv1alpha1.StandardDeviceAttributeNetworkKind:         {StringValue: &networkKind},
			multinetworkv1alpha1.StandardDeviceAttributePodNetwork:          {StringValue: &podNetworkName},
			multinetworkv1alpha1.StandardDeviceAttributePodNetworkNamespace: {StringValue: &podNetworkNamespace},
		},
	}

	sliceDeviceOtherRef := resourcev1.Device{
		Name: "dev-1",
		Attributes: map[resourcev1.QualifiedName]resourcev1.DeviceAttribute{
			multinetworkv1alpha1.StandardDeviceAttributeNetworkKind:         {StringValue: &otherNetworkKind},
			multinetworkv1alpha1.StandardDeviceAttributePodNetwork:          {StringValue: &otherPodNetworkName},
			multinetworkv1alpha1.StandardDeviceAttributePodNetworkNamespace: {StringValue: &podNetworkNamespace},
		},
	}

	allocatedDeviceStatus := resourcev1.AllocatedDeviceStatus{
		Driver: "test-driver", Pool: "test-pool", Device: "dev-0", NetworkData: &resourcev1.NetworkDeviceData{},
	}

	allocatedDeviceStatusOtherRef := resourcev1.AllocatedDeviceStatus{
		Driver: "test-driver", Pool: "test-pool", Device: "dev-1", NetworkData: &resourcev1.NetworkDeviceData{},
	}

	makePod := func(name string, podLabels map[string]string) *v1.Pod {
		return &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: name, Namespace: "default", UID: types.UID("uid-" + name),
				Labels: podLabels,
			},
			Status: v1.PodStatus{
				ResourceClaimStatuses: []v1.PodResourceClaimStatus{
					{Name: name + "-claim", ResourceClaimName: ptr.To(name + "-claim")},
				},
			},
		}
	}

	makeSlice := func(name string, devices ...resourcev1.Device) *resourcev1.ResourceSlice {
		return &resourcev1.ResourceSlice{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: resourcev1.ResourceSliceSpec{
				Driver:  "test-driver",
				Pool:    resourcev1.ResourcePool{Name: "test-pool"},
				Devices: devices,
			},
		}
	}

	makeClaim := func(name, podName string, devices ...resourcev1.AllocatedDeviceStatus) *resourcev1.ResourceClaim {
		return &resourcev1.ResourceClaim{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Status: resourcev1.ResourceClaimStatus{
				Devices: devices,
				ReservedFor: []resourcev1.ResourceClaimConsumerReference{
					{Resource: "pods", Name: podName, UID: types.UID("uid-" + podName)},
				},
			},
		}
	}

	podA := makePod("pod-a", map[string]string{"app": "web", "env": "prod"})
	podB := makePod("pod-b", map[string]string{"app": "api", "env": "prod"})
	podC := makePod("pod-c", map[string]string{"app": "web", "env": "staging"})

	makeExpectedPND := func(pod *v1.Pod, devices map[string]*Device) *PodNetworkDevice {
		return &PodNetworkDevice{
			TypeMeta:   metav1.TypeMeta{Kind: "PodNetworkDevices", APIVersion: "multinetwork.networking.k8s.io/v1alpha1"},
			ObjectMeta: metav1.ObjectMeta{Name: pod.Name, Namespace: pod.Namespace, UID: pod.UID, Labels: pod.Labels},
			Pod:        pod,
			Devices:    devices,
		}
	}

	pndA := makeExpectedPND(podA, map[string]*Device{
		"test-driver/test-pool/dev-0": {
			AllocatedDeviceStatus: &allocatedDeviceStatus,
			Device:                &sliceDevice,
			PodNetworkRef:         &PodNetworkRef{Kind: networkKind, Name: podNetworkName, Namespace: &podNetworkNamespace},
		},
	})
	pndB := makeExpectedPND(podB, map[string]*Device{
		"test-driver/test-pool/dev-0": {
			AllocatedDeviceStatus: &allocatedDeviceStatus,
			Device:                &sliceDevice,
			PodNetworkRef:         &PodNetworkRef{Kind: networkKind, Name: podNetworkName, Namespace: &podNetworkNamespace},
		},
	})
	pndC := makeExpectedPND(podC, map[string]*Device{
		"test-driver/test-pool/dev-0": {
			AllocatedDeviceStatus: &allocatedDeviceStatus,
			Device:                &sliceDevice,
			PodNetworkRef:         &PodNetworkRef{Kind: networkKind, Name: podNetworkName, Namespace: &podNetworkNamespace},
		},
	})
	sortPNDs := func(pnds []*PodNetworkDevice) {
		slices.SortFunc(pnds, func(a, b *PodNetworkDevice) int {
			return cmp.Compare(a.Name, b.Name)
		})
	}

	tests := []struct {
		name           string
		initialObjects []runtime.Object
		opts           []Option
		want           []*PodNetworkDevice
	}{
		{
			name: "no options returns all devices",
			initialObjects: []runtime.Object{
				makeSlice("slice-a", sliceDevice),
				makeSlice("slice-b", sliceDevice),
				makeClaim("pod-a-claim", "pod-a", allocatedDeviceStatus),
				makeClaim("pod-b-claim", "pod-b", allocatedDeviceStatus),
				podA, podB,
			},
			want: []*PodNetworkDevice{pndA, pndB},
		},
		{
			name: "empty cache returns empty list",
		},
		{
			name: "WithLabels filters by matching labels",
			initialObjects: []runtime.Object{
				makeSlice("slice-a", sliceDevice),
				makeSlice("slice-b", sliceDevice),
				makeSlice("slice-c", sliceDevice),
				makeClaim("pod-a-claim", "pod-a", allocatedDeviceStatus),
				makeClaim("pod-b-claim", "pod-b", allocatedDeviceStatus),
				makeClaim("pod-c-claim", "pod-c", allocatedDeviceStatus),
				podA, podB, podC,
			},
			opts: []Option{WithLabels(map[string]string{"app": "web"})},
			want: []*PodNetworkDevice{pndA, pndC},
		},
		{
			name: "WithLabels with multiple labels narrows results",
			initialObjects: []runtime.Object{
				makeSlice("slice-a", sliceDevice),
				makeSlice("slice-b", sliceDevice),
				makeSlice("slice-c", sliceDevice),
				makeClaim("pod-a-claim", "pod-a", allocatedDeviceStatus),
				makeClaim("pod-b-claim", "pod-b", allocatedDeviceStatus),
				makeClaim("pod-c-claim", "pod-c", allocatedDeviceStatus),
				podA, podB, podC,
			},
			opts: []Option{WithLabels(map[string]string{"app": "web", "env": "prod"})},
			want: []*PodNetworkDevice{pndA},
		},
		{
			name: "WithLabels with no matching labels returns empty",
			initialObjects: []runtime.Object{
				makeSlice("slice-a", sliceDevice),
				makeClaim("pod-a-claim", "pod-a", allocatedDeviceStatus),
				podA,
			},
			opts: []Option{WithLabels(map[string]string{"app": "nonexistent"})},
		},
		{
			name: "WithPodNetworkRef filters by matching ref",
			initialObjects: []runtime.Object{
				makeSlice("slice-ab", sliceDevice, sliceDeviceOtherRef),
				makeClaim("pod-a-claim", "pod-a", allocatedDeviceStatus),
				makeClaim("pod-b-claim", "pod-b", allocatedDeviceStatusOtherRef),
				podA, podB,
			},
			opts: []Option{WithPodNetworkRef(&PodNetworkRef{
				Kind: networkKind, Name: podNetworkName, Namespace: &podNetworkNamespace,
			})},
			want: []*PodNetworkDevice{pndA},
		},
		{
			name: "WithLabels and WithPodNetworkRef combined",
			initialObjects: []runtime.Object{
				makeSlice("slice-abc", sliceDevice, sliceDeviceOtherRef),
				makeClaim("pod-a-claim", "pod-a", allocatedDeviceStatus),
				makeClaim("pod-b-claim", "pod-b", allocatedDeviceStatus),
				makeClaim("pod-c-claim", "pod-c", allocatedDeviceStatusOtherRef),
				podA, podB, podC,
			},
			opts: []Option{
				WithLabels(map[string]string{"app": "web"}),
				WithPodNetworkRef(&PodNetworkRef{
					Kind: networkKind, Name: podNetworkName, Namespace: &podNetworkNamespace,
				}),
			},
			want: []*PodNetworkDevice{pndA},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			_, networkDeviceCache := newController(ctx, t, tt.initialObjects)

			go func() {
				_ = networkDeviceCache.Run(ctx, 1)
			}()

			expectedTotal := 0
			for _, obj := range tt.initialObjects {
				if pod, ok := obj.(*v1.Pod); ok && len(pod.Status.ResourceClaimStatuses) > 0 {
					expectedTotal++
				}
			}
			waitForCachePopulated(ctx, t, networkDeviceCache, expectedTotal)

			got := networkDeviceCache.List(ctx, tt.opts...)
			expected := tt.want
			if expected == nil {
				expected = []*PodNetworkDevice{}
			}
			sortPNDs(got)
			sortPNDs(expected)
			if !reflect.DeepEqual(got, expected) {
				t.Errorf("List() unexpected result\n got: %+v\nwant: %+v", got, expected)
			}
		})
	}
}
