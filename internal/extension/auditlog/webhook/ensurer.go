// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"
	"errors"
	"fmt"

	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	extensionscontextwebhook "github.com/gardener/gardener/extensions/pkg/webhook/context"
	"github.com/gardener/gardener/extensions/pkg/webhook/controlplane/genericmutator"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/sap-cloud-infrastructure/persephone/internal/extension/auditlog"
)

const (
	mountPath  = "/etc/kubernetes/webhook/audit"
	volumeName = "audit-webhook-kubeconfig"
	flagValue  = "--audit-webhook-config-file=" + mountPath + "/kubeconfig.yaml"
)

// Ensurer mutates kube-apiserver Deployments on the seed to inject or remove
// the audit webhook configuration depending on whether persephone-auditlog is
// enabled for the shoot.
type Ensurer struct {
	genericmutator.NoopEnsurer
}

// NewEnsurer creates a new Ensurer.
func NewEnsurer(_ manager.Manager) *Ensurer {
	return &Ensurer{}
}

// EnsureKubeAPIServerDeployment injects (or removes) the audit webhook flag,
// volume mount, and volume into the kube-apiserver Deployment.
func (e *Ensurer) EnsureKubeAPIServerDeployment(ctx context.Context, gctx extensionscontextwebhook.GardenContext, dep, _ *appsv1.Deployment) error {
	cluster, err := gctx.GetCluster(ctx)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}

	if isExtensionEnabled(cluster.Shoot) {
		return injectAuditWebhook(dep)
	}
	return removeAuditWebhook(dep)
}

func isExtensionEnabled(shoot *gardencorev1beta1.Shoot) bool {
	if shoot == nil {
		return false
	}
	for _, ext := range shoot.Spec.Extensions {
		if ext.Type == "persephone-auditlog" {
			return ext.Disabled == nil || !*ext.Disabled
		}
	}
	return false
}

func injectAuditWebhook(dep *appsv1.Deployment) error {
	if len(dep.Spec.Template.Spec.Containers) == 0 {
		return errors.New("kube-apiserver Deployment has no containers")
	}
	c := &dep.Spec.Template.Spec.Containers[0]
	c.Args = extensionswebhook.EnsureStringWithPrefix(c.Args, "--audit-webhook-config-file=", mountPath+"/kubeconfig.yaml")
	c.VolumeMounts = extensionswebhook.EnsureVolumeMountWithName(c.VolumeMounts, corev1.VolumeMount{
		Name:      volumeName,
		MountPath: mountPath,
		ReadOnly:  true,
	})
	vol := corev1.Volume{Name: volumeName, Secret: &corev1.SecretVolumeSource{SecretName: auditlog.WebhookKubeconfigSecretName}}
	dep.Spec.Template.Spec.Volumes = extensionswebhook.EnsureVolumeWithName(dep.Spec.Template.Spec.Volumes, vol)
	return nil
}

func removeAuditWebhook(dep *appsv1.Deployment) error {
	if len(dep.Spec.Template.Spec.Containers) == 0 {
		return nil
	}
	c := &dep.Spec.Template.Spec.Containers[0]
	c.Args = extensionswebhook.EnsureNoStringWithPrefix(c.Args, "--audit-webhook-config-file=")
	c.VolumeMounts = extensionswebhook.EnsureNoVolumeMountWithName(c.VolumeMounts, volumeName)
	dep.Spec.Template.Spec.Volumes = extensionswebhook.EnsureNoVolumeWithName(dep.Spec.Template.Spec.Volumes, volumeName)
	return nil
}
