<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
SPDX-License-Identifier: MIT
-->

<!-- textlint-disable terminology,common-misspellings -->

[English](../caa-iodef.md) · [Українська](../uk/caa-iodef.md)

# Recibir informes IODEF de CAA en el relé

Una propiedad `iodef` de CAA (RFC 8659, sección 4.4) indica a la autoridad de certificación dónde enviar un informe de incidente IODEF (RFC 7970) cuando una solicitud o emisión de certificado viola tu política. Apunta esa propiedad al relé y esos informes llegan a tu pila de Loki como registros del dominio `cert`, junto al resto de tu telemetría de seguridad.

## Publica el registro

Un registro CAA lleva flags, una etiqueta y un valor. La etiqueta `iodef` acepta una URL, y solo están definidos los esquemas `mailto:`, `http:` y `https:`. El relé atiende la vía HTTP(S); un valor `mailto:` sigue llegando por correo.

```dns
example.com.  300  IN  CAA  0  issue "ca1.example.net"
example.com.  300  IN  CAA  0  iodef "https://relay.example.com/"
```

Tres detalles que conviene conocer:

- `0` es el byte de flags; `iodef` no necesita flag crítico.
- El valor es una URL simple sin ruta obligatoria: cada POST a la entrada se enruta por `Content-Type`, así que basta el origen. Un prefijo de ruta también funciona y se ignora.
- CAA sube por el árbol DNS, así que publicar en el ápice cubre cada nombre bajo él salvo que un nombre tenga su propio juego CAA.

## Lo que espera el relé

- Un `Content-Type` de `application/iodef+xml`, `application/xml` o `text/xml`, que es el tipo de transporte RFC 6546 que envían las autoridades de certificación.
- Al menos un elemento `Incident`, cada uno con su `IncidentID`; un incidente se convierte en un registro de log.
- Los cuerpos están limitados por `REPORT_RELAY_MAX_BODY_BYTES` (64 KiB por defecto), suficiente para cualquier informe realista.
- Los remitentes tienen límite de tasa por IP de cliente (`REPORT_RELAY_RATE_LIMIT_RPS`, 20 por defecto). Tras un proxy inverso, configura `REPORT_RELAY_TRUST_PROXY=true` con `REPORT_RELAY_TRUSTED_PROXIES` para contabilizar la dirección de la autoridad en vez de la del proxy.
- Configura `REPORT_RELAY_ENABLE_IODEF=false` para rechazar la entrada con `503` mientras el resto sigue funcionando.

## Comprueba que funciona

Revisa que el registro sea visible y envía un documento IODEF mínimo:

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

Una respuesta `204` significa que el informe fue aceptado y encolado. Consulta Loki con `{event_name="iodef"}` para verlo.

<!-- textlint-enable -->
