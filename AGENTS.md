<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

# damian-buho/report-relay

Docker image built on [b19/go](../../b19/go/AGENTS.md) and [b19/Ubuntu](../../b19/ubuntu/AGENTS.md)

OpenTelemetry relay for the security reports browsers and mail servers send.

## Key facts

- Builder: `b19/go`; the whole Go application lives in the project directory
- Final Base: `b19/ubuntu/resolute`
- Arch: amd64, arm64
- **No upstream version pin** — the source is local, versioned by the projectfile
- Reports are an OPEN list: a type this build has never seen still arrives, because
  the Reporting API envelope is decoded without typing the body. Per-type validation
  is a hook in `internal/intake/media.go` (`RegisterBodyHook`), never a closed switch
  in the decoder.

## ENV

- `REPORT_RELAY_LOG_LEVEL=info`
- `REPORT_RELAY_HTTP_PORT=8080` (public intake)
- `REPORT_RELAY_ADMIN_PORT=8081` (`/healthz`, `/readyz`)
- `REPORT_RELAY_MAX_BODY_BYTES=65536`, `REPORT_RELAY_MAX_JSON_DEPTH=32`, `REPORT_RELAY_MAX_ARRAY_ITEMS=512`
- `REPORT_RELAY_RATE_LIMIT_RPS=20`, `REPORT_RELAY_RATE_LIMIT_BURST=40`
- `REPORT_RELAY_KEEP_QUERY=false`, `REPORT_RELAY_TRUST_PROXY=false`
- `REPORT_RELAY_EXPORT_TIMEOUT=10s`, `REPORT_RELAY_SHUTDOWN_TIMEOUT=15s`
- `REPORT_RELAY_ENABLE_REPORTING_API=true`, `REPORT_RELAY_ENABLE_CSP=true`, `REPORT_RELAY_ENABLE_TLSRPT=true`
- The exporter reads the standard `OTEL_EXPORTER_OTLP_*` variables itself

## Contracts with the fleet

- Every log record carries `event.name` and `event.domain` as ATTRIBUTES, plus
  `service.name` and `service.namespace` on the resource. `o9s/alloy` promotes
  exactly those four to Loki labels (`otelcol.processor.attributes` in its
  `config.alloy.j2`), and it promotes them by reading attributes — the first-class
  OTLP `EventName` field of the same name is invisible to it. Setting only the
  field lands a line whose report type no query can select.
- The intake answers `204` after validation and enqueue, never after the export.
- `REPORT_RELAY_*` config, plus the standard `OTEL_EXPORTER_OTLP_*` the SDK reads.

## Test

- The pipeline compose runs the REAL `o9s/alloy` and `o9s/loki` beside the relay
  (images declared in this projectfile, not in a reusable fragment), and
  `2100-check-report-relay-intake.sh` posts a report with a unique marker, polls
  Loki, then re-queries it BY LABEL. A stub collector would not catch a label the
  fleet’s own pipeline refuses to promote.
- The Loki query window is epoch NANOSECONDS; a relative `0s` is rejected.
