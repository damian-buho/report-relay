<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

<!-- textlint-disable terminology,common-misspellings -->

# Informes de infracciones de Subresource Integrity

- Acepta informes `integrity-violation`, que indican cuándo un script u hoja de estilo falló su comprobación de integridad y se bloqueó.
- Un informe que no nombra documento ni recurso bloqueado se rechaza y se cuenta.
- Las cadenas de consulta y los fragmentos se eliminan de las URL que contiene, porque la URL de un informe suele llevar un token.

<!-- textlint-enable -->
