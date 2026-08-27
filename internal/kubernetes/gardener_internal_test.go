// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package kubernetes

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

var _ = Describe("sanitizeLabelValue", func() {
	DescribeTable("should produce valid Kubernetes label values",
		func(input, expected string) {
			result := sanitizeLabelValue(input)
			Expect(result).To(Equal(expected))
			Expect(metav1validation.ValidateLabels(map[string]string{"key": result}, field.NewPath("labels"))).To(BeEmpty())
		},
		Entry("empty string", "", ""),
		Entry("already valid", "my-project_1.0", "my-project_1.0"),
		Entry("spaces replaced with underscores", "SAP Analytics Sandbox Build System", "SAP_Analytics_Sandbox_Build_System"),
		Entry("leading special chars stripped", " leading", "leading"),
		Entry("trailing special chars stripped", "trailing ", "trailing"),
		Entry("leading and trailing special chars stripped", " foo bar ", "foo_bar"),
		Entry("only special chars returns empty", "   ", ""),
		Entry("slash replaced", "foo/bar", "foo_bar"),
		Entry("truncated to 63 chars",
			"abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz",
			"abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijk"),
		Entry("truncation does not leave trailing non-alphanumeric",
			"abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghij___",
			"abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghij"),
	)
})
