<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

# One intake for every report a site can send

- Accepts Reporting API batches, legacy CSP `report-uri` bodies, network error logs and SMTP TLS reports on a single endpoint, so no per-report-type collector has to be deployed.
- One POST becomes one log record per report, ready for the Loki, Tempo and Grafana stack you already run — a query never sees a whole batch as a single line.
- The legacy CSP body is normalised to the Reporting API shape, so a query never has to care which mechanism the browser used.
- A report type this build has never heard of still arrives, with its body intact — the Reporting API is an open list, and a new type is not a reason to lose the report.
