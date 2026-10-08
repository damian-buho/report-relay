<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

<!-- textlint-disable terminology,common-misspellings -->

# Informes heredados de CSP report-uri

- Acepta los cuerpos `application/csp-report` que envían los navegadores anteriores a la Reporting API.
- El cuerpo heredado se normaliza a la forma de Reporting API y al mismo tipo `csp-violation`, así una sola consulta cubre ambos mecanismos.
- La URL de la página se toma del propio informe, así los registros siguen siendo seleccionables por sitio.

<!-- textlint-enable -->
