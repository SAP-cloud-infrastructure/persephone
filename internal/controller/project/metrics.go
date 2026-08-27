// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package project

import (
	"github.com/prometheus/client_golang/prometheus"
)

// metrics holds all Prometheus metrics for the project controller.
type metrics struct {
	resourceCreations *prometheus.CounterVec
}

// newMetrics creates and registers all metrics.
func newMetrics(registry prometheus.Registerer) *metrics {
	m := &metrics{
		resourceCreations: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "persephone_project_resource_creations_total",
				Help: "Total number of Gardener project resource creations",
			},
			[]string{"region", "outcome"},
		),
	}

	registry.MustRegister(
		m.resourceCreations,
	)

	return m
}

// recordResourceCreation records a resource creation attempt.
func (m *metrics) recordResourceCreation(region, outcome string) {
	m.resourceCreations.WithLabelValues(region, outcome).Inc()
}
