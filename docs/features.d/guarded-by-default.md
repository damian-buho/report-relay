<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

# A public intake, guarded by default

- Every guard is on by default: a body cap, a per-client rate limit, a content-type allow-list, JSON depth and array limits, and per-type schema validation.
- Query strings and fragments are stripped from the URLs a report carries, because a report URL routinely holds a token; a switch keeps them when you need the whole value.
- Nothing unvalidated is ever exported: a report the service cannot read is counted with its reason and dropped.
- The CORS preflight is answered for the reporting methods, with an optional origin allow-list; unset means any origin, because reporting is cross-origin by nature.
- Behind a reverse proxy, the forwarded chain is honored only from trusted proxy networks (`REPORT_RELAY_TRUST_PROXY` plus `REPORT_RELAY_TRUSTED_PROXIES`), so a direct caller cannot pick its own rate-limit bucket by forging a header.
