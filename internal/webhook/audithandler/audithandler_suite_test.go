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
