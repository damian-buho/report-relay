<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

# HPKP pin validation failure reports

- Accepts RFC 7469 public key pin failure reports, which have no media type of their own and arrive as plain `application/json`.
- Only a body carrying the full pin failure shape is admitted, so the JSON endpoint is not an open door for arbitrary payloads.
