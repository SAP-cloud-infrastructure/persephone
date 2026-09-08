package openstack

import (
	"github.com/gophercloud/gophercloud/v2"
	openstack_gophercloud "github.com/gophercloud/gophercloud/v2/openstack"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func GetTestIdentityClient(t GinkgoTInterface) *gophercloud.ServiceClient {
	t.Helper()

	authOptions := gophercloud.AuthOptions{
		DomainName: "Default",
		Username:   "dummy",
		Password:   "dummy",
	}

	providerClient, err := GetFakeE2EClient(t.Context(), authOptions)
	Expect(err).NotTo(HaveOccurred())

	identityClient, err := openstack_gophercloud.NewIdentityV3(providerClient, gophercloud.EndpointOpts{})
	Expect(err).NotTo(HaveOccurred())

	return identityClient
}
