// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package token_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"

	"github.com/gardener/gardener/pkg/client/kubernetes"
	kubernetesutils "github.com/gardener/gardener/pkg/utils/kubernetes"
	netutils "github.com/gardener/gardener/pkg/utils/net"
	gardenerenvtest "github.com/gardener/gardener/test/envtest"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"k8s.io/apimachinery/pkg/runtime"
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

	"github.com/sap-cloud-infrastructure/persephone/internal/config"
	"github.com/sap-cloud-infrastructure/persephone/internal/webhook/token"
)

func TestToken(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Test Integration Webhook Token Suite")
}

const (
	testID        = "token-webhook-test"
	landscapeName = "test"
)

var (
	ctx = context.Background()
	log logr.Logger

	testEnv        *gardenerenvtest.GardenerTestEnvironment
	testRestConfig *rest.Config
	testClient     client.Client

	webhookHandler    *token.Handler
	kubeAPIServerLogs *gbytes.Buffer
)

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true}), zap.WriteTo(GinkgoWriter)))
	log = logf.Log.WithName(testID)

	By("Create kubeconfig files for the authentication webhook")
	webhookAddress, err := net.ResolveTCPAddr("tcp", net.JoinHostPort("localhost", "0"))
	Expect(err).NotTo(HaveOccurred())
	webhookPort, _, err := netutils.SuggestPort("")
	Expect(err).ToNot(HaveOccurred())

	kubeconfigFilename, err := createKubeconfigFileForAuthenticationWebhook(webhookAddress.IP.String(), strconv.Itoa(webhookPort))
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(func() {
		By("Delete kubeconfig file for authentication webhook")
		Expect(os.Remove(kubeconfigFilename)).To(Succeed())
	})

	kubeAPIServerLogs = gbytes.NewBuffer()

	By("Start test environment")
	_, err = os.Stat(kubeconfigFilename)
	Expect(err).NotTo(HaveOccurred())
	testAPIServer := &envtest.APIServer{Err: kubeAPIServerLogs}
	testAPIServer.
		Configure().
		Set("authentication-token-webhook-config-file", kubeconfigFilename).
		Set("authentication-token-webhook-cache-ttl", "0")
	testEnv = &gardenerenvtest.GardenerTestEnvironment{
		Environment: &envtest.Environment{
			ControlPlane: envtest.ControlPlane{
				APIServer: testAPIServer,
			},
			ErrorIfCRDPathMissing: true,
			WebhookInstallOptions: envtest.WebhookInstallOptions{
				LocalServingHost: webhookAddress.IP.String(),
			},
		},
		GardenerAPIServer: &gardenerenvtest.GardenerAPIServer{
			Args: []string{"--disable-admission-plugins=DeletionConfirmation,ResourceReferenceManager"},
		},
	}

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

	By("Setup managers")
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

	By("Register webhook")
	webhookHandler = &token.Handler{
		Logger: log.WithName("token-webhook-handler"),
		Client: mgr.GetClient(),
		Config: config.PersephoneConfig{
			LandscapeName: landscapeName,
		},
		ReconcileGardenerProjectResources: true,
	}
	Expect(webhookHandler.AddToManager(mgr)).To(Succeed())

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

func createKubeconfigFileForAuthenticationWebhook(address, port string) (string, error) {
	kubeconfig, err := runtime.Encode(clientcmdlatest.Codec, kubernetesutils.NewKubeconfig(
		"authentication-webhook",
		clientcmdv1.Cluster{
			Server:                fmt.Sprintf("https://%s%s", net.JoinHostPort(address, port), "/validate-authentication-k8s-io-v1-tokenreview"),
			InsecureSkipTLSVerify: true,
		},
		clientcmdv1.AuthInfo{},
	))
	if err != nil {
		return "", err
	}

	kubeConfigFile, err := os.CreateTemp("", "kubeconfig-token-review-webhook-")
	if err != nil {
		return "", err
	}

	return kubeConfigFile.Name(), os.WriteFile(kubeConfigFile.Name(), kubeconfig, 0600)
}
