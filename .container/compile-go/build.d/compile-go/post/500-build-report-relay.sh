#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
#
# SPDX-License-Identifier: MIT

  b19-run "REPORT_RELAY" "$(_ "Download modules")" --     \
    go mod download

  b19-run "REPORT_RELAY" "$(_ "Build")" --      \
    go build -ldflags="-s -w -X main.version=${M6E_VERSION:-dev}" -o report-relay .

  b19-strip "REPORT_RELAY" report-relay

  b19-run "REPORT_RELAY" "$(_ "Make directory in export")" --     \
    mkdir -p /export/usr/local/bin

  b19-run "REPORT_RELAY" "$(_p "Copy %s to %s" "report-relay" "/export/usr/local/bin")" --     \
    cp report-relay /export/usr/local/bin
