package shootadmission_test

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	gardenersecurityv1alpha1 "github.com/gardener/gardener/pkg/apis/security/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/utils/test"
	. "github.com/gardener/gardener/pkg/utils/test/matchers"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/users"
	th "github.com/gophercloud/gophercloud/v2/testhelper"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sap-cloud-infrastructure/persephone/internal/openstack"
	"github.com/sap-cloud-infrastructure/persephone/internal/webhook/shootadmission"
)

var _ = Describe("ShootAdmission tests", func() {
	var shoot *gardenercorev1beta1.Shoot

	BeforeEach(OncePerOrdered, func() {
		shoot = &gardenercorev1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-shoot",
				Namespace: testNamespace.Name,
				Labels:    map[string]string{testID: testRunID},
			},
			Spec: gardenercorev1beta1.ShootSpec{
				Provider: gardenercorev1beta1.Provider{
					Workers: []gardenercorev1beta1.Worker{{
						Name:    "cpu-worker",
						Minimum: 3,
						Maximum: 3,
						Machine: gardenercorev1beta1.Machine{
							Type: "large",
							Image: &gardenercorev1beta1.ShootMachineImage{
								Name:    "some-image",
								Version: new("1.0.0"),
							},
						},
					}},
				},
				Kubernetes: gardenercorev1beta1.Kubernetes{Version: "1.31.1"},
			},
		}
	})

	It("should fail creating the Shoot when OpenStack user info is missing because required mutations are not performed", func() {
		Expect(testClient.Create(ctx, shoot)).To(BeInvalidError())
	})

	It("should fail mutating the Shoot when region is not supported", func() {
		openStackUserClient := newOpenStackUserClient("some-region")
		Expect(openStackUserClient.Create(ctx, shoot)).To(MatchError(ContainSubstring(`region "some-region" not supported`)))
	})

	It("should fail mutating the Shoot when there are no external networks", func() {
		DeferCleanup(test.WithVar(&shootadmission.NewClusterUserClientSet, func(_ context.Context, adminClient *openstack.ClientSet, username, projectID string) (*openstack.ClusterUserClientSet, error) {
			return &openstack.ClusterUserClientSet{
				User: &users.User{ID: "fake-user-id"},
				ClientSet: openstack.ClientSet{
					IdentityClient: newFakeIdentityClient(),
					NetworkClient:  newFakeNetworkClient(0),
				},
			}, nil
		}))

		openStackUserClient := newOpenStackUserClient(supportedRegion)
		Expect(openStackUserClient.Create(ctx, shoot)).To(MatchError(ContainSubstring("unable to auto-detect external network")))
	})

	It("should fail mutating the Shoot when there are multiple external networks", func() {
		DeferCleanup(test.WithVar(&shootadmission.NewClusterUserClientSet, func(_ context.Context, adminClient *openstack.ClientSet, username, projectID string) (*openstack.ClusterUserClientSet, error) {
			return &openstack.ClusterUserClientSet{
				User: &users.User{ID: "fake-user-id"},
				ClientSet: openstack.ClientSet{
					IdentityClient: newFakeIdentityClient(),
					NetworkClient:  newFakeNetworkClient(2),
				},
			}, nil
		}))

		openStackUserClient := newOpenStackUserClient(supportedRegion)
		Expect(openStackUserClient.Create(ctx, shoot)).To(MatchError(ContainSubstring("unable to auto-detect external network")))
	})

	When("defaulting prerequisites are all met", Ordered, func() {
		var internalSecret *gardenercorev1beta1.InternalSecret

		It("should succeed with Shoot creation", func() {
			DeferCleanup(test.WithVar(&shootadmission.NewClusterUserClientSet, func(_ context.Context, adminClient *openstack.ClientSet, username, projectID string) (*openstack.ClusterUserClientSet, error) {
				return &openstack.ClusterUserClientSet{
					User: &users.User{ID: "fake-user-id"},
					ClientSet: openstack.ClientSet{
						IdentityClient: newFakeIdentityClient(),
						NetworkClient:  newFakeNetworkClient(1),
					},
				}, nil
			}))

			DeferCleanup(func() {
				Expect(testClient.Delete(ctx, shoot)).To(Or(Succeed(), BeNotFoundError()))
			})

			openStackUserClient := newOpenStackUserClient(supportedRegion)
			Expect(openStackUserClient.Create(ctx, shoot)).To(Succeed())
		})

		It("should have mutated the Shoot", func() {
			Expect(shoot.Spec.CloudProfile).To(Equal(&gardenercorev1beta1.CloudProfileReference{Kind: "CloudProfile", Name: "openstack"}))
			Expect(shoot.Spec.CredentialsBindingName).To(PointTo(Equal("project-id--" + shoot.Name + "--shoot-timestamp")))
			Expect(shoot.Spec.Networking).NotTo(BeNil())
			Expect(shoot.Spec.Networking.Type).To(PointTo(Equal("calico")))
			Expect(shoot.Spec.Networking.Pods).To(PointTo(Equal("10.44.0.0/16")))
			Expect(shoot.Spec.Networking.Services).To(PointTo(Equal("10.45.0.0/16")))
			Expect(shoot.Spec.Networking.Nodes).To(PointTo(Equal("10.180.24.0/24")))
			Expect(shoot.Spec.Provider.Type).To(Equal("openstack"))
			Expect(shoot.Spec.Region).To(Equal(supportedRegion))

			Expect(shoot.Spec.Provider.ControlPlaneConfig).NotTo(BeNil())
			Expect(string(shoot.Spec.Provider.ControlPlaneConfig.Raw)).To(Equal(`{"apiVersion":"openstack.provider.extensions.gardener.cloud/v1alpha1","kind":"ControlPlaneConfig","loadBalancerProvider":"f5"}`))

			Expect(shoot.Spec.Provider.InfrastructureConfig).NotTo(BeNil())
			Expect(string(shoot.Spec.Provider.InfrastructureConfig.Raw)).To(Equal(`{"apiVersion":"openstack.provider.extensions.gardener.cloud/v1alpha1","floatingPoolName":"fake-external-network-0","kind":"InfrastructureConfig","networks":{"worker":"","workers":"10.180.24.0/24"}}`))
		})

		It("should have created an InternalSecret with application credentials for the Shoot", func() {
			internalSecretList := &gardenercorev1beta1.InternalSecretList{}
			Expect(testClient.List(ctx, internalSecretList, client.InNamespace(shoot.Namespace), client.MatchingLabels{"persephone.sci.cloud.sap/shoot-name": shoot.Name})).To(Succeed())
			Expect(internalSecretList.Items).To(HaveLen(1))
			internalSecret = &internalSecretList.Items[0]

			Expect(internalSecret.Labels).To(And(
				HaveKeyWithValue("sci.cloud.sap/region", supportedRegion),
				HaveKeyWithValue("persephone.sci.cloud.sap/shoot-name", shoot.Name),
				HaveKeyWithValue("sci.cloud.sap/domain-name", "project-domain-name"),
				HaveKeyWithValue("sci.cloud.sap/project-name", "project-name"),
				HaveKeyWithValue("sci.cloud.sap/project-id", "project-id"),
			))
			Expect(internalSecret.Type).To(Equal(corev1.SecretTypeOpaque))
			Expect(internalSecret.Data).To(Equal(map[string][]byte{
				"domainName":                  []byte("project-domain-name"),
				"tenantName":                  []byte("project-name"),
				"applicationCredentialID":     []byte("fake-new-app-cred-id"),
				"applicationCredentialSecret": []byte("fake-secret-value"),
			}))
		})

		It("should have created a CredentialsBinding targeting the created application credentials for the Shoot", func() {
			credentialsBindingList := &gardenersecurityv1alpha1.CredentialsBindingList{}
			Expect(testClient.List(ctx, credentialsBindingList, client.InNamespace(shoot.Namespace))).To(Succeed())
			Expect(credentialsBindingList.Items).To(HaveLen(1))
			credentialsBinding := credentialsBindingList.Items[0]

			Expect(credentialsBinding.Provider).To(Equal(gardenersecurityv1alpha1.CredentialsBindingProvider{Type: "openstack"}))
			Expect(credentialsBinding.CredentialsRef).To(Equal(corev1.ObjectReference{
				APIVersion: gardenercorev1beta1.SchemeGroupVersion.String(),
				Kind:       "InternalSecret",
				Name:       internalSecret.Name,
				Namespace:  internalSecret.Namespace,
			}))
		})
	})

	When("Shoot spec contains user-provided configuration", Ordered, func() {
		var (
			podNetworkCIDR     = "10.251.0.0/16"
			serviceNetworkCIDR = "10.252.0.0/16"
			nodeNetworkCIDR    = "10.253.0.0/16"
			networkType        = "custom-network"
			floatingPoolName   = "foo"
		)

		It("should succeed with Shoot creation", func() {
			DeferCleanup(test.WithVar(&shootadmission.NewClusterUserClientSet, func(_ context.Context, adminClient *openstack.ClientSet, username, projectID string) (*openstack.ClusterUserClientSet, error) {
				return &openstack.ClusterUserClientSet{
					User: &users.User{ID: "fake-user-id"},
					ClientSet: openstack.ClientSet{
						IdentityClient: newFakeIdentityClient(),
						NetworkClient:  newFakeNetworkClient(1),
					},
				}, nil
			}))

			// these values are not overridden by the webhook handler
			shoot.Spec.Networking = &gardenercorev1beta1.Networking{
				Type:     &networkType,
				Pods:     &podNetworkCIDR,
				Services: &serviceNetworkCIDR,
				Nodes:    &nodeNetworkCIDR,
			}
			shoot.Spec.Provider.ControlPlaneConfig = &runtime.RawExtension{Raw: []byte(`{
  "apiVersion":"openstack.provider.extensions.gardener.cloud/v1alpha1",
  "kind":"ControlPlaneConfig"
}`)}
			shoot.Spec.Provider.InfrastructureConfig = &runtime.RawExtension{Raw: []byte(`{
  "apiVersion":"openstack.provider.extensions.gardener.cloud/v1alpha1",
  "kind":"InfrastructureConfig",
  "floatingPoolName":"` + floatingPoolName + `",
  "networks":{"workers":"` + nodeNetworkCIDR + `"}
}`)}

			// these values are overridden by the webhook handler
			shoot.Spec.CloudProfile = &gardenercorev1beta1.CloudProfileReference{Kind: "CloudProfile", Name: "my-cloudprofile-overwrite-me"}
			shoot.Spec.CredentialsBindingName = new("my-credentials-binding-name-overwrite-me")
			shoot.Spec.Provider.Type = "my-provider-type-overwrite-me"
			shoot.Spec.Region = "my-region-overwrite-me"

			DeferCleanup(func() {
				Expect(testClient.Delete(ctx, shoot)).To(Or(Succeed(), BeNotFoundError()))
			})

			openStackUserClient := newOpenStackUserClient(supportedRegion)
			Expect(openStackUserClient.Create(ctx, shoot)).To(Succeed())
		})

		It("should not override user-provided values in the Shoot", func() {
			Expect(shoot.Spec.CloudProfile).To(Equal(&gardenercorev1beta1.CloudProfileReference{Kind: "CloudProfile", Name: "openstack"}))
			Expect(shoot.Spec.CredentialsBindingName).To(PointTo(Equal("project-id--" + shoot.Name + "--shoot-timestamp")))
			Expect(shoot.Spec.Networking).NotTo(BeNil())
			Expect(shoot.Spec.Networking.Type).To(PointTo(Equal(networkType)))
			Expect(shoot.Spec.Networking.Pods).To(PointTo(Equal(podNetworkCIDR)))
			Expect(shoot.Spec.Networking.Services).To(PointTo(Equal(serviceNetworkCIDR)))
			Expect(shoot.Spec.Networking.Nodes).To(PointTo(Equal(nodeNetworkCIDR)))
			Expect(shoot.Spec.Provider.Type).To(Equal("openstack"))
			Expect(shoot.Spec.Region).To(Equal(supportedRegion))

			Expect(shoot.Spec.Provider.ControlPlaneConfig).NotTo(BeNil())
			Expect(string(shoot.Spec.Provider.ControlPlaneConfig.Raw)).To(Equal(`{"apiVersion":"openstack.provider.extensions.gardener.cloud/v1alpha1","kind":"ControlPlaneConfig"}`))

			Expect(shoot.Spec.Provider.InfrastructureConfig).NotTo(BeNil())
			Expect(string(shoot.Spec.Provider.InfrastructureConfig.Raw)).To(Equal(`{"apiVersion":"openstack.provider.extensions.gardener.cloud/v1alpha1","floatingPoolName":"` + floatingPoolName + `","kind":"InfrastructureConfig","networks":{"worker":"","workers":"` + nodeNetworkCIDR + `"}}`))
		})
	})

	When("existing Shoot is updated", Ordered, func() {
		BeforeAll(func() {
			DeferCleanup(test.WithVar(&shootadmission.NewClusterUserClientSet, func(_ context.Context, adminClient *openstack.ClientSet, username, projectID string) (*openstack.ClusterUserClientSet, error) {
				return &openstack.ClusterUserClientSet{
					User: &users.User{ID: "fake-user-id"},
					ClientSet: openstack.ClientSet{
						IdentityClient: newFakeIdentityClient(),
						NetworkClient:  newFakeNetworkClient(1),
					},
				}, nil
			}))
		})

		AfterAll(func() {
			Expect(testClient.Delete(ctx, shoot)).To(Or(Succeed(), BeNotFoundError()))
		})

		It("should succeed with Shoot creation", func() {
			openStackUserClient := newOpenStackUserClient(supportedRegion)
			Expect(openStackUserClient.Create(ctx, shoot)).To(Succeed())

			internalSecretList := &gardenercorev1beta1.InternalSecretList{}
			Expect(testClient.List(ctx, internalSecretList, client.InNamespace(shoot.Namespace), client.MatchingLabels{"persephone.sci.cloud.sap/shoot-name": shoot.Name})).To(Succeed())
			Expect(internalSecretList.Items).To(HaveLen(1))

			credentialsBindingList := &gardenersecurityv1alpha1.CredentialsBindingList{}
			Expect(testClient.List(ctx, credentialsBindingList, client.InNamespace(shoot.Namespace))).To(Succeed())
			Expect(credentialsBindingList.Items).To(HaveLen(1))
		})

		It("should not create another Secret or CredentialsBinding when an existing Shoot is updated", func() {
			oldCredentialsBindingName := shoot.Spec.CredentialsBindingName

			// UPDATE bypasses the webhook entirely; use testClient (no OpenStack credentials needed)
			metav1.SetMetaDataAnnotation(&shoot.ObjectMeta, "foo", "bar")
			Expect(testClient.Update(ctx, shoot)).To(Succeed())
			Expect(shoot.Spec.CredentialsBindingName).To(Equal(oldCredentialsBindingName))

			internalSecretList := &gardenercorev1beta1.InternalSecretList{}
			Expect(testClient.List(ctx, internalSecretList, client.InNamespace(shoot.Namespace), client.MatchingLabels{"persephone.sci.cloud.sap/shoot-name": shoot.Name})).To(Succeed())
			Expect(internalSecretList.Items).To(HaveLen(1))

			credentialsBindingList := &gardenersecurityv1alpha1.CredentialsBindingList{}
			Expect(testClient.List(ctx, credentialsBindingList, client.InNamespace(shoot.Namespace))).To(Succeed())
			Expect(credentialsBindingList.Items).To(HaveLen(1))
		})
	})

	When("creating a workerless Shoot", Ordered, func() {
		var workerlessShoot *gardenercorev1beta1.Shoot

		BeforeAll(func() {
			DeferCleanup(test.WithVar(&shootadmission.NewClusterUserClientSet, func(_ context.Context, adminClient *openstack.ClientSet, username, projectID string) (*openstack.ClusterUserClientSet, error) {
				return &openstack.ClusterUserClientSet{
					User: &users.User{ID: "fake-user-id"},
					ClientSet: openstack.ClientSet{
						IdentityClient: newFakeIdentityClient(),
						NetworkClient:  newFakeNetworkClient(1),
					},
				}, nil
			}))

			workerlessShoot = &gardenercorev1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workerless-shoot",
					Namespace: testNamespace.Name,
					Labels:    map[string]string{testID: testRunID},
				},
				Spec: gardenercorev1beta1.ShootSpec{
					Provider: gardenercorev1beta1.Provider{
						Workers: []gardenercorev1beta1.Worker{}, // Empty workers list makes it workerless
					},
					Kubernetes: gardenercorev1beta1.Kubernetes{Version: "1.31.1"},
				},
			}
		})

		AfterAll(func() {
			Expect(testClient.Delete(ctx, workerlessShoot)).To(Or(Succeed(), BeNotFoundError()))
		})

		It("should succeed with workerless Shoot creation", func() {
			openStackUserClient := newOpenStackUserClient(supportedRegion)
			Expect(openStackUserClient.Create(ctx, workerlessShoot)).To(Succeed())
		})

		It("should have mutated basic fields but not infrastructure/networking/credentials", func() {
			Expect(workerlessShoot.Spec.CloudProfile).To(Equal(&gardenercorev1beta1.CloudProfileReference{Kind: "CloudProfile", Name: "openstack"}))
			Expect(workerlessShoot.Spec.Provider.Type).To(Equal("openstack"))
			Expect(workerlessShoot.Spec.Region).To(Equal(supportedRegion))

			// These fields should NOT be set for workerless shoots
			Expect(workerlessShoot.Spec.CredentialsBindingName).To(BeNil())
			Expect(workerlessShoot.Spec.Provider.InfrastructureConfig).To(BeNil())
			Expect(workerlessShoot.Spec.Provider.ControlPlaneConfig).To(BeNil())
			Expect(workerlessShoot.Spec.Networking.Type).To(BeNil())
			Expect(workerlessShoot.Spec.Networking.Pods).To(BeNil())
			Expect(workerlessShoot.Spec.Networking.Nodes).To(BeNil())
		})

		It("should not have created an InternalSecret or CredentialsBinding for workerless Shoot", func() {
			internalSecretList := &gardenercorev1beta1.InternalSecretList{}
			Expect(testClient.List(ctx, internalSecretList, client.InNamespace(workerlessShoot.Namespace), client.MatchingLabels{"persephone.sci.cloud.sap/shoot-name": workerlessShoot.Name})).To(Succeed())
			Expect(internalSecretList.Items).To(BeEmpty())

			// Verify the CredentialsBindingName is not set on the workerless shoot spec
			// (Existing CredentialsBindings from other shoots in the same namespace should not affect this test)
			Expect(workerlessShoot.Spec.CredentialsBindingName).To(BeNil())
		})
	})
})

func newOpenStackUserClient(region string) client.Client {
	GinkgoHelper()

	userConfig := rest.CopyConfig(testRestConfig)
	userConfig.Impersonate = rest.ImpersonationConfig{
		UserName: "openstack-user",
		Groups:   []string{"system:masters"},
		Extra: map[string][]string{
			"project_domain_id":   {"project-domain-id"},
			"project_domain_name": {"project-domain-name"},
			"project_name":        {"project-name"},
			"project_id":          {"project-id"},
			"user_domain_id":      {"user-domain-id"},
			"user_domain_name":    {"user-domain-name"},
			"region":              {region},
		},
	}

	openStackUserClient, err := client.New(userConfig, client.Options{Scheme: kubernetes.GardenScheme})
	Expect(err).NotTo(HaveOccurred())
	return openStackUserClient
}

func newFakeNetworkClient(numberOfExternalNetworks int) *gophercloud.ServiceClient {
	newNetwork := func(ordinal int) string {
		return `{
  "id": "external-net-` + strconv.Itoa(ordinal) + `",
  "name": "fake-external-network-` + strconv.Itoa(ordinal) + `",
  "router:external": true,
  "shared": true,
  "status": "ACTIVE"
}`
	}

	var networks []string
	for i := range numberOfExternalNetworks {
		networks = append(networks, newNetwork(i))
	}

	fakeServer := th.SetupHTTP()

	fakeServer.Mux.HandleFunc("/v2.0/networks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"networks": [`+strings.Join(networks, ",")+`]}`)
	})

	return &gophercloud.ServiceClient{
		ProviderClient: &gophercloud.ProviderClient{},
		Endpoint:       fakeServer.Endpoint() + "v2.0/",
	}
}

func newFakeIdentityClient() *gophercloud.ServiceClient {
	fakeServer := th.SetupHTTP()

	// Handle GET requests - list application credentials
	fakeServer.Mux.HandleFunc("GET /v3/users/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{
  "links": {
    "self": "`+fakeServer.Endpoint()+`v3/users/fake-user-id/application_credentials",
    "previous": null,
    "next": null
  },
  "application_credentials": [
    {
      "id": "fake-app-cred-id",
      "name": "test-shoot-project-id-credential",
      "description": "Application credential for shoot",
      "roles": [
        {
          "id": "role-id-1",
          "name": "member"
        }
      ],
      "expires_at": "2030-12-31T23:59:59.000000",
      "project_id": "project-id",
      "unrestricted": false,
      "links": {
        "self": "`+fakeServer.Endpoint()+`v3/users/fake-user-id/application_credentials/fake-app-cred-id"
      }
    }
  ]
}`)
	})

	// Handle POST requests - create application credential
	fakeServer.Mux.HandleFunc("POST /v3/users/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{
  "application_credential": {
    "id": "fake-new-app-cred-id",
    "name": "shoot-timestamp",
    "description": null,
    "roles": [
      {
        "id": "role-id-1",
        "name": "member"
      }
    ],
    "expires_at": "2030-12-31T23:59:59.000000",
    "secret": "fake-secret-value",
    "project_id": "project-id",
    "unrestricted": false,
    "links": {
      "self": "`+fakeServer.Endpoint()+`v3/users/fake-user-id/application_credentials/fake-new-app-cred-id"
    }
  }
}`)
	})

	return &gophercloud.ServiceClient{
		ProviderClient: &gophercloud.ProviderClient{},
		Endpoint:       fakeServer.Endpoint() + "v3/",
	}
}
