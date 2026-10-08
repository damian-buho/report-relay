<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

<!-- textlint-disable terminology,common-misspellings -->

# Informes de Network Error Logging

- Acepta informes `network-error`, que indican cuándo visitantes reales no lograron llegar a tu sitio: fallos de DNS, TCP, TLS y HTTP vistos desde su lado.
- Los nombres `nel` y `networkerror` se unifican en un solo tipo, así una consulta no tiene que saber cuál se envió.
- Un informe sin su fase y tipo de error se rechaza y se cuenta.

<!-- textlint-enable -->
