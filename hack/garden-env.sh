#!/usr/bin/env bash

SCRATCH_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../_scratch"

# shellcheck disable=SC2155
# TODO DOES NOT exist
#export PATH="$PATH:$(realpath "$SCRATCH_DIR"/gardener/hack/tools/bin/*)"

# Darwin has ancient tools, we don't need such crutches on Linux :)
if [[ $(uname -s) == Darwin ]]; then
  BREW_PREFIX="$(brew --prefix)"
  export PATH="$BREW_PREFIX/opt/coreutils/libexec/gnubin:$PATH"
  export PATH="$BREW_PREFIX/opt/gnu-sed/libexec/gnubin:$PATH"
  export PATH="$BREW_PREFIX/opt/gnu-tar/libexec/gnubin:$PATH"
  export PATH="$BREW_PREFIX/opt/grep/libexec/gnubin:$PATH"
  export PATH="$BREW_PREFIX/opt/gzip/bin:$PATH"
fi
