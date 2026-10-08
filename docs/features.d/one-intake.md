<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

# One endpoint for every report a site can send

- Every format listed here arrives on a single endpoint and is routed by its content type, so no per-report-type collector has to be deployed.
- One POST becomes one log record per report, ready for the Loki, Tempo and Grafana stack you already run — a query never sees a whole batch as a single line.
- Every record is selectable by its report type and by the site it came from, so a dashboard filters on `event_name` and `report.url_host` without touching a line.
- Each format has its own switch, so an intake you do not use can be refused while the rest keeps running.
