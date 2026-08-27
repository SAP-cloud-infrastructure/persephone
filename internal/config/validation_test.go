// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/sap-cloud-infrastructure/persephone/internal/config"
)

func TestConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Config Suite")
}

var _ = Describe("Validate", func() {
	// validRegionalConfig provides a valid base config for tests.
	validRegionalConfig := func() RegionalConfig {
		return RegionalConfig{
			Enabled:                     true,
			IdentityEndpoint:            "https://identity.example.com/",
			ApplicationCredentialID:     "test-id",
			ApplicationCredentialSecret: "test-secret",
		}
	}

	// wrapInPersephoneConfig wraps a regional config as the default region.
	wrapInPersephoneConfig := func(rc RegionalConfig) PersephoneConfig {
		return PersephoneConfig{
			DefaultRegion:     "test-region",
			Regions:           map[string]RegionalConfig{"test-region": rc},
			DefaultShootQuota: 10,
		}
	}

	DescribeTable("should validate regional config",
		func(modify func(RegionalConfig) RegionalConfig, shouldErr bool, errSubstring string) {
			rc := modify(validRegionalConfig())
			err := Validate(wrapInPersephoneConfig(rc))
			if shouldErr {
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(errSubstring))
			} else {
				Expect(err).NotTo(HaveOccurred())
			}
		},
		Entry("valid config with audit URL",
			func(rc RegionalConfig) RegionalConfig {
				rc.AuditTransportURL = "amqp://localhost:5672/vhost"
				return rc
			},
			false,
			"",
		),
		Entry("invalid audit URL scheme",
			func(rc RegionalConfig) RegionalConfig {
				rc.AuditTransportURL = "http://localhost:5672"
				return rc
			},
			true,
			"auditTransportURL must use amqp:// or amqps:// scheme",
		),
		Entry("empty audit URL for enabled region",
			func(rc RegionalConfig) RegionalConfig {
				rc.AuditTransportURL = ""
				return rc
			},
			false,
			"",
		),
		Entry("missing identity endpoint",
			func(rc RegionalConfig) RegionalConfig {
				rc.IdentityEndpoint = ""
				return rc
			},
			true,
			"identity endpoint",
		),
		Entry("missing application credentials",
			func(rc RegionalConfig) RegionalConfig {
				rc.ApplicationCredentialID = ""
				rc.ApplicationCredentialSecret = ""
				return rc
			},
			true,
			"application credentials",
		),
	)

	DescribeTable("should validate defaultShootQuota",
		func(quota uint64, shouldErr bool) {
			cfg := wrapInPersephoneConfig(validRegionalConfig())
			cfg.DefaultShootQuota = quota
			err := Validate(cfg)
			if shouldErr {
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("defaultShootQuota"))
			} else {
				Expect(err).NotTo(HaveOccurred())
			}
		},
		Entry("valid quota", uint64(10), false),
		Entry("zero quota", uint64(0), true),
	)
})
