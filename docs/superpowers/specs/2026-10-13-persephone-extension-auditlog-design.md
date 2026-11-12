# persephone-extension: Audit Log Shipping Design

## Overview

Add a Gardener extension (`type: persephone-auditlog`) that ships raw Kubernetes audit logs from shoot API servers to OpenStack Swift object storage. The extension runs in the seed, deploys a receiver HTTP service into each opted-in shoot's technical namespace, and automatically wires it into `gardener-extension-auditing` without requiring manual configuration from the user.

## Context and Prior Art

`gardener-extension-auditing` (https://github.com/gardener/gardener-extension-auditing) is an existing Gardener extension that:
- Deploys an `auditlog-forwarder` pod into the shoot's technical namespace in the seed
- Patches the shoot kube-apiserver Deployment (via a mutating webhook) to add `--audit-webhook-*` flags pointing at the forwarder
- Forwards raw `auditv1.EventList` POSTs to one or more HTTP backends

The forwarder's backend URL is fully user-configurable. This design uses it as a transport: `persephone-extension` deploys the receiver, and automatically injects itself as a backend into the `auditing` Extension's `providerConfig`.

The existing `persephone-audit-webhook` is a separate, unrelated component — it receives filtered Shoot CRUD events and publishes CADF events to an AMQP queue. This new extension handles raw, unfiltered audit logs for long-term storage.

## User-Facing Interface

Users declare two extensions in the Shoot spec (both required; the `auditing` entry can have an empty providerConfig — `persephone-extension` fills it in automatically):

```yaml
spec:
  extensions:
  - type: auditing
    providerConfig: {}
  - type: persephone-auditlog
    providerConfig:
      apiVersion: extension.persephone.cloud/v1alpha1
      kind: AuditLogConfig
      objectStore:
        container: shoot-audit-logs
        prefix: "shoots/{{.shootName}}/"   # literal Go template, expanded by actuator
```

No credentials need to be specified — the receiver uses the shoot's existing `cloudprovider` Secret (OpenStack application credentials managed by persephone) to authenticate against Swift.

## Architecture

```
[Shoot kube-apiserver]
  --audit-webhook-config-file (injected by gardener-extension-auditing webhook)
        ↓ mTLS POST /audit
[auditlog-forwarder pod]          (deployed by gardener-extension-auditing)
        ↓ mTLS POST /audit
[persephone-auditlog-receiver pod] (deployed by persephone-extension actuator)
        ↓ gzip NDJSON PUT
[OpenStack Swift]                 (authenticated via cloudprovider secret)
```

All components run in the shoot's technical namespace in the seed (e.g. `shoot--project--name`). No cross-cluster calls; no virtual garden API access required from the extension actuator.

## Components

### 1. `cmd/persephone-extension`

A Gardener extension controller binary. Registers for `ExtensionClass: Shoot`, extension type `persephone-auditlog`, lifecycle `BeforeKubeAPIServer`.

Uses `extensionscontroller.NewReconciler` (or implements `extensionscontroller.Actuator`) backed by the `auditlog` actuator.

### 2. `internal/extension/auditlog/actuator.go`

Implements `extensionscontroller.Actuator`. On `Reconcile`:

1. Decode `ex.Spec.ProviderConfig.Raw` → `AuditLogConfig`
2. Run SecretsManager (`identity: "extension-persephone-auditlog"`) to generate TLS material
3. Deploy receiver Deployment + Service via server-side apply
4. Patch `Extension{type:"auditing"}` providerConfig to add/update the persephone backend

On `Delete`: remove the backend entry from the `auditing` Extension providerConfig, then delete the receiver resources and TLS secrets.

### 3. `internal/extension/auditlog/secrets.go`

SecretsManager config. Three certificates:

| Name | CommonName | Type | Options |
|---|---|---|---|
| `ca-persephone-auditlog` | `ca-persephone-auditlog` | CACert | `Persist()` |
| `persephone-auditlog-receiver-tls` | `persephone-auditlog-receiver` | ServerCert | `SignedByCA("ca-persephone-auditlog")` |
| `persephone-auditlog-forwarder-client-tls` | `auditlog-forwarder` | ClientCert | `SignedByCA("ca-persephone-auditlog", UseCurrentCA)` |

The server cert is mounted into the receiver pod. The client cert secret is referenced by name in the `auditing` Extension providerConfig — the `auditing` extension reads it directly from the shoot namespace (seed-local, no virtual garden access needed).

### 4. `internal/extension/auditlog/types.go`

`AuditLogConfig` CRD type (internal; decoded from `RawExtension`, no CRD registration needed):

```go
type AuditLogConfig struct {
    metav1.TypeMeta
    ObjectStore ObjectStoreConfig
}

type ObjectStoreConfig struct {
    Container string  // Swift container name
    Prefix    string  // object key prefix, may contain "{{.shootName}}"
}
```

### 5. `internal/extension/auditlog/receiver_deploy.go`

Builds the receiver Deployment and Service manifests:
- Deployment: 1 replica, mounts `persephone-auditlog-receiver-tls` as server cert, mounts `cloudprovider` secret for Swift auth
- Service: ClusterIP, port 8443
- Deployed via server-side apply directly to the seed

### 6. `internal/extension/auditlog/patch_auditing.go`

Reads the `auditing` Extension object from the shoot namespace, decodes its providerConfig as `AuditConfiguration`, upserts the persephone backend entry, and patches it back. Idempotent — safe to call on every reconcile.

The backend entry injected:
```go
AuditBackend{
    DeliveryMode: "BestEffort",
    HTTP: &BackendHTTP{
        URL: "https://persephone-auditlog-receiver.<namespace>.svc:8443/audit",
        TLS: TLSConfig{SecretName: "persephone-auditlog-forwarder-client-tls"},
    },
}
```

### 7. `cmd/persephone-auditlog-receiver`

Stateless HTTP server binary:
- Accepts POST `/audit` with `Content-Type: application/json` body of `auditv1.EventList`
- Buffers events in memory; flushes on interval (default 30s) or when buffer reaches 1000 events
- On flush: serializes events as NDJSON, gzip-compresses, PUTs to Swift with key `<prefix><date>/<timestamp>-<uuid>.ndjson.gz`
- Swift auth on startup from env vars (populated from the `cloudprovider` secret via Deployment env)
- mTLS: serves with `persephone-auditlog-receiver-tls` cert, requires client cert from CA

### 8. `internal/receiver/`

- `handler.go` — HTTP handler: decode `EventList`, enqueue to buffer
- `buffer.go` — in-memory ring buffer with flush-on-interval and flush-on-size
- `writer.go` — Swift writer using `gophercloud/v2/openstack/objectstorage/v1/objects`

## TLS Flow

```
persephone-extension SecretsManager generates:
  ca-persephone-auditlog                    (CA, persisted)
  persephone-auditlog-receiver-tls          (server cert, signed by CA)
  persephone-auditlog-forwarder-client-tls  (client cert, signed by CA)

receiver pod mounts:
  persephone-auditlog-receiver-tls → /etc/receiver/tls/{tls.crt, tls.key}
  ca-persephone-auditlog            → /etc/receiver/tls/ca.crt  (to verify forwarder client)

auditing Extension providerConfig references:
  persephone-auditlog-forwarder-client-tls
  → gardener-extension-auditing reads it from the seed namespace by name
  → mounts into forwarder pod, used for outbound mTLS to receiver
```

## Swift Object Layout

```
<container>/
  <prefix>                       e.g. shoots/my-shoot/
    2026-10-13/
      20261013T142301Z-<uuid>.ndjson.gz
      20261013T142331Z-<uuid>.ndjson.gz
    2026-10-14/
      ...
```

Each file contains one NDJSON record per `auditv1.Event`, gzip-compressed.

## Swift Authentication

The receiver reads from env vars injected from the `cloudprovider` Secret:

| Env var | Secret key | Description |
|---|---|---|
| `OS_AUTH_URL` | `authURL` | Keystone endpoint |
| `OS_APPLICATION_CREDENTIAL_ID` | `applicationCredentialID` | App credential ID |
| `OS_APPLICATION_CREDENTIAL_SECRET` | `applicationCredentialSecret` | App credential secret |

The `cloudprovider` secret already exists in the shoot namespace, maintained and rotated by persephone.

## ControllerRegistration

Two `ControllerRegistration` objects (deployed by the persephone operator):

1. `gardener-extension-auditing` — standard registration, no `autoEnable`
2. `persephone-extension` — type `persephone-auditlog`, lifecycle `BeforeKubeAPIServer`

Both are declarative prerequisites; the user adds both extension types to `Shoot.spec.extensions`. Documentation covers this requirement.

## Constraints

- Go module: `github.com/sap-cloud-infrastructure/persephone`
- Gardener: `github.com/gardener/gardener v1.135.2-*`
- gophercloud: `github.com/gophercloud/gophercloud/v2 v2.15.0`
- ExtensionClass: `Shoot` only (Seed class deferred)
- No virtual garden API access from extension actuator — all operations are seed-local
- SPDX license header on all new files: `SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors` + `SPDX-License-Identifier: Apache-2.0`
- English only in all code, comments, docs
- No comments unless the WHY is non-obvious
