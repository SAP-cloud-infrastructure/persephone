// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"github.com/sapcc/go-api-declarations/bininfo"
	"github.com/sapcc/go-bits/audittools"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	controllerruntime "sigs.k8s.io/controller-runtime"
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	controllerwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	secretsutils "github.com/gardener/gardener/pkg/utils/secrets"

	"github.com/sap-cloud-infrastructure/persephone/cmd/persephone-audit-webhook/options"
	"github.com/sap-cloud-infrastructure/persephone/internal/config"
	"github.com/sap-cloud-infrastructure/persephone/internal/controller/auditcert"
	"github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
	"github.com/sap-cloud-infrastructure/persephone/internal/webhook/audithandler"
)

func main() {
	bininfo.HandleVersionArgument()

	opts := &options.Options{}
	opts.AddFlags()
	flag.Parse()

	log := zap.New(zap.UseFlagOptions(&opts.Zap)).WithName("audit-webhook")
	logf.SetLogger(log)
	klog.SetLogger(log)

	if err := run(signals.SetupSignalHandler(), log, opts); err != nil {
		panic(err)
	}
}

func run(ctx context.Context, logger logr.Logger, opts *options.Options) error {
	log := logger.WithName("persephone-audit-webhook")
	if err := opts.Validate(); err != nil {
		return fmt.Errorf("failed to validate options: %w", err)
	}

	log.Info("Loading and validating config file")
	persephoneConfig, err := config.Load(opts.ConfigFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if err := config.Validate(persephoneConfig); err != nil {
		return fmt.Errorf("failed to validate config: %w", err)
	}

	certDir := opts.CertDir

	namespace := os.Getenv("NAMESPACE")
	if namespace == "" {
		namespace = "garden"
	}

	log.Info("Setting up manager")
	mgr, err := manager.New(controllerruntime.GetConfigOrDie(), manager.Options{
		Logger:                  logger,
		Scheme:                  kubernetes.GardenerScheme,
		GracefulShutdownTimeout: ptr.To(5 * time.Second),

		HealthProbeBindAddress: opts.Health.Addr,
		Metrics:                metricsserver.Options{BindAddress: opts.Metrics.Addr},

		Cache: ctrlcache.Options{
			DefaultNamespaces: map[string]ctrlcache.Config{namespace: {}},
		},

		LeaderElection:                opts.LeaderElect,
		LeaderElectionID:              "persephone-audit-webhook",
		LeaderElectionNamespace:       namespace,
		LeaderElectionReleaseOnCancel: true,

		WebhookServer: controllerwebhook.NewServer(controllerwebhook.Options{
			CertDir: certDir,
		}),
	})
	if err != nil {
		return fmt.Errorf("failed to create manager: %w", err)
	}

	log.Info("Setting up health check endpoints")
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("failed to setup health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", certReadyCheck(certDir)); err != nil {
		return fmt.Errorf("failed to setup readiness check: %w", err)
	}

	log.Info("Adding certificate management to manager")
	certOpts := auditcert.DefaultOptions()
	certOpts.SecretName = opts.CertSecretName
	certOpts.Namespace = namespace
	certOpts.CertDir = certDir
	if err := auditcert.AddToManager(ctx, mgr, certOpts); err != nil {
		return fmt.Errorf("failed adding certificate management to manager: %w", err)
	}

	// Set up certwatcher for dynamic TLS reload. The reloader has already
	// written certs to certDir during AddToManager, so the files exist.
	certPath := filepath.Join(certDir, secretsutils.DataKeyCertificate)
	keyPath := filepath.Join(certDir, secretsutils.DataKeyPrivateKey)
	watcher, err := certwatcher.New(certPath, keyPath)
	if err != nil {
		return fmt.Errorf("failed to create certificate watcher: %w", err)
	}

	// Load initial CA pool and store in atomic pointer. The certwatcher
	// callback updates this on every cert rotation. The reloader writes
	// ca.crt before tls.crt/tls.key (with fsync), so by the time this
	// callback fires the new CA is already on disk.
	var clientCAs atomic.Pointer[x509.CertPool]
	initialPool, err := loadCAPool(certDir)
	if err != nil {
		return fmt.Errorf("failed to load initial client CA pool: %w", err)
	}
	clientCAs.Store(initialPool)

	watcher.RegisterCallback(func(_ tls.Certificate) {
		pool, err := loadCAPool(certDir)
		if err != nil {
			log.Error(err, "Failed to reload client CA pool")
			return
		}
		clientCAs.Store(pool)
		log.Info("Reloaded client CA pool")
	})

	// Configure mTLS: use our certwatcher for dynamic cert serving and
	// GetConfigForClient to always serve the latest CA pool.
	mgr.GetWebhookServer().(*controllerwebhook.DefaultServer).Options.TLSOpts = []func(*tls.Config){
		func(cfg *tls.Config) {
			cfg.GetCertificate = watcher.GetCertificate
			cfg.ClientAuth = tls.RequireAndVerifyClientCert
			cfg.ClientCAs = clientCAs.Load()
			cfg.GetConfigForClient = func(_ *tls.ClientHelloInfo) (*tls.Config, error) {
				return &tls.Config{
					GetCertificate: watcher.GetCertificate,
					ClientAuth:     tls.RequireAndVerifyClientCert,
					ClientCAs:      clientCAs.Load(),
					NextProtos:     []string{"h2"},
				}, nil
			}
		},
	}

	// Add certwatcher as a runnable so it starts watching when the manager starts.
	if err := mgr.Add(watcher); err != nil {
		return fmt.Errorf("failed to add certificate watcher to manager: %w", err)
	}

	log.Info("Adding audit handler to manager")
	hostname := os.Getenv("HOSTNAME")
	if hostname == "" {
		return errors.New("HOSTNAME environment variable must be set for audit observer identification")
	}

	observer := audittools.Observer{
		TypeURI: opts.Audit.ObserverTypeURI,
		Name:    opts.Audit.ObserverName,
		ID:      hostname,
	}

	regionAuditors, err := audithandler.NewRegionAuditorSet(ctx, persephoneConfig.Regions, observer, opts.Audit.QueueName, ctrlmetrics.Registry)
	if err != nil {
		return fmt.Errorf("failed creating region auditor set: %w", err)
	}

	if err := (&audithandler.Handler{
		Logger:         mgr.GetLogger().WithName(audithandler.HandlerName),
		RegionAuditors: regionAuditors,
	}).AddToManager(mgr); err != nil {
		return fmt.Errorf("failed adding audit handler: %w", err)
	}

	log.Info("Starting manager")
	return mgr.Start(ctx)
}

// loadCAPool reads the CA certificate from the cert directory and returns a CertPool.
func loadCAPool(certDir string) (*x509.CertPool, error) {
	caCertPEM, err := os.ReadFile(filepath.Join(certDir, secretsutils.DataKeyCertificateCA))
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCertPEM) {
		return nil, errors.New("failed to parse CA certificate")
	}

	return pool, nil
}

func certReadyCheck(dir string) healthz.Checker {
	return func(_ *http.Request) error {
		for _, name := range []string{secretsutils.DataKeyCertificate, secretsutils.DataKeyPrivateKey} {
			if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
				return fmt.Errorf("certificate file %s not ready: %w", name, err)
			}
		}
		return nil
	}
}
