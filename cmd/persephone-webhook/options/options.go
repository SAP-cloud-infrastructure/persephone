// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package options

import (
	"errors"
	"flag"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// Options contains options for this command.
type Options struct {
	// ConfigFile points to a YAML config file containing OpenStack identity endpoints and application credentials by
	// region.
	ConfigFile string
	// CreateGardenerResources controls whether Gardener resources should be created after successful token validation.
	CreateGardenerResources bool
	// LeaderElect controls whether leader election should be performed.
	LeaderElect bool
	// Health contains settings for the health probes.
	Health HealthOptions
	// Metrics contains settings for the metrics.
	Metrics MetricsOptions
	// Zap holds the zap logger flags (-zap-log-level, -zap-devel, -zap-encoder, ...).
	// Bound on AddFlags and consumed by main to construct the root logger.
	Zap zap.Options
}

// MetricsOptions contains settings for the metrics.
type MetricsOptions struct {
	// Addr is the address the metrics endpoint binds to.
	Addr string
}

// HealthOptions contains settings for the health probes.
type HealthOptions struct {
	// Addr is the address the health probe endpoint binds to.
	Addr string
}

// Validate validates the options.
func (o *Options) Validate() error {
	if o.ConfigFile == "" {
		return errors.New("config flag must be provided")
	}

	return nil
}

// AddFlags adds the flags to the default flag set.
func (o *Options) AddFlags() {
	flag.StringVar(&o.ConfigFile, "config", "",
		"yaml config containing OpenStack identity endpoints and application credentials by region.")
	flag.BoolVar(&o.CreateGardenerResources, "gardener-create-resources", true,
		"Create Gardener resources after successful token validation.")
	flag.BoolVar(&o.LeaderElect, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	flag.StringVar(&o.Health.Addr, "health-probe-bind-address", ":8081",
		"The address the probe endpoint binds to.")

	flag.StringVar(&o.Metrics.Addr, "metrics-bind-address", "0",
		"The address the metrics endpoint binds to. "+
			"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")

	o.Zap = zap.Options{Development: false}
	o.Zap.BindFlags(flag.CommandLine)
}
