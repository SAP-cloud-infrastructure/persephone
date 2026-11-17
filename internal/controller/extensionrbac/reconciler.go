// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package extensionrbac

import (
	"context"
	"fmt"

	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/controllerutils"
	gardenerutils "github.com/gardener/gardener/pkg/utils/gardener"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// FinalizerName is a constant for the name of a finalizer used by this controller.
	FinalizerName = "persephone.sci.cloud.sap/extension-rbac-controller"

	// clusterRolePrefix is the naming prefix for the per-installation ClusterRole and ClusterRoleBinding.
	clusterRolePrefix = "persephone-extension-ec2-reader-"
)

// Reconciler reconciles ControllerInstallation resources and manages RBAC for the extension SA.
type Reconciler struct {
	// Client is the controller-runtime Kubernetes client.
	Client client.Client
}

// Reconcile reconciles ControllerInstallation resources.
func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := logf.FromContext(ctx)

	ci := &gardenercorev1beta1.ControllerInstallation{}
	if err := r.Client.Get(ctx, request.NamespacedName, ci); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(1).Info("Object is gone, stop reconciling")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("error retrieving ControllerInstallation from store: %w", err)
	}

	if ci.DeletionTimestamp != nil {
		return r.delete(ctx, ci)
	}

	return r.reconcile(ctx, ci)
}

func (r *Reconciler) reconcile(ctx context.Context, ci *gardenercorev1beta1.ControllerInstallation) (reconcile.Result, error) {
	if !controllerutil.ContainsFinalizer(ci, FinalizerName) {
		if err := controllerutils.AddFinalizers(ctx, r.Client, ci, FinalizerName); err != nil {
			return reconcile.Result{}, fmt.Errorf("error adding finalizer to ControllerInstallation: %w", err)
		}
	}

	seedName := ""
	if ci.Spec.SeedRef != nil {
		seedName = ci.Spec.SeedRef.Name
	}

	clusterRoleName := extensionClusterRoleName(ci.Name)
	clusterRole := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName}}
	if _, err := controllerutil.CreateOrPatch(ctx, r.Client, clusterRole, func() error {
		clusterRole.Rules = []rbacv1.PolicyRule{
			{
				APIGroups: []string{"core.gardener.cloud"},
				Resources: []string{"internalsecrets"},
				Verbs:     []string{"get", "list", "watch"},
			},
		}
		return nil
	}); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to reconcile ClusterRole %q: %w", clusterRoleName, err)
	}

	clusterRoleBindingName := extensionClusterRoleName(ci.Name)
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: clusterRoleBindingName}}
	if _, err := controllerutil.CreateOrPatch(ctx, r.Client, clusterRoleBinding, func() error {
		clusterRoleBinding.RoleRef = rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     clusterRoleName,
		}
		clusterRoleBinding.Subjects = []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      extensionSAName(ci.Name),
			Namespace: gardenerutils.ComputeGardenNamespace(seedName),
		}}
		return nil
	}); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to reconcile ClusterRoleBinding %q: %w", clusterRoleBindingName, err)
	}

	return reconcile.Result{}, nil
}

func (r *Reconciler) delete(ctx context.Context, ci *gardenercorev1beta1.ControllerInstallation) (reconcile.Result, error) {
	clusterRoleName := extensionClusterRoleName(ci.Name)

	if err := r.Client.Delete(ctx, &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName}}); client.IgnoreNotFound(err) != nil {
		return reconcile.Result{}, fmt.Errorf("failed to delete ClusterRoleBinding %q: %w", clusterRoleName, err)
	}

	if err := r.Client.Delete(ctx, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName}}); client.IgnoreNotFound(err) != nil {
		return reconcile.Result{}, fmt.Errorf("failed to delete ClusterRole %q: %w", clusterRoleName, err)
	}

	return reconcile.Result{}, controllerutils.RemoveFinalizers(ctx, r.Client, ci, FinalizerName)
}

func extensionClusterRoleName(ciName string) string {
	return clusterRolePrefix + ciName
}

func extensionSAName(ciName string) string {
	return "extension-" + ciName
}
