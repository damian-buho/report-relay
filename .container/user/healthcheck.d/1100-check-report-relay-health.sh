#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
#
# SPDX-License-Identifier: MIT

set -o pipefail

ADMIN_PORT="${REPORT_RELAY_ADMIN_PORT}"

if ! curl -sf "http://localhost:${ADMIN_PORT}/healthz" > /dev/null; then
  b19-log bad "HEALTH.D" "$(_p "Health check failed on port %s" "${ADMIN_PORT}")"
  exit 1
fi

b19-log good "HEALTH.D" "$(_p "Service is responsive on port %s" "${ADMIN_PORT}")"
exit 0
