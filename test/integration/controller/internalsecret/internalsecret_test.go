// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
// SPDX-License-Identifier: Apache-2.0

package internalsecret_test

import (
	"time"

	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	gardenersecurityv1alpha1 "github.com/gardener/gardener/pkg/apis/security/v1alpha1"
	"github.com/gardener/gardener/pkg/controllerutils"
	"github.com/gardener/gardener/pkg/utils/test"
	. "github.com/gardener/gardener/pkg/utils/test/matchers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sap-cloud-infrastructure/persephone/internal/constants"
	"github.com/sap-cloud-infrastructure/persephone/internal/controller/internalsecret"
)

var _ = Describe("InternalSecret Controller Integration Tests", func() {
	var (
		shoot              *gardenercorev1beta1.Shoot
		testInternalSecret *gardenercorev1beta1.InternalSecret
	)

	BeforeEach(func() {
		fakeClock.SetTime(time.Now())

		shoot = &gardenercorev1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-shoot-" + testRunID,
				Namespace: testNamespace.Name,
				Labels:    map[string]string{testID: testRunID},
			},
			Spec: gardenercorev1beta1.ShootSpec{
				CloudProfile: &gardenercorev1beta1.CloudProfileReference{Kind: "CloudProfile", Name: "test"},
				Region:       "test-region",
				Provider: gardenercorev1beta1.Provider{
					Type: "openstack",
					Workers: []gardenercorev1beta1.Worker{{
						Name:    "worker",
						Minimum: 1,
						Maximum: 1,
						Machine: gardenercorev1beta1.Machine{
							Type: "test",
							Image: &gardenercorev1beta1.ShootMachineImage{
								Name:    "gardenlinux",
								Version: new("1.2.3"),
							},
						},
					}},
				},
				Kubernetes: gardenercorev1beta1.Kubernetes{Version: "1.30.0"},
				Networking: &gardenercorev1beta1.Networking{Type: new("calico")},
			},
		}

		testInternalSecret = &gardenercorev1beta1.InternalSecret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-creds-" + testRunID + "-" + time.Now().Format("150405.000"),
				Namespace: testNamespace.Name,
				Annotations: map[string]string{
					"secret.persephone.sci.cloud.sap/expires-at": time.Now().Add(24 * 60 * time.Hour).Format(time.RFC3339),
				},
				Labels: map[string]string{
					"persephone.sci.cloud.sap/shoot-name": shoot.Name,
					"sci.cloud.sap/region":                "test-region",
					"sci.cloud.sap/domain-name":           "test-domain",
					"sci.cloud.sap/domain-id":             "test-domain-id",
					"sci.cloud.sap/project-name":          "test-project",
					"sci.cloud.sap/project-id":            "test-project-id",
				},
			},
			Data: map[string][]byte{
				"applicationCredentialID":     []byte("test-cred-id"),
				"applicationCredentialSecret": []byte("test-secret"),
			},
		}
		shoot.Spec.CredentialsBindingName = &testInternalSecret.Name

		DeferCleanup(func() {
			test.WithVar(&internalsecret.RequeueAfterWhenSecretOwnsClusterServiceUser, 50*time.Millisecond)

			Expect(testClient.Delete(ctx, shoot)).To(Or(Succeed(), BeNotFoundError()))

			// get rid of all secrets after a test case to ensure we have a fresh environment
			internalSecretList := &gardenercorev1beta1.InternalSecretList{}
			Expect(testClient.List(ctx, internalSecretList, client.InNamespace(testNamespace.Name))).Should(Succeed())
			for _, secret := range internalSecretList.Items {
				Expect(testClient.Delete(ctx, &secret)).To(Succeed(), "for secret "+client.ObjectKeyFromObject(&secret).String())
				Expect(controllerutils.RemoveAllFinalizers(ctx, testClient, &secret)).To(Succeed(), "for secret "+client.ObjectKeyFromObject(&secret).String())
			}
		})
	})

	When("expires-at annotation is invalid", func() {
		It("should fail reconciliation gracefully", func() {
			By("Create Shoot")
			Expect(testClient.Create(ctx, shoot)).To(Succeed())

			By("Create InternalSecret with invalid expires-at annotation")
			testInternalSecret.Annotations["secret.persephone.sci.cloud.sap/expires-at"] = "invalid-date"
			Expect(testClient.Create(ctx, testInternalSecret)).To(Succeed())

			By("Verify InternalSecret does not get finalizer due to parse error")
			Consistently(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testInternalSecret), testInternalSecret)).To(Succeed())
				g.Expect(testInternalSecret.Finalizers).NotTo(ContainElement("persephone.sci.cloud.sap/secret-controller"))
			}).Should(Succeed())
		})
	})

	When("Shoot does not exist", func() {
		It("should delete InternalSecret older than 1h to trigger cleanup", func() {
			By("Create InternalSecret without Shoot")
			Expect(testClient.Create(ctx, testInternalSecret)).To(Succeed())

			By("Verify finalizer is added and CredentialsBinding is created (regular flow)")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testInternalSecret), testInternalSecret)).To(Succeed())
				g.Expect(testInternalSecret.Finalizers).To(ContainElement("persephone.sci.cloud.sap/secret-controller"))
			}).Should(Succeed())

			By("Advance fake clock by 2 hours to simulate secret being old")
			fakeClock.Step(2 * time.Hour)

			By("Trigger reconciliation by updating the secret")
			patch := client.MergeFrom(testInternalSecret.DeepCopy())
			if testInternalSecret.Annotations == nil {
				testInternalSecret.Annotations = make(map[string]string)
			}
			testInternalSecret.Annotations["test-trigger"] = "reconcile"
			Expect(testClient.Patch(ctx, testInternalSecret, patch)).To(Succeed())

			By("Verify InternalSecret is eventually deleted due to age")
			Eventually(func() error {
				return testClient.Get(ctx, client.ObjectKeyFromObject(testInternalSecret), testInternalSecret)
			}).Should(BeNotFoundError())
		})

		It("should NOT delete InternalSecret younger than 1h and should ensure CredentialsBinding", func() {
			By("Create InternalSecret without Shoot (with recent creation timestamp)")
			Expect(testClient.Create(ctx, testInternalSecret)).To(Succeed())

			By("Verify finalizer is added")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testInternalSecret), testInternalSecret)).To(Succeed())
				g.Expect(testInternalSecret.Finalizers).To(ContainElement("persephone.sci.cloud.sap/secret-controller"))
			}).Should(Succeed())

			By("Verify InternalSecret is NOT deleted (still exists)")
			Consistently(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testInternalSecret), testInternalSecret)).To(Succeed())
				g.Expect(testInternalSecret.DeletionTimestamp).To(BeNil())
			}).Should(Succeed())

			By("Verify CredentialsBinding is still created even though Shoot doesn't exist")
			Eventually(func(g Gomega) {
				credentialsBinding := &gardenersecurityv1alpha1.CredentialsBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testInternalSecret.Name,
						Namespace: shoot.Namespace,
					},
				}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(credentialsBinding), credentialsBinding)).To(Succeed())
				g.Expect(credentialsBinding.Provider.Type).To(Equal("openstack"))
			}).Should(Succeed())
		})
	})

	When("Secret is expired", func() {
		It("should delete the InternalSecret", func() {
			By("Create Shoot")
			Expect(testClient.Create(ctx, shoot)).To(Succeed())

			By("Create InternalSecret with expired credentials")
			testInternalSecret.Annotations[constants.AnnotationKeyExpiresAt] = time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
			Expect(testClient.Create(ctx, testInternalSecret)).To(Succeed())

			By("Verify InternalSecret is eventually deleted due to expiration")
			Eventually(func() error {
				return testClient.Get(ctx, client.ObjectKeyFromObject(testInternalSecret), testInternalSecret)
			}).Should(BeNotFoundError())
		})
	})

	When("Secret should be renewed (past 50% validity)", func() {
		It("should create a new InternalSecret with newer credentials", func() {
			By("Create Shoot")
			Expect(testClient.Create(ctx, shoot)).To(Succeed())

			By("Create InternalSecret with very short validity to trigger immediate renewal")
			testInternalSecret.Annotations[constants.AnnotationKeyExpiresAt] = fakeClock.Now().Add(2 * time.Second).Format(time.RFC3339)
			Expect(testClient.Create(ctx, testInternalSecret)).To(Succeed())

			By("Wait to ensure a newer secret has another creation timestamp")
			// If we proceeded too fast with the next step (clock stepping), the reconciler would immediately create a
			// new secret having the same creation timestamp as our test secret.
			time.Sleep(time.Second)

			By("Step fake clock to trigger renewal")
			fakeClock.Step(time.Second)

			By("Verify a new InternalSecret is created")
			Eventually(func(g Gomega) {
				internalSecretList := &gardenercorev1beta1.InternalSecretList{}
				g.Expect(testClient.List(ctx, internalSecretList,
					client.InNamespace(testNamespace.Name),
					client.MatchingLabels{"persephone.sci.cloud.sap/shoot-name": shoot.Name})).To(Succeed())
				// Should have 2 secrets now: the original + the new one
				g.Expect(internalSecretList.Items).To(HaveLen(2))
			}).Should(Succeed())

			By("Verify the new InternalSecret has expected properties")
			Eventually(func(g Gomega) {
				internalSecretList := &gardenercorev1beta1.InternalSecretList{}
				g.Expect(testClient.List(ctx, internalSecretList,
					client.InNamespace(testNamespace.Name),
					client.MatchingLabels{
						constants.LabelKeyShootName: shoot.Name,
					})).To(Succeed())

				// Find the newer secret (not the original testInternalSecret)
				var newerInternalSecret *gardenercorev1beta1.InternalSecret
				for i := range internalSecretList.Items {
					if internalSecretList.Items[i].Name != testInternalSecret.Name {
						newerInternalSecret = &internalSecretList.Items[i]
						break
					}
				}
				g.Expect(newerInternalSecret).NotTo(BeNil())
				g.Expect(newerInternalSecret.CreationTimestamp.After(testInternalSecret.CreationTimestamp.Time)).To(BeTrue())
				g.Expect(newerInternalSecret.Data).To(HaveKey("applicationCredentialID"))
				g.Expect(newerInternalSecret.Data).To(HaveKey("applicationCredentialSecret"))
			}).Should(Succeed())
		})
	})

	When("Secret validity is still more than 50%", func() {
		It("should add finalizer and ensure CredentialsBinding exists", func() {
			By("Create Shoot")
			Expect(testClient.Create(ctx, shoot)).To(Succeed())

			By("Create InternalSecret")
			Expect(testClient.Create(ctx, testInternalSecret)).To(Succeed())

			By("Verify finalizer is added")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testInternalSecret), testInternalSecret)).To(Succeed())
				g.Expect(testInternalSecret.Finalizers).To(ContainElement("persephone.sci.cloud.sap/secret-controller"))
			}).Should(Succeed())

			By("Verify CredentialsBinding is created")
			Eventually(func(g Gomega) {
				credentialsBinding := &gardenersecurityv1alpha1.CredentialsBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testInternalSecret.Name,
						Namespace: shoot.Namespace,
					},
				}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(credentialsBinding), credentialsBinding)).To(Succeed())
				g.Expect(credentialsBinding.Provider.Type).To(Equal("openstack"))
				g.Expect(credentialsBinding.CredentialsRef.Name).To(Equal(testInternalSecret.Name))
				g.Expect(credentialsBinding.CredentialsRef.Namespace).To(Equal(testInternalSecret.Namespace))
			}).Should(Succeed())
		})
	})

	When("Secret has deletion timestamp", func() {
		var credentialsBinding *gardenersecurityv1alpha1.CredentialsBinding

		BeforeEach(func() {
			By("Create Shoot")
			Expect(testClient.Create(ctx, shoot)).To(Succeed())

			By("Create InternalSecret")
			Expect(testClient.Create(ctx, testInternalSecret)).To(Succeed())

			By("Wait for finalizer and CredentialsBinding")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testInternalSecret), testInternalSecret)).To(Succeed())
				g.Expect(testInternalSecret.Finalizers).To(ContainElement("persephone.sci.cloud.sap/secret-controller"))
			}).Should(Succeed())

			credentialsBinding = &gardenersecurityv1alpha1.CredentialsBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testInternalSecret.Name,
					Namespace: shoot.Namespace,
				},
			}
			Eventually(func() error {
				return testClient.Get(ctx, client.ObjectKeyFromObject(credentialsBinding), credentialsBinding)
			}).Should(Succeed())
		})

		It("should wait if InternalSecret is still in use by Shoot", func() {
			By("Delete InternalSecret while it's still referenced by Shoot")
			Expect(testClient.Delete(ctx, testInternalSecret)).To(Succeed())

			By("Verify InternalSecret still exists (not cleaned up yet)")
			Consistently(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testInternalSecret), testInternalSecret)).To(Succeed())
				g.Expect(testInternalSecret.DeletionTimestamp).NotTo(BeNil())
				g.Expect(testInternalSecret.Finalizers).To(ContainElement("persephone.sci.cloud.sap/secret-controller"))
			}).Should(Succeed())
		})

		It("should cleanup when InternalSecret is not in use", func() {
			By("Update Shoot to use different credentials")
			patch := client.MergeFrom(shoot.DeepCopy())
			shoot.Spec.CredentialsBindingName = new("different-credentials")
			Expect(testClient.Patch(ctx, shoot, patch)).To(Succeed())

			By("Delete all secrets (including the test secret, but also the others which must be gone as well to allow cluster service user cleanup)")
			Expect(testClient.DeleteAllOf(ctx, &gardenercorev1beta1.InternalSecret{}, client.InNamespace(shoot.Namespace), client.MatchingLabels{"persephone.sci.cloud.sap/shoot-name": shoot.Name})).To(Succeed())

			By("Verify CredentialsBinding is deleted")
			Eventually(func() error {
				return testClient.Get(ctx, client.ObjectKeyFromObject(credentialsBinding), credentialsBinding)
			}).Should(BeNotFoundError())

			By("Verify InternalSecret finalizer is removed and InternalSecret is deleted")
			Eventually(func() error {
				return testClient.Get(ctx, client.ObjectKeyFromObject(testInternalSecret), testInternalSecret)
			}).Should(BeNotFoundError())
		})

		It("should cleanup when Shoot has been deleted successfully", func() {
			By("Mark Shoot as successfully deleted")
			patch := client.MergeFrom(shoot.DeepCopy())
			shoot.Status.LastOperation = &gardenercorev1beta1.LastOperation{
				Type:  gardenercorev1beta1.LastOperationTypeDelete,
				State: gardenercorev1beta1.LastOperationStateSucceeded,
			}
			Expect(testClient.Status().Patch(ctx, shoot, patch)).To(Succeed())

			By("Delete InternalSecret while it's still referenced by Shoot")
			Expect(testClient.Delete(ctx, testInternalSecret)).To(Succeed())

			By("Verify CredentialsBinding is deleted")
			Eventually(func() error {
				return testClient.Get(ctx, client.ObjectKeyFromObject(credentialsBinding), credentialsBinding)
			}).Should(BeNotFoundError())

			By("Verify InternalSecret finalizer is removed and InternalSecret is deleted")
			Eventually(func() error {
				return testClient.Get(ctx, client.ObjectKeyFromObject(testInternalSecret), testInternalSecret)
			}).Should(BeNotFoundError())
		})
	})

	When("multiple InternalSecrets exist for same Shoot", func() {
		var olderInternalSecret, newerInternalSecret *gardenercorev1beta1.InternalSecret

		BeforeEach(func() {
			By("Create Shoot")
			Expect(testClient.Create(ctx, shoot)).To(Succeed())

			By("Create older InternalSecret")
			olderInternalSecret = testInternalSecret.DeepCopy()
			olderInternalSecret.Name = "older-credentials"
			Expect(testClient.Create(ctx, olderInternalSecret)).To(Succeed())

			By("Wait a moment to ensure different creation timestamps")
			time.Sleep(100 * time.Millisecond)

			By("Create newer InternalSecret")
			newerInternalSecret = testInternalSecret.DeepCopy()
			newerInternalSecret.Name = "newer-credentials"
			newerInternalSecret.ResourceVersion = ""
			Expect(testClient.Create(ctx, newerInternalSecret)).To(Succeed())
		})

		It("should create CredentialsBinding for each InternalSecret", func() {
			By("Verify CredentialsBinding for older InternalSecret")
			Eventually(func(g Gomega) {
				credentialsBinding := &gardenersecurityv1alpha1.CredentialsBinding{ObjectMeta: metav1.ObjectMeta{Name: olderInternalSecret.Name, Namespace: shoot.Namespace}}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(credentialsBinding), credentialsBinding)).To(Succeed())
			}).Should(Succeed())

			By("Verify CredentialsBinding for newer InternalSecret")
			Eventually(func(g Gomega) {
				credentialsBinding := &gardenersecurityv1alpha1.CredentialsBinding{ObjectMeta: metav1.ObjectMeta{Name: newerInternalSecret.Name, Namespace: shoot.Namespace}}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(credentialsBinding), credentialsBinding)).To(Succeed())
			}).Should(Succeed())
		})
	})
})
