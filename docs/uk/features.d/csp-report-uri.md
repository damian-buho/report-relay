<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

<!-- textlint-disable terminology,common-misspellings -->

# Старі звіти CSP report-uri

- Приймає тіла `application/csp-report`, які надсилають браузери, що з’явилися до Reporting API.
- Старе тіло нормалізується до форми Reporting API і до того самого типу `csp-violation`, тож один запит охоплює обидва механізми.
- URL сторінки береться з самого звіту, тож записи й далі можна вибирати за сайтом.

<!-- textlint-enable -->
