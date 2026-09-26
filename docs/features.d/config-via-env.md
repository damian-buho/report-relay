<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

# Dropped into an OpenTelemetry setup unchanged

- Configuration is environment-first under `REPORT_RELAY_*`, and the exporter reads the standard `OTEL_EXPORTER_OTLP_*` variables, so no custom client block is needed.
- With no `OTEL_EXPORTER_OTLP_*` endpoint set, every record goes to standard output as JSON, so the relay runs with no collector at all.
- The service reports on itself over the same channel: reports received, accepted and dropped, each drop labelled with the reason that caused it.
- Structured JSON logs carry the variable behind every decision, so an operator reads why a report was dropped from the log line itself.
