// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package kubernetes_test

import (
	"context"
	"time"

	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/applicationcredentials"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/sap-cloud-infrastructure/persephone/internal/config"
	. "github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
)

var _ = Describe("Gardener", func() {
	Describe("GetGardenerProjectNamespaceName", func() {
		It("should return namespace name with garden prefix for a region plus standard project ID combination", func() {
			region := "region-1"
			projectID := "12345678901234567890"
			result := GetGardenerProjectNamespaceName(region, projectID)
			Expect(result).To(Equal("garden-region-1-12345678901234567890"))
		})
	})

	Describe("GetGardenerProjectName", func() {
		It("should create a predictable hash string out of the given region, project ID and landscape", func() {
			region := "region-1"
			projectID := "12345678901234567890"
			landscapeName := "canary"
			result1 := GetGardenerProjectName(region, projectID, landscapeName)
			result2 := GetGardenerProjectName(region, projectID, landscapeName)
			Expect(result1).To(Equal(result2))
		})

		It("should not exceed 10 characters", func() {
			region := "region-1"
			projectID := "12345678901234567890"
			landscapeName := "canary"
			result := GetGardenerProjectName(region, projectID, landscapeName)
			Expect(result).To(HaveLen(10))
		})
	})

	Describe("ReconcileGardenerProjectResources", func() {
		var (
			ctx        context.Context
			fakeClient client.Client
			cfg        config.PersephoneConfig

			openStackUserInfo OpenStackUserInfo
		)

		BeforeEach(func() {
			ctx = context.Background()
			fakeClient = fakeclient.NewClientBuilder().WithScheme(kubernetes.GardenScheme).Build()
			cfg = config.PersephoneConfig{LandscapeName: "test"}

			openStackUserInfo = OpenStackUserInfo{
				ProjectDomainID:   "domain-id-123",
				ProjectDomainName: "domain-name",
				ProjectID:         "1234567890123456",
				ProjectName:       "test-project",
				UserDomainID:      "user-domain-id",
				UserDomainName:    "user-domain-name",
				Region:            "region-1",
			}
		})

		It("should create a new project namespace", func() {
			Expect(ReconcileGardenerProjectResources(ctx, fakeClient, cfg, openStackUserInfo)).To(Succeed())

			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "garden-" + openStackUserInfo.Region + "-" + openStackUserInfo.ProjectID}}
			projectName := GetGardenerProjectName(openStackUserInfo.Region, openStackUserInfo.ProjectID, cfg.LandscapeName)

			Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(namespace), namespace)).NotTo(HaveOccurred())
			Expect(namespace.Labels).To(HaveKeyWithValue("gardener.cloud/role", "project"))
			Expect(namespace.Labels).To(HaveKeyWithValue("project.gardener.cloud/name", projectName))
			Expect(namespace.Labels).To(HaveKeyWithValue("sci.cloud.sap/project-id", openStackUserInfo.ProjectID))
			Expect(namespace.Labels).To(HaveKeyWithValue("sci.cloud.sap/project-name", openStackUserInfo.ProjectName))
			Expect(namespace.Labels).To(HaveKeyWithValue("sci.cloud.sap/domain-id", openStackUserInfo.ProjectDomainID))
			Expect(namespace.Labels).To(HaveKeyWithValue("sci.cloud.sap/domain-name", openStackUserInfo.ProjectDomainName))
			Expect(namespace.Labels).To(HaveKeyWithValue("sci.cloud.sap/region", openStackUserInfo.Region))
		})

		It("should reconcile an existing project namespace", func() {
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: GetGardenerProjectNamespaceName(openStackUserInfo.Region, openStackUserInfo.ProjectID),
					Labels: map[string]string{
						"existing-label": "existing-value",
					},
				},
			}
			Expect(fakeClient.Create(ctx, namespace)).To(Succeed())

			Expect(ReconcileGardenerProjectResources(ctx, fakeClient, cfg, openStackUserInfo)).To(Succeed())

			projectName := GetGardenerProjectName(openStackUserInfo.Region, openStackUserInfo.ProjectID, cfg.LandscapeName)

			Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(namespace), namespace)).NotTo(HaveOccurred())
			Expect(namespace.Labels).To(HaveKeyWithValue("gardener.cloud/role", "project"))
			Expect(namespace.Labels).To(HaveKeyWithValue("project.gardener.cloud/name", projectName))
			Expect(namespace.Labels).To(HaveKeyWithValue("sci.cloud.sap/project-id", openStackUserInfo.ProjectID))
			Expect(namespace.Labels).To(HaveKeyWithValue("sci.cloud.sap/project-name", openStackUserInfo.ProjectName))
			Expect(namespace.Labels).To(HaveKeyWithValue("sci.cloud.sap/domain-id", openStackUserInfo.ProjectDomainID))
			Expect(namespace.Labels).To(HaveKeyWithValue("sci.cloud.sap/domain-name", openStackUserInfo.ProjectDomainName))
			Expect(namespace.Labels).To(HaveKeyWithValue("sci.cloud.sap/region", openStackUserInfo.Region))
			Expect(namespace.Labels).To(HaveKeyWithValue("existing-label", "existing-value"))
		})

		It("should create a new project", func() {
			Expect(ReconcileGardenerProjectResources(ctx, fakeClient, cfg, openStackUserInfo)).To(Succeed())

			projectName := GetGardenerProjectName(openStackUserInfo.Region, openStackUserInfo.ProjectID, cfg.LandscapeName)

			project := &gardenercorev1beta1.Project{ObjectMeta: metav1.ObjectMeta{Name: projectName}}
			Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(project), project)).NotTo(HaveOccurred())
			Expect(project.Spec.Namespace).To(PointTo(Equal("garden-" + openStackUserInfo.Region + "-" + openStackUserInfo.ProjectID)))
		})

		It("should reconcile an existing project", func() {
			projectName := GetGardenerProjectName(openStackUserInfo.Region, openStackUserInfo.ProjectID, cfg.LandscapeName)

			project := &gardenercorev1beta1.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name: projectName,
				},
				Spec: gardenercorev1beta1.ProjectSpec{
					Namespace: new("old-namespace"),
				},
			}
			Expect(fakeClient.Create(ctx, project)).To(Succeed())

			Expect(ReconcileGardenerProjectResources(ctx, fakeClient, cfg, openStackUserInfo)).To(Succeed())

			Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(project), project)).NotTo(HaveOccurred())
			Expect(project.Spec.Namespace).To(PointTo(Equal("garden-" + openStackUserInfo.Region + "-" + openStackUserInfo.ProjectID)))
		})

		It("should create a new role", func() {
			Expect(ReconcileGardenerProjectResources(ctx, fakeClient, cfg, openStackUserInfo)).To(Succeed())

			role := &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "persephone.sci.cloud.sap:openstack-project-kubernetes-admin",
					Namespace: "garden-" + openStackUserInfo.Region + "-" + openStackUserInfo.ProjectID,
				},
			}
			Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(role), role)).NotTo(HaveOccurred())
			Expect(role.Rules).To(HaveExactElements(
				rbacv1.PolicyRule{
					APIGroups: []string{gardenercorev1beta1.GroupName},
					Resources: []string{"shoots"},
					Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
				},
				rbacv1.PolicyRule{
					APIGroups: []string{gardenercorev1beta1.GroupName},
					Resources: []string{"shoots/adminkubeconfig"},
					Verbs:     []string{"create"},
				},
				rbacv1.PolicyRule{
					APIGroups: []string{""},
					Resources: []string{"configmaps", "secrets"},
					Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
				},
				rbacv1.PolicyRule{
					APIGroups: []string{""},
					Resources: []string{"resourcequotas"},
					Verbs:     []string{"get", "list"},
				},
			))
		})

		It("should reconcile an existing role", func() {
			role := &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "persephone.sci.cloud.sap:openstack-project-kubernetes-admin",
					Namespace: "garden-" + openStackUserInfo.Region + "-" + openStackUserInfo.ProjectID,
				},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{"old-api-group"},
						Resources: []string{"old-resource"},
						Verbs:     []string{"get"},
					},
				},
			}
			Expect(fakeClient.Create(ctx, role)).To(Succeed())

			Expect(ReconcileGardenerProjectResources(ctx, fakeClient, cfg, openStackUserInfo)).To(Succeed())

			Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(role), role)).NotTo(HaveOccurred())
			Expect(role.Rules).To(HaveExactElements(
				rbacv1.PolicyRule{
					APIGroups: []string{"core.gardener.cloud"},
					Resources: []string{"shoots"},
					Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
				},
				rbacv1.PolicyRule{
					APIGroups: []string{"core.gardener.cloud"},
					Resources: []string{"shoots/adminkubeconfig"},
					Verbs:     []string{"create"},
				},
				rbacv1.PolicyRule{
					APIGroups: []string{""},
					Resources: []string{"configmaps", "secrets"},
					Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
				},
				rbacv1.PolicyRule{
					APIGroups: []string{""},
					Resources: []string{"resourcequotas"},
					Verbs:     []string{"get", "list"},
				},
			))
		})

		It("should create a new rolebinding", func() {
			Expect(ReconcileGardenerProjectResources(ctx, fakeClient, cfg, openStackUserInfo)).To(Succeed())

			roleBinding := &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "persephone.sci.cloud.sap:openstack-project-kubernetes-admin",
					Namespace: "garden-" + openStackUserInfo.Region + "-" + openStackUserInfo.ProjectID,
				},
			}
			Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(roleBinding), roleBinding)).NotTo(HaveOccurred())
			Expect(roleBinding.Subjects).To(HaveExactElements(rbacv1.Subject{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Group",
				Name:     openStackUserInfo.ProjectID + ":kubernetes_admin",
			}))
			Expect(roleBinding.RoleRef).To(Equal(rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     "persephone.sci.cloud.sap:openstack-project-kubernetes-admin",
			}))
		})

		It("should not set sci.cloud.sap/* labels on the Project object", func() {
			Expect(ReconcileGardenerProjectResources(ctx, fakeClient, cfg, openStackUserInfo)).To(Succeed())

			projectName := GetGardenerProjectName(openStackUserInfo.Region, openStackUserInfo.ProjectID, cfg.LandscapeName)
			project := &gardenercorev1beta1.Project{ObjectMeta: metav1.ObjectMeta{Name: projectName}}
			Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(project), project)).NotTo(HaveOccurred())
			for k := range project.Labels {
				Expect(k).NotTo(HavePrefix("sci.cloud.sap/"), "Project.Labels must not contain OpenStack labels — they belong on the Namespace")
			}
		})

		It("should reconcile an existing rolebinding", func() {
			roleBinding := &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "persephone.sci.cloud.sap:openstack-project-kubernetes-admin",
					Namespace: "garden-" + openStackUserInfo.Region + "-" + openStackUserInfo.ProjectID,
				},
				Subjects: []rbacv1.Subject{{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "Group",
					Name:     "old-group",
				}},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "Role",
					Name:     "old-role",
				},
			}
			Expect(fakeClient.Create(ctx, roleBinding)).To(Succeed())

			Expect(ReconcileGardenerProjectResources(ctx, fakeClient, cfg, openStackUserInfo)).To(Succeed())

			Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(roleBinding), roleBinding)).NotTo(HaveOccurred())
			Expect(roleBinding.Subjects).To(HaveExactElements(rbacv1.Subject{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Group",
				Name:     openStackUserInfo.ProjectID + ":kubernetes_admin",
			}))
			Expect(roleBinding.RoleRef).To(Equal(rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     "persephone.sci.cloud.sap:openstack-project-kubernetes-admin",
			}))
		})
	})

	Describe("GetClusterUserName", func() {
		It("should return correctly formatted cluster user name", func() {
			landscapeName := "production"
			shootName := "my-shoot"
			projectID := "1234567890123456"

			Expect(GetClusterUserName(landscapeName, shootName, projectID)).To(Equal("gardener--production--my-shoot--1234567890123456"))
		})
	})

	Describe("NewApplicationCredentialName", func() {
		It("should return name with correct prefix", func() {
			Expect(NewApplicationCredentialName()).To(HavePrefix("shoot-"))
		})
	})

	Describe("ApplicationCredentialsInternalSecret", func() {
		var (
			shoot                 *gardenercorev1beta1.Shoot
			openStackUserInfo     OpenStackUserInfo
			applicationCredential *applicationcredentials.ApplicationCredential
		)

		BeforeEach(func() {
			shoot = &gardenercorev1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot",
					Namespace: "garden-1234567890123456",
				},
			}

			openStackUserInfo = OpenStackUserInfo{
				ProjectDomainID:   "domain-id-123",
				ProjectDomainName: "domain-name",
				ProjectID:         "1234567890123456",
				ProjectName:       "test-project",
				UserDomainID:      "user-domain-id",
				UserDomainName:    "user-domain-name",
				Region:            "region-1",
			}

			expiresAt := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)
			applicationCredential = &applicationcredentials.ApplicationCredential{
				ID:        "app-cred-id-123",
				Name:      "shoot-2025-12-15",
				Secret:    "super-secret-value",
				ExpiresAt: expiresAt,
			}
		})

		It("should create secret with correct name format", func() {
			secret := ApplicationCredentialsInternalSecret(shoot, openStackUserInfo, applicationCredential)
			Expect(secret.Name).To(Equal("1234567890123456--test-shoot--shoot-2025-12-15"))
		})

		It("should set correct namespace", func() {
			secret := ApplicationCredentialsInternalSecret(shoot, openStackUserInfo, applicationCredential)
			Expect(secret.Namespace).To(Equal(shoot.Namespace))
		})

		It("should set expiration annotation", func() {
			secret := ApplicationCredentialsInternalSecret(shoot, openStackUserInfo, applicationCredential)
			Expect(secret.Annotations).To(HaveKeyWithValue("secret.persephone.sci.cloud.sap/expires-at", "2025-12-31T23:59:59Z"))
		})

		It("should set all required labels", func() {
			secret := ApplicationCredentialsInternalSecret(shoot, openStackUserInfo, applicationCredential)
			Expect(secret.Labels).To(HaveKeyWithValue("sci.cloud.sap/domain-id", "domain-id-123"))
			Expect(secret.Labels).To(HaveKeyWithValue("sci.cloud.sap/domain-name", "domain-name"))
			Expect(secret.Labels).To(HaveKeyWithValue("sci.cloud.sap/project-id", "1234567890123456"))
			Expect(secret.Labels).To(HaveKeyWithValue("sci.cloud.sap/project-name", "test-project"))
			Expect(secret.Labels).To(HaveKeyWithValue("sci.cloud.sap/region", "region-1"))
			Expect(secret.Labels).To(HaveKeyWithValue("persephone.sci.cloud.sap/shoot-name", "test-shoot"))
		})

		It("should set immutable to true", func() {
			secret := ApplicationCredentialsInternalSecret(shoot, openStackUserInfo, applicationCredential)
			Expect(secret.Immutable).To(PointTo(BeTrue()))
		})

		It("should set correct string data fields", func() {
			secret := ApplicationCredentialsInternalSecret(shoot, openStackUserInfo, applicationCredential)
			Expect(secret.StringData).To(HaveKeyWithValue("domainName", "domain-name"))
			Expect(secret.StringData).To(HaveKeyWithValue("tenantName", "test-project"))
			Expect(secret.StringData).To(HaveKeyWithValue("applicationCredentialID", "app-cred-id-123"))
			Expect(secret.StringData).To(HaveKeyWithValue("applicationCredentialSecret", "super-secret-value"))
		})
	})

	Describe("CredentialsBindingForSecret", func() {
		var (
			shoot          *gardenercorev1beta1.Shoot
			internalSecret *gardenercorev1beta1.InternalSecret
		)

		BeforeEach(func() {
			shoot = &gardenercorev1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot",
					Namespace: "garden-1234567890123456",
				},
			}

			internalSecret = &gardenercorev1beta1.InternalSecret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-1234567890123456-shoot-2025-12-15",
					Namespace: "garden-1234567890123456",
				},
			}
		})

		It("should create credentials binding with matching internalSecret name", func() {
			binding := CredentialsBindingForSecret(shoot, internalSecret)
			Expect(binding.Name).To(Equal(internalSecret.Name))
		})

		It("should set namespace to shoot namespace", func() {
			binding := CredentialsBindingForSecret(shoot, internalSecret)
			Expect(binding.Namespace).To(Equal(shoot.Namespace))
		})

		It("should set provider type to openstack", func() {
			binding := CredentialsBindingForSecret(shoot, internalSecret)
			Expect(binding.Provider.Type).To(Equal("openstack"))
		})

		It("should set correct credentials reference", func() {
			binding := CredentialsBindingForSecret(shoot, internalSecret)
			Expect(binding.CredentialsRef).To(Equal(corev1.ObjectReference{
				APIVersion: "core.gardener.cloud/v1beta1",
				Kind:       "InternalSecret",
				Name:       internalSecret.Name,
				Namespace:  shoot.Namespace,
			}))
		})
	})
})
