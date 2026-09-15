// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package openstack

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Masterminds/goutils"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/applicationcredentials"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/roles"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/users"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/sap-cloud-infrastructure/persephone/internal/constants"
)

func (c *ClientSet) reconcileClusterServiceUser(ctx context.Context, username, projectID string) (*users.User, string, error) {
	user, err := c.GetUserByName(ctx, username)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get user %q by name: %w", username, err)
	}

	password, err := goutils.Random(48, 32, 127, true, true)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate random service user credentials: %w", err)
	}

	if user != nil {
		_, err = users.Update(ctx, c.IdentityClient, user.ID, users.UpdateOpts{
			Password: password,
		}).Extract()
	} else {
		user, err = users.Create(ctx, c.IdentityClient, users.CreateOpts{
			Name:             username,
			DomainID:         c.DomainID,
			Password:         password,
			DefaultProjectID: projectID,
			Description:      "Gardener customer shoot service user",
		}).Extract()
	}
	if err != nil {
		return nil, "", fmt.Errorf("failed to create or update user: %w", err)
	}

	if err = c.assignUserRoles(ctx, user, projectID); err != nil {
		return nil, "", fmt.Errorf("failed to assign roles to service user: %w", err)
	}

	return user, password, nil
}

// DeleteClusterServiceUser deletes the service user created for a specific shoot cluster.
func (c *ClientSet) DeleteClusterServiceUser(ctx context.Context, username string) error {
	user, err := c.GetUserByName(ctx, username)
	if err != nil {
		return fmt.Errorf("failed to get user %q by name: %w", username, err)
	}

	if user == nil {
		return nil
	}

	return users.Delete(ctx, c.IdentityClient, user.ID).ExtractErr()
}

func (c *ClientSet) assignUserRoles(ctx context.Context, user *users.User, projectID string) error {
	for _, roleName := range GetDefaultServiceUserRoles() {
		role, err := c.getRoleByName(ctx, roleName)
		if err != nil || role == nil {
			return fmt.Errorf("failed to get role by name %q: %w", roleName, err)
		}

		if err := roles.Assign(ctx, c.IdentityClient, role.ID, roles.AssignOpts{UserID: user.ID, ProjectID: projectID}).ExtractErr(); err != nil {
			return fmt.Errorf("failed to assign role %q to user %q: %w", roleName, user.Name, err)
		}
	}

	return nil
}

// GetUserByName returns the *users.User object for the given OpenStack username and domain ID. It returns <nil> if no
// such user is found, or an error if multiple users are found.
func (c *ClientSet) GetUserByName(ctx context.Context, username string) (*users.User, error) {
	pages, err := users.List(c.IdentityClient, users.ListOpts{Name: username, DomainID: c.DomainID}).AllPages(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list all users in domain %q with name %q: %w", c.DomainID, username, err)
	}

	userList, err := users.ExtractUsers(pages)
	if err != nil {
		return nil, fmt.Errorf("failed to extract users from all pages: %w", err)
	}

	if len(userList) == 0 {
		return nil, nil
	}

	if len(userList) > 1 {
		return nil, fmt.Errorf("found multiple users (%d) in domain %q with name %q", len(userList), c.DomainID, username)
	}

	return &userList[0], nil
}

// getRoleByName returns the *roles.Role object for the given OpenStack role name. It returns <nil> if no such role is
// found, or an error if multiple roles are found.
func (c *ClientSet) getRoleByName(ctx context.Context, roleName string) (*roles.Role, error) {
	pages, err := roles.List(c.IdentityClient, roles.ListOpts{Name: roleName}).AllPages(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list roles for name %q: %w", roleName, err)
	}

	roleList, err := roles.ExtractRoles(pages)
	if err != nil {
		return nil, fmt.Errorf("failed to extract roles from all pages: %w", err)
	}

	if len(roleList) == 0 {
		return nil, nil
	}

	if len(roleList) != 1 {
		return nil, fmt.Errorf("found multiple roles (%d) with name %q", len(roleList), roleName)
	}

	return &roleList[0], nil
}

// GetDefaultServiceUserRoles returns the default user roles used for the service users.
func GetDefaultServiceUserRoles() []string {
	return []string{
		"compute_admin",
		"member",
		"network_admin",
		"objectstore_admin",
		"volume_admin",
	}
}

// ClusterUserClientSet is a ClientSet for a specific cluster user (a dedicated OpenStack user created for a Shoot).
// This cluster user will be used to manage application credentials (that are used for the related shoot cluster).
type ClusterUserClientSet struct {
	// ClientSet is the OpenStack ClientSet created for the cluster user.
	ClientSet

	// ProjectID is the ID of the project for the user.
	ProjectID string
	// User is the users.User object for the user.
	User *users.User
}

// NewClusterUserClientSet creates a new ClusterUserClientSet for the given username and project ID.
func NewClusterUserClientSet(ctx context.Context, clientSet *ClientSet, username, projectID string) (*ClusterUserClientSet, error) {
	user, password, err := clientSet.reconcileClusterServiceUser(ctx, username, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create cluster service user: %w", err)
	}

	var (
		providerClient *gophercloud.ProviderClient
		identityClient *gophercloud.ServiceClient
		networkClient  *gophercloud.ServiceClient
	)

	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// Wait for service user to be available
	if err := wait.PollUntilContextCancel(timeoutCtx, 3*time.Second, true, func(ctx context.Context) (bool, error) {
		providerClient, err = openstack.AuthenticatedClient(ctx, gophercloud.AuthOptions{
			IdentityEndpoint: clientSet.ProviderClient.IdentityEndpoint,
			Username:         username,
			Password:         password,
			DomainID:         clientSet.DomainID,
			Scope:            &gophercloud.AuthScope{ProjectID: projectID},
		})
		if err != nil {
			//nolint:nilerr // returning the error would abort the poller, but we want to retry instead
			return false, nil
		}

		identityClient, err = openstack.NewIdentityV3(providerClient, gophercloud.EndpointOpts{})
		if err != nil {
			//nolint:nilerr // returning the error would abort the poller, but we want to retry instead
			return false, nil
		}

		networkClient, err = openstack.NewNetworkV2(providerClient, gophercloud.EndpointOpts{})
		if err != nil {
			//nolint:nilerr // returning the error would abort the poller, but we want to retry instead
			return false, nil
		}

		return true, nil
	}); err != nil {
		return nil, fmt.Errorf("failed waiting for service user to be available: %w", err)
	}

	return &ClusterUserClientSet{
		ClientSet: ClientSet{
			ProviderClient: providerClient,
			IdentityClient: identityClient,
			NetworkClient:  networkClient,
			DomainID:       clientSet.DomainID,
		},
		ProjectID: projectID,
		User:      user,
	}, nil
}

// GetValidApplicationCredential gets valid application credentials for the given user ID. It filters all existing
// credentials by the given name prefix. If multiple valid credentials exist, the one with the earliest expiration is
// returned, provided it does not expire within the next 24 hours. If there is no such credential or no credentials at
// all for this user, <nil> is returned.
func (c *ClusterUserClientSet) GetValidApplicationCredential(ctx context.Context, namePrefix string) (*applicationcredentials.ApplicationCredential, error) {
	pages, err := applicationcredentials.List(c.IdentityClient, c.User.ID, applicationcredentials.ListOpts{}).AllPages(ctx)
	if err != nil {
		return nil, fmt.Errorf("error listing application credentials: %w", err)
	}

	applicationCredentials, err := applicationcredentials.ExtractApplicationCredentials(pages)
	if err != nil {
		return nil, fmt.Errorf("error extracting application credentials: %w", err)
	}

	if len(applicationCredentials) == 0 {
		return nil, nil
	}

	// Remove all application credentials whose names don't start with the given prefix.
	applicationCredentials = slices.DeleteFunc(applicationCredentials, func(appCred applicationcredentials.ApplicationCredential) bool {
		return !strings.HasPrefix(appCred.Name, namePrefix)
	})

	// Sort by expiration date, so that the first one is the one expiring the soonest
	slices.SortFunc(applicationCredentials, func(a, b applicationcredentials.ApplicationCredential) int {
		if a.ExpiresAt.Before(b.ExpiresAt) {
			return -1
		}
		if a.ExpiresAt.After(b.ExpiresAt) {
			return 1
		}
		return 0
	})

	for _, credential := range applicationCredentials {
		if credential.ExpiresAt.After(time.Now().Add(constants.ApplicationCredentialsRenewTime)) {
			return &credential, nil
		}
	}

	return nil, nil
}

// CreateApplicationCredential creates new application credentials with the given name and roles.
func (c *ClusterUserClientSet) CreateApplicationCredential(ctx context.Context, name string, roleList []string) (*applicationcredentials.ApplicationCredential, error) {
	createOpts := applicationcredentials.CreateOpts{
		Name:      name,
		ExpiresAt: new(time.Now().Add(constants.ApplicationCredentialsValidity)),
	}

	for _, role := range roleList {
		createOpts.Roles = append(createOpts.Roles, applicationcredentials.Role{Name: role})
	}

	return applicationcredentials.Create(ctx, c.IdentityClient, c.User.ID, createOpts).Extract()
}

// DeleteApplicationCredential deletes application credentials with the given ID.
func (c *ClusterUserClientSet) DeleteApplicationCredential(ctx context.Context, id string) error {
	return applicationcredentials.Delete(ctx, c.IdentityClient, c.User.ID, id).ExtractErr()
}
