// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package token

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	authenticationv1 "k8s.io/api/authentication/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/webhook/authentication"

	"github.com/sap-cloud-infrastructure/persephone/internal/config"
	"github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
)

type Handler struct {
	// Logger is a logger.
	Logger logr.Logger
	// Client is the controller-runtime Kubernetes client.
	Client client.Client
	// Config is the Keystone configuration.
	Config config.PersephoneConfig
	// ReconcileGardenerProjectResources specifies whether the Gardener project-related resources should be reconciled
	// after successful token validation.
	ReconcileGardenerProjectResources bool
	// metrics holds the Prometheus metrics.
	metrics *metrics
}

func (h *Handler) Handle(ctx context.Context, request authentication.Request) authentication.Response {
	// Lazy initialize metrics
	if h.metrics == nil {
		h.metrics = newMetrics(ctrlmetrics.Registry)
	}

	userInfo, err := h.makeUserInfo(ctx, request)
	if err != nil {
		h.Logger.Error(err, "failed to make user info")
		_, _, region, _ := h.getRegionalIdentityEndpoint(request) //nolint:errcheck // region is "" on malformed token, acceptable for metrics
		h.metrics.recordAuthenticationAttempt(region, "failure")
		h.metrics.recordAuthenticationFailure(region, "make_user_info_failed")
		return authentication.Unauthenticated(fmt.Sprintf("failed to make user info: %v", err), authenticationv1.UserInfo{})
	}

	if h.ReconcileGardenerProjectResources {
		openStackUserInfo, err := kubernetes.OpenStackUserInfoFromExtra(userInfo.Extra)
		if err != nil {
			return authentication.Unauthenticated(fmt.Sprintf("failed getting OpenStack user info from extras: %v", err), authenticationv1.UserInfo{})
		}

		if err := kubernetes.ReconcileGardenerProjectResources(ctx, h.Client, h.Config, openStackUserInfo); err != nil {
			h.Logger.Error(err, "preparation of garden resources failed")
			h.metrics.recordAuthenticationAttempt(openStackUserInfo.Region, "failure")
			h.metrics.recordResourceReconciliation(openStackUserInfo.Region, "error")
			return authentication.Unauthenticated(fmt.Sprintf("preparation of garden resources failed: %v", err), authenticationv1.UserInfo{})
		}

		h.metrics.recordAuthenticationAttempt(openStackUserInfo.Region, "success")
		h.metrics.recordResourceReconciliation(openStackUserInfo.Region, "success")
	}

	return authentication.Authenticated("", userInfo)
}

func (h *Handler) getRegionalIdentityEndpoint(req authentication.Request) (endpoint, token, region string, err error) {
	split := strings.Split(req.Spec.Token, ":")
	if len(split) != 2 {
		return "", "", "", errors.New("invalid token format")
	}
	region, token = split[0], split[1]

	regionalConfig, ok := h.Config.Regions[region]
	if !ok {
		return "", "", "", fmt.Errorf("%q region not found in config", region)
	}
	if regionalConfig.IdentityEndpoint == "" {
		return "", "", "", fmt.Errorf("%q region found, but identity endpoint is empty", region)
	}

	return regionalConfig.IdentityEndpoint, token, region, nil
}

func getOpenStackClient(endpoint, tokenID string) (*gophercloud.ServiceClient, error) {
	provider := &gophercloud.ProviderClient{
		IdentityBase: gophercloud.NormalizeURL(endpoint),
		TokenID:      tokenID,
	}

	return openstack.NewIdentityV3(provider, gophercloud.EndpointOpts{
		// Leave Region empty unless needed
	})
}

func (h *Handler) makeUserInfo(ctx context.Context, request authentication.Request) (authenticationv1.UserInfo, error) {
	endpoint, tokenID, region, err := h.getRegionalIdentityEndpoint(request)
	if err != nil {
		return authenticationv1.UserInfo{}, fmt.Errorf("failed to get regional identity endpoint: %w", err)
	}

	openStackClient, err := getOpenStackClient(endpoint, tokenID)
	if err != nil {
		return authenticationv1.UserInfo{}, fmt.Errorf("failed to create OpenStack client: %w", err)
	}

	result := tokens.Get(ctx, openStackClient, tokenID)

	user, err := result.ExtractUser()
	if err != nil {
		h.metrics.recordKeystoneAuthFailure(region, classifyError(err))
		return authenticationv1.UserInfo{}, fmt.Errorf("failed extracting user: %w", err)
	}

	project, err := result.ExtractProject()
	if err != nil {
		h.metrics.recordKeystoneAuthFailure(region, classifyError(err))
		return authenticationv1.UserInfo{}, fmt.Errorf("failed extracting project: %w", err)
	}

	roles, err := result.ExtractRoles()
	if err != nil {
		h.metrics.recordKeystoneAuthFailure(region, classifyError(err))
		return authenticationv1.UserInfo{}, fmt.Errorf("failed extracting roles: %w", err)
	}

	groups := make([]string, 0, len(roles))
	for _, role := range roles {
		// TODO: handle region prefix
		groups = append(groups, project.ID+":"+role.Name)
	}

	return authenticationv1.UserInfo{
		Username: user.Name,
		UID:      user.ID,
		Groups:   groups,
		Extra: kubernetes.OpenStackUserInfo{
			ProjectDomainID:   project.Domain.ID,
			ProjectDomainName: project.Domain.Name,
			ProjectID:         project.ID,
			ProjectName:       project.Name,
			UserDomainID:      user.Domain.ID,
			UserDomainName:    user.Domain.Name,
			Region:            region,
		}.ToExtra(),
	}, nil
}

// classifyError classifies an error into a high-level category for metrics.
func classifyError(err error) string {
	if err == nil {
		return "unknown"
	}

	errStr := err.Error()
	switch {
	case strings.Contains(errStr, "401"):
		return "unauthorized"
	case strings.Contains(errStr, "403"):
		return "forbidden"
	case strings.Contains(errStr, "404"):
		return "not_found"
	case strings.Contains(errStr, "timeout"):
		return "timeout"
	case strings.Contains(errStr, "connection"):
		return "connection_error"
	default:
		return "other"
	}
}
