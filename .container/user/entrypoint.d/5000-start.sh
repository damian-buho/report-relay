#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
#
# SPDX-License-Identifier: MIT

  if [ "${ENTRYPOINT_COMMAND_EXECUTED:-N}" == "N" ]; then
    b19-log info "REPORT_RELAY" "$(_ "Starting")"

    b19-exec -- report-relay
  fi
