<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

# Network Error Logging reports

- Accepts `network-error` reports, which tell you when real visitors failed to reach your site: DNS, TCP, TLS and HTTP failures seen from their side.
- The `nel` and `networkerror` spellings are folded into one type, so a query never has to know which one was sent.
- A report without its phase and error type is refused and counted.
