// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package token

import (
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook/authentication"
)

const (
	// HandlerName is the name of this admission webhook handler.
	HandlerName = "token-review"
	// WebhookPath is the HTTP handler path for this admission webhook handler.
	WebhookPath = "/validate-authentication-k8s-io-v1-tokenreview"
)

// AddToManager adds Handler to the given manager.
func (h *Handler) AddToManager(mgr manager.Manager) error {
	webhook := &authentication.Webhook{
		Handler: h,
	}

	mgr.GetWebhookServer().Register(WebhookPath, webhook)
	return nil
}
