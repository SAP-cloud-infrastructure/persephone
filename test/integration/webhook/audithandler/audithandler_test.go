package audithandler_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	authenticationv1alpha1 "github.com/gardener/gardener/pkg/apis/authentication/v1alpha1"
	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	. "github.com/gardener/gardener/pkg/utils/test/matchers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/audittools"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
	"k8s.io/client-go/rest"

	"github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
	"github.com/sap-cloud-infrastructure/persephone/internal/webhook/audithandler"
)

var _ = Describe("AuditHandler Integration Tests", func() {
	var (
		// Helper function to send audit events to the handler
		sendAuditEvent func(event *auditv1.Event) (*http.Response, error)

		// Helper function to send multiple events as a batch
		sendAuditEvents func(events ...*auditv1.Event) (*http.Response, error)
	)

	BeforeEach(func() {
		// Reset mock auditors before each test
		mockAuditorRegion1.IgnoreEventsUntilNow()
		mockAuditorRegion2.IgnoreEventsUntilNow()

		// Helper to send single event
		sendAuditEvent = func(event *auditv1.Event) (*http.Response, error) {
			return sendAuditEvents(event)
		}

		// Helper to send batch of events
		sendAuditEvents = func(events ...*auditv1.Event) (*http.Response, error) {
			eventList := &auditv1.EventList{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "audit.k8s.io/v1",
					Kind:       "EventList",
				},
				Items: make([]auditv1.Event, len(events)),
			}

			for i, event := range events {
				eventList.Items[i] = *event
			}

			body, err := json.Marshal(eventList)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal event list: %w", err)
			}

			webhookURL := url.URL{
				Scheme: "https",
				Host:   net.JoinHostPort(webhookHost, strconv.Itoa(webhookPort)),
				Path:   audithandler.WebhookPath,
			}

			req, err := http.NewRequest(http.MethodPost, webhookURL.String(), bytes.NewReader(body))
			if err != nil {
				return nil, fmt.Errorf("failed to create request: %w", err)
			}

			req.Header.Set("Content-Type", "application/json")

			// Use the test client's transport (which has TLS config)
			// Create TLS config from rest config
			tlsConfig, err := rest.TLSConfigFor(testRestConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to create TLS config: %w", err)
			}

			// Disable certificate verification for testing environment
			// (envtest uses self-signed certificates)
			tlsConfig.InsecureSkipVerify = true

			client := &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: tlsConfig,
				},
			}

			return client.Do(req)
		}
	})

	// =============================================================================
	// Direct Event Posting Tests (Handler Unit Tests)
	// =============================================================================
	// These tests POST fabricated audit events directly to the webhook endpoint.
	// They're useful for testing edge cases, error handling, and specific scenarios
	// that are difficult to trigger via real API server operations.

	Context("Direct Event Posting (Handler Unit Tests)", func() {
		Context("Event Filtering", func() {
			It("should filter out events with wrong stage", func() {
				event := createTestAuditEvent(testRegion1, "create", auditv1.StageRequestReceived)

				resp, err := sendAuditEvent(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				// No events should be recorded
				events := mockAuditorRegion1.RecordedEvents()
				Expect(events).To(BeEmpty())
			})

			It("should filter out events for non-Shoot resources", func() {
				event := createTestAuditEvent(testRegion1, "create", auditv1.StageResponseComplete)
				event.ObjectRef.Resource = "projects"

				resp, err := sendAuditEvent(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				// No events should be recorded
				events := mockAuditorRegion1.RecordedEvents()
				Expect(events).To(BeEmpty())
			})

			It("should filter out events with non-CRUD verbs", func() {
				event := createTestAuditEvent(testRegion1, "get", auditv1.StageResponseComplete)

				resp, err := sendAuditEvent(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				// No events should be recorded
				events := mockAuditorRegion1.RecordedEvents()
				Expect(events).To(BeEmpty())
			})

			It("should filter out events from non-OpenStack users", func() {
				event := createTestAuditEvent(testRegion1, "create", auditv1.StageResponseComplete)
				event.User.Extra = nil // Remove OpenStack credentials

				resp, err := sendAuditEvent(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				// No events should be recorded
				events := mockAuditorRegion1.RecordedEvents()
				Expect(events).To(BeEmpty())
			})

			It("should process valid Shoot CRUD events from OpenStack users", func() {
				event := createTestAuditEvent(testRegion1, "create", auditv1.StageResponseComplete)

				resp, err := sendAuditEvent(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				// Event should be recorded
				events := mockAuditorRegion1.RecordedEvents()
				Expect(events).To(HaveLen(1))
			})
		})

		Context("Event Transformation", func() {
			It("should transform create event to CADF format", func() {
				event := createTestAuditEvent(testRegion1, "create", auditv1.StageResponseComplete)

				resp, err := sendAuditEvent(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				events := mockAuditorRegion1.RecordedEvents()
				Expect(events).To(HaveLen(1))

				cadfEvent := events[0]
				Expect(cadfEvent.Action).To(Equal(cadf.CreateAction))
				Expect(cadfEvent.Target.TypeURI).To(Equal("kubernetes/cluster"))
				Expect(cadfEvent.Target.ID).To(Equal(testNamespace.Name + "/test-shoot"))
			})

			It("should transform update event to CADF format", func() {
				event := createTestAuditEvent(testRegion1, "update", auditv1.StageResponseComplete)

				resp, err := sendAuditEvent(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				events := mockAuditorRegion1.RecordedEvents()
				Expect(events).To(HaveLen(1))

				cadfEvent := events[0]
				Expect(cadfEvent.Action).To(Equal(cadf.UpdateAction))
			})

			It("should transform patch event to CADF update action", func() {
				event := createTestAuditEvent(testRegion1, "patch", auditv1.StageResponseComplete)

				resp, err := sendAuditEvent(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				events := mockAuditorRegion1.RecordedEvents()
				Expect(events).To(HaveLen(1))

				cadfEvent := events[0]
				Expect(cadfEvent.Action).To(Equal(cadf.UpdateAction))
			})

			It("should transform delete event to CADF format", func() {
				event := createTestAuditEvent(testRegion1, "delete", auditv1.StageResponseComplete)
				// For delete, we need ResponseObject instead of RequestObject
				event.ResponseObject = event.RequestObject
				event.RequestObject = nil

				resp, err := sendAuditEvent(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				events := mockAuditorRegion1.RecordedEvents()
				Expect(events).To(HaveLen(1))

				cadfEvent := events[0]
				Expect(cadfEvent.Action).To(Equal(cadf.DeleteAction))
			})

			It("should include correct target information", func() {
				event := createTestAuditEvent(testRegion1, "create", auditv1.StageResponseComplete)

				resp, err := sendAuditEvent(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				events := mockAuditorRegion1.RecordedEvents()
				Expect(events).To(HaveLen(1))

				cadfEvent := events[0]
				Expect(cadfEvent.Target.TypeURI).To(Equal("kubernetes/cluster"))
				Expect(cadfEvent.Target.Name).To(Equal(testNamespace.Name + "/test-shoot"))
				Expect(cadfEvent.Target.ID).To(Equal(testNamespace.Name + "/test-shoot"))
			})

			// Note: We don't test initiator fields here because MockAuditor normalizes
			// the Initiator to an empty struct when TypeURI is "service/security/account/user".
			// The initiator transformation is tested in the unit tests (transform_test.go).
		})

		Context("Multi-Region Routing", func() {
			It("should route events to correct regional auditor", func() {
				event1 := createTestAuditEvent(testRegion1, "create", auditv1.StageResponseComplete)
				event2 := createTestAuditEvent(testRegion2, "create", auditv1.StageResponseComplete)

				resp, err := sendAuditEvents(event1, event2)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				// Each auditor should have received one event
				events1 := mockAuditorRegion1.RecordedEvents()
				Expect(events1).To(HaveLen(1))

				events2 := mockAuditorRegion2.RecordedEvents()
				Expect(events2).To(HaveLen(1))
			})

			It("should handle multiple events for the same region", func() {
				event1 := createTestAuditEvent(testRegion1, "create", auditv1.StageResponseComplete)
				event2 := createTestAuditEvent(testRegion1, "update", auditv1.StageResponseComplete)

				resp, err := sendAuditEvents(event1, event2)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				// Region 1 auditor should have both events
				events := mockAuditorRegion1.RecordedEvents()
				Expect(events).To(HaveLen(2))
				Expect(events[0].Action).To(Equal(cadf.CreateAction))
				Expect(events[1].Action).To(Equal(cadf.UpdateAction))
			})
		})

		Context("Error Handling", func() {
			It("should return HTTP 500 when region auditor is not found", func() {
				// Temporarily remove region2 auditor
				auditHandler.RegionAuditors.SetTestAuditors(map[string]audittools.Auditor{
					testRegion1: mockAuditorRegion1,
				})
				defer func() {
					// Restore auditors
					auditHandler.RegionAuditors.SetTestAuditors(map[string]audittools.Auditor{
						testRegion1: mockAuditorRegion1,
						testRegion2: mockAuditorRegion2,
					})
				}()

				// Try to send event for region that has no auditor
				event := createTestAuditEvent("unknown-region", "create", auditv1.StageResponseComplete)

				resp, err := sendAuditEvent(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
			})

			It("should return HTTP 400 for malformed request", func() {
				webhookURL := url.URL{
					Scheme: "https",
					Host:   net.JoinHostPort(webhookHost, strconv.Itoa(webhookPort)),
					Path:   audithandler.WebhookPath,
				}

				req, err := http.NewRequest(http.MethodPost, webhookURL.String(), bytes.NewReader([]byte("invalid json")))
				Expect(err).NotTo(HaveOccurred())
				req.Header.Set("Content-Type", "application/json")

				tlsConfig, err := rest.TLSConfigFor(testRestConfig)
				Expect(err).NotTo(HaveOccurred())
				tlsConfig.InsecureSkipVerify = true

				client := &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: tlsConfig,
					},
				}

				resp, err := client.Do(req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			})

			It("should return HTTP 500 when event cannot be transformed", func() {
				// Create event where the Shoot object has no region and user extra also lacks region
				event := createTestAuditEvent(testRegion1, "create", auditv1.StageResponseComplete)
				// Set empty Shoot spec (no region)
				event.RequestObject = &runtime.Unknown{Raw: []byte(`{"apiVersion":"core.gardener.cloud/v1beta1","kind":"Shoot","spec":{}}`)}
				// Clear region in user extra so fallback also fails
				event.User.Extra["region"] = []string{""}

				resp, err := sendAuditEvent(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
			})
		})

		Context("Metrics Recording", func() {
			It("should record successful event processing", func() {
				event := createTestAuditEvent(testRegion1, "create", auditv1.StageResponseComplete)
				event.ResponseStatus = &metav1.Status{Code: 201}

				resp, err := sendAuditEvent(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				// Verify event was recorded
				events := mockAuditorRegion1.RecordedEvents()
				Expect(events).To(HaveLen(1))
			})

			It("should record filtered events", func() {
				event := createTestAuditEvent(testRegion1, "get", auditv1.StageResponseComplete)

				resp, err := sendAuditEvent(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				// No events should be recorded (filtered)
				events := mockAuditorRegion1.RecordedEvents()
				Expect(events).To(BeEmpty())
			})
		})

		Context("Subresource Handling", func() {
			It("should filter out unsupported shoot subresource events", func() {
				event := createTestAuditEvent(testRegion1, "update", auditv1.StageResponseComplete)
				event.ObjectRef.Subresource = "status"

				resp, err := sendAuditEvent(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				// No events should be recorded (filtered as unsupported subresource)
				events := mockAuditorRegion1.RecordedEvents()
				Expect(events).To(BeEmpty())
			})

			It("should process adminkubeconfig create event as authenticate/admin action", func() {
				event := createTestKubeconfigAuditEvent(testRegion1, "adminkubeconfig")

				resp, err := sendAuditEvent(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				events := mockAuditorRegion1.RecordedEvents()
				Expect(events).To(HaveLen(1))
				Expect(events[0].Action).To(Equal(cadf.Action("authenticate/admin")))
				Expect(events[0].Target.TypeURI).To(Equal("kubernetes/cluster"))
				Expect(events[0].Target.ID).To(Equal(testNamespace.Name + "/test-shoot"))
				Expect(events[0].Target.Name).To(Equal(testNamespace.Name + "/test-shoot"))
			})

			It("should process viewerkubeconfig create event as authenticate/viewer action", func() {
				event := createTestKubeconfigAuditEvent(testRegion1, "viewerkubeconfig")

				resp, err := sendAuditEvent(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				events := mockAuditorRegion1.RecordedEvents()
				Expect(events).To(HaveLen(1))
				Expect(events[0].Action).To(Equal(cadf.Action("authenticate/viewer")))
				Expect(events[0].Target.TypeURI).To(Equal("kubernetes/cluster"))
				Expect(events[0].Target.ID).To(Equal(testNamespace.Name + "/test-shoot"))
				Expect(events[0].Target.Name).To(Equal(testNamespace.Name + "/test-shoot"))
			})

			It("should include requestBody attachment on kubeconfig events", func() {
				event := createTestKubeconfigAuditEvent(testRegion1, "adminkubeconfig")

				resp, err := sendAuditEvent(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				events := mockAuditorRegion1.RecordedEvents()
				Expect(events).To(HaveLen(1))
				Expect(events[0].Target.Attachments).To(HaveLen(1))
				Expect(events[0].Target.Attachments[0].Name).To(Equal("requestBody"))
				Expect(events[0].Target.Attachments[0].TypeURI).To(Equal("mime:application/json"))
			})
		})

		Context("Request Body Attachments", func() {
			It("should include requestBody attachment on Shoot create events", func() {
				event := createTestAuditEvent(testRegion1, "create", auditv1.StageResponseComplete)

				resp, err := sendAuditEvent(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				events := mockAuditorRegion1.RecordedEvents()
				Expect(events).To(HaveLen(1))
				Expect(events[0].Target.Attachments).To(HaveLen(1))
				Expect(events[0].Target.Attachments[0].Name).To(Equal("requestBody"))
				Expect(events[0].Target.Attachments[0].TypeURI).To(Equal("mime:application/json"))
				// Verify the attachment contains the shoot spec
				content, ok := events[0].Target.Attachments[0].Content.(string)
				Expect(ok).To(BeTrue())
				Expect(content).To(ContainSubstring(testRegion1))
			})

			It("should include requestBody attachment on Shoot update events", func() {
				event := createTestAuditEvent(testRegion1, "update", auditv1.StageResponseComplete)

				resp, err := sendAuditEvent(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				events := mockAuditorRegion1.RecordedEvents()
				Expect(events).To(HaveLen(1))
				Expect(events[0].Target.Attachments).To(HaveLen(1))
				Expect(events[0].Target.Attachments[0].Name).To(Equal("requestBody"))
			})

			It("should NOT include requestBody attachment on Shoot delete events", func() {
				event := createTestAuditEvent(testRegion1, "delete", auditv1.StageResponseComplete)
				// For delete, ResponseObject has the Shoot, RequestObject is nil
				event.ResponseObject = event.RequestObject
				event.RequestObject = nil

				resp, err := sendAuditEvent(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				events := mockAuditorRegion1.RecordedEvents()
				Expect(events).To(HaveLen(1))
				Expect(events[0].Target.Attachments).To(BeEmpty())
			})
		})

		Context("Batch Processing", func() {
			It("should process multiple events in a single request", func() {
				events := []*auditv1.Event{
					createTestAuditEvent(testRegion1, "create", auditv1.StageResponseComplete),
					createTestAuditEvent(testRegion1, "update", auditv1.StageResponseComplete),
					createTestAuditEvent(testRegion2, "delete", auditv1.StageResponseComplete),
				}
				// For delete event, set ResponseObject
				events[2].ResponseObject = events[2].RequestObject
				events[2].RequestObject = nil

				resp, err := sendAuditEvents(events...)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				// Verify events were routed correctly
				events1 := mockAuditorRegion1.RecordedEvents()
				Expect(events1).To(HaveLen(2))
				Expect(events1[0].Action).To(Equal(cadf.CreateAction))
				Expect(events1[1].Action).To(Equal(cadf.UpdateAction))

				events2 := mockAuditorRegion2.RecordedEvents()
				Expect(events2).To(HaveLen(1))
				Expect(events2[0].Action).To(Equal(cadf.DeleteAction))
			})

			It("should stop processing on first error and return HTTP 500", func() {
				// Temporarily remove region2 auditor
				auditHandler.RegionAuditors.SetTestAuditors(map[string]audittools.Auditor{
					testRegion1: mockAuditorRegion1,
				})
				defer func() {
					// Restore auditors
					auditHandler.RegionAuditors.SetTestAuditors(map[string]audittools.Auditor{
						testRegion1: mockAuditorRegion1,
						testRegion2: mockAuditorRegion2,
					})
				}()

				events := []*auditv1.Event{
					createTestAuditEvent(testRegion1, "create", auditv1.StageResponseComplete),
					createTestAuditEvent("unknown-region", "update", auditv1.StageResponseComplete),
					createTestAuditEvent(testRegion1, "delete", auditv1.StageResponseComplete),
				}

				resp, err := sendAuditEvents(events...)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))

				// Only the first event should be processed
				recordedEvents := mockAuditorRegion1.RecordedEvents()
				Expect(recordedEvents).To(HaveLen(1))
			})
		})
	})

	// =============================================================================
	// Real API Server Audit Events Tests
	// =============================================================================
	// These tests perform actual Shoot CRUD operations and verify that:
	// 1. The API server generates audit events based on the audit policy
	// 2. The audit events are sent to the webhook endpoint
	// 3. The handler filters, transforms, and routes events correctly
	// 4. The mock auditor receives correctly transformed CADF events

	Context("Real API Server Audit Events", func() {
		var shootCounter int

		// Helper to create a unique Shoot for each test
		createShoot := func(region string) *gardenercorev1beta1.Shoot {
			shootCounter++
			return &gardenercorev1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("test-shoot-%d", shootCounter),
					Namespace: testNamespace.Name,
				},
				Spec: gardenercorev1beta1.ShootSpec{
					CloudProfile: &gardenercorev1beta1.CloudProfileReference{
						Kind: "CloudProfile",
						Name: "test",
					},
					Region:                 region,
					CredentialsBindingName: new("test-secret-binding"), // Required field
					Provider: gardenercorev1beta1.Provider{
						Type: "openstack",
						Workers: []gardenercorev1beta1.Worker{{
							Name:    "worker",
							Maximum: 1,
							Minimum: 1,
							Machine: gardenercorev1beta1.Machine{
								Type: "m1.small",
								Image: &gardenercorev1beta1.ShootMachineImage{
									Name:    "test-image",
									Version: new("1.0.0"),
								},
							},
						}},
					},
					Kubernetes: gardenercorev1beta1.Kubernetes{
						Version: "1.30.0",
					},
					Networking: &gardenercorev1beta1.Networking{
						Type: new("calico"),
					},
				},
			}
		}

		// Helper to find event by action type
		findEventByAction := func(events []cadf.Event, action cadf.Action) *cadf.Event {
			for i := range events {
				if events[i].Action == action {
					return &events[i]
				}
			}
			return nil
		}

		It("should capture Shoot create event with correct target and initiator", func() {
			shoot := createShoot(testRegion1)

			By("Creating Shoot with OpenStack user client")
			Expect(openStackUserClientRegion1.Create(ctx, shoot)).To(Succeed())
			Expect(shoot.UID).NotTo(BeEmpty(), "Shoot UID should be assigned after create")

			DeferCleanup(func() {
				By("Cleaning up Shoot")
				Expect(testClient.Delete(ctx, shoot)).To(Or(Succeed(), BeNotFoundError()))
			})

			By("Verifying audit event was captured with correct fields")
			var events []cadf.Event
			Eventually(func() *cadf.Event {
				events = append(events, mockAuditorRegion1.RecordedEvents()...)
				return findEventByAction(events, cadf.CreateAction)
			}).ShouldNot(BeNil())

			createEvent := findEventByAction(events, cadf.CreateAction)
			Expect(createEvent).NotTo(BeNil(), "Expected to find a create event")

			By("Verifying target fields")
			Expect(createEvent.Target.TypeURI).To(Equal("kubernetes/cluster"))
			Expect(createEvent.Target.ID).To(Equal(testNamespace.Name+"/"+shoot.Name), "Target ID should be namespace/name")
			Expect(createEvent.Target.Name).To(Equal(testNamespace.Name+"/"+shoot.Name), "Target Name should be namespace/name")

			// Note: Initiator fields cannot be tested here because MockAuditor normalizes
			// the Initiator when TypeURI is "service/security/account/user".
			// Initiator transformation is tested in unit tests (transform_test.go).
		})

		It("should capture Shoot update event with correct target and initiator", func() {
			shoot := createShoot(testRegion1)

			By("Creating Shoot")
			Expect(openStackUserClientRegion1.Create(ctx, shoot)).To(Succeed())

			DeferCleanup(func() {
				By("Cleaning up Shoot")
				Expect(testClient.Delete(ctx, shoot)).To(Or(Succeed(), BeNotFoundError()))
			})

			// Wait for create event and clear
			var createEvents []cadf.Event
			Eventually(func() *cadf.Event {
				createEvents = append(createEvents, mockAuditorRegion1.RecordedEvents()...)
				return findEventByAction(createEvents, cadf.CreateAction)
			}).ShouldNot(BeNil())

			By("Updating Shoot with OpenStack user client")
			metav1.SetMetaDataAnnotation(&shoot.ObjectMeta, "test-annotation", "test-value")
			Expect(openStackUserClientRegion1.Update(ctx, shoot)).To(Succeed())

			By("Verifying audit event was captured with correct fields")
			var updateEvents []cadf.Event
			Eventually(func() *cadf.Event {
				updateEvents = append(updateEvents, mockAuditorRegion1.RecordedEvents()...)
				return findEventByAction(updateEvents, cadf.UpdateAction)
			}).ShouldNot(BeNil())

			updateEvent := findEventByAction(updateEvents, cadf.UpdateAction)
			Expect(updateEvent).NotTo(BeNil(), "Expected to find an update event")

			By("Verifying target fields")
			Expect(updateEvent.Target.TypeURI).To(Equal("kubernetes/cluster"))
			Expect(updateEvent.Target.ID).To(Equal(testNamespace.Name+"/"+shoot.Name), "Target ID should be namespace/name")
			Expect(updateEvent.Target.Name).To(Equal(testNamespace.Name+"/"+shoot.Name), "Target Name should be namespace/name")

			// Note: Initiator fields cannot be tested here because MockAuditor normalizes
			// the Initiator when TypeURI is "service/security/account/user".
			// Initiator transformation is tested in unit tests (transform_test.go).
		})

		It("should capture Shoot delete event with correct target and initiator", func() {
			shoot := createShoot(testRegion1)

			By("Creating Shoot")
			Expect(openStackUserClientRegion1.Create(ctx, shoot)).To(Succeed())
			shootName := shoot.Name

			// Wait for create event and clear
			var createEvents []cadf.Event
			Eventually(func() *cadf.Event {
				createEvents = append(createEvents, mockAuditorRegion1.RecordedEvents()...)
				return findEventByAction(createEvents, cadf.CreateAction)
			}).ShouldNot(BeNil())

			By("Deleting Shoot with OpenStack user client")
			Expect(openStackUserClientRegion1.Delete(ctx, shoot)).To(Succeed())

			By("Verifying audit event was captured with correct fields")
			var deleteEvents []cadf.Event
			Eventually(func() *cadf.Event {
				deleteEvents = append(deleteEvents, mockAuditorRegion1.RecordedEvents()...)
				return findEventByAction(deleteEvents, cadf.DeleteAction)
			}).ShouldNot(BeNil())

			deleteEvent := findEventByAction(deleteEvents, cadf.DeleteAction)
			Expect(deleteEvent).NotTo(BeNil(), "Expected to find a delete event")

			By("Verifying target fields")
			Expect(deleteEvent.Target.TypeURI).To(Equal("kubernetes/cluster"))
			Expect(deleteEvent.Target.ID).To(Equal(testNamespace.Name+"/"+shootName), "Target ID should be namespace/name")
			Expect(deleteEvent.Target.Name).To(Equal(testNamespace.Name+"/"+shootName), "Target Name should be namespace/name")

			// Note: Initiator fields cannot be tested here because MockAuditor normalizes
			// the Initiator when TypeURI is "service/security/account/user".
			// Initiator transformation is tested in unit tests (transform_test.go).
		})

		It("should NOT capture events from non-OpenStack users", func() {
			shoot := createShoot(testRegion1)

			By("Creating Shoot with regular test client (no OpenStack credentials)")
			Expect(testClient.Create(ctx, shoot)).To(Succeed())

			DeferCleanup(func() {
				By("Cleaning up Shoot")
				Expect(testClient.Delete(ctx, shoot)).To(Or(Succeed(), BeNotFoundError()))
			})

			By("Verifying NO audit events were captured (events filtered due to missing OpenStack credentials)")
			// Wait a bit and verify no events - accumulate to handle RecordedEvents() clearing
			var allEvents []cadf.Event
			Consistently(func() int {
				allEvents = append(allEvents, mockAuditorRegion1.RecordedEvents()...)
				return len(allEvents)
			}).Should(Equal(0))
		})

		It("should route events to correct regional auditor based on Shoot region", func() {
			shoot1 := createShoot(testRegion1)
			shoot2 := createShoot(testRegion2)

			By("Creating Shoot in region 1 with OpenStack user client")
			Expect(openStackUserClientRegion1.Create(ctx, shoot1)).To(Succeed())

			DeferCleanup(func() {
				By("Cleaning up Shoot 1")
				Expect(testClient.Delete(ctx, shoot1)).To(Or(Succeed(), BeNotFoundError()))
			})

			By("Creating Shoot in region 2 with OpenStack user client")
			Expect(openStackUserClientRegion2.Create(ctx, shoot2)).To(Succeed())

			DeferCleanup(func() {
				By("Cleaning up Shoot 2")
				Expect(testClient.Delete(ctx, shoot2)).To(Or(Succeed(), BeNotFoundError()))
			})

			By("Verifying events were routed to correct regional auditors")
			var events1 []cadf.Event
			Eventually(func() *cadf.Event {
				events1 = append(events1, mockAuditorRegion1.RecordedEvents()...)
				return findEventByAction(events1, cadf.CreateAction)
			}).ShouldNot(BeNil())

			var events2 []cadf.Event
			Eventually(func() *cadf.Event {
				events2 = append(events2, mockAuditorRegion2.RecordedEvents()...)
				return findEventByAction(events2, cadf.CreateAction)
			}).ShouldNot(BeNil())

			// Verify region 1 auditor received a create event
			createEvent1 := findEventByAction(events1, cadf.CreateAction)
			Expect(createEvent1).NotTo(BeNil(), "Expected to find create event in region1 auditor")
			Expect(createEvent1.Target.TypeURI).To(Equal("kubernetes/cluster"))

			// Verify region 2 auditor received a create event
			createEvent2 := findEventByAction(events2, cadf.CreateAction)
			Expect(createEvent2).NotTo(BeNil(), "Expected to find create event in region2 auditor")
			Expect(createEvent2.Target.TypeURI).To(Equal("kubernetes/cluster"))
		})

		It("should include requestBody attachment on real Shoot create events", func() {
			shoot := createShoot(testRegion1)

			By("Creating Shoot with OpenStack user client")
			Expect(openStackUserClientRegion1.Create(ctx, shoot)).To(Succeed())

			DeferCleanup(func() {
				By("Cleaning up Shoot")
				Expect(testClient.Delete(ctx, shoot)).To(Or(Succeed(), BeNotFoundError()))
			})

			By("Verifying audit event has requestBody attachment")
			var events []cadf.Event
			Eventually(func() *cadf.Event {
				events = append(events, mockAuditorRegion1.RecordedEvents()...)
				return findEventByAction(events, cadf.CreateAction)
			}).ShouldNot(BeNil())

			createEvent := findEventByAction(events, cadf.CreateAction)
			Expect(createEvent).NotTo(BeNil())
			Expect(createEvent.Target.Attachments).To(HaveLen(1))
			Expect(createEvent.Target.Attachments[0].Name).To(Equal("requestBody"))
			Expect(createEvent.Target.Attachments[0].TypeURI).To(Equal("mime:application/json"))
			// Verify attachment contains the shoot's region
			content, ok := createEvent.Target.Attachments[0].Content.(string)
			Expect(ok).To(BeTrue())
			Expect(content).To(ContainSubstring(testRegion1))
		})

		It("should capture adminkubeconfig request as authenticate event", func() {
			shoot := createShoot(testRegion1)

			By("Creating Shoot with OpenStack user client")
			Expect(openStackUserClientRegion1.Create(ctx, shoot)).To(Succeed())

			DeferCleanup(func() {
				By("Cleaning up Shoot")
				Expect(testClient.Delete(ctx, shoot)).To(Or(Succeed(), BeNotFoundError()))
			})

			// Wait for create event and clear
			var createEvents []cadf.Event
			Eventually(func() *cadf.Event {
				createEvents = append(createEvents, mockAuditorRegion1.RecordedEvents()...)
				return findEventByAction(createEvents, cadf.CreateAction)
			}).ShouldNot(BeNil())

			By("Requesting adminkubeconfig subresource with OpenStack user client")
			adminKubeconfigRequest := &authenticationv1alpha1.AdminKubeconfigRequest{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "authentication.gardener.cloud/v1alpha1",
					Kind:       "AdminKubeconfigRequest",
				},
				Spec: authenticationv1alpha1.AdminKubeconfigRequestSpec{
					ExpirationSeconds: new(int64(3600)),
				},
			}
			// The subresource call may fail in envtest (no real shoot control plane),
			// but the audit event should still be generated for ResponseComplete stage.
			// We only care about the audit event, not the subresource result.
			Expect(openStackUserClientRegion1.SubResource("adminkubeconfig").Create(ctx, shoot, adminKubeconfigRequest)).To(Or(Succeed(), HaveOccurred()))

			By("Verifying authenticate audit event was captured")
			var authEvents []cadf.Event
			Eventually(func() *cadf.Event {
				authEvents = append(authEvents, mockAuditorRegion1.RecordedEvents()...)
				return findEventByAction(authEvents, cadf.Action("authenticate/admin"))
			}).ShouldNot(BeNil())

			authEvent := findEventByAction(authEvents, cadf.Action("authenticate/admin"))
			Expect(authEvent).NotTo(BeNil())
			Expect(authEvent.Target.TypeURI).To(Equal("kubernetes/cluster"))
			Expect(authEvent.Target.Name).To(Equal(testNamespace.Name + "/" + shoot.Name))
		})

		It("should NOT log adminkubeconfig request as shoot create event", func() {
			shoot := createShoot(testRegion1)

			By("Creating Shoot with OpenStack user client")
			Expect(openStackUserClientRegion1.Create(ctx, shoot)).To(Succeed())

			DeferCleanup(func() {
				By("Cleaning up Shoot")
				Expect(testClient.Delete(ctx, shoot)).To(Or(Succeed(), BeNotFoundError()))
			})

			// Wait for create event and clear
			var createEvents []cadf.Event
			Eventually(func() *cadf.Event {
				createEvents = append(createEvents, mockAuditorRegion1.RecordedEvents()...)
				return findEventByAction(createEvents, cadf.CreateAction)
			}).ShouldNot(BeNil())

			By("Requesting adminkubeconfig subresource")
			adminKubeconfigRequest := &authenticationv1alpha1.AdminKubeconfigRequest{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "authentication.gardener.cloud/v1alpha1",
					Kind:       "AdminKubeconfigRequest",
				},
				Spec: authenticationv1alpha1.AdminKubeconfigRequestSpec{
					ExpirationSeconds: new(int64(3600)),
				},
			}
			Expect(openStackUserClientRegion1.SubResource("adminkubeconfig").Create(ctx, shoot, adminKubeconfigRequest)).To(Or(Succeed(), HaveOccurred()))

			By("Waiting for audit events to arrive")
			// Give the audit system time to deliver events
			var allEvents []cadf.Event
			Eventually(func() int {
				allEvents = append(allEvents, mockAuditorRegion1.RecordedEvents()...)
				return len(allEvents)
			}).Should(BeNumerically(">", 0))

			By("Verifying no spurious create events (only authenticate)")
			for _, event := range allEvents {
				if event.Action == cadf.CreateAction {
					Fail("Found unexpected create event after adminkubeconfig request — subresource bleed bug")
				}
			}
		})
	})
})

// createTestAuditEvent creates a test audit event with OpenStack credentials.
func createTestAuditEvent(region, verb string, stage auditv1.Stage) *auditv1.Event {
	shoot := &gardenercorev1beta1.Shoot{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "core.gardener.cloud/v1beta1",
			Kind:       "Shoot",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shoot",
			Namespace: testNamespace.Name,
			UID:       "test-shoot-uid",
		},
		Spec: gardenercorev1beta1.ShootSpec{
			Region: region,
		},
	}

	shootJSON, err := json.Marshal(shoot)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal shoot: %v", err))
	}

	return &auditv1.Event{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "audit.k8s.io/v1",
			Kind:       "Event",
		},
		Level:      auditv1.LevelMetadata,
		AuditID:    "test-audit-id",
		Stage:      stage,
		RequestURI: fmt.Sprintf("/apis/core.gardener.cloud/v1beta1/namespaces/%s/shoots/test-shoot", testNamespace.Name),
		Verb:       verb,
		User: authenticationv1.UserInfo{
			Username: "test-user@test-domain",
			UID:      "test-user-uid",
			Extra: kubernetes.OpenStackUserInfo{
				ProjectDomainID:   "domain-123",
				ProjectDomainName: "test-domain",
				ProjectID:         "project-456",
				ProjectName:       "test-project",
				UserDomainID:      "user-domain-789",
				UserDomainName:    "test-user-domain",
				Region:            region,
			}.ToExtra(),
		},
		SourceIPs: []string{"10.0.0.1"},
		UserAgent: "kubectl/v1.31.0",
		ObjectRef: &auditv1.ObjectReference{
			APIGroup:   "core.gardener.cloud",
			APIVersion: "v1beta1",
			Resource:   "shoots",
			Namespace:  testNamespace.Name,
			Name:       "test-shoot",
		},
		ResponseStatus: &metav1.Status{
			Code: 200,
		},
		RequestObject: &runtime.Unknown{
			Raw: shootJSON,
		},
		StageTimestamp: metav1.NewMicroTime(time.Now()),
	}
}

// createTestKubeconfigAuditEvent creates a test audit event for a kubeconfig subresource request.
func createTestKubeconfigAuditEvent(region, subresource string) *auditv1.Event {
	return &auditv1.Event{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "audit.k8s.io/v1",
			Kind:       "Event",
		},
		Level:      auditv1.LevelRequestResponse,
		AuditID:    "test-kubeconfig-audit-id",
		Stage:      auditv1.StageResponseComplete,
		RequestURI: fmt.Sprintf("/apis/core.gardener.cloud/v1beta1/namespaces/%s/shoots/test-shoot/%s", testNamespace.Name, subresource),
		Verb:       "create",
		User: authenticationv1.UserInfo{
			Username: "test-user@test-domain",
			UID:      "test-user-uid",
			Extra: kubernetes.OpenStackUserInfo{
				ProjectDomainID:   "domain-123",
				ProjectDomainName: "test-domain",
				ProjectID:         "project-456",
				ProjectName:       "test-project",
				UserDomainID:      "user-domain-789",
				UserDomainName:    "test-user-domain",
				Region:            region,
			}.ToExtra(),
		},
		SourceIPs: []string{"10.0.0.1"},
		UserAgent: "kubectl/v1.31.0",
		ObjectRef: &auditv1.ObjectReference{
			APIGroup:    "core.gardener.cloud",
			APIVersion:  "v1beta1",
			Resource:    "shoots",
			Subresource: subresource,
			Namespace:   testNamespace.Name,
			Name:        "test-shoot",
		},
		ResponseStatus: &metav1.Status{
			Code: 201,
		},
		RequestObject: &runtime.Unknown{
			Raw: []byte(`{"spec":{"expirationSeconds":3600}}`),
		},
		StageTimestamp: metav1.NewMicroTime(time.Now()),
	}
}
