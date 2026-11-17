// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package extension

import (
	"context"
	"fmt"
	"os"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/gardener/gardener/extensions/pkg/controller/extension"
	webhookcmd "github.com/gardener/gardener/extensions/pkg/webhook/cmd"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"

	"github.com/sap-cloud-infrastructure/persephone/internal/extension/auditlog"
	auditlogwebhook "github.com/sap-cloud-infrastructure/persephone/internal/extension/auditlog/webhook"
)

const extensionType = "persephone-auditlog"

// AddToManager registers the persephone-auditlog extension actuator and webhook with the manager.
func AddToManager(ctx context.Context, mgr manager.Manager, gardenClient client.Client, receiverImage, s3EndpointTemplate string) error {
	actuator := &auditlog.Actuator{
		Client:             mgr.GetClient(),
		GardenClient:       gardenClient,
		ReceiverImage:      receiverImage,
		S3EndpointTemplate: s3EndpointTemplate,
	}

	if err := extension.Add(mgr, extension.AddArgs{
		Actuator:        actuator,
		Name:            extensionType,
		FinalizerSuffix: extensionType,
		Resync:          60 * time.Minute,
		Type:            extensionType,
		ExtensionClasses: []extensionsv1alpha1.ExtensionClass{
			extensionsv1alpha1.ExtensionClassShoot,
		},
		Predicates:                extension.DefaultPredicates(ctx, mgr, false),
		IgnoreOperationAnnotation: false,
	}); err != nil {
		return fmt.Errorf("add %s controller: %w", extensionType, err)
	}

	webhookOpts := webhookcmd.NewAddToManagerOptions(
		extensionType,
		"",  // no shoot webhook managed resource — only seed webhook
		nil, // no shoot namespace selector
		nil, // no general options
		&webhookcmd.ServerOptions{
			Namespace: os.Getenv("WEBHOOK_CONFIG_NAMESPACE"),
		},
		webhookcmd.NewSwitchOptions(
			webhookcmd.Switch("controlplane", auditlogwebhook.AddToManager),
		),
	)
	if err := webhookOpts.Complete(); err != nil {
		return fmt.Errorf("complete webhook options: %w", err)
	}
	if _, err := webhookOpts.Completed().AddToManager(ctx, mgr, nil); err != nil {
		return fmt.Errorf("add webhook to manager: %w", err)
	}

	return nil
}
