// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package receiver_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/go-logr/logr"
	"github.com/gophercloud/gophercloud/v2"
	k8stypes "k8s.io/apimachinery/pkg/types"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"github.com/sap-cloud-infrastructure/persephone/internal/receiver"
)

var _ = Describe("SwiftWriter", func() {
	var (
		srv       *httptest.Server
		putPaths  []string
		putBodies [][]byte
	)

	BeforeEach(func() {
		putPaths = nil
		putBodies = nil
		// srv is captured in the closure below; it's set before the handler runs.
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/v3/auth/tokens":
				// Minimal Keystone v3 token response
				w.Header().Set("X-Subject-Token", "test-token")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"token": map[string]any{
						"catalog": []map[string]any{
							{
								"type": "object-store",
								"endpoints": []map[string]any{
									{
										"interface": "public",
										"region_id": "RegionOne",
										"url":       srv.URL + "/v1/AUTH_test",
									},
								},
							},
						},
					},
				})
			case r.Method == http.MethodPut:
				body, _ := io.ReadAll(r.Body)
				putPaths = append(putPaths, r.URL.Path)
				putBodies = append(putBodies, body)
				w.WriteHeader(http.StatusCreated)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
	})

	AfterEach(func() {
		srv.Close()
	})

	newWriter := func(container, prefix string) (*receiver.SwiftWriter, error) {
		return receiver.NewSwiftWriter(ctx, logr.Discard(),
			gophercloud.AuthOptions{
				// Versioned endpoint bypasses version-discovery GET on root
				IdentityEndpoint: srv.URL + "/v3/",
				Username:         "u",
				Password:         "p",
				DomainName:       "Default",
			},
			container,
			prefix,
		)
	}

	It("writes gzip NDJSON with correct key structure", func() {
		w, err := newWriter("my-container", "audit/")
		Expect(err).NotTo(HaveOccurred())

		events := []auditv1.Event{
			{AuditID: "event-1"},
			{AuditID: "event-2"},
		}
		Expect(w.Write(ctx, events)).To(Succeed())

		Expect(putPaths).To(HaveLen(1))
		path := putPaths[0]
		// /v1/AUTH_test/my-container/audit/YYYY-MM-DD/<timestamp>-<uuid>.ndjson.gz
		Expect(path).To(ContainSubstring("/my-container/audit/"))
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
		w, err := newWriter("my-container", "")
		Expect(err).NotTo(HaveOccurred())
		Expect(w.Write(ctx, nil)).To(Succeed())
		Expect(putPaths).To(BeEmpty())
	})

	It("places objects under the given prefix", func() {
		w, err := newWriter("container", "prefix/")
		Expect(err).NotTo(HaveOccurred())
		Expect(w.Write(ctx, []auditv1.Event{{AuditID: "x"}})).To(Succeed())
		Expect(putPaths).To(HaveLen(1))
		Expect(putPaths[0]).To(ContainSubstring("/container/prefix/"))
		Expect(strings.Contains(putPaths[0], "//")).To(BeFalse())
	})
})
