// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	"github.com/gardener/gardener/extensions/pkg/webhook/controlplane/genericmutator"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// AddToManager registers the persephone-auditlog ControlPlane mutating webhook.
func AddToManager(mgr manager.Manager) (*extensionswebhook.Webhook, error) {
	return extensionswebhook.New(mgr, extensionswebhook.Args{
		Name:     "persephone-auditlog-controlplane",
		Provider: "persephone-auditlog",
		Target:   extensionswebhook.TargetSeed,
		Path:     "/persephone-auditlog-controlplane",
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				v1beta1constants.GardenRole: v1beta1constants.GardenRoleShoot,
			},
		},
		Mutators: map[extensionswebhook.Mutator][]extensionswebhook.Type{
			genericmutator.NewMutator(
				mgr,
				NewEnsurer(mgr),
				nil, nil, nil,
				mgr.GetLogger().WithName("persephone-auditlog-controlplane"),
			): {
				{Obj: &appsv1.Deployment{}},
			},
		},
	})
}
