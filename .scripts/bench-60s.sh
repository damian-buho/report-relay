#!/bin/sh

# SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
#
# SPDX-License-Identifier: MIT

set -eu

# bench-60s.sh — answer "how many reports in 60s" against the real binary.
# usage: bench-60s.sh [duration_s [concurrency [http_port [admin_port]]]

duration="${1:-60}"
workers="${2:-32}"
http_port="${3:-18080}"
admin_port="${4:-18081}"

tmp="$(mktemp -d)"
cleanup() { rm -rf "${tmp}"; }
trap 'cleanup; kill "${srvpid:-}" 2>/dev/null || true' EXIT INT TERM

log() { printf '[bench-60s] %s\n' "$*" >&2; }

go build -o "${tmp}/report-relay" .
log "binary built"

mkfifo "${tmp}/out.fifo"
wc -l <"${tmp}/out.fifo" >"${tmp}/lines" &
wcpid=$!

env -u OTEL_EXPORTER_OTLP_ENDPOINT -u OTEL_EXPORTER_OTLP_LOGS_ENDPOINT \
  REPORT_RELAY_LOG_LEVEL=error \
  REPORT_RELAY_HTTP_PORT="${http_port}" \
  REPORT_RELAY_ADMIN_PORT="${admin_port}" \
  REPORT_RELAY_RATE_LIMIT_RPS=1000000 \
  REPORT_RELAY_RATE_LIMIT_BURST=1000000 \
  REPORT_RELAY_QUEUE_SIZE=200000 \
  "${tmp}/report-relay" >"${tmp}/out.fifo" 2>"${tmp}/stderr.log" &
srvpid=$!

i=0
while [ "${i}" -lt 100 ]; do
  if python3 -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:${admin_port}/healthz', timeout=1).read()" 2>/dev/null; then
    break
  fi
  i=$((i + 1))
  sleep 0.1
done
log "server up on 127.0.0.1:${http_port} (admin :${admin_port})"

python3 - "${duration}" "${workers}" "${http_port}" <<'EOF'
import http.client, json, sys, threading, time
duration, workers, port = float(sys.argv[1]), int(sys.argv[2]), int(sys.argv[3])
body = b'[{"type":"deprecation","age":1,"url":"https://beta.dbuho.me/","body":{"id":"websql","message":"WebSQL is deprecated"}}]'
deadline = time.monotonic() + duration
counts, lats, lock = {"204": 0, "err": 0, "other": {}}, [], threading.Lock()
def work():
    local = []
    ok = other = errs = 0
    conn = http.client.HTTPConnection("127.0.0.1", port, timeout=10)
    while time.monotonic() < deadline:
        t = time.perf_counter()
        try:
            conn.request("POST", "/", body=body, headers={"Content-Type": "application/reports+json"})
            r = conn.getresponse()
            r.read()
        except Exception:
            errs += 1
            conn = http.client.HTTPConnection("127.0.0.1", port, timeout=10)
            continue
        local.append((time.perf_counter() - t) * 1000.0)
        if r.status == 204:
            ok += 1
        else:
            other += 1
    with lock:
        counts["204"] += ok
        counts["err"] += errs
        counts["other"] = {**counts["other"], "non204": counts["other"].get("non204", 0) + other}
        lats.extend(local)
threads = [threading.Thread(target=work) for _ in range(workers)]
[t.start() for t in threads]
[t.join() for t in threads]
lats.sort()
n = len(lats)
pct = lambda q: lats[min(n - 1, int(q * n))] if n else 0.0
print(json.dumps({"requests": n + counts["err"], "accepted_204": counts["204"],
  "non_204": counts["other"].get("non204", 0), "conn_errors": counts["err"],
  "req_per_s": round((n + counts["err"]) / duration, 1),
  "lat_ms_p50": round(pct(0.50), 3), "lat_ms_p99": round(pct(0.99), 3)}))
EOF

kill "${srvpid}" 2>/dev/null || true
wait "${srvpid}" 2>/dev/null || true
wait "${wcpid}" 2>/dev/null || true
log "stdout records exported: $(cat "${tmp}/lines")"
log "stderr tail: $(tail -c 500 "${tmp}/stderr.log" | tr '\n' ' ')"
