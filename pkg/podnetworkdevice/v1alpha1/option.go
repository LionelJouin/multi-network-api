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

import "k8s.io/apimachinery/pkg/labels"

type listOption struct {
	labelSelector labels.Selector
	podNetworkRef *PodNetworkRef
}

// Option defines optional parameters for listing PodNetworkDevice resources.
type Option func(*listOption)

// WithLabels returns an Option that filters the list of PodNetworkDevice resources
// by the given label set. Only PodNetworkDevices whose labels are a superset of
// the given set are returned.
func WithLabels(l labels.Set) Option {
	return func(lo *listOption) {
		lo.labelSelector = l.AsSelectorPreValidated()
	}
}

// WithPodNetworkRef returns an Option that filters the list of PodNetworkDevice resources
// by the given PodNetworkRef.
func WithPodNetworkRef(podNetworkRef *PodNetworkRef) Option {
	return func(lo *listOption) {
		lo.podNetworkRef = podNetworkRef
	}
}
