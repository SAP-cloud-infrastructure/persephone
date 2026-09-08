package project

import (
	"context"
	"fmt"

	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/sap-cloud-infrastructure/persephone/internal/config"
	"github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
)

// Reconciler reconciles Project resources.
type Reconciler struct {
	// Client is the controller-runtime Kubernetes client.
	Client client.Client
	// PersephoneConfig is the configuration for Persephone.
	PersephoneConfig config.PersephoneConfig
	// metrics holds the Prometheus metrics.
	metrics *metrics
}

// Reconcile reconciles Project resources.
func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	// Lazy initialize metrics
	if r.metrics == nil {
		r.metrics = newMetrics(ctrlmetrics.Registry)
	}

	log := logf.FromContext(ctx)

	project := &gardenercorev1beta1.Project{}
	if err := r.Client.Get(ctx, request.NamespacedName, project); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(1).Info("Object is gone, stop reconciling")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("error retrieving object from store: %w", err)
	}

	if project.DeletionTimestamp != nil {
		return reconcile.Result{}, nil
	}

	log.Info("Reconciling Gardener Project resources")

	// Under normal operation Persephone's token webhook creates the Project object with
	// Spec.Namespace already set. A nil Spec.Namespace therefore indicates an anomalous
	// Project (e.g. created manually or by another controller). Since this controller only
	// reacts to Create events — not Update events — this object will not be re-processed
	// if it is later updated; it will only be retried on the next operator restart.
	if project.Spec.Namespace == nil {
		// The expected namespace name (e.g. garden-{region}-{projectID}) cannot be determined here
		// because it requires OpenStack labels from the Namespace, which we have not read yet.
		log.Error(nil, "Project has no Spec.Namespace, cannot reconcile; will retry on next operator restart", "project", project.Name)
		return reconcile.Result{}, nil
	}

	ns := &corev1.Namespace{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: *project.Spec.Namespace}, ns); err != nil {
		return reconcile.Result{}, fmt.Errorf("error retrieving project namespace %s for project %s: %w", *project.Spec.Namespace, project.Name, err)
	}

	openStackUserInfo, err := kubernetes.OpenStackUserInfoFromLabels(ns.Labels)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("error retrieving OpenStackUserInfo from Namespace labels: %w", err)
	}

	region := openStackUserInfo.Region
	err = kubernetes.ReconcileGardenerProjectResources(ctx, r.Client, r.PersephoneConfig, openStackUserInfo)

	// Record metrics
	if err != nil {
		r.metrics.recordResourceCreation(region, "error")
	} else {
		r.metrics.recordResourceCreation(region, "success")
	}

	return reconcile.Result{}, err
}
