// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"

	"github.com/sap-cloud-infrastructure/persephone/internal/auth"
)

func setAuthTestEnv(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{
		"OS_AUTH_URL":                      "https://identity.example.com/v3",
		"OS_USER_DOMAIN_NAME":              "users",
		"OS_DOMAIN_NAME":                   "",
		"OS_USERNAME":                      "alice",
		"OS_PROJECT_NAME":                  "test-project",
		"OS_PROJECT_DOMAIN_NAME":           "projects",
		"OS_PASSWORD":                      "inherited-password",
		"OS_APPLICATION_CREDENTIAL_ID":     "",
		"OS_APPLICATION_CREDENTIAL_NAME":   "",
		"OS_APPLICATION_CREDENTIAL_SECRET": "",
		"SCIKUBE_URL":                      "",
		"SCIKUBE_PROJECT_ID":               "",
		"RUN_E2E_TEST":                     "false",
	} {
		t.Setenv(key, value)
	}
}

func TestGetAuthOptionsCredentialSelection(t *testing.T) {
	for _, tc := range []struct {
		name, id, credentialName, secret string
		wantMethod, wantError            string
	}{
		{name: "password", wantMethod: "password"},
		{name: "application credential ID and name", id: "app-id", credentialName: "app-name", secret: "app-secret", wantMethod: "application_credential"},
		{name: "missing application credential identifier", secret: "app-secret", wantError: "application credentials missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setAuthTestEnv(t)
			t.Setenv("OS_APPLICATION_CREDENTIAL_ID", tc.id)
			t.Setenv("OS_APPLICATION_CREDENTIAL_NAME", tc.credentialName)
			t.Setenv("OS_APPLICATION_CREDENTIAL_SECRET", tc.secret)
			opts, err := getAuthOptions()
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("error = %v, want %q", err, tc.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.secret != "" && opts.Password != "" {
				t.Error("application credential auth retained the inherited password")
			}
			if opts.Scope == nil || opts.Scope.ProjectName != "test-project" {
				t.Fatal("project metadata needed by kubeconfig generation was lost")
			}
			request, err := opts.ToTokenV3CreateMap(nil)
			if err != nil {
				t.Fatal(err)
			}
			identity := request["auth"].(map[string]any)["identity"].(map[string]any)
			methods := identity["methods"].([]any)
			if len(methods) != 1 || methods[0] != tc.wantMethod {
				t.Errorf("authentication methods = %v, want [%s]", methods, tc.wantMethod)
			}
			if tc.secret != "" {
				key, err := auth.CacheKey(auth.FileStorage, opts)
				if err != nil {
					t.Fatal(err)
				}
				t.Setenv("OS_PASSWORD", "")
				withoutPassword, err := getAuthOptions()
				if err != nil {
					t.Fatal(err)
				}
				otherKey, err := auth.CacheKey(auth.FileStorage, withoutPassword)
				if err != nil {
					t.Fatal(err)
				}
				if key != otherKey {
					t.Error("inherited password changed the application credential cache key")
				}
			}
		})
	}
}

func TestKubeconfigForGardenMissingProject(t *testing.T) {
	for _, tc := range []struct {
		name  string
		token map[string]any
	}{
		{name: "project absent", token: map[string]any{}},
		{name: "project ID empty", token: map[string]any{"project": map[string]any{"name": "test-project"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setAuthTestEnv(t)
			original := auth.AuthenticatedClientFunc
			t.Cleanup(func() { auth.AuthenticatedClientFunc = original })
			auth.AuthenticatedClientFunc = func(ctx context.Context, opts gophercloud.AuthOptions) (*gophercloud.ProviderClient, error) {
				provider := &gophercloud.ProviderClient{}
				result := tokens.CreateResult{}
				result.Body = map[string]any{"token": tc.token}
				result.Header = map[string][]string{"X-Subject-Token": {"test-token"}}
				if err := provider.SetTokenAndAuthResult(result); err != nil {
					return nil, err
				}
				return provider, nil
			}
			err := kubeconfigForGarden(kubeconfigForGardenCmd, nil)
			if err == nil || !strings.Contains(err.Error(), "project-scoped") {
				t.Fatalf("error = %v, want a project-scoped authentication error", err)
			}
		})
	}
}
