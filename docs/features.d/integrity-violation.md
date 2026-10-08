<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

# Subresource Integrity violation reports

- Accepts `integrity-violation` reports, which tell you when a script or style sheet failed its integrity check and was blocked.
- A report that names no document or blocked resource is refused and counted.
- Query strings and fragments are stripped from the URLs it carries, because a report URL routinely holds a token.
