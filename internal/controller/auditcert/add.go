package auditcert

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/gardener/gardener/pkg/controllerutils"
)

// Options configures the certificate management controllers.
type Options struct {
	// SecretName is the name of the Secret to store TLS materials in.
	SecretName string
	// Namespace is the namespace where the Secret is stored.
	Namespace string
	// ServiceName is the DNS name of the Service (used for server cert SANs).
	ServiceName string
	// CertDir is the directory where certs are written to disk. If empty, it is
	// read from the manager's webhook server configuration.
	CertDir string
	// CertValidity is the validity duration for certificates.
	CertValidity time.Duration
	// SyncPeriod is how often the reconciler checks for rotation.
	SyncPeriod time.Duration
	// ReloadPeriod is how often the reloader syncs certs from Secret to disk.
	ReloadPeriod time.Duration
}

// DefaultOptions returns Options with sensible defaults.
//
//nolint:gosec // G101: SecretName is a Kubernetes resource name, not a credential.
func DefaultOptions() Options {
	return Options{
		SecretName:   "persephone-audit-webhook-tls",
		Namespace:    "garden",
		ServiceName:  "persephone-audit-webhook",
		CertValidity: 30 * 24 * time.Hour,
		SyncPeriod:   5 * time.Minute,
		ReloadPeriod: 30 * time.Second,
	}
}

// AddToManager adds both the certificate reconciler (leader-elected) and the certificate
// reloader (all replicas) to the given manager.
func AddToManager(ctx context.Context, mgr manager.Manager, opts Options) error {
	rec := &reconciler{
		client:       mgr.GetClient(),
		secretName:   opts.SecretName,
		namespace:    opts.Namespace,
		serviceName:  opts.ServiceName,
		certValidity: opts.CertValidity,
		syncPeriod:   opts.SyncPeriod,
	}

	// Ensure the certificate Secret exists before the webhook server starts.
	// On first deploy the leader hasn't reconciled yet, so we create the
	// Secret eagerly here (same pattern as Gardener's certificate management).
	// The cache is not started yet, so we need an uncached client.
	secret := &corev1.Secret{}
	apiReader := mgr.GetAPIReader()
	if err := apiReader.Get(ctx, types.NamespacedName{Name: opts.SecretName, Namespace: opts.Namespace}, secret); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to check for existing certificate secret: %w", err)
		}

		uncachedClient, err := client.New(mgr.GetConfig(), client.Options{
			Cache: &client.CacheOptions{Reader: apiReader},
		})
		if err != nil {
			return fmt.Errorf("failed to create uncached client: %w", err)
		}

		initialRec := &reconciler{
			client:       uncachedClient,
			secretName:   opts.SecretName,
			namespace:    opts.Namespace,
			serviceName:  opts.ServiceName,
			certValidity: opts.CertValidity,
		}
		if err := initialRec.generateAndCreateSecret(ctx); err != nil {
			return fmt.Errorf("failed to generate initial certificates: %w", err)
		}
	}

	ctrl, err := controller.New("audit-webhook-certificate", mgr, controller.Options{
		Reconciler:   rec,
		RecoverPanic: ptr.To(true), //nolint:modernize // ptr.To(true) is clearer than new(bool) which would be false.
	})
	if err != nil {
		return fmt.Errorf("failed to create certificate reconciler: %w", err)
	}
	if err := ctrl.Watch(controllerutils.EnqueueOnce); err != nil {
		return fmt.Errorf("failed to set up watch for certificate reconciler: %w", err)
	}

	rel := &reloader{
		reader:       mgr.GetClient(),
		secretName:   opts.SecretName,
		namespace:    opts.Namespace,
		reloadPeriod: opts.ReloadPeriod,
		certDir:      opts.CertDir,
	}

	if err := rel.AddToManager(ctx, mgr); err != nil {
		return fmt.Errorf("failed to add certificate reloader: %w", err)
	}

	return nil
}
