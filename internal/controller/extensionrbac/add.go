// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package extensionrbac

import (
	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/sap-cloud-infrastructure/persephone/internal/constants"
)

// ControllerName is the name of the controller.
const ControllerName = "extension-rbac"

// AddToManager adds the controller to the manager.
func (r *Reconciler) AddToManager(mgr manager.Manager, maxConcurrentReconciles int) error {
	if r.Client == nil {
		r.Client = mgr.GetClient()
	}

	return builder.
		ControllerManagedBy(mgr).
		Named(ControllerName).
		For(&gardenercorev1beta1.ControllerInstallation{}, builder.WithPredicates(r.ControllerInstallationPredicate())).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrentReconciles}).
		Complete(r)
}

// ControllerInstallationPredicate filters ControllerInstallations to only those for the auditlog extension.
func (r *Reconciler) ControllerInstallationPredicate() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(object client.Object) bool {
		ci, ok := object.(*gardenercorev1beta1.ControllerInstallation)
		if !ok {
			return false
		}
		return ci.Spec.RegistrationRef.Name == constants.AuditLogExtensionType
	})
}
