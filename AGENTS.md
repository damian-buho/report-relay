<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

# damian-buho/report-relay

Docker image built on [b19/go](../../b19/go/AGENTS.md) and [b19/Ubuntu](../../b19/ubuntu/AGENTS.md)

OpenTelemetry relay for the security reports browsers and mail servers send.

## Key facts

- Builder: `b19/go`; the whole Go application lives in the project directory
- Final Base: `b19/ubuntu:resolute`
- Arch: amd64, arm64
- Binary: `dist/report-relay-linux-{amd64,arm64,riscv64}`, built by `.scripts/build-binaries.sh`, released with `.scripts/gh-release.sh` and torrented as one bundle
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
- `REPORT_RELAY_TRUSTED_PROXIES=""` — comma CIDRs or bare IPs honored as proxy peers; with the flag on but this empty, forwarded headers stay ignored, and a bad entry refuses to start
- `REPORT_RELAY_EXPORT_TIMEOUT=10s`, `REPORT_RELAY_SHUTDOWN_TIMEOUT=15s`
- `REPORT_RELAY_QUEUE_SIZE=2048`, `REPORT_RELAY_BATCH_TIMEOUT=5s`
- `REPORT_RELAY_EXPORT_INITIAL_BACKOFF=500ms`, `REPORT_RELAY_EXPORT_MAX_BACKOFF=30s`, `REPORT_RELAY_EXPORT_MAX_ELAPSED=2m`
- `REPORT_RELAY_ENABLE_REPORTING_API=true`, `REPORT_RELAY_ENABLE_CSP=true`, `REPORT_RELAY_ENABLE_TLSRPT=true`
- `REPORT_RELAY_ENABLE_EXPECT_CT=true`, `REPORT_RELAY_ENABLE_HPKP=true`, `REPORT_RELAY_ENABLE_IODEF=true`
- The exporter reads the standard `OTEL_EXPORTER_OTLP_*` variables itself
- With no OTLP endpoint named, records go to stdout as one JSON line each and
  `/readyz` stays green; `telemetry.OTLPConfigured` owns that switch

## Contracts with the fleet

- Every log record carries `event.name` and `event.domain` as ATTRIBUTES, plus
  `service.name` and `service.namespace` on the resource. `o9s/alloy` promotes
  exactly those four to Loki labels (`otelcol.processor.attributes` in its
  `config.alloy.j2`), and it promotes them by reading attributes — the first-class
  OTLP `EventName` field of the same name is invisible to it. Setting only the
  field lands a line whose report type no query can select.
- The intake answers `204` after validation and enqueue, never after the export.
- `REPORT_RELAY_*` config, plus the standard `OTEL_EXPORTER_OTLP_*` the SDK reads.
- Label safety: a report `type` becomes the `event.name` label and the
  `report_type` metric label, so `intake.SanitizeType` folds every type to
  `^[a-z0-9-]{1,64}$` and buckets the rest as `unknown` (raw kept in the body).
- Backpressure: the emitter gates admissions on the queue size and counts
  `queue-full` drops itself; three consecutive export failures trip `/readyz`.
- Startup validation refuses `burst < 1`, `rps <= 0` and ports outside 1025–65535.
- TLS-RPT yields one record per failure detail (individual) or per result type
  (aggregate); that mapping is the decided answer to the spec’s open question.
- IODEF (RFC 7970, the CAA `iodef` property of RFC 8659 §4.4) arrives as XML on
  `application/iodef+xml`, `application/xml` or `text/xml` (the RFC 6546 wire
  type); one `Incident` becomes one record of type `iodef` in domain `cert`,
  keyed by `IncidentID`, with DTD declarations refused and the JSON depth and
  array caps reused as the XML depth and incident caps.
- Pointing a CAA `iodef` property at the relay is documented in
  `docs/caa-iodef.md` (translations beside it in `docs/es` and `docs/uk`).

## Test

- The pipeline compose runs the REAL `o9s/alloy` and `o9s/loki` beside the relay
  (images declared in this projectfile, not in a reusable fragment), and
  `2100-check-report-relay-intake.sh` posts a report with a unique marker, polls
  Loki, then re-queries it BY LABEL. A stub collector would not catch a label the
  fleet’s own pipeline refuses to promote.
- The Loki query window is epoch NANOSECONDS; a relative `0s` is rejected.
- Perf baseline: `.scripts/bench-60s.sh` (real binary, loopback, stdout
  exporter, limits raised) does ~18k rps for 60s at 100% 204; Go-level
  `BenchmarkIntakeSerial/Parallel` in `internal/server/bench_test.go` does
  ~46k/~93k rps. Default rate limits (20 rps, burst 40) cap one client far
  below either number.
