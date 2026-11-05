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
	// Bucket is the Ceph S3 bucket to write objects to.
	Bucket string
	// Prefix is the object key prefix inside the bucket.
	Prefix string
	// FlushIntervalSeconds is the flush interval in seconds.
	FlushIntervalSeconds int
	// MaxEvents is the maximum number of events to buffer before flushing.
	MaxEvents int
	// ObjectLockEnabled enables S3 Object Lock on the bucket.
	ObjectLockEnabled bool
	// ObjectLockRetentionDays is the number of days to retain locked objects.
	ObjectLockRetentionDays int
	// ObjectLockMode is the Object Lock retention mode: "COMPLIANCE" or "GOVERNANCE".
	ObjectLockMode string
	// Zap holds zap logger flags.
	Zap zap.Options
}

// Validate validates the options.
func (o *Options) Validate() error {
	if o.Bucket == "" {
		return errors.New("bucket flag must be provided")
	}
	if o.ObjectLockEnabled {
		if o.ObjectLockRetentionDays <= 0 {
			return errors.New("object-lock-retention-days must be positive when object lock is enabled")
		}
		if o.ObjectLockMode != "COMPLIANCE" && o.ObjectLockMode != "GOVERNANCE" {
			return errors.New("object-lock-mode must be COMPLIANCE or GOVERNANCE")
		}
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
	flag.StringVar(&o.Bucket, "bucket", "",
		"Ceph S3 bucket to write audit event objects to.")
	flag.StringVar(&o.Prefix, "prefix", "",
		"Object key prefix inside the bucket (e.g. 'shoot--project--name/').")
	flag.IntVar(&o.FlushIntervalSeconds, "flush-interval", 300,
		"Seconds between periodic flushes.")
	flag.IntVar(&o.MaxEvents, "max-events", 10000,
		"Maximum buffered events before an immediate flush is triggered.")
	flag.BoolVar(&o.ObjectLockEnabled, "object-lock", false,
		"Enable S3 Object Lock (WORM) on the bucket.")
	flag.IntVar(&o.ObjectLockRetentionDays, "object-lock-retention-days", 0,
		"Number of days to retain locked objects.")
	flag.StringVar(&o.ObjectLockMode, "object-lock-mode", "COMPLIANCE",
		"Object Lock retention mode: COMPLIANCE or GOVERNANCE.")

	o.Zap = zap.Options{Development: false}
	o.Zap.BindFlags(flag.CommandLine)
}
