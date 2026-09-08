<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors

SPDX-License-Identifier: Apache-2.0
-->

# Project Persephone 'hack' scripts

This directory contains ad-hoc scripts. Code quality is ensured by `shellcheck` via `make run-shellcheck`.

## Managing license headers

To avoid issues with `make license-headers`, there are two scripts to help keep all Go files in check.

* `repo-management--add-go-files-license-headers.bash`: adds the license header to all files of interest
* `repo-management--strip-go-files-license-headers.bash`: strips the license header to all files of interest

## Running integration tests

See `test-integration.bash`.
