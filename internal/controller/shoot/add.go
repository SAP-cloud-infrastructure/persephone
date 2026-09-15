// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package shoot

import (
	"context"
	"maps"
	"slices"

	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	gardenersecurityv1alpha1 "github.com/gardener/gardener/pkg/apis/security/v1alpha1"
	predicateutils "github.com/gardener/gardener/pkg/controllerutils/predicate"
	"github.com/go-logr/logr"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/sap-cloud-infrastructure/persephone/internal/constants"
)

// ControllerName is the name of the controller.
const ControllerName = "shoot"

// AddToManager adds the controller to the manager.
func (r *Reconciler) AddToManager(mgr manager.Manager, maxConcurrentReconciles int) error {
	if r.Client == nil {
		r.Client = mgr.GetClient()
	}
	if r.Clock == nil {
		r.Clock = clock.RealClock{}
	}

	// Exclude Shoots in the garden namespace (managed seed Shoots) from reconciliation.
	notInGardenNamespace := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return obj.GetNamespace() != v1beta1constants.GardenNamespace
	})

	return builder.
		ControllerManagedBy(mgr).
		Named(ControllerName).
		For(&gardenercorev1beta1.Shoot{}, builder.WithPredicates(notInGardenNamespace, shootRelevantUpdatePredicate{})).
		Watches(
			&gardenersecurityv1alpha1.CredentialsBinding{},
			handler.EnqueueRequestsFromMapFunc(r.MapCredentialsBindingToShoot(mgr.GetLogger().WithValues("controller", ControllerName))),
			builder.WithPredicates(predicateutils.ForEventTypes(predicateutils.Create)),
		).
		Watches(
			&gardenercorev1beta1.InternalSecret{},
			handler.EnqueueRequestsFromMapFunc(r.MapInternalSecretToShoot()),
			builder.WithPredicates(predicateutils.ForEventTypes(predicateutils.Delete)),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrentReconciles}).
		Complete(r)
}

// MapCredentialsBindingToShoot is a handler.MapFunc for mapping a CredentialsBinding to the related Shoot.
func (r *Reconciler) MapCredentialsBindingToShoot(log logr.Logger) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		credentialsBinding, ok := obj.(*gardenersecurityv1alpha1.CredentialsBinding)
		if !ok {
			return nil
		}

		internalSecret := &gardenercorev1beta1.InternalSecret{ObjectMeta: metav1.ObjectMeta{Name: credentialsBinding.CredentialsRef.Name, Namespace: credentialsBinding.CredentialsRef.Namespace}}
		if err := r.Client.Get(ctx, client.ObjectKeyFromObject(internalSecret), internalSecret); err != nil {
			log.Error(err, "Failed to get InternalSecret referenced in CredentialsBinding", "internalSecret", client.ObjectKeyFromObject(internalSecret))
			return nil
		}

		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: internalSecret.Labels[constants.LabelKeyShootName], Namespace: internalSecret.Namespace}}}
	}
}

// MapInternalSecretToShoot is a handler.MapFunc for mapping an InternalSecret to the related Shoot.
func (r *Reconciler) MapInternalSecretToShoot() handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		internalSecret, ok := obj.(*gardenercorev1beta1.InternalSecret)
		if !ok {
			return nil
		}

		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: internalSecret.Labels[constants.LabelKeyShootName], Namespace: internalSecret.Namespace}}}
	}
}

// shootRelevantUpdatePredicate filters out Shoot updates that only change the status subresource.
// Gardenlet updates Shoot status frequently during reconciliation, and these updates are irrelevant
// to this controller — EXCEPT when the Shoot is being deleted (the delete flow depends on
// status.lastOperation to determine when cleanup is safe).
type shootRelevantUpdatePredicate struct {
	predicate.Funcs
}

func (shootRelevantUpdatePredicate) Update(e event.UpdateEvent) bool {
	// Defensive guard — controller-runtime should never send an UpdateEvent with nil objects,
	// but the fields are pointers so we check to be safe.
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}

	// During deletion, reconcile when:
	// - The DeletionTimestamp was just set (initial delete event), or
	// - lastOperation indicates the Shoot has been successfully deleted
	//   (the only status transition the delete flow acts on).
	if e.ObjectNew.GetDeletionTimestamp() != nil {
		if e.ObjectOld.GetDeletionTimestamp() == nil {
			return true
		}
		newShoot, ok := e.ObjectNew.(*gardenercorev1beta1.Shoot)
		if !ok {
			return true
		}
		return newShoot.Status.LastOperation != nil &&
			newShoot.Status.LastOperation.Type == gardenercorev1beta1.LastOperationTypeDelete &&
			newShoot.Status.LastOperation.State == gardenercorev1beta1.LastOperationStateSucceeded
	}

	// Reconcile when the generation changed (spec was modified).
	if e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() {
		return true
	}

	// Reconcile when annotations changed (e.g. force credential rotation annotation).
	if !maps.Equal(e.ObjectOld.GetAnnotations(), e.ObjectNew.GetAnnotations()) {
		return true
	}

	// Reconcile when finalizers changed.
	if !slices.Equal(e.ObjectOld.GetFinalizers(), e.ObjectNew.GetFinalizers()) {
		return true
	}

	// Reconcile when labels changed.
	if !maps.Equal(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels()) {
		return true
	}

	// For Shoot objects, compare the spec to catch spec changes that don't bump
	// generation (the Gardener API server only bumps generation for certain spec fields).
	oldShoot, oldOK := e.ObjectOld.(*gardenercorev1beta1.Shoot)
	newShoot, newOK := e.ObjectNew.(*gardenercorev1beta1.Shoot)
	if oldOK && newOK {
		return !apiequality.Semantic.DeepEqual(oldShoot.Spec, newShoot.Spec)
	}

	return false
}
