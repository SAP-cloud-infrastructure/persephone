// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"encoding/json"
	"fmt"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	auditingExtensionName = "auditing"
	persephoneBackendURL  = "https://%s.%s.svc:%d/audit"
)

type auditConfiguration struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Backends   []auditBackend `json:"backends"`
}

type auditBackend struct {
	DeliveryMode string           `json:"deliveryMode"`
	HTTP         *auditBackendHTTP `json:"http,omitempty"`
}

type auditBackendHTTP struct {
	URL string           `json:"url"`
	TLS *auditBackendTLS `json:"tls,omitempty"`
}

type auditBackendTLS struct {
	SecretName string `json:"secretName"`
}

// EnsureAuditingBackend upserts the persephone backend into the auditing
// Extension's providerConfig. Idempotent.
func EnsureAuditingBackend(ctx context.Context, c client.Client, namespace string) error {
	ext, cfg, err := getAuditingConfig(ctx, c, namespace)
	if err != nil {
		return err
	}

	url := fmt.Sprintf(persephoneBackendURL, ReceiverServiceName, namespace, receiverPort)

	cfg.Backends = removeBackendByURL(cfg.Backends, url)
	cfg.Backends = append(cfg.Backends, auditBackend{
		DeliveryMode: "BestEffort",
		HTTP: &auditBackendHTTP{
			URL: url,
			TLS: &auditBackendTLS{SecretName: ForwarderClientTLSSecretName},
		},
	})

	return patchAuditingConfig(ctx, c, ext, cfg)
}

// RemoveAuditingBackend removes the persephone backend from the auditing
// Extension's providerConfig. Idempotent.
func RemoveAuditingBackend(ctx context.Context, c client.Client, namespace string) error {
	ext, cfg, err := getAuditingConfig(ctx, c, namespace)
	if err != nil {
		return err
	}

	url := fmt.Sprintf(persephoneBackendURL, ReceiverServiceName, namespace, receiverPort)
	cfg.Backends = removeBackendByURL(cfg.Backends, url)

	return patchAuditingConfig(ctx, c, ext, cfg)
}

func getAuditingConfig(ctx context.Context, c client.Client, namespace string) (*extensionsv1alpha1.Extension, *auditConfiguration, error) {
	ext := &extensionsv1alpha1.Extension{}
	if err := c.Get(ctx, client.ObjectKey{Name: auditingExtensionName, Namespace: namespace}, ext); err != nil {
		return nil, nil, fmt.Errorf("get auditing Extension: %w", err)
	}

	cfg := &auditConfiguration{
		APIVersion: "auditing.extensions.gardener.cloud/v1alpha1",
		Kind:       "AuditConfiguration",
	}
	if ext.Spec.ProviderConfig != nil && len(ext.Spec.ProviderConfig.Raw) > 0 {
		if err := json.Unmarshal(ext.Spec.ProviderConfig.Raw, cfg); err != nil {
			return nil, nil, fmt.Errorf("decode auditing providerConfig: %w", err)
		}
	}

	return ext, cfg, nil
}

func patchAuditingConfig(ctx context.Context, c client.Client, ext *extensionsv1alpha1.Extension, cfg *auditConfiguration) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode auditing providerConfig: %w", err)
	}

	patch := client.MergeFrom(ext.DeepCopy())
	ext.Spec.ProviderConfig = &runtime.RawExtension{Raw: raw}
	if err := c.Patch(ctx, ext, patch); err != nil {
		return fmt.Errorf("patch auditing Extension: %w", err)
	}
	return nil
}

func removeBackendByURL(backends []auditBackend, url string) []auditBackend {
	result := backends[:0]
	for _, b := range backends {
		if b.HTTP == nil || b.HTTP.URL != url {
			result = append(result, b)
		}
	}
	return result
}
