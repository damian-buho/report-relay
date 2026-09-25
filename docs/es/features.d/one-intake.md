<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

<!-- textlint-disable terminology,common-misspellings -->

# Una entrada para todos los informes que puede enviar un sitio

- Acepta lotes de Reporting API, cuerpos heredados de `report-uri` de CSP, registros de errores de red e informes SMTP TLS en un mismo punto de conexión, así no hay que desplegar un recolector por cada tipo de informe.
- Un POST se convierte en un registro de log por informe, listo para la pila de Loki, Tempo y Grafana que ya usas: una consulta nunca ve un lote entero como una sola línea.
- El cuerpo heredado de CSP se normaliza a la forma de Reporting API, así una consulta no tiene que importar qué mecanismo usó el navegador.
- Un tipo de informe que esta versión nunca ha visto llega igualmente, con su cuerpo intacto: la Reporting API es una lista abierta, y un tipo nuevo no es razón para perder el informe.

<!-- textlint-enable -->
