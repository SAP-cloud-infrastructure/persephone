// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"bytes"
	"context"
	"fmt"
	"html/template"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	extensionssecretsmanager "github.com/gardener/gardener/extensions/pkg/util/secret/manager"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Actuator reconciles Extension{type:"persephone-auditlog"} objects.
// It implements extension.Actuator from extensions/pkg/controller/extension.
type Actuator struct {
	Client        client.Client
	ReceiverImage string
}

// Reconcile ensures the receiver Deployment, Service, TLS secrets, and auditing
// Extension backend are all in place.
func (a *Actuator) Reconcile(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension) error {
	namespace := ex.Namespace

	cfg, err := DecodeAuditLogConfig(ex.Spec.ProviderConfig)
	if err != nil {
		return fmt.Errorf("decode AuditLogConfig: %w", err)
	}

	cluster, err := extensionscontroller.GetCluster(ctx, a.Client, namespace)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}

	secretsManager, err := extensionssecretsmanager.SecretsManagerForCluster(
		ctx,
		log.WithName("secretsmanager"),
		clock.RealClock{},
		a.Client,
		cluster,
		ManagerIdentity,
		SecretConfigs(namespace),
	)
	if err != nil {
		return fmt.Errorf("create SecretsManager: %w", err)
	}

	if _, err := extensionssecretsmanager.GenerateAllSecrets(ctx, secretsManager, SecretConfigs(namespace)); err != nil {
		return fmt.Errorf("generate TLS secrets: %w", err)
	}

	prefix, err := expandPrefix(cfg.ObjectStore.Prefix, cluster.Shoot.Name)
	if err != nil {
		return fmt.Errorf("expand prefix: %w", err)
	}

	deployment := ReceiverDeployment(namespace, a.ReceiverImage, cfg.ObjectStore.Container, prefix)
	if err := applyObject(ctx, a.Client, deployment); err != nil {
		return fmt.Errorf("apply receiver Deployment: %w", err)
	}

	service := ReceiverService(namespace)
	if err := applyObject(ctx, a.Client, service); err != nil {
		return fmt.Errorf("apply receiver Service: %w", err)
	}

	if err := EnsureAuditingBackend(ctx, a.Client, namespace); err != nil {
		return fmt.Errorf("ensure auditing backend: %w", err)
	}

	return secretsManager.Cleanup(ctx)
}

// Delete removes the receiver resources and cleans up the auditing backend entry.
func (a *Actuator) Delete(ctx context.Context, _ logr.Logger, ex *extensionsv1alpha1.Extension) error {
	namespace := ex.Namespace

	if err := RemoveAuditingBackend(ctx, a.Client, namespace); err != nil {
		return fmt.Errorf("remove auditing backend: %w", err)
	}
	if err := deleteObject(ctx, a.Client, ReceiverService(namespace)); err != nil {
		return fmt.Errorf("delete receiver Service: %w", err)
	}
	return deleteObject(ctx, a.Client, ReceiverDeployment(namespace, "", "", ""))
}

// ForceDelete skips graceful cleanup — used during forced shoot deletion.
func (a *Actuator) ForceDelete(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension) error {
	return a.Delete(ctx, log, ex)
}

// Migrate stores state before cross-seed migration. No persistent state for this extension.
func (a *Actuator) Migrate(_ context.Context, _ logr.Logger, _ *extensionsv1alpha1.Extension) error {
	return nil
}

// Restore re-creates resources after cross-seed migration.
func (a *Actuator) Restore(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension) error {
	return a.Reconcile(ctx, log, ex)
}

func applyObject(ctx context.Context, c client.Client, obj client.Object) error {
	return c.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("persephone-extension"))
}

func deleteObject(ctx context.Context, c client.Client, obj client.Object) error {
	return client.IgnoreNotFound(c.Delete(ctx, obj))
}

// expandPrefix replaces "{{.shootName}}" in prefix with the actual shoot name.
func expandPrefix(prefix, shootName string) (string, error) {
	tmpl, err := template.New("prefix").Parse(prefix)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"shootName": shootName}); err != nil {
		return "", err
	}
	return buf.String(), nil
}
