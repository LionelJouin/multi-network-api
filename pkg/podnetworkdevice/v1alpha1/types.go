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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	resourcev1 "k8s.io/api/resource/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PodNetworkDevice represents the joined view of a Pod with its
// network devices.
// +k8s:deepcopy-gen=true
type PodNetworkDevice struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Pod is the pod to which the devices are allocated.
	Pod *v1.Pod `json:"pod"`
	// Devices is a map of allocated devices for the pod, indexed by their shared device ID.
	Devices map[string]*Device `json:"devices"`
}

// Device holds the joined data for a single allocated network device.
// +k8s:deepcopy-gen=true
type Device struct {
	// AllocatedDeviceStatus is the status of the allocated device.
	AllocatedDeviceStatus *resourcev1.AllocatedDeviceStatus `json:"allocatedDeviceStatus"`
	// Device is the device object from the ResourceSlice that was
	// allocated to the pod.
	Device *resourcev1.Device `json:"device"`
	// PodNetworkRef is a reference to the pod network object to which the device is connected.
	PodNetworkRef *PodNetworkRef `json:"podNetworkRef,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PodNetworkDeviceList is a list of PodNetworkDevice resources.
// +k8s:deepcopy-gen=true
type PodNetworkDeviceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []PodNetworkDevice `json:"items"`
}

// PodNetworkRef identifies a specific pod network instance.
// +k8s:deepcopy-gen=true
type PodNetworkRef struct {
	// Kind identifies the NetworkKind responsible for the pod network.
	Kind string `json:"kind"`

	// Name identifies the pod network object.
	Name string `json:"name"`

	// Namespace identifies the namespace of the pod network object.
	// Optional if the pod network object is a non-namespace object.
	Namespace *string `json:"namespace,omitempty"`
}
