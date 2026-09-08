package openstack

import (
	"context"
	"time"

	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/users"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/go-bits/must"
)

var _ = Describe("ClusterUserClientSet", func() {
	var (
		ctx                  = context.Background()
		clusterUserClientSet *ClusterUserClientSet
		prefix               = "test-prefix-"
	)

	Describe("GetValidApplicationCredential", func() {
		When("no credentials exist", func() {
			BeforeEach(func() {
				FakeE2EOptions = fakeE2EOptions{
					UserName: "user-id",
				}
				clusterUserClientSet = &ClusterUserClientSet{
					ClientSet: ClientSet{IdentityClient: GetTestIdentityClient(GinkgoT())},
					User:      &users.User{ID: FakeE2EOptions.UserName, Name: FakeE2EOptions.UserName},
				}
			})

			It("should return nil since there are no valid credentials", func() {
				cred, err := clusterUserClientSet.GetValidApplicationCredential(ctx, prefix)
				Expect(err).NotTo(HaveOccurred())
				Expect(cred).To(BeNil())
			})
		})

		When("a valid credential exists", func() {
			BeforeEach(func() {
				FakeE2EOptions = fakeE2EOptions{
					Credentials: []fakeCredential{{
						Name:      prefix + "1",
						Roles:     []string{"role1", "role2"},
						ExpiresAt: "2030-01-01T13:45:00.000000",
					}},
					UserName: "user-id",
				}
				clusterUserClientSet = &ClusterUserClientSet{
					ClientSet: ClientSet{IdentityClient: GetTestIdentityClient(GinkgoT())},
					User:      &users.User{ID: FakeE2EOptions.UserName, Name: FakeE2EOptions.UserName},
				}
			})

			It("should return the credential with correct properties", func() {
				cred, err := clusterUserClientSet.GetValidApplicationCredential(ctx, prefix)
				Expect(err).NotTo(HaveOccurred())
				Expect(cred).NotTo(BeNil())

				credential := FakeE2EOptions.Credentials[0]
				Expect(cred.Name).To(Equal(credential.Name))
				Expect(cred.ExpiresAt).To(Equal(must.Return(time.Parse("2006-01-02T15:04:05.000000", "2030-01-01T13:45:00.000000"))))
				for i, roleName := range credential.Roles {
					Expect(cred.Roles[i].Name).To(Equal(roleName))
				}
			})
		})
	})

	Describe("#CreateApplicationCredential", func() {
		When("creating a credential with specific roles", func() {
			BeforeEach(func() {
				FakeE2EOptions = fakeE2EOptions{
					Credentials: []fakeCredential{{
						Name:      "test-prefix",
						Roles:     []string{"role1", "role2"},
						ExpiresAt: "2030-01-01T13:45:00.000000",
					}},
					UserName: "user-id",
				}
				clusterUserClientSet = &ClusterUserClientSet{
					ClientSet: ClientSet{IdentityClient: GetTestIdentityClient(GinkgoT())},
					User:      &users.User{ID: FakeE2EOptions.UserName, Name: FakeE2EOptions.UserName},
				}
			})

			It("should create credential with correct name and roles", func() {
				cred, err := clusterUserClientSet.CreateApplicationCredential(ctx, FakeE2EOptions.Credentials[0].Name, FakeE2EOptions.Credentials[0].Roles)
				Expect(err).NotTo(HaveOccurred())
				Expect(cred.Name).To(Equal(FakeE2EOptions.Credentials[0].Name))
				for i, roleName := range FakeE2EOptions.Credentials[0].Roles {
					Expect(cred.Roles[i].Name).To(Equal(roleName))
				}
			})
		})

		When("creating another credential with different roles", func() {
			BeforeEach(func() {
				FakeE2EOptions = fakeE2EOptions{
					Credentials: []fakeCredential{{
						Name:      "testing-123",
						Roles:     []string{"role3", "role4", "role5"},
						ExpiresAt: "2030-01-01T13:45:00.000000",
					}},
					UserName: "user-id",
				}
				clusterUserClientSet = &ClusterUserClientSet{
					ClientSet: ClientSet{IdentityClient: GetTestIdentityClient(GinkgoT())},
					User:      &users.User{ID: FakeE2EOptions.UserName, Name: FakeE2EOptions.UserName},
				}
			})

			It("should create credential with correct name and roles", func() {
				cred, err := clusterUserClientSet.CreateApplicationCredential(ctx, FakeE2EOptions.Credentials[0].Name, FakeE2EOptions.Credentials[0].Roles)
				Expect(err).NotTo(HaveOccurred())
				Expect(cred.Name).To(Equal(FakeE2EOptions.Credentials[0].Name))
				for i, roleName := range FakeE2EOptions.Credentials[0].Roles {
					Expect(cred.Roles[i].Name).To(Equal(roleName))
				}
			})
		})
	})
})
