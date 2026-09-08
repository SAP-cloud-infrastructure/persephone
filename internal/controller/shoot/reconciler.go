package shoot

import (
	"context"
	"fmt"

	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/controllerutils"
	gardenerutils "github.com/gardener/gardener/pkg/utils/gardener"
	kubernetesutils "github.com/gardener/gardener/pkg/utils/kubernetes"
	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/clock"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/sap-cloud-infrastructure/persephone/internal/constants"
	openstacklocal "github.com/sap-cloud-infrastructure/persephone/internal/openstack"
)

// FinalizerName is a constant for the name of a finalizer used by this controller.
const FinalizerName = "persephone.sci.cloud.sap/shoot-controller"

// Reconciler reconciles Shoot resources.
type Reconciler struct {
	// Client is the controller-runtime Kubernetes client.
	Client client.Client
	// OpenStackClientSet is a mapping of OpenStack region to respective client set.
	OpenStackClientSet openstacklocal.RegionOpenStackClientSet
	// Clock is a clock.
	Clock clock.Clock
	// LandscapeName is the name of the landscape.
	LandscapeName string
}

// Reconcile reconciles Shoot resources.
func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := logf.FromContext(ctx)

	shoot := &gardenercorev1beta1.Shoot{}
	if err := r.Client.Get(ctx, request.NamespacedName, shoot); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(1).Info("Object is gone, stop reconciling")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("error retrieving object from store: %w", err)
	}

	if shoot.DeletionTimestamp != nil {
		return r.delete(ctx, log, shoot)
	}

	return r.reconcile(ctx, log, shoot)
}

func (r *Reconciler) reconcile(ctx context.Context, log logr.Logger, shoot *gardenercorev1beta1.Shoot) (reconcile.Result, error) {
	if !controllerutil.ContainsFinalizer(shoot, FinalizerName) {
		if err := controllerutils.AddFinalizers(ctx, r.Client, shoot, FinalizerName); err != nil {
			return reconcile.Result{}, fmt.Errorf("error adding finalizer to Shoot: %w", err)
		}
	}

	newestInternalSecret, err := kubernetesutils.NewestObject(ctx, r.Client, &gardenercorev1beta1.InternalSecretList{}, nil, client.InNamespace(shoot.Namespace), client.MatchingLabels{constants.LabelKeyShootName: shoot.Name})
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to list InternalSecrets for Shoot %q: %w", client.ObjectKeyFromObject(shoot), err)
	}

	if newestInternalSecret == nil {
		return reconcile.Result{}, nil
	}

	// For each Secret, there is a corresponding CredentialsBinding with the same name created by the 'internalsecret'
	// controller.
	credentialsBindingName := newestInternalSecret.GetName()

	switch {
	case ptr.Deref(shoot.Spec.CredentialsBindingName, "") == credentialsBindingName:
		log.Info("Shoot already reference the newest InternalSecret containing OpenStack application credentials, nothing to do", "credentialsBindingName", *shoot.Spec.CredentialsBindingName)
		return reconcile.Result{}, nil

	case shoot.Annotations[constants.AnnotationKeyForceCredentialsBindingUpdate] == "true":
		log.Info("Shoot is marked for forceful credentials binding update")
		return reconcile.Result{}, r.patchCredentialsBindingName(ctx, log, shoot, credentialsBindingName)

	case ptr.Deref(shoot.Spec.Maintenance.ConfineSpecUpdateRollout, false):
		log.Info("Shoot confines spec update rollouts into maintenance time window")
		return reconcile.Result{}, r.patchCredentialsBindingName(ctx, log, shoot, credentialsBindingName)

	case gardenerutils.IsNowInEffectiveShootMaintenanceTimeWindow(shoot, r.Clock):
		log.Info("Current time is within the Shoot's maintenance time window")
		return reconcile.Result{}, r.patchCredentialsBindingName(ctx, log, shoot, credentialsBindingName)

	default:
		var (
			now             = r.Clock.Now().UTC()
			duration        = gardenerutils.EffectiveShootMaintenanceTimeWindow(shoot).RandomDurationUntilNext(now, false)
			nextMaintenance = now.Add(duration)
		)

		log.Info("Current time is outside of Shoot's maintenance time window, requeuing", "durationUntilMaintenanceTimeWindow", duration, "nextMaintenance", nextMaintenance.String())
		return reconcile.Result{RequeueAfter: duration}, nil
	}
}

func (r *Reconciler) patchCredentialsBindingName(ctx context.Context, log logr.Logger, shoot *gardenercorev1beta1.Shoot, credentialsBindingName string) error {
	log.Info("Patching Shoot's .spec.credentialsBindingName", "credentialsBindingName", credentialsBindingName)

	patch := client.MergeFrom(shoot.DeepCopy())
	shoot.Spec.CredentialsBindingName = &credentialsBindingName
	delete(shoot.Annotations, constants.AnnotationKeyForceCredentialsBindingUpdate)
	return r.Client.Patch(ctx, shoot, patch)
}

func (r *Reconciler) delete(ctx context.Context, log logr.Logger, shoot *gardenercorev1beta1.Shoot) (reconcile.Result, error) {
	if shoot.Status.LastOperation == nil ||
		shoot.Status.LastOperation.Type != gardenercorev1beta1.LastOperationTypeDelete ||
		shoot.Status.LastOperation.State != gardenercorev1beta1.LastOperationStateSucceeded {
		log.V(1).Info("Shoot has not yet been successfully deleted, nothing to be done", "lastOperation", shoot.Status.LastOperation)
		return reconcile.Result{}, nil
	}

	log.Info("Shoot status indicates successful deletion, deleting all InternalSecrets for Shoot", "lastOperation", shoot.Status.LastOperation)
	if err := r.Client.DeleteAllOf(ctx, &gardenercorev1beta1.InternalSecret{}, client.InNamespace(shoot.Namespace), client.MatchingLabels{constants.LabelKeyShootName: shoot.Name}); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed deleting all InternalSecrets for Shoot: %w", err)
	}

	log.Info("Deletion triggered, removing finalizer from Shoot")
	return reconcile.Result{}, controllerutils.RemoveFinalizers(ctx, r.Client, shoot, FinalizerName)
}
