<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
SPDX-License-Identifier: MIT
-->

<!-- textlint-disable terminology,common-misspellings -->

[English](../../FEATURES.md) · [Українська](../uk/FEATURES.md)

# Características

## Características del proyecto

### Un colector lento nunca ralentiza a un navegador

- La entrada responde en cuanto un informe se valida y se encola, nunca después de la exportación, así que un colector detenido cuesta informes y no cargas de página.
- La cola de exportación tiene tamaño acotado: si se llena, descarta y cuenta en lugar de crecer sin límite.
- Las exportaciones reintentan con retroceso exponencial y dispersión bajo un plazo total, así que un colector muerto retrasa el apagado una cantidad acotada y nada más.
- Un apagado ordenado drena la cola dentro de un plazo configurado; `/healthz` y `/readyz` viven en un puerto de administración aparte del público.

### Se integra sin cambios en una instalación de OpenTelemetry

- La configuración es primero por entorno bajo `REPORT_RELAY_*`, y el exportador lee las variables estándar `OTEL_EXPORTER_OTLP_*`, así que no hace falta ningún bloque de cliente a medida.
- El servicio informa sobre sí mismo por el mismo canal: informes recibidos, aceptados y descartados, cada descarte etiquetado con el motivo que lo causó.
- Los registros estructurados en JSON llevan la variable detrás de cada decisión, así que un operador lee por qué se descartó un informe en la propia línea de log.

### Una entrada pública, protegida por defecto

- Todas las protecciones están activas por defecto: límite del cuerpo, límite de peticiones por cliente, lista blanca de tipos de contenido, límites de profundidad y longitud de arreglos, y validación de esquema por tipo.
- Se eliminan las cadenas de consulta y los fragmentos de las URL que trae un informe, porque una URL de informe suele llevar un token; un conmutador los conserva cuando necesitas el valor completo.
- Nunca se exporta nada sin validar: un informe que el servicio no puede leer se cuenta con su motivo y se descarta.
- Se responde al preflight CORS de los métodos de envío, con lista blanca de orígenes opcional; sin configurar vale cualquier origen, porque el envío es entre orígenes por naturaleza.

### Una entrada para todos los informes que puede enviar un sitio

- Acepta lotes de Reporting API, cuerpos heredados de `report-uri` de CSP, registros de errores de red e informes SMTP TLS en un mismo punto de conexión, así no hay que desplegar un recolector por cada tipo de informe.
- Un POST se convierte en un registro de log por informe, listo para la pila de Loki, Tempo y Grafana que ya usas: una consulta nunca ve un lote entero como una sola línea.
- El cuerpo heredado de CSP se normaliza a la forma de Reporting API, así una consulta no tiene que importar qué mecanismo usó el navegador.
- Un tipo de informe que esta versión nunca ha visto llega igualmente, con su cuerpo intacto: la Reporting API es una lista abierta, y un tipo nuevo no es razón para perder el informe.
<!-- textlint-enable -->
