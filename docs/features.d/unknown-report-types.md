<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

# A report type nobody has defined yet is still kept

- The Reporting API is an open list, so a type this build has never heard of is accepted with its body intact instead of being thrown away.
- A type that is not safe to use as a label is bucketed as `unknown`, and the sender’s own spelling stays in the record body.
