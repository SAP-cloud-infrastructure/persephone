// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package openstack

import (
	"fmt"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	"github.com/sapcc/go-bits/logg"
	"k8s.io/apimachinery/pkg/util/sets"
)

// OpenStackServiceType is the service type for the client (e.g., "compute",
// "object-store"), as defined by the OpenStack Service Types Authority.
const OpenStackServiceType = "kubernetes"

// PersephoneEndpointOpts specifies search criteria for Persephone in an OpenStack service catalog.
type PersephoneEndpointOpts struct {
	// Landscape is a mandatory landscape filter.
	Landscape string
	// Region is an optional region filter.
	Region string
}

func getPersephoneOpenStackServiceName(landscape string) string {
	return "persephone-" + landscape
}

// OpenStackServiceCatalogExtractor extracts/retrieves the entire OpenStack service catalog.
type OpenStackServiceCatalogExtractor func() (*tokens.ServiceCatalog, error)

// GetPersephoneEndpoint locates a Persephone endpoint matching the provided search criteria.
func GetPersephoneEndpoint(endpointLocator gophercloud.EndpointLocator, catalogExtractor OpenStackServiceCatalogExtractor, persephoneOpts PersephoneEndpointOpts) (tokens.Endpoint, error) {
	// there's not enough filters to confidently find the correct endpoint (region is missing),
	// therefore the region must be extracted from the only possible match. if more than one match
	// exists (i.e. there are multiple regions matching the persephone endpoint), then this function
	// shall return an error.
	if persephoneOpts.Region == "" {
		logg.Debug("No region provided for Persephone endpoint search.")
		return searchPersephoneEndpoint(catalogExtractor, persephoneOpts.Landscape)
	}
	// there's enough filters to find the correct endpoint
	searchCriteria := gophercloud.EndpointOpts{
		Availability: gophercloud.AvailabilityPublic,
		Name:         getPersephoneOpenStackServiceName(persephoneOpts.Landscape),
		Type:         OpenStackServiceType,
		Region:       persephoneOpts.Region,
	}
	url, err := endpointLocator(searchCriteria)
	if err != nil {
		return tokens.Endpoint{}, fmt.Errorf("endpoint matching criteria %+v not found: %w", searchCriteria, err)
	}
	logg.Debug("✅ Persephone endpoint found. URL = %s", url)
	return tokens.Endpoint{Region: persephoneOpts.Region, URL: url}, nil
}

func searchPersephoneEndpoint(catalogExtractor OpenStackServiceCatalogExtractor, landscape string) (tokens.Endpoint, error) {
	var matchingEndpoints []tokens.Endpoint
	matchingRegions := sets.New[string]()
	catalog, err := catalogExtractor()
	if err != nil {
		return tokens.Endpoint{}, fmt.Errorf("could not extract service catalog out of auth result: %w", err)
	}
	for _, svc := range catalog.Entries {
		if svc.Type != OpenStackServiceType || svc.Name != getPersephoneOpenStackServiceName(landscape) {
			continue
		}
		for _, endpoint := range svc.Endpoints {
			if endpoint.Interface != "public" {
				continue
			}
			matchingEndpoints = append(matchingEndpoints, endpoint)
			matchingRegions.Insert(endpoint.Region)
		}
	}
	switch len(matchingEndpoints) {
	case 0:
		return tokens.Endpoint{}, fmt.Errorf("no matching Persephone endpoint found for %q landscape", landscape)
	case 1:
		logg.Debug("✅ Persephone endpoint found. URL = %s", matchingEndpoints[0].URL)
		return matchingEndpoints[0], nil
	default:
		regions := sets.List(matchingRegions)
		return tokens.Endpoint{}, fmt.Errorf("more than one Persephone endpoint found for %q landscape; use OS_REGION_NAME/--os-region-name to narrow down the endpoint search to one of following regions: %+v", landscape, regions)
	}
}
