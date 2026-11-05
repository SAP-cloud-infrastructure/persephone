// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package options

import (
	"errors"
	"flag"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const DefaultS3EndpointTemplate = "https://rgw.st1.{region}.cloud.sap"

// Options holds command-line options for the extension binary.
type Options struct {
	ReceiverImage string
	// S3EndpointTemplate is used to build the per-shoot Ceph S3 endpoint URL.
	// Use "{region}" as placeholder; it is replaced with the shoot's region at reconcile time.
	S3EndpointTemplate string
	Health             struct{ Addr string }
	Metrics            struct{ Addr string }
	Zap                zap.Options
}

// AddFlags registers flags on the default flag set.
func (o *Options) AddFlags() {
	flag.StringVar(&o.ReceiverImage, "receiver-image", "", "Image for the auditlog-receiver (required)")
	flag.StringVar(&o.S3EndpointTemplate, "s3-endpoint-template", DefaultS3EndpointTemplate,
		`Template for the Ceph S3 endpoint URL; "{region}" is replaced with the shoot region.`)
	flag.StringVar(&o.Health.Addr, "health-bind-address", ":8081", "Address for health probes")
	flag.StringVar(&o.Metrics.Addr, "metrics-bind-address", ":8080", "Address for metrics")
	o.Zap.BindFlags(flag.CommandLine)
}

// Validate returns an error if required options are missing.
func (o *Options) Validate() error {
	if o.ReceiverImage == "" {
		return errors.New("--receiver-image is required")
	}
	return nil
}
