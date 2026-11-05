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
		d := auditlog.ReceiverDeployment("shoot--p--n", "img:sha", "my-bucket", "logs/", "", "persephone-auditlog-receiver-tls-abc123", nil)
		Expect(d.Namespace).To(Equal("shoot--p--n"))
		Expect(d.Name).To(Equal(auditlog.ReceiverServiceName))
	})

	It("passes bucket and prefix as flags", func() {
		d := auditlog.ReceiverDeployment("ns", "img:sha", "bucket", "pfx/", "", "tls-secret-versioned", nil)
		args := d.Spec.Template.Spec.Containers[0].Args
		Expect(args).To(ContainElement("--bucket=bucket"))
		Expect(args).To(ContainElement("--prefix=pfx/"))
	})

	It("passes s3Endpoint flag when provided", func() {
		d := auditlog.ReceiverDeployment("ns", "img:sha", "b", "p", "https://rgw.st1.eu-de-1.cloud.sap", "tls", nil)
		args := d.Spec.Template.Spec.Containers[0].Args
		Expect(args).To(ContainElement("--s3-endpoint=https://rgw.st1.eu-de-1.cloud.sap"))
	})

	It("omits s3Endpoint flag when empty", func() {
		d := auditlog.ReceiverDeployment("ns", "img:sha", "b", "p", "", "tls", nil)
		args := d.Spec.Template.Spec.Containers[0].Args
		Expect(args).NotTo(ContainElement(ContainSubstring("--s3-endpoint")))
	})

	It("mounts receiver-tls secret with versioned name", func() {
		d := auditlog.ReceiverDeployment("ns", "img:sha", "b", "p", "", "persephone-auditlog-receiver-tls-abc", nil)
		vols := d.Spec.Template.Spec.Volumes
		Expect(vols).To(ContainElement(HaveField("Name", "receiver-tls")))
		tlsVol := vols[0]
		Expect(tlsVol.Secret.SecretName).To(Equal("persephone-auditlog-receiver-tls-abc"))
		mounts := d.Spec.Template.Spec.Containers[0].VolumeMounts
		Expect(mounts).To(ContainElement(HaveField("Name", "receiver-tls")))
	})

	It("maps cloudprovider secret keys to OS_ env vars", func() {
		d := auditlog.ReceiverDeployment("ns", "img:sha", "b", "p", "", "tls-secret", nil)
		env := d.Spec.Template.Spec.Containers[0].Env
		Expect(env).To(ContainElement(HaveField("Name", "OS_AUTH_URL")))
		Expect(env).To(ContainElement(HaveField("Name", "OS_APPLICATION_CREDENTIAL_ID")))
		Expect(env).To(ContainElement(HaveField("Name", "OS_APPLICATION_CREDENTIAL_SECRET")))
	})

	It("adds object lock flags when ObjectLockConfig is provided", func() {
		ol := &auditlog.ObjectLockConfig{Enabled: true, RetentionDays: 90, Mode: "COMPLIANCE"}
		d := auditlog.ReceiverDeployment("ns", "img:sha", "b", "p", "", "tls-secret", ol)
		args := d.Spec.Template.Spec.Containers[0].Args
		Expect(args).To(ContainElement("--object-lock"))
		Expect(args).To(ContainElement("--object-lock-retention-days=90"))
		Expect(args).To(ContainElement("--object-lock-mode=COMPLIANCE"))
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
