// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package receiver

import (
	"context"
	"sync"
	"time"

	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
)

// Buffer accumulates audit events and flushes them either when a size
// threshold is reached or on a periodic interval.
type Buffer struct {
	flushInterval time.Duration
	maxEvents     int
	flushFn       func([]auditv1.Event)

	mu      sync.Mutex
	events  []auditv1.Event
	trigger chan struct{}
}

// NewBuffer creates a new Buffer. flushFn is called with a snapshot of
// accumulated events; it must not modify the slice.
func NewBuffer(flushInterval time.Duration, maxEvents int, flushFn func([]auditv1.Event)) *Buffer {
	return &Buffer{
		flushInterval: flushInterval,
		maxEvents:     maxEvents,
		flushFn:       flushFn,
		trigger:       make(chan struct{}, 1),
	}
}

// Add appends events to the buffer and triggers an immediate flush if
// maxEvents is reached.
func (b *Buffer) Add(events []auditv1.Event) {
	b.mu.Lock()
	b.events = append(b.events, events...)
	shouldFlush := len(b.events) >= b.maxEvents
	b.mu.Unlock()

	if shouldFlush {
		select {
		case b.trigger <- struct{}{}:
		default:
		}
	}
}

// Start runs the flush loop until ctx is cancelled. Call in a goroutine.
func (b *Buffer) Start(ctx context.Context) {
	ticker := time.NewTicker(b.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			b.flush()
			return
		case <-ticker.C:
			b.flush()
		case <-b.trigger:
			b.flush()
		}
	}
}

func (b *Buffer) flush() {
	b.mu.Lock()
	if len(b.events) == 0 {
		b.mu.Unlock()
		return
	}
	snapshot := b.events
	b.events = nil
	b.mu.Unlock()

	b.flushFn(snapshot)
}
