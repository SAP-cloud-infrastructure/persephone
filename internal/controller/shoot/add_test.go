// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package shoot

import (
	"testing"

	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestShootController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Shoot Controller Suite")
}

var _ = Describe("shootRelevantUpdatePredicate", func() {
	var (
		p   shootRelevantUpdatePredicate
		now = metav1.Now()
	)

	BeforeEach(func() {
		p = shootRelevantUpdatePredicate{}
	})

	baseShoot := func() *gardenercorev1beta1.Shoot {
		return &gardenercorev1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-shoot",
				Namespace:  "garden-test",
				Generation: 1,
			},
			Status: gardenercorev1beta1.ShootStatus{
				Conditions: []gardenercorev1beta1.Condition{
					{Type: "Ready", Status: "True"},
				},
			},
		}
	}

	Describe("Update", func() {
		It("returns true when DeletionTimestamp is first set", func() {
			oldShoot := baseShoot()
			newShoot := baseShoot()
			newShoot.DeletionTimestamp = &now

			e := event.UpdateEvent{ObjectOld: oldShoot, ObjectNew: newShoot}
			Expect(p.Update(e)).To(BeTrue())
		})

		It("returns false during deletion for status-only update without successful delete", func() {
			oldShoot := baseShoot()
			oldShoot.DeletionTimestamp = &now
			oldShoot.Status.LastOperation = &gardenercorev1beta1.LastOperation{
				Type:  gardenercorev1beta1.LastOperationTypeDelete,
				State: gardenercorev1beta1.LastOperationStateProcessing,
			}

			newShoot := baseShoot()
			newShoot.DeletionTimestamp = &now
			newShoot.Status.LastOperation = &gardenercorev1beta1.LastOperation{
				Type:  gardenercorev1beta1.LastOperationTypeDelete,
				State: gardenercorev1beta1.LastOperationStateProcessing,
			}
			newShoot.Status.Conditions = []gardenercorev1beta1.Condition{
				{Type: "Ready", Status: "False"},
			}

			e := event.UpdateEvent{ObjectOld: oldShoot, ObjectNew: newShoot}
			Expect(p.Update(e)).To(BeFalse())
		})

		It("returns true during deletion when lastOperation indicates successful delete", func() {
			oldShoot := baseShoot()
			oldShoot.DeletionTimestamp = &now
			oldShoot.Status.LastOperation = &gardenercorev1beta1.LastOperation{
				Type:  gardenercorev1beta1.LastOperationTypeDelete,
				State: gardenercorev1beta1.LastOperationStateProcessing,
			}

			newShoot := baseShoot()
			newShoot.DeletionTimestamp = &now
			newShoot.Status.LastOperation = &gardenercorev1beta1.LastOperation{
				Type:  gardenercorev1beta1.LastOperationTypeDelete,
				State: gardenercorev1beta1.LastOperationStateSucceeded,
			}

			e := event.UpdateEvent{ObjectOld: oldShoot, ObjectNew: newShoot}
			Expect(p.Update(e)).To(BeTrue())
		})

		It("returns true when generation changes", func() {
			oldShoot := baseShoot()
			newShoot := baseShoot()
			newShoot.Generation = 2

			e := event.UpdateEvent{ObjectOld: oldShoot, ObjectNew: newShoot}
			Expect(p.Update(e)).To(BeTrue())
		})

		It("returns true when annotations change", func() {
			oldShoot := baseShoot()
			newShoot := baseShoot()
			newShoot.Annotations = map[string]string{"force-update": "true"}

			e := event.UpdateEvent{ObjectOld: oldShoot, ObjectNew: newShoot}
			Expect(p.Update(e)).To(BeTrue())
		})

		It("returns true when finalizers change", func() {
			oldShoot := baseShoot()
			newShoot := baseShoot()
			newShoot.Finalizers = []string{"some-finalizer"}

			e := event.UpdateEvent{ObjectOld: oldShoot, ObjectNew: newShoot}
			Expect(p.Update(e)).To(BeTrue())
		})

		It("returns true when labels change", func() {
			oldShoot := baseShoot()
			newShoot := baseShoot()
			newShoot.Labels = map[string]string{"new-label": "value"}

			e := event.UpdateEvent{ObjectOld: oldShoot, ObjectNew: newShoot}
			Expect(p.Update(e)).To(BeTrue())
		})

		It("returns true when spec changes", func() {
			oldShoot := baseShoot()
			newShoot := baseShoot()
			newShoot.Spec.Region = "eu-de-1"

			e := event.UpdateEvent{ObjectOld: oldShoot, ObjectNew: newShoot}
			Expect(p.Update(e)).To(BeTrue())
		})

		It("returns false for status-only update", func() {
			oldShoot := baseShoot()
			newShoot := baseShoot()
			newShoot.Status.Conditions = []gardenercorev1beta1.Condition{
				{Type: "Ready", Status: "False"},
			}

			e := event.UpdateEvent{ObjectOld: oldShoot, ObjectNew: newShoot}
			Expect(p.Update(e)).To(BeFalse())
		})

		It("returns false when nothing changed", func() {
			oldShoot := baseShoot()
			newShoot := baseShoot()

			e := event.UpdateEvent{ObjectOld: oldShoot, ObjectNew: newShoot}
			Expect(p.Update(e)).To(BeFalse())
		})
	})
})
