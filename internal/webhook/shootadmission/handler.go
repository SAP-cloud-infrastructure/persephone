// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package shootadmission

import (
	"context"
	"fmt"
	"slices"

	gardeneropenstackhelper "github.com/gardener/gardener-extension-provider-openstack/pkg/apis/openstack/helper"
	gardeneropenstackv1alpha1 "github.com/gardener/gardener-extension-provider-openstack/pkg/apis/openstack/v1alpha1"
	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	gardenersecurityv1alpha1 "github.com/gardener/gardener/pkg/apis/security/v1alpha1"
	kubernetesutils "github.com/gardener/gardener/pkg/utils/kubernetes"
	"github.com/go-logr/logr"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/external"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/sap-cloud-infrastructure/persephone/internal/constants"
	"github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
	openstacklocal "github.com/sap-cloud-infrastructure/persephone/internal/openstack"
)

const (
	// DefaultPodCIDR is the default pod CIDR for Shoots.
	DefaultPodCIDR = "10.44.0.0/16"
	// DefaultServiceCIDR is the default service CIDR for Shoots.
	DefaultServiceCIDR = "10.45.0.0/16"
	// DefaultNodeCIDR is the default node CIDR for Shoots.
	DefaultNodeCIDR = "10.180.24.0/24"
	// DefaultNetworkType is the default network type for Shoots.
	DefaultNetworkType = "calico"
	// DefaultLoadBalancerType is the default load balancer type for Shoots.
	DefaultLoadBalancerType = "f5"
)

// The following +api-doc:allow markers define the ShootSpec fields exposed in
// the public Persephone API reference. Fields not listed here are
// auto-injected by this webhook and must not be set by customers
// (provider.type, region, cloudProfile, credentialsBindingName,
// seedSelector, provider.controlPlaneConfig, provider.infrastructureConfig).
//
// +api-doc:allow spec.kubernetes
// +api-doc:allow spec.networking
// +api-doc:allow spec.provider  workers-only; type/infrastructureConfig/controlPlaneConfig are injected
// +api-doc:allow spec.maintenance
// +api-doc:allow spec.hibernation
// +api-doc:allow spec.dns
// +api-doc:allow spec.purpose
// +api-doc:allow spec.addons
// +api-doc:allow spec.extensions
// +api-doc:allow spec.tolerations
// +api-doc:allow spec.systemComponents
// +api-doc:allow spec.monitoring
// +api-doc:allow spec.accessRestrictions

// NewClusterUserClientSet is an alias for openstacklocal.NewClusterUserClientSet.
// Exposed for testing.
var NewClusterUserClientSet = openstacklocal.NewClusterUserClientSet

type Handler struct {
	// Logger is a logger.
	Logger logr.Logger
	// Client is the controller-runtime Kubernetes client.
	Client client.Client
	// OpenStackClientSet is a mapping of OpenStack region to respective client set.
	OpenStackClientSet openstacklocal.RegionOpenStackClientSet
	// LandscapeName is the name of the landscape.
	LandscapeName string
	// DefaultCloudProfileName is the name of the CloudProfile to use for Shoots. If empty, defaults to the provider type.
	DefaultCloudProfileName string
	// DefaultProviderType is the provider type to use for Shoots. If empty, defaults to the constant GardenerProviderType.
	DefaultProviderType string
	// metrics holds the Prometheus metrics.
	metrics *metrics
}

func (h *Handler) Default(ctx context.Context, obj runtime.Object) error {
	// Lazy initialize metrics
	if h.metrics == nil {
		h.metrics = newMetrics(ctrlmetrics.Registry)
	}

	if err := h.handle(ctx, obj); err != nil {
		h.Logger.Error(err, "Webhook handler failed")
		return err
	}

	return nil
}

func (h *Handler) handle(ctx context.Context, obj runtime.Object) error {
	shoot, ok := obj.(*gardenercorev1beta1.Shoot)
	if !ok {
		return fmt.Errorf("expected *gardenercorev1beta1.Shoot but got %T", obj)
	}

	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		return err
	}

	if req.RequestKind == nil || req.RequestKind.Kind != "Shoot" || req.Operation == admissionv1.Delete || req.Operation == admissionv1.Connect {
		h.Logger.Info("Not responsible for requested kind or operation")
		return nil
	}

	log := h.Logger.WithValues(
		"shoot", client.ObjectKeyFromObject(shoot),
		"userInfo", req.UserInfo.Extra,
		"operation", req.Operation,
	)

	if !kubernetes.HasOpenStackUserInfo(req.UserInfo.Extra) {
		// This is to allow interacting with a Shoot with an admin user (that is not authenticated with OpenStack
		// credentials), see also https://github.com/sap-cloud-infrastructure/persephone/commit/56615779823c58ead119ff427a580765021e650c#commitcomment-775028.
		log.Info("Not all OpenStack info found in user's extras, exiting early")
		return nil
	}

	openStackUserInfo, err := kubernetes.OpenStackUserInfoFromExtra(req.UserInfo.Extra)
	if err != nil {
		return fmt.Errorf("failed getting OpenStack user info from extras: %w", err)
	}

	openStackClientSet, ok := h.OpenStackClientSet[openStackUserInfo.Region]
	if !ok {
		return fmt.Errorf("region %q not supported", openStackUserInfo.Region)
	}

	// Check if this is a workerless shoot
	isWorkerless := len(shoot.Spec.Provider.Workers) == 0

	log.Info("Getting cluster user client set")
	clusterUserName := kubernetes.GetClusterUserName(h.LandscapeName, shoot.Name, openStackUserInfo.ProjectID)
	userClientSet, err := NewClusterUserClientSet(ctx, openStackClientSet, clusterUserName, openStackUserInfo.ProjectID)
	if err != nil {
		return fmt.Errorf("could not create cluster user clients: %w", err)
	}

	var credentialsBinding *gardenersecurityv1alpha1.CredentialsBinding
	// Only create credentials binding for non-workerless shoots
	if req.Operation == admissionv1.Create && !isWorkerless {
		log.Info("Ensuring Openstack application credentials for this Shoot")
		credentialsBinding, err = ensureOpenStackCredentialInternalSecretAndBinding(ctx, log, h.Client, userClientSet, shoot, openStackUserInfo)
		if err != nil {
			h.metrics.recordCredentialIssuance(openStackUserInfo.Region, "error")
			h.metrics.recordShootMutation(openStackUserInfo.Region, "error")
			return fmt.Errorf("failed to ensure OpenStack credentials Secret and CredentialsBinding: %w", err)
		}
		h.metrics.recordCredentialIssuance(openStackUserInfo.Region, "success")
	}

	// Only set infrastructure and control plane config for non-workerless shoots
	if !isWorkerless {
		var infrastructureConfig *gardeneropenstackv1alpha1.InfrastructureConfig
		if shoot.Spec.Provider.InfrastructureConfig == nil {
			infrastructureConfig = &gardeneropenstackv1alpha1.InfrastructureConfig{}
		} else {
			internalInfrastructureConfig, err := gardeneropenstackhelper.InfrastructureConfigFromRawExtension(shoot.Spec.Provider.InfrastructureConfig)
			if err != nil {
				return fmt.Errorf("failed decoding infrastructure config from raw extension: %w", err)
			}

			infrastructureConfig = &gardeneropenstackv1alpha1.InfrastructureConfig{}
			if err := gardeneropenstackhelper.Scheme.Convert(internalInfrastructureConfig, infrastructureConfig, nil); err != nil {
				return fmt.Errorf("failed converting internal OpenStack infrastructure config to v1alpha1: %w", err)
			}
		}

		if infrastructureConfig.FloatingPoolName == "" {
			log.Info("Getting external networks for project")
			infrastructureConfig.FloatingPoolName, err = getExternalNetworkForProject(ctx, userClientSet.NetworkClient)
			if err != nil {
				h.metrics.recordFloatingPoolDetection(openStackUserInfo.Region, "error")
				h.metrics.recordShootMutation(openStackUserInfo.Region, "error")
				return fmt.Errorf("error detecting floating pool name: %w", err)
			}
			h.metrics.recordFloatingPoolDetection(openStackUserInfo.Region, "success")
		}

		if infrastructureConfig.Networks.Workers == "" {
			infrastructureConfig.Networks.Workers = ptr.Deref(shoot.Spec.Networking.Nodes, DefaultNodeCIDR)
		}

		log.Info("Setting OpenStack infrastructure configuration")
		infrastructureConfig.TypeMeta = metav1.TypeMeta{APIVersion: gardeneropenstackv1alpha1.SchemeGroupVersion.String(), Kind: "InfrastructureConfig"}
		shoot.Spec.Provider.InfrastructureConfig = &runtime.RawExtension{Object: infrastructureConfig}

		if shoot.Spec.Provider.ControlPlaneConfig == nil {
			log.Info("Setting OpenStack control plane configuration")
			shoot.Spec.Provider.ControlPlaneConfig = &runtime.RawExtension{Object: &gardeneropenstackv1alpha1.ControlPlaneConfig{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gardeneropenstackv1alpha1.SchemeGroupVersion.String(),
					Kind:       "ControlPlaneConfig",
				},
				LoadBalancerProvider: DefaultLoadBalancerType,
			}}
		}

		// Only set networking configuration for non-workerless shoots
		if shoot.Spec.Networking.Pods == nil {
			shoot.Spec.Networking.Pods = new(DefaultPodCIDR)
		}
		if shoot.Spec.Networking.Services == nil {
			shoot.Spec.Networking.Services = new(DefaultServiceCIDR)
		}
		if shoot.Spec.Networking.Nodes == nil {
			shoot.Spec.Networking.Nodes = new(DefaultNodeCIDR)
		}
		if shoot.Spec.Networking.Type == nil {
			shoot.Spec.Networking.Type = new(DefaultNetworkType)
		}
	}

	shoot.Spec.CloudProfile = &gardenercorev1beta1.CloudProfileReference{Kind: "CloudProfile", Name: h.getCloudProfileName()}
	shoot.Spec.Provider.Type = h.getProviderType()
	shoot.Spec.Region = openStackUserInfo.Region

	// HOTFIX: Hardcode seed assignment based on region until Gardener fixes upstream scheduler issue
	// See: https://github.com/gardener/gardener/issues/14926
	// The SameRegion scheduler strategy is not working correctly, causing random seed assignments.
	// This hotfix ensures shoots are ALWAYS scheduled to seeds in their own region on creation by
	// setting a seedSelector that matches the seed region label.
	if req.Operation == admissionv1.Create {
		log.Info("Setting seed selector to match region (hotfix for scheduler issue)", "region", openStackUserInfo.Region)
		shoot.Spec.SeedSelector = &gardenercorev1beta1.SeedSelector{
			LabelSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"seed.gardener.cloud/region": openStackUserInfo.Region,
				},
			},
		}
	}

	// Only set credentials binding for non-workerless shoots
	if credentialsBinding != nil && !isWorkerless {
		shoot.Spec.CredentialsBindingName = &credentialsBinding.Name
	}

	h.metrics.recordShootMutation(openStackUserInfo.Region, "success")
	return nil
}

// getCloudProfileName returns the CloudProfile name to use for Shoots.
// If DefaultCloudProfileName is set, it is returned. Otherwise, the constant GardenerProviderType is returned.
func (h *Handler) getCloudProfileName() string {
	if h.DefaultCloudProfileName != "" {
		return h.DefaultCloudProfileName
	}
	return constants.GardenerProviderType
}

// getProviderType returns the provider type to use for Shoots.
// If DefaultProviderType is set, it is returned. Otherwise, the constant GardenerProviderType is returned.
func (h *Handler) getProviderType() string {
	if h.DefaultProviderType != "" {
		return h.DefaultProviderType
	}
	return constants.GardenerProviderType
}

// ensureOpenStackCredentialInternalSecretAndBinding creates a new InternalSecret and CredentialsBinding containing
// OpenStack application credentials for the given Shoot. The resources are always created in the shoot namespace. If
// there are no such InternalSecrets yet, a new one is created. Otherwise, the newest is used. It returns the name of
// the CredentialsBinding which should be used for the Shoot resource.
func ensureOpenStackCredentialInternalSecretAndBinding(
	ctx context.Context,
	logger logr.Logger,
	kubernetesClient client.Client,
	userClients *openstacklocal.ClusterUserClientSet,
	shoot *gardenercorev1beta1.Shoot,
	openStackUserInfo kubernetes.OpenStackUserInfo,
) (
	*gardenersecurityv1alpha1.CredentialsBinding,
	error,
) {

	log := logger.WithValues("component", "ensure-openstack-credential")

	internalSecretList := &gardenercorev1beta1.InternalSecretList{}
	if err := kubernetesClient.List(ctx, internalSecretList, client.InNamespace(shoot.Namespace), client.MatchingLabels{constants.LabelKeyShootName: shoot.Name}); err != nil {
		return nil, fmt.Errorf("failed listing InternalSecrets for Shoot %q containing credentials: %w", client.ObjectKeyFromObject(shoot), err)
	}

	if len(internalSecretList.Items) > 0 {
		kubernetesutils.ByCreationTimestamp().Sort(internalSecretList)
		newestInternalSecret := internalSecretList.Items[len(internalSecretList.Items)-1].DeepCopy()
		log.Info("InternalSecrets for Shoot found, ensuring CredentialsBinding exists for newest InternalSecret", "internalSecret", client.ObjectKeyFromObject(newestInternalSecret))
		return kubernetes.EnsureCredentialsBindingForInternalSecret(ctx, log, kubernetesClient, shoot, newestInternalSecret)
	}

	internalSecret, err := kubernetes.EnsureApplicationCredentialInternalSecretForShoot(ctx, log, kubernetesClient, userClients, shoot, openStackUserInfo)
	if err != nil {
		return nil, fmt.Errorf("failed ensuring a new application credential: %w", err)
	}

	return kubernetes.EnsureCredentialsBindingForInternalSecret(ctx, log, kubernetesClient, shoot, internalSecret)
}

func getExternalNetworkForProject(ctx context.Context, scopedNetworkClient *gophercloud.ServiceClient) (string, error) {
	type network struct {
		networks.Network
		external.NetworkExternalExt
	}

	allPages, err := networks.List(scopedNetworkClient, networks.ListOpts{Shared: new(true)}).AllPages(ctx)
	if err != nil {
		return "", fmt.Errorf("failed listing networks: %w", err)
	}

	var allNetworks []network
	if err := networks.ExtractNetworksInto(allPages, &allNetworks); err != nil {
		return "", fmt.Errorf("failed extracting networks: %w", err)
	}

	externalNetworks := slices.DeleteFunc(allNetworks, func(n network) bool {
		return !n.External || !n.Shared
	})

	if len(externalNetworks) != 1 {
		return "", fmt.Errorf("unable to auto-detect external network (%d != 1)", len(externalNetworks))
	}

	return externalNetworks[0].Name, nil
}
