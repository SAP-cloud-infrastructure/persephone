// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
// SPDX-License-Identifier: Apache-2.0

package internalsecret_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/utils"
	. "github.com/gardener/gardener/pkg/utils/test/matchers"
	gardenerenvtest "github.com/gardener/gardener/test/envtest"
	"github.com/go-logr/logr"
	"github.com/gophercloud/gophercloud/v2"
	th "github.com/gophercloud/gophercloud/v2/testhelper"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/rest"
	clocktesting "k8s.io/utils/clock/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controllerconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/sap-cloud-infrastructure/persephone/internal/controller/internalsecret"
	"github.com/sap-cloud-infrastructure/persephone/internal/openstack"
)

func TestInternalSecret(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Test Integration Controller InternalSecret Suite")
}

const (
	testID = "internal-secret-controller-test"
)

var (
	ctx = context.Background()
	log logr.Logger

	testEnv        *gardenerenvtest.GardenerTestEnvironment
	testRestConfig *rest.Config
	testClient     client.Client
	testRunID      = utils.ComputeSHA256Hex([]byte(uuid.NewUUID()))[:8]

	testNamespace *corev1.Namespace
	mgrClient     client.Client
	fakeClock     *clocktesting.FakeClock
)

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true}), zap.WriteTo(GinkgoWriter)))
	log = logf.Log.WithName(testID)

	By("Start test environment")
	testEnv = &gardenerenvtest.GardenerTestEnvironment{
		Environment: &envtest.Environment{},
		GardenerAPIServer: &gardenerenvtest.GardenerAPIServer{
			Args: []string{"--disable-admission-plugins=DeletionConfirmation,ResourceReferenceManager,ExtensionValidator,ShootDNS,ShootQuotaValidator,ShootTolerationRestriction,ShootValidator,ShootMutator"},
		},
	}

	var err error
	testRestConfig, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(testRestConfig).NotTo(BeNil())

	DeferCleanup(func() {
		By("Stop target environment")
		Expect(testEnv.Stop()).To(Succeed())
	})

	By("Create test client")
	testClient, err = client.New(testRestConfig, client.Options{Scheme: kubernetes.GardenScheme})
	Expect(err).NotTo(HaveOccurred())

	By("Create test Namespace")
	testNamespace = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "garden-" + testRunID,
			Labels: map[string]string{
				testID: testRunID,
			},
		},
	}
	Expect(testClient.Create(ctx, testNamespace)).To(Succeed())
	log.Info("Created Namespace for test", "namespaceName", testNamespace.Name)

	DeferCleanup(func() {
		By("Delete test Namespace")
		Expect(testClient.Delete(ctx, testNamespace)).To(Or(Succeed(), BeNotFoundError()))
	})

	By("Setup managers")
	mgr, err := manager.New(testRestConfig, manager.Options{
		Scheme:     kubernetes.GardenScheme,
		Metrics:    metricsserver.Options{BindAddress: "0"},
		Controller: controllerconfig.Controller{SkipNameValidation: new(true)},
	})
	Expect(err).NotTo(HaveOccurred())
	mgrClient = mgr.GetClient()

	By("Register controller")
	fakeClock = clocktesting.NewFakeClock(time.Now())
	Expect((&internalsecret.Reconciler{
		Client: mgrClient,
		OpenStackClientSet: openstack.RegionOpenStackClientSet{
			"test-region": newOpenStackClientSet(),
		},
		Clock:         fakeClock,
		LandscapeName: "test-landscape",
	}).AddToManager(mgr, 1)).To(Succeed())

	By("Start manager")
	mgrContext, mgrCancel := context.WithCancel(ctx)

	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(mgrContext)).To(Succeed())
	}()

	DeferCleanup(func() {
		By("Stop manager")
		mgrCancel()
	})
})

func newOpenStackClientSet() *openstack.ClientSet {
	fakeServer := th.SetupHTTP()

	// Handle root endpoint (version discovery)
	fakeServer.Mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{
  "versions": {
    "values": [
      {
        "status": "stable",
        "id": "v3.0",
        "links": [
          {
            "href": "`+fakeServer.Endpoint()+`v3/",
            "rel": "self"
          }
        ]
      }
    ]
  }
}`)
	})

	// Handle v3 base endpoint (identity service discovery)
	fakeServer.Mux.HandleFunc("/v3/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{
  "version": {
    "status": "stable",
    "id": "v3.0",
    "links": [
      {
        "href": "`+fakeServer.Endpoint()+`v3/",
        "rel": "self"
      }
    ]
  }
}`)
	})

	// Handle authentication requests
	fakeServer.Mux.HandleFunc("/v3/auth/tokens", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Subject-Token", "fake-auth-token")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{
  "token": {
    "methods": ["password"],
    "expires_at": "`+time.Now().Add(24*time.Hour).Format(time.RFC3339)+`",
    "catalog": [
      {
        "type": "identity",
        "id": "identity-service-id",
        "name": "keystone",
        "endpoints": [
          {
            "id": "identity-endpoint-id",
            "url": "`+fakeServer.Endpoint()+`v3/",
            "interface": "public",
            "region": "test-region",
            "region_id": "test-region"
          }
        ]
      },
      {
        "type": "network",
        "id": "network-service-id",
        "name": "neutron",
        "endpoints": [
          {
            "id": "network-endpoint-id",
            "url": "`+fakeServer.Endpoint()+`v2.0/",
            "interface": "public",
            "region": "test-region",
            "region_id": "test-region"
          }
        ]
      }
    ]
  }
}`)
	})

	// Handle network v2.0 endpoint (version discovery)
	fakeServer.Mux.HandleFunc("/v2.0/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{
  "version": {
    "status": "CURRENT",
    "id": "v2.0",
    "links": [
      {
        "href": "`+fakeServer.Endpoint()+`v2.0/",
        "rel": "self"
      }
    ]
  }
}`)
	})

	// Handle GET requests - list roles by name
	fakeServer.Mux.HandleFunc("/v3/roles", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			return
		}

		// Check if this is a role list request with name filter
		roleName := r.URL.Query().Get("name")
		if roleName != "" {
			// Return role if name matches expected roles
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{
  "roles": [
    {
      "id": "fake-role-id-`+roleName+`",
      "name": "`+roleName+`",
      "domain_id": "test-domain-id"
    }
  ]
}`)
			return
		}

		// Empty list if no name filter
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"roles": []}`)
	})

	// Handle GET requests - list users by name
	fakeServer.Mux.HandleFunc("/v3/users", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			return
		}

		// Check if this is a user list request with name filter
		userName := r.URL.Query().Get("name")
		if userName != "" {
			// Return user if name matches expected cluster user pattern
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{
  "users": [
    {
      "id": "fake-cluster-user-id",
      "name": "`+userName+`",
      "domain_id": "test-domain-id",
      "default_project_id": "test-project-id",
      "enabled": true,
      "description": "Gardener customer shoot service user"
    }
  ]
}`)
			return
		}

		// Empty list if no name filter
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"users": []}`)
	})

	// Handle role assignments - must come before the catch-all users/ handler
	fakeServer.Mux.HandleFunc("/v3/projects/", func(w http.ResponseWriter, r *http.Request) {
		// Handle role assignments: PUT /v3/projects/{project_id}/users/{user_id}/roles/{role_id}
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// Handle role revocations: DELETE /v3/projects/{project_id}/users/{user_id}/roles/{role_id}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	// Catch-all handler for user-related requests (creation, deletion, application credentials)
	fakeServer.Mux.HandleFunc("/v3/users/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.Method {
		case http.MethodPut, http.MethodPatch:
			// Create or update user
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{
  "user": {
    "id": "fake-cluster-user-id",
    "name": "fake-user",
    "domain_id": "test-domain-id",
    "default_project_id": "test-project-id",
    "enabled": true
  }
}`)
		case http.MethodGet:
			// List application credentials or get user
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{
  "links": {
    "self": "`+fakeServer.Endpoint()+`v3/users/fake-cluster-user-id/application_credentials",
    "previous": null,
    "next": null
  },
  "application_credentials": []
}`)
		case http.MethodPost:
			// Create application credential
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
    "expires_at": "`+time.Now().Add(24*60*time.Hour).UTC().Format("2006-01-02T15:04:05.000000")+`",
    "secret": "fake-secret-value",
    "project_id": "test-project-id",
    "unrestricted": false,
    "links": {
      "self": "`+fakeServer.Endpoint()+`v3/users/fake-cluster-user-id/application_credentials/fake-new-app-cred-id"
    }
  }
}`)
		case http.MethodDelete:
			// Delete user or application credential
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	return &openstack.ClientSet{
		IdentityClient: &gophercloud.ServiceClient{
			ProviderClient: &gophercloud.ProviderClient{},
			Endpoint:       fakeServer.Endpoint() + "v3/",
		},
		ProviderClient: &gophercloud.ProviderClient{
			IdentityEndpoint: fakeServer.Endpoint(),
		},
		DomainID: "test-domain-id",
	}
}
