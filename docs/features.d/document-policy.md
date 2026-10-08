<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

# Document Policy violation reports

- Accepts `document-policy-violation` reports, one record per violation.
- A report without its policy identifier and disposition is refused and counted, so a record always says which policy fired and whether it was enforced.
