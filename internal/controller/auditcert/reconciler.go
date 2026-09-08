// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package auditcert

import (
	"context"
	"encoding/pem"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	secretsutils "github.com/gardener/gardener/pkg/utils/secrets"
)

type reconciler struct {
	client       client.Client
	secretName   string
	namespace    string
	serviceName  string
	certValidity time.Duration
	syncPeriod   time.Duration
}

func (r *reconciler) Reconcile(ctx context.Context, _ reconcile.Request) (reconcile.Result, error) {
	log := logf.FromContext(ctx).WithValues("secret", r.secretName, "namespace", r.namespace)

	secret := &corev1.Secret{}
	err := r.client.Get(ctx, types.NamespacedName{Name: r.secretName, Namespace: r.namespace}, secret)
	if err != nil && !apierrors.IsNotFound(err) {
		return reconcile.Result{}, fmt.Errorf("failed to get certificate secret: %w", err)
	}

	if apierrors.IsNotFound(err) {
		log.Info("Certificate secret not found, generating new certificates")
		if err := r.generateAndCreateSecret(ctx); err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{RequeueAfter: r.syncPeriod}, nil
	}

	if r.needsRotation(secret) {
		log.Info("Certificate approaching expiry, rotating")
		if err := r.generateAndUpdateSecret(ctx, secret); err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{RequeueAfter: r.syncPeriod}, nil
	}

	log.V(1).Info("Certificates are still valid, checking again later")
	return reconcile.Result{RequeueAfter: r.syncPeriod}, nil
}

func (r *reconciler) needsRotation(secret *corev1.Secret) bool {
	caCertPEM := secret.Data[secretsutils.DataKeyCertificateCA]
	if len(caCertPEM) == 0 {
		return true
	}

	caCert, err := secretsutils.LoadCertificate("ca", secret.Data[secretsutils.DataKeyPrivateKeyCA], caCertPEM)
	if err != nil {
		return true
	}

	halfLife := caCert.Certificate.NotAfter.Sub(caCert.Certificate.NotBefore) / 2
	rotationTime := caCert.Certificate.NotBefore.Add(halfLife)
	return time.Now().After(rotationTime)
}

func (r *reconciler) generateAndCreateSecret(ctx context.Context) error {
	data, err := r.generateCertificates()
	if err != nil {
		return fmt.Errorf("failed to generate certificates: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.secretName,
			Namespace: r.namespace,
			Labels: map[string]string{
				"app":  "persephone",
				"role": "audit-webhook-tls",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}

	return r.client.Create(ctx, secret)
}

func (r *reconciler) generateAndUpdateSecret(ctx context.Context, secret *corev1.Secret) error {
	// Extract the current (soon-to-be-old) CA cert before generating new ones.
	// Only keep the first PEM block — we carry at most one previous generation.
	oldCACertPEM := firstPEMBlock(secret.Data[secretsutils.DataKeyCertificateCA])

	data, err := r.generateCertificates()
	if err != nil {
		return fmt.Errorf("failed to generate certificates: %w", err)
	}

	// Bundle the new CA with the previous CA so the server trusts client
	// certs signed by either during the rotation window.
	if len(oldCACertPEM) > 0 {
		data[secretsutils.DataKeyCertificateCA] = append(data[secretsutils.DataKeyCertificateCA], oldCACertPEM...)
	}

	patch := client.MergeFrom(secret.DeepCopy())
	secret.Data = data
	return r.client.Patch(ctx, secret, patch)
}

// firstPEMBlock returns the first PEM-encoded certificate from data.
// Returns nil if no valid PEM block is found.
func firstPEMBlock(data []byte) []byte {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil
	}
	return pem.EncodeToMemory(block)
}

func (r *reconciler) generateCertificates() (map[string][]byte, error) {
	validity := r.certValidity

	ca, err := (&secretsutils.CertificateSecretConfig{
		Name:       "audit-webhook-ca",
		CommonName: "persephone-audit-webhook-ca",
		CertType:   secretsutils.CACert,
		Validity:   &validity,
	}).GenerateCertificate()
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA certificate: %w", err)
	}

	serverDNSNames := []string{
		r.serviceName + "." + r.namespace + ".svc",
		r.serviceName + "." + r.namespace + ".svc.cluster.local",
		r.serviceName,
	}

	server, err := (&secretsutils.CertificateSecretConfig{
		Name:       "audit-webhook-server",
		CommonName: r.serviceName,
		DNSNames:   serverDNSNames,
		CertType:   secretsutils.ServerCert,
		SigningCA:  ca,
		Validity:   &validity,
	}).GenerateCertificate()
	if err != nil {
		return nil, fmt.Errorf("failed to generate server certificate: %w", err)
	}

	clientCert, err := (&secretsutils.CertificateSecretConfig{
		Name:       "audit-webhook-client",
		CommonName: "audit-forwarder",
		CertType:   secretsutils.ClientCert,
		SigningCA:  ca,
		Validity:   &validity,
	}).GenerateCertificate()
	if err != nil {
		return nil, fmt.Errorf("failed to generate client certificate: %w", err)
	}

	return map[string][]byte{
		secretsutils.DataKeyCertificateCA: ca.CertificatePEM,
		secretsutils.DataKeyPrivateKeyCA:  ca.PrivateKeyPEM,
		secretsutils.DataKeyCertificate:   server.CertificatePEM,
		secretsutils.DataKeyPrivateKey:    server.PrivateKeyPEM,
		"client.crt":                      clientCert.CertificatePEM,
		"client.key":                      clientCert.PrivateKeyPEM,
	}, nil
}
