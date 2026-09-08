// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package audithandler

import (
	"encoding/json"
	"net/http"
	"time"

	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/go-api-declarations/cadf"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
)

var _ = Describe("Transform", func() {
	Describe("extractShootRegion", func() {
		It("should extract region from RequestObject", func() {
			shoot := &gardenercorev1beta1.Shoot{
				Spec: gardenercorev1beta1.ShootSpec{
					Region: "qa-de-1",
				},
			}
			raw, err := json.Marshal(shoot)
			Expect(err).NotTo(HaveOccurred())

			event := &auditv1.Event{
				RequestObject: &runtime.Unknown{
					Raw: raw,
				},
			}

			region, err := extractShootRegion(event)
			Expect(err).NotTo(HaveOccurred())
			Expect(region).To(Equal("qa-de-1"))
		})

		It("should extract region from ResponseObject on delete", func() {
			shoot := &gardenercorev1beta1.Shoot{
				Spec: gardenercorev1beta1.ShootSpec{
					Region: "eu-de-1",
				},
			}
			raw, err := json.Marshal(shoot)
			Expect(err).NotTo(HaveOccurred())

			event := &auditv1.Event{
				Verb: "delete",
				ResponseObject: &runtime.Unknown{
					Raw: raw,
				},
			}

			region, err := extractShootRegion(event)
			Expect(err).NotTo(HaveOccurred())
			Expect(region).To(Equal("eu-de-1"))
		})

		It("should return error if region not found", func() {
			event := &auditv1.Event{}

			_, err := extractShootRegion(event)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("could not extract region from audit event"))
		})
	})

	Describe("transformToAuditEvent", func() {
		It("should transform create event", func() {
			now := time.Now()
			shoot := &gardenercorev1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot",
					Namespace: "garden-test",
				},
				Spec: gardenercorev1beta1.ShootSpec{
					Region: "qa-de-1",
				},
			}
			shootRaw, err := json.Marshal(shoot)
			Expect(err).NotTo(HaveOccurred())

			event := &auditv1.Event{
				AuditID: "test-audit-id",
				Verb:    "create",
				Stage:   auditv1.StageResponseComplete,
				ObjectRef: &auditv1.ObjectReference{
					APIGroup:  "core.gardener.cloud",
					Resource:  "shoots",
					Name:      "test-shoot",
					Namespace: "garden-test",
				},
				User: authenticationv1.UserInfo{
					Username: "test-user",
					Extra: kubernetes.OpenStackUserInfo{
						Region:            "qa-de-1",
						ProjectID:         "project-id",
						ProjectName:       "project-name",
						ProjectDomainID:   "domain-id",
						ProjectDomainName: "domain-name",
						UserDomainID:      "user-domain-id",
						UserDomainName:    "user-domain-name",
					}.ToExtra(),
				},
				RequestURI: "/apis/core.gardener.cloud/v1beta1/namespaces/garden-test/shoots",
				RequestObject: &runtime.Unknown{
					Raw: shootRaw,
				},
				ResponseStatus: &metav1.Status{
					Code: http.StatusCreated,
				},
				StageTimestamp: metav1.NewMicroTime(now),
				SourceIPs:      []string{"192.168.1.1"},
			}

			auditEvent := transformToAuditEvent(event)
			Expect(auditEvent).NotTo(BeNil())
			Expect(auditEvent.Time).To(Equal(now))
			Expect(auditEvent.ReasonCode).To(Equal(http.StatusCreated))
			Expect(auditEvent.Action).To(Equal(cadf.CreateAction))
		})
	})

	Describe("shootTarget", func() {
		It("should render with namespace/name as ID and Name", func() {
			target := shootTarget{
				namespace: "garden-test",
				name:      "test-shoot",
			}

			resource := target.Render()
			Expect(resource.TypeURI).To(Equal("kubernetes/cluster"))
			Expect(resource.ID).To(Equal("garden-test/test-shoot"))
			Expect(resource.Name).To(Equal("garden-test/test-shoot"))
		})

		It("should include requestBody attachment when present", func() {
			requestBody := json.RawMessage(`{"spec":{"region":"qa-de-1"}}`)
			target := shootTarget{
				namespace:   "garden-test",
				name:        "test-shoot",
				requestBody: requestBody,
			}

			resource := target.Render()
			Expect(resource.Attachments).To(HaveLen(1))
			Expect(resource.Attachments[0].Name).To(Equal("requestBody"))
			Expect(resource.Attachments[0].TypeURI).To(Equal("mime:application/json"))
			Expect(resource.Attachments[0].Content).To(ContainSubstring(`"region":"qa-de-1"`))
		})

		It("should omit attachment when requestBody is nil", func() {
			target := shootTarget{
				namespace: "garden-test",
				name:      "test-shoot",
			}

			resource := target.Render()
			Expect(resource.Attachments).To(BeEmpty())
		})
	})

	Describe("transformToKubeconfigAuditEvent", func() {
		It("should use authenticate/admin action for adminkubeconfig", func() {
			now := time.Now()
			event := &auditv1.Event{
				Verb:  "create",
				Stage: auditv1.StageResponseComplete,
				ObjectRef: &auditv1.ObjectReference{
					APIGroup:    "core.gardener.cloud",
					Resource:    "shoots",
					Subresource: "adminkubeconfig",
					Namespace:   "garden-test",
					Name:        "my-shoot",
				},
				User: authenticationv1.UserInfo{
					Username: "test-user",
					Extra: kubernetes.OpenStackUserInfo{
						Region: "qa-de-1",
					}.ToExtra(),
				},
				RequestURI: "/apis/core.gardener.cloud/v1beta1/namespaces/garden-test/shoots/my-shoot/adminkubeconfig",
				RequestObject: &runtime.Unknown{
					Raw: []byte(`{"spec":{"expirationSeconds":3600}}`),
				},
				ResponseStatus: &metav1.Status{
					Code: http.StatusCreated,
				},
				StageTimestamp: metav1.NewMicroTime(now),
				SourceIPs:      []string{"10.0.0.1"},
			}

			auditEvent := transformToKubeconfigAuditEvent(event)
			Expect(auditEvent).NotTo(BeNil())
			Expect(auditEvent.Action).To(Equal(cadf.Action("authenticate/admin")))
			Expect(auditEvent.ReasonCode).To(Equal(http.StatusCreated))
			Expect(auditEvent.Time).To(Equal(now))
		})

		It("should use authenticate/viewer action for viewerkubeconfig", func() {
			event := &auditv1.Event{
				Verb:  "create",
				Stage: auditv1.StageResponseComplete,
				ObjectRef: &auditv1.ObjectReference{
					APIGroup:    "core.gardener.cloud",
					Resource:    "shoots",
					Subresource: "viewerkubeconfig",
					Namespace:   "garden-myproject",
					Name:        "prod-cluster",
				},
				User: authenticationv1.UserInfo{
					Username: "test-user",
					Extra: kubernetes.OpenStackUserInfo{
						Region: "eu-de-1",
					}.ToExtra(),
				},
				RequestURI:     "/apis/core.gardener.cloud/v1beta1/namespaces/garden-myproject/shoots/prod-cluster/viewerkubeconfig",
				ResponseStatus: &metav1.Status{Code: 201},
				StageTimestamp: metav1.NewMicroTime(time.Now()),
				SourceIPs:      []string{"10.0.0.1"},
			}

			auditEvent := transformToKubeconfigAuditEvent(event)
			Expect(auditEvent.Action).To(Equal(cadf.Action("authenticate/viewer")))
		})

		It("should build target referencing the Shoot cluster", func() {
			event := &auditv1.Event{
				Verb:  "create",
				Stage: auditv1.StageResponseComplete,
				ObjectRef: &auditv1.ObjectReference{
					APIGroup:    "core.gardener.cloud",
					Resource:    "shoots",
					Subresource: "viewerkubeconfig",
					Namespace:   "garden-myproject",
					Name:        "prod-cluster",
				},
				User: authenticationv1.UserInfo{
					Username: "test-user",
					Extra: kubernetes.OpenStackUserInfo{
						Region: "eu-de-1",
					}.ToExtra(),
				},
				RequestURI:     "/apis/core.gardener.cloud/v1beta1/namespaces/garden-myproject/shoots/prod-cluster/viewerkubeconfig",
				ResponseStatus: &metav1.Status{Code: 201},
				StageTimestamp: metav1.NewMicroTime(time.Now()),
				SourceIPs:      []string{"10.0.0.1"},
			}

			auditEvent := transformToKubeconfigAuditEvent(event)
			target := auditEvent.Target.(shootTarget)
			Expect(target.namespace).To(Equal("garden-myproject"))
			Expect(target.name).To(Equal("prod-cluster"))

			resource := target.Render()
			Expect(resource.TypeURI).To(Equal("kubernetes/cluster"))
			Expect(resource.ID).To(Equal("garden-myproject/prod-cluster"))
			Expect(resource.Name).To(Equal("garden-myproject/prod-cluster"))
		})

		It("should include requestBody attachment", func() {
			event := &auditv1.Event{
				Verb:  "create",
				Stage: auditv1.StageResponseComplete,
				ObjectRef: &auditv1.ObjectReference{
					APIGroup:    "core.gardener.cloud",
					Resource:    "shoots",
					Subresource: "adminkubeconfig",
					Namespace:   "garden-test",
					Name:        "my-shoot",
				},
				User: authenticationv1.UserInfo{
					Username: "test-user",
					Extra: kubernetes.OpenStackUserInfo{
						Region: "qa-de-1",
					}.ToExtra(),
				},
				RequestURI: "/apis/core.gardener.cloud/v1beta1/namespaces/garden-test/shoots/my-shoot/adminkubeconfig",
				RequestObject: &runtime.Unknown{
					Raw: []byte(`{"spec":{"expirationSeconds":3600}}`),
				},
				ResponseStatus: &metav1.Status{Code: 201},
				StageTimestamp: metav1.NewMicroTime(time.Now()),
				SourceIPs:      []string{"10.0.0.1"},
			}

			auditEvent := transformToKubeconfigAuditEvent(event)
			resource := auditEvent.Target.Render()
			Expect(resource.Attachments).To(HaveLen(1))
			Expect(resource.Attachments[0].Name).To(Equal("requestBody"))
			Expect(resource.Attachments[0].Content).To(ContainSubstring(`"expirationSeconds":3600`))
		})
	})

	Describe("auditUserInfo", func() {
		It("should include OpenStack project and domain info in initiator", func() {
			userInfo := auditUserInfo{
				username: "test-user@example.com",
				uid:      "user-uid-123",
				extra: kubernetes.OpenStackUserInfo{
					ProjectID:         "project-123",
					ProjectName:       "test-project",
					ProjectDomainID:   "domain-456",
					ProjectDomainName: "test-domain",
					UserDomainID:      "user-domain-789",
					UserDomainName:    "user-domain-name",
					Region:            "qa-de-1",
				}.ToExtra(),
			}

			host := cadf.Host{
				Address: "192.168.1.1",
				Agent:   "kubectl/v1.28.0",
			}

			initiator := userInfo.AsInitiator(host)

			Expect(initiator.TypeURI).To(Equal("service/security/account/user"))
			Expect(initiator.ID).To(Equal("test-user@example.com"))
			Expect(initiator.ProjectID).To(Equal("project-123"))
			Expect(initiator.ProjectName).To(Equal("test-project"))
			Expect(initiator.DomainID).To(Equal("domain-456"))
			Expect(initiator.DomainName).To(Equal("test-domain"))
			Expect(initiator.ProjectDomainName).To(Equal("test-domain"))
			Expect(initiator.Host).NotTo(BeNil())
			Expect(initiator.Host.Address).To(Equal("192.168.1.1"))
			Expect(initiator.Host.Agent).To(Equal("kubectl/v1.28.0"))
		})

		It("should work without OpenStack extra fields", func() {
			userInfo := auditUserInfo{
				username: "test-user@example.com",
				uid:      "user-uid-123",
				extra:    nil,
			}

			host := cadf.Host{
				Address: "192.168.1.1",
			}

			initiator := userInfo.AsInitiator(host)

			Expect(initiator.TypeURI).To(Equal("service/security/account/user"))
			Expect(initiator.ID).To(Equal("test-user@example.com"))
			Expect(initiator.ProjectID).To(BeEmpty())
			Expect(initiator.ProjectName).To(BeEmpty())
			Expect(initiator.DomainID).To(BeEmpty())
		})
	})
})
