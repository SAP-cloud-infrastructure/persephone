// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audithandler

import (
	authenticationv1 "k8s.io/api/authentication/v1"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
)

const (
	gardenerAPIGroup = "core.gardener.cloud"
	shootResource    = "shoots"

	subresourceAdminKubeconfig  = "adminkubeconfig"
	subresourceViewerKubeconfig = "viewerkubeconfig"

	verbCreate = "create"
	verbUpdate = "update"
	verbDelete = "delete"
	verbPatch  = "patch"
)

// shouldProcessEvent returns true if the event should be audited.
func shouldProcessEvent(event *auditv1.Event) bool {
	// Only process ResponseComplete stage
	if event.Stage != auditv1.StageResponseComplete {
		return false
	}

	// Only process Shoot resources
	if event.ObjectRef == nil || event.ObjectRef.APIGroup != gardenerAPIGroup || event.ObjectRef.Resource != shootResource {
		return false
	}

	// Dispatch based on subresource
	if event.ObjectRef.Subresource == "" {
		return shouldProcessShootEvent(event)
	}
	return shouldProcessKubeconfigEvent(event)
}

// shouldProcessShootEvent checks if a main Shoot resource event should be audited.
func shouldProcessShootEvent(event *auditv1.Event) bool {
	// Only process create, update, delete operations
	switch event.Verb {
	case verbCreate, verbUpdate, verbDelete, verbPatch:
		// patch is treated as update
	default:
		return false
	}

	// Only process events from users with OpenStack credentials
	if !hasOpenStackUserInfo(event) {
		return false
	}

	return true
}

// shouldProcessKubeconfigEvent checks if a kubeconfig subresource event should be audited.
func shouldProcessKubeconfigEvent(event *auditv1.Event) bool {
	// Only audit adminkubeconfig and viewerkubeconfig subresources
	switch event.ObjectRef.Subresource {
	case subresourceAdminKubeconfig, subresourceViewerKubeconfig:
	default:
		return false
	}

	// Only process create operations (kubeconfig requests are always POST)
	if event.Verb != verbCreate {
		return false
	}

	// Only process events from users with OpenStack credentials
	if !hasOpenStackUserInfo(event) {
		return false
	}

	return true
}

// hasOpenStackUserInfo checks if the event has OpenStack user info either in User.Extra
// or ImpersonatedUser.Extra (when impersonation is used).
func hasOpenStackUserInfo(event *auditv1.Event) bool {
	// First check ImpersonatedUser if present (impersonation takes precedence)
	if event.ImpersonatedUser != nil {
		return kubernetes.HasOpenStackUserInfo(event.ImpersonatedUser.Extra)
	}

	// Fall back to checking User.Extra
	return kubernetes.HasOpenStackUserInfo(event.User.Extra)
}

// getEffectiveUserExtra returns the Extra map from the effective user
// (ImpersonatedUser if present, otherwise User).
func getEffectiveUserExtra(event *auditv1.Event) map[string]authenticationv1.ExtraValue {
	if event.ImpersonatedUser != nil {
		return event.ImpersonatedUser.Extra
	}
	return event.User.Extra
}
