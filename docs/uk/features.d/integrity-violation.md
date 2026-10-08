<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

<!-- textlint-disable terminology,common-misspellings -->

# Звіти про порушення Subresource Integrity

- Приймає звіти `integrity-violation`, які показують, коли скрипт чи таблиця стилів не пройшли перевірку цілісності й були заблоковані.
- Звіт без документа чи заблокованого ресурсу відхиляється й рахується.
- Рядки запиту та фрагменти вилучаються з URL у звіті, бо URL звіту зазвичай містить токен.

<!-- textlint-enable -->
