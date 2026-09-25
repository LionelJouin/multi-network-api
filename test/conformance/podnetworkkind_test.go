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

package conformance

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubernetes-sigs/multi-network-api/apis/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("PodNetworkKind Conformance", func() {
	It("should be installed with the implementation and have ImplementationTypeReady condition set to True with reason Compliant", func(ctx context.Context) {
		podNetworkKindName := v1alpha1.GetPodNetworkKindName(implementationGroup, implementationKind)

		Eventually(func(g Gomega) {
			pnk, err := multinetworkClient.PodNetworkKinds().Get(ctx, podNetworkKindName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred(), "PodNetworkKind %q should be installed with the implementation", podNetworkKindName)
			g.Expect(pnk.Status.Conditions).NotTo(BeEmpty(), "no conditions set on PodNetworkKind %q", podNetworkKindName)

			cond := findCondition(pnk.Status.Conditions, v1alpha1.PodNetworkKindConditionImplementationTypeReady)
			g.Expect(cond).NotTo(BeNil(), "ImplementationTypeReady condition not found on PodNetworkKind %q", podNetworkKindName)
			g.Expect(cond.Status).To(Equal(metav1.ConditionTrue),
				"expected ImplementationTypeReady=True, got reason=%s message=%q", cond.Reason, cond.Message)
			g.Expect(cond.Reason).To(Equal(v1alpha1.PodNetworkKindReasonCompliant))
		}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())
	})
})

func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}
