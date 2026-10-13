# persephone-extension: Audit Log Shipping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a Gardener extension (`type: persephone-auditlog`) that ships raw shoot API server audit logs to OpenStack Swift, automatically wiring into `gardener-extension-auditing`.

**Architecture:** A new `persephone-extension` controller binary reconciles `Extension{type:"persephone-auditlog"}` objects in shoot namespaces. It generates TLS via Gardener's SecretsManager, deploys a `persephone-auditlog-receiver` HTTP service into each shoot's technical namespace, and patches the `auditing` Extension's providerConfig to route the `auditlog-forwarder` at the receiver. The receiver buffers incoming `EventList` POSTs and flushes gzip-compressed NDJSON to Swift using the shoot's existing `cloudprovider` credentials.

**Tech Stack:** Go, controller-runtime, `github.com/gardener/gardener/extensions/pkg`, `github.com/gardener/gardener/pkg/utils/secrets/manager`, `github.com/gophercloud/gophercloud/v2/openstack/objectstorage/v1/objects`, `k8s.io/apiserver/pkg/apis/audit/v1`

**Spec:** `docs/superpowers/specs/2026-10-13-persephone-extension-auditlog-design.md`

## Global Constraints

- Go module path: `github.com/sap-cloud-infrastructure/persephone`
- Gardener: `github.com/gardener/gardener v1.135.2-*` — already in go.mod
- gophercloud: `github.com/gophercloud/gophercloud/v2 v2.15.0` — already in go.mod
- ExtensionClass: `Shoot` only
- All new files need SPDX header:
  ```
  // SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
  //
  // SPDX-License-Identifier: Apache-2.0
  ```
- Follow existing package layout: `internal/` for library code, `cmd/` for binaries
- No comments unless the WHY is non-obvious
- Tests use Ginkgo/Gomega (see existing `*_suite_test.go` files for bootstrap pattern)
- Run `go build ./...` after each task to catch import/compile errors early

---

### Task 1: AuditLogConfig types and providerConfig decoding

**Files:**
- Create: `internal/extension/auditlog/types.go`
- Create: `internal/extension/auditlog/types_test.go`
- Create: `internal/extension/auditlog/auditlog_suite_test.go`

**Interfaces:**
- Produces:
  - `AuditLogConfig` struct with `ObjectStore ObjectStoreConfig`
  - `ObjectStoreConfig` struct with `Container string`, `Prefix string`
  - `func DecodeAuditLogConfig(raw *runtime.RawExtension) (*AuditLogConfig, error)`

- [ ] **Step 1: Create the suite test file**

```go
// internal/extension/auditlog/auditlog_suite_test.go
package auditlog_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAuditlog(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Auditlog Suite")
}
```

- [ ] **Step 2: Write failing test for DecodeAuditLogConfig**

```go
// internal/extension/auditlog/types_test.go
package auditlog_test

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/sap-cloud-infrastructure/persephone/internal/extension/auditlog"
)

var _ = Describe("DecodeAuditLogConfig", func() {
	It("decodes a valid providerConfig", func() {
		raw, err := json.Marshal(map[string]any{
			"apiVersion": "extension.persephone.cloud/v1alpha1",
			"kind":       "AuditLogConfig",
			"objectStore": map[string]any{
				"container": "shoot-audit-logs",
				"prefix":    "shoots/my-shoot/",
			},
		})
		Expect(err).NotTo(HaveOccurred())

		cfg, err := auditlog.DecodeAuditLogConfig(&runtime.RawExtension{Raw: raw})
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.ObjectStore.Container).To(Equal("shoot-audit-logs"))
		Expect(cfg.ObjectStore.Prefix).To(Equal("shoots/my-shoot/"))
	})

	It("returns error for nil raw extension", func() {
		_, err := auditlog.DecodeAuditLogConfig(nil)
		Expect(err).To(HaveOccurred())
	})

	It("returns error for invalid JSON", func() {
		_, err := auditlog.DecodeAuditLogConfig(&runtime.RawExtension{Raw: []byte(`not json`)})
		Expect(err).To(HaveOccurred())
	})
})
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./internal/extension/auditlog/... -v
```
Expected: compile error ("auditlog" package does not exist)

- [ ] **Step 4: Create types.go**

```go
// internal/extension/auditlog/types.go
package auditlog

import (
	"encoding/json"
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// AuditLogConfig is the providerConfig for the persephone-auditlog extension.
type AuditLogConfig struct {
	metav1.TypeMeta `json:",inline"`
	ObjectStore     ObjectStoreConfig `json:"objectStore"`
}

// ObjectStoreConfig configures the Swift object store destination.
type ObjectStoreConfig struct {
	Container string `json:"container"`
	// Prefix is the object key prefix. The literal string "{{.shootName}}" is
	// replaced with the shoot name by the actuator before passing to the receiver.
	Prefix string `json:"prefix"`
}

// DecodeAuditLogConfig decodes an AuditLogConfig from a RawExtension.
func DecodeAuditLogConfig(raw *runtime.RawExtension) (*AuditLogConfig, error) {
	if raw == nil {
		return nil, errors.New("providerConfig is nil")
	}
	cfg := &AuditLogConfig{}
	if err := json.Unmarshal(raw.Raw, cfg); err != nil {
		return nil, fmt.Errorf("failed to decode AuditLogConfig: %w", err)
	}
	return cfg, nil
}
```

Note: add `"fmt"` to imports.

- [ ] **Step 5: Run test to verify it passes**

```bash
go test ./internal/extension/auditlog/... -v
```
Expected: all specs pass

- [ ] **Step 6: Verify build**

```bash
go build ./...
```

- [ ] **Step 7: Commit**

```bash
git add internal/extension/auditlog/
git commit -m "feat(extension): add AuditLogConfig types and providerConfig decoding"
```

---

### Task 2: SecretsManager certificate configuration

**Files:**
- Create: `internal/extension/auditlog/secrets.go`
- Create: `internal/extension/auditlog/secrets_test.go`

**Interfaces:**
- Consumes: `extensionssecretsmanager "github.com/gardener/gardener/extensions/pkg/util/secret/manager"`
- Produces:
  - `const (CASecretName, ReceiverTLSSecretName, ForwarderClientTLSSecretName string)`
  - `func SecretConfigs(namespace string) []extensionssecretsmanager.SecretConfigWithOptions`

- [ ] **Step 1: Write failing test**

```go
// internal/extension/auditlog/secrets_test.go
package auditlog_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	secretsutils "github.com/gardener/gardener/pkg/utils/secrets"

	"github.com/sap-cloud-infrastructure/persephone/internal/extension/auditlog"
)

var _ = Describe("SecretConfigs", func() {
	It("returns three configs with correct names", func() {
		configs := auditlog.SecretConfigs("shoot--proj--name")
		Expect(configs).To(HaveLen(3))

		names := make([]string, 0, 3)
		for _, c := range configs {
			names = append(names, c.Config.GetName())
		}
		Expect(names).To(ConsistOf(
			auditlog.CASecretName,
			auditlog.ReceiverTLSSecretName,
			auditlog.ForwarderClientTLSSecretName,
		))
	})

	It("CA config is CACert type", func() {
		configs := auditlog.SecretConfigs("shoot--proj--name")
		for _, c := range configs {
			if c.Config.GetName() == auditlog.CASecretName {
				certCfg, ok := c.Config.(*secretsutils.CertificateSecretConfig)
				Expect(ok).To(BeTrue())
				Expect(certCfg.CertType).To(Equal(secretsutils.CACert))
			}
		}
	})

	It("receiver TLS config has correct DNS names for given namespace", func() {
		configs := auditlog.SecretConfigs("shoot--proj--name")
		for _, c := range configs {
			if c.Config.GetName() == auditlog.ReceiverTLSSecretName {
				certCfg, ok := c.Config.(*secretsutils.CertificateSecretConfig)
				Expect(ok).To(BeTrue())
				Expect(certCfg.DNSNames).To(ContainElement("persephone-auditlog-receiver.shoot--proj--name.svc"))
			}
		}
	})
})
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/extension/auditlog/... -v
```
Expected: compile error (missing constants and function)

- [ ] **Step 3: Create secrets.go**

```go
// internal/extension/auditlog/secrets.go
package auditlog

import (
	extensionssecretsmanager "github.com/gardener/gardener/extensions/pkg/util/secret/manager"
	secretsutils "github.com/gardener/gardener/pkg/utils/secrets"
	secretsmanager "github.com/gardener/gardener/pkg/utils/secrets/manager"
)

const (
	// ManagerIdentity is the SecretsManager identity for this extension.
	ManagerIdentity = "extension-persephone-auditlog"

	// CASecretName is the name of the self-signed CA secret.
	CASecretName = "ca-persephone-auditlog"
	// ReceiverTLSSecretName is the name of the receiver's server TLS secret.
	ReceiverTLSSecretName = "persephone-auditlog-receiver-tls"
	// ForwarderClientTLSSecretName is the name of the client TLS secret mounted
	// into the auditlog-forwarder for mTLS to the receiver.
	ForwarderClientTLSSecretName = "persephone-auditlog-forwarder-client-tls"
)

// SecretConfigs returns the SecretsManager configs for the auditlog extension.
func SecretConfigs(namespace string) []extensionssecretsmanager.SecretConfigWithOptions {
	return []extensionssecretsmanager.SecretConfigWithOptions{
		{
			Config: &secretsutils.CertificateSecretConfig{
				Name:       CASecretName,
				CommonName: CASecretName,
				CertType:   secretsutils.CACert,
			},
			Options: []secretsmanager.GenerateOption{secretsmanager.Persist()},
		},
		{
			Config: &secretsutils.CertificateSecretConfig{
				Name:       ReceiverTLSSecretName,
				CommonName: "persephone-auditlog-receiver",
				DNSNames: []string{
					ReceiverServiceName + "." + namespace + ".svc",
					ReceiverServiceName + "." + namespace + ".svc.cluster.local",
					ReceiverServiceName,
				},
				CertType: secretsutils.ServerCert,
			},
			Options: []secretsmanager.GenerateOption{secretsmanager.SignedByCA(CASecretName)},
		},
		{
			Config: &secretsutils.CertificateSecretConfig{
				Name:       ForwarderClientTLSSecretName,
				CommonName: "auditlog-forwarder",
				CertType:   secretsutils.ClientCert,
			},
			Options: []secretsmanager.GenerateOption{
				secretsmanager.SignedByCA(CASecretName, secretsmanager.UseCurrentCA),
			},
		},
	}
}
```

- [ ] **Step 4: Add `ReceiverServiceName` constant to types.go**

Add to `internal/extension/auditlog/types.go`:
```go
const (
	// ReceiverServiceName is the Kubernetes Service name for the auditlog receiver.
	ReceiverServiceName = "persephone-auditlog-receiver"
)
```

- [ ] **Step 5: Run test to verify it passes**

```bash
go test ./internal/extension/auditlog/... -v
```

- [ ] **Step 6: Verify build**

```bash
go build ./...
```

- [ ] **Step 7: Commit**

```bash
git add internal/extension/auditlog/secrets.go internal/extension/auditlog/secrets_test.go internal/extension/auditlog/types.go
git commit -m "feat(extension): add SecretsManager cert configs for auditlog extension"
```

---

### Task 3: Receiver HTTP handler and buffer

**Files:**
- Create: `internal/receiver/handler.go`
- Create: `internal/receiver/buffer.go`
- Create: `internal/receiver/receiver_suite_test.go`
- Create: `internal/receiver/handler_test.go`
- Create: `internal/receiver/buffer_test.go`

**Interfaces:**
- Produces:
  - `type Buffer struct` with `func NewBuffer(flushInterval time.Duration, maxEvents int, flushFn func([]auditv1.Event)) *Buffer`
  - `func (b *Buffer) Add(events []auditv1.Event)`
  - `func (b *Buffer) Start(ctx context.Context)`
  - `type Handler struct { Logger logr.Logger; Buffer *Buffer }`
  - `func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request)`

- [ ] **Step 1: Create suite test file**

```go
// internal/receiver/receiver_suite_test.go
package receiver_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestReceiver(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Receiver Suite")
}
```

- [ ] **Step 2: Write failing buffer test**

```go
// internal/receiver/buffer_test.go
package receiver_test

import (
	"context"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"github.com/sap-cloud-infrastructure/persephone/internal/receiver"
)

var _ = Describe("Buffer", func() {
	It("flushes when max events is reached", func() {
		var mu sync.Mutex
		var flushed []auditv1.Event

		buf := receiver.NewBuffer(10*time.Second, 3, func(events []auditv1.Event) {
			mu.Lock()
			flushed = append(flushed, events...)
			mu.Unlock()
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go buf.Start(ctx)

		buf.Add([]auditv1.Event{{}, {}, {}})

		Eventually(func() int {
			mu.Lock()
			defer mu.Unlock()
			return len(flushed)
		}).Should(Equal(3))
	})

	It("flushes on interval", func() {
		var mu sync.Mutex
		var flushed []auditv1.Event

		buf := receiver.NewBuffer(50*time.Millisecond, 1000, func(events []auditv1.Event) {
			mu.Lock()
			flushed = append(flushed, events...)
			mu.Unlock()
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go buf.Start(ctx)

		buf.Add([]auditv1.Event{{}, {}})

		Eventually(func() int {
			mu.Lock()
			defer mu.Unlock()
			return len(flushed)
		}, 500*time.Millisecond).Should(Equal(2))
	})

	It("does not flush empty buffer", func() {
		flushCount := 0
		buf := receiver.NewBuffer(20*time.Millisecond, 1000, func(_ []auditv1.Event) {
			flushCount++
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go buf.Start(ctx)

		time.Sleep(100 * time.Millisecond)
		Expect(flushCount).To(Equal(0))
	})
})
```

- [ ] **Step 3: Write failing handler test**

```go
// internal/receiver/handler_test.go
package receiver_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/go-logr/logr"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"github.com/sap-cloud-infrastructure/persephone/internal/receiver"
)

var _ = Describe("Handler", func() {
	It("returns 200 and enqueues events for valid EventList", func() {
		var mu sync.Mutex
		var received []auditv1.Event

		buf := receiver.NewBuffer(10*time.Second, 1000, func(events []auditv1.Event) {
			mu.Lock()
			received = append(received, events...)
			mu.Unlock()
		})

		h := &receiver.Handler{
			Logger: logr.Discard(),
			Buffer: buf,
		}

		eventList := auditv1.EventList{
			Items: []auditv1.Event{{AuditID: "abc123"}},
		}
		body, _ := json.Marshal(eventList)
		req := httptest.NewRequest(http.MethodPost, "/audit", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))
		Eventually(func() int {
			mu.Lock()
			defer mu.Unlock()
			return len(received)
		}).Should(Equal(1))
	})

	It("returns 400 for invalid JSON body", func() {
		buf := receiver.NewBuffer(10*time.Second, 1000, func(_ []auditv1.Event) {})
		h := &receiver.Handler{Logger: logr.Discard(), Buffer: buf}

		req := httptest.NewRequest(http.MethodPost, "/audit", bytes.NewReader([]byte("not json")))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusBadRequest))
	})
})
```

- [ ] **Step 4: Run tests to verify they fail**

```bash
go test ./internal/receiver/... -v
```
Expected: compile error (package does not exist)

- [ ] **Step 5: Create buffer.go**

```go
// internal/receiver/buffer.go
package receiver

import (
	"context"
	"sync"
	"time"

	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
)

// Buffer accumulates audit events and flushes them either when a size
// threshold is reached or on a periodic interval.
type Buffer struct {
	flushInterval time.Duration
	maxEvents     int
	flushFn       func([]auditv1.Event)

	mu     sync.Mutex
	events []auditv1.Event
	trigger chan struct{}
}

// NewBuffer creates a new Buffer. flushFn is called with a snapshot of
// accumulated events; it must not modify the slice.
func NewBuffer(flushInterval time.Duration, maxEvents int, flushFn func([]auditv1.Event)) *Buffer {
	return &Buffer{
		flushInterval: flushInterval,
		maxEvents:     maxEvents,
		flushFn:       flushFn,
		trigger:       make(chan struct{}, 1),
	}
}

// Add appends events to the buffer and triggers an immediate flush if
// maxEvents is reached.
func (b *Buffer) Add(events []auditv1.Event) {
	b.mu.Lock()
	b.events = append(b.events, events...)
	shouldFlush := len(b.events) >= b.maxEvents
	b.mu.Unlock()

	if shouldFlush {
		select {
		case b.trigger <- struct{}{}:
		default:
		}
	}
}

// Start runs the flush loop until ctx is cancelled. Call in a goroutine.
func (b *Buffer) Start(ctx context.Context) {
	ticker := time.NewTicker(b.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			b.flush()
			return
		case <-ticker.C:
			b.flush()
		case <-b.trigger:
			b.flush()
		}
	}
}

func (b *Buffer) flush() {
	b.mu.Lock()
	if len(b.events) == 0 {
		b.mu.Unlock()
		return
	}
	snapshot := b.events
	b.events = nil
	b.mu.Unlock()

	b.flushFn(snapshot)
}
```

- [ ] **Step 6: Create handler.go**

```go
// internal/receiver/handler.go
package receiver

import (
	"encoding/json"
	"net/http"

	"github.com/go-logr/logr"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
)

const AuditPath = "/audit"

// Handler accepts POST /audit requests containing an auditv1.EventList
// and enqueues the events into the buffer.
type Handler struct {
	Logger logr.Logger
	Buffer *Buffer
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var eventList auditv1.EventList
	if err := json.NewDecoder(r.Body).Decode(&eventList); err != nil {
		h.Logger.Error(err, "failed to decode EventList")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	h.Buffer.Add(eventList.Items)
	w.WriteHeader(http.StatusOK)
}
```

- [ ] **Step 7: Run tests to verify they pass**

```bash
go test ./internal/receiver/... -v
```

- [ ] **Step 8: Verify build**

```bash
go build ./...
```

- [ ] **Step 9: Commit**

```bash
git add internal/receiver/
git commit -m "feat(receiver): add HTTP handler and event buffer"
```

---

### Task 4: Swift writer

**Files:**
- Create: `internal/receiver/writer.go`
- Create: `internal/receiver/writer_test.go`

**Interfaces:**
- Consumes: `*Buffer` from Task 3
- Produces:
  - `type SwiftWriter struct`
  - `func NewSwiftWriter(ctx context.Context, authURL, appCredID, appCredSecret, container, prefix string) (*SwiftWriter, error)`
  - `func (w *SwiftWriter) Write(events []auditv1.Event) error`

- [ ] **Step 1: Write failing test**

```go
// internal/receiver/writer_test.go
package receiver_test

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/gophercloud/gophercloud/v2"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/sap-cloud-infrastructure/persephone/internal/receiver"
)

var _ = Describe("SwiftWriter", func() {
	It("writes gzip-compressed NDJSON to Swift", func() {
		var receivedBody []byte
		var receivedPath string

		// Minimal fake Swift server: handles token issuance and object PUT.
		mux := http.NewServeMux()
		var server *httptest.Server

		mux.HandleFunc("/v3/auth/tokens", func(w http.ResponseWriter, r *http.Request) {
			// Return a minimal token response pointing back at our test server.
			w.Header().Set("X-Subject-Token", "test-token")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"token": map[string]any{
					"catalog": []map[string]any{
						{
							"type": "object-store",
							"endpoints": []map[string]any{
								{"interface": "public", "url": server.URL + "/v1/AUTH_test", "region": "test"},
							},
						},
					},
				},
			})
		})
		mux.HandleFunc("/v1/AUTH_test/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPut {
				receivedPath = r.URL.Path
				receivedBody, _ = io.ReadAll(r.Body)
				w.WriteHeader(http.StatusCreated)
			}
		})

		server = httptest.NewServer(mux)
		defer server.Close()

		w, err := receiver.NewSwiftWriter(GinkgoT().Context(), server.URL, "cred-id", "cred-secret", "my-container", "shoots/test/")
		Expect(err).NotTo(HaveOccurred())

		now := metav1.NewTime(time.Date(2026, 10, 13, 14, 23, 1, 0, time.UTC))
		events := []auditv1.Event{{AuditID: "ev1", RequestReceivedTimestamp: now}}
		Expect(w.Write(events)).To(Succeed())

		Expect(receivedPath).To(ContainSubstring("my-container/shoots/test/2026-10-13/"))
		Expect(receivedPath).To(HaveSuffix(".ndjson.gz"))

		gr, err := gzip.NewReader(strings.NewReader(string(receivedBody)))
		Expect(err).NotTo(HaveOccurred())
		data, err := io.ReadAll(gr)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(ContainSubstring(`"auditID":"ev1"`))
	})
})
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/receiver/... -run TestReceiver/SwiftWriter -v
```
Expected: compile error (SwiftWriter not defined)

- [ ] **Step 3: Create writer.go**

```go
// internal/receiver/writer.go
package receiver

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/objectstorage/v1/objects"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
)

// SwiftWriter writes gzip-compressed NDJSON audit event batches to Swift.
type SwiftWriter struct {
	client    *gophercloud.ServiceClient
	container string
	prefix    string
}

// NewSwiftWriter authenticates against Keystone and returns a writer ready to PUT objects.
func NewSwiftWriter(ctx context.Context, authURL, appCredID, appCredSecret, container, prefix string) (*SwiftWriter, error) {
	provider, err := openstack.AuthenticatedClient(ctx, gophercloud.AuthOptions{
		IdentityEndpoint:            authURL,
		ApplicationCredentialID:     appCredID,
		ApplicationCredentialSecret: appCredSecret,
		AllowReauth:                 true,
	})
	if err != nil {
		return nil, fmt.Errorf("swift auth failed: %w", err)
	}

	client, err := openstack.NewObjectStorageV1(provider, gophercloud.EndpointOpts{})
	if err != nil {
		return nil, fmt.Errorf("failed to create object storage client: %w", err)
	}

	return &SwiftWriter{client: client, container: container, prefix: prefix}, nil
}

// Write serializes events as NDJSON, gzip-compresses, and PUTs to Swift.
// The object key is <prefix><YYYY-MM-DD>/<timestamp>-<uuid>.ndjson.gz.
func (w *SwiftWriter) Write(events []auditv1.Event) error {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)

	enc := json.NewEncoder(gz)
	for i := range events {
		if err := enc.Encode(&events[i]); err != nil {
			return fmt.Errorf("failed to encode event: %w", err)
		}
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("failed to finalize gzip: %w", err)
	}

	now := time.Now().UTC()
	objectName := fmt.Sprintf("%s%s/%s-%s.ndjson.gz",
		w.prefix,
		now.Format("2006-01-02"),
		now.Format("20060102T150405Z"),
		uuid.New().String(),
	)

	result := objects.Create(context.Background(), w.client, w.container, objectName, objects.CreateOpts{
		Content:         &buf,
		ContentType:     "application/gzip",
		ContentEncoding: "gzip",
	})
	if result.Err != nil {
		return fmt.Errorf("failed to PUT object %s: %w", objectName, result.Err)
	}
	return nil
}
```

- [ ] **Step 4: Add google/uuid dependency**

```bash
go get github.com/google/uuid
go mod tidy
```

- [ ] **Step 5: Run test to verify it passes**

```bash
go test ./internal/receiver/... -v
```

- [ ] **Step 6: Verify build**

```bash
go build ./...
```

- [ ] **Step 7: Commit**

```bash
git add internal/receiver/writer.go internal/receiver/writer_test.go go.mod go.sum
git commit -m "feat(receiver): add Swift writer for gzip-compressed NDJSON audit logs"
```

---

### Task 5: Receiver binary

**Files:**
- Create: `cmd/persephone-auditlog-receiver/main.go`
- Create: `cmd/persephone-auditlog-receiver/options/options.go`

**Interfaces:**
- Consumes: `internal/receiver` from Tasks 3 and 4
- Produces: runnable binary `persephone-auditlog-receiver`

- [ ] **Step 1: Create options.go**

```go
// cmd/persephone-auditlog-receiver/options/options.go
package options

import (
	"errors"
	"flag"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// Options holds command-line options for the receiver binary.
type Options struct {
	Addr      string
	CertDir   string
	Container string
	Prefix    string
	Zap       zap.Options
}

// AddFlags registers flags on the default flag set.
func (o *Options) AddFlags() {
	flag.StringVar(&o.Addr, "addr", ":8443", "Address to listen on")
	flag.StringVar(&o.CertDir, "cert-dir", "/etc/receiver/tls", "Directory containing tls.crt, tls.key, and ca.crt")
	flag.StringVar(&o.Container, "container", "", "Swift container name (required)")
	flag.StringVar(&o.Prefix, "prefix", "", "Object key prefix (required)")
	o.Zap.BindFlags(flag.CommandLine)
}

// Validate returns an error if required options are missing.
func (o *Options) Validate() error {
	if o.Container == "" {
		return errors.New("--container is required")
	}
	if o.Prefix == "" {
		return errors.New("--prefix is required")
	}
	return nil
}
```

- [ ] **Step 2: Create main.go**

```go
// cmd/persephone-auditlog-receiver/main.go
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/sapcc/go-api-declarations/bininfo"
	"k8s.io/klog/v2"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	secretsutils "github.com/gardener/gardener/pkg/utils/secrets"

	"github.com/sap-cloud-infrastructure/persephone/cmd/persephone-auditlog-receiver/options"
	"github.com/sap-cloud-infrastructure/persephone/internal/receiver"
)

func main() {
	bininfo.HandleVersionArgument()
	opts := &options.Options{}
	opts.AddFlags()
	flag.Parse()

	log := zap.New(zap.UseFlagOptions(&opts.Zap)).WithName("auditlog-receiver")
	logf.SetLogger(log)
	klog.SetLogger(log)

	if err := run(signals.SetupSignalHandler(), opts); err != nil {
		panic(err)
	}
}

func run(ctx context.Context, opts *options.Options) error {
	if err := opts.Validate(); err != nil {
		return fmt.Errorf("invalid options: %w", err)
	}

	authURL := os.Getenv("OS_AUTH_URL")
	appCredID := os.Getenv("OS_APPLICATION_CREDENTIAL_ID")
	appCredSecret := os.Getenv("OS_APPLICATION_CREDENTIAL_SECRET")
	if authURL == "" || appCredID == "" || appCredSecret == "" {
		return errors.New("OS_AUTH_URL, OS_APPLICATION_CREDENTIAL_ID and OS_APPLICATION_CREDENTIAL_SECRET must be set")
	}

	w, err := receiver.NewSwiftWriter(ctx, authURL, appCredID, appCredSecret, opts.Container, opts.Prefix)
	if err != nil {
		return fmt.Errorf("failed to create Swift writer: %w", err)
	}

	buf := receiver.NewBuffer(30*time.Second, 1000, func(events []auditv1.Event) {
		if err := w.Write(events); err != nil {
			logf.Log.Error(err, "failed to flush events to Swift")
		}
	})
	go buf.Start(ctx)

	certFile := filepath.Join(opts.CertDir, secretsutils.DataKeyCertificate)
	keyFile := filepath.Join(opts.CertDir, secretsutils.DataKeyPrivateKey)
	caFile := filepath.Join(opts.CertDir, secretsutils.DataKeyCertificateCA)

	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return fmt.Errorf("failed to read CA cert: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		return errors.New("failed to parse CA cert")
	}

	h := &receiver.Handler{Logger: logf.Log.WithName("handler"), Buffer: buf}
	mux := http.NewServeMux()
	mux.Handle(receiver.AuditPath, h)

	srv := &http.Server{
		Addr:    opts.Addr,
		Handler: mux,
		TLSConfig: &tls.Config{
			ClientAuth: tls.RequireAndVerifyClientCert,
			ClientCAs:  caPool,
		},
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServeTLS(certFile, keyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		return srv.Shutdown(context.Background())
	case err := <-errCh:
		return err
	}
}
```

Note: add `auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"` to imports.

- [ ] **Step 3: Verify build**

```bash
go build ./cmd/persephone-auditlog-receiver/...
```

- [ ] **Step 4: Commit**

```bash
git add cmd/persephone-auditlog-receiver/
git commit -m "feat(receiver): add persephone-auditlog-receiver binary"
```

---

### Task 6: Receiver Deployment and Service manifests

**Files:**
- Create: `internal/extension/auditlog/receiver_deploy.go`
- Create: `internal/extension/auditlog/receiver_deploy_test.go`

**Interfaces:**
- Consumes: constants from Task 1 (`ReceiverServiceName`, `ForwarderClientTLSSecretName`, `ReceiverTLSSecretName`)
- Produces:
  - `func ReceiverDeployment(namespace, image string, cfg ObjectStoreConfig, shootName string) *appsv1.Deployment`
  - `func ReceiverService(namespace string) *corev1.Service`

- [ ] **Step 1: Write failing tests**

```go
// internal/extension/auditlog/receiver_deploy_test.go
package auditlog_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	"github.com/sap-cloud-infrastructure/persephone/internal/extension/auditlog"
)

var _ = Describe("ReceiverDeployment", func() {
	It("mounts receiver TLS secret", func() {
		d := auditlog.ReceiverDeployment("shoot--p--n", "receiver:latest", auditlog.ObjectStoreConfig{Container: "c", Prefix: "p/"}, "my-shoot")
		var volumeNames []string
		for _, v := range d.Spec.Template.Spec.Volumes {
			volumeNames = append(volumeNames, v.Name)
		}
		Expect(volumeNames).To(ContainElement("receiver-tls"))
	})

	It("mounts cloudprovider secret", func() {
		d := auditlog.ReceiverDeployment("shoot--p--n", "receiver:latest", auditlog.ObjectStoreConfig{Container: "c", Prefix: "p/"}, "my-shoot")
		var secretNames []string
		for _, v := range d.Spec.Template.Spec.Volumes {
			if v.Secret != nil {
				secretNames = append(secretNames, v.Secret.SecretName)
			}
		}
		Expect(secretNames).To(ContainElement("cloudprovider"))
	})

	It("sets container and prefix flags", func() {
		d := auditlog.ReceiverDeployment("shoot--p--n", "receiver:latest", auditlog.ObjectStoreConfig{Container: "audit-logs", Prefix: "shoots/my-shoot/"}, "my-shoot")
		args := d.Spec.Template.Spec.Containers[0].Args
		Expect(args).To(ContainElement("--container=audit-logs"))
		Expect(args).To(ContainElement("--prefix=shoots/my-shoot/"))
	})

	It("expands {{.shootName}} in prefix", func() {
		d := auditlog.ReceiverDeployment("shoot--p--n", "receiver:latest", auditlog.ObjectStoreConfig{Container: "c", Prefix: "shoots/{{.shootName}}/"}, "my-shoot")
		args := d.Spec.Template.Spec.Containers[0].Args
		Expect(args).To(ContainElement("--prefix=shoots/my-shoot/"))
	})
})

var _ = Describe("ReceiverService", func() {
	It("exposes port 8443", func() {
		svc := auditlog.ReceiverService("shoot--p--n")
		Expect(svc.Spec.Ports).To(HaveLen(1))
		Expect(svc.Spec.Ports[0].Port).To(Equal(int32(8443)))
	})
})
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/extension/auditlog/... -v
```
Expected: compile error (functions not defined)

- [ ] **Step 3: Create receiver_deploy.go**

```go
// internal/extension/auditlog/receiver_deploy.go
package auditlog

import (
	"bytes"
	"fmt"
	"text/template"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	secretsutils "github.com/gardener/gardener/pkg/utils/secrets"
)

const (
	receiverPort    = int32(8443)
	cloudProviderSecret = "cloudprovider"
)

// ReceiverDeployment returns the Deployment for the auditlog receiver.
// The prefix in cfg may contain "{{.shootName}}" which is expanded to shootName.
func ReceiverDeployment(namespace, image string, cfg ObjectStoreConfig, shootName string) *appsv1.Deployment {
	prefix := expandPrefix(cfg.Prefix, shootName)

	labels := map[string]string{
		"app":  "persephone",
		"role": "auditlog-receiver",
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ReceiverServiceName,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "receiver",
						Image: image,
						Args: []string{
							"--cert-dir=/etc/receiver/tls",
							fmt.Sprintf("--container=%s", cfg.Container),
							fmt.Sprintf("--prefix=%s", prefix),
						},
						Env: []corev1.EnvVar{
							envFromSecret("OS_AUTH_URL", cloudProviderSecret, "authURL"),
							envFromSecret("OS_APPLICATION_CREDENTIAL_ID", cloudProviderSecret, "applicationCredentialID"),
							envFromSecret("OS_APPLICATION_CREDENTIAL_SECRET", cloudProviderSecret, "applicationCredentialSecret"),
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "receiver-tls", MountPath: "/etc/receiver/tls", ReadOnly: true},
						},
					}},
					Volumes: []corev1.Volume{
						secretVolume("receiver-tls", ReceiverTLSSecretName),
						secretVolume(cloudProviderSecret, cloudProviderSecret),
					},
				},
			},
		},
	}
}

// ReceiverService returns the ClusterIP Service for the auditlog receiver.
func ReceiverService(namespace string) *corev1.Service {
	labels := map[string]string{
		"app":  "persephone",
		"role": "auditlog-receiver",
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ReceiverServiceName,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{{
				Port:       receiverPort,
				TargetPort: intstr.FromInt32(receiverPort),
				Protocol:   corev1.ProtocolTCP,
			}},
		},
	}
}

func expandPrefix(prefix, shootName string) string {
	tmpl, err := template.New("prefix").Parse(prefix)
	if err != nil {
		return prefix
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"shootName": shootName}); err != nil {
		return prefix
	}
	return buf.String()
}

func secretVolume(name, secretName string) corev1.Volume {
	return corev1.Volume{
		Name:         name,
		VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: secretName}},
	}
}

func envFromSecret(envName, secretName, key string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: envName,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
				Key:                  key,
			},
		},
	}
}
```

Note: the `secretsutils` import is unused here — remove it if the compiler flags it.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/extension/auditlog/... -v
```

- [ ] **Step 5: Verify build**

```bash
go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add internal/extension/auditlog/receiver_deploy.go internal/extension/auditlog/receiver_deploy_test.go
git commit -m "feat(extension): add receiver Deployment and Service builders"
```

---

### Task 7: auditing Extension providerConfig patcher

**Files:**
- Create: `internal/extension/auditlog/patch_auditing.go`
- Create: `internal/extension/auditlog/patch_auditing_test.go`

**Interfaces:**
- Produces:
  - `func EnsureAuditingBackend(ctx context.Context, c client.Client, namespace string) error`
  - `func RemoveAuditingBackend(ctx context.Context, c client.Client, namespace string) error`

The patcher encodes/decodes the `auditing` Extension's `spec.providerConfig` as a JSON object directly (no import of `gardener-extension-auditing` types needed — we construct the JSON structure ourselves to avoid a dependency).

- [ ] **Step 1: Write failing tests**

```go
// internal/extension/auditlog/patch_auditing_test.go
package auditlog_test

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/sap-cloud-infrastructure/persephone/internal/extension/auditlog"
	"github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
)

var _ = Describe("EnsureAuditingBackend", func() {
	var (
		ctx    context.Context
		c      client.Client
		ns     = "shoot--proj--name"
	)

	BeforeEach(func() {
		ctx = context.Background()
		auditingExt := &extensionsv1alpha1.Extension{
			ObjectMeta: metav1.ObjectMeta{Name: "auditing", Namespace: ns},
			Spec:       extensionsv1alpha1.ExtensionSpec{},
		}
		c = fake.NewClientBuilder().
			WithScheme(kubernetes.GardenerScheme).
			WithObjects(auditingExt).
			Build()
	})

	It("adds the persephone backend to an Extension with empty providerConfig", func() {
		Expect(auditlog.EnsureAuditingBackend(ctx, c, ns)).To(Succeed())

		ext := &extensionsv1alpha1.Extension{}
		Expect(c.Get(ctx, client.ObjectKey{Name: "auditing", Namespace: ns}, ext)).To(Succeed())
		Expect(ext.Spec.ProviderConfig).NotTo(BeNil())

		var cfg map[string]any
		Expect(json.Unmarshal(ext.Spec.ProviderConfig.Raw, &cfg)).To(Succeed())

		backends, ok := cfg["backends"].([]any)
		Expect(ok).To(BeTrue())
		Expect(backends).To(HaveLen(1))

		backend := backends[0].(map[string]any)
		http := backend["http"].(map[string]any)
		Expect(http["url"]).To(ContainSubstring("persephone-auditlog-receiver." + ns + ".svc"))
		Expect(http["tls"].(map[string]any)["secretName"]).To(Equal(auditlog.ForwarderClientTLSSecretName))
	})

	It("is idempotent — does not add a second backend on re-reconcile", func() {
		Expect(auditlog.EnsureAuditingBackend(ctx, c, ns)).To(Succeed())
		Expect(auditlog.EnsureAuditingBackend(ctx, c, ns)).To(Succeed())

		ext := &extensionsv1alpha1.Extension{}
		Expect(c.Get(ctx, client.ObjectKey{Name: "auditing", Namespace: ns}, ext)).To(Succeed())

		var cfg map[string]any
		Expect(json.Unmarshal(ext.Spec.ProviderConfig.Raw, &cfg)).To(Succeed())
		Expect(cfg["backends"].([]any)).To(HaveLen(1))
	})
})

var _ = Describe("RemoveAuditingBackend", func() {
	It("removes the persephone backend entry", func() {
		ctx := context.Background()
		ns := "shoot--proj--name"

		rawCfg, _ := json.Marshal(map[string]any{
			"apiVersion": "auditing.extensions.gardener.cloud/v1alpha1",
			"kind":       "AuditConfiguration",
			"backends": []any{
				map[string]any{
					"deliveryMode": "BestEffort",
					"http": map[string]any{
						"url": "https://persephone-auditlog-receiver." + ns + ".svc:8443/audit",
						"tls": map[string]any{"secretName": auditlog.ForwarderClientTLSSecretName},
					},
				},
			},
		})

		auditingExt := &extensionsv1alpha1.Extension{
			ObjectMeta: metav1.ObjectMeta{Name: "auditing", Namespace: ns},
			Spec: extensionsv1alpha1.ExtensionSpec{
				DefaultSpec: extensionsv1alpha1.DefaultSpec{
					ProviderConfig: &runtime.RawExtension{Raw: rawCfg},
				},
			},
		}
		c := fake.NewClientBuilder().
			WithScheme(kubernetes.GardenerScheme).
			WithObjects(auditingExt).
			Build()

		Expect(auditlog.RemoveAuditingBackend(ctx, c, ns)).To(Succeed())

		ext := &extensionsv1alpha1.Extension{}
		Expect(c.Get(ctx, client.ObjectKey{Name: "auditing", Namespace: ns}, ext)).To(Succeed())

		var cfg map[string]any
		Expect(json.Unmarshal(ext.Spec.ProviderConfig.Raw, &cfg)).To(Succeed())
		Expect(cfg["backends"].([]any)).To(BeEmpty())
	})
})
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/extension/auditlog/... -v
```
Expected: compile error

- [ ] **Step 3: Create patch_auditing.go**

```go
// internal/extension/auditlog/patch_auditing.go
package auditlog

import (
	"context"
	"encoding/json"
	"fmt"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	auditingExtensionName = "auditing"
	persephoneBackendURL  = "https://%s.%s.svc:%d/audit"
)

// auditConfiguration is a minimal representation of the auditing extension's
// providerConfig. We avoid importing gardener-extension-auditing to keep
// dependencies minimal.
type auditConfiguration struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Backends   []auditBackend `json:"backends"`
}

type auditBackend struct {
	DeliveryMode string           `json:"deliveryMode"`
	HTTP         *auditBackendHTTP `json:"http,omitempty"`
}

type auditBackendHTTP struct {
	URL string          `json:"url"`
	TLS *auditBackendTLS `json:"tls,omitempty"`
}

type auditBackendTLS struct {
	SecretName string `json:"secretName"`
}

// EnsureAuditingBackend upserts the persephone backend into the auditing
// Extension's providerConfig. Idempotent.
func EnsureAuditingBackend(ctx context.Context, c client.Client, namespace string) error {
	ext, cfg, err := getAuditingConfig(ctx, c, namespace)
	if err != nil {
		return err
	}

	url := fmt.Sprintf(persephoneBackendURL, ReceiverServiceName, namespace, receiverPort)

	// Remove any existing persephone entry to avoid duplicates, then re-add.
	cfg.Backends = removeBackendByURL(cfg.Backends, url)
	cfg.Backends = append(cfg.Backends, auditBackend{
		DeliveryMode: "BestEffort",
		HTTP: &auditBackendHTTP{
			URL: url,
			TLS: &auditBackendTLS{SecretName: ForwarderClientTLSSecretName},
		},
	})

	return patchAuditingConfig(ctx, c, ext, cfg)
}

// RemoveAuditingBackend removes the persephone backend from the auditing
// Extension's providerConfig. Idempotent.
func RemoveAuditingBackend(ctx context.Context, c client.Client, namespace string) error {
	ext, cfg, err := getAuditingConfig(ctx, c, namespace)
	if err != nil {
		return err
	}

	url := fmt.Sprintf(persephoneBackendURL, ReceiverServiceName, namespace, receiverPort)
	cfg.Backends = removeBackendByURL(cfg.Backends, url)

	return patchAuditingConfig(ctx, c, ext, cfg)
}

func getAuditingConfig(ctx context.Context, c client.Client, namespace string) (*extensionsv1alpha1.Extension, *auditConfiguration, error) {
	ext := &extensionsv1alpha1.Extension{}
	if err := c.Get(ctx, client.ObjectKey{Name: auditingExtensionName, Namespace: namespace}, ext); err != nil {
		return nil, nil, fmt.Errorf("failed to get auditing Extension: %w", err)
	}

	cfg := &auditConfiguration{
		APIVersion: "auditing.extensions.gardener.cloud/v1alpha1",
		Kind:       "AuditConfiguration",
	}
	if ext.Spec.ProviderConfig != nil && len(ext.Spec.ProviderConfig.Raw) > 0 {
		if err := json.Unmarshal(ext.Spec.ProviderConfig.Raw, cfg); err != nil {
			return nil, nil, fmt.Errorf("failed to decode auditing providerConfig: %w", err)
		}
	}

	return ext, cfg, nil
}

func patchAuditingConfig(ctx context.Context, c client.Client, ext *extensionsv1alpha1.Extension, cfg *auditConfiguration) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to encode auditing providerConfig: %w", err)
	}

	patch := client.MergeFrom(ext.DeepCopy())
	ext.Spec.ProviderConfig = &runtime.RawExtension{Raw: raw}
	if err := c.Patch(ctx, ext, patch); err != nil {
		return fmt.Errorf("failed to patch auditing Extension: %w", err)
	}
	return nil
}

func removeBackendByURL(backends []auditBackend, url string) []auditBackend {
	result := backends[:0]
	for _, b := range backends {
		if b.HTTP == nil || b.HTTP.URL != url {
			result = append(result, b)
		}
	}
	return result
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/extension/auditlog/... -v
```

- [ ] **Step 5: Verify build**

```bash
go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add internal/extension/auditlog/patch_auditing.go internal/extension/auditlog/patch_auditing_test.go
git commit -m "feat(extension): add auditing Extension providerConfig patcher"
```

---

### Task 8: Extension actuator

**Files:**
- Create: `internal/extension/auditlog/actuator.go`
- Create: `internal/extension/auditlog/actuator_test.go`
- Create: `internal/extension/add.go`

**Interfaces:**
- Consumes: all of Tasks 1–7
- Produces:
  - `type Actuator struct { Client client.Client; ReceiverImage string }`
  - `func (a *Actuator) Reconcile(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension, cluster *extensionscontroller.Cluster) error`
  - `func (a *Actuator) Delete(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension, cluster *extensionscontroller.Cluster) error`
  - `func AddToManager(mgr manager.Manager, receiverImage string) error`

- [ ] **Step 1: Write failing tests**

```go
// internal/extension/auditlog/actuator_test.go
package auditlog_test

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"github.com/go-logr/logr"

	"github.com/sap-cloud-infrastructure/persephone/internal/extension/auditlog"
	"github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
)

var _ = Describe("Actuator.Reconcile", func() {
	It("patches the auditing Extension with receiver backend", func() {
		ctx := context.Background()
		ns := "shoot--proj--shoot"

		providerCfg, _ := json.Marshal(map[string]any{
			"apiVersion":  "extension.persephone.cloud/v1alpha1",
			"kind":        "AuditLogConfig",
			"objectStore": map[string]any{"container": "logs", "prefix": "shoots/shoot/"},
		})

		ex := &extensionsv1alpha1.Extension{
			ObjectMeta: metav1.ObjectMeta{Name: "persephone-auditlog", Namespace: ns},
			Spec: extensionsv1alpha1.ExtensionSpec{
				DefaultSpec: extensionsv1alpha1.DefaultSpec{
					ProviderConfig: &runtime.RawExtension{Raw: providerCfg},
				},
			},
		}
		auditingExt := &extensionsv1alpha1.Extension{
			ObjectMeta: metav1.ObjectMeta{Name: "auditing", Namespace: ns},
		}

		c := fake.NewClientBuilder().
			WithScheme(kubernetes.GardenerScheme).
			WithObjects(ex, auditingExt).
			Build()

		cluster := &extensionscontroller.Cluster{
			Shoot: &gardenercorev1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{Name: "shoot", Namespace: "proj"},
			},
		}

		a := &auditlog.Actuator{Client: c, ReceiverImage: "receiver:test"}
		Expect(a.Reconcile(ctx, logr.Discard(), ex, cluster)).To(Succeed())

		// The auditing Extension should now have a backend pointing at our receiver.
		auditing := &extensionsv1alpha1.Extension{}
		Expect(c.Get(ctx, client.ObjectKey{Name: "auditing", Namespace: ns}, auditing)).To(Succeed())
		Expect(auditing.Spec.ProviderConfig).NotTo(BeNil())
	})
})
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/extension/auditlog/... -v
```
Expected: compile error (Actuator not defined)

- [ ] **Step 3: Create actuator.go**

```go
// internal/extension/auditlog/actuator.go
package auditlog

import (
	"context"
	"fmt"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	extensionssecretsmanager "github.com/gardener/gardener/extensions/pkg/util/secret/manager"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Actuator reconciles Extension{type:"persephone-auditlog"} objects.
type Actuator struct {
	Client        client.Client
	ReceiverImage string
}

// Reconcile ensures the receiver Deployment, Service, TLS secrets, and
// auditing Extension backend are all in place.
func (a *Actuator) Reconcile(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension, cluster *extensionscontroller.Cluster) error {
	namespace := ex.Namespace

	cfg, err := DecodeAuditLogConfig(ex.Spec.ProviderConfig)
	if err != nil {
		return fmt.Errorf("failed to decode AuditLogConfig: %w", err)
	}

	secretsManager, err := extensionssecretsmanager.SecretsManagerForCluster(
		ctx,
		log.WithName("secretsmanager"),
		clock.RealClock{},
		a.Client,
		cluster,
		ManagerIdentity,
		SecretConfigs(namespace),
	)
	if err != nil {
		return fmt.Errorf("failed to create SecretsManager: %w", err)
	}

	if _, err := extensionssecretsmanager.GenerateAllSecrets(ctx, secretsManager, SecretConfigs(namespace)); err != nil {
		return fmt.Errorf("failed to generate TLS secrets: %w", err)
	}

	shootName := cluster.Shoot.Name

	deployment := ReceiverDeployment(namespace, a.ReceiverImage, cfg.ObjectStore, shootName)
	if err := applyObject(ctx, a.Client, deployment); err != nil {
		return fmt.Errorf("failed to apply receiver Deployment: %w", err)
	}

	service := ReceiverService(namespace)
	if err := applyObject(ctx, a.Client, service); err != nil {
		return fmt.Errorf("failed to apply receiver Service: %w", err)
	}

	if err := EnsureAuditingBackend(ctx, a.Client, namespace); err != nil {
		return fmt.Errorf("failed to ensure auditing backend: %w", err)
	}

	return secretsManager.Cleanup(ctx)
}

// Delete removes the receiver resources and cleans up the auditing backend entry.
func (a *Actuator) Delete(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension, cluster *extensionscontroller.Cluster) error {
	namespace := ex.Namespace

	if err := RemoveAuditingBackend(ctx, a.Client, namespace); err != nil {
		return fmt.Errorf("failed to remove auditing backend: %w", err)
	}

	if err := deleteObject(ctx, a.Client, ReceiverService(namespace)); err != nil {
		return fmt.Errorf("failed to delete receiver Service: %w", err)
	}

	if err := deleteObject(ctx, a.Client, ReceiverDeployment(namespace, "", ObjectStoreConfig{}, "")); err != nil {
		return fmt.Errorf("failed to delete receiver Deployment: %w", err)
	}

	return nil
}

// applyObject creates or updates obj using server-side apply.
func applyObject(ctx context.Context, c client.Client, obj client.Object) error {
	return c.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("persephone-extension"))
}

// deleteObject deletes obj, ignoring not-found errors.
func deleteObject(ctx context.Context, c client.Client, obj client.Object) error {
	return client.IgnoreNotFound(c.Delete(ctx, obj))
}
```

- [ ] **Step 4: Create internal/extension/add.go**

```go
// internal/extension/add.go
package extension

import (
	"fmt"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/sap-cloud-infrastructure/persephone/internal/extension/auditlog"
)

const extensionType = "persephone-auditlog"

// AddToManager registers the persephone-auditlog extension actuator.
func AddToManager(mgr manager.Manager, receiverImage string) error {
	actuator := &auditlog.Actuator{
		Client:        mgr.GetClient(),
		ReceiverImage: receiverImage,
	}

	if err := extensionscontroller.Add(mgr, extensionscontroller.AddArgs{
		Actuator:          actuator,
		ControllerOptions: extensionscontroller.ControllerOptions{},
		Predicates:        extensionscontroller.DefaultPredicates(extensionType),
		Type:              extensionsv1alpha1.SchemeGroupVersion.WithKind("Extension"),
	}); err != nil {
		return fmt.Errorf("failed to add %s controller: %w", extensionType, err)
	}
	return nil
}
```

Note: verify the exact signature of `extensionscontroller.Add` against the gardener version in go.mod — it may differ. Check `github.com/gardener/gardener/extensions/pkg/controller` in the module cache.

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/extension/... -v
```

- [ ] **Step 6: Verify build**

```bash
go build ./...
```

- [ ] **Step 7: Commit**

```bash
git add internal/extension/
git commit -m "feat(extension): add auditlog actuator and extension controller registration"
```

---

### Task 9: Extension binary

**Files:**
- Create: `cmd/persephone-extension/main.go`
- Create: `cmd/persephone-extension/options/options.go`

**Interfaces:**
- Consumes: `internal/extension` from Task 8
- Produces: runnable binary `persephone-extension`

- [ ] **Step 1: Create options.go**

```go
// cmd/persephone-extension/options/options.go
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
```

- [ ] **Step 2: Create main.go**

```go
// cmd/persephone-extension/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/sapcc/go-api-declarations/bininfo"
	"github.com/go-logr/logr"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	controllerruntime "sigs.k8s.io/controller-runtime"

	"github.com/sap-cloud-infrastructure/persephone/cmd/persephone-extension/options"
	"github.com/sap-cloud-infrastructure/persephone/internal/extension"
	"github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
)

func main() {
	bininfo.HandleVersionArgument()
	opts := &options.Options{}
	opts.AddFlags()
	flag.Parse()

	log := zap.New(zap.UseFlagOptions(&opts.Zap)).WithName("persephone-extension")
	logf.SetLogger(log)
	klog.SetLogger(log)

	if err := run(signals.SetupSignalHandler(), log, opts); err != nil {
		panic(err)
	}
}

func run(ctx context.Context, logger logr.Logger, opts *options.Options) error {
	if err := opts.Validate(); err != nil {
		return fmt.Errorf("invalid options: %w", err)
	}

	mgr, err := manager.New(controllerruntime.GetConfigOrDie(), manager.Options{
		Logger:                  logger,
		Scheme:                  kubernetes.GardenerScheme,
		GracefulShutdownTimeout: ptr.To(5 * time.Second),
		HealthProbeBindAddress:  opts.Health.Addr,
		Metrics:                 metricsserver.Options{BindAddress: opts.Metrics.Addr},
	})
	if err != nil {
		return fmt.Errorf("failed to create manager: %w", err)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return err
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return err
	}

	if err := extension.AddToManager(mgr, opts.ReceiverImage); err != nil {
		return fmt.Errorf("failed to add extension controllers: %w", err)
	}

	return mgr.Start(ctx)
}
```

- [ ] **Step 3: Verify build**

```bash
go build ./cmd/persephone-extension/...
```

- [ ] **Step 4: Commit**

```bash
git add cmd/persephone-extension/
git commit -m "feat(extension): add persephone-extension binary"
```

---

### Task 10: Dockerfile and deployment manifests

**Files:**
- Modify: `Dockerfile` — add build stages for new binaries
- Create: `config/manifests/deployment-extension.yaml`
- Create: `config/manifests/deployment-auditlog-receiver.yaml`
- Create: `config/manifests/service-auditlog-receiver.yaml`
- Create: `config/manifests/rbac-extension.yaml`
- Create: `config/manifests/controllerregistration-auditlog.yaml`

**Interfaces:**
- Consumes: binaries from Tasks 5 and 9

- [ ] **Step 1: Read existing Dockerfile**

Read `Dockerfile` to understand the existing multi-stage build pattern, then add stages for `persephone-extension` and `persephone-auditlog-receiver` following the same pattern.

- [ ] **Step 2: Read an existing deployment manifest for reference**

Read `config/manifests/deployment-audit-webhook.yaml` to match naming conventions.

- [ ] **Step 3: Add build stages to Dockerfile**

Following the existing pattern (one `go build` RUN per binary, one FROM scratch stage per image), add:
```dockerfile
# Build persephone-extension
RUN CGO_ENABLED=0 go build -o /persephone-extension ./cmd/persephone-extension

# Build persephone-auditlog-receiver  
RUN CGO_ENABLED=0 go build -o /persephone-auditlog-receiver ./cmd/persephone-auditlog-receiver
```
And corresponding FROM stages for the final images.

- [ ] **Step 4: Create deployment-extension.yaml**

```yaml
# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
#
# SPDX-License-Identifier: Apache-2.0
apiVersion: apps/v1
kind: Deployment
metadata:
  name: persephone-extension
  namespace: garden
  labels:
    app: persephone
    role: extension
spec:
  replicas: 1
  selector:
    matchLabels:
      app: persephone
      role: extension
  template:
    metadata:
      labels:
        app: persephone
        role: extension
        networking.gardener.cloud/to-runtime-apiserver: allowed
        networking.gardener.cloud/to-dns: allowed
    spec:
      serviceAccountName: persephone-extension
      containers:
        - name: persephone-extension
          command: ["/usr/bin/persephone-extension"]
          args:
            - --receiver-image=persephone-auditlog-receiver:latest
            - --metrics-bind-address=:8080
            - --health-bind-address=:8081
          image: persephone-extension:latest
          ports:
            - containerPort: 8080
              name: metrics
            - containerPort: 8081
              name: health
```

- [ ] **Step 5: Create rbac-extension.yaml**

```yaml
# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
#
# SPDX-License-Identifier: Apache-2.0
apiVersion: v1
kind: ServiceAccount
metadata:
  name: persephone-extension
  namespace: garden
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: persephone-extension
rules:
  - apiGroups: [extensions.gardener.cloud]
    resources: [extensions]
    verbs: [get, list, watch, patch, update]
  - apiGroups: [""]
    resources: [secrets, services]
    verbs: [get, list, watch, create, update, patch, delete]
  - apiGroups: [apps]
    resources: [deployments]
    verbs: [get, list, watch, create, update, patch, delete]
  - apiGroups: [coordination.k8s.io]
    resources: [leases]
    verbs: [get, list, watch, create, update, patch, delete]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: persephone-extension
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: persephone-extension
subjects:
  - kind: ServiceAccount
    name: persephone-extension
    namespace: garden
```

- [ ] **Step 6: Create controllerregistration-auditlog.yaml**

```yaml
# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
#
# SPDX-License-Identifier: Apache-2.0
apiVersion: core.gardener.cloud/v1beta1
kind: ControllerRegistration
metadata:
  name: persephone-extension-auditlog
spec:
  resources:
    - kind: Extension
      type: persephone-auditlog
      lifecycle:
        reconcile: BeforeKubeAPIServer
        delete: AfterKubeAPIServer
  deployment:
    deploymentRefs:
      - name: persephone-extension
---
apiVersion: core.gardener.cloud/v1beta1
kind: ControllerDeployment
metadata:
  name: persephone-extension
spec:
  type: helm
  providerConfig:
    chart: <base64-encoded helm chart>
    values:
      image:
        repository: persephone-extension
        tag: latest
```

Note: the ControllerDeployment is a placeholder — adapt to your actual deployment mechanism (Helm chart, static manifests, or Operator-managed ManagedResource).

- [ ] **Step 7: Verify build still compiles**

```bash
go build ./...
```

- [ ] **Step 8: Commit**

```bash
git add Dockerfile config/manifests/deployment-extension.yaml config/manifests/rbac-extension.yaml config/manifests/controllerregistration-auditlog.yaml
git commit -m "chore(extension): add Dockerfile stages and deployment manifests for persephone-extension"
```

---

## Self-Review

### Spec coverage check

| Spec requirement | Covered by task |
|---|---|
| `AuditLogConfig` providerConfig types | Task 1 |
| SecretsManager with CA + server + client certs | Task 2 |
| Receiver HTTP handler + buffer | Task 3 |
| Swift writer with gzip NDJSON | Task 4 |
| Receiver binary with mTLS + env vars from cloudprovider | Task 5 |
| Receiver Deployment + Service with cloudprovider secret mount | Task 6 |
| Patch auditing Extension providerConfig — idempotent upsert/remove | Task 7 |
| Actuator reconcile + delete flow | Task 8 |
| Extension binary | Task 9 |
| Deployment manifests + RBAC + ControllerRegistration | Task 10 |
| SPDX headers | All tasks |
| ExtensionClass Shoot only | Task 8/9 |

### Known implementation notes for the executor

- `extensionscontroller.Add` signature: verify against `github.com/gardener/gardener/extensions/pkg/controller` in the cached module — the exact `AddArgs` fields differ between minor versions.
- `SecretsManager.Cleanup` removes obsolete secret versions — call it at the end of every reconcile.
- The fake client in tests does not support server-side apply (`client.Apply`) — use `client.Create`/`client.Update` in unit tests where SSA is exercised, or switch to envtest for integration coverage.
- `google/uuid` is not yet in go.mod — Task 4 adds it via `go get`.
