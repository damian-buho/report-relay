<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

<!-- textlint-disable terminology,common-misspellings -->

# Una entrada pública, protegida por defecto

- Todas las protecciones están activas por defecto: límite del cuerpo, límite de peticiones por cliente, lista blanca de tipos de contenido, límites de profundidad y longitud de arreglos, y validación de esquema por tipo.
- Se eliminan las cadenas de consulta y los fragmentos de las URL que trae un informe, porque una URL de informe suele llevar un token; un conmutador los conserva cuando necesitas el valor completo.
- Nunca se exporta nada sin validar: un informe que el servicio no puede leer se cuenta con su motivo y se descarta.
- Se responde al preflight CORS de los métodos de envío, con lista blanca de orígenes opcional; sin configurar vale cualquier origen, porque el envío es entre orígenes por naturaleza.

<!-- textlint-enable -->
