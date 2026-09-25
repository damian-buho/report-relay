#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
#
# SPDX-License-Identifier: MIT

set -eou pipefail

# shellcheck source=/dev/null
. b19-i18n

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

# ── End to end: the record must survive the real collector and the real store ──
#
# The point is not that the intake accepts the report — 2100 proved that — but
# that the RECORD lands in Loki with the labels the fleet's queries rely on. A
# record that arrives unlabelled is a silent failure for every dashboard, so the
# assertion is on the labels, not merely on a line existing.

LOKI_URL="http://${O9S_LOKI_HOST:-o9s-loki}:${O9S_LOKI_PORT:-8080}"
TENANT="${O9S_LOKI_TENANT_ID:-o9s}"
MARKER="probe-$(date +%s%N)"
E2E_BODY='{"csp-report":{"document-uri":"https://beta.dbuho.me/'"$MARKER"'","violated-directive":"script-src","blocked-uri":"https://evil.example/x.js","effective-directive":"script-src"}}'

code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "${REPORT_RELAY_URL}/" \
  -H 'Content-Type: application/csp-report' --data "${E2E_BODY}")
[ "${code}" = "204" ] || fail "end-to-end probe answered ${code}, expected 204"

# The exporter batches on an interval, so the line is not there the instant the
# intake answered. Poll, and report how long it took rather than sleeping blind.
# The window is epoch NANOSECONDS: a relative "0s" is rejected by this Loki.
START=$(( ($(date +%s) - 600) * 1000000000 ))
QUERY="{service_name=\"report-relay\"} |= \"${MARKER}\""
found=0
for attempt in $(seq 1 30); do
  # A poll that times out is a poll that found nothing yet, not a reason to abort:
  # the exporter and the collector are both still batching.
  response=$(curl -s -m 10 -H "X-Scope-OrgID: ${TENANT}" \
    -G "${LOKI_URL}/loki/api/v1/query_range" \
    --data-urlencode "query=${QUERY}" --data-urlencode "start=${START}" || true)
  if printf '%s' "${response}" | grep -q "${MARKER}"; then
    b19-log good "E2E" "$(_p "marker %s reached Loki after %s attempt(s)" "${MARKER}" "${attempt}")"
    found=1
    break
  fi
  b19-log info "E2E" "$(_p "marker %s not in Loki yet, attempt %s of 30" "${MARKER}" "${attempt}")"
  sleep 1
done
[ "${found}" = "1" ] || fail "the report never reached Loki under marker ${MARKER} after 30 attempts"

# The label contract, proved the way an operator uses it: the report type must
# be SELECTABLE. event.name is only a Loki label because the record carries it
# as an attribute — the first-class OTLP field of the same name is invisible to
# the collector's label promotion, and a type no query can select is a report
# nobody ever reads.
LABELLED=$(curl -s -m 10 -H "X-Scope-OrgID: ${TENANT}" \
  -G "${LOKI_URL}/loki/api/v1/query_range" \
  --data-urlencode "query={service_name=\"report-relay\", event_name=\"csp-violation\", event_domain=\"browser\"} |= \"${MARKER}\"" \
  --data-urlencode "start=${START}" || true)
printf '%s' "${LABELLED}" | grep -q "${MARKER}" \
  || fail "the line landed but is not selectable by {event_name, event_domain}; a query for the report type would return nothing"

echo "Report Relay record reached Loki labelled as the fleet queries expect"


