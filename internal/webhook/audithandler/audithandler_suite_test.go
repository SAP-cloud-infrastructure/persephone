// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package audithandler

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAuditHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AuditHandler Suite")
}
