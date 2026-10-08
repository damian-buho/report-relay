<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

<!-- textlint-disable terminology,common-misspellings -->

# Informes de infracciones de Content Security Policy

- Acepta los informes `csp-violation` que los navegadores actuales envían mediante la Reporting API, un registro por infracción.
- Un informe que no nombra documento, directiva ni recurso bloqueado se rechaza y se cuenta, así un emisor mal formado nunca llega al recolector.
- Las cadenas de consulta y los fragmentos se eliminan de las URL que contiene, porque la URL de un informe suele llevar un token.

<!-- textlint-enable -->
