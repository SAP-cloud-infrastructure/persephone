// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/sapcc/go-bits/must"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func addOpenstackAuthFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().String("os-auth-url", "", "URL for the OpenStack Keystone API (default $OS_AUTH_URL)")
	must.Succeed(viper.BindPFlag("OS_AUTH_URL", cmd.PersistentFlags().Lookup("os-auth-url")))
	cmd.PersistentFlags().String("os-domain-name", "", "Openstack domain name (default $OS_DOMAIN_NAME)")
	must.Succeed(viper.BindPFlag("OS_DOMAIN_NAME", cmd.PersistentFlags().Lookup("os-domain-name")))
	cmd.PersistentFlags().String("os-user-domain-name", "", "Openstack domain name (default $OS_USER_DOMAIN_NAME)")
	must.Succeed(viper.BindPFlag("OS_USER_DOMAIN_NAME", cmd.PersistentFlags().Lookup("os-user-domain-name")))
	cmd.PersistentFlags().String("os-project-domain-name", "", "Openstack domain name (default $OS_PROJECT_DOMAIN_NAME)")
	must.Succeed(viper.BindPFlag("OS_PROJECT_DOMAIN_NAME", cmd.PersistentFlags().Lookup("os-project-domain-name")))
	cmd.PersistentFlags().String("os-region-name", "", "Openstack region name (default $OS_REGION_NAME)")
	must.Succeed(viper.BindPFlag("OS_REGION_NAME", cmd.PersistentFlags().Lookup("os-region-name")))
	cmd.PersistentFlags().String("os-username", "", "Openstack user name (default $OS_USERNAME)")
	must.Succeed(viper.BindPFlag("OS_USERNAME", cmd.PersistentFlags().Lookup("os-username")))
	cmd.PersistentFlags().String("os-project-name", "", "Openstack project name (default $OS_PROJECT_NAME)")
	must.Succeed(viper.BindPFlag("OS_PROJECT_NAME", cmd.PersistentFlags().Lookup("os-project-name")))
	cmd.PersistentFlags().String("os-password", "", "Openstack password (default $OS_PASSWORD)")
	must.Succeed(viper.BindPFlag("OS_PASSWORD", cmd.PersistentFlags().Lookup("os-password")))
	cmd.PersistentFlags().String("os-application-credential-id", "", "Openstack Application Credential ID (default $OS_APPLICATION_CREDENTIAL_ID)")
	must.Succeed(viper.BindPFlag("OS_APPLICATION_CREDENTIAL_ID", cmd.PersistentFlags().Lookup("os-application-credential-id")))
	cmd.PersistentFlags().String("os-application-credential-name", "", "Openstack Application Credential Name (default $OS_APPLICATION_CREDENTIAL_NAME)")
	must.Succeed(viper.BindPFlag("OS_APPLICATION_CREDENTIAL_NAME", cmd.PersistentFlags().Lookup("os-application-credential-name")))
	cmd.PersistentFlags().String("os-application-credential-secret", "", "Openstack Application Credential Secret (default $OS_APPLICATION_CREDENTIAL_SECRET)")
	must.Succeed(viper.BindPFlag("OS_APPLICATION_CREDENTIAL_SECRET", cmd.PersistentFlags().Lookup("os-application-credential-secret")))
}

func getAuthOptions() (gophercloud.AuthOptions, error) {
	authURL := viper.GetString("OS_AUTH_URL")
	domain = viper.GetString("OS_USER_DOMAIN_NAME")
	if domain == "" {
		domain = viper.GetString("OS_DOMAIN_NAME")
	}
	project = viper.GetString("OS_PROJECT_NAME")
	user = viper.GetString("OS_USERNAME")
	password = viper.GetString("OS_PASSWORD")
	applicationCredentialID = viper.GetString("OS_APPLICATION_CREDENTIAL_ID")
	applicationCredentialName = viper.GetString("OS_APPLICATION_CREDENTIAL_NAME")
	applicationCredentialSecret = viper.GetString("OS_APPLICATION_CREDENTIAL_SECRET")
	noRegionPrefix = viper.GetBool("NO_REGION_PREFIX")
	runE2ETest = viper.GetBool("RUN_E2E_TEST")

	if authURL == "" && !runE2ETest {
		return gophercloud.AuthOptions{}, errAuthURLMissing
	}

	if applicationCredentialSecret == "" && !runE2ETest {
		missing := []string{}
		if domain == "" {
			missing = append(missing, "--os-user-domain-name/OS_USER_DOMAIN_NAME or --os-domain-name/OS_DOMAIN_NAME")
		}
		if user == "" {
			missing = append(missing, "--os-username/OS_USERNAME")
		}
		if project == "" {
			missing = append(missing, "--os-project-name/OS_PROJECT_NAME")
		}
		if password == "" {
			missing = append(missing, "--os-password/OS_PASSWORD")
		}
		if len(missing) > 0 {
			return gophercloud.AuthOptions{}, fmt.Errorf("when using password auth, please provide --os-domain-name, --os-project-name, --os-username and --os-password, you are missing %s", strings.Join(missing, ", "))
		}
	} else if (applicationCredentialID == "" || applicationCredentialName == "") && !runE2ETest {
		return gophercloud.AuthOptions{}, errors.New("application credentials missing")
	}

	// this is mostly copied from https://github.com/sapcc/go-bits/blob/master/gophercloudext/auth.go#L94-L124
	// minus the ID variants of domain, user and projects

	// most other consistency checks are delegated to gophercloud.AuthOptions,
	// so we just build an AuthOptions without checking a lot of stuff
	scope := gophercloud.AuthScope{
		ProjectName: project,
	}
	if scope.ProjectName == "" {
		// not project scope, so might be domain scope
		scope.DomainName = viper.GetString("OS_DOMAIN_NAME")
		if scope.DomainName == "" {
			// not domain scope either, so might be system scope
			scope.System = viper.GetString("OS_PROJECT_DOMAIN_NAME") != ""
		}
	} else {
		// definitely project scope
		scope.DomainName = viper.GetString("OS_PROJECT_DOMAIN_NAME")
	}

	return gophercloud.AuthOptions{
		IdentityEndpoint:            authURL,
		Username:                    user,
		DomainName:                  domain,
		Password:                    password,
		AllowReauth:                 true,
		Scope:                       &scope,
		ApplicationCredentialID:     applicationCredentialID,
		ApplicationCredentialName:   applicationCredentialName,
		ApplicationCredentialSecret: applicationCredentialSecret,
	}, nil
}
