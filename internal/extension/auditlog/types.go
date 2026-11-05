// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"encoding/json"
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// AuditLogConfig is the providerConfig for the persephone-auditlog Extension.
type AuditLogConfig struct {
	metav1.TypeMeta `json:",inline"`
	ObjectStore     ObjectStoreConfig `json:"objectStore"`
}

// ObjectStoreConfig holds Ceph S3 object storage target configuration.
type ObjectStoreConfig struct {
	Bucket string `json:"bucket"`
	// Prefix may contain "{{.shootName}}" which is expanded by the actuator.
	Prefix     string            `json:"prefix"`
	ObjectLock *ObjectLockConfig `json:"objectLock,omitempty"`
}

// ObjectLockConfig configures S3 Object Lock (WORM) for audit log objects.
// Mode must be "COMPLIANCE" or "GOVERNANCE".
type ObjectLockConfig struct {
	Enabled       bool   `json:"enabled"`
	RetentionDays int32  `json:"retentionDays"`
	Mode          string `json:"mode"`
}

const ReceiverServiceName = "persephone-auditlog-receiver"

// DecodeAuditLogConfig decodes the providerConfig raw extension into an AuditLogConfig.
func DecodeAuditLogConfig(raw *runtime.RawExtension) (*AuditLogConfig, error) {
	if raw == nil {
		return nil, errors.New("providerConfig is nil")
	}
	cfg := &AuditLogConfig{}
	if err := json.Unmarshal(raw.Raw, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
