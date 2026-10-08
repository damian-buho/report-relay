<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

# Browser crash reports

- Accepts `crash` reports from the Reporting API, one record per crash.
- The body arrives intact, so the crash reason a browser chooses to disclose is queryable as it was sent.
