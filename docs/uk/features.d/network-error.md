<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

<!-- textlint-disable terminology,common-misspellings -->

# Звіти Network Error Logging

- Приймає звіти `network-error`, які показують, коли справжні відвідувачі не змогли дістатися вашого сайту: збої DNS, TCP, TLS і HTTP з їхнього боку.
- Назви `nel` та `networkerror` зводяться до одного типу, тож запиту не потрібно знати, яку з них надіслано.
- Звіт без фази та типу помилки відхиляється й рахується.

<!-- textlint-enable -->
