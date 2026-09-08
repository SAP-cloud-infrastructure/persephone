package token_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	. "github.com/gardener/gardener/pkg/utils/test/matchers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/types"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sap-cloud-infrastructure/persephone/internal/config"
	persephone "github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
)

var _ = Describe("Token tests", func() {
	const (
		region            = "test-region"
		keystoneToken     = "keystone-token"
		role1             = "role1"
		role2             = "role2"
		projectID         = "projectid"
		projectName       = "projectname"
		projectDomainID   = "projectdomainid"
		projectDomainName = "projectdomainname"
		userID            = "userid"
		userName          = "username"
		userDomainID      = "userdomainid"
		userDomainName    = "userdomainname"

		validKeystoneResponse = `{
  "token": {
    "roles": [
      { "id": "role1id", "name": "` + role1 + `" },
      { "id": "role2id", "name": "` + role2 + `" }
    ],
    "project": {
      "id": "` + projectID + `",
      "name": "` + projectName + `",
      "domain": { "id": "` + projectDomainID + `", "name": "` + projectDomainName + `" }
    },
    "user": {
      "id": "` + userID + `",
      "name": "` + userName + `",
      "domain": { "id": "` + userDomainID + `", "name": "` + userDomainName + `" }
    }
  }
}`
	)

	BeforeEach(func() {
		Expect(kubeAPIServerLogs.Clear()).To(Succeed())
	})

	It("should complain about invalid token format", func() {
		Expect(newOpenStackUserClient("bearer-token").List(ctx, &corev1.NamespaceList{})).To(BeUnauthorizedError())
		Eventually(kubeAPIServerLogs).Should(gbytes.Say("invalid token format"))
	})

	When("bearer token format is valid", func() {
		var openStackUserClient client.Client

		BeforeEach(func() {
			openStackUserClient = newOpenStackUserClient(region + ":" + keystoneToken)
		})

		Describe("missing keystone information", func() {
			It("should complain about missing region in config", func() {
				Expect(openStackUserClient.List(ctx, &corev1.NamespaceList{})).To(BeUnauthorizedError())
				Eventually(kubeAPIServerLogs).Should(gbytes.Say("region not found in config"))
			})

			When("region exists but identity endpoint is empty", func() {
				BeforeEach(func() {
					webhookHandler.Config.Regions = map[string]config.RegionalConfig{region: {}}
				})

				It("should complain about empty identity endpoint", func() {
					Expect(openStackUserClient.List(ctx, &corev1.NamespaceList{})).To(BeUnauthorizedError())
					Eventually(kubeAPIServerLogs).Should(gbytes.Say("region found, but identity endpoint is empty"))
				})
			})
		})

		Describe("keystone information provided", func() {
			BeforeEach(func() {
				fakeKeystoneServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/v3/auth/tokens":
						w.Header().Set("X-Subject-Token", keystoneToken)
						w.WriteHeader(http.StatusOK)
						_, err := io.WriteString(w, validKeystoneResponse)
						Expect(err).NotTo(HaveOccurred())
					default:
						http.NotFound(w, r)
					}
				}))
				DeferCleanup(func() { fakeKeystoneServer.Close() })

				webhookHandler.Config.Regions = map[string]config.RegionalConfig{region: {IdentityEndpoint: fakeKeystoneServer.URL}}
			})

			It("should successfully authenticate the user", func() {
				// User is authenticated, but has no RBAC privileges - that's good enough for this test
				Expect(openStackUserClient.List(ctx, &corev1.NamespaceList{})).To(BeForbiddenError())

				selfSubjectReview := &authenticationv1.SelfSubjectReview{}
				Expect(openStackUserClient.Create(ctx, selfSubjectReview)).To(Succeed())
				Expect(selfSubjectReview.Status.UserInfo.Username).To(Equal(userName))
				Expect(selfSubjectReview.Status.UserInfo.UID).To(Equal(userID))
				Expect(selfSubjectReview.Status.UserInfo.Groups).To(ConsistOf("system:authenticated", projectID+":"+role1, projectID+":"+role2))
				Expect(selfSubjectReview.Status.UserInfo.Extra).To(Equal(map[string]authenticationv1.ExtraValue{
					"project_id":          {projectID},
					"project_name":        {projectName},
					"project_domain_id":   {projectDomainID},
					"project_domain_name": {projectDomainName},
					"user_domain_id":      {userDomainID},
					"user_domain_name":    {userDomainName},
					"region":              {region},
				}))
			})

			It("should create the Gardener resources", func() {
				gardenerProjectNamespaceName := persephone.GetGardenerProjectNamespaceName(region, projectID)
				gardenerProjectName := persephone.GetGardenerProjectName(region, projectID, landscapeName)

				// dummy request with OpenStack user client to trigger webhook logic
				Expect(openStackUserClient.List(ctx, &corev1.NamespaceList{})).To(BeForbiddenError())

				// use test client which has cluster-admin privileges for these GET requests
				// we only check whether the resources were created - unit tests will ensure that the spec is as
				// expected
				Expect(testClient.Get(ctx, client.ObjectKey{Name: gardenerProjectNamespaceName}, &corev1.Namespace{})).To(Succeed())
				Expect(testClient.Get(ctx, client.ObjectKey{Name: gardenerProjectName}, &gardenercorev1beta1.Project{})).To(Succeed())
				Expect(testClient.Get(ctx, client.ObjectKey{Name: "persephone.sci.cloud.sap:openstack-project-kubernetes-admin", Namespace: gardenerProjectNamespaceName}, &rbacv1.Role{})).To(Succeed())
				Expect(testClient.Get(ctx, client.ObjectKey{Name: "persephone.sci.cloud.sap:openstack-project-kubernetes-admin", Namespace: gardenerProjectNamespaceName}, &rbacv1.RoleBinding{})).To(Succeed())
			})
		})
	})
})

func newOpenStackUserClient(token string) client.Client {
	openStackUserRestConfig := &rest.Config{
		Host: testRestConfig.Host, TLSClientConfig: rest.TLSClientConfig{CAData: testRestConfig.CAData},
		QPS: 1000.0, Burst: 2000.0,
		BearerToken: token,
	}

	openStackUserClient, err := client.New(openStackUserRestConfig, client.Options{Scheme: kubernetes.GardenScheme})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	return openStackUserClient
}

// BeUnauthorizedError checks if error is Unauthorized.
func BeUnauthorizedError() types.GomegaMatcher {
	return &kubernetesErrors{
		checkFunc: apierrors.IsUnauthorized,
		message:   "Unauthorized",
	}
}

type kubernetesErrors struct {
	checkFunc func(error) bool
	message   string
}

func (k *kubernetesErrors) Match(actual any) (success bool, err error) {
	// is purely nil?
	if actual == nil {
		return false, nil
	}

	actualErr, actualOk := actual.(error)
	if !actualOk {
		return false, fmt.Errorf("expected an error-type.  got:\n%s", format.Object(actual, 1))
	}

	return k.checkFunc(actualErr), nil
}

func (k *kubernetesErrors) FailureMessage(actual any) (message string) {
	return format.Message(actual, fmt.Sprintf("to be %s error", k.message))
}
func (k *kubernetesErrors) NegatedFailureMessage(actual any) (message string) {
	return format.Message(actual, fmt.Sprintf("to not be %s error", k.message))
}
