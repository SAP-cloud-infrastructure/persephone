package audithandler

import (
	"github.com/prometheus/client_golang/prometheus"
)

// metrics holds all Prometheus metrics for the audit handler.
type metrics struct {
	eventsTotal        *prometheus.CounterVec
	eventsFiltered     *prometheus.CounterVec
	publishFailures    *prometheus.CounterVec
	processingDuration *prometheus.HistogramVec
}

// newMetrics creates and registers all metrics.
func newMetrics(registry prometheus.Registerer) *metrics {
	m := &metrics{
		eventsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "persephone_audit_events_total",
				Help: "Total number of audit events processed",
			},
			[]string{"verb", "region", "outcome"},
		),
		eventsFiltered: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "persephone_audit_events_filtered_total",
				Help: "Total number of audit events filtered out",
			},
			[]string{"reason"},
		),
		publishFailures: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "persephone_audit_publish_failures_total",
				Help: "Total number of audit event publish failures",
			},
			[]string{"region", "error_type"},
		),
		processingDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "persephone_audit_processing_duration_seconds",
				Help:    "Duration of audit event processing",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"region"},
		),
	}

	registry.MustRegister(
		m.eventsTotal,
		m.eventsFiltered,
		m.publishFailures,
		m.processingDuration,
	)

	return m
}

// recordEvent records a processed event.
func (m *metrics) recordEvent(verb, region, outcome string) {
	m.eventsTotal.WithLabelValues(verb, region, outcome).Inc()
}

// recordFiltered records a filtered event.
func (m *metrics) recordFiltered(reason string) {
	m.eventsFiltered.WithLabelValues(reason).Inc()
}

// recordPublishFailure records a publish failure.
func (m *metrics) recordPublishFailure(region, errorType string) {
	m.publishFailures.WithLabelValues(region, errorType).Inc()
}

// observeProcessingDuration records processing duration.
func (m *metrics) observeProcessingDuration(region string, duration float64) {
	m.processingDuration.WithLabelValues(region).Observe(duration)
}
