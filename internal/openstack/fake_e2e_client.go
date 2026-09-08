// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package openstack

import (
	"context"
	"crypto/md5" //nolint:gosec // only used to generate none security relebant IDs in tests
	"fmt"
	"net/http"
	"strings"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	th "github.com/gophercloud/gophercloud/v2/testhelper"
)

type fakeCredential struct {
	Name      string
	Roles     []string
	ExpiresAt string
}

type fakeE2EOptions struct {
	Credentials []fakeCredential
	UserName    string
}

var FakeE2EOptions = fakeE2EOptions{}

func GetFakeE2EClient(ctx context.Context, authOptions gophercloud.AuthOptions) (*gophercloud.ProviderClient, error) {
	fakeServer := th.SetupHTTP()

	fakeServer.Mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{
			"versions": {
				"values": [
					{
						"id": "v3.0",
						"status": "stable",
						"links": [
							{
								"rel": "self",
								"href": "`+fakeServer.Endpoint()+`v3/"
							}
						]
					}
				]
			}
		}`)
	})

	fakeServer.Mux.HandleFunc("POST /v3/auth/tokens", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Subject-Token", "fake-token-id")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{
			"token": {
				"expires_at": "2030-01-01T00:00:00Z",
				"catalog": []
			}
		}`)
	})

	fakeServer.Mux.HandleFunc(fmt.Sprintf("GET /v3/users/%s/application_credentials", FakeE2EOptions.UserName), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		var application_credential string
		userHash := fmt.Sprintf("%x", md5.Sum([]byte(FakeE2EOptions.UserName))) //nolint:gosec // tests only
		for _, credential := range FakeE2EOptions.Credentials {
			var roles string
			for i, credentialRole := range credential.Roles {
				hash := fmt.Sprintf("%x", md5.Sum([]byte(credentialRole))) //nolint:gosec // tests only
				roles += `{
					"id": "` + hash + `",
					"domain_id": null,
					"name": "` + credentialRole + `"
					}`

				if i < len(credential.Roles)-1 {
					roles += ","
				}
			}

			hash := fmt.Sprintf("%x", md5.Sum([]byte(credential.Name+roles))) //nolint:gosec // tests only
			//nolint:modernize // tests only
			application_credential += `{
				"links": {
					"self": "https://` + fakeServer.Endpoint() + `/users/` + userHash + `/application_credentials/` + hash + `"
				},
				"description": null,
				"roles": [` + roles +
				`	],
				"access_rules": [ ],
				"expires_at": "` + credential.ExpiresAt + `",
				"secret": "mysecret",
				"unrestricted": false,
				"project_id": "53c2b94f63fb4f43a21b92d119ce549f",
				"id": "` + hash + `",
				"name": "` + credential.Name + `"
			}`
		}

		fmt.Fprint(w, `{
			"links": {
				"self": "https://`+fakeServer.Endpoint()+`/users/`+userHash+`/application_credentials",
				"previous": null,
				"next": null
			},
			"application_credentials": [`+application_credential+`]
		}`)
	})

	fakeServer.Mux.HandleFunc(fmt.Sprintf("POST /v3/users/%s/application_credentials", FakeE2EOptions.UserName), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)

		var rolesBuilder strings.Builder
		for i, credentialRole := range FakeE2EOptions.Credentials[0].Roles {
			hash := fmt.Sprintf("%x", md5.Sum([]byte(credentialRole))) //nolint:gosec // tests only
			rolesBuilder.WriteString(`{
				"id": "` + hash + `",
				"domain_id": null,
				"name": "` + credentialRole + `"
				}`)

			if i < len(FakeE2EOptions.Credentials[0].Roles)-1 {
				rolesBuilder.WriteString(",")
			}
		}
		roles := rolesBuilder.String()

		hash := fmt.Sprintf("%x", md5.Sum([]byte(FakeE2EOptions.Credentials[0].Name+roles))) //nolint:gosec // tests only
		userHash := fmt.Sprintf("%x", md5.Sum([]byte(FakeE2EOptions.UserName)))              //nolint:gosec // tests only
		fmt.Fprint(w, `{
			"application_credential": {
				"links": {
					"self": "https://identity/v3/users/`+userHash+`/application_credentials/`+hash+`"
				},
				"description": null,
				"roles": [`+roles+
			`	],
				"access_rules": [ ],
				"expires_at": "`+FakeE2EOptions.Credentials[0].ExpiresAt+`",
				"secret": "mysecret",
				"unrestricted": false,
				"project_id": "53c2b94f63fb4f43a21b92d119ce549f",
				"id": "f741662395b249c9b8acdebf1722c5ae",
				"name": "`+FakeE2EOptions.Credentials[0].Name+`"
			}
		}`)
	})

	client, err := openstack.NewClient(fakeServer.Endpoint())
	if err != nil {
		return nil, err
	}

	err = openstack.Authenticate(ctx, client, authOptions)
	if err != nil {
		return nil, err
	}

	return client, nil
}
