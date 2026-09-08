// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"go.xyrillian.de/gg/assert"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/client-go/pkg/apis/clientauthentication/v1"
)

type StubTransport struct {
	region string
}

func (t *StubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusCreated,
		Header:     http.Header{"X-Subject-Token": []string{"my_token"}},
		Body:       io.NopCloser(strings.NewReader(`{"token": {"catalog": [{"endpoints": [{"interface": "public", "region": "` + t.region + `"}], "type": "identity"}] }}}`)),
	}, nil
}

type ExpirationTimestamp time.Time

func (t ExpirationTimestamp) Format(f fmt.State, c rune) {
	if c == 'D' {
		fmt.Fprintf(f, "%s", time.Time(t).UTC().Format("2006-01-02T15:04:05Z"))
	}
}

func TestCreatesTokenWithRegionPrefix(t *testing.T) {
	createToken(t, "unit_testing_region", true)
}

func TestCreatesTokenWithoutRegionPrefix(t *testing.T) {
	createToken(t, "to_ignore", false)
}

func TestFileStorageExecCredentialCache_Read_FailsIfMissingTokenID(t *testing.T) {
	_, cacheFile := mustWriteTempCacheFile(t, []byte(`{"kind": "ExecCredential", "status": {"expirationTimestamp": "2025-06-19T09:00:25Z"}}`))
	s := &FileStorageExecCredentialCache{}
	_, err := s.Read(cacheFile)
	if err == nil {
		t.Error("expected loading to fail")
		t.FailNow()
	}
	if !errors.Is(err, errMissingTokenID) {
		t.Errorf("expected loading to fail due to missing token ID; actual error = %s", err)
		t.FailNow()
	}
}

func TestFileStorageExecCredentialCache_Read_FailsIfMissingTokenExpirationTimestamp(t *testing.T) {
	_, cacheFile := mustWriteTempCacheFile(t, []byte(`{"kind": "ExecCredential", "status": {"token": "atoken"}}`))
	s := &FileStorageExecCredentialCache{}
	_, err := s.Read(cacheFile)
	if err == nil {
		t.Error("expected loading to fail")
		t.FailNow()
	}
	if !errors.Is(err, errMissingTokenExpirationTimestamp) {
		t.Errorf("expected loading to fail due to missing token expiration timestamp; actual error = %s", err)
		t.FailNow()
	}
}

func TestFileStorageExecCredentialCache_Read_FailsIfExpiredToken(t *testing.T) {
	expiresAt := time.Now().Add(-5 * time.Minute).UTC().Format("2006-01-02T15:04:05Z")
	_, cacheFile := mustWriteTempCacheFile(t, []byte(`{
		"kind": "ExecCredential",
		"apiVersion": "client.authentication.k8s.io/v1",
		"spec": { "interactive": false },
		"status": { "expirationTimestamp": "`+expiresAt+`", "token": "devstack:averylongtoken" }}`))
	s := &FileStorageExecCredentialCache{}
	_, err := s.Read(cacheFile)
	if err == nil {
		t.Error("expected loading to fail")
		t.FailNow()
	}
	if typedErr, ok := errors.AsType[*ExpiredCachedExecCredentialError](err); !ok || typedErr == nil {
		t.Errorf("expected loading to fail due to cached token having expired; actual error = %s", err)
		t.FailNow()
	}
}

func TestFileStorageExecCredentialCache_Read_FailsIfUnreadableCacheFile(t *testing.T) {
	cacheDir, cacheFile := mustWriteTempCacheFile(t, []byte(`{"kind":"ExecCredential"}`))
	// remove the directory recursively immediately to force an "unreadable file" scenario
	os.RemoveAll(cacheDir)
	s := &FileStorageExecCredentialCache{}
	_, err := s.Read(cacheFile)
	if err == nil {
		t.Error("expected loading to fail")
		t.FailNow()
	}
	if !errors.Is(err, ErrCacheKeyNotFound) {
		t.Errorf("expected loading to fail at file read step; actual error = %s", err)
		t.FailNow()
	}
}

func TestFileStorageExecCredentialCache_Read_FailsIfInvalidJSON(t *testing.T) {
	_, cacheFile := mustWriteTempCacheFile(t, []byte(`{"kind`))
	s := &FileStorageExecCredentialCache{}
	_, err := s.Read(cacheFile)
	if err == nil {
		t.Error("expected loading to fail")
		t.FailNow()
	}
	if typedErr, ok := errors.AsType[*invalidCachedExecCredentialError](err); !ok || typedErr == nil {
		t.Errorf("expected loading to fail at unmarshalling step; actual error = %s", err)
		t.FailNow()
	}
}

func TestFileStorageExecCredentialCache_Read_FailsIfUnsafePermissions(t *testing.T) {
	expiresAt := time.Now().Add(5 * time.Minute).UTC()
	_, cacheFile := mustWriteTempCacheFile(t, fmt.Appendf([]byte{}, `{
		"kind": "ExecCredential",
		"apiVersion": "client.authentication.k8s.io/v1",
		"spec": { "interactive": false },
		"status": { "expirationTimestamp": "%D", "token": "devstack:averylongtoken" }}`, ExpirationTimestamp(expiresAt)))
	mustChmod(t, cacheFile, 0644)
	s := &FileStorageExecCredentialCache{}
	_, err := s.Read(cacheFile)
	if err == nil {
		t.Error("expected loading to fail")
		t.FailNow()
	}
	if typedErr, ok := errors.AsType[*unsafeCachedExecCredentialError](err); !ok || typedErr == nil {
		t.Errorf("expected loading to fail due to unsafe permissions; actual error = %s", err)
		t.FailNow()
	}
}
func TestFileStorageExecCredentialCache_Read_SucceedsIfValidJSON(t *testing.T) {
	expiresAt := time.Now().Add(5 * time.Minute).Truncate(time.Second)
	_, cacheFile := mustWriteTempCacheFile(t, fmt.Appendf([]byte{}, `{
		"kind": "ExecCredential",
		"apiVersion": "client.authentication.k8s.io/v1",
		"spec": { "interactive": false },
		"status": { "expirationTimestamp": "%D", "token": "devstack:averylongtoken" }}`, ExpirationTimestamp(expiresAt)))
	s := &FileStorageExecCredentialCache{}
	ec, err := s.Read(cacheFile)
	if err != nil {
		t.Errorf("expected unmarshalling to succeed: %v", err)
		t.FailNow()
	}
	expectedExpirationTimestamp := meta_v1.NewTime(expiresAt)
	assert.Equal(t, ec, &v1.ExecCredential{
		TypeMeta: meta_v1.TypeMeta{
			Kind:       "ExecCredential",
			APIVersion: "client.authentication.k8s.io/v1",
		},
		Spec: v1.ExecCredentialSpec{
			Interactive: false,
		},
		//nolint:gosec // G101: Potential hardcoded credentials // affects this test only
		Status: &v1.ExecCredentialStatus{
			ExpirationTimestamp: &expectedExpirationTimestamp,
			Token:               "devstack:averylongtoken",
		},
	})
}

func TestFileStorageExecCredentialCache_Write_WritesByCreatingCacheDir(t *testing.T) {
	//nolint:usetesting // this test checks that the cache dir is created if missing
	cacheDir := filepath.Join(os.TempDir(), "TestFileStorageExecCredentialCache_Write_WritesByCreatingCacheDir")
	defer os.RemoveAll(cacheDir)
	cacheFile := filepath.Join(cacheDir, "exec-credential")
	ec := &v1.ExecCredential{TypeMeta: meta_v1.TypeMeta{Kind: "ExecCredential"}}
	s := &FileStorageExecCredentialCache{}
	if err := s.Write(cacheFile, ec); err != nil {
		t.Error(err)
		t.FailNow()
	}
}

func TestFileStorageExecCredentialCache_Write_WritesWith0600Mode(t *testing.T) {
	cacheDir := t.TempDir()
	cacheFile := filepath.Join(cacheDir, "exec-credential")
	ec := &v1.ExecCredential{TypeMeta: meta_v1.TypeMeta{Kind: "ExecCredential"}}
	s := &FileStorageExecCredentialCache{}
	if err := s.Write(cacheFile, ec); err != nil {
		t.Error(err)
		t.FailNow()
	}
	fileInfo, err := os.Stat(cacheFile)
	if err != nil {
		t.Errorf("unable to perform os.Stat on %s: %s", cacheFile, err)
		t.FailNow()
	}
	expectedFileMode := fs.FileMode(0600)
	if !assert.Equal(t, fileInfo.Mode(), expectedFileMode) {
		t.Errorf("expected cache file to be created with Unix permission %o, got %o instead", expectedFileMode, fileInfo.Mode().Perm())
		t.FailNow()
	}
}

func TestFileStorageExecCredentialCache_Write_WritesToCacheAsJSON(t *testing.T) {
	cacheDir := t.TempDir()
	cacheFile := filepath.Join(cacheDir, "exec-credential")
	ec := &v1.ExecCredential{TypeMeta: meta_v1.TypeMeta{Kind: "ExecCredential"}}
	s := &FileStorageExecCredentialCache{}
	if err := s.Write(cacheFile, ec); err != nil {
		t.Error(err)
		t.FailNow()
	}
	contents, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Errorf("could not verify the contents written to exec credential cache file: %v", err)
		t.FailNow()
	}
	expected := `{"kind":"ExecCredential","spec":{"interactive":false}}`
	// force string comparison so the diff output uses strings instead of byte codes
	if !assert.Equal(t, string(contents), expected) {
		t.Errorf("the JSON contents of the exec credential cache file do not match expectations")
		t.FailNow()
	}
}

func createToken(t *testing.T, region string, withRegionPrefix bool) {
	AuthenticatedClientFunc = func(ctx context.Context, options gophercloud.AuthOptions) (*gophercloud.ProviderClient, error) {
		return &gophercloud.ProviderClient{HTTPClient: http.Client{Transport: &StubTransport{region}}}, nil
	}

	authOptions := gophercloud.AuthOptions{
		IdentityEndpoint: "http://devstack:5050/v3/",
		DomainID:         "Default",
		Username:         "me",
		Password:         "insecure",
	}

	cred, err := Do(t.Context(), authOptions, !withRegionPrefix)

	if err != nil {
		t.Errorf("failed to create token: %s", err)
	}

	expected := "my_token"
	if withRegionPrefix {
		expected = region + ":my_token"
	}

	if expected != cred.Status.Token {
		t.Errorf("expected token to be '%s', got '%s' instead", expected, cred.Status.Token)
	}
}

func mustWriteTempCacheFile(t *testing.T, contents []byte) (cacheDir, filePath string) {
	cacheDir = t.TempDir()
	filePath = filepath.Join(cacheDir, "exec-credential")
	if err := os.WriteFile(filePath, contents, 0600); err != nil {
		t.Fatalf("could not write file %s: %s", filePath, err)
	}
	return
}

func mustChmod(t *testing.T, filePath string, fileMode fs.FileMode) {
	if err := os.Chmod(filePath, fileMode); err != nil {
		t.Fatalf("could not change file mode: %s", err)
	}
}
