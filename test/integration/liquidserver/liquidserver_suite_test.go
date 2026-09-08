// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package liquidserver_test

import (
	"context"
	"testing"

	// kubernetes.GardenScheme is used for consistency with other integration test suites in this project,
	// even though only core/v1 types are exercised here.
	"github.com/gardener/gardener/pkg/client/kubernetes"
	gardenerenvtest "github.com/gardener/gardener/test/envtest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// testProjectUUID returns a unique 20-character hex string suitable for use as a project UUID in tests.
// Using a fresh UUID per test avoids namespace collisions when Kubernetes deletes namespaces asynchronously.
func testProjectUUID() string {
	return string(uuid.NewUUID())[:20]
}

func TestLiquidServer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Test Integration LiquidServer Suite")
}

var (
	ctx = context.Background()

	testEnv        *gardenerenvtest.GardenerTestEnvironment
	testRestConfig *rest.Config
	testClient     client.Client
)

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true}), zap.WriteTo(GinkgoWriter)))

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
		By("Stop test environment")
		Expect(testEnv.Stop()).To(Succeed())
	})

	By("Create test client")
	testClient, err = client.New(testRestConfig, client.Options{Scheme: kubernetes.GardenScheme})
	Expect(err).NotTo(HaveOccurred())
})
