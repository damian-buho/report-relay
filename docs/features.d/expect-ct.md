<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

# Expect-CT violation reports

- Accepts RFC 9163 Expect-CT reports, which tell you when a certificate failed a browser’s Certificate Transparency check.
- The failing hostname is the record’s site, so a report that names none is refused and counted.
