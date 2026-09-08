// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/must"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
)

const (
	// DefaultShootKubeconfigExpiration is a constant for the default expiration assigned to kubeconfig files created using 'scikube kubeconfig-for-shoot'.
	DefaultShootKubeconfigExpiration = 8 * time.Hour
)

var (
	kubeconfigForShootCmd = &cobra.Command{
		Use:   "kubeconfig-for-shoot",
		Short: "Create kubeconfig file for a shoot cluster",
		RunE:  kubeconfigForShoot,
		Long: `Create kubeconfig file for a shoot cluster.

Sample usage:
  # create the garden cluster kubeconfig
  scikube kubeconfig-for-garden <args> > garden-kubeconfig.yaml

  # if the garden kubeconfig was created using password authentication, then OS_PASSWORD must be exported
  export OS_PASSWORD='...'

  # ensure 'scikube' command is in PATH
  export PATH="/path/to/scikube:$PATH"

  # create the shoot cluster kubeconfig for a shoot 'my-shoot' belonging to this garden cluster (to expire after 1 hour)
  export KUBECONFIG=garden-kubeconfig.yaml
  scikube kubeconfig-for-shoot --name my-shoot --expiration 1h > shoot-kubeconfig.yaml

  # operate on shoot cluster 'my-shoot'
  export KUBECONFIG=shoot-kubeconfig.yaml
  kubectl run box --image busybox -i --rm -- echo 'Hello world'
`,
	}
)

func init() {
	// the name of one of the shoots returned by "kubectl get shoot" for the given garden cluster kubeconfig
	kubeconfigForShootCmd.PersistentFlags().String("name", "", "The shoot cluster name (default $SCIKUBE_SHOOT_NAME)")
	must.Succeed(viper.BindPFlag("SCIKUBE_SHOOT_NAME", kubeconfigForShootCmd.PersistentFlags().Lookup("name")))

	// the expiration (expressed as type 'time.Duration') to be assigned to the newly generated shoot kubeconfig
	kubeconfigForShootCmd.PersistentFlags().Duration("expiration", DefaultShootKubeconfigExpiration, "The expiration (expressed as type 'time.Duration') (default $SCIKUBE_SHOOT_KUBECONFIG_EXPIRATION)")
	must.Succeed(viper.BindPFlag("SCIKUBE_SHOOT_KUBECONFIG_EXPIRATION", kubeconfigForShootCmd.PersistentFlags().Lookup("expiration")))

	// the garden cluster kubeconfig file
	kubeconfigForShootCmd.PersistentFlags().String("kubeconfig", "", "A kubeconfig file pointing to the garden cluster (default $KUBECONFIG)")
	must.Succeed(viper.BindPFlag("KUBECONFIG", kubeconfigForShootCmd.PersistentFlags().Lookup("kubeconfig")))

	viper.AutomaticEnv()
}

func kubeconfigForShoot(cmd *cobra.Command, args []string) error {
	// the garden kubeconfig depends on 'scikube auth' (see 'users[0].user.exec.command')
	if _, err := exec.LookPath("scikube"); err != nil {
		return errors.New("command 'scikube' must be in PATH")
	}

	gardenKubeconfigPath := viper.GetString("KUBECONFIG")
	if gardenKubeconfigPath == "" {
		return errors.New("--kubeconfig/KUBECONFIG (garden cluster kubeconfig file) must not be empty")
	}
	logg.Debug("Got garden cluster kubeconfig: " + gardenKubeconfigPath)

	shootName := viper.GetString("SCIKUBE_SHOOT_NAME")
	if shootName == "" {
		return errors.New("--name/SCIKUBE_SHOOT_NAME must not be empty")
	}
	logg.Debug("Got shoot cluster name: " + shootName)

	expiration := viper.GetDuration("SCIKUBE_SHOOT_KUBECONFIG_EXPIRATION")
	logg.Debug("Got shoot kubeconfig expiration: " + expiration.String())

	shootKubeconfig, err := kubernetes.GetKubeconfigForShoot(cmd.Context(), gardenKubeconfigPath, shootName, expiration)
	if err != nil {
		return err
	}

	fmt.Println(string(shootKubeconfig))

	return nil
}
