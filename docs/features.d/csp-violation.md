<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

# Content Security Policy violation reports

- Accepts the `csp-violation` reports current browsers send through the Reporting API, one record per violation.
- A report that names no document, directive or blocked resource is refused and counted, so a malformed sender never reaches the collector.
- Query strings and fragments are stripped from the URLs it carries, because a report URL routinely holds a token.
