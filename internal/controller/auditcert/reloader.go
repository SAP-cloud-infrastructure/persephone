// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package auditcert

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/gardener/gardener/pkg/controllerutils"
	secretsutils "github.com/gardener/gardener/pkg/utils/secrets"
)

const reloaderName = "audit-webhook-certificate-reloader"

type reloader struct {
	reader       client.Reader
	secretName   string
	namespace    string
	reloadPeriod time.Duration
	certDir      string

	lock                sync.Mutex
	lastResourceVersion string
}

// AddToManager performs an initial cert load then starts the reloader as a non-leader-elected runnable.
func (r *reloader) AddToManager(ctx context.Context, mgr manager.Manager) error {
	if r.certDir == "" {
		webhookServer := mgr.GetWebhookServer()
		defaultServer, ok := webhookServer.(*webhook.DefaultServer)
		if !ok {
			return fmt.Errorf("expected *webhook.DefaultServer, got %T", webhookServer)
		}
		r.certDir = defaultServer.Options.CertDir
	}

	// Initial blocking load so the webhook server can start with valid certs.
	if err := r.loadAndWriteCerts(ctx, mgr.GetAPIReader()); err != nil {
		return fmt.Errorf("initial certificate load failed: %w", err)
	}

	ctrl, err := controller.NewUnmanaged(reloaderName, controller.Options{
		Reconciler:   r,
		RecoverPanic: ptr.To(true), //nolint:modernize // ptr.To(true) is clearer than new(bool) which would be false.
	})
	if err != nil {
		return err
	}

	if err := ctrl.Watch(controllerutils.EnqueueOnce); err != nil {
		return err
	}

	return mgr.Add(nonLeaderElectionRunnable{ctrl})
}

func (r *reloader) Reconcile(ctx context.Context, _ reconcile.Request) (reconcile.Result, error) {
	log := logf.FromContext(ctx).WithValues("secret", r.secretName, "namespace", r.namespace)

	secret := &corev1.Secret{}
	if err := r.reader.Get(ctx, types.NamespacedName{Name: r.secretName, Namespace: r.namespace}, secret); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to get certificate secret: %w", err)
	}

	r.lock.Lock()
	defer r.lock.Unlock()

	if secret.ResourceVersion == r.lastResourceVersion {
		log.V(1).Info("Secret unchanged, checking again later")
		return reconcile.Result{RequeueAfter: r.reloadPeriod}, nil
	}

	log.Info("Certificate secret updated, writing to disk")
	if err := writeCertificatesToDisk(r.certDir, secret.Data); err != nil {
		return reconcile.Result{}, err
	}

	r.lastResourceVersion = secret.ResourceVersion
	return reconcile.Result{RequeueAfter: r.reloadPeriod}, nil
}

func (r *reloader) loadAndWriteCerts(ctx context.Context, reader client.Reader) error {
	secret := &corev1.Secret{}
	if err := reader.Get(ctx, types.NamespacedName{Name: r.secretName, Namespace: r.namespace}, secret); err != nil {
		return err
	}

	r.lock.Lock()
	defer r.lock.Unlock()

	if err := writeCertificatesToDisk(r.certDir, secret.Data); err != nil {
		return err
	}
	r.lastResourceVersion = secret.ResourceVersion
	return nil
}

func writeCertificatesToDisk(certDir string, data map[string][]byte) error {
	if err := os.MkdirAll(certDir, 0700); err != nil {
		return fmt.Errorf("failed to create cert directory: %w", err)
	}

	// Write CA before tls.crt/tls.key so the certwatcher callback
	// (triggered by tls.crt/tls.key changes) can safely re-read ca.crt.
	ordered := []string{
		secretsutils.DataKeyCertificateCA,
		secretsutils.DataKeyCertificate,
		secretsutils.DataKeyPrivateKey,
	}
	for _, name := range ordered {
		content := data[name]
		if len(content) == 0 {
			return fmt.Errorf("certificate data for %q is empty", name)
		}
		if err := os.WriteFile(filepath.Join(certDir, name), content, 0600); err != nil {
			return fmt.Errorf("failed to write %q: %w", name, err)
		}
	}

	return nil
}

type nonLeaderElectionRunnable struct {
	manager.Runnable
}

func (n nonLeaderElectionRunnable) NeedLeaderElection() bool {
	return false
}
