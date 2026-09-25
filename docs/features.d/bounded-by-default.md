<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

# A slow collector never slows a browser

- The intake answers as soon as a report is validated and queued, never after the export, so a stalled collector costs reports rather than page loads.
- The export queue is bounded: a full queue drops and counts instead of growing without limit.
- Exports retry with exponential backoff and jitter under a total deadline, so a dead collector delays shutdown by a bounded amount and no more.
- A graceful shutdown drains the queue within a configured deadline; `/healthz` and `/readyz` sit on a separate admin port from the public intake.
