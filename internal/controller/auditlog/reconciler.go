// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/gardener/gardener/pkg/controllerutils"

	gardencorev1 "github.com/gardener/gardener/pkg/apis/core/v1"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	operatorv1alpha1 "github.com/gardener/gardener/pkg/apis/operator/v1alpha1"
	resourcesv1alpha1 "github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
)

const (
	// ControllerName is the name of this controller.
	ControllerName = "auditlog-extension"

	// gardenNamespace is the namespace in the virtual garden where the Extension CR lives.
	gardenNamespace = "garden"

	// managedResourceName is the name of the ManagedResource and its backing Secret.
	managedResourceName = "persephone-auditlog-extension"

	// extensionName is the name of the operator.gardener.cloud/v1alpha1 Extension CR.
	extensionName = "persephone-auditlog"
)

// Options configures the auditlog extension controller.
type Options struct {
	// AuditlogChartRef is the OCI ref for the persephone-auditlog Helm chart,
	// e.g. "keppel.eu-de-1.cloud.sap/ccloud-helm/persephone-auditlog:0.0.0-<sha>".
	AuditlogChartRef string
}

// Reconciler applies the operator.gardener.cloud/v1alpha1 Extension CR for persephone-auditlog
// into the virtual garden via a ManagedResource.
type Reconciler struct {
	client   client.Client
	chartRef string
}

// AddToManager registers the auditlog extension controller with the given manager.
func AddToManager(ctx context.Context, mgr manager.Manager, opts Options) error {
	_ = ctx

	rec := &Reconciler{
		client:   mgr.GetClient(),
		chartRef: opts.AuditlogChartRef,
	}

	ctrl, err := controller.New(ControllerName, mgr, controller.Options{
		Reconciler:   rec,
		RecoverPanic: ptr.To(true), //nolint:modernize
	})
	if err != nil {
		return fmt.Errorf("failed to create %s controller: %w", ControllerName, err)
	}

	if err := ctrl.Watch(controllerutils.EnqueueOnce); err != nil {
		return fmt.Errorf("failed to set up watch for %s controller: %w", ControllerName, err)
	}

	return nil
}

// Reconcile applies the ManagedResource carrying the Extension CR.
func (r *Reconciler) Reconcile(ctx context.Context, _ reconcile.Request) (reconcile.Result, error) {
	log := logf.FromContext(ctx).WithName(ControllerName)

	if err := r.reconcile(ctx, log); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *Reconciler) reconcile(ctx context.Context, log logr.Logger) error {
	log.Info("Reconciling auditlog Extension CR")

	extensionCR := r.buildExtensionCR()

	raw, err := json.Marshal(extensionCR)
	if err != nil {
		return fmt.Errorf("failed to marshal Extension CR: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      managedResourceName,
			Namespace: gardenNamespace,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.client, secret, func() error {
		secret.Type = corev1.SecretTypeOpaque
		secret.Data = map[string][]byte{
			"extension.yaml": raw,
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to reconcile ManagedResource secret: %w", err)
	}

	mr := &resourcesv1alpha1.ManagedResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      managedResourceName,
			Namespace: gardenNamespace,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.client, mr, func() error {
		// class "" (empty) means the virtual garden resource manager handles it
		mr.Spec.InjectLabels = map[string]string{
			"app.kubernetes.io/name": extensionName,
		}
		mr.Spec.KeepObjects = ptr.To(false)
		mr.Spec.SecretRefs = []corev1.LocalObjectReference{
			{Name: managedResourceName},
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to reconcile ManagedResource: %w", err)
	}

	log.Info("Successfully reconciled auditlog Extension CR")
	return nil
}

func (r *Reconciler) buildExtensionCR() *operatorv1alpha1.Extension {
	chartRef := r.chartRef

	return &operatorv1alpha1.Extension{
		TypeMeta: metav1.TypeMeta{
			APIVersion: operatorv1alpha1.SchemeGroupVersion.String(),
			Kind:       "Extension",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: extensionName,
		},
		Spec: operatorv1alpha1.ExtensionSpec{
			Resources: []gardencorev1beta1.ControllerResource{
				{
					Kind: "Extension",
					Type: extensionName,
				},
			},
			Deployment: &operatorv1alpha1.Deployment{
				ExtensionDeployment: &operatorv1alpha1.ExtensionDeploymentSpec{
					DeploymentSpec: operatorv1alpha1.DeploymentSpec{
						Helm: &operatorv1alpha1.ExtensionHelm{
							OCIRepository: &gardencorev1.OCIRepository{
								Ref: ptr.To(chartRef),
							},
						},
					},
					Policy: ptr.To(gardencorev1beta1.ControllerDeploymentPolicyOnDemand),
				},
			},
			},
	}
}
