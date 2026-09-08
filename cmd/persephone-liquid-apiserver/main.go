package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"github.com/sapcc/go-api-declarations/bininfo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
	"github.com/sap-cloud-infrastructure/persephone/internal/liquidserver"
)

var (
	region      string
	listen      string
	kubeContext string
)

func main() {
	bininfo.HandleVersionArgument()

	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	log := zap.New(zap.UseFlagOptions(&opts)).WithName("liquid-apiserver")
	logf.SetLogger(log)
	klog.SetLogger(log)

	if err := run(signals.SetupSignalHandler(), log); err != nil {
		log.Error(err, "Fatal error")
		os.Exit(1)
	}
}

func init() {
	flag.StringVar(&region, "region", os.Getenv("REGION"), "OpenStack region this server instance manages (required, e.g., 'qa-de-1')")
	flag.StringVar(&listen, "listen", ":8080", "HTTP bind address for the Liquid API server")
	flag.StringVar(&kubeContext, "kube-context", os.Getenv("KUBECONTEXT"), "Kubernetes context to use from kubeconfig")
}

func run(ctx context.Context, logger logr.Logger) error {
	log := logger.WithName("persephone-liquid-apiserver")
	log.Info("Starting Persephone Liquid API Server")

	// Validate required flags
	if region == "" {
		return errors.New("-region flag is required")
	}

	log.Info("Starting Persephone Liquid API Server",
		"region", region,
		"listen", listen)

	// Get REST config
	// Target: the virtual garden cluster (NOT the runtime cluster).
	log.Info("Getting REST config")
	restConfig, err := clientconfig.GetConfigWithContext(kubeContext)
	if err != nil {
		return fmt.Errorf("failed getting REST config: %w", err)
	}

	// Create controller-runtime manager with a cache for Namespace and ResourceQuota objects.
	log.Info("Creating manager", "region", region)
	mgr, err := manager.New(restConfig, manager.Options{
		Scheme: kubernetes.GardenerScheme,
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&corev1.Namespace{}:     {},
				&corev1.ResourceQuota{}: {},
			},
		},
		// Disable metrics and health probe servers since we only need the cache
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
	})
	if err != nil {
		return fmt.Errorf("failed creating manager: %w", err)
	}

	// Start the manager in the background to run the cache
	go func() {
		log.Info("Starting manager")
		if err := mgr.Start(ctx); err != nil {
			log.Error(err, "Manager failed")
		}
	}()

	// Wait for cache to sync before proceeding
	log.Info("Waiting for cache to sync")
	if !mgr.GetCache().WaitForCacheSync(ctx) {
		return errors.New("failed waiting for cache sync")
	}
	log.Info("Cache synced successfully")

	// Get the cached client from the manager
	kubeClient := mgr.GetClient()

	// Initialize Liquid logic
	log.Info("Initializing Liquid logic", "region", region)
	logic, err := liquidserver.NewLogic(ctx, region, kubeClient, log)
	if err != nil {
		return fmt.Errorf("failed initializing Liquid logic: %w", err)
	}

	// Read the Keystone identity endpoint from OS_AUTH_URL (no application credential needed).
	identityEndpoint := os.Getenv("OS_AUTH_URL")
	if identityEndpoint == "" {
		return errors.New("OS_AUTH_URL environment variable is required")
	}

	// Start Liquid API server
	log.Info("Starting Liquid API server", "address", listen)
	return liquidserver.Run(ctx, logic, liquidserver.RunOpts{
		DefaultListenAddress: listen,
		IdentityEndpoint:     identityEndpoint,
	})
}
