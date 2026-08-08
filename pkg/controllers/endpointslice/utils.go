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

package endpointslice

import (
	"net"

	"github.com/kubernetes-sigs/multi-network-api/pkg/podnetworkdevice/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	v1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

// getServicesForPodNetworkDevices returns a set of services matching the given labels (via service selector).
func getServicesForPodNetworkDevices(serviceLister v1listers.ServiceLister, podNetworkDevice *v1alpha1.PodNetworkDevice) (sets.Set[string], error) {
	services, err := serviceLister.Services(podNetworkDevice.Namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}

	podNetworkDeviceLabel := labels.Set(podNetworkDevice.Pod.Labels)

	set := sets.Set[string]{}
	for _, service := range services {
		if service.Spec.Selector == nil {
			// If the service has a nil selector this means selectors match nothing, not everything.
			continue
		}

		if labels.ValidatedSetSelector(service.Spec.Selector).Matches(podNetworkDeviceLabel) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(service)
			if err != nil {
				return nil, err
			}
			set.Insert(key)
		}
	}
	return set, nil
}

// getPodNetworkRefForService returns the PodNetworkRef for the given service, if any.
func getPodNetworkRefForService(service *v1.Service) *v1alpha1.PodNetworkRef {
	if len(service.Labels) == 0 {
		return nil
	}

	podNetworkKind, ok := service.Labels[ServiceLabelPodNetworkKind]
	if !ok {
		return nil
	}

	podNetworkName, ok := service.Labels[ServiceLabelPodNetworkName]
	if !ok {
		return nil
	}

	var podNetworkNamespacePtr *string
	podNetworkNamespace, ok := service.Labels[ServiceLabelPodNetworkNamespace]
	if ok {
		podNetworkNamespacePtr = &podNetworkNamespace
	}

	return &v1alpha1.PodNetworkRef{
		Name:      podNetworkName,
		Namespace: podNetworkNamespacePtr,
		Kind:      podNetworkKind,
	}
}

func deviceToAddresses(device *v1alpha1.Device) []string {
	if device == nil || device.AllocatedDeviceStatus == nil || device.AllocatedDeviceStatus.NetworkData == nil {
		return nil
	}

	ips := []string{}

	for _, cidr := range device.AllocatedDeviceStatus.NetworkData.IPs {
		ip, _, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if ip.String() != "" {
			ips = append(ips, ip.String())
		}
	}

	return ips
}
