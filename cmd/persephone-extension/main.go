// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/sap-cloud-infrastructure/persephone/cmd/persephone-extension/options"
	"github.com/sap-cloud-infrastructure/persephone/internal/extension"
	"github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
)

func main() {
	opts := &options.Options{}
	opts.AddFlags()
	flag.Parse()

	log := zap.New(zap.UseFlagOptions(&opts.Zap)).WithName("persephone-extension")
	logf.SetLogger(log)
	klog.SetLogger(log)

	if err := run(signals.SetupSignalHandler(), log, opts); err != nil {
		log.Error(err, "fatal error")
		os.Exit(1)
	}
}

func run(ctx context.Context, logger logr.Logger, opts *options.Options) error {
	if err := opts.Validate(); err != nil {
		return fmt.Errorf("invalid options: %w", err)
	}

	mgr, err := manager.New(controllerruntime.GetConfigOrDie(), manager.Options{
		Logger:                  logger,
		Scheme:                  kubernetes.GardenerScheme,
		GracefulShutdownTimeout: ptr.To(5 * time.Second),
		HealthProbeBindAddress:  opts.Health.Addr,
		Metrics:                 metricsserver.Options{BindAddress: opts.Metrics.Addr},
	})
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return err
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return err
	}

	if err := extension.AddToManager(ctx, mgr, opts.ReceiverImage); err != nil {
		return fmt.Errorf("add extension controllers: %w", err)
	}

	return mgr.Start(ctx)
}
