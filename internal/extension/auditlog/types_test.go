// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package auditlog_test

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/sap-cloud-infrastructure/persephone/internal/extension/auditlog"
)

var _ = Describe("DecodeAuditLogConfig", func() {
	It("decodes a valid providerConfig", func() {
		raw, err := json.Marshal(map[string]any{
			"apiVersion": "extension.persephone.cloud/v1alpha1",
			"kind":       "AuditLogConfig",
			"objectStore": map[string]any{
				"container": "shoot-audit-logs",
				"prefix":    "shoots/my-shoot/",
			},
		})
		Expect(err).NotTo(HaveOccurred())

		cfg, err := auditlog.DecodeAuditLogConfig(&runtime.RawExtension{Raw: raw})
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.ObjectStore.Container).To(Equal("shoot-audit-logs"))
		Expect(cfg.ObjectStore.Prefix).To(Equal("shoots/my-shoot/"))
	})

	It("returns error for nil raw extension", func() {
		_, err := auditlog.DecodeAuditLogConfig(nil)
		Expect(err).To(HaveOccurred())
	})

	It("returns error for invalid JSON", func() {
		_, err := auditlog.DecodeAuditLogConfig(&runtime.RawExtension{Raw: []byte(`not json`)})
		Expect(err).To(HaveOccurred())
	})
})
