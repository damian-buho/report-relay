<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

<!-- textlint-disable terminology,common-misspellings -->

# Informes de incidentes IODEF de CAA

- Acepta informes de incidentes IODEF del RFC 7970, los que las autoridades de certificación envían a una dirección `iodef` de CAA, como XML en `application/iodef+xml`, `application/xml` o `text/xml`.
- Un incidente se convierte en un registro del dominio `cert`, identificado por su identificador de incidente.
- Los cuerpos XML con declaraciones DTD se rechazan, y los límites de profundidad y tamaño se aplican al XML igual que al JSON.

<!-- textlint-enable -->
