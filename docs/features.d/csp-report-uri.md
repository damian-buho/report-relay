<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

# Legacy CSP report-uri reports

- Accepts the `application/csp-report` bodies sent by browsers that predate the Reporting API.
- The legacy body is normalised to the Reporting API shape and the same `csp-violation` type, so one query covers both mechanisms.
- The page URL is taken from the report itself, so records stay selectable by site.
