<!--
SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

[![REUSE status](https://api.reuse.software/badge/github.com/SAP-cloud-infrastructure/persephone)](https://api.reuse.software/info/github.com/SAP-cloud-infrastructure/persephone)

# Project Persephone

## About this project

Kubernetes-as-a-Service operator for SAP Cloud Infrastructure. Automates the provisioning and lifecycle management of Gardener-managed Kubernetes clusters on OpenStack. It provides fully managed, CNCF-conformant Kubernetes clusters where the control plane is operated by the service and worker nodes run in the user (customer) OpenStack project.

Project Persephone is powered by [project "Gardener"](https://gardener.cloud/).

### Architecture

Persephone uses a split-responsibility model:

- **Control plane**: Runs on central seed clusters managed by Persephone. Users do not have direct access to the control plane infrastructure.
- **Worker nodes**: Run as compute instances in the user OpenStack project. Worker nodes consume quota within user projects like any other VM. User workloads execute within user projects alongside any other resources in the project.

This separation means that the Kubernetes API server, etcd, scheduler, and controller manager are fully managed. Users interact with their cluster through the Kubernetes API (via `kubectl`) as with any other Kubernetes cluster.

### Responsibility Model

| Responsibility | Managed by Persephone | Managed by users |
|---|---|---|
| Control plane availability | Yes | |
| Kubernetes version upgrades | Yes | |
| Certificate rotation | Yes | |
| Node OS updates | Yes | |
| Worker node provisioning | Yes | |
| Cluster configuration (shoot spec) | | Yes |
| Workload deployment and management | | Yes |
| Application security | | Yes |
| Resource quotas and limits | | Yes |

### Gardener Under the Hood

Persephone is built on [project "Gardener"](https://gardener.cloud/), an open-source Kubernetes cluster management system. Gardener's shoot specification is directly exposed — users can customize their cluster using the full range of options that Gardener supports for OpenStack.

Key Gardener concepts relevant to Persephone users:

- **Shoot**: Your Kubernetes cluster, represented as a declarative specification.
- **Worker pool**: A group of nodes with the same machine type, OS image, and scaling configuration.
- **Maintenance window**: A daily time window during which automatic reconciliation and updates occur.

## Requirements and Setup

Before users can create a Persephone Kubernetes cluster, they must ensure the following prerequisites are met.

### OpenStack Project

You need an active OpenStack project on SAP Cloud Infrastructure. Persephone creates worker nodes and networking resources in your project.

### Roles

You need the `kubernetes_admin` role assigned in the project where you want to create clusters.

### Quota

Your project needs sufficient quota for the resources that Persephone provisions:

- **Compute**: Instances for worker nodes (depends on your worker pool configuration)
- **Networking**: Networks, subnets, routers, floating IPs, and security groups
- **Storage**: Volumes for node root disks

The exact quota requirements depend on the number and size of worker nodes in your cluster.

## Support, Feedback, Contributing

This project is open to feature requests/suggestions, bug reports etc. via [GitHub issues](https://github.com/SAP-cloud-infrastructure/persephone/issues). Contribution and feedback are encouraged and always welcome. For more information about how to contribute, the project structure, as well as additional contribution information, see our [Contribution Guidelines](CONTRIBUTING.md).

## Security / Disclosure
If you find any bug that may be a security problem, please follow our instructions at [in our security policy](https://github.com/SAP-cloud-infrastructure/persephone/security/policy) on how to report it. Please do not create GitHub issues for security-related doubts or problems.

## Code of Conduct

We as members, contributors, and leaders pledge to make participation in our community a harassment-free experience for everyone. By participating in this project, you agree to abide by its [Code of Conduct](https://github.com/SAP/.github/blob/main/CODE_OF_CONDUCT.md) at all times.

## Licensing

Copyright 2026 SAP SE or an SAP affiliate company and persephone contributors. Please see our [LICENSE](LICENSE) for copyright and license information. Detailed information including third-party components and their licensing/copyright information is available [via the REUSE tool](https://api.reuse.software/info/github.com/SAP-cloud-infrastructure/persephone).
