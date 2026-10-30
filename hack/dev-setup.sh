#!/usr/bin/env bash

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./hack/garden-env.sh
source "$SCRIPT_DIR/garden-env.sh"

export KUBECONFIG=$PWD/_scratch/gardener/dev-setup/kubeconfigs/runtime/kubeconfig
# Drop any inherited context override so the kubectl wrapper honours $KUBECONFIG.
unset KUBECONTEXT

# Create the persephone-config-(operator|webhook) secrets the components need.
# Each invocation issues a fresh OpenStack application credential and prints
# the corresponding Secret manifest; pipe straight into kubectl.
for PERSEPHONE_COMMAND in operator webhook; do
    "${SCRIPT_DIR}"/persephone-config-secret-local "$PERSEPHONE_COMMAND" | kubectl apply -n garden -f -
done

# Build and deploy the Persephone components. This also creates the
# authentication-webhook-config Secret that the garden patch below references,
# so it must run before the patch.
skaffold run

#patch 'local' garden resource to use the authentication webhook
kubectl patch garden local \
  --type=merge \
  --patch "$(cat <<EOF
spec:
  virtualCluster:
    kubernetes:
      kubeAPIServer:
        authentication:
          webhook:
            kubeconfigSecretName: authentication-webhook-config
            cacheTTL: 30s
            version: v1beta1
EOF
)"

# ---
# Sample code for fetching the persephone-config-(operator|webhook) secrets
# from a real landscape (Vault or a live cluster) instead of issuing a local
# application credential. Useful for reproducing landscape-specific issues.
# ---

# Requires 'containers-secrets' Vault access.
create_persephone_secrets_from_vault() {
    set -eou pipefail
    export KUBECONFIG=./_scratch/gardener/dev-setup/kubeconfigs/runtime/kubeconfig
    for PERSEPHONE_COMMAND in operator webhook; do
        "${SCRIPT_DIR}"/persephone-config-secret "$PERSEPHONE_COMMAND" | kubectl -n garden apply -f -
    done
}

create_persephone_secrets_from_remote_cluster() {
    local CONFIG
    set -eou pipefail
    export KUBECONFIG=./_scratch/gardener/dev-setup/kubeconfigs/runtime/kubeconfig
    for PERSEPHONE_COMMAND in operator webhook; do
        CONFIG="$(KUBECONFIG="$HOME"/SAPCC/u8s/.kube/config kubectl --context prt-q-eu-de-1 --namespace garden get persephone-config-"$PERSEPHONE_COMMAND" -o yaml | yq '.data["config.yaml"]')"
        kubectl apply -n garden -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: persephone-config-${PERSEPHONE_COMMAND}
type: Opaque
data:
  config.yaml: ${CONFIG}
EOF
    done
}
