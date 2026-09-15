// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package internalsecret

import (
	"github.com/prometheus/client_golang/prometheus"
)

// metrics holds all Prometheus metrics for the internalsecret controller.
type metrics struct {
	credentialRotations *prometheus.CounterVec
	credentialCleanups  *prometheus.CounterVec
}

// newMetrics creates and registers all metrics.
func newMetrics(registry prometheus.Registerer) *metrics {
	m := &metrics{
		credentialRotations: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "persephone_credential_rotations_total",
				Help: "Total number of credential rotations",
			},
			[]string{"region", "outcome"},
		),
		credentialCleanups: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "persephone_credential_cleanups_total",
				Help: "Total number of credential cleanups",
			},
			[]string{"region", "outcome"},
		),
	}

	registry.MustRegister(
		m.credentialRotations,
		m.credentialCleanups,
	)

	return m
}

// recordCredentialRotation records a credential rotation attempt.
func (m *metrics) recordCredentialRotation(region, outcome string) {
	m.credentialRotations.WithLabelValues(region, outcome).Inc()
}

// recordCredentialCleanup records a credential cleanup attempt.
func (m *metrics) recordCredentialCleanup(region, outcome string) {
	m.credentialCleanups.WithLabelValues(region, outcome).Inc()
}
