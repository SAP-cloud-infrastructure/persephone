package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gophercloud/gophercloud/v2"

	"github.com/sap-cloud-infrastructure/persephone/internal/auth"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/client-go/pkg/apis/clientauthentication/v1"
)

func TestAuthCmd_FailsIfAuthURLMissing(t *testing.T) {
	t.Setenv("OS_AUTH_URL", "")
	err := authenticate(authCmd, []string{})
	if err == nil {
		t.Fatal("expected `authenticate()` to fail")
	}
	if !errors.Is(err, errAuthURLMissing) {
		t.Fatalf("expected `authenticate()` to fail due to missing auth URL; actual error = %s", err)
	}
}

func TestAuthCmd_FailsIfPasswordAuthIncomplete(t *testing.T) {
	domain = ""
	user = ""
	password = "insecure"
	t.Setenv("OS_AUTH_URL", "http://devstack:5050/v3")
	t.Setenv("OS_DOMAIN_NAME", domain)
	t.Setenv("OS_PROJECT_NAME", project)
	t.Setenv("OS_USERNAME", user)
	t.Setenv("OS_PASSWORD", password)
	err := authenticate(authCmd, []string{})
	if err == nil {
		t.Fatal("expected `authenticate()` to fail")
	}
	if !strings.HasPrefix(err.Error(), "when using password auth, please provide") {
		t.Fatalf("expected `authenticate()` to fail due to incomplete password auth; actual error = %s", err)
	}
}

func TestAuthCmd_FailsIfKeystoneAuthFails(t *testing.T) {
	domain = "unit_testing"
	project = "auth"
	user = "go"
	password = "insecure"
	authFunc = func(ctx context.Context, authOptions gophercloud.AuthOptions, noRegionPrefix bool) (*v1.ExecCredential, error) {
		t.Log("Hello please")
		return nil, errors.New("auth failed")
	}
	cacheKeyFunc = func(_ auth.CacheStorageType, _ gophercloud.AuthOptions) (string, error) {
		return "", errors.New("can not determine a cache key - please reauth with Keystone")
	}
	t.Setenv("OS_AUTH_URL", "http://devstack:5050/v3")
	t.Setenv("OS_DOMAIN_NAME", domain)
	t.Setenv("OS_PROJECT_NAME", project)
	t.Setenv("OS_USERNAME", user)
	t.Setenv("OS_PASSWORD", password)
	err := authenticate(authCmd, []string{})
	if err == nil {
		t.Fatal("expected `authenticate()` to fail")
	}
	if typedErr, ok := errors.AsType[*keystoneAuthError](err); !ok || typedErr == nil {
		t.Fatalf("expected `authenticate()` to fail due to Keystone auth failure; actual error = %s", err)
	}
}

// scenario: cache file path is not readable.
func TestAuthCmd_SucceedsByAuthenticatingWithKeystone_IfDeterminingCacheFileFails(t *testing.T) {
	domain = "unit_testing"
	project = "auth"
	user = "go"
	password = "insecure"
	authFunc = func(ctx context.Context, authOptions gophercloud.AuthOptions, noRegionPrefix bool) (*v1.ExecCredential, error) {
		// auth succeeds
		return &v1.ExecCredential{Status: &v1.ExecCredentialStatus{}}, nil
	}
	// cache file can NOT be determined
	cacheKeyFunc = func(_ auth.CacheStorageType, _ gophercloud.AuthOptions) (string, error) {
		return "", errors.New("cache file path can not be determined; writing to cache would also fail")
	}
	t.Setenv("OS_AUTH_URL", "http://devstack:5050/v3")
	t.Setenv("OS_DOMAIN_NAME", domain)
	t.Setenv("OS_PROJECT_NAME", project)
	t.Setenv("OS_USERNAME", user)
	t.Setenv("OS_PASSWORD", password)
	err := authenticate(authCmd, []string{})
	if err != nil {
		t.Fatalf("expected `authenticate()` to succeed but failed with error = %s", err)
	}
}

// scenario: cache file path is both readable and writable and not yet a file.
func TestAuthCmd_SucceedsByAuthenticatingWithKeystone_IfNoCacheFileExists_AndWritingToCacheSucceeds(t *testing.T) {
	domain = "unit_testing"
	project = "auth"
	user = "go"
	password = "insecure"
	// auth succeeds
	authFunc = func(ctx context.Context, authOptions gophercloud.AuthOptions, noRegionPrefix bool) (*v1.ExecCredential, error) {
		return &v1.ExecCredential{Status: &v1.ExecCredentialStatus{}}, nil
	}
	// cache file path can be determined because proper read permissions in place, but cache file does not exist
	cacheKeyFunc = func(_ auth.CacheStorageType, _ gophercloud.AuthOptions) (string, error) {
		return filepath.Join(t.TempDir(), "exec_credential"), nil
	}
	t.Setenv("OS_AUTH_URL", "http://devstack:5050/v3")
	t.Setenv("OS_DOMAIN_NAME", domain)
	t.Setenv("OS_PROJECT_NAME", project)
	t.Setenv("OS_USERNAME", user)
	t.Setenv("OS_PASSWORD", password)
	err := authenticate(authCmd, []string{})
	if err != nil {
		t.Fatalf("expected `authenticate()` to succeed but failed with error = %s", err)
	}
}

// scenario: cache file path is readable but not writable.
//
// as long as `auth.ExecCredentialReadWriter.Read()` returns an error if file
// permissions are not 600, succeeding to read but failing to write should not
// be possible.
func TestAuthCmd_SucceedsByAuthenticatingWithKeystone_IfNoCacheFileExists_AndWritingToCacheFails(t *testing.T) {
	// empty
}

// scenario: cache credential is loaded, verified to have not expired, auth is
// skipped, writing to cache is skipped.
func TestAuthCmd_SucceedsBySkippingKeystoneAuth_IfCachedCredentialsExist_AndHaveNotExpired(t *testing.T) {
	domain = "unit_testing"
	project = "auth"
	user = "go"
	password = "insecure"
	// we force auth to fail (it should not be called by `authenticate`)
	authFunc = func(ctx context.Context, authOptions gophercloud.AuthOptions, noRegionPrefix bool) (*v1.ExecCredential, error) {
		return &v1.ExecCredential{}, errors.New("function `auth.Do()` should not have been called")
	}
	//nolint:gosec // this is a unit test
	token := "devstack:my_fresh_token"
	expiresAt := time.Now().Add(5 * time.Minute).UTC()
	// these credentials should be considered fresh and should cause `auth.Do()` to be skipped
	cacheFile := mustWriteTempCacheFile(t, []byte(`{
		"kind": "ExecCredential",
		"apiVersion": "client.authentication.k8s.io/v1",
		"spec": { "interactive": false },
		"status": { "expirationTimestamp": "`+expiresAt.Format("2006-01-02T15:04:05Z")+`", "token": "`+token+`"}}`))
	cacheKeyFunc = func(cst auth.CacheStorageType, _ gophercloud.AuthOptions) (string, error) {
		return cacheFile, nil
	}
	t.Setenv("OS_AUTH_URL", "http://devstack:5050/v3")
	t.Setenv("OS_DOMAIN_NAME", domain)
	t.Setenv("OS_PROJECT_NAME", project)
	t.Setenv("OS_USERNAME", user)
	t.Setenv("OS_PASSWORD", password)
	err := authenticate(authCmd, []string{})
	if err != nil {
		t.Fatalf("expected `authenticate()` to succeed but failed with error = %s", err)
	}
	cst := &auth.FileStorageExecCredentialCache{}
	ec, err := cst.Read(cacheFile)
	if err != nil {
		t.Fatalf("expected `auth.LoadExecCredentialsFromFile()` to succeed but failed with error = %s", err)
	}
	if token != ec.Status.Token {
		t.Errorf("expected token in cache file to have been left unchanged as '%s', got '%s' instead", token, ec.Status.Token)
	}
	expectedExpirationTimestamp := meta_v1.NewTime(expiresAt).Format(time.RFC3339)
	actualExpirationTimestamp := ec.Status.ExpirationTimestamp.UTC().Format(time.RFC3339)
	if actualExpirationTimestamp != expectedExpirationTimestamp {
		t.Errorf("expected expiration timestamp in cache file to have been left unchanged as '%s', got '%s' instead", expectedExpirationTimestamp, actualExpirationTimestamp)
	}
}

// scenario: cache credential is loaded, discarded due to having expired, auth
// happens then fresh credentials are cached to file.
func TestAuthCmd_SucceedsByReauthenticatingWithKeystone_IfCachedCredentialsExist_ButHaveExpired(t *testing.T) {
	domain = "unit_testing"
	project = "auth"
	user = "go"
	password = "insecure"
	freshTokenExpiresAt := time.Now().Add(5 * time.Minute).UTC()
	token := "some_region:the_token"
	// auth succeeds
	authFunc = func(ctx context.Context, authOptions gophercloud.AuthOptions, noRegionPrefix bool) (*v1.ExecCredential, error) {
		ts := meta_v1.NewTime(freshTokenExpiresAt)
		return &v1.ExecCredential{
			TypeMeta: meta_v1.TypeMeta{Kind: "ExecCredential", APIVersion: "client.authentication.k8s.io/v1"},
			Spec:     v1.ExecCredentialSpec{Interactive: false},
			Status:   &v1.ExecCredentialStatus{ExpirationTimestamp: &ts, Token: token},
		}, nil
	}
	expiresAt := time.Now().Add(-5 * time.Minute).UTC().Format("2006-01-02T15:04:05Z")
	cacheFile := mustWriteTempCacheFile(t, []byte(`{
		"kind": "ExecCredential",
		"apiVersion": "client.authentication.k8s.io/v1",
		"spec": { "interactive": false },
		"status": { "expirationTimestamp": "`+expiresAt+`", "token": "devstack:my_expired_token" }}`))
	cacheKeyFunc = func(_ auth.CacheStorageType, _ gophercloud.AuthOptions) (string, error) {
		return cacheFile, nil
	}
	t.Setenv("OS_AUTH_URL", "http://devstack:5050/v3")
	t.Setenv("OS_DOMAIN_NAME", domain)
	t.Setenv("OS_PROJECT_NAME", project)
	t.Setenv("OS_USERNAME", user)
	t.Setenv("OS_PASSWORD", password)
	err := authenticate(authCmd, []string{})
	if err != nil {
		t.Fatalf("expected `authenticate()` to succeed but failed with error = %s", err)
	}
	cst := &auth.FileStorageExecCredentialCache{}
	ec, err := cst.Read(cacheFile)
	if err != nil {
		t.Fatalf("expected `auth.FileStorageExecCredentialCache.Read()` to succeed but failed with error = %s", err)
	}
	if token != ec.Status.Token {
		t.Errorf("expected token to have been stored in cache file as '%s', got '%s' instead", token, ec.Status.Token)
	}
	expectedExpirationTimestamp := meta_v1.NewTime(freshTokenExpiresAt).Format(time.RFC3339)
	actualExpirationTimestamp := ec.Status.ExpirationTimestamp.UTC().Format(time.RFC3339)
	if actualExpirationTimestamp != expectedExpirationTimestamp {
		t.Errorf("expected expiration timestamp to have been stored in cache file as '%s', got '%s' instead", expectedExpirationTimestamp, actualExpirationTimestamp)
	}
}

func mustWriteTempCacheFile(t *testing.T, contents []byte) string {
	filePath := filepath.Join(t.TempDir(), "exec-credential")
	if err := os.WriteFile(filePath, contents, 0600); err != nil {
		t.Fatalf("could not write file %s: %s", filePath, err)
	}
	return filePath
}
