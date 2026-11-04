#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
#
# SPDX-License-Identifier: MIT

set -o pipefail

# Probes /readyz, the readiness endpoint (503 once exports fail); /healthz stays bare liveness for the server's callers.
ADMIN_PORT="${REPORT_RELAY_ADMIN_PORT}"

if ! curl -sf --max-time 2 "http://localhost:${ADMIN_PORT}/readyz" > /dev/null; then
  b19-log bad "HEALTH.D" "$(_p "Health check failed on port %s" "${ADMIN_PORT}")"
  exit 1
fi

b19-log good "HEALTH.D" "$(_p "Service is responsive on port %s" "${ADMIN_PORT}")"
exit 0
