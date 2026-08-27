// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package project

import (
	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	predicateutils "github.com/gardener/gardener/pkg/controllerutils/predicate"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// ControllerName is the name of the controller.
const ControllerName = "project"

// AddToManager adds the controller to the manager.
func (r *Reconciler) AddToManager(mgr manager.Manager, maxConcurrentReconciles int) error {
	if r.Client == nil {
		r.Client = mgr.GetClient()
	}
	return builder.
		ControllerManagedBy(mgr).
		Named(ControllerName).
		For(&gardenercorev1beta1.Project{}, builder.WithPredicates(predicateutils.ForEventTypes(predicateutils.Create))).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrentReconciles}).
		Complete(r)
}
