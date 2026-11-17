// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"strings"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	extensionssecretsmanager "github.com/gardener/gardener/extensions/pkg/util/secret/manager"
	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	clientcmdlatest "k8s.io/client-go/tools/clientcmd/api/latest"
	clientcmdv1 "k8s.io/client-go/tools/clientcmd/api/v1"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sap-cloud-infrastructure/persephone/internal/constants"
)

// Actuator reconciles Extension{type:"persephone-auditlog"} objects.
// It implements extension.Actuator from extensions/pkg/controller/extension.
type Actuator struct {
	Client             client.Client
	GardenClient       client.Client
	ReceiverImage      string
	S3EndpointTemplate string
}

// Reconcile ensures the receiver Deployment, Service, TLS secrets, and audit webhook
// kubeconfig are all in place.
func (a *Actuator) Reconcile(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension) error {
	namespace := ex.Namespace

	cfg, err := DecodeAuditLogConfig(ex.Spec.ProviderConfig)
	if err != nil {
		return fmt.Errorf("decode AuditLogConfig: %w", err)
	}

	cluster, err := extensionscontroller.GetCluster(ctx, a.Client, namespace)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}

	secretsManager, err := extensionssecretsmanager.SecretsManagerForCluster(
		ctx,
		log.WithName("secretsmanager"),
		clock.RealClock{},
		a.Client,
		cluster,
		ManagerIdentity,
		SecretConfigs(namespace),
	)
	if err != nil {
		return fmt.Errorf("create SecretsManager: %w", err)
	}

	secrets, err := extensionssecretsmanager.GenerateAllSecrets(ctx, secretsManager, SecretConfigs(namespace))
	if err != nil {
		return fmt.Errorf("generate TLS secrets: %w", err)
	}

	prefix, err := expandPrefix(cfg.ObjectStore.Prefix, cluster.Shoot.Name)
	if err != nil {
		return fmt.Errorf("expand prefix: %w", err)
	}

	receiverTLSName := secrets[ReceiverTLSSecretName].Name
	s3Endpoint := strings.ReplaceAll(a.S3EndpointTemplate, "{region}", cluster.Shoot.Spec.Region)
	deployment := ReceiverDeployment(namespace, a.ReceiverImage, cfg.ObjectStore.Bucket, prefix, s3Endpoint, receiverTLSName, cfg.ObjectStore.ObjectLock)
	if err := applyObject(ctx, a.Client, deployment); err != nil {
		return fmt.Errorf("apply receiver Deployment: %w", err)
	}

	service := ReceiverService(namespace)
	if err := applyObject(ctx, a.Client, service); err != nil {
		return fmt.Errorf("apply receiver Service: %w", err)
	}

	if err := a.syncWebhookKubeconfigToSeed(ctx, secrets, namespace); err != nil {
		return fmt.Errorf("sync webhook kubeconfig secret: %w", err)
	}

	if err := a.syncEC2CredentialsToSeed(ctx, log, cluster.Shoot, namespace); err != nil {
		return fmt.Errorf("sync EC2 credentials to seed: %w", err)
	}

	return secretsManager.Cleanup(ctx)
}

// Delete removes the receiver resources and the webhook kubeconfig.
func (a *Actuator) Delete(ctx context.Context, _ logr.Logger, ex *extensionsv1alpha1.Extension) error {
	namespace := ex.Namespace

	webhookSecret := &corev1.Secret{}
	webhookSecret.APIVersion = "v1"
	webhookSecret.Kind = "Secret"
	webhookSecret.Name = WebhookKubeconfigSecretName
	webhookSecret.Namespace = namespace
	if err := deleteObject(ctx, a.Client, webhookSecret); err != nil {
		return fmt.Errorf("delete webhook kubeconfig secret: %w", err)
	}
	ec2Secret := &corev1.Secret{}
	ec2Secret.APIVersion = "v1"
	ec2Secret.Kind = "Secret"
	ec2Secret.Name = constants.EC2CredentialsSecretName
	ec2Secret.Namespace = namespace
	if err := deleteObject(ctx, a.Client, ec2Secret); err != nil {
		return fmt.Errorf("delete EC2 credentials secret: %w", err)
	}
	if err := deleteObject(ctx, a.Client, ReceiverService(namespace)); err != nil {
		return fmt.Errorf("delete receiver Service: %w", err)
	}
	return deleteObject(ctx, a.Client, ReceiverDeployment(namespace, "", "", "", "", "", nil))
}

// ForceDelete skips graceful cleanup — used during forced shoot deletion.
func (a *Actuator) ForceDelete(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension) error {
	return a.Delete(ctx, log, ex)
}

// Migrate stores state before cross-seed migration. No persistent state for this extension.
func (a *Actuator) Migrate(_ context.Context, _ logr.Logger, _ *extensionsv1alpha1.Extension) error {
	return nil
}

// Restore re-creates resources after cross-seed migration.
func (a *Actuator) Restore(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension) error {
	return a.Reconcile(ctx, log, ex)
}

func applyObject(ctx context.Context, c client.Client, obj client.Object) error {
	return c.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("persephone-extension"))
}

func deleteObject(ctx context.Context, c client.Client, obj client.Object) error {
	return client.IgnoreNotFound(c.Delete(ctx, obj))
}

// expandPrefix replaces "{{.shootName}}" in prefix with the actual shoot name.
func expandPrefix(prefix, shootName string) (string, error) {
	tmpl, err := template.New("prefix").Parse(prefix)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"shootName": shootName}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// syncWebhookKubeconfigToSeed creates or updates a Secret in the seed shoot namespace
// containing the kubeconfig that kube-apiserver uses to authenticate to the audit webhook receiver.
func (a *Actuator) syncWebhookKubeconfigToSeed(ctx context.Context, secrets map[string]*corev1.Secret, namespace string) error {
	src := secrets[ForwarderClientTLSSecretName]
	if src == nil {
		return fmt.Errorf("seed secret %q not found", ForwarderClientTLSSecretName)
	}

	serverURL := fmt.Sprintf("https://%s.%s.svc/audit", ReceiverServiceName, namespace)
	kubeconfig, err := buildWebhookKubeconfig(serverURL, src.Data["ca.crt"], src.Data["tls.crt"], src.Data["tls.key"])
	if err != nil {
		return fmt.Errorf("build webhook kubeconfig: %w", err)
	}

	desired := &corev1.Secret{}
	desired.APIVersion = "v1"
	desired.Kind = "Secret"
	desired.Name = WebhookKubeconfigSecretName
	desired.Namespace = namespace
	desired.Data = map[string][]byte{"kubeconfig.yaml": kubeconfig}
	return applyObject(ctx, a.Client, desired)
}

// syncEC2CredentialsToSeed copies the newest EC2 InternalSecret from the virtual garden
// into a regular Secret in the seed shoot namespace so the shoot workload can use it.
func (a *Actuator) syncEC2CredentialsToSeed(ctx context.Context, log logr.Logger, shoot *gardenercorev1beta1.Shoot, seedNamespace string) error {
	if a.GardenClient == nil {
		return nil
	}

	list := &gardenercorev1beta1.InternalSecretList{}
	if err := a.GardenClient.List(ctx, list,
		client.InNamespace(shoot.Namespace),
		client.MatchingLabels{constants.LabelKeyShootName: shoot.Name, constants.LabelKeyEC2Credentials: "true"},
	); err != nil {
		return fmt.Errorf("list EC2 InternalSecrets in garden: %w", err)
	}

	if len(list.Items) == 0 {
		return fmt.Errorf("no EC2 InternalSecret found for Shoot %q in garden yet, requeuing", shoot.Name)
	}

	newest := &list.Items[0]
	for i := range list.Items {
		if list.Items[i].CreationTimestamp.After(newest.CreationTimestamp.Time) {
			newest = &list.Items[i]
		}
	}

	desired := &corev1.Secret{}
	desired.APIVersion = "v1"
	desired.Kind = "Secret"
	desired.Name = constants.EC2CredentialsSecretName
	desired.Namespace = seedNamespace
	desired.Data = newest.Data
	log.Info("Syncing EC2 credentials to seed namespace", "secret", client.ObjectKeyFromObject(desired))
	return applyObject(ctx, a.Client, desired)
}

func buildWebhookKubeconfig(serverURL string, caCert, clientCert, clientKey []byte) ([]byte, error) {
	cfg := &clientcmdv1.Config{
		APIVersion: "v1",
		Kind:       "Config",
		Clusters: []clientcmdv1.NamedCluster{
			{
				Name: "receiver",
				Cluster: clientcmdv1.Cluster{
					Server:                   serverURL,
					CertificateAuthorityData: caCert,
				},
			},
		},
		AuthInfos: []clientcmdv1.NamedAuthInfo{
			{
				Name: "apiserver",
				AuthInfo: clientcmdv1.AuthInfo{
					ClientCertificateData: clientCert,
					ClientKeyData:         clientKey,
				},
			},
		},
		Contexts: []clientcmdv1.NamedContext{
			{
				Name: "default",
				Context: clientcmdv1.Context{
					Cluster:  "receiver",
					AuthInfo: "apiserver",
				},
			},
		},
		CurrentContext: "default",
	}

	var buf bytes.Buffer
	if err := clientcmdlatest.Codec.Encode(cfg, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
