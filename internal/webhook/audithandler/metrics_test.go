// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audithandler

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

var _ = Describe("Metrics", func() {
	var (
		registry *prometheus.Registry
		m        *metrics
	)

	BeforeEach(func() {
		registry = prometheus.NewRegistry()
		m = newMetrics(registry)
	})

	Describe("recordEvent", func() {
		It("should increment events total counter", func() {
			m.recordEvent("create", "qa-de-1", "success")

			count := testutil.ToFloat64(m.eventsTotal.WithLabelValues("create", "qa-de-1", "success"))
			Expect(count).To(Equal(1.0))
		})

		It("should track multiple regions separately", func() {
			m.recordEvent("create", "qa-de-1", "success")
			m.recordEvent("create", "eu-de-1", "success")

			countQA := testutil.ToFloat64(m.eventsTotal.WithLabelValues("create", "qa-de-1", "success"))
			countEU := testutil.ToFloat64(m.eventsTotal.WithLabelValues("create", "eu-de-1", "success"))

			Expect(countQA).To(Equal(1.0))
			Expect(countEU).To(Equal(1.0))
		})
	})

	Describe("recordFiltered", func() {
		It("should increment filtered counter", func() {
			m.recordFiltered("not_shoot_resource")

			count := testutil.ToFloat64(m.eventsFiltered.WithLabelValues("not_shoot_resource"))
			Expect(count).To(Equal(1.0))
		})
	})

	Describe("recordPublishFailure", func() {
		It("should increment publish failure counter", func() {
			m.recordPublishFailure("qa-de-1", "connection_error")

			count := testutil.ToFloat64(m.publishFailures.WithLabelValues("qa-de-1", "connection_error"))
			Expect(count).To(Equal(1.0))
		})
	})
})
