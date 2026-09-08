package openstack_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestOpenStack(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Internal OpenStack Suite")
}
