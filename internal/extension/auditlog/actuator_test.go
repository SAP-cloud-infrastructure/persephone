// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package auditlog_test

import (
	"context"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/sap-cloud-infrastructure/persephone/internal/extension/auditlog"
)

var actuatorTestScheme = func() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(extensionsv1alpha1.AddToScheme(s))
	return s
}()

var _ = Describe("Actuator.Delete", func() {
	It("removes receiver Service and webhook kubeconfig secret on delete", func() {
		ctx := context.Background()
		ns := "shoot--proj--shoot"

		ex := &extensionsv1alpha1.Extension{}
		ex.Name = "persephone-auditlog"
		ex.Namespace = ns
		webhookSecret := &corev1.Secret{}
		webhookSecret.Name = auditlog.WebhookKubeconfigSecretName
		webhookSecret.Namespace = ns
		svc := &corev1.Service{}
		svc.Name = auditlog.ReceiverServiceName
		svc.Namespace = ns

		c := fake.NewClientBuilder().
			WithScheme(actuatorTestScheme).
			WithObjects(ex, webhookSecret, svc).
			Build()

		a := &auditlog.Actuator{Client: c, ReceiverImage: "receiver:test"}
		Expect(a.Delete(ctx, logr.Discard(), ex)).To(Succeed())

		// Webhook kubeconfig secret should be gone.
		err := c.Get(ctx, client.ObjectKey{Name: auditlog.WebhookKubeconfigSecretName, Namespace: ns}, &corev1.Secret{})
		Expect(err).To(HaveOccurred())

		// Service should be gone.
		err = c.Get(ctx, client.ObjectKey{Name: auditlog.ReceiverServiceName, Namespace: ns}, &corev1.Service{})
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("Actuator.Migrate", func() {
	It("returns nil without error", func() {
		a := &auditlog.Actuator{}
		Expect(a.Migrate(context.Background(), logr.Discard(), &extensionsv1alpha1.Extension{})).To(Succeed())
	})
})
