// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package auditlog_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	"github.com/sap-cloud-infrastructure/persephone/internal/extension/auditlog"
)

var _ = Describe("ReceiverDeployment", func() {
	It("creates a Deployment in the given namespace", func() {
		d := auditlog.ReceiverDeployment("shoot--p--n", "img:sha", "my-bucket", "logs/")
		Expect(d.Namespace).To(Equal("shoot--p--n"))
		Expect(d.Name).To(Equal(auditlog.ReceiverServiceName))
	})

	It("passes container and prefix as flags", func() {
		d := auditlog.ReceiverDeployment("ns", "img:sha", "bucket", "pfx/")
		args := d.Spec.Template.Spec.Containers[0].Args
		Expect(args).To(ContainElement("--container=bucket"))
		Expect(args).To(ContainElement("--prefix=pfx/"))
	})

	It("mounts receiver-tls secret", func() {
		d := auditlog.ReceiverDeployment("ns", "img:sha", "b", "p")
		vols := d.Spec.Template.Spec.Volumes
		Expect(vols).To(ContainElement(HaveField("Name", "receiver-tls")))
		mounts := d.Spec.Template.Spec.Containers[0].VolumeMounts
		Expect(mounts).To(ContainElement(HaveField("Name", "receiver-tls")))
	})

	It("loads cloud-provider secret as env", func() {
		d := auditlog.ReceiverDeployment("ns", "img:sha", "b", "p")
		envFrom := d.Spec.Template.Spec.Containers[0].EnvFrom
		Expect(envFrom).To(HaveLen(1))
		Expect(envFrom[0].SecretRef.Name).To(Equal("cloudprovider"))
	})
})

var _ = Describe("ReceiverService", func() {
	It("creates a Service in the given namespace", func() {
		svc := auditlog.ReceiverService("shoot--p--n")
		Expect(svc.Namespace).To(Equal("shoot--p--n"))
		Expect(svc.Name).To(Equal(auditlog.ReceiverServiceName))
	})

	It("exposes port 443 mapping to 8443", func() {
		svc := auditlog.ReceiverService("ns")
		Expect(svc.Spec.Ports).To(HaveLen(1))
		port := svc.Spec.Ports[0]
		Expect(port.Port).To(Equal(int32(443)))
		Expect(port.Protocol).To(Equal(corev1.ProtocolTCP))
	})
})
