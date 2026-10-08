<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

# CAA IODEF incident reports

- Accepts RFC 7970 IODEF incident reports, the kind certification authorities send to a CAA `iodef` address, as XML on `application/iodef+xml`, `application/xml` or `text/xml`.
- One incident becomes one record in the `cert` domain, keyed by its incident identifier.
- XML bodies with DTD declarations are refused, and the depth and size limits apply to XML as they do to JSON.
