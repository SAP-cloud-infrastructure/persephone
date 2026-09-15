<!--
SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

Persephone exposes the project "Gardener" API directly. Clusters are represented as `Shoot` resources of the `core.gardener.cloud/v1beta1` API group. The upstream Gardener documentation is the authoritative reference for all configuration options.

## Shoot Resource

The Shoot custom resource defines your Kubernetes cluster. It includes the Kubernetes version, worker pool configuration, networking, maintenance windows, and provider-specific infrastructure settings.

- [Shoot Configuration Guide](https://gardener.cloud/docs/getting-started/shoots/) — Getting started with shoot configuration, key concepts, and immutability constraints.
- [Shoot API Reference](https://gardener.cloud/docs/gardener/api-reference/core/#shoot) — Formal API reference for all Shoot fields and types.
- [Complete Shoot Spec Example](https://github.com/gardener/gardener/blob/master/example/90-shoot.yaml) — Annotated example showing every available configuration option.

## OpenStack Provider Configuration

Persephone clusters run on OpenStack. The provider extension documentation covers infrastructure, control plane, and worker configuration specific to OpenStack.

- [OpenStack Provider Usage](https://gardener.cloud/docs/extensions/infrastructure-extensions/gardener-extension-provider-openstack/usage/) — InfrastructureConfig, ControlPlaneConfig, WorkerConfig, and storage options for OpenStack.

## Cluster Lifecycle

- [Cluster Lifecycle](https://gardener.cloud/docs/getting-started/lifecycle/) — How reconciliation works, version classifications, and update behavior.
- [Kubernetes Version Management](https://gardener.cloud/docs/gardener/shoot-operations/shoot_versions/) — Version lifecycle (preview, supported, deprecated, expired) and forced upgrade behavior.
- [Maintenance Windows](https://gardener.cloud/docs/gardener/shoot/shoot_maintenance/) — Configuration of daily maintenance windows and auto-update policies.

## High Availability

- [Control Plane HA](https://gardener.cloud/docs/guides/high-availability/control-plane/) — Node-level and zone-level failure tolerance for the control plane.
- [HA Best Practices](https://gardener.cloud/docs/guides/high-availability/best-practices/) — Recommendations for running highly available workloads across zones.

## Cluster Features

- [Hibernation](https://gardener.cloud/docs/gardener/shoot/shoot_hibernate/) — Shut down worker nodes and control plane to save costs while preserving persistent volumes.
- [Cluster Autoscaler](https://gardener.cloud/docs/gardener/autoscaling/shoot_autoscaling/) — Automatic node scaling based on pod scheduling demands.
- [Credential Rotation](https://gardener.cloud/docs/getting-started/features/credential-rotation/) — Two-phase rotation of certificates, keys, and credentials.
