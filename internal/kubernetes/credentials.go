// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package kubernetes

import (
	"context"
	"fmt"

	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	gardenersecurityv1alpha1 "github.com/gardener/gardener/pkg/apis/security/v1alpha1"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openstacklocal "github.com/sap-cloud-infrastructure/persephone/internal/openstack"
)

// EnsureApplicationCredentialInternalSecretForShoot ensures a new InternalSecret containing application credentials for
// the given Shoot.
func EnsureApplicationCredentialInternalSecretForShoot(
	ctx context.Context,
	log logr.Logger,
	kubernetesClient client.Client,
	userClients *openstacklocal.ClusterUserClientSet,
	shoot *gardenercorev1beta1.Shoot,
	openStackUserInfo OpenStackUserInfo,
) (
	*gardenercorev1beta1.InternalSecret,
	error,
) {

	log.Info("Creating new application credential for Shoot")
	applicationCredential, err := userClients.CreateApplicationCredential(ctx, NewApplicationCredentialName(), openstacklocal.GetDefaultServiceUserRoles())
	if err != nil {
		return nil, fmt.Errorf("failed creating new a application credential: %w", err)
	}

	internalSecret := ApplicationCredentialsInternalSecret(shoot, openStackUserInfo, applicationCredential)

	log.Info("Ensuring new Secret for application credential", "internalSecret", client.ObjectKeyFromObject(internalSecret), "applicationCredentialName", applicationCredential.Name)
	if err := kubernetesClient.Create(ctx, internalSecret); err != nil {
		return nil, fmt.Errorf("failed creating application credential InternalSecret %q: %w", client.ObjectKeyFromObject(internalSecret), err)
	}

	return internalSecret, nil
}

// EnsureCredentialsBindingForInternalSecret ensures the CredentialsBinding for the given Secret.
func EnsureCredentialsBindingForInternalSecret(
	ctx context.Context,
	log logr.Logger,
	kubernetesClient client.Client,
	shoot *gardenercorev1beta1.Shoot,
	internalSecret *gardenercorev1beta1.InternalSecret,
) (
	*gardenersecurityv1alpha1.CredentialsBinding,
	error,
) {

	credentialsBinding := CredentialsBindingForSecret(shoot, internalSecret)

	log.Info("Ensuring CredentialsBinding for application credential secret", "credentialsBinding", client.ObjectKeyFromObject(credentialsBinding))
	if err := kubernetesClient.Create(ctx, credentialsBinding); client.IgnoreAlreadyExists(err) != nil {
		return nil, fmt.Errorf("failed creating CredentialsBinding for application credential secret %q: %w", client.ObjectKeyFromObject(credentialsBinding), err)
	}

	return credentialsBinding, nil
}
