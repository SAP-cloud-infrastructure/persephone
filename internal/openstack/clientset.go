// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package openstack

import (
	"context"
	"fmt"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/domains"

	"github.com/sap-cloud-infrastructure/persephone/internal/config"
)

// RegionOpenStackClientSet is a set of OpenStack clients for multiple regions. The key is the region name, the value is
// the ClientSet for this region.
type RegionOpenStackClientSet map[string]*ClientSet

// ClientSet contains multiple OpenStack clients.
type ClientSet struct {
	// ProviderClient is a provider client.
	ProviderClient *gophercloud.ProviderClient
	// IdentityClient is an identity v3 client.
	IdentityClient *gophercloud.ServiceClient
	// NetworkClient is a network v2 client.
	NetworkClient *gophercloud.ServiceClient

	// DomainID is the OpenStack domain ID.
	DomainID string
}

// NewRegionOpenStackClientSet creates a new RegionOpenStackClientSet for the provided regions.
func NewRegionOpenStackClientSet(ctx context.Context, regions map[string]config.RegionalConfig, clusterUsersDomain string) (RegionOpenStackClientSet, error) {
	openStackClientSet := make(RegionOpenStackClientSet)

	for region, regionConfig := range regions {
		if !regionConfig.Enabled {
			continue
		}

		providerClient, err := openstack.AuthenticatedClient(ctx, gophercloud.AuthOptions{
			IdentityEndpoint:            regionConfig.IdentityEndpoint,
			ApplicationCredentialID:     regionConfig.ApplicationCredentialID,
			ApplicationCredentialSecret: regionConfig.ApplicationCredentialSecret,
			AllowReauth:                 true,
		})
		if err != nil {
			return nil, fmt.Errorf("unable to create OpenStack provider client: %w", err)
		}

		openStackClientSet[region], err = NewClientSet(ctx, providerClient, clusterUsersDomain)
		if err != nil {
			return nil, fmt.Errorf("unable to create OpenStack admin client: %w", err)
		}
	}

	return openStackClientSet, nil
}

// NewClientSet returns a new ClientSet based on the provided provider client and OpenStack domain name.
func NewClientSet(ctx context.Context, providerClient *gophercloud.ProviderClient, domainName string) (*ClientSet, error) {
	identityClient, err := openstack.NewIdentityV3(providerClient, gophercloud.EndpointOpts{})
	if err != nil {
		return nil, fmt.Errorf("failed creating a new identity v3 client: %w", err)
	}

	networkClient, err := openstack.NewNetworkV2(providerClient, gophercloud.EndpointOpts{})
	if err != nil {
		return nil, fmt.Errorf("failed creating a new network v2 client: %w", err)
	}

	clientSet := &ClientSet{
		ProviderClient: providerClient,
		IdentityClient: identityClient,
		NetworkClient:  networkClient,
	}

	domain, err := clientSet.getDomainByName(ctx, domainName)
	if err != nil || domain == nil {
		return nil, fmt.Errorf("failed to determine domain ID for domain name %s: %w", domainName, err)
	}
	clientSet.DomainID = domain.ID

	return clientSet, nil
}

// getDomainByName returns the *domains.Domain object for the given OpenStack domain name. It returns <nil> if no such
// domain is found, or an error if multiple domains are found.
func (c *ClientSet) getDomainByName(ctx context.Context, domainName string) (*domains.Domain, error) {
	pages, err := domains.List(c.IdentityClient, domains.ListOpts{Name: domainName}).AllPages(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list all domains with name %q: %w", domainName, err)
	}

	domainList, err := domains.ExtractDomains(pages)
	if err != nil {
		return nil, fmt.Errorf("failed to extract domains from all pages: %w", err)
	}

	if len(domainList) == 0 {
		return nil, nil
	}

	if len(domainList) != 1 {
		return nil, fmt.Errorf("could not determine domain id for %q", domainName)
	}

	return &domainList[0], nil
}
