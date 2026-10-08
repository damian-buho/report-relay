<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

# SMTP TLS reports

- Accepts RFC 8460 TLS-RPT reports from mail servers, plain JSON or gzip, and files them in the `mail` domain beside the browser reports.
- An individual report becomes one record per failure; an aggregate report becomes one record per result type, with its failing session counts.
- A gzip body is capped after decompression, so a small upload cannot expand past the body limit.
