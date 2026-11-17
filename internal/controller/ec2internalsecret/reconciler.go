// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package ec2internalsecret

import (
	"context"
	"fmt"
	"net/http"
	"time"

	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/controllerutils"
	kubernetesutils "github.com/gardener/gardener/pkg/utils/kubernetes"
	"github.com/go-logr/logr"
	"github.com/gophercloud/gophercloud/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/sap-cloud-infrastructure/persephone/internal/constants"
	kuberneteslocal "github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
	openstacklocal "github.com/sap-cloud-infrastructure/persephone/internal/openstack"
)

const (
	// FinalizerName is a constant for the name of a finalizer used by this controller.
	FinalizerName = "persephone.sci.cloud.sap/ec2-secret-controller"
	// RotationThreshold is the fraction of EC2CredentialsValidity after which rotation is triggered.
	RotationThreshold = 0.5
)

// Reconciler reconciles EC2 credential InternalSecret resources.
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

// Reconcile reconciles EC2 InternalSecret resources.
func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := logf.FromContext(ctx)

	internalSecret := &gardenercorev1beta1.InternalSecret{}
	if err := r.Client.Get(ctx, request.NamespacedName, internalSecret); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(1).Info("Object is gone, stop reconciling")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("error retrieving object from store: %w", err)
	}

	shoot := &gardenercorev1beta1.Shoot{Name: internalSecret.Labels[constants.LabelKeyShootName], Namespace: internalSecret.Namespace}
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(shoot), shoot); client.IgnoreNotFound(err) != nil {
		return reconcile.Result{}, fmt.Errorf("failed to retrieve Shoot for EC2 InternalSecret: %w", err)
	}

	if internalSecret.DeletionTimestamp != nil {
		return r.delete(ctx, log, internalSecret, shoot)
	}

	return r.reconcile(ctx, log, internalSecret, shoot)
}

func (r *Reconciler) reconcile(ctx context.Context, log logr.Logger, internalSecret *gardenercorev1beta1.InternalSecret, shoot *gardenercorev1beta1.Shoot) (reconcile.Result, error) {
	if err := controllerutils.AddFinalizers(ctx, r.Client, internalSecret, FinalizerName); err != nil {
		return reconcile.Result{}, fmt.Errorf("error adding finalizer to EC2 InternalSecret: %w", err)
	}

	var (
		now       = r.Clock.Now().UTC()
		created   = internalSecret.CreationTimestamp.UTC()
		renewTime = created.Add(time.Duration(RotationThreshold * float64(constants.EC2CredentialsValidity)))
		expiresAt = created.Add(constants.EC2CredentialsValidity)
	)

	log = log.WithValues("renewTime", renewTime.String(), "expiresAt", expiresAt.String())

	if now.Before(renewTime) {
		durationUntilRenewTime := renewTime.Sub(now)
		log.Info("EC2 InternalSecret does not need rotation yet, requeuing", "durationUntilRenewTime", durationUntilRenewTime)
		return reconcile.Result{RequeueAfter: durationUntilRenewTime}, nil
	}

	// Past renew time — ensure a newer EC2 InternalSecret exists.
	newerExists, err := kubernetesutils.NewestObject(ctx, r.Client, &gardenercorev1beta1.InternalSecretList{},
		func(obj client.Object) bool { return obj.GetCreationTimestamp().UTC().After(renewTime) },
		client.InNamespace(internalSecret.Namespace),
		client.MatchingLabels{constants.LabelKeyShootName: shoot.Name, constants.LabelKeyEC2Credentials: "true"},
	)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to list EC2 InternalSecrets for Shoot: %w", err)
	}

	if newerExists != nil {
		log.Info("Newer EC2 InternalSecret already exists", "newer", client.ObjectKeyFromObject(newerExists))
	} else {
		if err := r.createNewEC2InternalSecret(ctx, log, internalSecret, shoot); err != nil {
			return reconcile.Result{}, err
		}
	}

	if !now.Before(expiresAt) {
		log.Info("EC2 InternalSecret has expired, deleting")
		return reconcile.Result{}, r.Client.Delete(ctx, internalSecret)
	}

	return reconcile.Result{}, nil
}

func (r *Reconciler) createNewEC2InternalSecret(ctx context.Context, log logr.Logger, internalSecret *gardenercorev1beta1.InternalSecret, shoot *gardenercorev1beta1.Shoot) error {
	openStackUserInfo, err := kuberneteslocal.OpenStackUserInfoFromLabels(internalSecret.Labels)
	if err != nil {
		return fmt.Errorf("failed to get OpenStack user info from InternalSecret labels: %w", err)
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

	newSecret := kuberneteslocal.EC2CredentialsInternalSecret(shoot, openStackUserInfo, accessKey, secretKey)
	log.Info("Creating new EC2 InternalSecret for rotation", "newInternalSecret", client.ObjectKeyFromObject(newSecret))
	if err := r.Client.Create(ctx, newSecret); err != nil {
		return fmt.Errorf("failed to create new EC2 InternalSecret: %w", err)
	}

	return nil
}

func (r *Reconciler) delete(ctx context.Context, log logr.Logger, internalSecret *gardenercorev1beta1.InternalSecret, shoot *gardenercorev1beta1.Shoot) (reconcile.Result, error) {
	// Block deletion if this is the only EC2 InternalSecret and the Shoot still exists and needs it.
	if shootExists := len(shoot.UID) > 0; shootExists {
		isOnly, err := r.isOnlyEC2InternalSecretForShoot(ctx, internalSecret, shoot)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to check remaining EC2 InternalSecrets for Shoot: %w", err)
		}
		if isOnly {
			log.Info("EC2 InternalSecret is the only one for its Shoot, not deleting the underlying credential yet")
			return reconcile.Result{}, controllerutils.RemoveFinalizers(ctx, r.Client, internalSecret, FinalizerName)
		}
	}

	if err := r.deleteEC2Credential(ctx, log, internalSecret, shoot); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, controllerutils.RemoveFinalizers(ctx, r.Client, internalSecret, FinalizerName)
}

func (r *Reconciler) deleteEC2Credential(ctx context.Context, log logr.Logger, internalSecret *gardenercorev1beta1.InternalSecret, shoot *gardenercorev1beta1.Shoot) error {
	openStackUserInfo, err := kuberneteslocal.OpenStackUserInfoFromLabels(internalSecret.Labels)
	if err != nil {
		return fmt.Errorf("failed to get OpenStack user info from InternalSecret labels: %w", err)
	}

	openStackClientSet, ok := r.OpenStackClientSet[openStackUserInfo.Region]
	if !ok {
		return fmt.Errorf("region %q not supported", openStackUserInfo.Region)
	}

	clusterUserName := kuberneteslocal.GetClusterUserName(r.LandscapeName, shoot.Name, openStackUserInfo.ProjectID)
	clusterUserClientSet, err := openstacklocal.NewClusterUserClientSet(ctx, openStackClientSet, clusterUserName, openStackUserInfo.ProjectID)
	if err != nil {
		return fmt.Errorf("failed to create cluster user client set for deletion: %w", err)
	}

	accessKey := string(internalSecret.Data[constants.EC2AccessKey])
	if err := clusterUserClientSet.DeleteEC2Credential(ctx, accessKey); err != nil && !gophercloud.ResponseCodeIs(err, http.StatusNotFound) {
		return fmt.Errorf("failed to delete EC2 credential %q: %w", accessKey, err)
	}
	log.Info("EC2 credential deleted", "accessKey", accessKey)

	return nil
}

func (r *Reconciler) isOnlyEC2InternalSecretForShoot(ctx context.Context, internalSecret *gardenercorev1beta1.InternalSecret, shoot *gardenercorev1beta1.Shoot) (bool, error) {
	list := &gardenercorev1beta1.InternalSecretList{}
	if err := r.Client.List(ctx, list,
		client.InNamespace(internalSecret.Namespace),
		client.MatchingLabels{constants.LabelKeyShootName: shoot.Name, constants.LabelKeyEC2Credentials: "true"},
	); err != nil {
		return false, fmt.Errorf("failed to list EC2 InternalSecrets for Shoot: %w", err)
	}

	for _, item := range list.Items {
		if item.Name == internalSecret.Name && item.UID == internalSecret.UID {
			continue
		}
		return false, nil
	}

	return true, nil
}
