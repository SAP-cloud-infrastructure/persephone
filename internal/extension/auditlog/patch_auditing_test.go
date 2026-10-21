// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package auditlog_test

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/sap-cloud-infrastructure/persephone/internal/extension/auditlog"
)

var auditingTestScheme = func() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(extensionsv1alpha1.AddToScheme(s))
	return s
}()

var _ = Describe("EnsureAuditingBackend", func() {
	var (
		ctx context.Context
		c   client.Client
		ns  = "shoot--proj--name"
	)

	BeforeEach(func() {
		ctx = context.Background()
		auditingExt := &extensionsv1alpha1.Extension{
			ObjectMeta: metav1.ObjectMeta{Name: "auditing", Namespace: ns},
			Spec:       extensionsv1alpha1.ExtensionSpec{},
		}
		c = fake.NewClientBuilder().
			WithScheme(auditingTestScheme).
			WithObjects(auditingExt).
			Build()
	})

	It("adds the persephone backend to an Extension with empty providerConfig", func() {
		Expect(auditlog.EnsureAuditingBackend(ctx, c, ns)).To(Succeed())

		ext := &extensionsv1alpha1.Extension{}
		Expect(c.Get(ctx, client.ObjectKey{Name: "auditing", Namespace: ns}, ext)).To(Succeed())
		Expect(ext.Spec.ProviderConfig).NotTo(BeNil())

		var cfg map[string]any
		Expect(json.Unmarshal(ext.Spec.ProviderConfig.Raw, &cfg)).To(Succeed())

		backends, ok := cfg["backends"].([]any)
		Expect(ok).To(BeTrue())
		Expect(backends).To(HaveLen(1))

		backend := backends[0].(map[string]any)
		httpCfg := backend["http"].(map[string]any)
		Expect(httpCfg["url"]).To(ContainSubstring("persephone-auditlog-receiver." + ns + ".svc"))
		Expect(httpCfg["tls"].(map[string]any)["secretName"]).To(Equal(auditlog.ForwarderClientTLSSecretName))
	})

	It("is idempotent — does not add a second backend on re-reconcile", func() {
		Expect(auditlog.EnsureAuditingBackend(ctx, c, ns)).To(Succeed())
		Expect(auditlog.EnsureAuditingBackend(ctx, c, ns)).To(Succeed())

		ext := &extensionsv1alpha1.Extension{}
		Expect(c.Get(ctx, client.ObjectKey{Name: "auditing", Namespace: ns}, ext)).To(Succeed())

		var cfg map[string]any
		Expect(json.Unmarshal(ext.Spec.ProviderConfig.Raw, &cfg)).To(Succeed())
		Expect(cfg["backends"].([]any)).To(HaveLen(1))
	})
})

var _ = Describe("RemoveAuditingBackend", func() {
	It("removes the persephone backend entry", func() {
		ctx := context.Background()
		ns := "shoot--proj--name"

		rawCfg, _ := json.Marshal(map[string]any{
			"apiVersion": "auditing.extensions.gardener.cloud/v1alpha1",
			"kind":       "AuditConfiguration",
			"backends": []any{
				map[string]any{
					"deliveryMode": "BestEffort",
					"http": map[string]any{
						"url": "https://persephone-auditlog-receiver." + ns + ".svc:8443/audit",
						"tls": map[string]any{"secretName": auditlog.ForwarderClientTLSSecretName},
					},
				},
			},
		})

		auditingExt := &extensionsv1alpha1.Extension{
			ObjectMeta: metav1.ObjectMeta{Name: "auditing", Namespace: ns},
			Spec: extensionsv1alpha1.ExtensionSpec{
				DefaultSpec: extensionsv1alpha1.DefaultSpec{
					ProviderConfig: &runtime.RawExtension{Raw: rawCfg},
				},
			},
		}
		c := fake.NewClientBuilder().
			WithScheme(auditingTestScheme).
			WithObjects(auditingExt).
			Build()

		Expect(auditlog.RemoveAuditingBackend(ctx, c, ns)).To(Succeed())

		ext := &extensionsv1alpha1.Extension{}
		Expect(c.Get(ctx, client.ObjectKey{Name: "auditing", Namespace: ns}, ext)).To(Succeed())

		var cfg map[string]any
		Expect(json.Unmarshal(ext.Spec.ProviderConfig.Raw, &cfg)).To(Succeed())
		Expect(cfg["backends"].([]any)).To(BeEmpty())
	})
})
