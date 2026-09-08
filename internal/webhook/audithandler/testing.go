// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package audithandler

import "github.com/sapcc/go-bits/audittools"

// SetTestAuditors is a test helper to inject mock auditors for integration testing.
func (s *RegionAuditorSet) SetTestAuditors(auditors map[string]audittools.Auditor) {
	s.auditors = auditors
}
