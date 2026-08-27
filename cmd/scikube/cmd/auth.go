// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/must"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/sap-cloud-infrastructure/persephone/internal/auth"
	"github.com/sap-cloud-infrastructure/persephone/internal/openstack"

	v1 "k8s.io/client-go/pkg/apis/clientauthentication/v1"
)

var (
	authCmd = &cobra.Command{
		Use:   "auth",
		Short: "kubectl authentication plugin",
		RunE:  authenticate,
	}

	authFunc                        func(context.Context, gophercloud.AuthOptions, bool) (*v1.ExecCredential, error) = auth.Do
	cacheKeyFunc                    func(cst auth.CacheStorageType, ao gophercloud.AuthOptions) (string, error)      = auth.CacheKey
	newExecCredentialReadWriterFunc func(cst auth.CacheStorageType) (auth.ExecCredentialReadWriter, error)           = auth.NewExecCredentialReadWriter
)

func init() {
	addOpenstackAuthFlags(authCmd)

	authCmd.PersistentFlags().Bool("no-region-prefix", false, "Disable region prefix in bearer token")
	must.Succeed(viper.BindPFlag("NO_REGION_PREFIX", authCmd.PersistentFlags().Lookup("no-region-prefix")))
	authCmd.PersistentFlags().Bool("debug", false, "Enable debug output (default $SCIKUBE_DEBUG)")
	must.Succeed(viper.BindPFlag("SCIKUBE_DEBUG", authCmd.PersistentFlags().Lookup("debug")))
	authCmd.PersistentFlags().Bool("run-e2e-test", false, "Enable e2e test mocking (default $RUN_E2E_TEST)")
	must.Succeed(viper.BindPFlag("RUN_E2E_TEST", authCmd.PersistentFlags().Lookup("run-e2e-test")))

	viper.AutomaticEnv()
}

func authenticate(cmd *cobra.Command, args []string) error {
	authOptions, err := getAuthOptions()
	if err != nil {
		return err
	}

	var (
		cacheKey                         string
		cacheStorage                     auth.ExecCredentialReadWriter
		cacheStorageType                 = auth.FileStorage
		cred                             *v1.ExecCredential
		expiredCachedExecCredentialError *auth.ExpiredCachedExecCredentialError
		mustReauth                       = true
		useCache                         = true
	)

	if noRegionPrefix {
		logg.Debug("Region prefix is OFF")
	} else {
		logg.Debug("Region prefix is ON")
	}

	if runE2ETest {
		// we do not want to test against the cache file in e2e tests
		useCache = false
		auth.AuthenticatedClientFunc = openstack.GetFakeE2EClient

		if authOptions.DomainName == "" {
			authOptions.DomainName = "Default"
		}
		if authOptions.Username == "" {
			authOptions.Username = "dummy"
		}
		if authOptions.Password == "" {
			authOptions.Password = "dummy"
		}
		if authOptions.TokenID == "" {
			authOptions.TokenID = "dummy"
		}

		openstack.FakeE2EOptions.UserName = authOptions.Username

		logg.Debug("E2E test mode is ON")
	}

	if useCache {
		logg.Debug("Credential caching is ON (storage type = %s)", cacheStorageType)

		var err error
		cacheStorage, err = newExecCredentialReadWriterFunc(cacheStorageType)
		if err != nil {
			logg.Debug("could not initialize cache storage handler: %s", err)
			useCache = false
			logg.Debug("Credential caching is OFF")
		}

		cacheKey, err = cacheKeyFunc(cacheStorageType, authOptions)
		if err != nil {
			logg.Debug("could not determine cache key: %s", err)
			useCache = false
			logg.Debug("Credential caching is OFF")
		} else {
			logg.Debug("Attempting to use cache key " + cacheKey)
		}

		cred, err = cacheStorage.Read(cacheKey)
		switch {
		case err == nil:
			mustReauth = false
			logg.Debug("Cached credentials loaded successfully")
		case errors.Is(err, auth.ErrCacheKeyNotFound):
			logg.Debug("Cache key not found")
		case errors.As(err, &expiredCachedExecCredentialError):
			logg.Debug("Cached credentials have expired")
		default:
			logg.Debug("could not read credentials from cache storage: %s", err)
			useCache = false
			logg.Debug("Credential caching is OFF (storage type = %s); delete cache key %s in order to use caching", cacheStorageType, cacheKey)
		}
	}

	if mustReauth {
		logg.Debug("Authenticating with Keystone...")
		var err error
		cred, err = authFunc(cmd.Context(), authOptions, noRegionPrefix)
		if err != nil {
			return &keystoneAuthError{err}
		}
	}

	credBytes, err := json.MarshalIndent(cred, "", "  ")
	if err != nil {
		return fmt.Errorf("could not marshal fresh credentials to JSON: %w", err)
	}
	fmt.Println(string(credBytes))

	if useCache && mustReauth {
		logg.Debug("Attempting to write fresh credentials to cache file...")
		if err := cacheStorage.Write(cacheKey, cred); err != nil {
			return fmt.Errorf("could not write fresh credentials to cache storage with key %s: %w", cacheKey, err)
		}
		logg.Debug("Done")
	}

	return nil
}
