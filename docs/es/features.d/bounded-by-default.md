<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

<!-- textlint-disable terminology,common-misspellings -->

# Un colector lento nunca ralentiza a un navegador

- La entrada responde en cuanto un informe se valida y se encola, nunca después de la exportación, así que un colector detenido cuesta informes y no cargas de página.
- La cola de exportación tiene tamaño acotado: si se llena, descarta y cuenta en lugar de crecer sin límite.
- Las exportaciones reintentan con retroceso exponencial y dispersión bajo un plazo total, así que un colector muerto retrasa el apagado una cantidad acotada y nada más.
- Un apagado ordenado drena la cola dentro de un plazo configurado; `/healthz` y `/readyz` viven en un puerto de administración aparte del público.

<!-- textlint-enable -->
