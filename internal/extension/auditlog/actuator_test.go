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
	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	utilruntime.Must(gardenercorev1beta1.AddToScheme(s))
	return s
}()

var _ = Describe("Actuator.Delete", func() {
	It("removes auditing backend on delete", func() {
		ctx := context.Background()
		ns := "shoot--proj--shoot"

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

		ex := &extensionsv1alpha1.Extension{
			ObjectMeta: metav1.ObjectMeta{Name: "persephone-auditlog", Namespace: ns},
		}
		auditingExt := &extensionsv1alpha1.Extension{
			ObjectMeta: metav1.ObjectMeta{Name: "auditing", Namespace: ns},
			Spec: extensionsv1alpha1.ExtensionSpec{
				DefaultSpec: extensionsv1alpha1.DefaultSpec{
					ProviderConfig: &runtime.RawExtension{Raw: rawCfg},
				},
			},
		}

		c := fake.NewClientBuilder().
			WithScheme(actuatorTestScheme).
			WithObjects(ex, auditingExt).
			Build()

		a := &auditlog.Actuator{Client: c, ReceiverImage: "receiver:test"}
		Expect(a.Delete(ctx, logr.Discard(), ex)).To(Succeed())

		updated := &extensionsv1alpha1.Extension{}
		Expect(c.Get(ctx, client.ObjectKey{Name: "auditing", Namespace: ns}, updated)).To(Succeed())

		var cfg map[string]any
		Expect(json.Unmarshal(updated.Spec.ProviderConfig.Raw, &cfg)).To(Succeed())
		Expect(cfg["backends"].([]any)).To(BeEmpty())
	})
})

var _ = Describe("Actuator.Migrate", func() {
	It("returns nil without error", func() {
		a := &auditlog.Actuator{}
		Expect(a.Migrate(context.Background(), logr.Discard(), &extensionsv1alpha1.Extension{})).To(Succeed())
	})
})
