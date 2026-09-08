#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
# SPDX-License-Identifier: Apache-2.0

set -eoux pipefail

find cmd internal test -type f -name '*.go' \
    | while read -r FILE_PATH; do
          reuse annotate \
              --copyright "SAP SE or an SAP affiliate company and Project Persephone contributors" \
              --license 'Apache-2.0' \
              --year '2026' \
              --style 'cppsingle' \
              --skip-existing \
              "$FILE_PATH"
      done
