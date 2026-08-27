// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
// SPDX-License-Identifier: Apache-2.0

package shoot_test

import (
	"fmt"
	"net/http"
	"time"

	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	"github.com/gardener/gardener/pkg/controllerutils"
	. "github.com/gardener/gardener/pkg/utils/test/matchers"
	"github.com/gophercloud/gophercloud/v2"
	th "github.com/gophercloud/gophercloud/v2/testhelper"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Shoot Controller Integration Tests", func() {
	Context("when Shoot is in the garden namespace", func() {
		It("should not reconcile the Shoot", func() {
			gardenShoot := &gardenercorev1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "managed-seed-shoot",
					Namespace: v1beta1constants.GardenNamespace,
					Labels:    map[string]string{testID: testRunID},
				},
				Spec: gardenercorev1beta1.ShootSpec{
					CloudProfile:           &gardenercorev1beta1.CloudProfileReference{Kind: "CloudProfile", Name: "test"},
					Region:                 "test-region",
					Provider:               gardenercorev1beta1.Provider{Type: "test", Workers: []gardenercorev1beta1.Worker{{Name: "worker", Minimum: 1, Maximum: 1, Machine: gardenercorev1beta1.Machine{Type: "test", Image: &gardenercorev1beta1.ShootMachineImage{Name: "test", Version: new("1.2.3")}}}}},
					Kubernetes:             gardenercorev1beta1.Kubernetes{Version: "1.30.0"},
					Networking:             &gardenercorev1beta1.Networking{Type: new("test")},
					CredentialsBindingName: new("some-credentials"),
				},
			}

			By("Create garden namespace")
			gardenNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: v1beta1constants.GardenNamespace}}
			Expect(testClient.Create(ctx, gardenNs)).To(Or(Succeed(), BeAlreadyExistsError()))

			By("Create Shoot in garden namespace")
			Expect(testClient.Create(ctx, gardenShoot)).To(Succeed())
			DeferCleanup(func() {
				Expect(testClient.Delete(ctx, gardenShoot)).To(Or(Succeed(), BeNotFoundError()))
			})

			By("Verify the controller does not add a finalizer")
			Consistently(func(g Gomega) []string {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(gardenShoot), gardenShoot)).To(Succeed())
				return gardenShoot.Finalizers
			}).ShouldNot(ContainElement("persephone.sci.cloud.sap/shoot-controller"))
		})
	})

	var (
		shoot               *gardenercorev1beta1.Shoot
		oldInternalSecret   *gardenercorev1beta1.InternalSecret
		newerInternalSecret *gardenercorev1beta1.InternalSecret
	)

	BeforeEach(func() {
		shoot = &gardenercorev1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-shoot",
				Namespace: testNamespace.Name,
				Labels:    map[string]string{testID: testRunID},
			},
			Spec: gardenercorev1beta1.ShootSpec{
				CloudProfile:           &gardenercorev1beta1.CloudProfileReference{Kind: "CloudProfile", Name: "test"},
				Region:                 "test-region",
				Provider:               gardenercorev1beta1.Provider{Type: "test", Workers: []gardenercorev1beta1.Worker{{Name: "worker", Minimum: 1, Maximum: 1, Machine: gardenercorev1beta1.Machine{Type: "test", Image: &gardenercorev1beta1.ShootMachineImage{Name: "test", Version: new("1.2.3")}}}}},
				Kubernetes:             gardenercorev1beta1.Kubernetes{Version: "1.30.0"},
				Networking:             &gardenercorev1beta1.Networking{Type: new("test")},
				CredentialsBindingName: new("old-credentials"),
			},
		}

		oldInternalSecret = &gardenercorev1beta1.InternalSecret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "old-credentials",
				Namespace: testNamespace.Name,
				Labels:    map[string]string{"persephone.sci.cloud.sap/shoot-name": shoot.Name},
			},
			Data: map[string][]byte{"key": []byte("old-value")},
		}

		newerInternalSecret = &gardenercorev1beta1.InternalSecret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "newer-credentials",
				Namespace: testNamespace.Name,
				Labels:    map[string]string{"persephone.sci.cloud.sap/shoot-name": shoot.Name},
			},
			Data: map[string][]byte{"key": []byte("newer-value")},
		}

		DeferCleanup(func() {
			Expect(testClient.Delete(ctx, shoot)).To(Or(Succeed(), BeNotFoundError()))
			Expect(testClient.Delete(ctx, oldInternalSecret)).To(Or(Succeed(), BeNotFoundError()))
			Expect(testClient.Delete(ctx, newerInternalSecret)).To(Or(Succeed(), BeNotFoundError()))

			// Teardown must be inert with respect to the controller under test:
			// do not exercise the delete() guard (a real gardenlet would set
			// Status.LastOperation to Delete/Succeeded — envtest has none), and
			// do not rely on the controller to remove its finalizer. Strip all
			// finalizers directly so the API server can garbage-collect the
			// shoot. Any test that wants to *exercise* the guard drives it
			// explicitly in the It block (see "when Shoot is deleted").
			Eventually(func(g Gomega) {
				err := testClient.Get(ctx, client.ObjectKeyFromObject(shoot), shoot)
				if err != nil {
					g.Expect(err).To(BeNotFoundError())
					return
				}
				g.Expect(controllerutils.RemoveAllFinalizers(ctx, testClient, shoot)).To(Succeed())
			}).Should(Succeed())

			Eventually(func() error { return testClient.Get(ctx, client.ObjectKeyFromObject(shoot), shoot) }).Should(BeNotFoundError())
		})
	})

	When("Shoot already references the newest InternalSecret", func() {
		It("should do nothing", func() {
			By("Create old InternalSecret")
			Expect(testClient.Create(ctx, oldInternalSecret)).To(Succeed())

			By("Create Shoot referencing the only InternalSecret")
			Expect(testClient.Create(ctx, shoot)).To(Succeed())

			By("Verify finalizer is added")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(shoot), shoot)).To(Succeed())
				g.Expect(shoot.Finalizers).To(ContainElement("persephone.sci.cloud.sap/shoot-controller"))
			}).Should(Succeed())

			By("Verify credentials binding name is not changed")
			Consistently(func(g Gomega) *string {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(shoot), shoot)).To(Succeed())
				return shoot.Spec.CredentialsBindingName
			}).Should(PointTo(Equal("old-credentials")))
		})
	})

	When("a newer InternalSecret exists", func() {
		BeforeEach(func() {
			By("Create old InternalSecret")
			Expect(testClient.Create(ctx, oldInternalSecret)).To(Succeed())

			By("Create Shoot")
			Expect(testClient.Create(ctx, shoot)).To(Succeed())

			By("Wait for controller to process")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(shoot), shoot)).To(Succeed())
				g.Expect(shoot.Finalizers).To(ContainElement("persephone.sci.cloud.sap/shoot-controller"))
			}).Should(Succeed())

			By("Create newer InternalSecret")
			time.Sleep(100 * time.Millisecond) // Sleep to ensure newer InternalSecret has a later creationTimestamp
			Expect(testClient.Create(ctx, newerInternalSecret)).To(Succeed())

			By("Wait until manager client observes the newer secret") // needed because mgr's client is cached
			Eventually(func(g Gomega) error {
				return mgrClient.Get(ctx, client.ObjectKeyFromObject(newerInternalSecret), newerInternalSecret)
			}).Should(Succeed())
		})

		Context("with force update annotation", func() {
			It("should update credentials binding immediately", func() {
				By("Add force update annotation")
				patch := client.MergeFrom(shoot.DeepCopy())
				metav1.SetMetaDataAnnotation(&shoot.ObjectMeta, "shoot.persephone.sci.cloud.sap/force-credentials-binding-update", "true")
				Expect(testClient.Patch(ctx, shoot, patch)).To(Succeed())

				By("Verify credentials binding is updated and annotation is removed")
				Eventually(func(g Gomega) *string {
					g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(shoot), shoot)).To(Succeed())
					g.Expect(shoot.Annotations).NotTo(HaveKey("shoot.persephone.sci.cloud.sap/force-credentials-binding-update"))
					return shoot.Spec.CredentialsBindingName
				}).Should(PointTo(Equal("newer-credentials")))
			})
		})

		Context("with confineSpecUpdateRollout=true", func() {
			It("should update credentials binding immediately", func() {
				By("Update Shoot with confineSpecUpdateRollout=true")
				patch := client.MergeFrom(shoot.DeepCopy())
				shoot.Spec.Maintenance = &gardenercorev1beta1.Maintenance{
					TimeWindow: &gardenercorev1beta1.MaintenanceTimeWindow{
						Begin: "220000+0000",
						End:   "230000+0000",
					},
					ConfineSpecUpdateRollout: new(true),
				}
				Expect(testClient.Patch(ctx, shoot, patch)).To(Succeed())

				By("Verify credentials binding is updated")
				Eventually(func(g Gomega) *string {
					g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(shoot), shoot)).To(Succeed())
					return shoot.Spec.CredentialsBindingName
				}).Should(PointTo(Equal("newer-credentials")))
			})
		})

		Context("with maintenance window outside current time", func() {
			It("should not update credentials binding immediately", func() {
				By("Update Shoot with future maintenance window")
				patch := client.MergeFrom(shoot.DeepCopy())
				shoot.Spec.Maintenance = &gardenercorev1beta1.Maintenance{
					TimeWindow: &gardenercorev1beta1.MaintenanceTimeWindow{
						Begin: "220000+0000",
						End:   "230000+0000",
					},
				}
				Expect(testClient.Patch(ctx, shoot, patch)).To(Succeed())

				By("Verify credentials binding is not updated")
				Consistently(func(g Gomega) *string {
					g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(shoot), shoot)).To(Succeed())
					return shoot.Spec.CredentialsBindingName
				}).Should(PointTo(Equal("old-credentials")))
			})
		})
	})

	Context("when Shoot is deleted", func() {
		var internalSecret1, internalSecret2 *gardenercorev1beta1.InternalSecret

		BeforeEach(func() {
			internalSecret1 = &gardenercorev1beta1.InternalSecret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "internal-secret-1",
					Namespace: testNamespace.Name,
					Labels:    map[string]string{"persephone.sci.cloud.sap/shoot-name": shoot.Name},
				},
				Data: map[string][]byte{"key": []byte("value1")},
			}
			internalSecret2 = &gardenercorev1beta1.InternalSecret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "internal-secret-2",
					Namespace: testNamespace.Name,
					Labels:    map[string]string{"persephone.sci.cloud.sap/shoot-name": shoot.Name},
				},
				Data: map[string][]byte{"key": []byte("value2")},
			}

			By("Create InternalSecrets")
			Expect(testClient.Create(ctx, internalSecret1)).To(Succeed())
			Expect(testClient.Create(ctx, internalSecret2)).To(Succeed())

			By("Create Shoot")
			shoot.Spec.CredentialsBindingName = new(internalSecret1.Name)
			Expect(testClient.Create(ctx, shoot)).To(Succeed())
			shoot.Status.LastOperation = &gardenercorev1beta1.LastOperation{
				Type:  gardenercorev1beta1.LastOperationTypeReconcile,
				State: gardenercorev1beta1.LastOperationStateProcessing,
			}
			Expect(testClient.Status().Update(ctx, shoot)).To(Succeed())

			By("Wait for finalizer")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(shoot), shoot)).To(Succeed())
				g.Expect(shoot.Finalizers).To(ContainElement("persephone.sci.cloud.sap/shoot-controller"))
			}).Should(Succeed())
		})

		It("should wait for successful deletion before cleanup", func() {
			By("Delete Shoot without successful deletion status")
			Expect(testClient.Delete(ctx, shoot)).To(Succeed())

			By("Verify InternalSecrets are not deleted yet")
			Consistently(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(internalSecret1), internalSecret1)).To(Succeed())
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(internalSecret2), internalSecret2)).To(Succeed())
			}).Should(Succeed())

			By("Update Shoot status to successful deletion")
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(shoot), shoot)).To(Succeed())
			shoot.Status.LastOperation = &gardenercorev1beta1.LastOperation{
				Type:  gardenercorev1beta1.LastOperationTypeDelete,
				State: gardenercorev1beta1.LastOperationStateSucceeded,
			}
			Expect(testClient.Status().Update(ctx, shoot)).To(Succeed())

			By("Verify InternalSecrets are deleted")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(internalSecret1), internalSecret1)).To(BeNotFoundError())
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(internalSecret2), internalSecret2)).To(BeNotFoundError())
			}).Should(Succeed())

			By("Verify Shoot finalizer is removed and Shoot is deleted")
			Eventually(func(g Gomega) error {
				return testClient.Get(ctx, client.ObjectKeyFromObject(shoot), shoot)
			}).Should(BeNotFoundError())
		})

		It("should handle case when InternalSecrets are already deleted", func() {
			By("Delete InternalSecrets manually")
			Expect(testClient.Delete(ctx, internalSecret1)).To(Succeed())
			Expect(testClient.Delete(ctx, internalSecret2)).To(Succeed())
			Eventually(func() error {
				return testClient.Get(ctx, client.ObjectKeyFromObject(internalSecret1), internalSecret1)
			}).Should(BeNotFoundError())

			By("Mark Shoot with successful deletion status")
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(shoot), shoot)).To(Succeed())
			shoot.Status.LastOperation = &gardenercorev1beta1.LastOperation{
				Type:  gardenercorev1beta1.LastOperationTypeDelete,
				State: gardenercorev1beta1.LastOperationStateSucceeded,
			}
			Expect(testClient.Status().Update(ctx, shoot)).To(Succeed())
			Expect(testClient.Delete(ctx, shoot)).To(Succeed())

			By("Verify Shoot finalizer is removed and Shoot is deleted")
			Eventually(func(g Gomega) error {
				return testClient.Get(ctx, client.ObjectKeyFromObject(shoot), shoot)
			}).Should(BeNotFoundError())
		})
	})

	Context("when no InternalSecrets exist for Shoot", func() {
		It("should not fail reconciliation", func() {
			By("Create Shoot without any InternalSecrets")
			shoot.Spec.CredentialsBindingName = new("some-value")
			Expect(testClient.Create(ctx, shoot)).To(Succeed())

			By("Verify finalizer is added")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(shoot), shoot)).To(Succeed())
				g.Expect(shoot.Finalizers).To(ContainElement("persephone.sci.cloud.sap/shoot-controller"))
			}).Should(Succeed())

			By("Verify credentials binding name remains untouched")
			Consistently(func(g Gomega) *string {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(shoot), shoot)).To(Succeed())
				return shoot.Spec.CredentialsBindingName
			}).Should(PointTo(Equal("some-value")))
		})
	})
})

// newFakeIdentityClient creates a fake OpenStack identity service client for testing.
func newFakeIdentityClient() *gophercloud.ServiceClient {
	fakeServer := th.SetupHTTP()

	// Handle GET requests - list users by name
	fakeServer.Mux.HandleFunc("/v3/users", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			return
		}

		// Check if this is a user list request with name filter
		userName := r.URL.Query().Get("name")
		if userName != "" {
			// Return user if name matches expected cluster user pattern
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{
  "users": [
    {
      "id": "fake-cluster-user-id",
      "name": "`+userName+`",
      "domain_id": "domain-id",
      "default_project_id": "project-id",
      "enabled": true,
      "description": "Gardener customer shoot service user"
    }
  ]
}`)
			return
		}

		// Empty list if no name filter
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"users": []}`)
	})

	// Handle GET requests - list application credentials
	fakeServer.Mux.HandleFunc("GET /v3/users/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{
  "links": {
    "self": "`+fakeServer.Endpoint()+`v3/users/fake-user-id/application_credentials",
    "previous": null,
    "next": null
  },
  "application_credentials": [
    {
      "id": "fake-app-cred-id",
      "name": "test-shoot-project-id-credential",
      "description": "Application credential for shoot",
      "roles": [
        {
          "id": "role-id-1",
          "name": "member"
        }
      ],
      "expires_at": "2030-12-31T23:59:59.000000",
      "project_id": "project-id",
      "unrestricted": false,
      "links": {
        "self": "`+fakeServer.Endpoint()+`v3/users/fake-user-id/application_credentials/fake-app-cred-id"
      }
    }
  ]
}`)
	})

	// Handle POST requests - create application credential
	fakeServer.Mux.HandleFunc("POST /v3/users/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{
  "application_credential": {
    "id": "fake-new-app-cred-id",
    "name": "shoot-timestamp",
    "description": null,
    "roles": [
      {
        "id": "role-id-1",
        "name": "member"
      }
    ],
    "expires_at": "2030-12-31T23:59:59.000000",
    "secret": "fake-secret-value",
    "project_id": "project-id",
    "unrestricted": false,
    "links": {
      "self": "`+fakeServer.Endpoint()+`v3/users/fake-user-id/application_credentials/fake-new-app-cred-id"
    }
  }
}`)
	})

	// Handle DELETE requests - delete application credential
	fakeServer.Mux.HandleFunc("DELETE /v3/users/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// Handle DELETE requests - delete user
	fakeServer.Mux.HandleFunc("/v3/users/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
		}
	})

	return &gophercloud.ServiceClient{
		ProviderClient: &gophercloud.ProviderClient{},
		Endpoint:       fakeServer.Endpoint() + "v3/",
	}
}
