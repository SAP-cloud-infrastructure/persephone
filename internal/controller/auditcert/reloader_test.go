// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package auditcert

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	secretsutils "github.com/gardener/gardener/pkg/utils/secrets"
)

var _ = Describe("Reloader", func() {
	var (
		ctx     context.Context
		certDir string
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		certDir, err = os.MkdirTemp("", "auditcert-test-*")
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			os.RemoveAll(certDir)
		})
	})

	Describe("Reconcile", func() {
		It("should write certificate files to disk", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-cert",
					Namespace:       "default",
					ResourceVersion: "1",
				},
				Data: map[string][]byte{
					secretsutils.DataKeyCertificate:   []byte("server-cert-pem"),
					secretsutils.DataKeyPrivateKey:    []byte("server-key-pem"),
					secretsutils.DataKeyCertificateCA: []byte("ca-cert-pem"),
				},
			}

			fakeClient := fake.NewClientBuilder().WithObjects(secret).Build()
			rel := &reloader{
				reader:       fakeClient,
				secretName:   "test-cert",
				namespace:    "default",
				reloadPeriod: 30 * time.Second,
				certDir:      certDir,
			}

			result, err := rel.Reconcile(ctx, reconcile.Request{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(30 * time.Second))

			Expect(readFile(certDir, secretsutils.DataKeyCertificate)).To(Equal([]byte("server-cert-pem")))
			Expect(readFile(certDir, secretsutils.DataKeyPrivateKey)).To(Equal([]byte("server-key-pem")))
			Expect(readFile(certDir, secretsutils.DataKeyCertificateCA)).To(Equal([]byte("ca-cert-pem")))
		})

		It("should skip writing when resource version is unchanged", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-cert",
					Namespace:       "default",
					ResourceVersion: "42",
				},
				Data: map[string][]byte{
					secretsutils.DataKeyCertificate:   []byte("server-cert-pem"),
					secretsutils.DataKeyPrivateKey:    []byte("server-key-pem"),
					secretsutils.DataKeyCertificateCA: []byte("ca-cert-pem"),
				},
			}

			fakeClient := fake.NewClientBuilder().WithObjects(secret).Build()
			rel := &reloader{
				reader:              fakeClient,
				secretName:          "test-cert",
				namespace:           "default",
				reloadPeriod:        30 * time.Second,
				certDir:             certDir,
				lastResourceVersion: "42",
			}

			result, err := rel.Reconcile(ctx, reconcile.Request{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(30 * time.Second))

			// Files should NOT exist since we skipped writing
			_, err = os.Stat(filepath.Join(certDir, secretsutils.DataKeyCertificate))
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("should write when resource version changes", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-cert",
					Namespace:       "default",
					ResourceVersion: "43",
				},
				Data: map[string][]byte{
					secretsutils.DataKeyCertificate:   []byte("new-cert"),
					secretsutils.DataKeyPrivateKey:    []byte("new-key"),
					secretsutils.DataKeyCertificateCA: []byte("new-ca"),
				},
			}

			fakeClient := fake.NewClientBuilder().WithObjects(secret).Build()
			rel := &reloader{
				reader:              fakeClient,
				secretName:          "test-cert",
				namespace:           "default",
				reloadPeriod:        30 * time.Second,
				certDir:             certDir,
				lastResourceVersion: "42",
			}

			result, err := rel.Reconcile(ctx, reconcile.Request{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(30 * time.Second))

			Expect(readFile(certDir, secretsutils.DataKeyCertificate)).To(Equal([]byte("new-cert")))
			Expect(rel.lastResourceVersion).To(Equal("43"))
		})

		It("should return error when secret data is empty", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-cert",
					Namespace:       "default",
					ResourceVersion: "1",
				},
				Data: map[string][]byte{
					secretsutils.DataKeyCertificate:   {},
					secretsutils.DataKeyPrivateKey:    []byte("key"),
					secretsutils.DataKeyCertificateCA: []byte("ca"),
				},
			}

			fakeClient := fake.NewClientBuilder().WithObjects(secret).Build()
			rel := &reloader{
				reader:       fakeClient,
				secretName:   "test-cert",
				namespace:    "default",
				reloadPeriod: 30 * time.Second,
				certDir:      certDir,
			}

			_, err := rel.Reconcile(ctx, reconcile.Request{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("tls.crt"))
		})
	})

	Describe("writeCertificatesToDisk", func() {
		It("should create cert directory if it doesn't exist", func() {
			newDir := filepath.Join(certDir, "nested", "dir")
			data := map[string][]byte{
				secretsutils.DataKeyCertificate:   []byte("cert"),
				secretsutils.DataKeyPrivateKey:    []byte("key"),
				secretsutils.DataKeyCertificateCA: []byte("ca"),
			}

			err := writeCertificatesToDisk(newDir, data)
			Expect(err).NotTo(HaveOccurred())
			Expect(readFile(newDir, secretsutils.DataKeyCertificate)).To(Equal([]byte("cert")))
		})
	})
})

func readFile(dir, name string) []byte {
	GinkgoHelper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	Expect(err).NotTo(HaveOccurred())
	return data
}
