// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package auditcert

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	secretsutils "github.com/gardener/gardener/pkg/utils/secrets"
)

var _ = Describe("Reconciler", func() {
	var (
		ctx context.Context
		rec *reconciler
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("generateCertificates", func() {
		BeforeEach(func() {
			rec = &reconciler{
				secretName:   "test-cert",
				namespace:    "garden",
				serviceName:  "persephone-audit-webhook",
				certValidity: 30 * 24 * time.Hour,
				syncPeriod:   5 * time.Minute,
			}
		})

		It("should generate valid CA, server, and client certificates", func() {
			data, err := rec.generateCertificates()
			Expect(err).NotTo(HaveOccurred())

			Expect(data).To(HaveKey(secretsutils.DataKeyCertificateCA))
			Expect(data).To(HaveKey(secretsutils.DataKeyPrivateKeyCA))
			Expect(data).To(HaveKey(secretsutils.DataKeyCertificate))
			Expect(data).To(HaveKey(secretsutils.DataKeyPrivateKey))
			Expect(data).To(HaveKey("client.crt"))
			Expect(data).To(HaveKey("client.key"))

			By("verifying the CA certificate")
			caCert := parseCert(data[secretsutils.DataKeyCertificateCA])
			Expect(caCert.IsCA).To(BeTrue())
			Expect(caCert.Subject.CommonName).To(Equal("persephone-audit-webhook-ca"))

			By("verifying the server certificate")
			serverCert := parseCert(data[secretsutils.DataKeyCertificate])
			Expect(serverCert.IsCA).To(BeFalse())
			Expect(serverCert.DNSNames).To(ContainElements(
				"persephone-audit-webhook.garden.svc",
				"persephone-audit-webhook.garden.svc.cluster.local",
				"persephone-audit-webhook",
			))
			Expect(serverCert.ExtKeyUsage).To(ContainElement(x509.ExtKeyUsageServerAuth))

			By("verifying the server cert is signed by the CA")
			caPool := x509.NewCertPool()
			caPool.AddCert(caCert)
			_, err = serverCert.Verify(x509.VerifyOptions{
				Roots:     caPool,
				KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying the client certificate")
			clientCert := parseCert(data["client.crt"])
			Expect(clientCert.IsCA).To(BeFalse())
			Expect(clientCert.Subject.CommonName).To(Equal("audit-forwarder"))
			Expect(clientCert.ExtKeyUsage).To(ContainElement(x509.ExtKeyUsageClientAuth))

			By("verifying the client cert is signed by the CA")
			_, err = clientCert.Verify(x509.VerifyOptions{
				Roots:     caPool,
				KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should set correct validity on certificates", func() {
			data, err := rec.generateCertificates()
			Expect(err).NotTo(HaveOccurred())

			caCert := parseCert(data[secretsutils.DataKeyCertificateCA])
			validity := caCert.NotAfter.Sub(caCert.NotBefore)
			Expect(validity).To(BeNumerically("~", 30*24*time.Hour, 2*time.Minute))
		})
	})

	Describe("needsRotation", func() {
		BeforeEach(func() {
			rec = &reconciler{
				secretName:   "test-cert",
				namespace:    "garden",
				serviceName:  "persephone-audit-webhook",
				certValidity: 30 * 24 * time.Hour,
				syncPeriod:   5 * time.Minute,
			}
		})

		It("should return true when secret has no CA cert", func() {
			secret := &corev1.Secret{Data: map[string][]byte{}}
			Expect(rec.needsRotation(secret)).To(BeTrue())
		})

		It("should return true when CA cert is invalid PEM", func() {
			secret := &corev1.Secret{Data: map[string][]byte{
				secretsutils.DataKeyCertificateCA: []byte("not-a-cert"),
			}}
			Expect(rec.needsRotation(secret)).To(BeTrue())
		})

		It("should return false when CA cert is fresh", func() {
			data, err := rec.generateCertificates()
			Expect(err).NotTo(HaveOccurred())

			secret := &corev1.Secret{Data: data}
			Expect(rec.needsRotation(secret)).To(BeFalse())
		})
	})

	Describe("Reconcile", func() {
		It("should create a new secret when none exists", func() {
			fakeClient := fake.NewClientBuilder().Build()
			rec = &reconciler{
				client:       fakeClient,
				secretName:   "test-cert",
				namespace:    "default",
				serviceName:  "persephone-audit-webhook",
				certValidity: 30 * 24 * time.Hour,
				syncPeriod:   5 * time.Minute,
			}

			result, err := rec.Reconcile(ctx, reconcile.Request{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5 * time.Minute))

			secret := &corev1.Secret{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-cert", Namespace: "default"}, secret)
			Expect(err).NotTo(HaveOccurred())
			Expect(secret.Data).To(HaveKey(secretsutils.DataKeyCertificateCA))
			Expect(secret.Data).To(HaveKey(secretsutils.DataKeyCertificate))
			Expect(secret.Data).To(HaveKey("client.crt"))
		})

		It("should not rotate when certificates are still valid", func() {
			certGen := &reconciler{
				serviceName:  "persephone-audit-webhook",
				certValidity: 30 * 24 * time.Hour,
			}
			data, err := certGen.generateCertificates()
			Expect(err).NotTo(HaveOccurred())

			existingSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cert",
					Namespace: "default",
				},
				Data: data,
			}
			fakeClient := fake.NewClientBuilder().WithObjects(existingSecret).Build()
			rec = &reconciler{
				client:       fakeClient,
				secretName:   "test-cert",
				namespace:    "default",
				serviceName:  "persephone-audit-webhook",
				certValidity: 30 * 24 * time.Hour,
				syncPeriod:   5 * time.Minute,
			}

			result, err := rec.Reconcile(ctx, reconcile.Request{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5 * time.Minute))

			// Verify the secret was NOT updated (same data)
			secret := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-cert", Namespace: "default"}, secret)).To(Succeed())
			Expect(secret.Data[secretsutils.DataKeyCertificateCA]).To(Equal(data[secretsutils.DataKeyCertificateCA]))
		})

		It("should rotate when the secret has invalid cert data", func() {
			existingSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cert",
					Namespace: "default",
				},
				Data: map[string][]byte{
					secretsutils.DataKeyCertificateCA: []byte("invalid"),
				},
			}
			fakeClient := fake.NewClientBuilder().WithObjects(existingSecret).Build()
			rec = &reconciler{
				client:       fakeClient,
				secretName:   "test-cert",
				namespace:    "default",
				serviceName:  "persephone-audit-webhook",
				certValidity: 30 * 24 * time.Hour,
				syncPeriod:   5 * time.Minute,
			}

			result, err := rec.Reconcile(ctx, reconcile.Request{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5 * time.Minute))

			// Verify new valid certs were generated
			secret := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-cert", Namespace: "default"}, secret)).To(Succeed())
			Expect(secret.Data[secretsutils.DataKeyCertificateCA]).NotTo(Equal([]byte("invalid")))
			caCert := parseCert(secret.Data[secretsutils.DataKeyCertificateCA])
			Expect(caCert.IsCA).To(BeTrue())
		})
	})

	Describe("generateAndCreateSecret", func() {
		It("should fail if secret already exists", func() {
			existingSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cert",
					Namespace: "default",
				},
			}
			fakeClient := fake.NewClientBuilder().WithObjects(existingSecret).Build()
			rec = &reconciler{
				client:       fakeClient,
				secretName:   "test-cert",
				namespace:    "default",
				serviceName:  "persephone-audit-webhook",
				certValidity: 30 * 24 * time.Hour,
			}

			err := rec.generateAndCreateSecret(ctx)
			Expect(apierrors.IsAlreadyExists(err)).To(BeTrue())
		})
	})

	Describe("CA bundle rotation", func() {
		BeforeEach(func() {
			rec = &reconciler{
				secretName:   "test-cert",
				namespace:    "default",
				serviceName:  "persephone-audit-webhook",
				certValidity: 30 * 24 * time.Hour,
				syncPeriod:   5 * time.Minute,
			}
		})

		It("should bundle old CA with new CA during rotation", func() {
			data, err := rec.generateCertificates()
			Expect(err).NotTo(HaveOccurred())
			originalCACert := parseCert(data[secretsutils.DataKeyCertificateCA])

			existingSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cert",
					Namespace: "default",
				},
				Data: data,
			}
			fakeClient := fake.NewClientBuilder().WithObjects(existingSecret).Build()
			rec.client = fakeClient

			err = rec.generateAndUpdateSecret(ctx, existingSecret)
			Expect(err).NotTo(HaveOccurred())

			secret := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-cert", Namespace: "default"}, secret)).To(Succeed())

			By("verifying ca.crt contains exactly two PEM blocks")
			pemBlocks := countPEMBlocks(secret.Data[secretsutils.DataKeyCertificateCA])
			Expect(pemBlocks).To(Equal(2))

			By("verifying the first PEM block is the new CA")
			newCACert := parseCert(secret.Data[secretsutils.DataKeyCertificateCA])
			Expect(newCACert.SerialNumber).NotTo(Equal(originalCACert.SerialNumber))

			By("verifying the second PEM block is the old CA")
			secondCert := parseSecondCert(secret.Data[secretsutils.DataKeyCertificateCA])
			Expect(secondCert.SerialNumber).To(Equal(originalCACert.SerialNumber))

			By("verifying the old CA can still verify old client certs")
			oldPool := x509.NewCertPool()
			oldPool.AddCert(originalCACert)
			oldClientCert := parseCert(data["client.crt"])
			_, err = oldClientCert.Verify(x509.VerifyOptions{
				Roots:     oldPool,
				KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying the new CA verifies the new server/client certs")
			newPool := x509.NewCertPool()
			newPool.AddCert(newCACert)
			newServerCert := parseCert(secret.Data[secretsutils.DataKeyCertificate])
			_, err = newServerCert.Verify(x509.VerifyOptions{
				Roots:     newPool,
				KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should keep only one previous generation during successive rotations", func() {
			data, err := rec.generateCertificates()
			Expect(err).NotTo(HaveOccurred())

			existingSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cert",
					Namespace: "default",
				},
				Data: data,
			}
			fakeClient := fake.NewClientBuilder().WithObjects(existingSecret).Build()
			rec.client = fakeClient

			By("first rotation: produces 2 PEM blocks")
			err = rec.generateAndUpdateSecret(ctx, existingSecret)
			Expect(err).NotTo(HaveOccurred())

			secret := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-cert", Namespace: "default"}, secret)).To(Succeed())
			Expect(countPEMBlocks(secret.Data[secretsutils.DataKeyCertificateCA])).To(Equal(2))

			firstRotationCACert := parseCert(secret.Data[secretsutils.DataKeyCertificateCA])

			By("second rotation: still only 2 PEM blocks (oldest dropped)")
			err = rec.generateAndUpdateSecret(ctx, secret)
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-cert", Namespace: "default"}, secret)).To(Succeed())
			Expect(countPEMBlocks(secret.Data[secretsutils.DataKeyCertificateCA])).To(Equal(2))

			By("verifying the second PEM block is the CA from the first rotation (not the original)")
			secondCert := parseSecondCert(secret.Data[secretsutils.DataKeyCertificateCA])
			Expect(secondCert.SerialNumber).To(Equal(firstRotationCACert.SerialNumber))
		})

		It("should work correctly with needsRotation when ca.crt is bundled", func() {
			data, err := rec.generateCertificates()
			Expect(err).NotTo(HaveOccurred())

			existingSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cert",
					Namespace: "default",
				},
				Data: data,
			}
			fakeClient := fake.NewClientBuilder().WithObjects(existingSecret).Build()
			rec.client = fakeClient

			err = rec.generateAndUpdateSecret(ctx, existingSecret)
			Expect(err).NotTo(HaveOccurred())

			secret := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-cert", Namespace: "default"}, secret)).To(Succeed())

			By("verifying needsRotation returns false for freshly rotated bundled ca.crt")
			Expect(rec.needsRotation(secret)).To(BeFalse())
		})
	})

	Describe("firstPEMBlock", func() {
		It("should return the first PEM block from a bundle", func() {
			rec = &reconciler{
				secretName:   "test-cert",
				namespace:    "default",
				serviceName:  "persephone-audit-webhook",
				certValidity: 30 * 24 * time.Hour,
			}
			data, err := rec.generateCertificates()
			Expect(err).NotTo(HaveOccurred())

			originalCA := data[secretsutils.DataKeyCertificateCA]
			bundle := append(originalCA, originalCA...)

			result := firstPEMBlock(bundle)
			Expect(result).NotTo(BeNil())
			Expect(countPEMBlocks(result)).To(Equal(1))
			Expect(parseCert(result).SerialNumber).To(Equal(parseCert(originalCA).SerialNumber))
		})

		It("should return nil for invalid PEM data", func() {
			Expect(firstPEMBlock([]byte("not-pem"))).To(BeNil())
		})

		It("should return nil for empty data", func() {
			Expect(firstPEMBlock(nil)).To(BeNil())
		})
	})
})

func parseCert(pemData []byte) *x509.Certificate {
	GinkgoHelper()
	block, _ := pem.Decode(pemData)
	Expect(block).NotTo(BeNil(), "failed to decode PEM data")
	cert, err := x509.ParseCertificate(block.Bytes)
	Expect(err).NotTo(HaveOccurred())
	return cert
}

func parseSecondCert(pemData []byte) *x509.Certificate {
	GinkgoHelper()
	_, rest := pem.Decode(pemData)
	Expect(rest).NotTo(BeEmpty(), "expected at least two PEM blocks")
	block, _ := pem.Decode(rest)
	Expect(block).NotTo(BeNil(), "failed to decode second PEM block")
	cert, err := x509.ParseCertificate(block.Bytes)
	Expect(err).NotTo(HaveOccurred())
	return cert
}

func countPEMBlocks(data []byte) int {
	count := 0
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		count++
	}
	return count
}
