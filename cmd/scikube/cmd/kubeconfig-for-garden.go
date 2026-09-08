package cmd

import (
	"errors"
	"fmt"

	"github.com/sapcc/go-bits/must"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"sigs.k8s.io/yaml"

	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"

	"github.com/sap-cloud-infrastructure/persephone/internal/auth"
	"github.com/sap-cloud-infrastructure/persephone/internal/constants"
	"github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
	"github.com/sap-cloud-infrastructure/persephone/internal/openstack"
)

var (
	kubeconfigForGardenCmd = &cobra.Command{
		Use:   "kubeconfig-for-garden",
		Short: "Create kubeconfig file for a garden cluster.",
		RunE:  kubeconfigForGarden,
	}
)

func init() {
	addOpenstackAuthFlags(kubeconfigForGardenCmd)

	kubeconfigForGardenCmd.PersistentFlags().Bool("debug", false, "Enable debug output (default $SCIKUBE_DEBUG)")
	must.Succeed(viper.BindPFlag("SCIKUBE_DEBUG", kubeconfigForGardenCmd.PersistentFlags().Lookup("debug")))
	kubeconfigForGardenCmd.PersistentFlags().String("landscape", "canary", "Persephone landscape to choose (prod, canary or qa) (default $SCIKUBE_LANDSCAPE)")
	must.Succeed(viper.BindPFlag("SCIKUBE_LANDSCAPE", kubeconfigForGardenCmd.PersistentFlags().Lookup("landscape")))
	kubeconfigForGardenCmd.PersistentFlags().String("url", "", "Manually define Persephone URL without looking into the keystone catalog (default $SCIKUBE_URL)")
	must.Succeed(viper.BindPFlag("SCIKUBE_URL", kubeconfigForGardenCmd.PersistentFlags().Lookup("url")))
	kubeconfigForGardenCmd.PersistentFlags().String("project-id", "", "Manually define the project id without querying keystone (default $SCIKUBE_PROJECT_ID)")
	must.Succeed(viper.BindPFlag("SCIKUBE_PROJECT_ID", kubeconfigForGardenCmd.PersistentFlags().Lookup("project-id")))

	viper.AutomaticEnv()
}

func kubeconfigForGarden(cmd *cobra.Command, args []string) error {
	authOptions, err := getAuthOptions()
	if err != nil {
		return err
	}

	apiUrl := viper.GetString("SCIKUBE_URL")
	projectID := viper.GetString("SCIKUBE_PROJECT_ID")
	region := viper.GetString("OS_REGION_NAME")

	if apiUrl == "" {
		if projectID != "" {
			return errors.New("when the openstack catalog is queried, --project-id cannot be used")
		}

		providerClient, err := auth.AuthenticatedClientFunc(cmd.Context(), authOptions)
		if err != nil {
			return fmt.Errorf("could not create provider client: %w", err)
		}

		authResult, ok := providerClient.GetAuthResult().(tokens.CreateResult)
		if !ok {
			return errors.New("could not get result from auth request (reason unknown)")
		}

		project, err := authResult.ExtractProject()
		if err != nil {
			return fmt.Errorf("could not extract project from project result: %w", err)
		}
		authOptions.Scope.ProjectID = project.ID

		endpointOpts := openstack.PersephoneEndpointOpts{
			Landscape: viper.GetString("SCIKUBE_LANDSCAPE"),
			Region:    viper.GetString("OS_REGION_NAME"),
		}
		endpoint, err := openstack.GetPersephoneEndpoint(providerClient.EndpointLocator, authResult.ExtractServiceCatalog, endpointOpts)
		if err != nil {
			return err
		}
		apiUrl = endpoint.URL
		region = endpoint.Region
	} else {
		if projectID == "" {
			return errors.New("when --url is set, --project-id must be set, too")
		}

		authOptions.Scope.ProjectID = projectID
	}

	authMethod := constants.KeystonePasswordAuth
	if authOptions.ApplicationCredentialID != "" || authOptions.ApplicationCredentialName != "" || authOptions.ApplicationCredentialSecret != "" {
		authMethod = constants.KeystoneApplicationCredentialAuth
	}

	kubeconfig, err := kubernetes.GetKubeconfigForGarden(authOptions, authMethod, apiUrl, region)
	if err != nil {
		return fmt.Errorf("could not create KUBECONFIG: %w", err)
	}

	yamlData, err := yaml.Marshal(kubeconfig)
	if err != nil {
		return fmt.Errorf("could not marshal kubeconfig as YAML: %w", err)
	}
	fmt.Println(string(yamlData))
	return nil
}
