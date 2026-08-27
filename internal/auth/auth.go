// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/adrg/xdg"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/client-go/pkg/apis/clientauthentication/v1"
)

type CacheStorageType string

const (
	// StorageDisk stores cached ExecCredential on file.
	FileStorage CacheStorageType = "file"
	// KeyringStorage stores cached ExecCredential in the OS keyring.
	KeyringStorage CacheStorageType = "keyring"
)

const (
	cacheRelDir = "scikube"
)

var (
	ErrCacheKeyNotFound                = errors.New("cache key not found")
	errMissingTokenExpirationTimestamp = errors.New("missing token expiration timestamp")
	errMissingTokenID                  = errors.New("missing token ID")

	AuthenticatedClientFunc func(ctx context.Context, options gophercloud.AuthOptions) (*gophercloud.ProviderClient, error) = openstack.AuthenticatedClient
)

type (
	ExpiredCachedExecCredentialError struct {
		cacheFile  string
		expiryDate time.Time
	}
	invalidCachedExecCredentialError struct {
		cacheFile string
		err       error
	}
	unreadableCachedExecCredentialError struct {
		cacheFile string
		err       error
	}
	unsafeCachedExecCredentialError struct {
		cacheFile string
		fileMode  fs.FileMode
	}

	ExecCredentialReadWriter interface {
		Read(key string) (*v1.ExecCredential, error)
		Write(key string, ec *v1.ExecCredential) error
	}

	FileStorageExecCredentialCache struct {
		// TODO: ???
	}

	KeyringStorageExecCredentialCache struct {
		// TODO: to be implemented
	}
)

func NewExecCredentialReadWriter(cst CacheStorageType) (ExecCredentialReadWriter, error) {
	switch cst {
	case FileStorage:
		return &FileStorageExecCredentialCache{}, nil
	case KeyringStorage:
		return nil, errors.New("keyring storage not yet implemented")
	default:
		return nil, fmt.Errorf("unknown storage type '%v'", cst)
	}
}

func (e *ExpiredCachedExecCredentialError) Error() string {
	return fmt.Sprintf("the cached exec credential stored in %s has expired on %s", e.cacheFile, e.expiryDate)
}

func (e *invalidCachedExecCredentialError) Error() string {
	return fmt.Sprintf("could not decode JSON contents of exec credential cache file %s: %s", e.cacheFile, e.err)
}

func (e *unreadableCachedExecCredentialError) Error() string {
	return fmt.Sprintf("could not read exec credential cache file %s: %s", e.cacheFile, e.err)
}

func (e *unsafeCachedExecCredentialError) Error() string {
	return fmt.Sprintf("exec credential cache file %s has unsafe permissions (expected = 0600 / actual = %o)", e.cacheFile, e.fileMode)
}

func (c *FileStorageExecCredentialCache) Read(key string) (*v1.ExecCredential, error) {
	if !FileExists(key) {
		return nil, ErrCacheKeyNotFound
	}
	data, err := os.ReadFile(key)
	if err != nil {
		return nil, &unreadableCachedExecCredentialError{key, err}
	}
	fileInfo, err := os.Stat(key)
	if err != nil {
		return nil, fmt.Errorf("unable to perform os.Stat on %s: %w", key, err)
	}
	if fileInfo.Mode().Perm() != fs.FileMode(0600) {
		return nil, &unsafeCachedExecCredentialError{key, fileInfo.Mode().Perm()}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var ec v1.ExecCredential
	if err := decoder.Decode(&ec); err != nil {
		return nil, &invalidCachedExecCredentialError{key, err}
	}
	if ec.Status == nil || ec.Status.Token == "" {
		return nil, errMissingTokenID
	}
	if ec.Status == nil || ec.Status.ExpirationTimestamp == nil || ec.Status.ExpirationTimestamp.Time.IsZero() {
		return nil, errMissingTokenExpirationTimestamp
	}
	if !ec.Status.ExpirationTimestamp.After(time.Now()) {
		return nil, &ExpiredCachedExecCredentialError{key, ec.Status.ExpirationTimestamp.Time}
	}
	return &ec, nil
}

func (c *FileStorageExecCredentialCache) Write(key string, ec *v1.ExecCredential) error {
	dirPath := filepath.Dir(key)
	if !dirExists(dirPath) {
		err := os.MkdirAll(dirPath, 0755)
		if err != nil {
			return fmt.Errorf("could not create cache directory %s: %w", dirPath, err)
		}
	}
	contents, err := json.Marshal(ec)
	if err != nil {
		return fmt.Errorf("could not encode exec credential as JSON: %w", err)
	}
	if err := os.WriteFile(key, contents, 0600); err != nil {
		return fmt.Errorf("could not write exec credential cache file: %w", err)
	}
	return nil
}

func Do(ctx context.Context, authOptions gophercloud.AuthOptions, noRegionPrefix bool) (*v1.ExecCredential, error) {
	providerClient, err := AuthenticatedClientFunc(ctx, authOptions)
	if err != nil {
		return nil, fmt.Errorf("could not create provider client: %w", err)
	}

	identityClient, err := openstack.NewIdentityV3(providerClient, gophercloud.EndpointOpts{})
	if err != nil {
		return nil, fmt.Errorf("could not create identity v3 client: %w", err)
	}

	resp := tokens.Create(ctx, identityClient, &authOptions)
	token, err := resp.ExtractToken()
	if err != nil {
		return nil, fmt.Errorf("could not extract token: %w", err)
	}

	catalog, err := resp.ExtractServiceCatalog()
	if err != nil {
		return nil, fmt.Errorf("could not extract service catalog: %w", err)
	}

	region := getRegion(catalog.Entries)

	var bearerToken string
	if region == "" || noRegionPrefix {
		bearerToken = token.ID
	} else {
		bearerToken = fmt.Sprintf("%s:%s", region, token.ID)
	}

	expiresAt := meta_v1.NewTime(token.ExpiresAt)
	cred := &v1.ExecCredential{
		TypeMeta: meta_v1.TypeMeta{
			Kind:       "ExecCredential",
			APIVersion: "client.authentication.k8s.io/v1",
		},
		Spec: v1.ExecCredentialSpec{
			Interactive: false,
		},
		Status: &v1.ExecCredentialStatus{
			ExpirationTimestamp: &expiresAt,
			Token:               bearerToken,
		},
	}

	return cred, nil
}

func getRegion(entries []tokens.CatalogEntry) string {
	for _, service := range entries {
		if service.Type == "identity" {
			for _, endpoint := range service.Endpoints {
				if endpoint.Interface == "public" {
					return endpoint.Region
				}
			}
		}
	}

	return ""
}

func CacheKey(cst CacheStorageType, ao gophercloud.AuthOptions) (string, error) {
	switch cst {
	case FileStorage:
		checksum, err := computeChecksum(ao)
		if err != nil {
			return "", err
		}
		return filepath.Join(cacheDir(""), checksum), nil
	case KeyringStorage:
		return "", errors.New("keyring storage not yet implemented")
	default:
		return "", fmt.Errorf("unknown storage type '%v'", cst)
	}
}

func CacheFile() string {
	return filepath.Join(cacheDir(""), "exec-credential")
}

// cacheDir returns the full path to the exec credential cache file directory.
// A baseDir argument is expected to accommodate unit testing.
func cacheDir(baseDir string) string {
	if baseDir == "" {
		baseDir = xdg.CacheHome
	}
	return filepath.Join(baseDir, cacheRelDir)
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return info.IsDir()
}

// FileExists returns true if the given path exists in the file system
func FileExists(path string) bool {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return err == nil
}

func computeChecksum(key gophercloud.AuthOptions) (string, error) {
	s := sha256.New()
	e := gob.NewEncoder(s)
	if err := e.Encode(&key); err != nil {
		return "", fmt.Errorf("could not encode the key: %w", err)
	}
	h := hex.EncodeToString(s.Sum(nil))
	return h, nil
}
