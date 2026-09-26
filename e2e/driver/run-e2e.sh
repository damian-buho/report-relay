#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
#
# SPDX-License-Identifier: MIT
# Real browser to Loki: throwaway site, headless chrome, synthetic matrix, Loki label asserts.
set -eou pipefail
ROOT="$(dirname "$(dirname "$(dirname "$(readlink -f "$0")")")")"
RELAY_PORT="${E2E_RELAY_PORT:-8080}"
ADMIN_PORT="${E2E_ADMIN_PORT:-8081}"
SITE_PORT="${E2E_SITE_PORT:-8901}"
LOKI_URL="${E2E_LOKI_URL:-http://localhost:3100}"
TENANT="${E2E_TENANT:-o9s}"
OTLP_URL="${E2E_OTLP_URL:-http://localhost:4318}"
MARKER="e2e-$(date +%s%N)"
TMP="$(mktemp -d)"
RELAY_BIN="${TMP}/relay"
CHROME_PROFILE="${TMP}/chrome-profile"
CHROME_PORT="19222"
fail() { echo "FATAL: $1" >&2; exit 1; }
log() { echo "E2E: $1"; }
cleanup() { if [ "${E2E_KEEP_LOGS:-0}" = "1" ]; then rm -rf /tmp/e2e-fail; cp -r "${TMP}" /tmp/e2e-fail; fi; kill "${RELAY_PID:-}" "${SITE_PID:-}" "${CHROME_PID:-}" 2>/dev/null || true; rm -rf "${TMP}"; }
trap cleanup EXIT
[ "${E2E_KEEP_STACK:-0}" = "1" ] || true
log "marker is ${MARKER}"
docker compose -f "${ROOT}/e2e/compose.yaml" up -d >/dev/null || fail "compose up failed"
for _ in $(seq 1 60); do curl -sf -m 3 -H "X-Scope-OrgID: ${TENANT}" "${LOKI_URL}/loki/api/v1/labels" >/dev/null 2>&1 && break; sleep 2; done
curl -sf -m 3 -H "X-Scope-OrgID: ${TENANT}" "${LOKI_URL}/loki/api/v1/labels" >/dev/null || fail "loki never answered"
(cd "${ROOT}" && go build -o "${RELAY_BIN}" .) || fail "relay build failed"
REPORT_RELAY_HTTP_PORT="${RELAY_PORT}" REPORT_RELAY_ADMIN_PORT="${ADMIN_PORT}" OTEL_EXPORTER_OTLP_ENDPOINT="${OTLP_URL}" OTEL_EXPORTER_OTLP_INSECURE=true "${RELAY_BIN}" >"${TMP}/relay.log" 2>&1 &
RELAY_PID=$!
E2E_SITE_PORT="${SITE_PORT}" E2E_RELAY_URL="http://localhost:${RELAY_PORT}" python3 -u "${ROOT}/e2e/site/server.py" >"${TMP}/site.log" 2>&1 &
SITE_PID=$!
sleep 1
kill -0 "${SITE_PID}" 2>/dev/null || fail "site process died on startup"
for _ in $(seq 1 30); do curl -sf -m 2 "http://localhost:${ADMIN_PORT}/healthz" >/dev/null 2>&1 && break; sleep 1; done
curl -sf "http://localhost:${ADMIN_PORT}/healthz" >/dev/null || fail "relay healthz never answered"
curl -sf "http://localhost:${SITE_PORT}/healthz" >/dev/null || fail "site never answered"
google-chrome --headless=new --no-sandbox --disable-gpu --disable-dev-shm-usage --user-data-dir="${CHROME_PROFILE}" --remote-debugging-port="${CHROME_PORT}" about:blank >"${TMP}/chrome.log" 2>&1 &
CHROME_PID=$!
for _ in $(seq 1 30); do curl -sf -m 2 "http://localhost:${CHROME_PORT}/json/version" >/dev/null 2>&1 && break; sleep 1; done
python3 "${ROOT}/e2e/driver/cdp.py" "${CHROME_PORT}" "http://127.0.0.1:${SITE_PORT}" backgroundSync || fail "backgroundSync grant failed"
# Chrome 154 delivers no report while CSP names report-to: legacy goes quiet and the V1 batch never comes. The site therefore reports via report-uri only.
TAB_JSON="$(curl -sf -X PUT "http://localhost:${CHROME_PORT}/json/new?http://127.0.0.1:${SITE_PORT}/m/${MARKER}/")"
TAB_ID="$(printf '%s' "${TAB_JSON}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"
log "chrome tab ${TAB_ID} on the throwaway page"
sleep 15
curl -sf "http://localhost:${CHROME_PORT}/json/close/${TAB_ID}" >/dev/null || true
kill "${CHROME_PID}" 2>/dev/null || true
wait "${CHROME_PID}" 2>/dev/null || true
unset CHROME_PID
RELAY="http://localhost:${RELAY_PORT}"
post() { curl -s -o /dev/null -w '%{http_code}' -X POST "${RELAY}/" -H "Content-Type: $1" --data "$2"; }
expect204() { [ "$(post "$1" "$2")" = "204" ] || fail "POST $1 answered non-204"; }
U="https://beta.dbuho.me/e2e-${MARKER}"
log "posting the synthetic matrix"
expect204 'application/reports+json' '[{"type":"csp-violation","age":5,"url":"'"${U}"'/","body":{"documentURL":"'"${U}"'/","effectiveDirective":"script-src","blockedURL":"https://evil.example/x.js"}}]'
expect204 'application/reports+json' '[{"type":"coop","age":3,"url":"'"${U}"'/","body":{"disposition":"enforce","effectivePolicy":"same-origin","type":"navigation-to-response"}}]'
expect204 'application/reports+json' '[{"type":"coep","age":8,"url":"'"${U}"'/","body":{"type":"corp","blockedURL":"https://evil.example/x.js","destination":"script","disposition":"enforce"}}]'
expect204 'application/reports+json' '[{"type":"network-error","age":10,"url":"'"${U}"'/img.png","body":{"phase":"connection","type":"tcp.timed_out","method":"GET","protocol":"h2","referrer":"'"${U}"'/","sampling-fraction":1.0,"server-ip":"93.184.216.34","status-code":0,"elapsed-time":210}}]'
expect204 'application/reports+json' '[{"type":"deprecation","age":1,"url":"'"${U}"'/","body":{"id":"websql","message":"WebSQL is deprecated '"${MARKER}"'","sourceFile":"'"${U}"'/a.js"}}]'
expect204 'application/reports+json' '[{"type":"intervention","age":1,"url":"'"${U}"'/","body":{"id":"audio-no-gesture","message":"interrupted '"${MARKER}"'"}}]'
expect204 'application/reports+json' '[{"type":"crash","age":1,"url":"'"${U}"'/","body":{"reason":"oom '"${MARKER}"'"}}]'
expect204 'application/reports+json' '[{"type":"integrity-violation","age":2,"url":"'"${U}"'/","body":{"documentURL":"'"${U}"'/","blockedURL":"https://cdn.example/lib.js","destination":"script","reportOnly":false}}]'
expect204 'application/reports+json' '[{"type":"permissions-policy-violation","age":1,"url":"'"${U}"'/","body":{"policyId":"geolocation","disposition":"enforce","message":"denied '"${MARKER}"'"}}]'
expect204 'application/reports+json' '[{"type":"feature-policy-violation","age":1,"url":"'"${U}"'/","body":{"featureId":"geolocation","disposition":"enforce"}}]'
expect204 'application/reports+json' '[{"type":"potential-permissions-policy-violation","age":1,"url":"'"${U}"'/","body":{"policyId":"fullscreen","disposition":"report","message":"would block '"${MARKER}"'"}}]'
expect204 'application/reports+json' '[{"type":"document-policy-violation","age":1,"url":"'"${U}"'/","body":{"policyId":"document-write","disposition":"enforce","message":"blocked '"${MARKER}"'"}}]'
expect204 'application/reports+json' '[{"type":"certificate-transparency","age":7,"url":"'"${U}"'/","body":{"ct-policy":"scts","host":"beta.dbuho.me","marker":"'"${MARKER}"'"}}]'
expect204 'application/csp-report' '{"csp-report":{"document-uri":"'"${U}"'/","violated-directive":"script-src","blocked-uri":"https://evil.example/x.js","effective-directive":"script-src","original-policy":"default-src '"'"'self'"'"'"}}'
expect204 'application/expect-ct-report+json' '{"expect-ct-report":{"date-time":"2026-09-26T00:00:00Z","hostname":"beta.dbuho.me","port":443,"effective-expiration-date":"2026-10-26T00:00:00Z","served-certificate-chain":["'"${MARKER}"'"],"validated-certificate-chain":["PEM1"]}}'
expect204 'application/json' '{"date-time":"2026-09-26T00:00:00Z","hostname":"beta.dbuho.me","port":443,"effective-expiration-date":"2026-10-26T00:00:00Z","include-subdomains":false,"noted-hostname":"beta.dbuho.me","served-certificate-chain":["'"${MARKER}"'"],"validated-certificate-chain":["PEM1"],"known-pins":["pin-sha256=\"abcd\""]}'
expect204 'application/tlsrpt+json' '{"organization-name":"dbuho.me","contact-info":"tls@dbuho.me","report-id":"'"${MARKER}"'","result-type":"individual","failure-details":[{"result-type":"expired","resulting-cipher-suite":"TLS_AES_128_GCM_SHA256","server-name":"mx1.dbuho.me"}]}'
expect204 'application/tlsrpt+json' '{"organization-name":"dbuho.me","contact-info":"tls@dbuho.me","report-id":"'"${MARKER}"'-agg","result-type":"aggregate","failure-info":{"result-type":"aggregate","failing-sessions":{"expired":4}}}'
log "synthetic matrix accepted"
START=$(( ($(date +%s) - 600) * 1000000000 ))
query() { curl -s -m 10 -H "X-Scope-OrgID: ${TENANT}" -G "${LOKI_URL}/loki/api/v1/query_range" --data-urlencode "query=$1" --data-urlencode "start=${START}"; }
wait_for() { for _ in $(seq 1 45); do query "$1" | grep -q "${MARKER}" && return 0; sleep 2; done; return 1; }
wait_for "{service_name=\"report-relay\"} |= \"${MARKER}\"" || fail "marker never reached Loki"
log "marker reached Loki"
wait_for "{service_name=\"report-relay\"} |= \"/m/${MARKER}/\"" || fail "no line carries the throwaway page path; the browser sent nothing"
log "real browser reports reached Loki"
check_label() { wait_for "{service_name=\"report-relay\", event_name=\"$1\", event_domain=\"$2\"} |= \"${MARKER}\"" || fail "no line for {event_name=$1, event_domain=$2}"; log "label ok: $1/$2"; }
check_label "csp-violation" "browser"
check_label "coop" "browser"
check_label "coep" "browser"
check_label "network-error" "browser"
check_label "deprecation" "browser"
check_label "intervention" "browser"
check_label "crash" "browser"
check_label "integrity-violation" "browser"
check_label "permissions-policy-violation" "browser"
check_label "feature-policy-violation" "browser"
check_label "potential-permissions-policy-violation" "browser"
check_label "document-policy-violation" "browser"
check_label "certificate-transparency" "browser"
check_label "expect-ct" "browser"
check_label "hpkp" "browser"
check_label "expired" "mail"
check_label "aggregate" "mail"
log "every report type landed selectable by label under marker ${MARKER}"
