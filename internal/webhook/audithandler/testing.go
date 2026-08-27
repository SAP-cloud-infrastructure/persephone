// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audithandler

import "github.com/sapcc/go-bits/audittools"

// SetTestAuditors is a test helper to inject mock auditors for integration testing.
func (s *RegionAuditorSet) SetTestAuditors(auditors map[string]audittools.Auditor) {
	s.auditors = auditors
}
