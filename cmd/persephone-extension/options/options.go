// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package options

import (
	"errors"
	"flag"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// Options holds command-line options for the extension binary.
type Options struct {
	ReceiverImage string
	Health        struct{ Addr string }
	Metrics       struct{ Addr string }
	Zap           zap.Options
}

// AddFlags registers flags on the default flag set.
func (o *Options) AddFlags() {
	flag.StringVar(&o.ReceiverImage, "receiver-image", "", "Image for the auditlog-receiver (required)")
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
