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
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubernetes-sigs/multi-network-api/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("NetworkKind", func() {
	It("should be ready when the implementation is installed", func(ctx context.Context) {
		networkKindName := v1alpha1.GetNetworkKindName(implementationGroup, implementationKind)

		Eventually(func(g Gomega) {
			nk, err := multinetworkClient.NetworkKinds().Get(ctx, networkKindName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred(), "NetworkKind %q not found", networkKindName)
			g.Expect(nk.Status.Conditions).NotTo(BeEmpty(), "no conditions set on NetworkKind")

			var found bool
			for _, condition := range nk.Status.Conditions {
				if condition.Type == v1alpha1.NetworkKindConditionImplementationTypeReady {
					found = true
					g.Expect(condition.Status).To(Equal(metav1.ConditionTrue),
						"expected ImplementationTypeReady=True, got reason=%s message=%q", condition.Reason, condition.Message)
					g.Expect(condition.Reason).To(Equal(v1alpha1.NetworkKindReasonCompliant))
				}
			}
			g.Expect(found).To(BeTrue(), "ImplementationTypeReady condition not found")
		}).WithContext(ctx).WithTimeout(timeout).WithPolling(polling).Should(Succeed())
	})

	It("should allocate network devices to a pod via ResourceClaim", func(ctx context.Context) {
		networkKindName := v1alpha1.GetNetworkKindName(implementationGroup, implementationKind)

		deviceClassName := "e2e-test-" + networkKindName
		_, err := kubeClient.ResourceV1().DeviceClasses().Create(ctx, &resourcev1.DeviceClass{
			ObjectMeta: metav1.ObjectMeta{Name: deviceClassName},
		}, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to create DeviceClass")
		DeferCleanup(func(ctx context.Context) {
			_ = kubeClient.ResourceV1().DeviceClasses().Delete(ctx, deviceClassName, metav1.DeleteOptions{})
		})

		domain := v1alpha1.StandardDeviceAttributePrefix
		celExpr := fmt.Sprintf(`device.attributes["%s"].networkKind == "%s" && device.attributes["%s"].podNetwork == "%s"`,
			domain, networkKindName, domain, podNetwork)
		if podNetworkNamespace != "" {
			celExpr += fmt.Sprintf(` && device.attributes["%s"].podNetworkNamespace == "%s"`, domain, podNetworkNamespace)
		}

		claim := &resourcev1.ResourceClaim{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "e2e-test-",
				Namespace:    testNamespace,
			},
			Spec: resourcev1.ResourceClaimSpec{
				Devices: resourcev1.DeviceClaim{
					Requests: []resourcev1.DeviceRequest{
						{
							Name: "network-device",
							Exactly: &resourcev1.ExactDeviceRequest{
								DeviceClassName: deviceClassName,
								Selectors: []resourcev1.DeviceSelector{
									{CEL: &resourcev1.CELDeviceSelector{Expression: celExpr}},
								},
							},
						},
					},
				},
			},
		}

		claim, err = kubeClient.ResourceV1().ResourceClaims(testNamespace).Create(ctx, claim, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to create ResourceClaim")
		DeferCleanup(func(ctx context.Context) {
			_ = kubeClient.ResourceV1().ResourceClaims(testNamespace).Delete(ctx, claim.Name, metav1.DeleteOptions{})
		})

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "e2e-test-",
				Namespace:    testNamespace,
			},
			Spec: corev1.PodSpec{
				ResourceClaims: []corev1.PodResourceClaim{
					{
						Name:              "network",
						ResourceClaimName: &claim.Name,
					},
				},
				Containers: []corev1.Container{
					{
						Name:  "pause",
						Image: "registry.k8s.io/pause:3.10",
						Resources: corev1.ResourceRequirements{
							Claims: []corev1.ResourceClaim{
								{Name: "network"},
							},
						},
					},
				},
			},
		}

		pod, err = kubeClient.CoreV1().Pods(testNamespace).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to create Pod")
		DeferCleanup(func(ctx context.Context) {
			_ = kubeClient.CoreV1().Pods(testNamespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
		})

		By("waiting for the pod to be running")
		Eventually(func(g Gomega) {
			p, err := kubeClient.CoreV1().Pods(testNamespace).Get(ctx, pod.Name, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p.Status.Phase).To(Equal(corev1.PodRunning), "pod phase is %s", p.Status.Phase)
		}).WithContext(ctx).WithTimeout(timeout).WithPolling(polling).Should(Succeed())

		By("verifying the ResourceClaim has allocated devices with NetworkData")
		Eventually(func(g Gomega) {
			c, err := kubeClient.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claim.Name, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(c.Status.Devices).NotTo(BeEmpty(), "no devices allocated on ResourceClaim")

			hasNetworkData := false
			for _, device := range c.Status.Devices {
				if device.NetworkData != nil {
					hasNetworkData = true
					break
				}
			}
			g.Expect(hasNetworkData).To(BeTrue(), "no allocated device has NetworkData")
		}).WithContext(ctx).WithTimeout(timeout).WithPolling(polling).Should(Succeed())
	})
})
