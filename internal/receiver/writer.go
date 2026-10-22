// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package receiver

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/objectstorage/v1/objects"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
)

// SwiftWriter writes batches of audit events as gzip-compressed NDJSON
// objects to an OpenStack Swift container.
type SwiftWriter struct {
	Logger    logr.Logger
	Client    *gophercloud.ServiceClient
	Container string
	Prefix    string
}

// NewSwiftWriter creates an authenticated Swift service client and returns
// a SwiftWriter. authOpts must contain valid OpenStack credentials.
func NewSwiftWriter(ctx context.Context, logger logr.Logger, authOpts gophercloud.AuthOptions, container, prefix string) (*SwiftWriter, error) {
	provider, err := openstack.AuthenticatedClient(ctx, authOpts)
	if err != nil {
		return nil, fmt.Errorf("openstack auth: %w", err)
	}
	client, err := openstack.NewObjectStorageV1(provider, gophercloud.EndpointOpts{})
	if err != nil {
		return nil, fmt.Errorf("object storage client: %w", err)
	}
	return &SwiftWriter{
		Logger:    logger,
		Client:    client,
		Container: container,
		Prefix:    prefix,
	}, nil
}

// Write serialises events as gzip NDJSON and PUTs the object into Swift.
// The object key is: <prefix><YYYY-MM-DD>/<timestamp>-<uuid>.ndjson.gz
func (w *SwiftWriter) Write(ctx context.Context, events []auditv1.Event) error {
	if len(events) == 0 {
		return nil
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	enc := json.NewEncoder(gz)
	for i := range events {
		if err := enc.Encode(&events[i]); err != nil {
			return fmt.Errorf("encode event: %w", err)
		}
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("gzip close: %w", err)
	}

	now := time.Now().UTC()
	key := fmt.Sprintf("%s%s/%s-%s.ndjson.gz",
		w.Prefix,
		now.Format("2006-01-02"),
		now.Format("20060102T150405Z"),
		uuid.New().String(),
	)

	createOpts := objects.CreateOpts{
		Content:     bytes.NewReader(buf.Bytes()),
		ContentType: "application/gzip",
	}
	if err := objects.Create(ctx, w.Client, w.Container, key, createOpts).Err; err != nil {
		return fmt.Errorf("swift put %s/%s: %w", w.Container, key, err)
	}

	w.Logger.Info("flushed audit events to swift", "container", w.Container, "key", key, "count", len(events))
	return nil
}
