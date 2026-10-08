<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

<!-- textlint-disable terminology,common-misspellings -->

# Звіти про інциденти CAA IODEF

- Приймає звіти про інциденти IODEF за RFC 7970, які центри сертифікації надсилають на адресу `iodef` у CAA, як XML у `application/iodef+xml`, `application/xml` або `text/xml`.
- Один інцидент стає одним записом у домені `cert`, ключем якого є ідентифікатор інциденту.
- Тіла XML з оголошеннями DTD відхиляються, а ліміти глибини й розміру діють для XML так само, як для JSON.

<!-- textlint-enable -->
