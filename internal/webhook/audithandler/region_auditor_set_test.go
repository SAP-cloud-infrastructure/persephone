// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audithandler

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/audittools"

	"github.com/sap-cloud-infrastructure/persephone/internal/config"
)

var _ = Describe("RegionAuditorSet", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())
	})

	AfterEach(func() {
		cancel()
	})

	Describe("NewRegionAuditorSet", func() {
		It("should create auditors for enabled regions with audit URLs", func() {
			regions := map[string]config.RegionalConfig{
				"qa-de-1": {
					Enabled:           true,
					AuditTransportURL: "", // Empty URL = null auditor
				},
				"eu-de-1": {
					Enabled:           true,
					AuditTransportURL: "", // Empty URL = null auditor
				},
			}

			observer := audittools.Observer{
				TypeURI: "service/persephone-webhook",
				Name:    "persephone-webhook",
				ID:      "test",
			}

			set, err := NewRegionAuditorSet(ctx, regions, observer, "notifications.info", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(set).NotTo(BeNil())
			Expect(set.auditors).To(HaveLen(2))

			auditor, err := set.GetAuditor("qa-de-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(auditor).NotTo(BeNil())

			auditor, err = set.GetAuditor("eu-de-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(auditor).NotTo(BeNil())
		})

		It("should register per-region metrics without panicking for multiple regions", func() {
			regions := map[string]config.RegionalConfig{
				"eu-de-1": {
					Enabled:           true,
					AuditTransportURL: "amqp://localhost:5672",
				},
				"eu-nl-1": {
					Enabled:           true,
					AuditTransportURL: "amqp://localhost:5672",
				},
			}

			observer := audittools.Observer{
				TypeURI: "service/persephone-webhook",
				Name:    "persephone-webhook",
				ID:      "test-host",
			}

			registry := prometheus.NewRegistry()

			set, err := NewRegionAuditorSet(ctx, regions, observer, "notifications.info", registry)
			Expect(err).NotTo(HaveOccurred())
			Expect(set.auditors).To(HaveLen(2))

			// Verify both per-region metric sets are gatherable and initialized to 0.
			families, err := registry.Gather()
			Expect(err).NotTo(HaveOccurred())

			metricsByName := make(map[string][]string) // metric name -> list of region label values
			for _, family := range families {
				for _, metric := range family.GetMetric() {
					for _, label := range metric.GetLabel() {
						if label.GetName() == "region" {
							metricsByName[family.GetName()] = append(metricsByName[family.GetName()], label.GetValue())
						}
					}
				}
			}

			Expect(metricsByName).To(HaveKey("audittools_successful_submissions"))
			Expect(metricsByName).To(HaveKey("audittools_failed_submissions"))
			Expect(metricsByName["audittools_successful_submissions"]).To(ConsistOf("eu-de-1", "eu-nl-1"))
			Expect(metricsByName["audittools_failed_submissions"]).To(ConsistOf("eu-de-1", "eu-nl-1"))
		})

		It("should return error for unknown region", func() {
			regions := map[string]config.RegionalConfig{
				"qa-de-1": {
					Enabled:           true,
					AuditTransportURL: "",
				},
			}

			observer := audittools.Observer{
				TypeURI: "service/persephone-webhook",
				Name:    "persephone-webhook",
				ID:      "test",
			}

			set, err := NewRegionAuditorSet(ctx, regions, observer, "notifications.info", nil)
			Expect(err).NotTo(HaveOccurred())

			_, err = set.GetAuditor("unknown-region")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no auditor configured for region"))
		})

		It("should skip disabled regions", func() {
			regions := map[string]config.RegionalConfig{
				"qa-de-1": {
					Enabled:           true,
					AuditTransportURL: "",
				},
				"disabled": {
					Enabled:           false,
					AuditTransportURL: "amqp://localhost:5672",
				},
			}

			observer := audittools.Observer{
				TypeURI: "service/persephone-webhook",
				Name:    "persephone-webhook",
				ID:      "test",
			}

			set, err := NewRegionAuditorSet(ctx, regions, observer, "notifications.info", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(set.auditors).To(HaveLen(1))
		})
	})
})
