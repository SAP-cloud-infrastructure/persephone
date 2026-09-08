package options

import (
	"errors"
	"flag"
	"os"
	"time"

	componentbaseconfigv1alpha1 "k8s.io/component-base/config/v1alpha1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// Options contains options for this command.
type Options struct {
	// ConfigFile points to a YAML config file containing OpenStack identity endpoints and application credentials by
	// region.
	ConfigFile string
	// KubeConfigContext is the name of the context in the kubeconfig file that should be used.
	KubeConfigContext string
	// LeaderElection defines the configuration of leader election client.
	LeaderElection componentbaseconfigv1alpha1.LeaderElectionConfiguration
	// Health contains settings for the health probes.
	Health HealthOptions
	// Metrics contains settings for the metrics.
	Metrics MetricsOptions
	// Controllers contains options for the controllers.
	Controllers ControllerOptions
	// Zap holds the zap logger flags (-zap-log-level, -zap-devel, -zap-encoder, ...).
	// Bound on AddFlags and consumed by main to construct the root logger.
	Zap zap.Options

	leaderElect bool
}

// MetricsOptions contains settings for the metrics.
type MetricsOptions struct {
	// Addr is the address the metrics endpoint binds to.
	Addr string
}

// HealthOptions contains settings for the health probes.
type HealthOptions struct {
	// Addr is the address the health probe endpoint binds to.
	Addr string
}

// ControllerOptions contains options for the controllers.
type ControllerOptions struct {
	// Project contains configuration for the project controller.
	Project ProjectController
	// InternalSecret contains configuration for the internal-secret controller.
	InternalSecret InternalSecret
	// Shoot contains configuration for the shoot controller.
	Shoot ShootController
}

// ProjectController contains configuration for the project controller.
type ProjectController struct {
	MaxConcurrentReconciles int
}

// InternalSecret contains configuration for the internal-secret controller.
type InternalSecret struct {
	MaxConcurrentReconciles int
}

// ShootController contains configuration for the shoot controller.
type ShootController struct {
	MaxConcurrentReconciles int
}

// Complete completes the options.
func (o *Options) Complete() {
	o.LeaderElection.LeaderElect = &o.leaderElect

	if ptr.Deref(o.LeaderElection.LeaderElect, false) {
		if o.LeaderElection.ResourceLock == "" {
			o.LeaderElection.ResourceLock = "leases"
		}
		if o.LeaderElection.ResourceName == "" {
			o.LeaderElection.ResourceName = "persephone-operator-leader-election"
		}
		if o.LeaderElection.ResourceNamespace == "" {
			o.LeaderElection.ResourceNamespace = os.Getenv("NAMESPACE")
		}
	}
}

// Validate validates the options.
func (o *Options) Validate() error {
	if o.ConfigFile == "" {
		return errors.New("config flag must be provided")
	}

	return nil
}

// AddFlags adds the flags to the default flag set.
func (o *Options) AddFlags() {
	flag.StringVar(&o.ConfigFile, "config", "",
		"yaml config containing OpenStack identity endpoints and application credentials by region.")
	flag.StringVar(&o.KubeConfigContext, "kube-context", os.Getenv("KUBECONTEXT"),
		"The name of the context in the kubeconfig file that should be used.")

	// flag.BoolVar requires a non-nil pointer to a bool. Hence, we cannot directly use o.LeaderElection.LeaderElect.
	flag.BoolVar(&o.leaderElect, "leader-elect", true,
		"Enable leader election for controller manager.")
	flag.DurationVar(&o.LeaderElection.LeaseDuration.Duration, "leader-lease-duration", 15*time.Second,
		"Duration that non-leader candidates will wait after the last leadership renewal until they attempt to acquire "+
			"a leader slot.")
	flag.DurationVar(&o.LeaderElection.RenewDeadline.Duration, "leader-renew-deadline", 10*time.Second,
		"Interval between attempts by the acting master to renew a leadership slot before it stops leading.")
	flag.DurationVar(&o.LeaderElection.RetryPeriod.Duration, "leader-retry-duration", 2*time.Second,
		"Duration the clients should wait between attempting acquisition and renewal of a leadership.")

	flag.StringVar(&o.Health.Addr, "health-probe-bind-address", ":8081",
		"The address the probe endpoint binds to.")

	flag.StringVar(&o.Metrics.Addr, "metrics-bind-address", "0",
		"The address the metrics endpoint binds to. Leave as 0 to disable the metrics service.")

	flag.IntVar(&o.Controllers.Project.MaxConcurrentReconciles, "project-controller-max-concurrent-reconciles", 20,
		"The maximum number of concurrent reconciliations performed by the project controller.")
	flag.IntVar(&o.Controllers.InternalSecret.MaxConcurrentReconciles, "internalsecret-controller-max-concurrent-reconciles", 20,
		"The maximum number of concurrent reconciliations performed by the internal-secret controller.")
	flag.IntVar(&o.Controllers.Shoot.MaxConcurrentReconciles, "shoot-controller-max-concurrent-reconciles", 20,
		"The maximum number of concurrent reconciliations performed by the shoot controller.")

	o.Zap = zap.Options{Development: false}
	o.Zap.BindFlags(flag.CommandLine)
}
