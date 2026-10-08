<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

# Permissions Policy violation reports

- Accepts `permissions-policy-violation` reports, the older `feature-policy-violation` name, and `potential-permissions-policy-violation` reports.
- A report without its policy identifier and disposition is refused and counted, so a record always says which policy fired and whether it was enforced.
