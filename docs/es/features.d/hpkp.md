<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

<!-- textlint-disable terminology,common-misspellings -->

# Informes de fallos de validación de pines HPKP

- Acepta informes de fallo de pines de clave pública del RFC 7469, que no tienen tipo de medio propio y llegan como `application/json` simple.
- Solo se admite un cuerpo con la forma completa de un fallo de pines, así el punto de conexión JSON no es una puerta abierta a cargas arbitrarias.

<!-- textlint-enable -->
