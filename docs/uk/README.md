<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
SPDX-License-Identifier: MIT
pf-cli-managed: yes
-->

<!-- textlint-disable terminology,common-misspellings -->

[English](../../README.md) · [Español](../es/README.md)

# Report Relay

Report Relay приймає звіти безпеки, які надсилають браузери та поштові сервери — пакети Reporting API, старі тіла report-uri від CSP, журнали помилок мережі та звіти SMTP TLS — і випускає один запис логу OpenTelemetry на звіт, тож вони потрапляють до наявного стеку Loki, Tempo та Grafana, а не до окремого збирача для кожного типу звітів.

[![Stand with Ukraine](https://raw.githubusercontent.com/vshymanskyy/StandWithUkraine/main/badges/StandWithUkraine.svg)](https://damian-buho.github.io/support-ukraine/) [![Projectfile inside](https://badges.kiota.ch/static/v1?label=projectfile&message=inside&labelColor=0d0d0d&color=8c6723&style=flat-square)](https://projectfile.org) [![License](https://badges.kiota.ch/static/v1?label=license&message=MIT&color=1e5913&style=flat-square)](LICENSE) [![Cosign](https://badges.kiota.ch/static/v1?label=cosign&message=enabled&color=1e5913&style=flat-square)](https://docs.sigstore.dev/cosign/verifying/verify/) [![PRs welcome](https://badges.kiota.ch/static/v1?label=PRs&message=welcome&color=1e5913&style=flat-square)](CONTRIBUTING.md) [![REUSE compliance](https://api.reuse.software/badge/codeberg.org/damian-buho/report-relay)](https://api.reuse.software/info/codeberg.org/damian-buho/report-relay)

![Project status](https://badges.kiota.ch/static/v1?label=status&message=experimental&color=1d63ed&style=flat-square) [![Last commit on kiota.ch](https://badges.kiota.ch/gitea/last-commit/damian-buho/report-relay?gitea_url=https://kiota.ch&label=last%20commit%20on%20kiota.ch&style=flat-square)](https://kiota.ch/damian-buho/report-relay) [![Last commit on Codeberg](https://badges.kiota.ch/gitea/last-commit/damian-buho/report-relay?gitea_url=https://codeberg.org&label=last%20commit%20on%20Codeberg&style=flat-square)](https://codeberg.org/damian-buho/report-relay) [![Last commit on GitHub](https://badges.kiota.ch/github/last-commit/damian-buho/report-relay?label=last%20commit%20on%20GitHub&style=flat-square)](https://github.com/damian-buho/report-relay)

[![Publish pipeline on GitHub](https://github.com/damian-buho/report-relay/actions/workflows/published.yaml/badge.svg?style=flat-square)](https://github.com/damian-buho/report-relay/actions) [![Vulnerability audit on GitHub](https://github.com/damian-buho/report-relay/actions/workflows/audited.yaml/badge.svg?style=flat-square)](https://github.com/damian-buho/report-relay/actions) [![Dependency freshness on GitHub](https://github.com/damian-buho/report-relay/actions/workflows/check-outdated.yaml/badge.svg?style=flat-square)](https://github.com/damian-buho/report-relay/actions) [![Analysis sweep on GitHub](https://github.com/damian-buho/report-relay/actions/workflows/analyzed.yaml/badge.svg?style=flat-square)](https://github.com/damian-buho/report-relay/actions)

[![Publish pipeline on kiota.ch](https://kiota.ch/damian-buho/report-relay/badges/workflows/published.yaml/badge.svg?style=flat-square)](https://kiota.ch/damian-buho/report-relay/actions) [![Vulnerability audit on kiota.ch](https://kiota.ch/damian-buho/report-relay/badges/workflows/audited.yaml/badge.svg?style=flat-square)](https://kiota.ch/damian-buho/report-relay/actions) [![Dependency freshness on kiota.ch](https://kiota.ch/damian-buho/report-relay/badges/workflows/check-outdated.yaml/badge.svg?style=flat-square)](https://kiota.ch/damian-buho/report-relay/actions) [![Analysis sweep on kiota.ch](https://kiota.ch/damian-buho/report-relay/badges/workflows/analyzed.yaml/badge.svg?style=flat-square)](https://kiota.ch/damian-buho/report-relay/actions)

## Можливості

- Повільний збирач ніколи не сповільнює браузер
- Звіти про інциденти CAA IODEF
- Звіти Cross-Origin-Embedder-Policy
- Вбудовується без змін у встановлення OpenTelemetry
- Звіти Cross-Origin-Opener-Policy
- Звіти про збої браузера
- Старі звіти CSP report-uri
- Звіти про порушення Content Security Policy
- Звіти про застарілі функції
- Звіти про порушення Document Policy
- Звіти про порушення Expect-CT
- Публічна точка приймання, захищена за замовчуванням
- Звіти про збої перевірки пінів HPKP
- Звіти про порушення Subresource Integrity
- Звіти про втручання браузера
- Звіти Network Error Logging
- Одна точка приймання для всіх звітів, які може надіслати сайт
- Звіти про порушення Permissions Policy
- Звіти SMTP TLS
- Тип звіту, який ще ніхто не визначив, усе одно зберігається

Також успадковує можливості B19 / Ubuntu — повний перелік див. у [Можливості](FEATURES.md).

## Що надає цей проєкт

- **Виконуваний файл** `report-relay` — команда `report-relay`
- **Образ контейнера** `ghcr.io/damian-buho/report-relay:latest`
- **Образ контейнера** `damianbuho/report-relay:latest`
- **Служба** `relay` — слухає на `8080 (intake)`, `8081 (admin)` — Приймання звітів і ендпоінти стану

## Встановлення

### Образ контейнера

Завантажте опублікований образ контейнера:

#### Завантажити з GHCR — linux/amd64, linux/arm64

```sh
docker pull ghcr.io/damian-buho/report-relay:latest
```

#### Завантажити з DockerHub — linux/amd64

```sh
docker pull damianbuho/report-relay:latest
```

Стабільні випуски також публікують теґи `X.Y.Z`, `X.Y` і `X` — завантажте той рівень точності, який хочете зафіксувати.

Якщо наведені вище реєстри недоступні, завантажте з джерела:

#### Завантажити з Kiota — linux/amd64

```sh
docker pull kiota.ch/damian-buho/report-relay:latest
```

### Готовий бінарний файл

Завантажте готовий бінарний файл для своєї платформи з випусків на GitHub:

```sh
mkdir -p ~/.local/bin
curl --fail --location --output ~/.local/bin/report-relay https://github.com/damian-buho/report-relay/releases/latest/download/report-relay-linux-$(uname -m)
chmod +x ~/.local/bin/report-relay
~/.local/bin/report-relay --help
```

Опубліковано для: `linux/amd64`, `linux/arm64`, `linux/riscv64`

## Використання

Запустіть сервіс у фоновому режимі, опублікувавши його порти:

### З GHCR

```sh
docker run --detach --publish 8080:8080/tcp --publish 8081:8081/tcp ghcr.io/damian-buho/report-relay:latest
```

### З DockerHub

```sh
docker run --detach --publish 8080:8080/tcp --publish 8081:8081/tcp damianbuho/report-relay:latest
```

Потім перевірте, що він відповідає:

```sh
curl http://localhost:8081/healthz
```

## Збирання

Клонуйте репозиторій разом із підмодулями:

```sh
git clone --recurse-submodules https://codeberg.org/damian-buho/report-relay report-relay && cd report-relay
```

Зберіть образ контейнера локально:

```sh
make container-build
```

- [Довідник із Makefile](../how-to/MAKEFILE.md)

Виконайте `make` без аргументів для типової цілі; виконайте `make help`, щоб переглянути всі цілі.

Для локального циклу розробки `make dev-container` піднімає dev-container.

Точки входу конвеєра:

- `make analyzed` — Запускає важкий аналіз (мутаційне тестування, бенчмарки)
- `make audited` — Повторно сканує закріплені залежності й опубліковані артефакти на нові вразливості
- `make check-outdated` — Звітує про кожну закріплену залежність, що відстає від upstream
- `make ready-to-publish` — Запускає псевдо-CI локально — збирає, тестує й сканує без публікації

## Політики

- [Як зробити внесок](CONTRIBUTING.md)
- [Політика безпеки](SECURITY.md)
- [Як отримати підтримку](SUPPORT.md)
- [Кодекс поведінки](CODE_OF_CONDUCT.md)
- [Політика щодо ШІ та LLM](AI_POLICY.md)

## Посилання

- [Специфікація Projectfile](https://projectfile.org)

## Ліцензія

Цей проєкт ліцензовано на умовах MIT — див. файл [LICENSE](LICENSE) для подробиць.

<!-- textlint-enable -->
