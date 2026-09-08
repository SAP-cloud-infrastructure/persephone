package options

import (
	"errors"
	"flag"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// Options contains options for the audit webhook command.
type Options struct {
	// ConfigFile points to a YAML config file containing regional configuration.
	ConfigFile string
	// LeaderElect controls whether leader election should be performed.
	LeaderElect bool
	// CertDir is the directory where TLS certificates are written.
	CertDir string
	// CertSecretName is the name of the Kubernetes Secret storing TLS materials.
	CertSecretName string
	// Health contains settings for the health probes.
	Health HealthOptions
	// Metrics contains settings for the metrics.
	Metrics MetricsOptions
	// Audit contains settings for the audit event publishing.
	Audit AuditOptions
	// Zap holds the zap logger flags (-zap-log-level, -zap-devel, -zap-encoder, ...).
	// Bound on AddFlags and consumed by main to construct the root logger.
	Zap zap.Options
}

// AuditOptions contains settings for audit event publishing.
type AuditOptions struct {
	// QueueName is the RabbitMQ queue name used for publishing audit events.
	QueueName string
	// ObserverName is the human-readable service name in CADF audit events.
	ObserverName string
	// ObserverTypeURI is the CADF type classification for the observer.
	ObserverTypeURI string
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

// Validate validates the options.
func (o *Options) Validate() error {
	if o.ConfigFile == "" {
		return errors.New("config flag must be provided")
	}
	if o.CertSecretName == "" {
		return errors.New("cert-secret-name flag must be provided")
	}

	return nil
}

// AddFlags adds the flags to the default flag set.
func (o *Options) AddFlags() {
	flag.StringVar(&o.ConfigFile, "config", "",
		"YAML config containing regional configuration with audit transport URLs.")
	flag.BoolVar(&o.LeaderElect, "leader-elect", true,
		"Enable leader election for controller manager.")
	flag.StringVar(&o.CertDir, "cert-dir", "/tmp/persephone-audit-webhook-cert",
		"Directory where TLS certificates are stored.")
	flag.StringVar(&o.CertSecretName, "cert-secret-name", "persephone-audit-webhook-tls",
		"Name of the Kubernetes Secret storing TLS materials.")

	flag.StringVar(&o.Health.Addr, "health-probe-bind-address", ":8081",
		"The address the probe endpoint binds to.")
	flag.StringVar(&o.Metrics.Addr, "metrics-bind-address", "0",
		"The address the metrics endpoint binds to. "+
			"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable.")

	flag.StringVar(&o.Audit.QueueName, "audit-queue-name", "notifications.info",
		"The RabbitMQ queue name used for publishing audit events.")
	flag.StringVar(&o.Audit.ObserverName, "audit-observer-name", "persephone",
		"The human-readable service name used in CADF audit events.")
	flag.StringVar(&o.Audit.ObserverTypeURI, "audit-observer-type-uri", "service/kubernetes",
		"The CADF type classification for the observer in audit events.")

	o.Zap = zap.Options{Development: false}
	o.Zap.BindFlags(flag.CommandLine)
}
