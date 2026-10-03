<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
SPDX-License-Identifier: MIT
pf-cli-managed: yes
-->

<!-- textlint-disable terminology,common-misspellings -->

[English](../../README.md) · [Українська](../uk/README.md)

# Report Relay

Report Relay acepta los informes de seguridad que envían los navegadores y los servidores de correo — lotes de Reporting API, cuerpos heredados de report-uri de CSP, registros de errores de red e informes SMTP TLS — y emite un registro de log de OpenTelemetry por informe, de modo que aterrizan en la pila existente de Loki, Tempo y Grafana en lugar de un recolector distinto por tipo de informe.

[![Stand with Ukraine](https://raw.githubusercontent.com/vshymanskyy/StandWithUkraine/main/badges/StandWithUkraine.svg)](https://damian-buho.github.io/support-ukraine/) [![Projectfile inside](https://badges.kiota.ch/static/v1?label=projectfile&message=inside&labelColor=0d0d0d&color=8c6723&style=flat-square)](https://projectfile.org) [![License](https://badges.kiota.ch/static/v1?label=license&message=MIT&color=1e5913&style=flat-square)](LICENSE) [![Cosign](https://badges.kiota.ch/static/v1?label=cosign&message=enabled&color=1e5913&style=flat-square)](https://docs.sigstore.dev/cosign/verifying/verify/) [![PRs welcome](https://badges.kiota.ch/static/v1?label=PRs&message=welcome&color=1e5913&style=flat-square)](CONTRIBUTING.md) [![REUSE compliance](https://api.reuse.software/badge/codeberg.org/damian-buho/report-relay)](https://api.reuse.software/info/codeberg.org/damian-buho/report-relay)

![Project status](https://badges.kiota.ch/static/v1?label=status&message=experimental&color=1d63ed&style=flat-square) [![Last commit on kiota.ch](https://badges.kiota.ch/gitea/last-commit/damian-buho/report-relay?gitea_url=https://kiota.ch&label=last%20commit%20on%20kiota.ch&style=flat-square)](https://kiota.ch/damian-buho/report-relay) [![Last commit on Codeberg](https://badges.kiota.ch/gitea/last-commit/damian-buho/report-relay?gitea_url=https://codeberg.org&label=last%20commit%20on%20Codeberg&style=flat-square)](https://codeberg.org/damian-buho/report-relay) [![Last commit on GitHub](https://badges.kiota.ch/github/last-commit/damian-buho/report-relay?label=last%20commit%20on%20GitHub&style=flat-square)](https://github.com/damian-buho/report-relay)

[![Publish pipeline on GitHub](https://github.com/damian-buho/report-relay/actions/workflows/published.yaml/badge.svg?style=flat-square)](https://github.com/damian-buho/report-relay/actions) [![Vulnerability audit on GitHub](https://github.com/damian-buho/report-relay/actions/workflows/audited.yaml/badge.svg?style=flat-square)](https://github.com/damian-buho/report-relay/actions) [![Dependency freshness on GitHub](https://github.com/damian-buho/report-relay/actions/workflows/check-outdated.yaml/badge.svg?style=flat-square)](https://github.com/damian-buho/report-relay/actions) [![Analysis sweep on GitHub](https://github.com/damian-buho/report-relay/actions/workflows/analyzed.yaml/badge.svg?style=flat-square)](https://github.com/damian-buho/report-relay/actions)

[![Publish pipeline on kiota.ch](https://kiota.ch/damian-buho/report-relay/badges/workflows/published.yaml/badge.svg?style=flat-square)](https://kiota.ch/damian-buho/report-relay/actions) [![Vulnerability audit on kiota.ch](https://kiota.ch/damian-buho/report-relay/badges/workflows/audited.yaml/badge.svg?style=flat-square)](https://kiota.ch/damian-buho/report-relay/actions) [![Dependency freshness on kiota.ch](https://kiota.ch/damian-buho/report-relay/badges/workflows/check-outdated.yaml/badge.svg?style=flat-square)](https://kiota.ch/damian-buho/report-relay/actions) [![Analysis sweep on kiota.ch](https://kiota.ch/damian-buho/report-relay/badges/workflows/analyze.yaml/badge.svg?style=flat-square)](https://kiota.ch/damian-buho/report-relay/actions)

## Características

- Un colector lento nunca ralentiza a un navegador
- Se integra sin cambios en una instalación de OpenTelemetry
- Una entrada pública, protegida por defecto
- Una entrada para todos los informes que puede enviar un sitio

También hereda las características de B19 / Ubuntu; consulta [Características](FEATURES.md) para ver la lista completa.

## Qué entrega este proyecto

- **Ejecutable** `report-relay` — comando `report-relay`
- **Imagen de contenedor** `ghcr.io/damian-buho/report-relay:latest`
- **Imagen de contenedor** `damianbuho/report-relay:latest`
- **Servicio** `relay` — escucha en `8080 (intake)`, `8081 (admin)` — Recepción de informes y endpoints de salud

## Instalación

### Imagen de contenedor

Descarga la imagen de contenedor publicada:

#### Descargar de GHCR — linux/amd64, linux/arm64

```sh
docker pull ghcr.io/damian-buho/report-relay:latest
```

#### Descargar de DockerHub — linux/amd64

```sh
docker pull damianbuho/report-relay:latest
```

Las versiones estables también publican las etiquetas `X.Y.Z`, `X.Y` y `X`: descarga el nivel de precisión que quieras fijar.

Si los registros anteriores no están disponibles, descarga desde el origen:

#### Descargar de Kiota — linux/amd64

```sh
docker pull kiota.ch/damian-buho/report-relay:latest
```

### Binario precompilado

Descarga el binario precompilado para tu plataforma desde la última versión en GitHub:

```sh
curl --fail --location --output report-relay https://github.com/damian-buho/report-relay/releases/latest/download/report-relay-$(uname -s | tr A-Z a-z)-$(uname -m | sed -e s/x86_64/amd64/ -e s/aarch64/arm64/) && chmod +x report-relay
./report-relay --help
```

Publicado para: `linux/amd64`, `linux/arm64`, `linux/riscv64`

## Uso

Ejecuta el servicio en segundo plano, publicando sus puertos:

### Desde GHCR

```sh
docker run --detach --publish 8080:8080/tcp --publish 8081:8081/tcp ghcr.io/damian-buho/report-relay:latest
```

### Desde DockerHub

```sh
docker run --detach --publish 8080:8080/tcp --publish 8081:8081/tcp damianbuho/report-relay:latest
```

Después, comprueba que responde:

```sh
curl http://localhost:8081/healthz
```

## Compilación

Clona el repositorio con sus submódulos:

```sh
git clone --recurse-submodules https://codeberg.org/damian-buho/report-relay report-relay && cd report-relay
```

Construye la imagen de contenedor en local:

```sh
make container-build
```

- [Referencia del Makefile](../how-to/MAKEFILE.md)

Ejecuta `make` sin argumentos para el destino predeterminado; ejecuta `make help` para listar todos los destinos.

Para el bucle de desarrollo local, `make dev-container` levanta el dev-container.

Puntos de entrada de la canalización:

- `make analyzed` — Ejecuta el análisis pesado (pruebas de mutación, benchmarks)
- `make audited` — Vuelve a escanear las dependencias fijadas y los artefactos publicados en busca de vulnerabilidades nuevas
- `make check-outdated` — Informa de cada dependencia fijada que va por detrás de su versión upstream
- `make ready-to-publish` — Ejecuta localmente el pipeline pseudo-CI — compila, prueba y escanea, sin publicar

## Políticas

- [Cómo contribuir](CONTRIBUTING.md)
- [Política de seguridad](SECURITY.md)
- [Cómo obtener ayuda](SUPPORT.md)
- [Código de conducta](CODE_OF_CONDUCT.md)
- [Política sobre IA y LLM](AI_POLICY.md)

## Enlaces

- [Especificación de Projectfile](https://projectfile.org)

## Licencia

Este proyecto se publica bajo la licencia MIT — consulta el archivo [LICENSE](LICENSE) para más detalles.

<!-- textlint-enable -->
