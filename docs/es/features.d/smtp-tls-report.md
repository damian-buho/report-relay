<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

<!-- textlint-disable terminology,common-misspellings -->

# Informes SMTP TLS

- Acepta informes TLS-RPT del RFC 8460 de servidores de correo, en JSON simple o gzip, y los archiva en el dominio `mail` junto a los informes de navegador.
- Un informe individual se convierte en un registro por fallo; uno agregado, en un registro por tipo de resultado, con su recuento de sesiones fallidas.
- Un cuerpo gzip se limita después de descomprimirlo, así una subida pequeña no puede expandirse más allá del límite de cuerpo.

<!-- textlint-enable -->
