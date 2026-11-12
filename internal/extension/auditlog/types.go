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

// ObjectStoreConfig holds Swift object storage target configuration.
type ObjectStoreConfig struct {
	Container string `json:"container"`
	// Prefix may contain "{{.shootName}}" which is expanded by the actuator.
	Prefix string `json:"prefix"`
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
