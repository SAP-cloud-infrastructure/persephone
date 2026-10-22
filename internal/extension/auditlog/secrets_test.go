// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package auditlog_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	secretsutils "github.com/gardener/gardener/pkg/utils/secrets"

	"github.com/sap-cloud-infrastructure/persephone/internal/extension/auditlog"
)

var _ = Describe("SecretConfigs", func() {
	It("returns three configs with correct names", func() {
		configs := auditlog.SecretConfigs("shoot--proj--name")
		Expect(configs).To(HaveLen(3))

		names := make([]string, 0, 3)
		for _, c := range configs {
			names = append(names, c.Config.GetName())
		}
		Expect(names).To(ConsistOf(
			auditlog.CASecretName,
			auditlog.ReceiverTLSSecretName,
			auditlog.ForwarderClientTLSSecretName,
		))
	})

	It("CA config is CACert type", func() {
		configs := auditlog.SecretConfigs("shoot--proj--name")
		for _, c := range configs {
			if c.Config.GetName() == auditlog.CASecretName {
				certCfg, ok := c.Config.(*secretsutils.CertificateSecretConfig)
				Expect(ok).To(BeTrue())
				Expect(certCfg.CertType).To(Equal(secretsutils.CACert))
			}
		}
	})

	It("receiver TLS config has correct DNS names for given namespace", func() {
		configs := auditlog.SecretConfigs("shoot--proj--name")
		for _, c := range configs {
			if c.Config.GetName() == auditlog.ReceiverTLSSecretName {
				certCfg, ok := c.Config.(*secretsutils.CertificateSecretConfig)
				Expect(ok).To(BeTrue())
				Expect(certCfg.DNSNames).To(ContainElement("persephone-auditlog-receiver.shoot--proj--name.svc"))
			}
		}
	})
})
