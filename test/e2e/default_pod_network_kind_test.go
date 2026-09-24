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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubernetes-sigs/multi-network-api/apis/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("DefaultPodNetworkKind Condition", func() {
	It("Creating a PodNetworkKind with spec.defaultPodNetworkKind: false or omitted results in no DefaultPodNetworkKind condition being added and status.defaultPodNetworkKind not set to true", func(ctx context.Context) {
		pnkOmittedName := "e2e-default-omitted"
		pnkOmitted := &v1alpha1.PodNetworkKind{
			ObjectMeta: metav1.ObjectMeta{Name: pnkOmittedName},
			Spec: v1alpha1.PodNetworkKindSpec{
				ImplementationType: metav1.GroupKind{
					Group: "default1.e2e.example.com",
					Kind:  "OmittedNet",
				},
				// spec.defaultPodNetworkKind is omitted
			},
		}

		By("creating a PodNetworkKind with spec.defaultPodNetworkKind omitted")
		_, err := multinetworkClient.PodNetworkKinds().Create(ctx, pnkOmitted, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func(ctx context.Context) {
			_ = multinetworkClient.PodNetworkKinds().Delete(ctx, pnkOmittedName, metav1.DeleteOptions{})
		})

		Eventually(func(g Gomega) {
			updated, err := multinetworkClient.PodNetworkKinds().Get(ctx, pnkOmittedName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(updated.Status.Conditions).NotTo(BeEmpty())

			cond := findCondition(updated.Status.Conditions, v1alpha1.PodNetworkKindConditionDefaultPodNetworkKind)
			g.Expect(cond).To(BeNil(), "DefaultPodNetworkKind condition should not be added when omitted")
			g.Expect(updated.Status.DefaultPodNetworkKind).To(BeNil())
		}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())

		pnkFalseName := "e2e-default-false"
		pnkFalse := &v1alpha1.PodNetworkKind{
			ObjectMeta: metav1.ObjectMeta{Name: pnkFalseName},
			Spec: v1alpha1.PodNetworkKindSpec{
				ImplementationType: metav1.GroupKind{
					Group: "default1.e2e.example.com",
					Kind:  "FalseNet",
				},
				DefaultPodNetworkKind: ptr.To(false),
			},
		}

		By("creating a PodNetworkKind with spec.defaultPodNetworkKind: false")
		_, err = multinetworkClient.PodNetworkKinds().Create(ctx, pnkFalse, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func(ctx context.Context) {
			_ = multinetworkClient.PodNetworkKinds().Delete(ctx, pnkFalseName, metav1.DeleteOptions{})
		})

		Eventually(func(g Gomega) {
			updated, err := multinetworkClient.PodNetworkKinds().Get(ctx, pnkFalseName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(updated.Status.Conditions).NotTo(BeEmpty())

			cond := findCondition(updated.Status.Conditions, v1alpha1.PodNetworkKindConditionDefaultPodNetworkKind)
			g.Expect(cond).To(BeNil(), "DefaultPodNetworkKind condition should not be added when false")
			g.Expect(updated.Status.DefaultPodNetworkKind).To(BeNil())
		}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())
	})

	It("Lifecycle of DefaultPodNetworkKind candidates A, B, and C", func(ctx context.Context) {
		pnkA := "e2e-default-candidate-a"
		pnkB := "e2e-default-candidate-b"
		pnkC := "e2e-default-candidate-c"

		By("creating a PodNetworkKind with spec.defaultPodNetworkKind: true when no default exists (PodNetworkKind A)")
		pnk1 := &v1alpha1.PodNetworkKind{
			ObjectMeta: metav1.ObjectMeta{Name: pnkA},
			Spec: v1alpha1.PodNetworkKindSpec{
				ImplementationType: metav1.GroupKind{
					Group: "defaultlifecycle.e2e.example.com",
					Kind:  "NetA",
				},
				DefaultPodNetworkKind: ptr.To(true),
			},
		}
		_, err := multinetworkClient.PodNetworkKinds().Create(ctx, pnk1, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func(ctx context.Context) {
			_ = multinetworkClient.PodNetworkKinds().Delete(ctx, pnkA, metav1.DeleteOptions{})
		})

		By("waiting for PodNetworkKind A to have status.defaultPodNetworkKind: true and condition True with reason DefaultPodNetworkKindSet")
		Eventually(func(g Gomega) {
			updated, err := multinetworkClient.PodNetworkKinds().Get(ctx, pnkA, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(updated.Status.DefaultPodNetworkKind).NotTo(BeNil())
			g.Expect(*updated.Status.DefaultPodNetworkKind).To(BeTrue())

			cond := findCondition(updated.Status.Conditions, v1alpha1.PodNetworkKindConditionDefaultPodNetworkKind)
			g.Expect(cond).NotTo(BeNil())
			g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(cond.Reason).To(Equal(v1alpha1.PodNetworkKindReasonDefaultPodNetworkKindSet))
		}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())

		By("creating subsequent PodNetworkKind objects (PodNetworkKind B and PodNetworkKind C) with spec.defaultPodNetworkKind: true while active default A exists")
		pnk2 := &v1alpha1.PodNetworkKind{
			ObjectMeta: metav1.ObjectMeta{Name: pnkB},
			Spec: v1alpha1.PodNetworkKindSpec{
				ImplementationType: metav1.GroupKind{
					Group: "defaultlifecycle.e2e.example.com",
					Kind:  "NetB",
				},
				DefaultPodNetworkKind: ptr.To(true),
			},
		}
		_, err = multinetworkClient.PodNetworkKinds().Create(ctx, pnk2, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func(ctx context.Context) {
			_ = multinetworkClient.PodNetworkKinds().Delete(ctx, pnkB, metav1.DeleteOptions{})
		})

		pnk3 := &v1alpha1.PodNetworkKind{
			ObjectMeta: metav1.ObjectMeta{Name: pnkC},
			Spec: v1alpha1.PodNetworkKindSpec{
				ImplementationType: metav1.GroupKind{
					Group: "defaultlifecycle.e2e.example.com",
					Kind:  "NetC",
				},
				DefaultPodNetworkKind: ptr.To(true),
			},
		}
		_, err = multinetworkClient.PodNetworkKinds().Create(ctx, pnk3, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func(ctx context.Context) {
			_ = multinetworkClient.PodNetworkKinds().Delete(ctx, pnkC, metav1.DeleteOptions{})
		})

		By("waiting for both PodNetworkKind B and C to have status.defaultPodNetworkKind: false and condition False with reason DefaultPodNetworkKindAlreadySet")
		Eventually(func(g Gomega) {
			b, err := multinetworkClient.PodNetworkKinds().Get(ctx, pnkB, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(b.Status.DefaultPodNetworkKind).NotTo(BeNil())
			g.Expect(*b.Status.DefaultPodNetworkKind).To(BeFalse())
			condB := findCondition(b.Status.Conditions, v1alpha1.PodNetworkKindConditionDefaultPodNetworkKind)
			g.Expect(condB).NotTo(BeNil())
			g.Expect(condB.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(condB.Reason).To(Equal(v1alpha1.PodNetworkKindReasonDefaultPodNetworkKindAlreadySet))

			c, err := multinetworkClient.PodNetworkKinds().Get(ctx, pnkC, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(c.Status.DefaultPodNetworkKind).NotTo(BeNil())
			g.Expect(*c.Status.DefaultPodNetworkKind).To(BeFalse())
			condC := findCondition(c.Status.Conditions, v1alpha1.PodNetworkKindConditionDefaultPodNetworkKind)
			g.Expect(condC).NotTo(BeNil())
			g.Expect(condC.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(condC.Reason).To(Equal(v1alpha1.PodNetworkKindReasonDefaultPodNetworkKindAlreadySet))
		}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())

		By("deleting the active default PodNetworkKind (A) when multiple PodNetworkKinds remain (PodNetworkKind B created before PodNetworkKind C)")
		err = multinetworkClient.PodNetworkKinds().Delete(ctx, pnkA, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("waiting for PodNetworkKind B to be promoted to status.defaultPodNetworkKind: true and condition True with reason DefaultPodNetworkKindSet, while PodNetworkKind C remains status.defaultPodNetworkKind: false with reason DefaultPodNetworkKindAlreadySet")
		Eventually(func(g Gomega) {
			b, err := multinetworkClient.PodNetworkKinds().Get(ctx, pnkB, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(b.Status.DefaultPodNetworkKind).NotTo(BeNil())
			g.Expect(*b.Status.DefaultPodNetworkKind).To(BeTrue())
			condB := findCondition(b.Status.Conditions, v1alpha1.PodNetworkKindConditionDefaultPodNetworkKind)
			g.Expect(condB).NotTo(BeNil())
			g.Expect(condB.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(condB.Reason).To(Equal(v1alpha1.PodNetworkKindReasonDefaultPodNetworkKindSet))

			c, err := multinetworkClient.PodNetworkKinds().Get(ctx, pnkC, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(c.Status.DefaultPodNetworkKind).NotTo(BeNil())
			g.Expect(*c.Status.DefaultPodNetworkKind).To(BeFalse())
			condC := findCondition(c.Status.Conditions, v1alpha1.PodNetworkKindConditionDefaultPodNetworkKind)
			g.Expect(condC).NotTo(BeNil())
			g.Expect(condC.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(condC.Reason).To(Equal(v1alpha1.PodNetworkKindReasonDefaultPodNetworkKindAlreadySet))
		}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())

		By("deleting PodNetworkKind B")
		err = multinetworkClient.PodNetworkKinds().Delete(ctx, pnkB, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("waiting for PodNetworkKind C to be promoted to status.defaultPodNetworkKind: true and condition True with reason DefaultPodNetworkKindSet")
		Eventually(func(g Gomega) {
			c, err := multinetworkClient.PodNetworkKinds().Get(ctx, pnkC, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(c.Status.DefaultPodNetworkKind).NotTo(BeNil())
			g.Expect(*c.Status.DefaultPodNetworkKind).To(BeTrue())
			condC := findCondition(c.Status.Conditions, v1alpha1.PodNetworkKindConditionDefaultPodNetworkKind)
			g.Expect(condC).NotTo(BeNil())
			g.Expect(condC.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(condC.Reason).To(Equal(v1alpha1.PodNetworkKindReasonDefaultPodNetworkKindSet))
		}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())
	})
})
