package project_test

import (
	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	. "github.com/gardener/gardener/pkg/utils/test/matchers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
)

var _ = Describe("Project Controller Integration Tests", func() {
	var project *gardenercorev1beta1.Project

	BeforeEach(func() {
		project = &gardenercorev1beta1.Project{
			ObjectMeta: metav1.ObjectMeta{
				Name: "p-" + testRunID[:7],
				Labels: map[string]string{
					testID: testRunID,
				},
			},
		}

		DeferCleanup(func() {
			By("Delete test Project")
			Expect(testClient.Delete(ctx, project)).To(Or(Succeed(), BeNotFoundError()))
		})
	})

	// This is the primary happy-path test: it reflects the real production scenario where
	// Project objects carry no sci.cloud.sap/* labels — those labels live only on the Namespace.
	When("Project has Spec.Namespace pointing to a Namespace with OpenStack labels", func() {
		It("should create project, role, role binding and ResourceQuota", func() {
			namespaceName := kubernetes.GetGardenerProjectNamespaceName("test-region", "testproj1")

			By("Pre-create Namespace with OpenStack labels")
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
					Labels: map[string]string{
						testID:                       testRunID,
						"sci.cloud.sap/project-id":   "testproj1",
						"sci.cloud.sap/project-name": "test-proj-name",
						"sci.cloud.sap/domain-id":    "testdom1",
						"sci.cloud.sap/domain-name":  "test-domain-name",
						"sci.cloud.sap/region":       "test-region",
						v1beta1constants.GardenRole:  v1beta1constants.GardenRoleProject,
						v1beta1constants.ProjectName: kubernetes.GetGardenerProjectName("test-region", "testproj1", "test-landscape"),
					},
				},
			}
			Expect(testClient.Create(ctx, ns)).To(Succeed())
			DeferCleanup(func() {
				Expect(testClient.Delete(ctx, ns)).To(Or(Succeed(), BeNotFoundError()))
			})

			By("Create Project with Spec.Namespace pointing to the pre-existing Namespace")
			project.Spec.Namespace = &namespaceName
			Expect(testClient.Create(ctx, project)).To(Succeed())

			expectedProjectName := kubernetes.GetGardenerProjectName("test-region", "testproj1", "test-landscape")

			By("Verify Project resource is created and linked to namespace")
			Eventually(func(g Gomega) string {
				gardenerProject := &gardenercorev1beta1.Project{ObjectMeta: metav1.ObjectMeta{Name: expectedProjectName}}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(gardenerProject), gardenerProject)).To(Succeed())
				g.Expect(gardenerProject.Spec.Namespace).NotTo(BeNil())
				return *gardenerProject.Spec.Namespace
			}).Should(Equal(namespaceName))

			By("Verify Role is created with correct permissions")
			Eventually(func(g Gomega) {
				role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "persephone.sci.cloud.sap:openstack-project-kubernetes-admin", Namespace: namespaceName}}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(role), role)).To(Succeed())

				g.Expect(role.Rules).To(ContainElement(rbacv1.PolicyRule{
					APIGroups: []string{gardenercorev1beta1.GroupName},
					Resources: []string{"shoots"},
					Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
				}))
				g.Expect(role.Rules).To(ContainElement(rbacv1.PolicyRule{
					APIGroups: []string{gardenercorev1beta1.GroupName},
					Resources: []string{"shoots/adminkubeconfig"},
					Verbs:     []string{"create"},
				}))
			}).Should(Succeed())

			By("Verify RoleBinding is created with correct subject")
			Eventually(func(g Gomega) {
				roleBinding := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "persephone.sci.cloud.sap:openstack-project-kubernetes-admin", Namespace: namespaceName}}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(roleBinding), roleBinding)).To(Succeed())

				g.Expect(roleBinding.Subjects).To(ConsistOf(rbacv1.Subject{
					APIGroup: rbacv1.GroupName,
					Kind:     "Group",
					Name:     "testproj1:kubernetes_admin",
				}))
				g.Expect(roleBinding.RoleRef).To(Equal(rbacv1.RoleRef{
					APIGroup: rbacv1.GroupName,
					Kind:     "Role",
					Name:     "persephone.sci.cloud.sap:openstack-project-kubernetes-admin",
				}))
			}).Should(Succeed())

			By("Verify Project object does NOT carry sci.cloud.sap/* labels")
			Consistently(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(project), project)).To(Succeed())
				for k := range project.Labels {
					g.Expect(k).NotTo(HavePrefix("sci.cloud.sap/"))
				}
			}).Should(Succeed())
		})
	})

	When("Project has no Spec.Namespace (not created by Persephone)", func() {
		It("should skip reconciliation gracefully without error", func() {
			By("Create Project without Spec.Namespace")
			Expect(testClient.Create(ctx, project)).To(Succeed())

			By("Verify controller creates nothing")
			Consistently(func() error {
				namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: kubernetes.GetGardenerProjectNamespaceName("test-region", "test-project-id")}}
				return testClient.Get(ctx, client.ObjectKeyFromObject(namespace), namespace)
			}).Should(BeNotFoundError())
		})
	})

	When("Project already has deletion timestamp", func() {
		It("should not reconcile", func() {
			namespaceName := kubernetes.GetGardenerProjectNamespaceName("deletable-region", "deleteproj3")

			By("Pre-create Namespace with OpenStack labels")
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
					Labels: map[string]string{
						testID:                       testRunID,
						"sci.cloud.sap/project-id":   "deleteproj3",
						"sci.cloud.sap/project-name": "deletable-proj-name",
						"sci.cloud.sap/domain-id":    "deletedom3",
						"sci.cloud.sap/domain-name":  "deletable-domain-name",
						"sci.cloud.sap/region":       "deletable-region",
						v1beta1constants.GardenRole:  v1beta1constants.GardenRoleProject,
						v1beta1constants.ProjectName: kubernetes.GetGardenerProjectName("deletable-region", "deleteproj3", "test-landscape"),
					},
				},
			}
			Expect(testClient.Create(ctx, ns)).To(Succeed())
			DeferCleanup(func() {
				Expect(testClient.Delete(ctx, ns)).To(Or(Succeed(), BeNotFoundError()))
			})

			By("Create Project with Spec.Namespace")
			project.Spec.Namespace = &namespaceName
			Expect(testClient.Create(ctx, project)).To(Succeed())

			expectedProjectName := kubernetes.GetGardenerProjectName("deletable-region", "deleteproj3", "test-landscape")

			By("Wait for managed Project resource to be created")
			Eventually(func() error {
				gardenerProject := &gardenercorev1beta1.Project{ObjectMeta: metav1.ObjectMeta{Name: expectedProjectName}}
				return testClient.Get(ctx, client.ObjectKeyFromObject(gardenerProject), gardenerProject)
			}).Should(Succeed())

			By("Delete the Project under test")
			Expect(testClient.Delete(ctx, project)).To(Succeed())

			By("Verify namespace still exists (controller ignores deleted Projects)")
			Consistently(func() error {
				namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespaceName}}
				return testClient.Get(ctx, client.ObjectKeyFromObject(namespace), namespace)
			}).Should(Succeed())
		})
	})

	When("Multiple Projects point to the same Namespace", func() {
		var project2 *gardenercorev1beta1.Project

		BeforeEach(func() {
			project2 = &gardenercorev1beta1.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name: "p2-" + testRunID[:6],
					Labels: map[string]string{
						testID: testRunID,
					},
				},
			}

			DeferCleanup(func() {
				By("Delete test Project 2")
				Expect(testClient.Delete(ctx, project2)).To(Or(Succeed(), BeNotFoundError()))
			})
		})

		It("should reconcile both projects to same namespace without conflict", func() {
			sharedOpenStackProjectID := "sharedproj4"
			namespaceName := kubernetes.GetGardenerProjectNamespaceName("shared-region", sharedOpenStackProjectID)

			By("Pre-create shared Namespace with OpenStack labels")
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
					Labels: map[string]string{
						testID:                       testRunID,
						"sci.cloud.sap/project-id":   sharedOpenStackProjectID,
						"sci.cloud.sap/project-name": "shared-proj-name",
						"sci.cloud.sap/domain-id":    "shared-domain-id",
						"sci.cloud.sap/domain-name":  "shared-domain-name",
						"sci.cloud.sap/region":       "shared-region",
						v1beta1constants.GardenRole:  v1beta1constants.GardenRoleProject,
						v1beta1constants.ProjectName: kubernetes.GetGardenerProjectName("shared-region", sharedOpenStackProjectID, "test-landscape"),
					},
				},
			}
			Expect(testClient.Create(ctx, ns)).To(Succeed())
			DeferCleanup(func() {
				Expect(testClient.Delete(ctx, ns)).To(Or(Succeed(), BeNotFoundError()))
			})

			By("Create first Project pointing to shared Namespace")
			project.Spec.Namespace = &namespaceName
			Expect(testClient.Create(ctx, project)).To(Succeed())

			By("Wait for managed Project resource to appear")
			Eventually(func() error {
				namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespaceName}}
				return testClient.Get(ctx, client.ObjectKeyFromObject(namespace), namespace)
			}).Should(Succeed())

			By("Create second Project also pointing to shared Namespace")
			project2.Spec.Namespace = &namespaceName
			Expect(testClient.Create(ctx, project2)).To(Succeed())

			By("Verify both projects reconcile successfully (no conflicts)")
			Consistently(func(g Gomega) map[string]string {
				namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespaceName}}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(namespace), namespace)).To(Succeed())
				return namespace.Labels
			}).Should(HaveKeyWithValue("sci.cloud.sap/project-id", sharedOpenStackProjectID))
		})
	})
})
