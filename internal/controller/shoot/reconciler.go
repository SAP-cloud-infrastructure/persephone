// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package shoot

import (
	"context"
	"fmt"

	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/controllerutils"
	gardenerutils "github.com/gardener/gardener/pkg/utils/gardener"
	kubernetesutils "github.com/gardener/gardener/pkg/utils/kubernetes"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/clock"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/sap-cloud-infrastructure/persephone/internal/constants"
	kuberneteslocal "github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
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

	if err := r.ensureEC2InternalSecret(ctx, log, shoot); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to ensure EC2 InternalSecret for Shoot %q: %w", client.ObjectKeyFromObject(shoot), err)
	}

	newestInternalSecret, err := kubernetesutils.NewestObject(ctx, r.Client, &gardenercorev1beta1.InternalSecretList{},
		func(obj client.Object) bool {
			return obj.GetLabels()[constants.LabelKeyEC2Credentials] != "true" && obj.GetDeletionTimestamp() == nil
		},
		client.InNamespace(shoot.Namespace), client.MatchingLabels{constants.LabelKeyShootName: shoot.Name})
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to list InternalSecrets for Shoot %q: %w", client.ObjectKeyFromObject(shoot), err)
	}

	currentCredentialsBindingName := ptr.Deref(shoot.Spec.CredentialsBindingName, "")

	if newestInternalSecret == nil {
		// No non-deleted InternalSecret exists. If the shoot's current binding references one that is
		// being deleted, bootstrap fresh credentials to break the deadlock. Otherwise this is a valid
		// early state (shoot created before the first InternalSecret is issued) — leave it alone.
		if currentCredentialsBindingName != "" && r.isInternalSecretBeingDeleted(ctx, currentCredentialsBindingName, shoot.Namespace) {
			log.Info("Shoot references a deleted InternalSecret and no valid one exists, bootstrapping new OpenStack application credentials")
			if err := r.ensureOpenStackInternalSecret(ctx, log, shoot); err != nil {
				return reconcile.Result{}, fmt.Errorf("failed to bootstrap OpenStack InternalSecret for Shoot %q: %w", client.ObjectKeyFromObject(shoot), err)
			}
		}
		return reconcile.Result{}, nil
	}

	// For each Secret, there is a corresponding CredentialsBinding with the same name created by the 'internalsecret'
	// controller.
	credentialsBindingName := newestInternalSecret.GetName()

	currentInternalSecretIsBeingDeleted := currentCredentialsBindingName != "" &&
		currentCredentialsBindingName != credentialsBindingName &&
		r.isInternalSecretBeingDeleted(ctx, currentCredentialsBindingName, shoot.Namespace)

	switch {
	case currentCredentialsBindingName == credentialsBindingName:
		log.Info("Shoot already reference the newest InternalSecret containing OpenStack application credentials, nothing to do", "credentialsBindingName", currentCredentialsBindingName)
		return reconcile.Result{}, nil

	case shoot.Annotations[constants.AnnotationKeyForceCredentialsBindingUpdate] == "true":
		log.Info("Shoot is marked for forceful credentials binding update")
		return reconcile.Result{}, r.patchCredentialsBindingName(ctx, log, shoot, credentialsBindingName)

	case currentInternalSecretIsBeingDeleted:
		log.Info("Shoot references an InternalSecret that is being deleted, switching immediately", "currentCredentialsBindingName", currentCredentialsBindingName)
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

func (r *Reconciler) isInternalSecretBeingDeleted(ctx context.Context, name, namespace string) bool {
	secret := &gardenercorev1beta1.InternalSecret{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, secret); err != nil {
		return false
	}
	return secret.DeletionTimestamp != nil
}

func (r *Reconciler) ensureOpenStackInternalSecret(ctx context.Context, log logr.Logger, shoot *gardenercorev1beta1.Shoot) error {
	namespace := &corev1.Namespace{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: shoot.Namespace}, namespace); err != nil {
		return fmt.Errorf("failed to get namespace %q: %w", shoot.Namespace, err)
	}

	openStackUserInfo, err := kuberneteslocal.OpenStackUserInfoFromLabels(namespace.Labels)
	if err != nil {
		return fmt.Errorf("failed to get OpenStack user info from namespace labels: %w", err)
	}

	openStackClientSet, ok := r.OpenStackClientSet[openStackUserInfo.Region]
	if !ok {
		return fmt.Errorf("region %q not supported", openStackUserInfo.Region)
	}

	clusterUserName := kuberneteslocal.GetClusterUserName(r.LandscapeName, shoot.Name, openStackUserInfo.ProjectID)
	clusterUserClientSet, err := openstacklocal.NewClusterUserClientSet(ctx, openStackClientSet, clusterUserName, openStackUserInfo.ProjectID)
	if err != nil {
		return fmt.Errorf("failed to create cluster user client set: %w", err)
	}

	if _, err := kuberneteslocal.EnsureApplicationCredentialInternalSecretForShoot(ctx, log, r.Client, clusterUserClientSet, shoot, openStackUserInfo); err != nil {
		return fmt.Errorf("failed to create OpenStack application credential: %w", err)
	}

	return nil
}

func (r *Reconciler) ensureEC2InternalSecret(ctx context.Context, log logr.Logger, shoot *gardenercorev1beta1.Shoot) error {
	if !hasAuditLogExtension(shoot) {
		return nil
	}

	existing, err := kubernetesutils.NewestObject(ctx, r.Client, &gardenercorev1beta1.InternalSecretList{},
		func(obj client.Object) bool { return obj.GetDeletionTimestamp() == nil },
		client.InNamespace(shoot.Namespace),
		client.MatchingLabels{constants.LabelKeyShootName: shoot.Name, constants.LabelKeyEC2Credentials: "true"},
	)
	if err != nil {
		return fmt.Errorf("failed to list EC2 InternalSecrets: %w", err)
	}
	if existing != nil {
		return nil
	}

	namespace := &corev1.Namespace{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: shoot.Namespace}, namespace); err != nil {
		return fmt.Errorf("failed to get namespace %q: %w", shoot.Namespace, err)
	}

	openStackUserInfo, err := kuberneteslocal.OpenStackUserInfoFromLabels(namespace.Labels)
	if err != nil {
		return fmt.Errorf("failed to get OpenStack user info from namespace labels: %w", err)
	}

	openStackClientSet, ok := r.OpenStackClientSet[openStackUserInfo.Region]
	if !ok {
		return fmt.Errorf("region %q not supported", openStackUserInfo.Region)
	}

	clusterUserName := kuberneteslocal.GetClusterUserName(r.LandscapeName, shoot.Name, openStackUserInfo.ProjectID)
	clusterUserClientSet, err := openstacklocal.NewClusterUserClientSet(ctx, openStackClientSet, clusterUserName, openStackUserInfo.ProjectID)
	if err != nil {
		return fmt.Errorf("failed to create cluster user client set: %w", err)
	}

	accessKey, secretKey, err := clusterUserClientSet.EnsureEC2Credential(ctx)
	if err != nil {
		return fmt.Errorf("failed to create EC2 credential: %w", err)
	}

	internalSecret := kuberneteslocal.EC2CredentialsInternalSecret(shoot, openStackUserInfo, accessKey, secretKey)
	log.Info("Creating EC2 InternalSecret for Shoot", "internalSecret", client.ObjectKeyFromObject(internalSecret))
	if err := r.Client.Create(ctx, internalSecret); err != nil {
		return fmt.Errorf("failed to create EC2 InternalSecret: %w", err)
	}

	return nil
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

func hasAuditLogExtension(shoot *gardenercorev1beta1.Shoot) bool {
	for _, ext := range shoot.Spec.Extensions {
		if ext.Type == constants.AuditLogExtensionType && !ptr.Deref(ext.Disabled, false) {
			return true
		}
	}
	return false
}
