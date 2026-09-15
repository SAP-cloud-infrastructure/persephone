// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package audithandler

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	authenticationv1 "k8s.io/api/authentication/v1"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
)

var _ = Describe("Filter", func() {
	Describe("shouldProcessEvent", func() {
		It("should accept Shoot create events", func() {
			event := &auditv1.Event{
				ObjectRef: &auditv1.ObjectReference{
					APIGroup: "core.gardener.cloud",
					Resource: "shoots",
				},
				Verb:  "create",
				Stage: auditv1.StageResponseComplete,
				User: authenticationv1.UserInfo{
					Extra: kubernetes.OpenStackUserInfo{
						Region: "qa-de-1",
					}.ToExtra(),
				},
			}
			Expect(shouldProcessEvent(event)).To(BeTrue())
		})

		It("should accept Shoot update events", func() {
			event := &auditv1.Event{
				ObjectRef: &auditv1.ObjectReference{
					APIGroup: "core.gardener.cloud",
					Resource: "shoots",
				},
				Verb:  "update",
				Stage: auditv1.StageResponseComplete,
				User: authenticationv1.UserInfo{
					Extra: kubernetes.OpenStackUserInfo{
						Region: "qa-de-1",
					}.ToExtra(),
				},
			}
			Expect(shouldProcessEvent(event)).To(BeTrue())
		})

		It("should accept Shoot delete events", func() {
			event := &auditv1.Event{
				ObjectRef: &auditv1.ObjectReference{
					APIGroup: "core.gardener.cloud",
					Resource: "shoots",
				},
				Verb:  "delete",
				Stage: auditv1.StageResponseComplete,
				User: authenticationv1.UserInfo{
					Extra: kubernetes.OpenStackUserInfo{
						Region: "qa-de-1",
					}.ToExtra(),
				},
			}
			Expect(shouldProcessEvent(event)).To(BeTrue())
		})

		It("should reject non-Shoot resources", func() {
			event := &auditv1.Event{
				ObjectRef: &auditv1.ObjectReference{
					APIGroup: "core.gardener.cloud",
					Resource: "projects",
				},
				Verb:  "create",
				Stage: auditv1.StageResponseComplete,
				User: authenticationv1.UserInfo{
					Extra: kubernetes.OpenStackUserInfo{
						Region: "qa-de-1",
					}.ToExtra(),
				},
			}
			Expect(shouldProcessEvent(event)).To(BeFalse())
		})

		It("should reject non-CRUD verbs", func() {
			event := &auditv1.Event{
				ObjectRef: &auditv1.ObjectReference{
					APIGroup: "core.gardener.cloud",
					Resource: "shoots",
				},
				Verb:  "get",
				Stage: auditv1.StageResponseComplete,
				User: authenticationv1.UserInfo{
					Extra: kubernetes.OpenStackUserInfo{
						Region: "qa-de-1",
					}.ToExtra(),
				},
			}
			Expect(shouldProcessEvent(event)).To(BeFalse())
		})

		It("should reject RequestReceived stage", func() {
			event := &auditv1.Event{
				ObjectRef: &auditv1.ObjectReference{
					APIGroup: "core.gardener.cloud",
					Resource: "shoots",
				},
				Verb:  "create",
				Stage: auditv1.StageRequestReceived,
				User: authenticationv1.UserInfo{
					Extra: kubernetes.OpenStackUserInfo{
						Region: "qa-de-1",
					}.ToExtra(),
				},
			}
			Expect(shouldProcessEvent(event)).To(BeFalse())
		})

		It("should reject users without OpenStack credentials", func() {
			event := &auditv1.Event{
				ObjectRef: &auditv1.ObjectReference{
					APIGroup: "core.gardener.cloud",
					Resource: "shoots",
				},
				Verb:  "create",
				Stage: auditv1.StageResponseComplete,
				User: authenticationv1.UserInfo{
					Username: "system:serviceaccount:garden:gardener",
				},
			}
			Expect(shouldProcessEvent(event)).To(BeFalse())
		})

		It("should reject unsupported shoot subresources", func() {
			event := &auditv1.Event{
				ObjectRef: &auditv1.ObjectReference{
					APIGroup:    "core.gardener.cloud",
					Resource:    "shoots",
					Subresource: "status",
				},
				Verb:  "update",
				Stage: auditv1.StageResponseComplete,
				User: authenticationv1.UserInfo{
					Extra: kubernetes.OpenStackUserInfo{
						Region: "qa-de-1",
					}.ToExtra(),
				},
			}
			Expect(shouldProcessEvent(event)).To(BeFalse())
		})

		It("should accept adminkubeconfig create events", func() {
			event := &auditv1.Event{
				ObjectRef: &auditv1.ObjectReference{
					APIGroup:    "core.gardener.cloud",
					Resource:    "shoots",
					Subresource: "adminkubeconfig",
					Namespace:   "garden-test",
					Name:        "my-shoot",
				},
				Verb:  "create",
				Stage: auditv1.StageResponseComplete,
				User: authenticationv1.UserInfo{
					Extra: kubernetes.OpenStackUserInfo{
						Region: "qa-de-1",
					}.ToExtra(),
				},
			}
			Expect(shouldProcessEvent(event)).To(BeTrue())
		})

		It("should accept viewerkubeconfig create events", func() {
			event := &auditv1.Event{
				ObjectRef: &auditv1.ObjectReference{
					APIGroup:    "core.gardener.cloud",
					Resource:    "shoots",
					Subresource: "viewerkubeconfig",
					Namespace:   "garden-test",
					Name:        "my-shoot",
				},
				Verb:  "create",
				Stage: auditv1.StageResponseComplete,
				User: authenticationv1.UserInfo{
					Extra: kubernetes.OpenStackUserInfo{
						Region: "qa-de-1",
					}.ToExtra(),
				},
			}
			Expect(shouldProcessEvent(event)).To(BeTrue())
		})

		It("should reject kubeconfig events without OpenStack credentials", func() {
			event := &auditv1.Event{
				ObjectRef: &auditv1.ObjectReference{
					APIGroup:    "core.gardener.cloud",
					Resource:    "shoots",
					Subresource: "adminkubeconfig",
				},
				Verb:  "create",
				Stage: auditv1.StageResponseComplete,
				User: authenticationv1.UserInfo{
					Username: "system:serviceaccount:garden:gardener",
				},
			}
			Expect(shouldProcessEvent(event)).To(BeFalse())
		})

		It("should reject kubeconfig get events", func() {
			event := &auditv1.Event{
				ObjectRef: &auditv1.ObjectReference{
					APIGroup:    "core.gardener.cloud",
					Resource:    "shoots",
					Subresource: "adminkubeconfig",
				},
				Verb:  "get",
				Stage: auditv1.StageResponseComplete,
				User: authenticationv1.UserInfo{
					Extra: kubernetes.OpenStackUserInfo{
						Region: "qa-de-1",
					}.ToExtra(),
				},
			}
			Expect(shouldProcessEvent(event)).To(BeFalse())
		})
	})
})
