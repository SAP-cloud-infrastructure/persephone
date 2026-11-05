// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package receiver

import (
	"context"
	"fmt"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/ec2credentials"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	"github.com/gophercloud/gophercloud/v2/pagination"
)

// FetchEC2Credentials authenticates with OpenStack using authOpts, fetches
// (or creates) EC2 credentials for the current project, and discovers the
// S3-compatible endpoint from the service catalog.
//
// It returns the EC2 access key, secret key, and the S3 endpoint URL.
func FetchEC2Credentials(ctx context.Context, authOpts gophercloud.AuthOptions) (accessKey, secretKey, s3Endpoint string, err error) {
	provider, err := openstack.AuthenticatedClient(ctx, authOpts)
	if err != nil {
		return "", "", "", fmt.Errorf("openstack auth: %w", err)
	}

	identityClient, err := openstack.NewIdentityV3(provider, gophercloud.EndpointOpts{})
	if err != nil {
		return "", "", "", fmt.Errorf("identity client: %w", err)
	}

	authResult, ok := provider.GetAuthResult().(tokens.CreateResult)
	if !ok {
		return "", "", "", fmt.Errorf("unexpected auth result type")
	}

	user, err := authResult.ExtractUser()
	if err != nil {
		return "", "", "", fmt.Errorf("extract user from token: %w", err)
	}

	project, err := authResult.ExtractProject()
	if err != nil {
		return "", "", "", fmt.Errorf("extract project from token: %w", err)
	}

	// Reuse existing EC2 credential for this project, or create one.
	cred, err := getOrCreateEC2Credential(ctx, identityClient, user.ID, project.ID)
	if err != nil {
		return "", "", "", err
	}

	// Discover S3 endpoint; fall back to "object-store" if "s3" not found.
	s3Endpoint, err = discoverS3Endpoint(provider)
	if err != nil {
		return "", "", "", err
	}

	return cred.Access, cred.Secret, s3Endpoint, nil
}

func getOrCreateEC2Credential(ctx context.Context, client *gophercloud.ServiceClient, userID, projectID string) (*ec2credentials.Credential, error) {
	var found *ec2credentials.Credential
	err := ec2credentials.List(client, userID).EachPage(ctx, func(_ context.Context, page pagination.Page) (bool, error) {
		creds, err := ec2credentials.ExtractCredentials(page)
		if err != nil {
			return false, err
		}
		for i := range creds {
			if creds[i].TenantID == projectID {
				c := creds[i]
				found = &c
				return false, nil // stop pagination
			}
		}
		return true, nil
	})
	if err != nil {
		return nil, fmt.Errorf("list ec2 credentials: %w", err)
	}
	if found != nil {
		return found, nil
	}

	cred, err := ec2credentials.Create(ctx, client, userID, ec2credentials.CreateOpts{TenantID: projectID}).Extract()
	if err != nil {
		return nil, fmt.Errorf("create ec2 credential: %w", err)
	}
	return cred, nil
}

func discoverS3Endpoint(provider *gophercloud.ProviderClient) (string, error) {
	endpoint, err := provider.EndpointLocator(gophercloud.EndpointOpts{Type: "s3"})
	if err == nil {
		return endpoint, nil
	}
	endpoint, err = provider.EndpointLocator(gophercloud.EndpointOpts{Type: "object-store"})
	if err != nil {
		return "", fmt.Errorf("discover s3 endpoint (tried types 's3' and 'object-store'): %w", err)
	}
	return endpoint, nil
}
