<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

<!-- textlint-disable terminology,common-misspellings -->

# Se integra sin cambios en una instalación de OpenTelemetry

- La configuración es primero por entorno bajo `REPORT_RELAY_*`, y el exportador lee las variables estándar `OTEL_EXPORTER_OTLP_*`, así que no hace falta ningún bloque de cliente a medida.
- Sin ningún punto de envío `OTEL_EXPORTER_OTLP_*` configurado, cada registro va a la salida estándar como JSON, así que el relé funciona sin ningún recolector.
- El servicio informa sobre sí mismo por el mismo canal: informes recibidos, aceptados y descartados, cada descarte etiquetado con el motivo que lo causó.
- Los registros estructurados en JSON llevan la variable detrás de cada decisión, así que un operador lee por qué se descartó un informe en la propia línea de log.

<!-- textlint-enable -->
