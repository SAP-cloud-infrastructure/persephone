package audithandler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	"github.com/sapcc/go-bits/audittools"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
)

const (
	// HandlerName is the name of the audit handler.
	HandlerName = "audit-handler"

	// WebhookPath is the HTTP path for the audit webhook.
	WebhookPath = "/audit"
)

// Handler handles Kubernetes audit events for Shoots.
type Handler struct {
	Logger         logr.Logger
	RegionAuditors *RegionAuditorSet
	metrics        *metrics
}

// AddToManager adds the audit handler to the controller manager.
func (h *Handler) AddToManager(mgr manager.Manager) error {
	if h.metrics == nil {
		h.metrics = newMetrics(ctrlmetrics.Registry)
	}

	mgr.GetWebhookServer().Register(WebhookPath, h)
	h.Logger.Info("registered audit handler", "path", WebhookPath)

	return nil
}

// ServeHTTP handles incoming audit events.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Parse event list
	var eventList auditv1.EventList
	if err := json.NewDecoder(r.Body).Decode(&eventList); err != nil {
		h.Logger.Error(err, "failed to decode audit event list")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	h.Logger.V(1).Info("received audit events", "count", len(eventList.Items))

	// Process each event
	for i := range eventList.Items {
		event := &eventList.Items[i]

		if err := h.processEvent(event); err != nil {
			h.Logger.Error(err, "failed to process audit event",
				"auditID", event.AuditID,
				"verb", event.Verb,
				"resource", event.ObjectRef.Resource,
				"namespace", event.ObjectRef.Namespace,
				"name", event.ObjectRef.Name,
			)
			// Return error to trigger API server retry
			http.Error(w, fmt.Sprintf("failed to process audit event: %v", err), http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}

// processEvent processes a single audit event.
func (h *Handler) processEvent(event *auditv1.Event) error {
	start := time.Now()

	// Filter out events we don't care about
	if !shouldProcessEvent(event) {
		h.metrics.recordFiltered(getFilterReason(event))
		return nil
	}

	// Dispatch based on subresource type
	if event.ObjectRef.Subresource != "" {
		return h.processKubeconfigEvent(event, start)
	}
	return h.processShootEvent(event, start)
}

// processShootEvent handles main Shoot resource audit events.
func (h *Handler) processShootEvent(event *auditv1.Event, start time.Time) error {
	// Extract region
	region, err := extractShootRegion(event)
	if err != nil {
		h.metrics.recordPublishFailure("unknown", "region_extraction_error")
		return fmt.Errorf("failed to extract region: %w", err)
	}

	// Transform to audit event
	auditEvent := transformToAuditEvent(event)

	return h.recordAndPublish(event, auditEvent, region, start)
}

// processKubeconfigEvent handles kubeconfig subresource audit events.
func (h *Handler) processKubeconfigEvent(event *auditv1.Event, start time.Time) error {
	// Extract region from user's Extra fields (kubeconfig requests don't contain a Shoot object)
	userExtra := getEffectiveUserExtra(event)
	if userExtra == nil {
		h.metrics.recordPublishFailure("unknown", "region_extraction_error")
		return errors.New("failed to extract region: no user extra fields")
	}

	userInfo, err := kubernetes.OpenStackUserInfoFromExtra(userExtra)
	if err != nil {
		h.metrics.recordPublishFailure("unknown", "region_extraction_error")
		return fmt.Errorf("failed to extract region from user info: %w", err)
	}
	if userInfo.Region == "" {
		h.metrics.recordPublishFailure("unknown", "region_extraction_error")
		return errors.New("failed to extract region from user info: region is empty")
	}

	// Transform to audit event
	auditEvent := transformToKubeconfigAuditEvent(event)

	return h.recordAndPublish(event, auditEvent, userInfo.Region, start)
}

// recordAndPublish gets the auditor for the region, records the event, and publishes metrics.
func (h *Handler) recordAndPublish(event *auditv1.Event, auditEvent *audittools.Event, region string, start time.Time) error {
	defer func() {
		duration := time.Since(start).Seconds()
		h.metrics.observeProcessingDuration(region, duration)
	}()

	auditor, err := h.RegionAuditors.GetAuditor(region)
	if err != nil {
		h.metrics.recordPublishFailure(region, "no_auditor")
		return fmt.Errorf("failed to get auditor for region %s: %w", region, err)
	}

	auditor.Record(*auditEvent)

	outcome := "success"
	if event.ResponseStatus != nil && event.ResponseStatus.Code >= 400 {
		outcome = "failure"
	}
	h.metrics.recordEvent(event.Verb, region, outcome)

	h.Logger.Info("recorded audit event",
		"region", region,
		"verb", event.Verb,
		"resource", event.ObjectRef.Resource,
		"subresource", event.ObjectRef.Subresource,
		"namespace", event.ObjectRef.Namespace,
		"name", event.ObjectRef.Name,
	)

	return nil
}

// getFilterReason returns a human-readable reason for filtering.
func getFilterReason(event *auditv1.Event) string {
	if event.Stage != auditv1.StageResponseComplete {
		return "wrong_stage"
	}
	if event.ObjectRef == nil || event.ObjectRef.Resource != shootResource {
		return "not_shoot_resource"
	}
	// Check subresource-specific filter reasons
	if event.ObjectRef.Subresource != "" {
		switch event.ObjectRef.Subresource {
		case subresourceAdminKubeconfig, subresourceViewerKubeconfig:
			if event.Verb != verbCreate {
				return "not_crud_verb"
			}
			return "no_openstack_credentials"
		default:
			return "unsupported_subresource"
		}
	}
	switch event.Verb {
	case verbCreate, verbUpdate, verbDelete, verbPatch:
	default:
		return "not_crud_verb"
	}
	return "no_openstack_credentials"
}
