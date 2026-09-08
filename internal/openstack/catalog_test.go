package openstack_test

import (
	"errors"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/sap-cloud-infrastructure/persephone/internal/openstack"
)

var _ = Describe("GetPersephoneEndpoint", func() {
	Describe("when provided a region name", func() {
		var (
			landscape        = "qa"
			region           = "region-a"
			url              = "http://qa.api.persephone"
			opts             = openstack.PersephoneEndpointOpts{Landscape: landscape, Region: region}
			catalogExtractor = func() (*tokens.ServiceCatalog, error) {
				return nil, errors.New("the catalog extractor should not have been called")
			}
		)

		It("should fail if the endpoint locator fails", func() {
			endpointLocator := func(gophercloud.EndpointOpts) (string, error) { return "", errors.New("the endpoint locator failed") }
			_, err := openstack.GetPersephoneEndpoint(endpointLocator, catalogExtractor, opts)
			Expect(err).To(HaveOccurred())
		})

		It("should query the service catalog correctly", func() {
			endpointLocator := func(criteria gophercloud.EndpointOpts) (string, error) {
				Expect(criteria).To(Equal(gophercloud.EndpointOpts{
					Type:         "kubernetes",
					Name:         "persephone-qa",
					Region:       "region-a",
					Availability: "public",
				}))
				return url, nil
			}
			_, err := openstack.GetPersephoneEndpoint(endpointLocator, catalogExtractor, opts)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should build the tokens.Endpoint object correctly if the endpoint locator succeeds", func() {
			endpointLocator := func(gophercloud.EndpointOpts) (string, error) { return url, nil }
			result, err := openstack.GetPersephoneEndpoint(endpointLocator, catalogExtractor, opts)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(tokens.Endpoint{Region: region, URL: url}))
		})
	})

	Describe("when not provided a region name", func() {
		var (
			landscape       = "qa"
			opts            = openstack.PersephoneEndpointOpts{Landscape: landscape}
			endpointLocator = func(gophercloud.EndpointOpts) (string, error) {
				return "", errors.New("the endpoint locator should not have been called")
			}
		)

		It("should fail if the catalog extractor fails", func() {
			catalogExtractor := func() (*tokens.ServiceCatalog, error) { return nil, errors.New("the catalog extractor failed") }
			_, err := openstack.GetPersephoneEndpoint(endpointLocator, catalogExtractor, opts)
			Expect(err).To(HaveOccurred())
		})

		It("should fail if the catalog extractor returns more than one matching endpoint for the given landscape", func() {
			catalogExtractor := func() (*tokens.ServiceCatalog, error) {
				return &tokens.ServiceCatalog{
					Entries: []tokens.CatalogEntry{
						{
							Type: "kubernetes",
							Name: "persephone-qa",
							Endpoints: []tokens.Endpoint{
								{
									ID:        "a",
									Region:    "region-a",
									Interface: "public",
									URL:       "http://qa.region-a.persephone",
								},
							},
						},
						{
							Type: "kubernetes",
							Name: "persephone-qa",
							Endpoints: []tokens.Endpoint{
								{
									ID:        "b",
									Region:    "region-b",
									Interface: "public",
									URL:       "http://qa.region-b.persephone",
								},
							},
						},
					},
				}, nil
			}
			_, err := openstack.GetPersephoneEndpoint(endpointLocator, catalogExtractor, opts)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("more than one Persephone endpoint found")))
			Expect(err).To(MatchError(ContainSubstring("region-a")))
			Expect(err).To(MatchError(ContainSubstring("region-b")))
		})

		It("should succeed if the catalog extractor returns a single matching endpoint for the given landscape", func() {
			catalogExtractor := func() (*tokens.ServiceCatalog, error) {
				return &tokens.ServiceCatalog{
					Entries: []tokens.CatalogEntry{
						{
							Type: "kubernetes",
							Name: "persephone-qa",
							Endpoints: []tokens.Endpoint{
								{
									ID:        "persephone-a",
									Region:    "region-a",
									Interface: "public",
									URL:       "http://qa.region-a.persephone",
								},
							},
						},
						{
							Type: "quota",
							Name: "limes",
							Endpoints: []tokens.Endpoint{
								{
									ID:        "limes-a",
									Region:    "region-a",
									Interface: "public",
									URL:       "http://region-a.limes",
								},
							},
						},
					},
				}, nil
			}
			result, err := openstack.GetPersephoneEndpoint(endpointLocator, catalogExtractor, opts)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(tokens.Endpoint{
				ID:        "persephone-a",
				Interface: "public",
				Region:    "region-a",
				URL:       "http://qa.region-a.persephone",
			}))
		})
	})
})
