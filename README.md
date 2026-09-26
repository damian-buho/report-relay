<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
SPDX-License-Identifier: MIT
pf-cli-managed: yes
-->

[Español](docs/es/README.md) · [Українська](docs/uk/README.md)

# Report Relay

OpenTelemetry relay for browser and mail security reports

[![Stand with Ukraine](https://raw.githubusercontent.com/vshymanskyy/StandWithUkraine/main/badges/StandWithUkraine.svg)](https://damian-buho.github.io/support-ukraine/) [![Projectfile inside](https://badges.kiota.ch/static/v1?label=projectfile&message=inside&labelColor=0d0d0d&color=8c6723&style=flat-square)](https://projectfile.org) [![License](https://badges.kiota.ch/static/v1?label=license&message=MIT&color=1e5913&style=flat-square)](LICENSE) [![Commit style](https://badges.kiota.ch/static/v1?label=commits&message=conventional%20v1.0.0&color=1877aa&style=flat-square)](https://www.conventionalcommits.org/en/v1.0.0/) ![Workflow](https://badges.kiota.ch/static/v1?label=workflow&message=git-flow&color=1877aa&style=flat-square) [![Versioning](https://badges.kiota.ch/static/v1?label=versioning&message=semantic%20v2.0.0&color=1877aa&style=flat-square)](https://semver.org/) [![PRs welcome](https://badges.kiota.ch/static/v1?label=PRs&message=welcome&color=1e5913&style=flat-square)](CONTRIBUTING.md) [![Citation](https://badges.kiota.ch/static/v1?label=citation&message=cff&color=1877aa&style=flat-square)](CITATION.cff) [![REUSE compliance](https://api.reuse.software/badge/codeberg.org/damian-buho/report-relay)](https://api.reuse.software/info/codeberg.org/damian-buho/report-relay)

![Project status](https://badges.kiota.ch/static/v1?label=status&message=experimental&color=1d63ed&style=flat-square) [![Last commit on kiota.ch](https://badges.kiota.ch/gitea/last-commit/damian-buho/report-relay?gitea_url=https://kiota.ch&label=last%20commit%20on%20kiota.ch&style=flat-square)](https://kiota.ch/damian-buho/report-relay) [![Last commit on Codeberg](https://badges.kiota.ch/gitea/last-commit/damian-buho/report-relay?gitea_url=https://codeberg.org&label=last%20commit%20on%20Codeberg&style=flat-square)](https://codeberg.org/damian-buho/report-relay) [![Last commit on GitHub](https://badges.kiota.ch/github/last-commit/damian-buho/report-relay?label=last%20commit%20on%20GitHub&style=flat-square)](https://github.com/damian-buho/report-relay)

[![Publish pipeline on GitHub](https://github.com/damian-buho/report-relay/actions/workflows/published.yaml/badge.svg?style=flat-square)](https://github.com/damian-buho/report-relay/actions) [![Vulnerability audit on GitHub](https://github.com/damian-buho/report-relay/actions/workflows/audited.yaml/badge.svg?style=flat-square)](https://github.com/damian-buho/report-relay/actions) [![Dependency freshness on GitHub](https://github.com/damian-buho/report-relay/actions/workflows/check-outdated.yaml/badge.svg?style=flat-square)](https://github.com/damian-buho/report-relay/actions) [![Analysis sweep on GitHub](https://github.com/damian-buho/report-relay/actions/workflows/analyze.yaml/badge.svg?style=flat-square)](https://github.com/damian-buho/report-relay/actions)

[![Publish pipeline on kiota.ch](https://kiota.ch/damian-buho/report-relay/badges/workflows/published.yaml/badge.svg?style=flat-square)](https://kiota.ch/damian-buho/report-relay/actions) [![Vulnerability audit on kiota.ch](https://kiota.ch/damian-buho/report-relay/badges/workflows/audited.yaml/badge.svg?style=flat-square)](https://kiota.ch/damian-buho/report-relay/actions) [![Dependency freshness on kiota.ch](https://kiota.ch/damian-buho/report-relay/badges/workflows/check-outdated.yaml/badge.svg?style=flat-square)](https://kiota.ch/damian-buho/report-relay/actions) [![Analysis sweep on kiota.ch](https://kiota.ch/damian-buho/report-relay/badges/workflows/analyze.yaml/badge.svg?style=flat-square)](https://kiota.ch/damian-buho/report-relay/actions)

## Features

- A slow collector never slows a browser
- Dropped into an OpenTelemetry setup unchanged
- A public intake, guarded by default
- One intake for every report a site can send

### Inherited from B19 / Ubuntu

- Persistent APT cache across builds
- Service process management with log routing (b19-exec)
- Cached artifact downloads with integrity verification
- Timed command execution with failure reporting (b19-run)
- Run-once initialization (bootstrap.d)
- Modular build hooks (build.d)
- Automatic CPU count detection
- Declarative dependency management (b19-deps)
- Pluggable startup system (entrypoint.d)
- Feature toggles for all subsystems
- Built-in health monitoring (healthcheck.d)
- Multilingual shell output (b19-i18n)
- Image lineage tracking
- Structured, level-filtered logging (b19-log)
- Non-root container by default
- Air-gapped / offline build and runtime support
- Runtime overlay injection
- Reproducible base image (pinned by digest)
- Port validation
- Unified lifecycle runner family
- Docker secrets auto-loading
- Interactive shell hooks
- Graceful signal handling
- Jinja2 configuration templates (minijinja-cli)
- Built-in test framework (test.d)
- Pre-installed utility tools
- XDG Base Directory paths

See [FEATURES.md](FEATURES.md) for the full list.

## What this provides

- **Container image** `ghcr.io/damian-buho/report-relay:latest`
- **Container image** `docker.io/damianbuho/report-relay:latest`

## Supported platforms

- `linux/amd64`
- `linux/arm64`

## Installation

Pull the published container image:

### Pull from GHCR

```sh
docker pull ghcr.io/damian-buho/report-relay:latest
```

### Pull from DockerHub

```sh
docker pull docker.io/damianbuho/report-relay:latest
```

Stable releases also publish `X.Y.Z`, `X.Y` and `X` tags — pull the precision you want to pin.

If the registries above are unreachable, pull from the origin instead:

### Pull from Kiota

```sh
docker pull kiota.ch/damian-buho/report-relay:latest
```

## Usage

Bring the stack up locally:

```sh
make dc-up
make dc-logs
make dc-down
```

## Building

Run `make` with no arguments for the default target; run `make help` to list every target.

For the local dev loop, `make dev-container` brings up the dev-container.

Pipeline entry points:

- `make analyze` — Run the heavy analysis sweep (mutation testing, benchmarks)
- `make audited` — Re-scan the pinned dependencies and published artifacts for new vulnerabilities
- `make check-outdated` — Report every pinned dependency that lags upstream
- `make ready-to-publish` — Run the pseudo-CI pipeline locally — build, test and scan, without publishing

## Policies

- [How to contribute](CONTRIBUTING.md)
- [Security policy](SECURITY.md)
- [Getting support](SUPPORT.md)
- [Code of Conduct](CODE_OF_CONDUCT.md)
- [AI and LLM Policy](AI_POLICY.md)

## Links

- [Projectfile Specification](https://projectfile.org)

## License

This project is licensed under MIT — see the [LICENSE](LICENSE) file for details.
