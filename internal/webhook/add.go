// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"
	"fmt"
	"os"

	"github.com/gardener/gardener/extensions/pkg/webhook"
	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/sap-cloud-infrastructure/persephone/internal/config"
	openstacklocal "github.com/sap-cloud-infrastructure/persephone/internal/openstack"
	"github.com/sap-cloud-infrastructure/persephone/internal/webhook/shootadmission"
	"github.com/sap-cloud-infrastructure/persephone/internal/webhook/token"
)

// AddToManager adds all webhook handlers to the given manager.
func AddToManager(ctx context.Context, mgr manager.Manager, cfg config.PersephoneConfig, reconcileGardenerProjectResources bool) error {
	openStackClientSet, err := openstacklocal.NewRegionOpenStackClientSet(ctx, cfg.Regions, cfg.ClusterUsersDomain)
	if err != nil {
		return fmt.Errorf("failed creating OpenStack clientset: %w", err)
	}

	if err := (&token.Handler{
		Logger:                            mgr.GetLogger().WithName(token.HandlerName),
		Client:                            mgr.GetClient(),
		Config:                            cfg,
		ReconcileGardenerProjectResources: reconcileGardenerProjectResources,
	}).AddToManager(mgr); err != nil {
		return fmt.Errorf("failed adding %s webhook handler: %w", token.HandlerName, err)
	}

	if err := (&shootadmission.Handler{
		Logger:                  mgr.GetLogger().WithName(shootadmission.HandlerName),
		Client:                  mgr.GetClient(),
		OpenStackClientSet:      openStackClientSet,
		LandscapeName:           cfg.LandscapeName,
		DefaultCloudProfileName: cfg.DefaultCloudProfileName,
		DefaultProviderType:     cfg.DefaultProviderType,
	}).AddToManager(mgr); err != nil {
		return fmt.Errorf("failed adding %s webhook handler: %w", shootadmission.HandlerName, err)
	}

	return nil
}

// GetMutatingWebhookConfiguration returns the webhook configuration for the given mode and URL.
func GetMutatingWebhookConfiguration() *admissionregistrationv1.MutatingWebhookConfiguration {
	return &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "persephone-webhook",
		},
		Webhooks: []admissionregistrationv1.MutatingWebhook{{
			Name:                    "shoot-admission.webhook.persephone.sci.cloud.sap",
			ClientConfig:            getClientConfig(shootadmission.WebhookPath),
			AdmissionReviewVersions: []string{"v1"},
			Rules: []admissionregistrationv1.RuleWithOperations{{
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{gardenercorev1beta1.SchemeGroupVersion.Group},
					APIVersions: []string{gardenercorev1beta1.SchemeGroupVersion.Version},
					Resources:   []string{"shoots"},
				},
				Operations: []admissionregistrationv1.OperationType{
					admissionregistrationv1.Create,
				},
			}},
			// Exclude Shoots in the garden namespace (managed seed Shoots).
			NamespaceSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      "kubernetes.io/metadata.name",
					Operator: metav1.LabelSelectorOpNotIn,
					Values:   []string{v1beta1constants.GardenNamespace},
				}},
			},
			SideEffects:    ptr.To(admissionregistrationv1.SideEffectClassNoneOnDryRun),
			FailurePolicy:  ptr.To(admissionregistrationv1.Fail),
			MatchPolicy:    ptr.To(admissionregistrationv1.Exact),
			TimeoutSeconds: ptr.To[int32](10),
		}},
	}
}

func getClientConfig(webhookPath string) admissionregistrationv1.WebhookClientConfig {
	return webhook.BuildClientConfigFor(
		webhookPath,
		os.Getenv("NAMESPACE"),
		"persephone-webhook", true,
		9443,
		webhook.ModeURLWithServiceName,
		"",
		nil,
	)
}
