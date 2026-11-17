// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package shoot

import (
	"context"

	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/clock"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/sap-cloud-infrastructure/persephone/internal/constants"
)

var _ = Describe("Reconciler delete()", func() {
	var (
		ctx        context.Context
		fakeClient client.Client
		rec        *Reconciler
		shoot      *gardenercorev1beta1.Shoot
		secret     *gardenercorev1beta1.InternalSecret
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeClient = fake.NewClientBuilder().WithScheme(kubernetes.GardenScheme).Build()
		rec = &Reconciler{
			Client: fakeClient,
			Clock:  clock.RealClock{},
		}

		now := metav1.Now()
		shoot = &gardenercorev1beta1.Shoot{
			Name:              "test-shoot",
			Namespace:         "garden-test",
			DeletionTimestamp: &now,
			// A finalizer is required so the object can carry a DeletionTimestamp
			// without being immediately garbage-collected by the fake client.
			Finalizers: []string{FinalizerName},
		}

		secret = &gardenercorev1beta1.InternalSecret{
			Name:      "credentials-v1",
			Namespace: "garden-test",
			Labels: map[string]string{
				constants.LabelKeyShootName: "test-shoot",
			},
		}
	})

	Context("when deletion is still in progress (Type=Delete, State=Processing)", func() {
		BeforeEach(func() {
			shoot.Status = gardenercorev1beta1.ShootStatus{
				LastOperation: &gardenercorev1beta1.LastOperation{
					Type:  gardenercorev1beta1.LastOperationTypeDelete,
					State: gardenercorev1beta1.LastOperationStateProcessing,
				},
			}
			Expect(fakeClient.Create(ctx, shoot)).To(Succeed())
			Expect(fakeClient.Create(ctx, secret)).To(Succeed())
		})

		It("should NOT delete InternalSecrets while deletion is still in progress", func() {
			_, err := rec.delete(ctx, GinkgoLogr, shoot)
			Expect(err).NotTo(HaveOccurred())

			// InternalSecret must still exist — MCM still needs the credentials.
			secretList := &gardenercorev1beta1.InternalSecretList{}
			Expect(fakeClient.List(ctx, secretList, client.InNamespace("garden-test"),
				client.MatchingLabels{constants.LabelKeyShootName: "test-shoot"})).To(Succeed())
			Expect(secretList.Items).To(HaveLen(1), "InternalSecret was deleted prematurely while shoot deletion was still in progress")
		})
	})

	Context("when deletion has succeeded (Type=Delete, State=Succeeded)", func() {
		BeforeEach(func() {
			shoot.Status = gardenercorev1beta1.ShootStatus{
				LastOperation: &gardenercorev1beta1.LastOperation{
					Type:  gardenercorev1beta1.LastOperationTypeDelete,
					State: gardenercorev1beta1.LastOperationStateSucceeded,
				},
			}
			Expect(fakeClient.Create(ctx, shoot)).To(Succeed())
			Expect(fakeClient.Create(ctx, secret)).To(Succeed())
		})

		It("should delete InternalSecrets and remove the finalizer", func() {
			_, err := rec.delete(ctx, GinkgoLogr, shoot)
			Expect(err).NotTo(HaveOccurred())

			secretList := &gardenercorev1beta1.InternalSecretList{}
			Expect(fakeClient.List(ctx, secretList, client.InNamespace("garden-test"),
				client.MatchingLabels{constants.LabelKeyShootName: "test-shoot"})).To(Succeed())
			Expect(secretList.Items).To(BeEmpty(), "InternalSecret should be deleted after successful shoot deletion")

			updated := &gardenercorev1beta1.Shoot{}
			Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(shoot), updated)).To(Succeed())
			Expect(updated.Finalizers).NotTo(ContainElement(FinalizerName), "finalizer should be removed after successful shoot deletion")
		})
	})

	Context("regression: && vs || bug in early-return condition", func() {
		It("should not delete InternalSecrets when Type=Delete and State=Failed", func() {
			// This case also has Type==Delete, so with the && bug the condition
			// evaluates to (false && true) = false and falls through to deletion.
			now := metav1.Now()
			shoot.DeletionTimestamp = &now
			shoot.Status = gardenercorev1beta1.ShootStatus{
				LastOperation: &gardenercorev1beta1.LastOperation{
					Type:  gardenercorev1beta1.LastOperationTypeDelete,
					State: gardenercorev1beta1.LastOperationStateFailed,
				},
			}
			Expect(fakeClient.Create(ctx, shoot)).To(Succeed())
			Expect(fakeClient.Create(ctx, secret)).To(Succeed())

			_, err := rec.delete(ctx, GinkgoLogr, shoot)
			Expect(err).NotTo(HaveOccurred())

			secretList := &gardenercorev1beta1.InternalSecretList{}
			Expect(fakeClient.List(ctx, secretList, client.InNamespace("garden-test"),
				client.MatchingLabels{constants.LabelKeyShootName: "test-shoot"})).To(Succeed())
			Expect(secretList.Items).To(HaveLen(1), "InternalSecret was deleted prematurely while shoot deletion was in Failed state")
		})

		It("should not delete InternalSecrets when Type=Reconcile and State=Succeeded", func() {
			// With the && bug: (true && false) = false — falls through to deletion
			// even though the shoot was never deleted.
			now := metav1.Now()
			shoot.DeletionTimestamp = &now
			shoot.Status = gardenercorev1beta1.ShootStatus{
				LastOperation: &gardenercorev1beta1.LastOperation{
					Type:  gardenercorev1beta1.LastOperationTypeReconcile,
					State: gardenercorev1beta1.LastOperationStateSucceeded,
				},
			}
			Expect(fakeClient.Create(ctx, shoot)).To(Succeed())
			Expect(fakeClient.Create(ctx, secret)).To(Succeed())

			_, err := rec.delete(ctx, GinkgoLogr, shoot)
			Expect(err).NotTo(HaveOccurred())

			secretList := &gardenercorev1beta1.InternalSecretList{}
			Expect(fakeClient.List(ctx, secretList, client.InNamespace("garden-test"),
				client.MatchingLabels{constants.LabelKeyShootName: "test-shoot"})).To(Succeed())
			Expect(secretList.Items).To(HaveLen(1), "InternalSecret was deleted prematurely — shoot had not been deleted yet (Type=Reconcile)")
		})
	})

	Context("when lastOperation is nil", func() {
		BeforeEach(func() {
			shoot.Status = gardenercorev1beta1.ShootStatus{}
			Expect(fakeClient.Create(ctx, shoot)).To(Succeed())
			Expect(fakeClient.Create(ctx, secret)).To(Succeed())
		})

		It("should not delete InternalSecrets", func() {
			result, err := rec.delete(ctx, GinkgoLogr, shoot)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			secretList := &gardenercorev1beta1.InternalSecretList{}
			Expect(fakeClient.List(ctx, secretList, client.InNamespace("garden-test"),
				client.MatchingLabels{constants.LabelKeyShootName: "test-shoot"})).To(Succeed())
			Expect(secretList.Items).To(HaveLen(1))
		})
	})
})

var _ = Describe("hasAuditLogExtension()", func() {
	It("returns false for a shoot with no extensions", func() {
		shoot := &gardenercorev1beta1.Shoot{}
		Expect(hasAuditLogExtension(shoot)).To(BeFalse())
	})

	It("returns true when persephone-auditlog extension is present and enabled", func() {
		shoot := &gardenercorev1beta1.Shoot{
			Spec: gardenercorev1beta1.ShootSpec{
				Extensions: []gardenercorev1beta1.Extension{
					{Type: constants.AuditLogExtensionType},
				},
			},
		}
		Expect(hasAuditLogExtension(shoot)).To(BeTrue())
	})

	It("returns false when persephone-auditlog extension is disabled", func() {
		shoot := &gardenercorev1beta1.Shoot{
			Spec: gardenercorev1beta1.ShootSpec{
				Extensions: []gardenercorev1beta1.Extension{
					{Type: constants.AuditLogExtensionType, Disabled: ptr.To(true)},
				},
			},
		}
		Expect(hasAuditLogExtension(shoot)).To(BeFalse())
	})

	It("returns false for an unrelated extension type", func() {
		shoot := &gardenercorev1beta1.Shoot{
			Spec: gardenercorev1beta1.ShootSpec{
				Extensions: []gardenercorev1beta1.Extension{
					{Type: "some-other-extension"},
				},
			},
		}
		Expect(hasAuditLogExtension(shoot)).To(BeFalse())
	})
})

var _ = Describe("ensureEC2InternalSecret()", func() {
	var (
		ctx        context.Context
		fakeClient client.Client
		rec        *Reconciler
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeClient = fake.NewClientBuilder().WithScheme(kubernetes.GardenScheme).Build()
		rec = &Reconciler{Client: fakeClient, Clock: clock.RealClock{}}
	})

	It("does nothing when the shoot has no persephone-auditlog extension", func() {
		shoot := &gardenercorev1beta1.Shoot{
			Name:      "test-shoot",
			Namespace: "garden-test",
		}
		Expect(rec.ensureEC2InternalSecret(ctx, GinkgoLogr, shoot)).To(Succeed())

		list := &gardenercorev1beta1.InternalSecretList{}
		Expect(fakeClient.List(ctx, list, client.InNamespace("garden-test"),
			client.MatchingLabels{constants.LabelKeyEC2Credentials: "true"})).To(Succeed())
		Expect(list.Items).To(BeEmpty())
	})

	It("does nothing when an EC2 InternalSecret already exists", func() {
		shoot := &gardenercorev1beta1.Shoot{
			Name:      "test-shoot",
			Namespace: "garden-test",
			Spec: gardenercorev1beta1.ShootSpec{
				Extensions: []gardenercorev1beta1.Extension{
					{Type: constants.AuditLogExtensionType},
				},
			},
		}
		existing := &gardenercorev1beta1.InternalSecret{
			Name:      "test-shoot-ec2-123",
			Namespace: "garden-test",
			Labels: map[string]string{
				constants.LabelKeyShootName:      "test-shoot",
				constants.LabelKeyEC2Credentials: "true",
			},
		}
		Expect(fakeClient.Create(ctx, existing)).To(Succeed())

		Expect(rec.ensureEC2InternalSecret(ctx, GinkgoLogr, shoot)).To(Succeed())

		list := &gardenercorev1beta1.InternalSecretList{}
		Expect(fakeClient.List(ctx, list, client.InNamespace("garden-test"),
			client.MatchingLabels{constants.LabelKeyEC2Credentials: "true"})).To(Succeed())
		Expect(list.Items).To(HaveLen(1), "no new EC2 InternalSecret should be created when one already exists")
	})
})
