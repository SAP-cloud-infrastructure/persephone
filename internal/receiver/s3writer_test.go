// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package receiver_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8stypes "k8s.io/apimachinery/pkg/types"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"github.com/sap-cloud-infrastructure/persephone/internal/receiver"
)

// newTestS3Writer creates an S3Writer pointing at a fake S3 server.
func newTestS3Writer(srv *httptest.Server, bucket, prefix string, objectLock *receiver.ObjectLockCfg) *receiver.S3Writer {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("default"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("access", "secret", "")),
	)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(srv.URL)
		o.UsePathStyle = true
	})

	return &receiver.S3Writer{
		Logger:     logr.Discard(),
		Client:     client,
		Bucket:     bucket,
		Prefix:     prefix,
		ObjectLock: objectLock,
	}
}

var _ = Describe("S3Writer", func() {
	var (
		srv      *httptest.Server
		putPaths []string
		putBodies [][]byte
	)

	BeforeEach(func() {
		putPaths = nil
		putBodies = nil
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPut:
				body, _ := io.ReadAll(r.Body)
				putPaths = append(putPaths, r.URL.Path)
				putBodies = append(putBodies, body)
				w.WriteHeader(http.StatusOK)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
	})

	AfterEach(func() {
		srv.Close()
	})

	It("writes gzip NDJSON with correct key structure", func() {
		w := newTestS3Writer(srv, "my-bucket", "audit/", nil)

		events := []auditv1.Event{
			{AuditID: "event-1"},
			{AuditID: "event-2"},
		}
		Expect(w.Write(ctx, events)).To(Succeed())

		Expect(putPaths).To(HaveLen(1))
		path := putPaths[0]
		// /my-bucket/audit/YYYY-MM-DD/<timestamp>-<uuid>.ndjson.gz
		Expect(path).To(ContainSubstring("/my-bucket/audit/"))
		Expect(path).To(MatchRegexp(`/\d{4}-\d{2}-\d{2}/\d{8}T\d{6}Z-[0-9a-f-]+\.ndjson\.gz$`))

		gr, err := gzip.NewReader(bytes.NewReader(putBodies[0]))
		Expect(err).NotTo(HaveOccurred())
		dec := json.NewDecoder(gr)
		var decoded []auditv1.Event
		for dec.More() {
			var e auditv1.Event
			Expect(dec.Decode(&e)).To(Succeed())
			decoded = append(decoded, e)
		}
		Expect(decoded).To(HaveLen(2))
		Expect(decoded[0].AuditID).To(Equal(k8stypes.UID("event-1")))
		Expect(decoded[1].AuditID).To(Equal(k8stypes.UID("event-2")))
	})

	It("does nothing for empty event slice", func() {
		w := newTestS3Writer(srv, "my-bucket", "", nil)
		Expect(w.Write(ctx, nil)).To(Succeed())
		Expect(putPaths).To(BeEmpty())
	})

	It("places objects under the given prefix", func() {
		w := newTestS3Writer(srv, "bucket", "prefix/", nil)
		Expect(w.Write(ctx, []auditv1.Event{{AuditID: "x"}})).To(Succeed())
		Expect(putPaths).To(HaveLen(1))
		Expect(putPaths[0]).To(ContainSubstring("/bucket/prefix/"))
		Expect(strings.Contains(putPaths[0], "//")).To(BeFalse())
	})
})
