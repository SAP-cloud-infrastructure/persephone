package internalsecret

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/gardener/gardener-extension-provider-openstack/pkg/openstack"
	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/controllerutils"
	kubernetesutils "github.com/gardener/gardener/pkg/utils/kubernetes"
	"github.com/go-logr/logr"
	"github.com/gophercloud/gophercloud/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/clock"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/sap-cloud-infrastructure/persephone/internal/constants"
	"github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
	openstacklocal "github.com/sap-cloud-infrastructure/persephone/internal/openstack"
)

const (
	// FinalizerName is a constant for the name of a finalizer used by this controller.
	FinalizerName = "persephone.sci.cloud.sap/secret-controller"
	// RotationThreshold is the fraction of a Secret's total validity after which new credentials should be generated.
	RotationThreshold = 0.5

	// gracePeriodSecretAge is the grace period for how long a Secret must exist before it can be cleaned up in case
	// there is no Shoot using it.
	gracePeriodSecretAge = time.Hour
)

var (
	// RequeueAfterWhenSecretStillInUse is the duration with which a Secret with a deletion timestamp will be requeued
	// in case it is still in use by its Shoot.
	RequeueAfterWhenSecretStillInUse = 5 * time.Minute
	// RequeueAfterWhenSecretOwnsClusterServiceUser is the duration with which a Secret with a deletion timestamp will
	// be requeued in case it owns the OpenStack cluster service user but is not the last Secret existing in the system
	// for the respective Shoot cluster it belongs to.
	RequeueAfterWhenSecretOwnsClusterServiceUser = 10 * time.Second
)

// Reconciler reconciles Secret resources.
type Reconciler struct {
	// Client is the controller-runtime Kubernetes client.
	Client client.Client
	// OpenStackClientSet is a mapping of OpenStack region to respective client set.
	OpenStackClientSet openstacklocal.RegionOpenStackClientSet
	// Clock is a clock.
	Clock clock.Clock
	// LandscapeName is the name of the landscape.
	LandscapeName string
	// metrics holds the Prometheus metrics.
	metrics *metrics
}

// Reconcile reconciles Secret resources.
func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	// Lazy initialize metrics
	if r.metrics == nil {
		r.metrics = newMetrics(ctrlmetrics.Registry)
	}

	log := logf.FromContext(ctx)

	internalSecret := &gardenercorev1beta1.InternalSecret{}
	if err := r.Client.Get(ctx, request.NamespacedName, internalSecret); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(1).Info("Object is gone, stop reconciling")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("error retrieving object from store: %w", err)
	}

	shoot := &gardenercorev1beta1.Shoot{ObjectMeta: metav1.ObjectMeta{Name: internalSecret.Labels[constants.LabelKeyShootName], Namespace: internalSecret.Namespace}}
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(shoot), shoot); client.IgnoreNotFound(err) != nil {
		return reconcile.Result{}, fmt.Errorf("failed to retrieve Shoot %q for InternalSecret: %w", client.ObjectKeyFromObject(shoot), err)
	}

	log = log.WithValues("shoot", client.ObjectKeyFromObject(shoot))

	expiresAt := internalSecret.CreationTimestamp.UTC()

	if expiresAtRaw, ok := internalSecret.Annotations[constants.AnnotationKeyExpiresAt]; ok {
		var err error
		expiresAt, err = time.Parse(time.RFC3339, expiresAtRaw)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to parse %q annotation (%s) on Secret: %w", constants.AnnotationKeyExpiresAt, expiresAtRaw, err)
		}
		expiresAt = expiresAt.UTC()
	}

	var result reconcile.Result
	var err error

	if internalSecret.DeletionTimestamp != nil {
		result, err = r.delete(ctx, log, internalSecret, shoot, expiresAt)
	} else {
		result, err = r.reconcile(ctx, log, internalSecret, shoot, expiresAt)
	}

	return result, err
}

func (r *Reconciler) reconcile(ctx context.Context, log logr.Logger, internalSecret *gardenercorev1beta1.InternalSecret, shoot *gardenercorev1beta1.Shoot, expiresAt time.Time) (reconcile.Result, error) {
	if err := controllerutils.AddFinalizers(ctx, r.Client, internalSecret, FinalizerName); err != nil {
		return reconcile.Result{}, fmt.Errorf("error adding finalizer to Secret: %w", err)
	}

	if shootExists := len(shoot.UID) > 0; !shootExists {
		age := r.Clock.Now().UTC().Sub(internalSecret.CreationTimestamp.UTC())
		if age > gracePeriodSecretAge {
			log.Info("Shoot does not exist and Secret is older than 1h, deleting Secret to trigger cleanup flow", "internalSecretAge", age)
			return reconcile.Result{}, r.Client.Delete(ctx, internalSecret)
		}
		log.Info("Shoot does not exist anymore (or yet), but Secret is younger than 1h, proceeding with regular flow", "internalSecretAge", age)
	}

	var (
		now           = r.Clock.Now().UTC()
		totalValidity = expiresAt.Sub(internalSecret.CreationTimestamp.UTC())
		renewTime     = internalSecret.CreationTimestamp.UTC().Add(time.Duration(RotationThreshold * float64(totalValidity)))
	)

	log = log.WithValues(
		"expiresAt", expiresAt.String(),
		"renewTime", renewTime.String(),
	)

	if now.Before(renewTime) {
		return r.handleNowBeforeRenewTime(ctx, log, internalSecret, shoot, renewTime)
	}

	if err := r.handleNowAfterRenewTime(ctx, log, internalSecret, shoot, renewTime); err != nil {
		return reconcile.Result{}, err
	}

	if !now.Before(expiresAt) {
		return reconcile.Result{}, r.handleCredentialExpiry(ctx, log, internalSecret)
	}

	return reconcile.Result{}, nil
}

func (r *Reconciler) handleCredentialExpiry(ctx context.Context, log logr.Logger, internalSecret *gardenercorev1beta1.InternalSecret) error {
	log.Info("InternalSecret is expired, deleting it to trigger cleanup flow")
	return r.Client.Delete(ctx, internalSecret)
}

func (r *Reconciler) handleNowBeforeRenewTime(ctx context.Context, log logr.Logger, internalSecret *gardenercorev1beta1.InternalSecret, shoot *gardenercorev1beta1.Shoot, renewTime time.Time) (reconcile.Result, error) {
	if _, err := kubernetes.EnsureCredentialsBindingForInternalSecret(ctx, log, r.Client, shoot, internalSecret); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to ensure OpenStack credentials InternalSecret and CredentialsBinding: %w", err)
	}

	if shootExists := len(shoot.UID) > 0; !shootExists {
		return reconcile.Result{RequeueAfter: gracePeriodSecretAge}, nil
	}

	durationUntilRenewTime := renewTime.Sub(internalSecret.CreationTimestamp.UTC())
	log.Info("InternalSecret should be not be renewed yet, ensuring CredentialsBinding and requeue", "durationUntilRenewTime", durationUntilRenewTime)

	return reconcile.Result{RequeueAfter: durationUntilRenewTime}, nil
}

func (r *Reconciler) handleNowAfterRenewTime(ctx context.Context, log logr.Logger, internalSecret *gardenercorev1beta1.InternalSecret, shoot *gardenercorev1beta1.Shoot, renewTime time.Time) error {
	log.Info("Secret should be renewed")

	region := internalSecret.Labels[constants.LabelKeyOpenStackRegion]

	newestInternalSecret, err := kubernetesutils.NewestObject(ctx, r.Client, &gardenercorev1beta1.InternalSecretList{},
		func(object client.Object) bool {
			return object.GetCreationTimestamp().UTC().After(renewTime.UTC())
		},
		client.InNamespace(internalSecret.Namespace),
		client.MatchingLabels{constants.LabelKeyShootName: shoot.Name},
	)
	if err != nil {
		return fmt.Errorf("failed to list InternalSecrets for Shoot %q: %w", client.ObjectKeyFromObject(shoot), err)
	}

	if newestInternalSecret != nil {
		log.Info("InternalSecret found with a creation timestamp after the renew time, nothing to be done", "newestInternalSecret", client.ObjectKeyFromObject(newestInternalSecret))
		return nil
	}

	log.Info("Creating new InternalSecret")

	userClientSet, openStackUserInfo, err := r.newOpenStackClusterUserClientSet(ctx, internalSecret, shoot, false)
	if err != nil {
		r.metrics.recordCredentialRotation(region, "error")
		return fmt.Errorf("could not create cluster user clients: %w", err)
	}

	if _, err := kubernetes.EnsureApplicationCredentialInternalSecretForShoot(ctx, log, r.Client, userClientSet, shoot, openStackUserInfo); err != nil {
		r.metrics.recordCredentialRotation(region, "error")
		return fmt.Errorf("failed ensuring a new application credential InternalSecret %q: %w", client.ObjectKeyFromObject(internalSecret), err)
	}

	r.metrics.recordCredentialRotation(region, "success")
	return nil
}

func (r *Reconciler) newOpenStackClusterUserClientSet(ctx context.Context, secret *gardenercorev1beta1.InternalSecret, shoot *gardenercorev1beta1.Shoot, checkForExistence bool) (*openstacklocal.ClusterUserClientSet, kubernetes.OpenStackUserInfo, error) {
	openStackUserInfo, err := kubernetes.OpenStackUserInfoFromLabels(secret.Labels)
	if err != nil {
		return nil, kubernetes.OpenStackUserInfo{}, fmt.Errorf("failed getting OpenStack user info from labels: %w", err)
	}

	openStackClientSet, ok := r.OpenStackClientSet[openStackUserInfo.Region]
	if !ok {
		return nil, kubernetes.OpenStackUserInfo{}, fmt.Errorf("region %q not supported", openStackUserInfo.Region)
	}

	clusterUserName := kubernetes.GetClusterUserName(r.LandscapeName, shoot.Name, openStackUserInfo.ProjectID)

	if checkForExistence {
		if clusterUser, err := openStackClientSet.GetUserByName(ctx, clusterUserName); err != nil {
			return nil, kubernetes.OpenStackUserInfo{}, fmt.Errorf("failed to check if cluster service user %q exists: %w", clusterUserName, err)
		} else if clusterUser == nil {
			return nil, kubernetes.OpenStackUserInfo{}, nil
		}
	}

	clusterUserClientSet, err := openstacklocal.NewClusterUserClientSet(ctx, openStackClientSet, clusterUserName, openStackUserInfo.ProjectID)
	if err != nil {
		return nil, kubernetes.OpenStackUserInfo{}, fmt.Errorf("failed to create new cluster user client set for user %q: %w", clusterUserName, err)
	}

	return clusterUserClientSet, openStackUserInfo, nil
}

func (r *Reconciler) delete(ctx context.Context, log logr.Logger, internalSecret *gardenercorev1beta1.InternalSecret, shoot *gardenercorev1beta1.Shoot, expiresAt time.Time) (reconcile.Result, error) {
	region := internalSecret.Labels[constants.LabelKeyOpenStackRegion]

	isInternalSecretDeletionAllowed, reason := r.internalSecretDeletionAllowed(internalSecret, shoot, expiresAt)
	if !isInternalSecretDeletionAllowed {
		log.Info("InternalSecret has deletion timestamp but cannot delete yet, requeuing", "requeueAfter", RequeueAfterWhenSecretStillInUse, "reason", reason)
		return reconcile.Result{RequeueAfter: RequeueAfterWhenSecretStillInUse}, nil
	}

	log.Info("InternalSecret has deletion timestamp and can be deleted, cleaning up", "reason", reason)

	credentialsBinding := kubernetes.CredentialsBindingForSecret(shoot, internalSecret)
	if err := r.Client.Delete(ctx, credentialsBinding); client.IgnoreNotFound(err) != nil {
		r.metrics.recordCredentialCleanup(region, "error")
		return reconcile.Result{}, fmt.Errorf("failed to delete CredentialsBinding %q: %w", client.ObjectKeyFromObject(credentialsBinding), err)
	}
	log.Info("CredentialsBinding deleted", "credentialsBinding", client.ObjectKeyFromObject(credentialsBinding))

	if err := r.deleteApplicationCredential(ctx, log, internalSecret, shoot); err != nil {
		r.metrics.recordCredentialCleanup(region, "error")
		return reconcile.Result{}, fmt.Errorf("failed to delete OpenStack application credential: %w", err)
	}
	log.Info("OpenStack application credential deleted")

	if secretIsClusterServiceUserOwner, err := r.secretOwnsClusterServiceUser(ctx, internalSecret, shoot); err != nil {
		r.metrics.recordCredentialCleanup(region, "error")
		return reconcile.Result{}, fmt.Errorf("failed to check if InternalSecret owns the OpenStack cluster service user: %w", err)
	} else if secretIsClusterServiceUserOwner {
		log.Info("Secret owns the OpenStack cluster service user - checking if it can be deleted")

		if internalSecretIsTheOnlyExistingForShoot, err := r.isOnlyRemainingInternalSecretForShoot(ctx, internalSecret, shoot); err != nil {
			r.metrics.recordCredentialCleanup(region, "error")
			return reconcile.Result{}, fmt.Errorf("failed to check if the InternalSecret is the only one remaining for the Shoot %q: %w", client.ObjectKeyFromObject(shoot), err)
		} else if !internalSecretIsTheOnlyExistingForShoot {
			log.Info("Other InternalSecrets for the same Shoot still exist, must wait until they are deleted, requeuing", "requeueAfter", RequeueAfterWhenSecretOwnsClusterServiceUser)
			return reconcile.Result{RequeueAfter: RequeueAfterWhenSecretOwnsClusterServiceUser}, nil
		}

		log.Info("InternalSecret is the last one existing for the Shoot - deleting OpenStack cluster service user")
		if err := r.deleteClusterServiceUser(ctx, internalSecret, shoot); err != nil {
			r.metrics.recordCredentialCleanup(region, "error")
			return reconcile.Result{}, fmt.Errorf("failed to ensure that the cluster user is deleted: %w", err)
		}
	}

	r.metrics.recordCredentialCleanup(region, "success")
	return reconcile.Result{}, controllerutils.RemoveFinalizers(ctx, r.Client, internalSecret, FinalizerName)
}

func (r *Reconciler) internalSecretDeletionAllowed(internalSecret *gardenercorev1beta1.InternalSecret, shoot *gardenercorev1beta1.Shoot, expiresAt time.Time) (ok bool, reason string) {
	if shoot.Status.LastOperation != nil &&
		shoot.Status.LastOperation.Type == gardenercorev1beta1.LastOperationTypeDelete &&
		shoot.Status.LastOperation.State == gardenercorev1beta1.LastOperationStateSucceeded {
		return true, "Shoot has been deleted successfully"
	}

	if isExpired := r.Clock.Now().UTC().After(expiresAt) || r.Clock.Now().UTC().Equal(expiresAt); isExpired {
		return true, "Secret is expired at " + expiresAt.Format(time.RFC3339)
	}

	if credentialsBinding := kubernetes.CredentialsBindingForSecret(shoot, internalSecret); ptr.Deref(shoot.Spec.CredentialsBindingName, "") != credentialsBinding.Name {
		return true, "Shoot no longer references the CredentialsBinding " + credentialsBinding.Name
	}

	return false, "Secret is still in use"
}

// The current InternalSecret owns the OpenStack cluster service user if it is the newest/youngest InternalSecret.
func (r *Reconciler) secretOwnsClusterServiceUser(ctx context.Context, internalSecret *gardenercorev1beta1.InternalSecret, shoot *gardenercorev1beta1.Shoot) (bool, error) {
	newestInternalSecret, err := kubernetesutils.NewestObject(ctx, r.Client, &gardenercorev1beta1.InternalSecretList{}, nil, client.InNamespace(internalSecret.Namespace), client.MatchingLabels{constants.LabelKeyShootName: shoot.Name})
	if err != nil {
		return false, fmt.Errorf("failed to list InternalSecrets for Shoot %q: %w", client.ObjectKeyFromObject(shoot), err)
	}

	return newestInternalSecret.GetName() == internalSecret.Name && newestInternalSecret.GetUID() == internalSecret.UID, nil
}

func (r *Reconciler) isOnlyRemainingInternalSecretForShoot(ctx context.Context, internalSecret *gardenercorev1beta1.InternalSecret, shoot *gardenercorev1beta1.Shoot) (bool, error) {
	internalSecretList := &gardenercorev1beta1.InternalSecretList{}
	if err := r.Client.List(ctx, internalSecretList, client.InNamespace(internalSecret.Namespace), client.MatchingLabels{constants.LabelKeyShootName: shoot.Name}); err != nil {
		return false, fmt.Errorf("failed to list all InternalSecrets for Shoot: %w", err)
	}

	internalSecretList.Items = slices.DeleteFunc(internalSecretList.Items, func(s gardenercorev1beta1.InternalSecret) bool {
		return s.Name == internalSecret.Name && s.UID == internalSecret.UID
	})

	return len(internalSecretList.Items) == 0, nil
}

func (r *Reconciler) deleteApplicationCredential(ctx context.Context, log logr.Logger, internalSecret *gardenercorev1beta1.InternalSecret, shoot *gardenercorev1beta1.Shoot) error {
	userClientSet, _, err := r.newOpenStackClusterUserClientSet(ctx, internalSecret, shoot, true)
	if err != nil {
		return fmt.Errorf("could not create cluster user clients: %w", err)
	}

	if userClientSet == nil {
		log.Info("Cluster user not found, related application credentials are gone as well")
		return nil
	}

	applicationCredentialID := string(internalSecret.Data[openstack.ApplicationCredentialID])
	if err := userClientSet.DeleteApplicationCredential(ctx, applicationCredentialID); err != nil && !gophercloud.ResponseCodeIs(err, http.StatusNotFound) {
		return fmt.Errorf("failed to delete OpenStack application credentials %q: %w", applicationCredentialID, err)
	}
	log.Info("OpenStack application credential deleted", "applicationCredentialID", applicationCredentialID)

	return nil
}

func (r *Reconciler) deleteClusterServiceUser(ctx context.Context, internalSecret *gardenercorev1beta1.InternalSecret, shoot *gardenercorev1beta1.Shoot) error {
	openStackUserInfo, err := kubernetes.OpenStackUserInfoFromLabels(internalSecret.Labels)
	if err != nil {
		return fmt.Errorf("failed getting OpenStack user info from labels: %w", err)
	}

	openStackClientSet, ok := r.OpenStackClientSet[openStackUserInfo.Region]
	if !ok {
		return fmt.Errorf("region %q not supported", openStackUserInfo.Region)
	}

	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: shoot.Namespace}}
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(namespace), namespace); err != nil {
		return fmt.Errorf("failed to get namespace %q: %w", namespace.Name, err)
	}

	clusterUserName := kubernetes.GetClusterUserName(r.LandscapeName, shoot.Name, namespace.Labels[constants.LabelKeyOpenStackProjectID])
	if err := openStackClientSet.DeleteClusterServiceUser(ctx, clusterUserName); err != nil && !gophercloud.ResponseCodeIs(err, http.StatusNotFound) {
		return fmt.Errorf("failed to delete OpenStack cluster service user %q: %w", clusterUserName, err)
	}

	return nil
}
