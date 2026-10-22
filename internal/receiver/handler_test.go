// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package receiver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/go-logr/logr"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"github.com/sap-cloud-infrastructure/persephone/internal/receiver"
)

var _ = Describe("Handler", func() {
	It("returns 200 and enqueues events for valid EventList", func() {
		var mu sync.Mutex
		var received []auditv1.Event

		buf := receiver.NewBuffer(10*time.Millisecond, 1000, func(events []auditv1.Event) {
			mu.Lock()
			received = append(received, events...)
			mu.Unlock()
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go buf.Start(ctx)

		h := &receiver.Handler{
			Logger: logr.Discard(),
			Buffer: buf,
		}

		eventList := auditv1.EventList{
			Items: []auditv1.Event{{AuditID: "abc123"}},
		}
		body, _ := json.Marshal(eventList)
		req := httptest.NewRequest(http.MethodPost, "/audit", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))
		Eventually(func() int {
			mu.Lock()
			defer mu.Unlock()
			return len(received)
		}).Should(Equal(1))
	})

	It("returns 400 for invalid JSON body", func() {
		buf := receiver.NewBuffer(10*time.Second, 1000, func(_ []auditv1.Event) {})
		h := &receiver.Handler{Logger: logr.Discard(), Buffer: buf}

		req := httptest.NewRequest(http.MethodPost, "/audit", bytes.NewReader([]byte("not json")))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusBadRequest))
	})
})
