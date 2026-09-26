<!--
SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
SPDX-License-Identifier: MIT
pf-cli-managed: yes
-->

<!-- textlint-disable terminology,common-misspellings -->

[English](../../README.md) · [Español](../es/README.md)

# Report Relay

Реле OpenTelemetry для звітів безпеки браузера та пошти

[![Stand with Ukraine](https://raw.githubusercontent.com/vshymanskyy/StandWithUkraine/main/badges/StandWithUkraine.svg)](https://damian-buho.github.io/support-ukraine/) [![Projectfile inside](https://badges.kiota.ch/static/v1?label=projectfile&message=inside&labelColor=0d0d0d&color=8c6723&style=flat-square)](https://projectfile.org) [![License](https://badges.kiota.ch/static/v1?label=license&message=MIT&color=1e5913&style=flat-square)](LICENSE) [![Commit style](https://badges.kiota.ch/static/v1?label=commits&message=conventional%20v1.0.0&color=1877aa&style=flat-square)](https://www.conventionalcommits.org/uk/v1.0.0/) ![Workflow](https://badges.kiota.ch/static/v1?label=workflow&message=git-flow&color=1877aa&style=flat-square) [![Versioning](https://badges.kiota.ch/static/v1?label=versioning&message=semantic%20v2.0.0&color=1877aa&style=flat-square)](https://semver.org/lang/uk/) [![PRs welcome](https://badges.kiota.ch/static/v1?label=PRs&message=welcome&color=1e5913&style=flat-square)](CONTRIBUTING.md) [![Citation](https://badges.kiota.ch/static/v1?label=citation&message=cff&color=1877aa&style=flat-square)](CITATION.cff) [![REUSE compliance](https://api.reuse.software/badge/codeberg.org/damian-buho/report-relay)](https://api.reuse.software/info/codeberg.org/damian-buho/report-relay)

![Project status](https://badges.kiota.ch/static/v1?label=status&message=experimental&color=1d63ed&style=flat-square) [![Last commit on kiota.ch](https://badges.kiota.ch/gitea/last-commit/damian-buho/report-relay?gitea_url=https://kiota.ch&label=last%20commit%20on%20kiota.ch&style=flat-square)](https://kiota.ch/damian-buho/report-relay) [![Last commit on Codeberg](https://badges.kiota.ch/gitea/last-commit/damian-buho/report-relay?gitea_url=https://codeberg.org&label=last%20commit%20on%20Codeberg&style=flat-square)](https://codeberg.org/damian-buho/report-relay) [![Last commit on GitHub](https://badges.kiota.ch/github/last-commit/damian-buho/report-relay?label=last%20commit%20on%20GitHub&style=flat-square)](https://github.com/damian-buho/report-relay)

[![Publish pipeline on GitHub](https://github.com/damian-buho/report-relay/actions/workflows/published.yaml/badge.svg?style=flat-square)](https://github.com/damian-buho/report-relay/actions) [![Vulnerability audit on GitHub](https://github.com/damian-buho/report-relay/actions/workflows/audited.yaml/badge.svg?style=flat-square)](https://github.com/damian-buho/report-relay/actions) [![Dependency freshness on GitHub](https://github.com/damian-buho/report-relay/actions/workflows/check-outdated.yaml/badge.svg?style=flat-square)](https://github.com/damian-buho/report-relay/actions) [![Analysis sweep on GitHub](https://github.com/damian-buho/report-relay/actions/workflows/analyze.yaml/badge.svg?style=flat-square)](https://github.com/damian-buho/report-relay/actions)

[![Publish pipeline on kiota.ch](https://kiota.ch/damian-buho/report-relay/badges/workflows/published.yaml/badge.svg?style=flat-square)](https://kiota.ch/damian-buho/report-relay/actions) [![Vulnerability audit on kiota.ch](https://kiota.ch/damian-buho/report-relay/badges/workflows/audited.yaml/badge.svg?style=flat-square)](https://kiota.ch/damian-buho/report-relay/actions) [![Dependency freshness on kiota.ch](https://kiota.ch/damian-buho/report-relay/badges/workflows/check-outdated.yaml/badge.svg?style=flat-square)](https://kiota.ch/damian-buho/report-relay/actions) [![Analysis sweep on kiota.ch](https://kiota.ch/damian-buho/report-relay/badges/workflows/analyze.yaml/badge.svg?style=flat-square)](https://kiota.ch/damian-buho/report-relay/actions)

## Можливості

- Повільний збирач ніколи не сповільнює браузер
- Вбудовується без змін у встановлення OpenTelemetry
- Публічна точка приймання, захищена за замовчуванням
- Одна точка приймання для всіх звітів, які може надіслати сайт

### Успадковано від B19 / Ubuntu

- Постійний APT-кеш між збираннями
- Керування службовими процесами зі спрямуванням журналів (b19-exec)
- Кешовані завантаження артефактів із перевіркою цілісності (b19-fetch)
- Вимірюване виконання команд зі звітуванням про збої (b19-run)
- Одноразова ініціалізація (bootstrap.d)
- Модульні хуки збирання (build.d)
- Автоматичне визначення кількості CPU (NUMPROCS)
- Декларативне керування залежностями (b19-deps)
- Підключована система запуску (entrypoint.d)
- Перемикачі функцій для всіх підсистем
- Вбудований моніторинг стану (healthcheck.d)
- Багатомовний вивід shell (b19-i18n)
- Відстеження лініжу образу
- Структуроване журналування з фільтром за рівнем (b19-log)
- Контейнер без прав root за замовчуванням
- Підтримка ізольованих від інтернету (air-gapped/offline) збирання й виконання
- Ін’єкція оверлеїв під час виконання
- Відтворюваний базовий образ (зафіксований за digest)
- Перевірка портів
- Уніфіковане сімейство ранерів життєвого циклу
- Автозавантаження Docker-секретів (secrets)
- Хуки інтерактивної shell (shell.d)
- Плавна обробка сигналів
- Шаблони конфігурації Jinja2 (minijinja-cli)
- Вбудований тестовий фреймворк (test.d)
- Попередньо встановлені службові інструменти
- Шляхи XDG Base Directory

Див. [FEATURES.md](FEATURES.md), щоб переглянути повний перелік.

## Що надає цей проєкт

- **Образ контейнера** `ghcr.io/damian-buho/report-relay:latest`
- **Образ контейнера** `docker.io/damianbuho/report-relay:latest`

## Підтримувані платформи

- `linux/amd64`
- `linux/arm64`

## Встановлення

Завантажте опублікований образ контейнера:

### Завантажити з GHCR

```sh
docker pull ghcr.io/damian-buho/report-relay:latest
```

### Завантажити з DockerHub

```sh
docker pull docker.io/damianbuho/report-relay:latest
```

Стабільні випуски також публікують теґи `X.Y.Z`, `X.Y` і `X` — завантажте той рівень точності, який хочете зафіксувати.

Якщо наведені вище реєстри недоступні, завантажте з джерела:

### Завантажити з Kiota

```sh
docker pull kiota.ch/damian-buho/report-relay:latest
```

## Використання

Запустіть стек локально:

```sh
make dc-up
make dc-logs
make dc-down
```

## Збирання

Виконайте `make` без аргументів для типової цілі; виконайте `make help`, щоб переглянути всі цілі.

Для локального циклу розробки `make dev-container` піднімає dev-container.

Точки входу конвеєра:

- `make analyze` — Run the heavy analysis sweep (mutation testing, benchmarks)
- `make audited` — Re-scan the pinned dependencies and published artifacts for new vulnerabilities
- `make check-outdated` — Report every pinned dependency that lags upstream
- `make ready-to-publish` — Run the pseudo-CI pipeline locally — build, test and scan, without publishing

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
