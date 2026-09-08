#!/usr/bin/env bash
# SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
#
# SPDX-License-Identifier: Apache-2.0

set -o errexit
set -o nounset
set -o pipefail

echo "> Integration Tests"

if ! type -P setup-envtest &>/dev/null; then
  # ref https://github.com/kubernetes-sigs/controller-runtime/tree/main/tools/setup-envtest#envtest-binaries-manager
  echo
  echo "> Ensuring setup-envtest is installed"
  CONTROLLER_RUNTIME_VERSION="$(go list -mod=mod -f '{{ .Version }}' -m sigs.k8s.io/controller-runtime)"
  go install "sigs.k8s.io/controller-runtime/tools/setup-envtest@release-$(printf "%s" "$CONTROLLER_RUNTIME_VERSION" | sed -E 's/^v([0-9]+)\.([0-9]+).*/\1.\2/')"
fi

export ENVTEST_K8S_VERSION=${ENVTEST_K8S_VERSION:-"1.34"}
KUBEBUILDER_ASSETS="$(setup-envtest use --use-env -p path "${ENVTEST_K8S_VERSION}")"
export KUBEBUILDER_ASSETS

# --use-env allows overwriting the envtest tools path via the KUBEBUILDER_ASSETS env var
echo "using envtest tools installed at '$KUBEBUILDER_ASSETS'"

echo
echo "> Starting integration tests..."

# reduce flakiness in contended pipelines
export KUBEBUILDER_CONTROLPLANE_START_TIMEOUT=2m
export GOMEGA_DEFAULT_EVENTUALLY_TIMEOUT=5s
export GOMEGA_DEFAULT_EVENTUALLY_POLLING_INTERVAL=200ms
# if we're running low on resources, it might take longer for tested code to do something "wrong"
# poll for 5s to make sure, we're not missing any wrong action
export GOMEGA_DEFAULT_CONSISTENTLY_DURATION=5s
export GOMEGA_DEFAULT_CONSISTENTLY_POLLING_INTERVAL=200ms

go test -timeout=5m "$@" | grep -v 'no test files'
