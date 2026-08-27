// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package token

import (
	"github.com/prometheus/client_golang/prometheus"
)

// metrics holds all Prometheus metrics for the authentication webhook.
type metrics struct {
	authenticationAttempts  *prometheus.CounterVec
	authenticationFailures  *prometheus.CounterVec
	keystoneAuthFailures    *prometheus.CounterVec
	resourceReconciliations *prometheus.CounterVec
}

// newMetrics creates and registers all metrics.
func newMetrics(registry prometheus.Registerer) *metrics {
	m := &metrics{
		authenticationAttempts: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "persephone_authentication_attempts_total",
				Help: "Total number of authentication attempts",
			},
			[]string{"region", "outcome"},
		),
		authenticationFailures: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "persephone_authentication_failures_total",
				Help: "Total number of authentication failures",
			},
			[]string{"region", "reason"},
		),
		keystoneAuthFailures: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "persephone_keystone_auth_failures_total",
				Help: "Total number of Keystone authentication failures",
			},
			[]string{"region", "error_type"},
		),
		resourceReconciliations: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "persephone_webhook_resource_reconciliations_total",
				Help: "Total number of Gardener resource reconciliation attempts during authentication",
			},
			[]string{"region", "outcome"},
		),
	}

	registry.MustRegister(
		m.authenticationAttempts,
		m.authenticationFailures,
		m.keystoneAuthFailures,
		m.resourceReconciliations,
	)

	return m
}

// recordAuthenticationAttempt records an authentication attempt.
func (m *metrics) recordAuthenticationAttempt(region, outcome string) {
	m.authenticationAttempts.WithLabelValues(region, outcome).Inc()
}

// recordAuthenticationFailure records an authentication failure.
func (m *metrics) recordAuthenticationFailure(region, reason string) {
	m.authenticationFailures.WithLabelValues(region, reason).Inc()
}

// recordKeystoneAuthFailure records a Keystone authentication failure.
func (m *metrics) recordKeystoneAuthFailure(region, errorType string) {
	m.keystoneAuthFailures.WithLabelValues(region, errorType).Inc()
}

// recordResourceReconciliation records a resource reconciliation attempt.
func (m *metrics) recordResourceReconciliation(region, outcome string) {
	m.resourceReconciliations.WithLabelValues(region, outcome).Inc()
}
