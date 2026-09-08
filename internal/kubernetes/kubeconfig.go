package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sapcc/go-bits/logg"

	authenticationv1alpha1 "github.com/gardener/gardener/pkg/apis/authentication/v1alpha1"
	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gophercloud/gophercloud/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	api "k8s.io/client-go/tools/clientcmd/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sap-cloud-infrastructure/persephone/internal/constants"
)

// GetKubeconfigForGarden creates a kubeconfig for a garden cluster.
func GetKubeconfigForGarden(authOptions gophercloud.AuthOptions, authMethod constants.KeystoneAuthMethod, apiEndpoint, region string) (api.Config, error) {
	if apiEndpoint == "" {
		return api.Config{}, errors.New("API endpoint should be a valid URL")
	}

	if region == "" {
		return api.Config{}, errors.New("missing Keystone RegionName")
	}

	if authOptions.IdentityEndpoint == "" {
		return api.Config{}, errors.New("missing Keystone IdentityEndpoint")
	}

	if authOptions.DomainName == "" {
		return api.Config{}, errors.New("missing Keystone DomainName")
	}

	if authOptions.Scope == nil {
		return api.Config{}, errors.New("missing Keystone Scope")
	}

	if authOptions.Scope.ProjectID == "" {
		return api.Config{}, errors.New("missing Keystone Scope.ProjectID")
	}

	if authOptions.Scope.ProjectName == "" {
		return api.Config{}, errors.New("missing Keystone Scope.ProjectName")
	}

	appCredID := authOptions.ApplicationCredentialID
	appCredName := authOptions.ApplicationCredentialName
	appCredSecret := authOptions.ApplicationCredentialSecret

	switch authMethod {
	case constants.KeystoneApplicationCredentialAuth:
		if appCredID == "" || appCredName == "" || appCredSecret == "" {
			return api.Config{}, fmt.Errorf("application credential ID/Name/Secret must be provided when using %s Keystone authentication method", authMethod)
		}
	case constants.KeystonePasswordAuth:
		if authOptions.Username == "" || authOptions.Password == "" {
			return api.Config{}, fmt.Errorf("both username and password must be provided when using %s Keystone authentication method", authMethod)
		}
	default:
		return api.Config{}, fmt.Errorf("unsupported Keystone authentication method: %s", authMethod)
	}

	authInfoKey := "garden-user"
	authInfo := api.AuthInfo{
		Exec: &api.ExecConfig{
			APIVersion: "client.authentication.k8s.io/v1",
			Command:    "scikube",
			Args:       []string{"auth"},
			Env: []api.ExecEnvVar{
				{Name: "OS_AUTH_URL", Value: authOptions.IdentityEndpoint},
				{Name: "OS_DOMAIN_NAME", Value: authOptions.DomainName},
				{Name: "OS_PROJECT_DOMAIN_NAME", Value: authOptions.DomainName},
				{Name: "OS_PROJECT_NAME", Value: authOptions.Scope.ProjectName},
			},
			InstallHint:        "Download the auth plugin from https://github.com/sap-cloud-infrastructure/persephone/releases/latest and add it to PATH",
			ProvideClusterInfo: true,
			InteractiveMode:    api.NeverExecInteractiveMode,
		},
	}

	switch authMethod {
	case constants.KeystoneApplicationCredentialAuth:
		authInfo.Exec.Env = append(
			authInfo.Exec.Env,
			api.ExecEnvVar{Name: "OS_APPLICATION_CREDENTIAL_ID", Value: appCredID},
			api.ExecEnvVar{Name: "OS_APPLICATION_CREDENTIAL_NAME", Value: appCredName},
			api.ExecEnvVar{Name: "OS_APPLICATION_CREDENTIAL_SECRET", Value: appCredSecret},
		)
	case constants.KeystonePasswordAuth:
		authInfo.Exec.Env = append(
			authInfo.Exec.Env,
			api.ExecEnvVar{Name: "OS_USERNAME", Value: authOptions.Username},
			// We do not forward OS_PASSWORD here, to not expose the users password
		)
	}

	clusterKey := "garden"
	cluster := api.Cluster{
		Server: apiEndpoint,
	}

	contextKey := clusterKey
	ctx := api.Context{
		AuthInfo:  authInfoKey,
		Cluster:   clusterKey,
		Namespace: GetGardenerProjectNamespaceName(region, authOptions.Scope.ProjectID),
	}

	return api.Config{
		APIVersion:     "v1",
		Kind:           "Config",
		Clusters:       []api.NamedCluster{{Name: clusterKey, Cluster: cluster}},
		Contexts:       []api.NamedContext{{Name: clusterKey, Context: ctx}},
		CurrentContext: contextKey,
		AuthInfos:      []api.NamedAuthInfo{{Name: authInfoKey, AuthInfo: authInfo}},
	}, nil
}

// GetKubeconfigForShoot creates a kubeconfig for a shoot cluster.
func GetKubeconfigForShoot(ctx context.Context, gardenKubeconfigPath, shootName string, expiration time.Duration) ([]byte, error) {
	expirationSeconds := int64(expiration.Seconds())
	adminKubeconfigRequest := &authenticationv1alpha1.AdminKubeconfigRequest{
		Spec: authenticationv1alpha1.AdminKubeconfigRequestSpec{
			ExpirationSeconds: &expirationSeconds,
		},
	}

	gardenKubeconfig, err := clientcmd.LoadFromFile(gardenKubeconfigPath)
	if err != nil {
		return []byte(""), fmt.Errorf("could not load garden cluster kubeconfig: %w", err)
	}

	currentContext := gardenKubeconfig.CurrentContext
	if currentContext == "" {
		return []byte(""), errors.New("kubeconfig current context must not be empty")
	}
	logg.Debug("Got kubeconfig current context: " + currentContext)

	currentNamespace := gardenKubeconfig.Contexts[currentContext].Namespace
	if currentNamespace == "" {
		return []byte(""), errors.New("kubeconfig current context namespace must not be empty")
	}
	logg.Debug("Got kubeconfig current context namespace: " + currentNamespace)

	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: gardenKubeconfigPath}
	overrides := &clientcmd.ConfigOverrides{}
	restConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return []byte(""), fmt.Errorf("could not create REST config from garden cluster kubeconfig: %w", err)
	}

	kubeClient, err := client.New(restConfig, client.Options{Scheme: GardenerScheme})
	if err != nil {
		return []byte(""), fmt.Errorf("could not create garden cluster client from config: %w", err)
	}

	shoot := &gardenercorev1beta1.Shoot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      shootName,
			Namespace: currentNamespace,
		},
	}
	if err := kubeClient.SubResource("adminkubeconfig").Create(ctx, shoot, adminKubeconfigRequest); err != nil {
		return []byte(""), fmt.Errorf("could not create admin kubeconfig for shoot cluster: %w", err)
	}

	logg.Debug("Shoot cluster kubeconfig expiration: " + adminKubeconfigRequest.Status.ExpirationTimestamp.String())

	return adminKubeconfigRequest.Status.Kubeconfig, nil
}
