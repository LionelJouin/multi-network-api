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

package e2e

import (
	"flag"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	multinetworkclient "github.com/kubernetes-sigs/multi-network-api/pkg/client/clientset/versioned/typed/apis/v1alpha1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	timeout = 60 * time.Second
	polling = 1 * time.Second
)

var (
	implementationGroup string
	implementationKind  string
	podNetwork          string
	podNetworkNamespace string
	testNamespace       string

	kubeClient         kubernetes.Interface
	multinetworkClient *multinetworkclient.MultinetworkV1alpha1Client
)

func init() {
	flag.StringVar(&implementationGroup, "implementation-group", "", "API group of the implementation CRD (e.g. devicenetwork.io)")
	flag.StringVar(&implementationKind, "implementation-kind", "", "Kind of the implementation CRD (e.g. DeviceNetwork)")
	flag.StringVar(&podNetwork, "pod-network", "", "Name of the pre-installed pod network object")
	flag.StringVar(&podNetworkNamespace, "pod-network-namespace", "", "Namespace of the pod network object (optional for cluster-scoped)")
	flag.StringVar(&testNamespace, "namespace", "default", "Namespace for test resources")
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Multi-Network API E2E Suite")
}

var _ = BeforeSuite(func() {
	Expect(implementationGroup).NotTo(BeEmpty(), "-implementation-group flag is required")
	Expect(implementationKind).NotTo(BeEmpty(), "-implementation-kind flag is required")
	Expect(podNetwork).NotTo(BeEmpty(), "-pod-network flag is required")

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := kubeConfig.ClientConfig()
	Expect(err).NotTo(HaveOccurred(), "failed to load kubeconfig")

	kubeClient, err = kubernetes.NewForConfig(config)
	Expect(err).NotTo(HaveOccurred(), "failed to create kube client")

	multinetworkClient, err = multinetworkclient.NewForConfig(config)
	Expect(err).NotTo(HaveOccurred(), "failed to create multinetwork client")
})
