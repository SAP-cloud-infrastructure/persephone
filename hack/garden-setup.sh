#!/usr/bin/env bash

GARDENER_REPO="https://github.com/gardener/gardener.git"
# Pinned Gardener version. Bump only after verifying compatibility with the
# Persephone local environment.
GARDENER_TAG="v1.142.0"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./hack/garden-env.sh
source "$SCRIPT_DIR/garden-env.sh"

# Create scratch dir if it doesn't exist
mkdir -p "$SCRATCH_DIR"

CLONE_PATH="$SCRATCH_DIR/gardener"
# Clone the repo if not already present
if [[ ! -d "$CLONE_PATH/.git" ]]; then
  git clone --branch "$GARDENER_TAG" "$GARDENER_REPO" "$CLONE_PATH"
else
  echo "Repository already exists at $CLONE_PATH, skipping clone."
  # Ensure the working tree is on the pinned tag
  git -C "$CLONE_PATH" fetch --tags
  git -C "$CLONE_PATH" checkout "$GARDENER_TAG"
fi

# Bring up the local Gardener landscape per
# https://gardener.cloud/docs/gardener/deployment/getting_started_locally/
make -C "$CLONE_PATH" kind-up gardener-up
