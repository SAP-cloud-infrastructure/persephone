// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

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
