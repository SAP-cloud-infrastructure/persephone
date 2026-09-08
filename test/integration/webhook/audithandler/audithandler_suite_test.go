package audithandler_test

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/utils"
	kubernetesutils "github.com/gardener/gardener/pkg/utils/kubernetes"
	netutils "github.com/gardener/gardener/pkg/utils/net"
	. "github.com/gardener/gardener/pkg/utils/test/matchers"
	gardenerenvtest "github.com/gardener/gardener/test/envtest"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/go-bits/audittools"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/rest"
	clientcmdlatest "k8s.io/client-go/tools/clientcmd/api/latest"
	clientcmdv1 "k8s.io/client-go/tools/clientcmd/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controllerconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/sap-cloud-infrastructure/persephone/internal/webhook/audithandler"
)

func TestAuditHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Test Integration Webhook AuditHandler Suite")
}

const (
	testID      = "audit-handler-test"
	testRegion1 = "qa-de-1"
	testRegion2 = "qa-de-2"
)

var (
	ctx = context.Background()
	log logr.Logger

	testEnv        *gardenerenvtest.GardenerTestEnvironment
	testRestConfig *rest.Config
	testClient     client.Client
	testRunID      = utils.ComputeSHA256Hex([]byte(uuid.NewUUID()))[:8]

	testNamespace *corev1.Namespace

	// Mock auditors for testing
	mockAuditorRegion1 *audittools.MockAuditor
	mockAuditorRegion2 *audittools.MockAuditor

	// Handler for testing
	auditHandler *audithandler.Handler

	// Webhook server address
	webhookHost string
	webhookPort int

	// Client with OpenStack user impersonation for real API server tests
	openStackUserClientRegion1 client.Client
	openStackUserClientRegion2 client.Client
)

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true}), zap.WriteTo(GinkgoWriter)))
	log = logf.Log.WithName(testID)

	By("Resolve webhook address and port")
	webhookAddress, err := net.ResolveTCPAddr("tcp", net.JoinHostPort("localhost", "0"))
	Expect(err).NotTo(HaveOccurred())
	webhookPort, _, err = netutils.SuggestPort("")
	Expect(err).NotTo(HaveOccurred())
	webhookHost = webhookAddress.IP.String()

	By("Create audit policy file")
	auditPolicyFile, err := createAuditPolicyFile()
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		By("Delete audit policy file")
		Expect(os.Remove(auditPolicyFile)).To(Succeed())
	})

	By("Create audit webhook config file")
	auditWebhookConfigFile, err := createAuditWebhookConfigFile(webhookHost, strconv.Itoa(webhookPort))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		By("Delete audit webhook config file")
		Expect(os.Remove(auditWebhookConfigFile)).To(Succeed())
	})

	By("Start test environment with audit configuration")
	testEnv = &gardenerenvtest.GardenerTestEnvironment{
		Environment: &envtest.Environment{
			// Increase stop timeout because apiserver with audit webhook may take longer to stop
			// as it tries to deliver pending audit events
			ControlPlaneStopTimeout: 60 * time.Second,
			WebhookInstallOptions: envtest.WebhookInstallOptions{
				LocalServingHost: webhookHost,
			},
		},
		GardenerAPIServer: &gardenerenvtest.GardenerAPIServer{
			Args: []string{
				"--disable-admission-plugins=DeletionConfirmation,ResourceReferenceManager,ExtensionValidator,ShootDNS,ShootQuotaValidator,ShootTolerationRestriction,ShootValidator,ShootMutator",
				"--audit-policy-file=" + auditPolicyFile,
				"--audit-webhook-config-file=" + auditWebhookConfigFile,
				"--audit-webhook-batch-max-wait=100ms",
				"--audit-webhook-initial-backoff=100ms",
				"--audit-webhook-batch-max-size=1",
			},
		},
	}

	testRestConfig, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(testRestConfig).NotTo(BeNil())

	DeferCleanup(func() {
		By("Stop test environment")
		Expect(testEnv.Stop()).To(Succeed())
	})

	By("Create test client")
	testClient, err = client.New(testRestConfig, client.Options{Scheme: kubernetes.GardenScheme})
	Expect(err).NotTo(HaveOccurred())

	By("Setup mock auditors")
	mockAuditorRegion1 = audittools.NewMockAuditor()
	mockAuditorRegion2 = audittools.NewMockAuditor()

	By("Setup manager with webhook server")
	mgr, err := manager.New(testRestConfig, manager.Options{
		Scheme: kubernetes.GardenScheme,
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    webhookPort,
			Host:    testEnv.WebhookInstallOptions.LocalServingHost,
			CertDir: testEnv.WebhookInstallOptions.LocalServingCertDir,
		}),
		Metrics:    metricsserver.Options{BindAddress: "0"},
		Controller: controllerconfig.Controller{SkipNameValidation: new(true)},
	})
	Expect(err).NotTo(HaveOccurred())

	By("Create audit handler with mock auditors")
	regionAuditors := &audithandler.RegionAuditorSet{}
	regionAuditors.SetTestAuditors(map[string]audittools.Auditor{
		testRegion1: mockAuditorRegion1,
		testRegion2: mockAuditorRegion2,
	})

	auditHandler = &audithandler.Handler{
		Logger:         log.WithName("audit-handler"),
		RegionAuditors: regionAuditors,
	}

	Expect(auditHandler.AddToManager(mgr)).To(Succeed())

	By("Start manager")
	mgrContext, mgrCancel := context.WithCancel(ctx)

	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(mgrContext)).To(Succeed())
	}()

	// Wait for the webhook server to start
	Eventually(func() error {
		return mgr.GetWebhookServer().StartedChecker()(&http.Request{})
	}).Should(Succeed())

	DeferCleanup(func() {
		By("Stop manager")
		mgrCancel()
	})

	By("Create OpenStack user clients for real API server tests")
	openStackUserClientRegion1 = newOpenStackUserClient(testRegion1)
	openStackUserClientRegion2 = newOpenStackUserClient(testRegion2)

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
})

// createAuditPolicyFile creates a temporary audit policy file for the API server.
func createAuditPolicyFile() (string, error) {
	policy := `apiVersion: audit.k8s.io/v1
kind: Policy
rules:
- level: RequestResponse
  verbs: ["create", "update", "patch", "delete"]
  resources:
  - group: "core.gardener.cloud"
    resources: ["shoots"]
  omitStages:
  - RequestReceived
- level: Request
  verbs: ["create"]
  resources:
  - group: "core.gardener.cloud"
    resources: ["shoots/adminkubeconfig", "shoots/viewerkubeconfig"]
  omitStages:
  - RequestReceived
`
	file, err := os.CreateTemp("", "audit-policy-*.yaml")
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(file.Name(), []byte(policy), 0600); err != nil {
		return "", err
	}

	return file.Name(), nil
}

// createAuditWebhookConfigFile creates a kubeconfig file for the audit webhook.
func createAuditWebhookConfigFile(address, port string) (string, error) {
	webhookURL := url.URL{
		Scheme: "https",
		Host:   net.JoinHostPort(address, port),
		Path:   audithandler.WebhookPath,
	}

	kubeconfig, err := runtime.Encode(clientcmdlatest.Codec, kubernetesutils.NewKubeconfig(
		"audit-webhook",
		clientcmdv1.Cluster{
			Server:                webhookURL.String(),
			InsecureSkipTLSVerify: true,
		},
		clientcmdv1.AuthInfo{},
	))
	if err != nil {
		return "", err
	}

	kubeconfigFile, err := os.CreateTemp("", "audit-webhook-config-*.yaml")
	if err != nil {
		return "", err
	}

	return kubeconfigFile.Name(), os.WriteFile(kubeconfigFile.Name(), kubeconfig, 0600)
}

// newOpenStackUserClient creates a client with OpenStack user impersonation.
func newOpenStackUserClient(region string) client.Client {
	GinkgoHelper()

	userConfig := rest.CopyConfig(testRestConfig)
	userConfig.Impersonate = rest.ImpersonationConfig{
		UserName: "openstack-user@test-domain",
		Groups:   []string{"system:masters"},
		Extra: map[string][]string{
			"project_domain_id":   {"domain-123"},
			"project_domain_name": {"test-domain"},
			"project_id":          {"project-456"},
			"project_name":        {"test-project"},
			"user_domain_id":      {"user-domain-789"},
			"user_domain_name":    {"test-user-domain"},
			"region":              {region},
		},
	}

	openStackUserClient, err := client.New(userConfig, client.Options{Scheme: kubernetes.GardenScheme})
	Expect(err).NotTo(HaveOccurred())
	return openStackUserClient
}
