// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package kubernetes_test

import (
	"github.com/gophercloud/gophercloud/v2"
	api "k8s.io/client-go/tools/clientcmd/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/sap-cloud-infrastructure/persephone/internal/constants"
	. "github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
)

var _ = Describe("GetKubeconfigForGarden", func() {

	It("Fails on empty API Endpoint", func() {
		apiEndpoint := ""
		region := ""
		_, err := GetKubeconfigForGarden(gophercloud.AuthOptions{}, constants.KeystoneApplicationCredentialAuth, apiEndpoint, region)
		Expect(err).To(MatchError(ContainSubstring("API endpoint")))
	})

	It("Fails on empty Keystone RegionName", func() {
		apiEndpoint := "https://virtual-garden.persephone.somecloud"
		region := ""
		authOptions := gophercloud.AuthOptions{
			IdentityEndpoint: "https://keystone-api.eu-de-1.somecloud/v3",
			DomainName:       "the-domain",
			Username:         "the-username",
			Password:         "the-password",
		}
		_, err := GetKubeconfigForGarden(authOptions, constants.KeystoneApplicationCredentialAuth, apiEndpoint, region)
		Expect(err).To(MatchError(ContainSubstring("missing Keystone RegionName")))
	})

	It("Fails on missing Keystone IdentityEndpoint", func() {
		apiEndpoint := "https://virtual-garden.persephone.somecloud"
		region := "eu-de-1"
		_, err := GetKubeconfigForGarden(gophercloud.AuthOptions{}, constants.KeystoneApplicationCredentialAuth, apiEndpoint, region)
		Expect(err).To(MatchError(ContainSubstring("missing Keystone IdentityEndpoint")))
	})

	It("Fails on missing Keystone DomainName", func() {
		apiEndpoint := "https://virtual-garden.persephone.somecloud"
		region := "eu-de-1"
		authOptions := gophercloud.AuthOptions{
			IdentityEndpoint: "https://keystone-api.eu-de-1.somecloud/v3",
			DomainName:       "",
			Username:         "the-username",
			Password:         "the-password",
		}
		_, err := GetKubeconfigForGarden(authOptions, constants.KeystoneApplicationCredentialAuth, apiEndpoint, region)
		Expect(err).To(MatchError(ContainSubstring("missing Keystone DomainName")))
	})

	It("Fails on missing Keystone Scope", func() {
		apiEndpoint := "https://virtual-garden.persephone.somecloud"
		region := "eu-de-1"
		authOptions := gophercloud.AuthOptions{
			IdentityEndpoint: "https://keystone-api.eu-de-1.somecloud/v3",
			DomainName:       "the-domain",
			Username:         "the-username",
			Password:         "the-password",
		}
		_, err := GetKubeconfigForGarden(authOptions, constants.KeystonePasswordAuth, apiEndpoint, region)
		Expect(err).To(MatchError(ContainSubstring("missing Keystone Scope")))
	})

	It("Fails on missing Keystone Scope.ProjectID", func() {
		apiEndpoint := "https://virtual-garden.persephone.somecloud"
		region := "eu-de-1"
		authOptions := gophercloud.AuthOptions{
			IdentityEndpoint: "https://keystone-api.eu-de-1.somecloud/v3",
			DomainName:       "the-domain-name",
			Username:         "the-username",
			Password:         "the-password",
			Scope: &gophercloud.AuthScope{
				ProjectID:   "",
				ProjectName: "the-project-name",
			},
		}
		_, err := GetKubeconfigForGarden(authOptions, constants.KeystonePasswordAuth, apiEndpoint, region)
		Expect(err).To(MatchError(ContainSubstring("missing Keystone Scope.ProjectID")))
	})

	It("Fails on missing Keystone Scope.ProjectName", func() {
		apiEndpoint := "https://virtual-garden.persephone.somecloud"
		region := "eu-de-1"
		authOptions := gophercloud.AuthOptions{
			IdentityEndpoint: "https://keystone-api.eu-de-1.somecloud/v3",
			DomainName:       "the-domain-name",
			Username:         "the-username",
			Password:         "the-password",
			Scope: &gophercloud.AuthScope{
				ProjectID:   "TheProjectID",
				ProjectName: "",
			},
		}
		_, err := GetKubeconfigForGarden(authOptions, constants.KeystonePasswordAuth, apiEndpoint, region)
		Expect(err).To(MatchError(ContainSubstring("missing Keystone Scope.ProjectName")))
	})

	It("Fails on missing Keystone Username when using password authentication", func() {
		apiEndpoint := "https://virtual-garden.persephone.somecloud"
		region := "eu-de-1"
		authOptions := gophercloud.AuthOptions{
			IdentityEndpoint: "https://keystone-api.eu-de-1.somecloud/v3",
			DomainName:       "the-domain-name",
			Username:         "",
			Scope: &gophercloud.AuthScope{
				ProjectID:   "TheProjectID",
				ProjectName: "the-project-name",
			},
		}
		_, err := GetKubeconfigForGarden(authOptions, constants.KeystonePasswordAuth, apiEndpoint, region)
		Expect(err).To(MatchError(ContainSubstring("when using password-auth")))
	})

	It("Fails on missing Keystone Password when using password authentication", func() {
		apiEndpoint := "https://virtual-garden.persephone.somecloud"
		region := "eu-de-1"
		authOptions := gophercloud.AuthOptions{
			IdentityEndpoint: "https://keystone-api.eu-de-1.somecloud/v3",
			DomainName:       "the-domain-name",
			Username:         "the-username",
			Password:         "",
			Scope: &gophercloud.AuthScope{
				ProjectID:   "TheProjectID",
				ProjectName: "the-project-name",
			},
		}
		_, err := GetKubeconfigForGarden(authOptions, constants.KeystonePasswordAuth, apiEndpoint, region)
		Expect(err).To(MatchError(ContainSubstring("when using password-auth")))
	})

	Describe("Validation of application credentials ID/name/secret combination", func() {
		var apiEndpoint = "https://virtual-garden.persephone.somecloud"
		var region = "eu-de-1"

		It("Enforces ID", func() {
			authOptions := gophercloud.AuthOptions{
				IdentityEndpoint:            "https://keystone-api.eu-de-1.somecloud/v3",
				DomainName:                  "the-domain-name",
				ApplicationCredentialID:     "",
				ApplicationCredentialName:   "the-name",
				ApplicationCredentialSecret: "the-secret",
				Scope: &gophercloud.AuthScope{
					ProjectID:   "TheProjectID",
					ProjectName: "the-project-name",
				},
			}
			_, err := GetKubeconfigForGarden(authOptions, constants.KeystoneApplicationCredentialAuth, apiEndpoint, region)
			Expect(err).To(MatchError(ContainSubstring("when using application-credential-auth")))
		})

		It("Enforces Name", func() {
			authOptions := gophercloud.AuthOptions{
				IdentityEndpoint:            "https://keystone-api.eu-de-1.somecloud/v3",
				DomainName:                  "the-domain-name",
				ApplicationCredentialID:     "the-id",
				ApplicationCredentialName:   "",
				ApplicationCredentialSecret: "the-secret",
				Scope: &gophercloud.AuthScope{
					ProjectID:   "TheProjectID",
					ProjectName: "the-project-name",
				},
			}
			_, err := GetKubeconfigForGarden(authOptions, constants.KeystoneApplicationCredentialAuth, apiEndpoint, region)
			Expect(err).To(MatchError(ContainSubstring("when using application-credential-auth")))
		})

		It("Enforces Secret", func() {
			authOptions := gophercloud.AuthOptions{
				IdentityEndpoint:            "https://keystone-api.eu-de-1.somecloud/v3",
				DomainName:                  "the-domain-name",
				ApplicationCredentialID:     "the-id",
				ApplicationCredentialName:   "the-name",
				ApplicationCredentialSecret: "",
				Scope: &gophercloud.AuthScope{
					ProjectID:   "TheProjectID",
					ProjectName: "the-project-name",
				},
			}
			_, err := GetKubeconfigForGarden(authOptions, constants.KeystoneApplicationCredentialAuth, apiEndpoint, region)
			Expect(err).To(MatchError(ContainSubstring("when using application-credential-auth")))
		})
	})

	Describe("Successful kubeconfig generation", func() {
		var (
			apiEndpoint = "https://persephone-api.somecloud"
			region      = "eu-de-1"
			scope       = &gophercloud.AuthScope{
				ProjectID:   "TheProjectID",
				ProjectName: "the-project-name",
			}

			expectedClusters = []api.NamedCluster{{
				Name: "garden",
				Cluster: api.Cluster{
					Server: apiEndpoint,
				},
			}}
			expectedContexts = []api.NamedContext{{
				Name: "garden",
				Context: api.Context{
					AuthInfo:  "garden-user",
					Cluster:   "garden",
					Namespace: "garden-eu-de-1-TheProjectID",
				},
			}}
		)

		It("Includes OS_USERNAME and not OS_PASSWORD when using password authentication", func() {
			var (
				authOptions = gophercloud.AuthOptions{
					IdentityEndpoint: "https://keystone-api.eu-de-1.somecloud/v3",
					DomainName:       "the-domain-name",
					Scope:            scope,
					Username:         "the-username",
					Password:         "the-password",
				}
				expectedKubeconfig = api.Config{
					APIVersion:     "v1",
					Kind:           "Config",
					Clusters:       expectedClusters,
					Contexts:       expectedContexts,
					CurrentContext: "garden",
					AuthInfos: []api.NamedAuthInfo{{
						Name: "garden-user",
						AuthInfo: api.AuthInfo{
							Exec: &api.ExecConfig{
								APIVersion: "client.authentication.k8s.io/v1",
								Command:    "scikube",
								Args:       []string{"auth"},
								Env: []api.ExecEnvVar{
									{Name: "OS_AUTH_URL", Value: authOptions.IdentityEndpoint},
									{Name: "OS_DOMAIN_NAME", Value: authOptions.DomainName},
									{Name: "OS_PROJECT_DOMAIN_NAME", Value: authOptions.DomainName},
									{Name: "OS_PROJECT_NAME", Value: authOptions.Scope.ProjectName},
									{Name: "OS_USERNAME", Value: authOptions.Username},
								},
								InstallHint:        "Download the auth plugin from https://github.com/sap-cloud-infrastructure/persephone/releases/latest and add it to PATH",
								ProvideClusterInfo: true,
								InteractiveMode:    api.NeverExecInteractiveMode,
							},
						},
					},
					},
				}
			)

			kubeconfig, err := GetKubeconfigForGarden(authOptions, constants.KeystonePasswordAuth, apiEndpoint, region)
			Expect(err).ToNot(HaveOccurred())
			Expect(kubeconfig).To(Equal(expectedKubeconfig))
		})

		It("Includes OS_APPLICATION_CREDENTIAL_* variables when using application credentials", func() {
			var (
				authOptions = gophercloud.AuthOptions{
					IdentityEndpoint:            "https://keystone-api.eu-de-1.somecloud/v3",
					DomainName:                  "the-domain-name",
					Scope:                       scope,
					ApplicationCredentialID:     "the-id",
					ApplicationCredentialName:   "the-name",
					ApplicationCredentialSecret: "the-secret",
				}
				expectedKubeconfig = api.Config{
					APIVersion:     "v1",
					Kind:           "Config",
					Clusters:       expectedClusters,
					Contexts:       expectedContexts,
					CurrentContext: "garden",
					AuthInfos: []api.NamedAuthInfo{{
						Name: "garden-user",
						AuthInfo: api.AuthInfo{
							Exec: &api.ExecConfig{
								APIVersion: "client.authentication.k8s.io/v1",
								Command:    "scikube",
								Args:       []string{"auth"},
								Env: []api.ExecEnvVar{
									{Name: "OS_AUTH_URL", Value: authOptions.IdentityEndpoint},
									{Name: "OS_DOMAIN_NAME", Value: authOptions.DomainName},
									{Name: "OS_PROJECT_DOMAIN_NAME", Value: authOptions.DomainName},
									{Name: "OS_PROJECT_NAME", Value: authOptions.Scope.ProjectName},
									{Name: "OS_APPLICATION_CREDENTIAL_ID", Value: authOptions.ApplicationCredentialID},
									{Name: "OS_APPLICATION_CREDENTIAL_NAME", Value: authOptions.ApplicationCredentialName},
									{Name: "OS_APPLICATION_CREDENTIAL_SECRET", Value: authOptions.ApplicationCredentialSecret},
								},
								InstallHint:        "Download the auth plugin from https://github.com/sap-cloud-infrastructure/persephone/releases/latest and add it to PATH",
								ProvideClusterInfo: true,
								InteractiveMode:    api.NeverExecInteractiveMode,
							},
						},
					}},
				}
			)

			kubeconfig, err := GetKubeconfigForGarden(authOptions, constants.KeystoneApplicationCredentialAuth, apiEndpoint, region)
			Expect(err).ToNot(HaveOccurred())
			Expect(kubeconfig).To(Equal(expectedKubeconfig))
		})
	})
})
