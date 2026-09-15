<!--
SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

Persephone clusters are configured via the Gardener shoot specification. This page summarizes the key configuration areas and links to the relevant upstream documentation.

## Cluster Basics

| Option | Description | Mutable |
|--------|-------------|---------|
| `metadata.name` | Cluster name (max 11 characters, lowercase, dashes allowed) | No |
| `spec.kubernetes.version` | Kubernetes version (upgrades only, no downgrades) | Yes |
| `spec.purpose` | Cluster purpose: `evaluation`, `development`, `production` | Yes |

## Worker Pools

Worker pools define the compute nodes that run your workloads. You can configure multiple pools with different machine types.

| Option | Description | Mutable |
|--------|-------------|---------|
| `spec.provider.workers[].name` | Worker pool name | No |
| `spec.provider.workers[].machine.type` | OpenStack flavor (for example, `g_c2_m4`) | Yes |
| `spec.provider.workers[].machine.image.name` | OS image (recommended: `gardenlinux`) | Yes |
| `spec.provider.workers[].minimum` | Minimum node count (autoscaler lower bound) | Yes |
| `spec.provider.workers[].maximum` | Maximum node count (autoscaler upper bound) | Yes |
| `spec.provider.workers[].zones` | Availability zones for the pool | Add only |

For details, see [Shoot Configuration Guide — Worker Pools](https://gardener.cloud/docs/getting-started/shoots/).

## Infrastructure (OpenStack)

| Option | Description | Mutable |
|--------|-------------|---------|
| `spec.provider.infrastructureConfig.floatingPoolName` | Floating IP pool for external access | No |
| `spec.provider.infrastructureConfig.networks.workers` | Worker subnet CIDR | No |

For details, see [OpenStack Provider Usage](https://gardener.cloud/docs/extensions/infrastructure-extensions/gardener-extension-provider-openstack/usage/).

## Networking

| Option | Description | Mutable |
|--------|-------------|---------|
| `spec.networking.pods` | Pod CIDR range | No |
| `spec.networking.services` | Service CIDR range | No |
| `spec.networking.nodes` | Node CIDR range | No |

**Warning**: Network CIDR ranges cannot be changed after cluster creation.

## Maintenance

| Option | Description | Mutable |
|--------|-------------|---------|
| `spec.maintenance.timeWindow.begin` | Start of daily maintenance window | Yes |
| `spec.maintenance.timeWindow.end` | End of daily maintenance window (min 30 min, max 6 h) | Yes |
| `spec.maintenance.autoUpdate.kubernetesVersion` | Auto-update Kubernetes patch versions | Yes |
| `spec.maintenance.autoUpdate.machineImageVersion` | Auto-update node OS images | Yes |

For details, see [Maintenance Windows](https://gardener.cloud/docs/gardener/shoot/shoot_maintenance/).

## High Availability

| Option | Description | Mutable |
|--------|-------------|---------|
| `spec.controlPlane.highAvailability.failureTolerance.type` | `node` or `zone` | Enable only (cannot disable) |

**Warning**: High availability cannot be disabled once enabled.

For details, see [Control Plane HA](https://gardener.cloud/docs/guides/high-availability/control-plane/).

## Hibernation

| Option | Description | Mutable |
|--------|-------------|---------|
| `spec.hibernation.enabled` | Hibernate the cluster (scales down nodes and control plane) | Yes |
| `spec.hibernation.schedules` | Cron-based hibernation/wake-up schedules | Yes |

For details, see [Hibernation](https://gardener.cloud/docs/gardener/shoot/shoot_hibernate/).

## Immutability Constraints

The following settings cannot be changed after cluster creation:

- Cluster name
- Infrastructure credentials binding
- Network CIDR ranges (pods, services, nodes)
- Floating IP pool
- High availability mode (cannot be disabled once enabled)
- Availability zones (can be added, never removed)
- Kubernetes version (can be upgraded, never downgraded)
