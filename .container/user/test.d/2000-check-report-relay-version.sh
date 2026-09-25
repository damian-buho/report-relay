#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
#
# SPDX-License-Identifier: MIT

set -eou pipefail

ACTUAL=$(get-report-relay-version)
EXPECTED=${M6E_VERSION:-}

if [ "${ACTUAL}" != "${EXPECTED}" ]
then
  echo "FATAL: expected ${EXPECTED}, got ${ACTUAL}" >&2
  exit 1
fi
