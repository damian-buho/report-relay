#!/bin/sh

# SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
#
# SPDX-License-Identifier: MIT

set -eu

# gh-release.sh — attach one matrix binary to the GitHub release, clobbering on a re-run.

version="${1:?usage: gh-release.sh <version>}"
goos="${GOOS:?GOOS must be set}"
goarch="${GOARCH:?GOARCH must be set}"
asset="dist/report-relay-${goos}-${goarch}"

log() { printf '[gh-release] %s\n' "$*" >&2; }

# Create wins on first run; upload-with-clobber wins on re-runs.
if gh release create "${version}" --generate-notes "${asset}"; then
    log "created release ${version} with ${asset}"
else
    log "release ${version} exists, uploading ${asset} (--clobber)"
    gh release upload "${version}" "${asset}" --clobber
fi
