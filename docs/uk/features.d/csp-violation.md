<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

<!-- textlint-disable terminology,common-misspellings -->

# Звіти про порушення Content Security Policy

- Приймає звіти `csp-violation`, які сучасні браузери надсилають через Reporting API, один запис на порушення.
- Звіт, у якому немає документа, директиви чи заблокованого ресурсу, відхиляється й рахується, тож неправильний відправник ніколи не дістається збирача.
- Рядки запиту та фрагменти вилучаються з URL у звіті, бо URL звіту зазвичай містить токен.

<!-- textlint-enable -->
