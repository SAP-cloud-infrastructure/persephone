// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	extensionssecretsmanager "github.com/gardener/gardener/extensions/pkg/util/secret/manager"
	secretsutils "github.com/gardener/gardener/pkg/utils/secrets"
	secretsmanager "github.com/gardener/gardener/pkg/utils/secrets/manager"
)

const (
	ManagerIdentity = "extension-persephone-auditlog"

	CASecretName                 = "ca-persephone-auditlog"
	ReceiverTLSSecretName        = "persephone-auditlog-receiver-tls"
	ForwarderClientTLSSecretName = "persephone-auditlog-forwarder-client-tls"
)

// SecretConfigs returns the SecretsManager configs for the auditlog extension TLS material.
func SecretConfigs(namespace string) []extensionssecretsmanager.SecretConfigWithOptions {
	return []extensionssecretsmanager.SecretConfigWithOptions{
		{
			Config: &secretsutils.CertificateSecretConfig{
				Name:       CASecretName,
				CommonName: CASecretName,
				CertType:   secretsutils.CACert,
			},
			Options: []secretsmanager.GenerateOption{secretsmanager.Persist()},
		},
		{
			Config: &secretsutils.CertificateSecretConfig{
				Name:       ReceiverTLSSecretName,
				CommonName: "persephone-auditlog-receiver",
				DNSNames: []string{
					ReceiverServiceName + "." + namespace + ".svc",
					ReceiverServiceName + "." + namespace + ".svc.cluster.local",
					ReceiverServiceName,
				},
				CertType: secretsutils.ServerCert,
			},
			Options: []secretsmanager.GenerateOption{secretsmanager.SignedByCA(CASecretName)},
		},
		{
			Config: &secretsutils.CertificateSecretConfig{
				Name:       ForwarderClientTLSSecretName,
				CommonName: "auditlog-forwarder",
				CertType:   secretsutils.ClientCert,
			},
			Options: []secretsmanager.GenerateOption{
				secretsmanager.SignedByCA(CASecretName, secretsmanager.UseCurrentCA),
			},
		},
	}
}
