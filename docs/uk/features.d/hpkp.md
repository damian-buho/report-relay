<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>

SPDX-License-Identifier: MIT
-->

<!-- textlint-disable terminology,common-misspellings -->

# Звіти про збої перевірки пінів HPKP

- Приймає звіти про збій пінів відкритого ключа за RFC 7469, які не мають власного типу медіа й надходять як звичайний `application/json`.
- Допускається лише тіло з повною формою збою пінів, тож JSON-точка приймання не стає відкритими дверима для довільних даних.

<!-- textlint-enable -->
