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
	"context"
	"fmt"

	"github.com/kubernetes-sigs/multi-network-api/pkg/podnetworkdevice/v1alpha1"
	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	clientset "k8s.io/client-go/kubernetes"
	v1listers "k8s.io/client-go/listers/core/v1"
	discoverylisters "k8s.io/client-go/listers/discovery/v1"
	"k8s.io/utils/ptr"
)

type EndpointSliceReconciler struct {
	podNetworkDeviceCache *v1alpha1.PodNetworkDeviceCache

	serviceLister       v1listers.ServiceLister
	endpointsliceLister discoverylisters.EndpointSliceLister

	clientset clientset.Interface
}

func NewEndpointSliceReconciler(
	serviceLister v1listers.ServiceLister,
	endpointsliceLister discoverylisters.EndpointSliceLister,
	podNetworkDeviceCache *v1alpha1.PodNetworkDeviceCache,
	clientset clientset.Interface,
) (*EndpointSliceReconciler, error) {
	esr := &EndpointSliceReconciler{
		serviceLister:         serviceLister,
		endpointsliceLister:   endpointsliceLister,
		podNetworkDeviceCache: podNetworkDeviceCache,
		clientset:             clientset,
	}

	return esr, nil
}

func (esr *EndpointSliceReconciler) Reconcile(ctx context.Context, serviceName string, serviceNamespace string) error {
	service, err := esr.serviceLister.Services(serviceNamespace).Get(serviceName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			err := esr.clientset.DiscoveryV1().EndpointSlices(serviceNamespace).Delete(ctx, serviceName, metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("failed to delete EndpointSlice for service %s/%s: %w", serviceNamespace, serviceName, err)
			}
			return nil
		}
		return fmt.Errorf("failed to get service %s/%s: %w", serviceNamespace, serviceName, err)
	}

	podNetworkRef := getPodNetworkRefForService(service)
	if podNetworkRef == nil {
		err := esr.clientset.DiscoveryV1().EndpointSlices(serviceNamespace).Delete(ctx, serviceName, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete EndpointSlice for service %s/%s: %w", serviceNamespace, serviceName, err)
		}
		return nil
	}

	oldEndpointSlice, err := esr.endpointsliceLister.EndpointSlices(serviceNamespace).Get(serviceName)
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get EndpointSlice for service %s/%s: %w", serviceNamespace, serviceName, err)
	}

	endpointsForService := esr.endpointsForService(ctx, service, podNetworkRef)
	if len(endpointsForService) == 0 {
		err := esr.clientset.DiscoveryV1().EndpointSlices(serviceNamespace).Delete(ctx, serviceName, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete EndpointSlice for service %s/%s: %w", serviceNamespace, serviceName, err)
		}
		return nil
	}

	endpointSlice := esr.buildEndpointSlice(ctx, service, endpointsForService)

	if oldEndpointSlice == nil { // create the endpointslice
		_, err := esr.clientset.DiscoveryV1().EndpointSlices(serviceNamespace).Create(ctx, endpointSlice, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create EndpointSlice for service %s/%s: %w", serviceNamespace, serviceName, err)
		}
	} else { // update the endpointslice
		_, err := esr.clientset.DiscoveryV1().EndpointSlices(serviceNamespace).Update(ctx, endpointSlice, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update EndpointSlice for service %s/%s: %w", serviceNamespace, serviceName, err)
		}
	}

	return nil
}

func (esr *EndpointSliceReconciler) buildEndpointSlice(
	ctx context.Context,
	service *v1.Service,
	endpoints []discoveryv1.Endpoint,
) *discoveryv1.EndpointSlice {
	endpintSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      service.Name,
			Namespace: service.Namespace,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: service.Name,
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4, // todo: support IPv6 and dual-stack
		Endpoints:   endpoints,
	}

	return endpintSlice
}

func (esr *EndpointSliceReconciler) endpointsForService(
	ctx context.Context,
	service *v1.Service,
	podNetworkRef *v1alpha1.PodNetworkRef,
) []discoveryv1.Endpoint {
	serviceSelector := labels.Set(service.Spec.Selector)
	delete(serviceSelector, DummyLabel)

	podNetworkDevices := esr.podNetworkDeviceCache.List(
		ctx,
		v1alpha1.WithLabels(serviceSelector),
		v1alpha1.WithPodNetworkRef(podNetworkRef),
	)

	endpoints := []discoveryv1.Endpoint{}

	for _, podNetworkDevice := range podNetworkDevices {
		for _, device := range podNetworkDevice.Devices {
			if !device.PodNetworkRef.IsEqual(podNetworkRef) {
				continue
			}

			endpoints = append(endpoints, discoveryv1.Endpoint{
				Addresses: deviceToAddresses(device),
				NodeName:  &podNetworkDevice.Pod.Spec.NodeName,
				TargetRef: &v1.ObjectReference{
					Kind:      "Pod",
					Name:      podNetworkDevice.Pod.Name,
					Namespace: podNetworkDevice.Pod.Namespace,
					UID:       podNetworkDevice.Pod.UID,
				},
				Conditions: discoveryv1.EndpointConditions{
					Ready: ptr.To(true),
				},
			})
		}
	}

	return endpoints
}
