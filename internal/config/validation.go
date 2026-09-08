// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"fmt"
	"net/url"
)

// Validate validates the given webhook config.
func Validate(webhookConfig PersephoneConfig) error {
	if webhookConfig.DefaultRegion == "" {
		return errors.New("default region needs to be set in config")
	}

	if err := validateRegionalConfig(webhookConfig.Regions[webhookConfig.DefaultRegion]); err != nil {
		return fmt.Errorf("failed validating regional config for default region %q: %w", webhookConfig.DefaultRegion, err)
	}

	if webhookConfig.DefaultShootQuota == 0 {
		return errors.New("defaultShootQuota must be greater than 0")
	}

	return nil
}
func validateRegionalConfig(regionalConfig RegionalConfig) error {
	if regionalConfig.IdentityEndpoint == "" {
		return errors.New("default identity endpoint needs to be set in config")
	}

	if regionalConfig.ApplicationCredentialID == "" || regionalConfig.ApplicationCredentialSecret == "" {
		return errors.New("application credentials need to be set in config")
	}

	if regionalConfig.AuditTransportURL != "" {
		u, err := url.Parse(regionalConfig.AuditTransportURL)
		if err != nil {
			return fmt.Errorf("auditTransportURL is not a valid URL: %w", err)
		}
		if u.Scheme != "amqp" && u.Scheme != "amqps" {
			return fmt.Errorf("auditTransportURL must use amqp:// or amqps:// scheme, got: %s", u.Scheme)
		}
	}

	return nil
}
