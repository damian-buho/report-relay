<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

<!-- textlint-disable terminology,common-misspellings -->

# Звіти SMTP TLS

- Приймає звіти TLS-RPT за RFC 8460 від поштових серверів, звичайний JSON або gzip, і зберігає їх у домені `mail` поруч зі звітами браузерів.
- Окремий звіт стає одним записом на збій; зведений звіт — одним записом на тип результату, з кількістю невдалих сеансів.
- Тіло gzip обмежується після розпакування, тож невелике надсилання не може розрастися понад ліміт тіла.

<!-- textlint-enable -->
