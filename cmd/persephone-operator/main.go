package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	gardenerhealthz "github.com/gardener/gardener/pkg/healthz"
	"github.com/go-logr/logr"
	"github.com/sapcc/go-api-declarations/bininfo"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	"k8s.io/utils/ptr"
	clientconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/sap-cloud-infrastructure/persephone/cmd/persephone-operator/options"
	"github.com/sap-cloud-infrastructure/persephone/internal/config"
	"github.com/sap-cloud-infrastructure/persephone/internal/controller"
	"github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
)

func main() {
	bininfo.HandleVersionArgument()

	opts := &options.Options{}
	opts.AddFlags()
	flag.Parse()

	log := zap.New(zap.UseFlagOptions(&opts.Zap)).WithName("operator")
	logf.SetLogger(log)
	klog.SetLogger(log)

	if err := run(signals.SetupSignalHandler(), log, opts); err != nil {
		panic(err)
	}
}

func run(ctx context.Context, logger logr.Logger, opts *options.Options) error {
	log := logger.WithName("persephone-operator")
	opts.Complete()
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

	log.Info("Getting rest config")
	restConfig, err := clientconfig.GetConfigWithContext(opts.KubeConfigContext)
	if err != nil {
		return fmt.Errorf("failed getting REST config: %w", err)
	}

	log.Info("Setting up manager")
	mgr, err := manager.New(restConfig, manager.Options{
		Logger:                  log,
		Scheme:                  kubernetes.GardenerScheme,
		GracefulShutdownTimeout: ptr.To(5 * time.Second),

		HealthProbeBindAddress: opts.Health.Addr,
		Metrics:                metricsserver.Options{BindAddress: opts.Metrics.Addr},

		LeaderElection:                *opts.LeaderElection.LeaderElect,
		LeaderElectionResourceLock:    opts.LeaderElection.ResourceLock,
		LeaderElectionID:              opts.LeaderElection.ResourceName,
		LeaderElectionNamespace:       opts.LeaderElection.ResourceNamespace,
		LeaderElectionReleaseOnCancel: true,
		LeaseDuration:                 &opts.LeaderElection.LeaseDuration.Duration,
		RenewDeadline:                 &opts.LeaderElection.RenewDeadline.Duration,
		RetryPeriod:                   &opts.LeaderElection.RetryPeriod.Duration,
	})
	if err != nil {
		return err
	}

	log.Info("Setting up health check endpoints")
	if err := mgr.AddHealthzCheck("ping", healthz.Ping); err != nil {
		return err
	}
	if err := mgr.AddHealthzCheck("informer-sync", gardenerhealthz.NewCacheSyncHealthzWithDeadline(mgr.GetLogger(), clock.RealClock{}, mgr.GetCache(), gardenerhealthz.DefaultCacheSyncDeadline)); err != nil {
		return err
	}
	if err := mgr.AddReadyzCheck("informer-sync", gardenerhealthz.NewCacheSyncHealthz(mgr.GetCache())); err != nil {
		return err
	}

	log.Info("Adding controllers to manager")
	if err := controller.AddToManager(ctx, mgr, opts, persephoneConfig); err != nil {
		return fmt.Errorf("failed adding controllers to manager: %w", err)
	}

	log.Info("Starting manager")
	return mgr.Start(ctx)
}
