// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	"github.com/gardener/gardener/extensions/pkg/webhook/certificates"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	"github.com/go-logr/logr"
	"github.com/sapcc/go-api-declarations/bininfo"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	"k8s.io/utils/ptr"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	controllerwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/sap-cloud-infrastructure/persephone/cmd/persephone-webhook/options"
	"github.com/sap-cloud-infrastructure/persephone/internal/config"
	"github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
	internalwebhook "github.com/sap-cloud-infrastructure/persephone/internal/webhook"
)

func main() {
	bininfo.HandleVersionArgument()

	opts := &options.Options{}
	opts.AddFlags()
	flag.Parse()

	log := zap.New(zap.UseFlagOptions(&opts.Zap)).WithName("webhook")
	logf.SetLogger(log)
	klog.SetLogger(log)

	if err := run(signals.SetupSignalHandler(), log, opts); err != nil {
		panic(err)
	}
}

func run(ctx context.Context, logger logr.Logger, opts *options.Options) error {
	log := logger.WithName("persephone-webhook")
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

	log.Info("Setting up manager")
	mgr, err := manager.New(controllerruntime.GetConfigOrDie(), manager.Options{
		Logger:                  logger,
		Scheme:                  kubernetes.GardenerScheme,
		GracefulShutdownTimeout: ptr.To(5 * time.Second),

		HealthProbeBindAddress: opts.Health.Addr,
		Metrics:                metricsserver.Options{BindAddress: opts.Metrics.Addr},

		LeaderElection:   opts.LeaderElect,
		LeaderElectionID: "persephone-webhook",

		WebhookServer: controllerwebhook.NewServer(controllerwebhook.Options{
			CertDir: "/tmp/persephone-webhook-cert",
		}),
	})
	if err != nil {
		return fmt.Errorf("failed to create manager: %w", err)
	}

	log.Info("Setting up health check endpoints")
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("failed to setup health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("failed to setup readiness check: %w", err)
	}

	log.Info("Adding certificate management to manager")
	if err := certificates.AddCertificateManagementToManager(
		ctx,
		mgr,
		nil,
		clock.RealClock{},
		extensionswebhook.Configs{MutatingWebhookConfig: internalwebhook.GetMutatingWebhookConfiguration()},
		nil,
		nil,
		nil,
		"",
		"persephone-webhook", true,
		v1beta1constants.GardenNamespace,
		extensionswebhook.ModeService,
		"",
	); err != nil {
		return fmt.Errorf("failed adding webhook certificate management to manager: %w", err)
	}

	log.Info("Adding webhook handlers to manager")
	if err := internalwebhook.AddToManager(ctx, mgr, persephoneConfig, opts.CreateGardenerResources); err != nil {
		return fmt.Errorf("failed adding webhooks to manager: %w", err)
	}

	log.Info("Starting manager")
	return mgr.Start(ctx)
}
