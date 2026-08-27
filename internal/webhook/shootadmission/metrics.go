// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package shootadmission

import (
	"github.com/prometheus/client_golang/prometheus"
)

// metrics holds all Prometheus metrics for the shoot admission webhook.
type metrics struct {
	shootMutations         *prometheus.CounterVec
	credentialIssuances    *prometheus.CounterVec
	floatingPoolDetections *prometheus.CounterVec
}

// newMetrics creates and registers all metrics.
func newMetrics(registry prometheus.Registerer) *metrics {
	m := &metrics{
		shootMutations: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "persephone_shoot_mutations_total",
				Help: "Total number of shoot mutations",
			},
			[]string{"region", "outcome"},
		),
		credentialIssuances: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "persephone_shoot_credential_issuances_total",
				Help: "Total number of application credential issuance attempts",
			},
			[]string{"region", "outcome"},
		),
		floatingPoolDetections: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "persephone_shoot_floating_pool_detections_total",
				Help: "Total number of floating pool detection attempts",
			},
			[]string{"region", "outcome"},
		),
	}

	registry.MustRegister(
		m.shootMutations,
		m.credentialIssuances,
		m.floatingPoolDetections,
	)

	return m
}

// recordShootMutation records a shoot mutation.
func (m *metrics) recordShootMutation(region, outcome string) {
	m.shootMutations.WithLabelValues(region, outcome).Inc()
}

// recordCredentialIssuance records a credential issuance attempt.
func (m *metrics) recordCredentialIssuance(region, outcome string) {
	m.credentialIssuances.WithLabelValues(region, outcome).Inc()
}

// recordFloatingPoolDetection records a floating pool detection attempt.
func (m *metrics) recordFloatingPoolDetection(region, outcome string) {
	m.floatingPoolDetections.WithLabelValues(region, outcome).Inc()
}
