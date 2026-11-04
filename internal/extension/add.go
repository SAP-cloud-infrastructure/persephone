// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package extension

import (
	"context"
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/gardener/gardener/extensions/pkg/controller/extension"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"

	"github.com/sap-cloud-infrastructure/persephone/internal/extension/auditlog"
)

const extensionType = "persephone-auditlog"

// AddToManager registers the persephone-auditlog extension actuator with the manager.
func AddToManager(ctx context.Context, mgr manager.Manager, receiverImage string) error {
	if err := extension.Add(mgr, extension.AddArgs{
		Actuator: &auditlog.Actuator{
			Client:        mgr.GetClient(),
			ReceiverImage: receiverImage,
		},
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
	return nil
}
