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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubernetes-sigs/multi-network-api/apis/v1alpha1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("PodNetworkKind ImplementationTypeReady Condition", func() {
	Context("CRD presence (CRDNotFound)", func() {
		It("should report False when CRD is missing, turn True when created, and turn back False when deleted", func(ctx context.Context) {
			group := "crdpresence.e2e.example.com"
			kind := "PresenceNet"
			plural := "presencenets"
			crdName := plural + "." + group
			pnkName := "e2e-crd-presence"

			By("granting RBAC permissions to the controller in advance")
			grantRBAC(ctx, "e2e-presence-reader", group, plural)
			DeferCleanup(func(ctx context.Context) {
				deleteRBAC(ctx, "e2e-presence-reader")
			})

			pnk := &v1alpha1.PodNetworkKind{
				ObjectMeta: metav1.ObjectMeta{Name: pnkName},
				Spec: v1alpha1.PodNetworkKindSpec{
					ImplementationType: metav1.GroupKind{Group: group, Kind: kind},
				},
			}

			By("creating the PodNetworkKind before the CRD exists")
			_, err := multinetworkClient.PodNetworkKinds().Create(ctx, pnk, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create PodNetworkKind")

			DeferCleanup(func(ctx context.Context) {
				_ = multinetworkClient.PodNetworkKinds().Delete(ctx, pnkName, metav1.DeleteOptions{})
			})

			By("waiting for ImplementationTypeReady=False with reason CRDNotFound")
			Eventually(func(g Gomega) {
				updated, err := multinetworkClient.PodNetworkKinds().Get(ctx, pnkName, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				cond := findCondition(updated.Status.Conditions, v1alpha1.PodNetworkKindConditionImplementationTypeReady)
				g.Expect(cond).NotTo(BeNil(), "ImplementationTypeReady condition not found")
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(v1alpha1.PodNetworkKindReasonCRDNotFound))
				g.Expect(cond.Message).To(Equal("CRD not found"))
			}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())

			By("creating the compliant CRD")
			createCRD(ctx, crdName, group, kind, plural, []string{"podnetwork", "podnetworks"})

			By("waiting for ImplementationTypeReady to turn True with reason Compliant")
			Eventually(func(g Gomega) {
				updated, err := multinetworkClient.PodNetworkKinds().Get(ctx, pnkName, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				cond := findCondition(updated.Status.Conditions, v1alpha1.PodNetworkKindConditionImplementationTypeReady)
				g.Expect(cond).NotTo(BeNil(), "ImplementationTypeReady condition not found")
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(v1alpha1.PodNetworkKindReasonCompliant))
			}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())

			By("deleting the CRD to make the condition unmet again")
			err = apiextensionsClient.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, crdName, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("waiting for ImplementationTypeReady to turn back False with reason CRDNotFound")
			Eventually(func(g Gomega) {
				updated, err := multinetworkClient.PodNetworkKinds().Get(ctx, pnkName, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				cond := findCondition(updated.Status.Conditions, v1alpha1.PodNetworkKindConditionImplementationTypeReady)
				g.Expect(cond).NotTo(BeNil(), "ImplementationTypeReady condition not found")
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(v1alpha1.PodNetworkKindReasonCRDNotFound))
				g.Expect(cond.Message).To(Equal("CRD not found"))
			}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())
		})
	})

	Context("CRD readiness (CRDNotReady)", func() {
		It("should report False when CRD names conflict, turn True when conflict resolved, and turn back False when conflict reintroduced", func(ctx context.Context) {
			group := "crdreadiness.e2e.example.com"
			sharedListKind := "SharedConflictList"
			kind1 := "ConflictKindOne"
			plural1 := "conflictkindones"
			crdName1 := plural1 + "." + group

			kind2 := "ConflictKindTwo"
			plural2 := "conflictkindtwos"
			crdName2 := plural2 + "." + group
			pnkName := "e2e-crd-not-ready"
			rbacName := "e2e-crd-not-ready-reader"

			By("granting RBAC permissions to the controller in advance")
			grantRBAC(ctx, rbacName, group, plural2)
			DeferCleanup(func(ctx context.Context) {
				deleteRBAC(ctx, rbacName)
			})

			By("creating the first CRD occupying the listKind")
			createCRDWithListKind(ctx, crdName1, group, kind1, plural1, sharedListKind, nil)
			DeferCleanup(func(ctx context.Context) {
				_ = apiextensionsClient.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, crdName1, metav1.DeleteOptions{})
			})

			By("creating the second CRD with conflicting listKind")
			createCRDUnestablished(ctx, crdName2, group, kind2, plural2, sharedListKind, []string{"podnetwork", "podnetworks"})
			DeferCleanup(func(ctx context.Context) {
				_ = apiextensionsClient.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, crdName2, metav1.DeleteOptions{})
			})

			pnk := &v1alpha1.PodNetworkKind{
				ObjectMeta: metav1.ObjectMeta{Name: pnkName},
				Spec: v1alpha1.PodNetworkKindSpec{
					ImplementationType: metav1.GroupKind{Group: group, Kind: kind2},
				},
			}

			By("creating the PodNetworkKind referencing the unready CRD")
			_, err := multinetworkClient.PodNetworkKinds().Create(ctx, pnk, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create PodNetworkKind")

			DeferCleanup(func(ctx context.Context) {
				_ = multinetworkClient.PodNetworkKinds().Delete(ctx, pnkName, metav1.DeleteOptions{})
			})

			By("waiting for ImplementationTypeReady=False with reason CRDNotReady")
			Eventually(func(g Gomega) {
				updated, err := multinetworkClient.PodNetworkKinds().Get(ctx, pnkName, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				cond := findCondition(updated.Status.Conditions, v1alpha1.PodNetworkKindConditionImplementationTypeReady)
				g.Expect(cond).NotTo(BeNil(), "ImplementationTypeReady condition not found")
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(v1alpha1.PodNetworkKindReasonCRDNotReady))
			}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())

			By("deleting the first CRD to resolve the conflict")
			err = apiextensionsClient.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, crdName1, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("waiting for ImplementationTypeReady to turn True with reason Compliant")
			Eventually(func(g Gomega) {
				updated, err := multinetworkClient.PodNetworkKinds().Get(ctx, pnkName, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				cond := findCondition(updated.Status.Conditions, v1alpha1.PodNetworkKindConditionImplementationTypeReady)
				g.Expect(cond).NotTo(BeNil(), "ImplementationTypeReady condition not found")
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(v1alpha1.PodNetworkKindReasonCompliant))
			}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())

			By("re-introducing conflict by deleting second CRD, recreating first CRD, and recreating second CRD")
			err = apiextensionsClient.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, crdName2, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() bool {
				_, getErr := apiextensionsClient.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, crdName2, metav1.GetOptions{})
				return apierrors.IsNotFound(getErr)
			}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(BeTrue())

			createCRDWithListKind(ctx, crdName1, group, kind1, plural1, sharedListKind, nil)
			createCRDUnestablished(ctx, crdName2, group, kind2, plural2, sharedListKind, []string{"podnetwork", "podnetworks"})

			By("waiting for ImplementationTypeReady to turn back False with reason CRDNotReady")
			Eventually(func(g Gomega) {
				updated, err := multinetworkClient.PodNetworkKinds().Get(ctx, pnkName, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				cond := findCondition(updated.Status.Conditions, v1alpha1.PodNetworkKindConditionImplementationTypeReady)
				g.Expect(cond).NotTo(BeNil(), "ImplementationTypeReady condition not found")
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(v1alpha1.PodNetworkKindReasonCRDNotReady))
			}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())
		})
	})

	Context("CRD categories (CRDMissingCategories)", func() {
		It("should report False when categories are missing, turn True when added, and turn back False when removed", func(ctx context.Context) {
			group := "crdcats.e2e.example.com"
			kind := "CatNet"
			plural := "catnets"
			crdName := plural + "." + group
			pnkName := "e2e-crd-categories"

			By("granting RBAC permissions to the controller in advance")
			grantRBAC(ctx, "e2e-cats-reader", group, plural)
			DeferCleanup(func(ctx context.Context) {
				deleteRBAC(ctx, "e2e-cats-reader")
			})

			By("creating a CRD without categories")
			createCRD(ctx, crdName, group, kind, plural, []string{})
			DeferCleanup(func(ctx context.Context) {
				_ = apiextensionsClient.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, crdName, metav1.DeleteOptions{})
			})

			pnk := &v1alpha1.PodNetworkKind{
				ObjectMeta: metav1.ObjectMeta{Name: pnkName},
				Spec: v1alpha1.PodNetworkKindSpec{
					ImplementationType: metav1.GroupKind{Group: group, Kind: kind},
				},
			}

			By("creating the PodNetworkKind referencing the CRD without categories")
			_, err := multinetworkClient.PodNetworkKinds().Create(ctx, pnk, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create PodNetworkKind")

			DeferCleanup(func(ctx context.Context) {
				_ = multinetworkClient.PodNetworkKinds().Delete(ctx, pnkName, metav1.DeleteOptions{})
			})

			By("waiting for ImplementationTypeReady=False with reason CRDMissingCategories")
			Eventually(func(g Gomega) {
				updated, err := multinetworkClient.PodNetworkKinds().Get(ctx, pnkName, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				cond := findCondition(updated.Status.Conditions, v1alpha1.PodNetworkKindConditionImplementationTypeReady)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(v1alpha1.PodNetworkKindReasonCRDMissingCategories))
				g.Expect(cond.Message).To(ContainSubstring("CRD is missing mandatory category"))
			}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())

			By("updating the CRD to add the mandatory categories")
			updateCRDCategories(ctx, crdName, []string{"podnetwork", "podnetworks"})

			By("waiting for ImplementationTypeReady to turn True with reason Compliant")
			Eventually(func(g Gomega) {
				updated, err := multinetworkClient.PodNetworkKinds().Get(ctx, pnkName, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				cond := findCondition(updated.Status.Conditions, v1alpha1.PodNetworkKindConditionImplementationTypeReady)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(v1alpha1.PodNetworkKindReasonCompliant))
			}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())

			By("updating the CRD to remove mandatory categories again")
			updateCRDCategories(ctx, crdName, []string{})

			By("waiting for ImplementationTypeReady to turn back False with reason CRDMissingCategories")
			Eventually(func(g Gomega) {
				updated, err := multinetworkClient.PodNetworkKinds().Get(ctx, pnkName, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				cond := findCondition(updated.Status.Conditions, v1alpha1.PodNetworkKindConditionImplementationTypeReady)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(v1alpha1.PodNetworkKindReasonCRDMissingCategories))
				g.Expect(cond.Message).To(ContainSubstring("CRD is missing mandatory category"))
			}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())
		})
	})

	Context("RBAC permissions (MissingRBAC)", func() {
		It("should report False when RBAC is missing, turn True when granted, and turn back False when revoked", func(ctx context.Context) {
			group := "crdrbac.e2e.example.com"
			kind := "RbacNet"
			plural := "rbacnets"
			crdName := plural + "." + group
			pnkName := "e2e-crd-rbac"
			rbacName := "e2e-rbac-test-reader"

			By("creating a compliant CRD without granting RBAC")
			createCRD(ctx, crdName, group, kind, plural, []string{"podnetwork", "podnetworks"})
			DeferCleanup(func(ctx context.Context) {
				_ = apiextensionsClient.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, crdName, metav1.DeleteOptions{})
			})

			pnk := &v1alpha1.PodNetworkKind{
				ObjectMeta: metav1.ObjectMeta{Name: pnkName},
				Spec: v1alpha1.PodNetworkKindSpec{
					ImplementationType: metav1.GroupKind{Group: group, Kind: kind},
				},
			}

			By("creating the PodNetworkKind referencing the CRD without RBAC")
			_, err := multinetworkClient.PodNetworkKinds().Create(ctx, pnk, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create PodNetworkKind")

			DeferCleanup(func(ctx context.Context) {
				_ = multinetworkClient.PodNetworkKinds().Delete(ctx, pnkName, metav1.DeleteOptions{})
			})

			By("waiting for ImplementationTypeReady=False with reason MissingRBAC")
			Eventually(func(g Gomega) {
				updated, err := multinetworkClient.PodNetworkKinds().Get(ctx, pnkName, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(updated.Status.Conditions).NotTo(BeEmpty())

				cond := findCondition(updated.Status.Conditions, v1alpha1.PodNetworkKindConditionImplementationTypeReady)
				g.Expect(cond).NotTo(BeNil(), "ImplementationTypeReady condition not found")
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(v1alpha1.PodNetworkKindReasonMissingRBAC))
				g.Expect(cond.Message).To(Equal("Insufficient permissions to access the Custom Resources"))
			}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())

			By("granting RBAC permissions to the controller")
			grantRBAC(ctx, rbacName, group, plural)

			By("waiting for ImplementationTypeReady to turn True with reason Compliant")
			Eventually(func(g Gomega) {
				updated, err := multinetworkClient.PodNetworkKinds().Get(ctx, pnkName, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				cond := findCondition(updated.Status.Conditions, v1alpha1.PodNetworkKindConditionImplementationTypeReady)
				g.Expect(cond).NotTo(BeNil(), "ImplementationTypeReady condition not found")
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(v1alpha1.PodNetworkKindReasonCompliant))
			}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())

			By("revoking RBAC permissions to make the condition unmet again")
			deleteRBAC(ctx, rbacName)

			By("waiting for ImplementationTypeReady to turn back False with reason MissingRBAC")
			Eventually(func(g Gomega) {
				updated, err := multinetworkClient.PodNetworkKinds().Get(ctx, pnkName, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				cond := findCondition(updated.Status.Conditions, v1alpha1.PodNetworkKindConditionImplementationTypeReady)
				g.Expect(cond).NotTo(BeNil(), "ImplementationTypeReady condition not found")
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(v1alpha1.PodNetworkKindReasonMissingRBAC))
				g.Expect(cond.Message).To(Equal("Insufficient permissions to access the Custom Resources"))
			}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())
		})
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

func grantRBAC(ctx context.Context, name, group, resource string) {
	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{group},
				Resources: []string{resource},
				Verbs:     []string{"list"},
			},
		},
	}
	_, err := kubeClient.RbacV1().ClusterRoles().Create(ctx, cr, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())

	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-binding"},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "multi-network-controller-service-account",
				Namespace: "default",
			},
		},
	}
	_, err = kubeClient.RbacV1().ClusterRoleBindings().Create(ctx, crb, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
}

func deleteRBAC(ctx context.Context, name string) {
	_ = kubeClient.RbacV1().ClusterRoleBindings().Delete(ctx, name+"-binding", metav1.DeleteOptions{})
	_ = kubeClient.RbacV1().ClusterRoles().Delete(ctx, name, metav1.DeleteOptions{})
}

func buildCRD(name, group, kind, plural, listKind string, categories []string) *apiextensionsv1.CustomResourceDefinition {
	names := apiextensionsv1.CustomResourceDefinitionNames{
		Plural:     plural,
		Singular:   strings.ToLower(kind),
		Kind:       kind,
		Categories: categories,
	}
	if listKind != "" {
		names.ListKind = listKind
	}
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: group,
			Names: names,
			Scope: apiextensionsv1.ClusterScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1alpha1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type: "object",
						},
					},
				},
			},
		},
	}
}

func createCRD(ctx context.Context, name, group, kind, plural string, categories []string) *apiextensionsv1.CustomResourceDefinition {
	return createCRDWithListKind(ctx, name, group, kind, plural, "", categories)
}

func createCRDWithListKind(ctx context.Context, name, group, kind, plural, listKind string, categories []string) *apiextensionsv1.CustomResourceDefinition {
	crd := buildCRD(name, group, kind, plural, listKind, categories)
	created, err := apiextensionsClient.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, crd, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())

	Eventually(func(g Gomega) {
		c, err := apiextensionsClient.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		var established bool
		for _, cond := range c.Status.Conditions {
			if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
				established = true
				break
			}
		}
		g.Expect(established).To(BeTrue(), "CRD is not established")
	}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())

	return created
}

func createCRDUnestablished(ctx context.Context, name, group, kind, plural, listKind string, categories []string) *apiextensionsv1.CustomResourceDefinition {
	crd := buildCRD(name, group, kind, plural, listKind, categories)
	created, err := apiextensionsClient.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, crd, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())

	Eventually(func(g Gomega) {
		c, err := apiextensionsClient.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(c.Status.Conditions).NotTo(BeEmpty())
	}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())

	return created
}

func updateCRDCategories(ctx context.Context, name string, categories []string) {
	Eventually(func(g Gomega) {
		crd, err := apiextensionsClient.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		crd.Spec.Names.Categories = categories
		_, err = apiextensionsClient.ApiextensionsV1().CustomResourceDefinitions().Update(ctx, crd, metav1.UpdateOptions{})
		if apierrors.IsConflict(err) {
			return
		}
		g.Expect(err).NotTo(HaveOccurred())
	}).WithContext(ctx).WithTimeout(Timeout).WithPolling(Polling).Should(Succeed())
}
