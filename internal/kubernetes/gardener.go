// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package kubernetes

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	gardeneropenstackv1alpha1 "github.com/gardener/gardener-extension-provider-openstack/pkg/apis/openstack/v1alpha1"
	"github.com/gardener/gardener-extension-provider-openstack/pkg/openstack"
	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	gardenersecurityv1alpha1 "github.com/gardener/gardener/pkg/apis/security/v1alpha1"
	"github.com/gardener/gardener/pkg/utils"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/applicationcredentials"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/sap-cloud-infrastructure/persephone/internal/config"
	"github.com/sap-cloud-infrastructure/persephone/internal/constants"
)

// GardenerScheme is the scheme that contains Gardener APIs.
var GardenerScheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(GardenerScheme))
	utilruntime.Must(gardenercorev1beta1.AddToScheme(GardenerScheme))
	utilruntime.Must(gardeneropenstackv1alpha1.AddToScheme(GardenerScheme))
	utilruntime.Must(gardenersecurityv1alpha1.AddToScheme(GardenerScheme))
}

// GetGardenerProjectNamespaceName returns the name of the namespace that is
// bound to a Gardener region and project with the given ID.
func GetGardenerProjectNamespaceName(region, projectID string) string {
	return fmt.Sprintf("garden-%s-%s", region, projectID)
}

// GetGardenerProjectName returns the name of the Gardener project for the given region, project ID and landscape name
// as a 10-characters hashed string, adhering to Gardener's project name max length.
func GetGardenerProjectName(region, projectID, landscapeName string) string {
	return utils.ComputeSHA256Hex([]byte(region + projectID + landscapeName))[:10]
}

// ReconcileGardenerProjectResources sets up the Gardener project based on the supplied OpenStack user info.
// It is called by both the operator (controller/project/reconciler.go) and the webhook (webhook/token/handler.go).
//
// CONTRACT with the liquid apiserver (internal/liquidserver/logic.go, docs/liquid-apiserver.md):
// This function uses lazy provisioning — namespaces are only created on first project use, not eagerly.
// The liquid apiserver's ScanUsage relies on namespace existence as the gate: no namespace → Forbidden: true
// (Limes skips SetQuota); namespace present → Forbidden: false (normal quota management).
//
// Two invariants this function must maintain for the contract to hold:
//  1. Namespace and ResourceQuota are always created together atomically (in this function).
//     There is no valid state where the namespace exists but the ResourceQuota does not.
//  2. ResourceQuota spec.hard is only set on initial creation; Limes owns the value after that via SetQuota.
func ReconcileGardenerProjectResources(ctx context.Context, c client.Client, cfg config.PersephoneConfig, openStackUserInfo OpenStackUserInfo) error {
	gardenerProjectName := GetGardenerProjectName(openStackUserInfo.Region, openStackUserInfo.ProjectID, cfg.LandscapeName)

	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: GetGardenerProjectNamespaceName(openStackUserInfo.Region, openStackUserInfo.ProjectID)}}
	if _, err := controllerutil.CreateOrPatch(ctx, c, namespace, func() error {
		metav1.SetMetaDataLabel(&namespace.ObjectMeta, v1beta1constants.GardenRole, v1beta1constants.GardenRoleProject)
		metav1.SetMetaDataLabel(&namespace.ObjectMeta, v1beta1constants.ProjectName, gardenerProjectName)

		metav1.SetMetaDataLabel(&namespace.ObjectMeta, constants.LabelKeyOpenStackProjectID, openStackUserInfo.ProjectID)
		metav1.SetMetaDataLabel(&namespace.ObjectMeta, constants.LabelKeyOpenStackProjectName, sanitizeLabelValue(openStackUserInfo.ProjectName))
		metav1.SetMetaDataLabel(&namespace.ObjectMeta, constants.LabelKeyOpenStackDomainID, openStackUserInfo.ProjectDomainID)
		metav1.SetMetaDataLabel(&namespace.ObjectMeta, constants.LabelKeyOpenStackDomainName, sanitizeLabelValue(openStackUserInfo.ProjectDomainName))
		metav1.SetMetaDataLabel(&namespace.ObjectMeta, constants.LabelKeyOpenStackRegion, openStackUserInfo.Region)
		return nil
	}); err != nil {
		return fmt.Errorf("failed reconciling project namespace %s: %w", namespace.Name, err)
	}

	project := &gardenercorev1beta1.Project{ObjectMeta: metav1.ObjectMeta{Name: gardenerProjectName}}
	if _, err := controllerutil.CreateOrPatch(ctx, c, project, func() error {
		project.Spec.Namespace = &namespace.Name
		return nil
	}); err != nil {
		return fmt.Errorf("failed reconciling project %s: %w", project.Name, err)
	}

	role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "persephone.sci.cloud.sap:openstack-project-kubernetes-admin", Namespace: namespace.Name}}
	if _, err := controllerutil.CreateOrPatch(ctx, c, role, func() error {
		role.Rules = []rbacv1.PolicyRule{
			{
				APIGroups: []string{gardenercorev1beta1.GroupName},
				Resources: []string{"shoots"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{gardenercorev1beta1.GroupName},
				Resources: []string{"shoots/adminkubeconfig"},
				Verbs:     []string{"create"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"configmaps", "secrets"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"resourcequotas"},
				Verbs:     []string{"get", "list"},
			},
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed reconciling role %s: %w", client.ObjectKeyFromObject(role), err)
	}

	roleBinding := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: role.Name, Namespace: namespace.Name}}
	if _, err := controllerutil.CreateOrPatch(ctx, c, roleBinding, func() error {
		roleBinding.Subjects = []rbacv1.Subject{{
			APIGroup: rbacv1.GroupName,
			Kind:     "Group",
			Name:     openStackUserInfo.ProjectID + ":kubernetes_admin",
		}}
		roleBinding.RoleRef = rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     role.Name,
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed reconciling role binding %s: %w", client.ObjectKeyFromObject(roleBinding), err)
	}

	// Create the ResourceQuota with the default shoot quota, but only on initial creation.
	// Limes manages the quota value after that via SetQuota, so we must not overwrite it on subsequent calls.
	rq := &corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: constants.ResourceQuotaName, Namespace: namespace.Name}}
	if _, err := controllerutil.CreateOrPatch(ctx, c, rq, func() error {
		if rq.CreationTimestamp.IsZero() {
			if rq.Spec.Hard == nil {
				rq.Spec.Hard = make(corev1.ResourceList)
			}
			rq.Spec.Hard[constants.ShootResourceQuotaKey] = *resource.NewQuantity(int64(cfg.DefaultShootQuota), resource.DecimalSI) //nolint:gosec // defaultShootQuota is operator-supplied config, not realistic to overflow int64
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed reconciling ResourceQuota %s: %w", namespace.Name, err)
	}

	return nil
}

const delimiter = "--"

// GetClusterUserName returns the name of an OpenStack user created for a shoot cluster.
func GetClusterUserName(landscapeName, shootName, projectID string) string {
	return "gardener" + delimiter + landscapeName + delimiter + shootName + delimiter + projectID
}

// NewApplicationCredentialName returns a new name for application credentials based on the current time.
func NewApplicationCredentialName() string {
	return "shoot-" + strconv.FormatInt(time.Now().UTC().Unix(), 10)
}

// GetApplicationCredentialInternalSecretName returns the name of an InternalSecret containing application credentials
// for a Shoot.
func GetApplicationCredentialInternalSecretName(projectID, shootName, applicationCredentialName string) string {
	return projectID + delimiter + shootName + delimiter + applicationCredentialName
}

// ApplicationCredentialsInternalSecret returns a new immutable Secret for application credentials for a Shoot. It is
// annotated with the expiration timestamp of the provided application credential, and labeled with information about
// the Shoot and the OpenStack environment.
func ApplicationCredentialsInternalSecret(shoot *gardenercorev1beta1.Shoot, openStackUserInfo OpenStackUserInfo, applicationCredential *applicationcredentials.ApplicationCredential) *gardenercorev1beta1.InternalSecret {
	return &gardenercorev1beta1.InternalSecret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetApplicationCredentialInternalSecretName(openStackUserInfo.ProjectID, shoot.Name, applicationCredential.Name),
			Namespace: shoot.Namespace,
			Annotations: map[string]string{
				constants.AnnotationKeyExpiresAt: applicationCredential.ExpiresAt.UTC().Format(time.RFC3339),
			},
			Labels: map[string]string{
				constants.LabelKeyOpenStackDomainID:    openStackUserInfo.ProjectDomainID,
				constants.LabelKeyOpenStackDomainName:  sanitizeLabelValue(openStackUserInfo.ProjectDomainName),
				constants.LabelKeyOpenStackProjectID:   openStackUserInfo.ProjectID,
				constants.LabelKeyOpenStackProjectName: sanitizeLabelValue(openStackUserInfo.ProjectName),
				constants.LabelKeyOpenStackRegion:      openStackUserInfo.Region,
				constants.LabelKeyShootName:            shoot.Name,
			},
		},
		Type:      corev1.SecretTypeOpaque,
		Immutable: new(true),
		StringData: map[string]string{
			openstack.DomainName:                  openStackUserInfo.ProjectDomainName,
			openstack.TenantName:                  openStackUserInfo.ProjectName,
			openstack.ApplicationCredentialID:     applicationCredential.ID,
			openstack.ApplicationCredentialSecret: applicationCredential.Secret,
		},
	}
}

// CredentialsBindingForSecret creates a new CredentialsBinding for the given Shoot and InternalSecret.
func CredentialsBindingForSecret(shoot *gardenercorev1beta1.Shoot, internalSecret *gardenercorev1beta1.InternalSecret) *gardenersecurityv1alpha1.CredentialsBinding {
	return &gardenersecurityv1alpha1.CredentialsBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      internalSecret.Name,
			Namespace: shoot.Namespace,
		},
		Provider: gardenersecurityv1alpha1.CredentialsBindingProvider{
			Type: constants.GardenerProviderType,
		},
		CredentialsRef: corev1.ObjectReference{
			APIVersion: gardenercorev1beta1.SchemeGroupVersion.String(),
			Kind:       "InternalSecret",
			Name:       internalSecret.Name,
			Namespace:  shoot.Namespace,
		},
	}
}

// sanitizeLabelValue replaces characters that are invalid in a Kubernetes label value with
// underscores, strips leading/trailing non-alphanumeric characters, and truncates to the
// maximum allowed length. A Kubernetes label value must match
// (([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])? and be at most 63 characters.
func sanitizeLabelValue(s string) string {
	isAlphanumeric := func(r rune) bool {
		return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
	}
	result := strings.Map(func(r rune) rune {
		if isAlphanumeric(r) || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, s)
	result = strings.TrimFunc(result, func(r rune) bool { return !isAlphanumeric(r) })
	if len(result) > validation.LabelValueMaxLength {
		result = strings.TrimRightFunc(result[:validation.LabelValueMaxLength], func(r rune) bool { return !isAlphanumeric(r) })
	}
	return result
}
