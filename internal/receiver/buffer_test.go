// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package receiver_test

import (
	"context"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"github.com/sap-cloud-infrastructure/persephone/internal/receiver"
)

var _ = Describe("Buffer", func() {
	It("flushes when max events is reached", func() {
		var mu sync.Mutex
		var flushed []auditv1.Event

		buf := receiver.NewBuffer(10*time.Second, 3, func(events []auditv1.Event) {
			mu.Lock()
			flushed = append(flushed, events...)
			mu.Unlock()
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go buf.Start(ctx)

		buf.Add([]auditv1.Event{{}, {}, {}})

		Eventually(func() int {
			mu.Lock()
			defer mu.Unlock()
			return len(flushed)
		}).Should(Equal(3))
	})

	It("flushes on interval", func() {
		var mu sync.Mutex
		var flushed []auditv1.Event

		buf := receiver.NewBuffer(50*time.Millisecond, 1000, func(events []auditv1.Event) {
			mu.Lock()
			flushed = append(flushed, events...)
			mu.Unlock()
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go buf.Start(ctx)

		buf.Add([]auditv1.Event{{}, {}})

		Eventually(func() int {
			mu.Lock()
			defer mu.Unlock()
			return len(flushed)
		}, 500*time.Millisecond).Should(Equal(2))
	})

	It("does not flush empty buffer", func() {
		flushCount := 0
		buf := receiver.NewBuffer(20*time.Millisecond, 1000, func(_ []auditv1.Event) {
			flushCount++
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go buf.Start(ctx)

		time.Sleep(100 * time.Millisecond)
		Expect(flushCount).To(Equal(0))
	})
})
