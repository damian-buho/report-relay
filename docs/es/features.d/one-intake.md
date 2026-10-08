<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

<!-- textlint-disable terminology,common-misspellings -->

# Una entrada para todos los informes que puede enviar un sitio

- Todos los formatos de esta lista llegan a un único punto de conexión y se enrutan por su tipo de contenido, así no hay que desplegar un recolector por cada tipo de informe.
- Un POST se convierte en un registro de log por informe, listo para la pila de Loki, Tempo y Grafana que ya usas: una consulta nunca ve un lote entero como una sola línea.
- Cada registro se puede seleccionar por su tipo de informe y por el sitio del que vino, así que un panel filtra por `event_name` y `report.url_host` sin tocar una línea.
- Cada formato tiene su propio interruptor, así que una entrada que no usas se puede rechazar mientras el resto sigue funcionando.

<!-- textlint-enable -->
