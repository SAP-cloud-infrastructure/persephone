// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package liquidserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sapcc/go-api-declarations/liquid"
	"github.com/sapcc/go-bits/gopherpolicy"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/httpapi/pprofapi"
	"github.com/sapcc/go-bits/httpext"
	"github.com/sapcc/go-bits/osext"
	"github.com/sapcc/go-bits/respondwith"
)

// RunOpts provides configuration to Run().
type RunOpts struct {
	// (Required.) Where the HTTP server will listen by default, e.g. ":8080".
	// Can be overridden at runtime by setting $LIQUID_LISTEN_ADDRESS.
	DefaultListenAddress string

	// (Required.) The Keystone v3 identity endpoint used to validate incoming tokens.
	// Typically the value of OS_AUTH_URL.
	IdentityEndpoint string
}

// Run spawns an HTTP server that serves the LIQUID API using self-validating tokens.
//
// Unlike liquidapi.Run(), this function does not require service account credentials
// (OS_APPLICATION_CREDENTIAL_ID/SECRET). Each incoming request's X-Auth-Token is used
// as its own validator against Keystone — the same pattern used in
// internal/webhook/token/handler.go.
//
// Incoming requests are authorised using oslo.policy loaded from $LIQUID_POLICY_PATH.
func Run(ctx context.Context, logic *Logic, opts RunOpts) error {
	if opts.DefaultListenAddress == "" {
		return errors.New("missing required value: liquidserver.RunOpts.DefaultListenAddress")
	}
	if opts.IdentityEndpoint == "" {
		return errors.New("missing required value: liquidserver.RunOpts.IdentityEndpoint")
	}

	// Build the policy enforcer (no Keystone client needed at startup).
	tv := &gopherpolicy.TokenValidator{
		Cacher: gopherpolicy.InMemoryCacher(),
	}
	policyPath, err := osext.NeedGetenv("LIQUID_POLICY_PATH")
	if err != nil {
		return err
	}
	if err := tv.LoadPolicyFile(policyPath, nil); err != nil {
		return err
	}

	// Logic.Init is a no-op for the liquid-apiserver, but we call it for interface compliance.
	if err := logic.Init(ctx, nil, gophercloud.EndpointOpts{}); err != nil {
		return fmt.Errorf("during Logic.Init(): %w", err)
	}
	serviceInfo, err := logic.BuildServiceInfo(ctx)
	if err != nil {
		return fmt.Errorf("during Logic.BuildServiceInfo(): %w", err)
	}

	srv := &server{
		Logic:            logic,
		ServiceInfo:      serviceInfo,
		tokenValidator:   tv,
		identityEndpoint: opts.IdentityEndpoint,
	}

	muxer := http.NewServeMux()
	muxer.Handle("/", httpapi.Compose(
		srv,
		httpapi.HealthCheckAPI{SkipRequestLog: true},
		pprofapi.API{IsAuthorized: pprofapi.IsRequestFromLocalhost},
	))
	muxer.Handle("/metrics", promhttp.Handler())

	listenAddr := osext.GetenvOrDefault("LIQUID_LISTEN_ADDRESS", opts.DefaultListenAddress)
	return httpext.ListenAndServeContext(ctx, listenAddr, muxer)
}

type server struct {
	Logic            *Logic
	ServiceInfo      liquid.ServiceInfo
	serviceInfoMu    sync.RWMutex
	tokenValidator   *gopherpolicy.TokenValidator
	identityEndpoint string
}

func (s *server) getServiceInfo() liquid.ServiceInfo {
	s.serviceInfoMu.RLock()
	defer s.serviceInfoMu.RUnlock()
	return s.ServiceInfo
}

// AddTo implements the httpapi.API interface.
func (s *server) AddTo(c *httpapi.Observer) {
	r := c.Router()
	r.Methods("GET").Path("/v1/info").HandlerFunc(s.handleGetInfo)
	r.Methods("POST").Path("/v1/report-capacity").HandlerFunc(s.handleReportCapacity)
	r.Methods("POST").Path("/v1/projects/{project_id}/report-usage").HandlerFunc(s.handleReportUsage)
	r.Methods("PUT").Path("/v1/projects/{project_id}/quota").HandlerFunc(s.handleSetQuota)
	r.Methods("POST").Path("/v1/change-commitments").HandlerFunc(s.handleChangeCommitments)
}

func (s *server) handleGetInfo(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/v1/info")
	if !s.requireToken(w, r, "liquid:get_info") {
		return
	}
	respondwith.JSON(w, http.StatusOK, s.getServiceInfo())
}

func (s *server) handleReportCapacity(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/v1/report-capacity")
	if !s.requireToken(w, r, "liquid:get_capacity") {
		return
	}
	var req liquid.ServiceCapacityRequest
	if !requireJSON(w, r, &req) {
		return
	}
	resp, err := s.Logic.ScanCapacity(r.Context(), req, s.getServiceInfo())
	if respondwith.ErrorText(w, err) {
		return
	}
	respondwith.JSON(w, http.StatusOK, resp)
}

func (s *server) handleReportUsage(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/v1/projects/:id/report-usage")
	if !s.requireToken(w, r, "liquid:get_usage") {
		return
	}
	vars := mux.Vars(r)
	var req liquid.ServiceUsageRequest
	if !requireJSON(w, r, &req) {
		return
	}
	resp, err := s.Logic.ScanUsage(r.Context(), vars["project_id"], req, s.getServiceInfo())
	if respondwith.ErrorText(w, err) {
		return
	}
	respondwith.JSON(w, http.StatusOK, resp)
}

func (s *server) handleSetQuota(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/v1/projects/:id/quota")
	if !s.requireToken(w, r, "liquid:set_quota") {
		return
	}
	vars := mux.Vars(r)
	var req liquid.ServiceQuotaRequest
	if !requireJSON(w, r, &req) {
		return
	}
	err := s.Logic.SetQuota(r.Context(), vars["project_id"], req, s.getServiceInfo())
	if respondwith.ErrorText(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleChangeCommitments(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/v1/change-commitments")
	if !s.requireToken(w, r, "liquid:change_commitments") {
		return
	}
	var req liquid.CommitmentChangeRequest
	if !requireJSON(w, r, &req) {
		return
	}
	srvInfo := s.getServiceInfo()
	if srvInfo.Version != req.InfoVersion {
		msg := fmt.Sprintf("request was for InfoVersion = %d, but current ServiceInfo.Version is %d", req.InfoVersion, srvInfo.Version)
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	resp, err := s.Logic.ReviewCommitmentChange(r.Context(), req, srvInfo)
	if respondwith.ErrorText(w, err) {
		return
	}
	respondwith.JSON(w, http.StatusOK, resp)
}

// requireToken validates the incoming X-Auth-Token using self-validation: the token
// is used as both the caller identity (X-Auth-Token) and the subject (X-Subject-Token)
// on the Keystone GET /v3/auth/tokens call. No service account credentials are needed.
//
// A per-request TokenValidator is constructed sharing the global enforcer and cacher,
// but with a fresh IdentityV3 client carrying the request's own token — avoiding any
// shared mutable state across concurrent requests.
func (s *server) requireToken(w http.ResponseWriter, r *http.Request, policyRule string) bool {
	tokenStr := r.Header.Get("X-Auth-Token")
	if tokenStr == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}

	identityClient, err := newIdentityClient(s.identityEndpoint, tokenStr)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}

	// Build a per-request validator that shares the enforcer and cacher but uses
	// the incoming token as the IdentityV3 client's own auth token.
	tv := &gopherpolicy.TokenValidator{
		IdentityV3: identityClient,
		Enforcer:   s.tokenValidator.Enforcer,
		Cacher:     s.tokenValidator.Cacher,
	}

	t := tv.CheckCredentials(r.Context(), tokenStr, func() gopherpolicy.TokenResult {
		return tokens.Get(r.Context(), identityClient, tokenStr)
	})
	t.Context.Request = mux.Vars(r)
	return t.Require(w, policyRule)
}

// newIdentityClient constructs a Keystone v3 service client using the given token
// as its own authentication — no application credential required.
func newIdentityClient(identityEndpoint, tokenID string) (*gophercloud.ServiceClient, error) {
	provider := &gophercloud.ProviderClient{
		IdentityBase: gophercloud.NormalizeURL(identityEndpoint),
		TokenID:      tokenID,
	}
	return openstack.NewIdentityV3(provider, gophercloud.EndpointOpts{})
}

func requireJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(&target)
	if err == nil {
		return true
	}
	msg := fmt.Sprintf("request body is not a valid JSON representation of %T: %s", target, err.Error())
	http.Error(w, msg, http.StatusBadRequest)
	return false
}
