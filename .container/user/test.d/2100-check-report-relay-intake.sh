#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
#
# SPDX-License-Identifier: MIT

set -eou pipefail

REPORT_RELAY_URL="http://localhost:${REPORT_RELAY_HTTP_PORT}"
ADMIN_URL="http://localhost:${REPORT_RELAY_ADMIN_PORT}"
CSP_BODY='{"csp-report":{"document-uri":"https://beta.dbuho.me/","violated-directive":"script-src","blocked-uri":"https://evil.example/x.js","effective-directive":"script-src","original-policy":"default-src '"'"'self'"'"'"}}'
REPORTING_BODY='[{"type":"csp-violation","age":5,"url":"https://beta.dbuho.me/","body":{"documentURL":"https://beta.dbuho.me/","effectiveDirective":"script-src","blockedURL":"https://evil.example/x.js"}}]'
TLSRPT_BODY='{"organization-name":"dbuho.me","contact-info":"tls-reports@dbuho.me","report-id":"2026-09-25T00:00:00Z","result-type":"aggregate","failure-info":{"result-type":"cancelled"}}'

fail() {
  echo "FATAL: $1" >&2
  exit 1
}

curl -sf "${ADMIN_URL}/healthz" > /dev/null || fail "healthz not answering on ${ADMIN_URL}"
curl -sf "${ADMIN_URL}/readyz" > /dev/null || fail "readyz not answering on ${ADMIN_URL}"

code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "${REPORT_RELAY_URL}/" \
  -H 'Content-Type: application/csp-report' --data "${CSP_BODY}")
[ "${code}" = "204" ] || fail "legacy CSP report answered ${code}, expected 204"

code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "${REPORT_RELAY_URL}/" \
  -H 'Content-Type: application/reports+json' --data "${REPORTING_BODY}")
[ "${code}" = "204" ] || fail "Reporting API batch answered ${code}, expected 204"

code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "${REPORT_RELAY_URL}/" \
  -H 'Content-Type: application/tlsrpt+json' --data "${TLSRPT_BODY}")
[ "${code}" = "204" ] || fail "TLS-RPT report answered ${code}, expected 204"

gzip -c <<< "${TLSRPT_BODY}" | curl -s -o /dev/null -w '%{http_code}' -X POST "${REPORT_RELAY_URL}/" \
  -H 'Content-Type: application/tlsrpt+gzip' -H 'Content-Encoding: gzip' --data-binary @- > "${B19_TEMP_PATH}/gz.code"
[ "$(cat "${B19_TEMP_PATH}/gz.code")" = "204" ] || fail "gzipped TLS-RPT report was not accepted"

code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "${REPORT_RELAY_URL}/" \
  -H 'Content-Type: text/plain' --data 'not a report')
[ "${code}" = "415" ] || fail "unknown content type answered ${code}, expected 415"

code=$(curl -s -o /dev/null -w '%{http_code}' -X OPTIONS "${REPORT_RELAY_URL}/" \
  -H 'Origin: https://beta.dbuho.me' -H 'Access-Control-Request-Method: POST' -H 'Access-Control-Request-Headers: content-type')
[ "${code}" = "204" ] || fail "CORS preflight answered ${code}, expected 204"

echo "Report Relay intake answered every wire format and every guard"
