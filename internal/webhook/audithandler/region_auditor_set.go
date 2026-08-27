// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audithandler

import (
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/audittools"

	"github.com/sap-cloud-infrastructure/persephone/internal/config"
)

// RegionAuditorSet manages per-region Auditor instances.
type RegionAuditorSet struct {
	auditors map[string]audittools.Auditor
}

// NewRegionAuditorSet creates a new RegionAuditorSet with one Auditor per enabled region.
func NewRegionAuditorSet(ctx context.Context, regions map[string]config.RegionalConfig, observer audittools.Observer, queueName string, registry prometheus.Registerer) (*RegionAuditorSet, error) {
	set := &RegionAuditorSet{
		auditors: make(map[string]audittools.Auditor),
	}

	for region, regionCfg := range regions {
		if !regionCfg.Enabled {
			continue
		}

		var auditor audittools.Auditor
		if regionCfg.AuditTransportURL == "" {
			// No transport URL configured, use null auditor (no-op)
			auditor = audittools.NewNullAuditor()
		} else {
			// Each auditor gets its own registry to avoid duplicate metric registration
			// (go-bits' NewAuditor unconditionally calls MustRegister with fixed names).
			// We wrap it with a region label and register it on the parent registry.
			auditorRegistry := prometheus.NewRegistry()

			var err error
			auditor, err = audittools.NewAuditor(ctx, audittools.AuditorOpts{
				Observer:      observer,
				ConnectionURL: regionCfg.AuditTransportURL,
				QueueName:     queueName,
				Registry:      auditorRegistry,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to create auditor for region %s: %w", region, err)
			}

			registry.MustRegister(prometheus.WrapCollectorWith(
				prometheus.Labels{"region": region},
				auditorRegistry,
			))
		}

		set.auditors[region] = auditor
	}

	return set, nil
}

// GetAuditor returns the Auditor for the given region.
func (s *RegionAuditorSet) GetAuditor(region string) (audittools.Auditor, error) {
	auditor, ok := s.auditors[region]
	if !ok {
		return nil, fmt.Errorf("no auditor configured for region: %s", region)
	}
	return auditor, nil
}
