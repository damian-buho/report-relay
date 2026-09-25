<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
SPDX-License-Identifier: MIT
-->

[Español](docs/es/FEATURES.md) · [Українська](docs/uk/FEATURES.md)

# Features

## Project Features

### A slow collector never slows a browser

- The intake answers as soon as a report is validated and queued, never after the export, so a stalled collector costs reports rather than page loads.
- The export queue is bounded: a full queue drops and counts instead of growing without limit.
- Exports retry with exponential backoff and jitter under a total deadline, so a dead collector delays shutdown by a bounded amount and no more.
- A graceful shutdown drains the queue within a configured deadline; `/healthz` and `/readyz` sit on a separate admin port from the public intake.

### Dropped into an OpenTelemetry setup unchanged

- Configuration is environment-first under `REPORT_RELAY_*`, and the exporter reads the standard `OTEL_EXPORTER_OTLP_*` variables, so no custom client block is needed.
- The service reports on itself over the same channel: reports received, accepted and dropped, each drop labelled with the reason that caused it.
- Structured JSON logs carry the variable behind every decision, so an operator reads why a report was dropped from the log line itself.

### A public intake, guarded by default

- Every guard is on by default: a body cap, a per-client rate limit, a content-type allow-list, JSON depth and array limits, and per-type schema validation.
- Query strings and fragments are stripped from the URLs a report carries, because a report URL routinely holds a token; a switch keeps them when you need the whole value.
- Nothing unvalidated is ever exported: a report the service cannot read is counted with its reason and dropped.
- The CORS preflight is answered for the reporting methods, with an optional origin allow-list; unset means any origin, because reporting is cross-origin by nature.

### One intake for every report a site can send

- Accepts Reporting API batches, legacy CSP `report-uri` bodies, network error logs and SMTP TLS reports on a single endpoint, so no per-report-type collector has to be deployed.
- One POST becomes one log record per report, ready for the Loki, Tempo and Grafana stack you already run — a query never sees a whole batch as a single line.
- The legacy CSP body is normalised to the Reporting API shape, so a query never has to care which mechanism the browser used.
- A report type this build has never heard of still arrives, with its body intact — the Reporting API is an open list, and a new type is not a reason to lose the report.
