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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("PodNetworkKind Validation", func() {
	Context("immutability", func() {
		It("should not allow modifying spec.implementationType", func(ctx context.Context) {
			pnkName := "e2e-val-immutable-impl-type"
			pnk := &v1alpha1.PodNetworkKind{
				ObjectMeta: metav1.ObjectMeta{
					Name: pnkName,
				},
				Spec: v1alpha1.PodNetworkKindSpec{
					ImplementationType: metav1.GroupKind{
						Group: "example.com",
						Kind:  "InitialNetwork",
					},
				},
			}

			By("creating the PodNetworkKind")
			_, err := multinetworkClient.PodNetworkKinds().Create(ctx, pnk, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create PodNetworkKind")

			DeferCleanup(func(ctx context.Context) {
				_ = multinetworkClient.PodNetworkKinds().Delete(ctx, pnkName, metav1.DeleteOptions{})
			})

			By("attempting to update spec.implementationType.group")
			Eventually(func(g Gomega) {
				current, err := multinetworkClient.PodNetworkKinds().Get(ctx, pnkName, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				toUpdateGroup := current.DeepCopy()
				toUpdateGroup.Spec.ImplementationType.Group = "updated.example.com"
				_, err = multinetworkClient.PodNetworkKinds().Update(ctx, toUpdateGroup, metav1.UpdateOptions{})
				g.Expect(err).To(HaveOccurred(), "expected error when updating implementationType.group")
				if apierrors.IsConflict(err) {
					return
				}
				g.Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected Invalid error, got: %v", err)
				g.Expect(err.Error()).To(ContainSubstring("spec.ImplementationType is immutable"))
			}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())

			By("attempting to update spec.implementationType.kind")
			Eventually(func(g Gomega) {
				current, err := multinetworkClient.PodNetworkKinds().Get(ctx, pnkName, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				toUpdateKind := current.DeepCopy()
				toUpdateKind.Spec.ImplementationType.Kind = "UpdatedNetwork"
				_, err = multinetworkClient.PodNetworkKinds().Update(ctx, toUpdateKind, metav1.UpdateOptions{})
				g.Expect(err).To(HaveOccurred(), "expected error when updating implementationType.kind")
				if apierrors.IsConflict(err) {
					return
				}
				g.Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected Invalid error, got: %v", err)
				g.Expect(err.Error()).To(ContainSubstring("spec.ImplementationType is immutable"))
			}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())
		})

		It("should not allow modifying spec.defaultPodNetworkKind", func(ctx context.Context) {
			pnkName := "e2e-val-immutable-default-pnk"
			pnk := &v1alpha1.PodNetworkKind{
				ObjectMeta: metav1.ObjectMeta{
					Name: pnkName,
				},
				Spec: v1alpha1.PodNetworkKindSpec{
					ImplementationType: metav1.GroupKind{
						Group: "example.com",
						Kind:  "DefaultNet",
					},
					DefaultPodNetworkKind: ptr.To(true),
				},
			}

			By("creating the PodNetworkKind with defaultPodNetworkKind=true")
			_, err := multinetworkClient.PodNetworkKinds().Create(ctx, pnk, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create PodNetworkKind")

			DeferCleanup(func(ctx context.Context) {
				_ = multinetworkClient.PodNetworkKinds().Delete(ctx, pnkName, metav1.DeleteOptions{})
			})

			By("attempting to update spec.defaultPodNetworkKind")
			Eventually(func(g Gomega) {
				current, err := multinetworkClient.PodNetworkKinds().Get(ctx, pnkName, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				toUpdate := current.DeepCopy()
				toUpdate.Spec.DefaultPodNetworkKind = ptr.To(false)
				_, err = multinetworkClient.PodNetworkKinds().Update(ctx, toUpdate, metav1.UpdateOptions{})
				g.Expect(err).To(HaveOccurred(), "expected error when updating defaultPodNetworkKind")
				if apierrors.IsConflict(err) {
					return
				}
				g.Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected Invalid error, got: %v", err)
				g.Expect(err.Error()).To(ContainSubstring("spec.DefaultPodNetworkKind is immutable"))
			}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())
		})
	})
})
