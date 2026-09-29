<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
SPDX-License-Identifier: MIT
-->

<!-- textlint-disable terminology,common-misspellings -->

[English](../caa-iodef.md) · [Español](../es/caa-iodef.md)

# Приймання звітів CAA IODEF на реле

Властивість CAA `iodef` (RFC 8659, розділ 4.4) вказує центру сертифікації, куди надсилати звіт про інцидент IODEF (RFC 7970), коли запит або випуск сертифіката порушує вашу політику. Спрямуйте цю властивість на реле, і такі звіти потраплятимуть до вашого стеку Loki як записи домену `cert`, поруч з рештою телеметрії безпеки.

## Опублікуйте запис

Запис CAA несе прапорці, тег і значення. Тег `iodef` приймає URL, причому визначено лише схеми `mailto:`, `http:` і `https:`. Реле обслуговує шлях HTTP(S); значення `mailto:` й надалі надходить поштою.

```dns
example.com.  300  IN  CAA  0  issue "ca1.example.net"
example.com.  300  IN  CAA  0  iodef "https://relay.example.com/"
```

Три деталі, які варто знати:

- `0` — це байт прапорців; `iodef` не потребує критичного прапорця.
- Значення — це проста URL без обовʼязкового шляху: кожен POST на точку приймання маршрутизується за `Content-Type`, тож достатньо самого origin. Префікс шляху теж працює й ігнорується.
- CAA піднімається деревом DNS, тож публікація на apex покриває кожне імʼя нижче, якщо імʼя не має власного набору CAA.

## Чого очікує реле

- `Content-Type` зі значень `application/iodef+xml`, `application/xml` або `text/xml` — це транспортний тип RFC 6546, який надсилають центри сертифікації.
- Щонайменше один елемент `Incident`, кожен зі своїм `IncidentID`; один інцидент стає одним записом логу.
- Тіла обмежено змінною `REPORT_RELAY_MAX_BODY_BYTES` (64 KiB за замовчуванням), чого досить для будь-якого реалістичного звіту.
- Відправники обмежені за частотою на IP клієнта (`REPORT_RELAY_RATE_LIMIT_RPS`, 20 за замовчуванням). За зворотним проксі встановіть `REPORT_RELAY_TRUST_PROXY=true` зі змінною `REPORT_RELAY_TRUSTED_PROXIES`, щоб обліковувалась адреса центру, а не проксі.
- Встановіть `REPORT_RELAY_ENABLE_IODEF=false`, щоб відхиляти приймання з кодом `503`, поки решта працює далі.

## Перевірте, що все працює

Переконайтеся, що запис видно, і надішліть мінімальний документ IODEF:

```sh
dig +short example.com CAA
curl -sS -o /dev/null -w "%{http_code}\n" \
  -H 'Content-Type: application/iodef+xml' \
  --data-binary @- https://relay.example.com/ <<'XML'
<?xml version="1.0" encoding="UTF-8"?>
<IODEF-Document version="2.00" xmlns="urn:ietf:params:xml:ns:iodef-2.0">
  <Incident purpose="reporting">
    <IncidentID name="ca1.example.net">probe-001</IncidentID>
    <Node><NodeName>example.com</NodeName></Node>
  </Incident>
</IODEF-Document>
XML
```

Відповідь `204` означає, що звіт прийнято й поставлено в чергу. Запитайте Loki за `{event_name="iodef"}`, щоб побачити його.

<!-- textlint-enable -->
