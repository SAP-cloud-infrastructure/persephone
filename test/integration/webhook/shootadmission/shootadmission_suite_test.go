// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package shootadmission_test

import (
	"context"
	"net/http"
	"testing"

	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/utils"
	. "github.com/gardener/gardener/pkg/utils/test/matchers"
	gardenerenvtest "github.com/gardener/gardener/test/envtest"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controllerconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/sap-cloud-infrastructure/persephone/internal/openstack"
	"github.com/sap-cloud-infrastructure/persephone/internal/webhook/shootadmission"
)

func TestShootAdmission(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Test Integration Webhook ShootAdmission Suite")
}

const (
	testID          = "token-webhook-test"
	supportedRegion = "supported-region"
)

var (
	ctx = context.Background()
	log logr.Logger

	testEnv        *gardenerenvtest.GardenerTestEnvironment
	testRestConfig *rest.Config
	testClient     client.Client
	testRunID      = utils.ComputeSHA256Hex([]byte(uuid.NewUUID()))[:8]

	testNamespace *corev1.Namespace
)

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true}), zap.WriteTo(GinkgoWriter)))
	log = logf.Log.WithName(testID)

	By("Start test environment")
	testEnv = &gardenerenvtest.GardenerTestEnvironment{
		Environment: &envtest.Environment{
			WebhookInstallOptions: envtest.WebhookInstallOptions{
				MutatingWebhooks: []*admissionregistrationv1.MutatingWebhookConfiguration{{
					ObjectMeta: metav1.ObjectMeta{Name: "shoot-mutating-webhoook"},
					Webhooks: []admissionregistrationv1.MutatingWebhook{{
						Name: "shoot-admission.webhook.persephone.sci.cloud.sap",
						Rules: []admissionregistrationv1.RuleWithOperations{{
							Rule: admissionregistrationv1.Rule{
								APIGroups:   []string{gardenercorev1beta1.GroupName},
								APIVersions: []string{"*"},
								Resources:   []string{"shoots"},
							},
							Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
						}},
						FailurePolicy: ptr.To(admissionregistrationv1.Fail),
						ClientConfig: admissionregistrationv1.WebhookClientConfig{
							Service: &admissionregistrationv1.ServiceReference{
								Path: ptr.To(shootadmission.WebhookPath),
							},
						},
						AdmissionReviewVersions: []string{admissionv1.SchemeGroupVersion.Version},
						MatchPolicy:             ptr.To(admissionregistrationv1.Exact),
						SideEffects:             ptr.To(admissionregistrationv1.SideEffectClassNoneOnDryRun),
						TimeoutSeconds:          ptr.To[int32](10),
					}},
				}},
			},
		},
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
		Scheme: kubernetes.GardenScheme,
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    testEnv.WebhookInstallOptions.LocalServingPort,
			Host:    testEnv.WebhookInstallOptions.LocalServingHost,
			CertDir: testEnv.WebhookInstallOptions.LocalServingCertDir,
		}),
		Metrics:    metricsserver.Options{BindAddress: "0"},
		Controller: controllerconfig.Controller{SkipNameValidation: new(true)},
	})
	Expect(err).NotTo(HaveOccurred())

	By("Register webhook")
	Expect((&shootadmission.Handler{
		Logger: log.WithName("shootadmission-webhook-handler"),
		Client: mgr.GetClient(),
		OpenStackClientSet: openstack.RegionOpenStackClientSet{
			supportedRegion: nil,
		},
	}).AddToManager(mgr)).To(Succeed())

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
})
