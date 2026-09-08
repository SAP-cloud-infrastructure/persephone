package audithandler

import "github.com/sapcc/go-bits/audittools"

// SetTestAuditors is a test helper to inject mock auditors for integration testing.
func (s *RegionAuditorSet) SetTestAuditors(auditors map[string]audittools.Auditor) {
	s.auditors = auditors
}
