// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"fmt"

	"github.com/sapcc/go-api-declarations/bininfo"
	"github.com/sapcc/go-bits/logg"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	rootCmd = &cobra.Command{
		Use:          "scikube",
		Short:        "CLI to interact with SCI Kubernetes service",
		Long:         "CLI to interact with SCI Kubernetes service",
		Version:      bininfo.Version(),
		SilenceUsage: true,
	}

	domain                      string
	user                        string
	project                     string
	password                    string
	applicationCredentialID     string
	applicationCredentialName   string
	applicationCredentialSecret string
	noRegionPrefix              bool
	runE2ETest                  bool

	errAuthURLMissing = errors.New("--os-auth-url/OS_AUTH_URL is required")
)

type (
	keystoneAuthError struct {
		err error
	}
)

func (e *keystoneAuthError) Error() string {
	return fmt.Sprintf("Keystone auth failed: %s", e.err)
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.AddCommand(authCmd)
	rootCmd.AddCommand(kubeconfigForGardenCmd)
	rootCmd.AddCommand(kubeconfigForShootCmd)
}

func initConfig() {
	logg.ShowDebug = viper.GetBool("SCIKUBE_DEBUG")
	logg.Debug("Debug output is ON")
}
