package audithandler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	gardenercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/audittools"
	authenticationv1 "k8s.io/api/authentication/v1"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
)

// extractShootRegion extracts the region from the Shoot in the audit event.
// It tries multiple sources in order:
// 1. RequestObject (for create/update) or ResponseObject (for delete)
// 2. User's Extra fields (from OpenStack authentication or impersonation)
func extractShootRegion(event *auditv1.Event) (string, error) {
	// First try to get region from the Shoot object
	// For delete operations, use ResponseObject instead of RequestObject
	rawObject := event.RequestObject
	if event.Verb == verbDelete && event.ResponseObject != nil {
		rawObject = event.ResponseObject
	}

	if rawObject != nil && len(rawObject.Raw) > 0 {
		var shoot gardenercorev1beta1.Shoot
		if err := json.Unmarshal(rawObject.Raw, &shoot); err == nil && shoot.Spec.Region != "" {
			return shoot.Spec.Region, nil
		}
	}

	// Fall back to extracting region from user's Extra fields.
	// This is needed for delete operations where the API server returns a Status
	// object instead of the Shoot, so the region cannot be extracted from the response body.
	userExtra := getEffectiveUserExtra(event)
	if userExtra != nil {
		userInfo, err := kubernetes.OpenStackUserInfoFromExtra(userExtra)
		if err == nil && userInfo.Region != "" {
			return userInfo.Region, nil
		}
	}

	return "", errors.New("could not extract region from audit event (no Shoot object with region and no region in user info)")
}

// transformToAuditEvent converts a Kubernetes audit event to an audittools.Event.
func transformToAuditEvent(event *auditv1.Event) *audittools.Event {
	// Reconstruct HTTP request from audit metadata
	req := reconstructHTTPRequest(event)

	// Convert user info
	user := convertUserInfo(event)

	// Map verb to CADF action
	action := mapVerbToAction(event.Verb)

	// Build target resource
	target := buildShootTarget(event)

	// Get response code
	reasonCode := 500 // Default to server error
	if event.ResponseStatus != nil {
		reasonCode = int(event.ResponseStatus.Code)
	}

	return &audittools.Event{
		Time:       event.StageTimestamp.Time,
		Request:    req,
		User:       user,
		ReasonCode: reasonCode,
		Action:     action,
		Target:     target,
	}
}

// reconstructHTTPRequest creates a minimal http.Request from audit event metadata.
func reconstructHTTPRequest(event *auditv1.Event) *http.Request {
	reqURL, err := url.Parse(event.RequestURI)
	if err != nil {
		// Fallback to a minimal URL
		reqURL = &url.URL{Path: event.RequestURI}
	}

	remoteAddr := ""
	if len(event.SourceIPs) > 0 {
		remoteAddr = event.SourceIPs[0]
	}

	req := &http.Request{
		Method:     verbToHTTPMethod(event.Verb),
		URL:        reqURL,
		Proto:      "HTTP/1.1",
		Header:     make(http.Header),
		RemoteAddr: remoteAddr,
	}

	if event.UserAgent != "" {
		req.Header.Set("User-Agent", event.UserAgent)
	}

	return req
}

// verbToHTTPMethod maps Kubernetes verb to HTTP method.
func verbToHTTPMethod(verb string) string {
	switch verb {
	case verbCreate:
		return http.MethodPost
	case verbUpdate, verbPatch:
		return http.MethodPut
	case verbDelete:
		return http.MethodDelete
	default:
		return http.MethodPost
	}
}

// convertUserInfo converts Kubernetes UserInfo to audittools-compatible format.
// If impersonation is used, the impersonated user info is used.
func convertUserInfo(event *auditv1.Event) auditUserInfo {
	// Use impersonated user if present
	if event.ImpersonatedUser != nil {
		return auditUserInfo{
			username: event.ImpersonatedUser.Username,
			uid:      event.ImpersonatedUser.UID,
			extra:    event.ImpersonatedUser.Extra,
		}
	}

	return auditUserInfo{
		username: event.User.Username,
		uid:      event.User.UID,
		extra:    event.User.Extra,
	}
}

// auditUserInfo implements the UserInfo interface expected by audittools.
type auditUserInfo struct {
	username string
	uid      string
	extra    map[string]authenticationv1.ExtraValue
}

// AsInitiator converts user info to CADF initiator format.
func (u auditUserInfo) AsInitiator(host cadf.Host) cadf.Resource {
	resource := cadf.Resource{
		TypeURI: "service/security/account/user",
		ID:      u.username,
		Host:    &host,
	}

	// Add OpenStack project and domain information from Extra fields
	if u.extra != nil {
		if userInfo, err := kubernetes.OpenStackUserInfoFromExtra(u.extra); err == nil {
			resource.ProjectID = userInfo.ProjectID
			resource.ProjectName = userInfo.ProjectName
			resource.DomainID = userInfo.ProjectDomainID
			resource.DomainName = userInfo.ProjectDomainName
			resource.ProjectDomainName = userInfo.ProjectDomainName
		}
	}

	return resource
}

// mapVerbToAction maps Kubernetes verb to CADF action.
func mapVerbToAction(verb string) cadf.Action {
	switch verb {
	case verbCreate:
		return cadf.CreateAction
	case verbUpdate, verbPatch:
		return cadf.UpdateAction
	case verbDelete:
		return cadf.DeleteAction
	default:
		return cadf.UpdateAction
	}
}

// buildShootTarget builds the CADF target resource from audit event.
func buildShootTarget(event *auditv1.Event) shootTarget {
	var requestBody json.RawMessage
	if event.Verb != verbDelete && event.RequestObject != nil && len(event.RequestObject.Raw) > 0 {
		requestBody = event.RequestObject.Raw
	}

	return shootTarget{
		namespace:   event.ObjectRef.Namespace,
		name:        event.ObjectRef.Name,
		requestBody: requestBody,
	}
}

// shootTarget implements the Target interface expected by audittools.
type shootTarget struct {
	namespace   string
	name        string
	requestBody json.RawMessage
}

// Render converts the Shoot to CADF resource format.
func (s shootTarget) Render() cadf.Resource {
	clusterRef := s.namespace + "/" + s.name

	resource := cadf.Resource{
		TypeURI: "kubernetes/cluster",
		ID:      clusterRef,
		Name:    clusterRef,
	}

	if len(s.requestBody) > 0 {
		attachment, err := cadf.NewJSONAttachment("requestBody", s.requestBody)
		if err == nil {
			resource.Attachments = []cadf.Attachment{attachment}
		}
	}

	return resource
}

// transformToKubeconfigAuditEvent converts a kubeconfig subresource audit event to an audittools.Event.
func transformToKubeconfigAuditEvent(event *auditv1.Event) *audittools.Event {
	req := reconstructHTTPRequest(event)
	user := convertUserInfo(event)

	var requestBody json.RawMessage
	if event.RequestObject != nil && len(event.RequestObject.Raw) > 0 {
		requestBody = event.RequestObject.Raw
	}

	target := shootTarget{
		namespace:   event.ObjectRef.Namespace,
		name:        event.ObjectRef.Name,
		requestBody: requestBody,
	}

	action := cadf.Action("authenticate/admin")
	if event.ObjectRef.Subresource == subresourceViewerKubeconfig {
		action = "authenticate/viewer"
	}

	reasonCode := 500
	if event.ResponseStatus != nil {
		reasonCode = int(event.ResponseStatus.Code)
	}

	return &audittools.Event{
		Time:       event.StageTimestamp.Time,
		Request:    req,
		User:       user,
		ReasonCode: reasonCode,
		Action:     action,
		Target:     target,
	}
}
