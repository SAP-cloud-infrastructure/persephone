// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audithandler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/audittools"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"github.com/sap-cloud-infrastructure/persephone/internal/config"
	"github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
)

var _ = Describe("Handler", func() {
	var (
		handler *Handler
		auditor *audittools.MockAuditor
	)

	BeforeEach(func() {
		auditor = audittools.NewMockAuditor()

		regions := map[string]config.RegionalConfig{
			"qa-de-1": {
				Enabled:           true,
				AuditTransportURL: "",
			},
		}

		observer := audittools.Observer{
			TypeURI: "service/persephone-webhook",
			Name:    "persephone-webhook",
			ID:      "test",
		}

		ctx := context.Background()
		regionAuditors, err := NewRegionAuditorSet(ctx, regions, observer, "notifications.info", nil)
		Expect(err).NotTo(HaveOccurred())

		// Override with mock auditor for testing
		regionAuditors.auditors["qa-de-1"] = auditor

		handler = &Handler{
			Logger:         logr.Discard(),
			RegionAuditors: regionAuditors,
			metrics:        newMetrics(prometheus.NewRegistry()),
		}
	})

	Describe("ServeHTTP", func() {
		It("should process valid Shoot create event", func() {
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

			eventList := &auditv1.EventList{
				Items: []auditv1.Event{
					{
						Verb:  "create",
						Stage: auditv1.StageResponseComplete,
						ObjectRef: &auditv1.ObjectReference{
							APIGroup:  "core.gardener.cloud",
							Resource:  "shoots",
							Name:      "test-shoot",
							Namespace: "garden-test",
						},
						User: authenticationv1.UserInfo{
							Username: "test-user",
							Extra: kubernetes.OpenStackUserInfo{
								Region: "qa-de-1",
							}.ToExtra(),
						},
						RequestObject: &runtime.Unknown{
							Raw: shootRaw,
						},
						ResponseStatus: &metav1.Status{
							Code: 201,
						},
						SourceIPs:      []string{"127.0.0.1"},
						StageTimestamp: metav1.NewMicroTime(metav1.Now().Time),
					},
				},
			}

			body, err := json.Marshal(eventList)
			Expect(err).NotTo(HaveOccurred())

			req := httptest.NewRequest(http.MethodPost, "/audit", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(auditor.RecordedEvents()).To(HaveLen(1))
		})

		It("should filter non-Shoot events", func() {
			eventList := &auditv1.EventList{
				Items: []auditv1.Event{
					{
						Verb:  "create",
						Stage: auditv1.StageResponseComplete,
						ObjectRef: &auditv1.ObjectReference{
							APIGroup: "core.gardener.cloud",
							Resource: "projects",
							Name:     "test-project",
						},
						User: authenticationv1.UserInfo{
							Username: "test-user",
							Extra: kubernetes.OpenStackUserInfo{
								Region: "qa-de-1",
							}.ToExtra(),
						},
					},
				},
			}

			body, err := json.Marshal(eventList)
			Expect(err).NotTo(HaveOccurred())

			req := httptest.NewRequest(http.MethodPost, "/audit", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(auditor.RecordedEvents()).To(BeEmpty())
		})

		It("should return error for unknown region", func() {
			shoot := &gardenercorev1beta1.Shoot{
				Spec: gardenercorev1beta1.ShootSpec{
					Region: "unknown-region",
				},
			}
			shootRaw, err := json.Marshal(shoot)
			Expect(err).NotTo(HaveOccurred())

			eventList := &auditv1.EventList{
				Items: []auditv1.Event{
					{
						Verb:  "create",
						Stage: auditv1.StageResponseComplete,
						ObjectRef: &auditv1.ObjectReference{
							APIGroup: "core.gardener.cloud",
							Resource: "shoots",
						},
						User: authenticationv1.UserInfo{
							Extra: kubernetes.OpenStackUserInfo{
								Region: "qa-de-1",
							}.ToExtra(),
						},
						RequestObject: &runtime.Unknown{
							Raw: shootRaw,
						},
						SourceIPs:      []string{"127.0.0.1"},
						StageTimestamp: metav1.NewMicroTime(metav1.Now().Time),
					},
				},
			}

			body, err := json.Marshal(eventList)
			Expect(err).NotTo(HaveOccurred())

			req := httptest.NewRequest(http.MethodPost, "/audit", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})

		It("should process kubeconfig subresource event", func() {
			eventList := &auditv1.EventList{
				Items: []auditv1.Event{
					{
						Verb:  "create",
						Stage: auditv1.StageResponseComplete,
						ObjectRef: &auditv1.ObjectReference{
							APIGroup:    "core.gardener.cloud",
							Resource:    "shoots",
							Subresource: "adminkubeconfig",
							Name:        "test-shoot",
							Namespace:   "garden-test",
						},
						User: authenticationv1.UserInfo{
							Username: "test-user",
							Extra: kubernetes.OpenStackUserInfo{
								Region: "qa-de-1",
							}.ToExtra(),
						},
						RequestURI: "/apis/core.gardener.cloud/v1beta1/namespaces/garden-test/shoots/test-shoot/adminkubeconfig",
						RequestObject: &runtime.Unknown{
							Raw: []byte(`{"spec":{"expirationSeconds":3600}}`),
						},
						ResponseStatus: &metav1.Status{
							Code: 201,
						},
						SourceIPs:      []string{"127.0.0.1"},
						StageTimestamp: metav1.NewMicroTime(metav1.Now().Time),
					},
				},
			}

			body, err := json.Marshal(eventList)
			Expect(err).NotTo(HaveOccurred())

			req := httptest.NewRequest(http.MethodPost, "/audit", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			events := auditor.RecordedEvents()
			Expect(events).To(HaveLen(1))
			Expect(events[0].Action).To(Equal(cadf.Action("authenticate/admin")))
			Expect(events[0].Target.TypeURI).To(Equal("kubernetes/cluster"))
			Expect(events[0].Target.Name).To(Equal("garden-test/test-shoot"))
		})

		It("should filter unsupported shoot subresource events", func() {
			eventList := &auditv1.EventList{
				Items: []auditv1.Event{
					{
						Verb:  "update",
						Stage: auditv1.StageResponseComplete,
						ObjectRef: &auditv1.ObjectReference{
							APIGroup:    "core.gardener.cloud",
							Resource:    "shoots",
							Subresource: "status",
							Name:        "test-shoot",
							Namespace:   "garden-test",
						},
						User: authenticationv1.UserInfo{
							Username: "test-user",
							Extra: kubernetes.OpenStackUserInfo{
								Region: "qa-de-1",
							}.ToExtra(),
						},
						SourceIPs:      []string{"127.0.0.1"},
						StageTimestamp: metav1.NewMicroTime(metav1.Now().Time),
					},
				},
			}

			body, err := json.Marshal(eventList)
			Expect(err).NotTo(HaveOccurred())

			req := httptest.NewRequest(http.MethodPost, "/audit", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(auditor.RecordedEvents()).To(BeEmpty())
		})
	})
})
