#!/bin/sh

# SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
#
# SPDX-License-Identifier: MIT

set -eu

# install-binary.sh — build report-relay host-native and install it into ~/.local/bin.

dst="${HOME}/.local/bin"
bin=report-relay

log() { printf '[install-binary] %s\n' "$*" >&2; }

# Build host-native (no version arg → build-binaries derives one from git).
log "building host-native ${bin}"
asset="$(.scripts/build-binaries.sh)"

mkdir -p "${dst}"
install -m 0755 "${asset}" "${dst}/${bin}"
log "installed ${asset} -> ${dst}/${bin}"
