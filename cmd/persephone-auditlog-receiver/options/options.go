// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package options

import (
	"errors"
	"flag"
	"os"

	"github.com/gophercloud/gophercloud/v2"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// Options contains options for the auditlog receiver command.
type Options struct {
	// Addr is the address the mTLS HTTP server binds to.
	Addr string
	// CertDir is the directory containing TLS certificates.
	CertDir string
	// Container is the Swift container to write objects to.
	Container string
	// Prefix is the object key prefix inside the container.
	Prefix string
	// FlushInterval is the flush interval in seconds.
	FlushIntervalSeconds int
	// MaxEvents is the maximum number of events to buffer before flushing.
	MaxEvents int
	// Zap holds zap logger flags.
	Zap zap.Options
}

// Validate validates the options and fills in OpenStack credentials from env.
func (o *Options) Validate() error {
	if o.Container == "" {
		return errors.New("container flag must be provided")
	}
	return nil
}

// AuthOptions builds gophercloud.AuthOptions from environment variables.
// The receiver gets the cloud-provider secret mounted via the extension actuator,
// which maps OS_* env vars from the shoot's cloud-provider secret.
func AuthOptions() (gophercloud.AuthOptions, error) {
	endpoint := os.Getenv("OS_AUTH_URL")
	if endpoint == "" {
		return gophercloud.AuthOptions{}, errors.New("OS_AUTH_URL must be set")
	}
	appID := os.Getenv("OS_APPLICATION_CREDENTIAL_ID")
	appSecret := os.Getenv("OS_APPLICATION_CREDENTIAL_SECRET")
	if appID == "" || appSecret == "" {
		return gophercloud.AuthOptions{}, errors.New("OS_APPLICATION_CREDENTIAL_ID and OS_APPLICATION_CREDENTIAL_SECRET must be set")
	}
	return gophercloud.AuthOptions{
		IdentityEndpoint:            endpoint,
		ApplicationCredentialID:     appID,
		ApplicationCredentialSecret: appSecret,
	}, nil
}

// AddFlags adds the flags to the default flag set.
func (o *Options) AddFlags() {
	flag.StringVar(&o.Addr, "addr", ":8443",
		"Address the mTLS HTTP server binds to.")
	flag.StringVar(&o.CertDir, "cert-dir", "/certs",
		"Directory containing tls.crt, tls.key, and ca.crt for mTLS.")
	flag.StringVar(&o.Container, "container", "",
		"Swift container to write audit event objects to.")
	flag.StringVar(&o.Prefix, "prefix", "",
		"Object key prefix inside the container (e.g. 'shoot--project--name/').")
	flag.IntVar(&o.FlushIntervalSeconds, "flush-interval", 30,
		"Seconds between periodic flushes to Swift.")
	flag.IntVar(&o.MaxEvents, "max-events", 10000,
		"Maximum buffered events before an immediate flush is triggered.")

	o.Zap = zap.Options{Development: false}
	o.Zap.BindFlags(flag.CommandLine)
}
