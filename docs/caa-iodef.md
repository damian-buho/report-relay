<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
SPDX-License-Identifier: MIT
-->

[Español](es/caa-iodef.md) · [Українська](uk/caa-iodef.md)

# Point CAA IODEF reports at the relay

A CAA `iodef` property (RFC 8659, section 4.4) tells a certification authority where to send an IODEF incident report (RFC 7970) when a certificate request or issuance violates your policy. Point that property at the relay and those reports land in your Loki stack as `cert`-domain records, next to the rest of your security telemetry.

## Publish the record

A CAA record carries flags, a tag and a value. The `iodef` tag takes a URL, and only the `mailto:`, `http:` and `https:` schemes are defined. The relay serves the HTTP(S) path; a `mailto:` value keeps going to email.

```dns
example.com.  300  IN  CAA  0  issue "ca1.example.net"
example.com.  300  IN  CAA  0  iodef "https://relay.example.com/"
```

Three details worth knowing:

- `0` is the flags byte; `iodef` needs no critical flag.
- The value is a plain URL with no endpoint path required: every POST to the intake is routed by `Content-Type`, so the bare origin is enough. A path prefix works too and is ignored.
- CAA climbs the DNS tree, so publishing at the apex covers every hostname below it unless a name carries its own CAA set.

## What the relay expects

- A `Content-Type` of `application/iodef+xml`, `application/xml` or `text/xml`, which is the RFC 6546 wire type certification authorities send.
- At least one `Incident` element, each carrying an `IncidentID`; one incident becomes one log record.
- Bodies are capped by `REPORT_RELAY_MAX_BODY_BYTES` (64 KiB by default), which fits any realistic incident report.
- Senders are rate limited per client IP (`REPORT_RELAY_RATE_LIMIT_RPS`, 20 by default). Behind a reverse proxy, set `REPORT_RELAY_TRUST_PROXY=true` with `REPORT_RELAY_TRUSTED_PROXIES` so the authority address is accounted instead of the proxy address.
- Set `REPORT_RELAY_ENABLE_IODEF=false` to refuse the intake with `503` while the rest keeps running.

## Verify it works

Check the record is visible, then POST a minimal IODEF document:

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

A `204` answer means the report was accepted and queued. Query Loki for `{event_name="iodef"}` to see it.
