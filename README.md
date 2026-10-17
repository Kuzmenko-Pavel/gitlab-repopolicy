[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](README.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](README.en.md)

[![CI](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/actions/workflows/ci.yml?query=branch%3Amain)
[![e2e](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/actions/workflows/e2e.yml/badge.svg?branch=main)](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/actions/workflows/e2e.yml?query=branch%3Amain)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/Kuzmenko-Pavel/gitlab-repopolicy/badge)](https://scorecard.dev/viewer/?uri=github.com/Kuzmenko-Pavel/gitlab-repopolicy)
[![Coverage](https://codecov.io/gh/Kuzmenko-Pavel/gitlab-repopolicy/graph/badge.svg?branch=main)](https://codecov.io/gh/Kuzmenko-Pavel/gitlab-repopolicy)
[![Release](https://img.shields.io/github/v/release/Kuzmenko-Pavel/gitlab-repopolicy?sort=semver&include_prereleases)](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/releases)
[![License](https://img.shields.io/github/license/Kuzmenko-Pavel/gitlab-repopolicy)](./LICENSE)
[![Docs](https://img.shields.io/badge/docs-latest-blue)](#документація)

# gitlab-repopolicy

Серверна перевірка правил репозиторію для GitLab Self-Managed. Глобальний `pre-receive` hook Gitaly перевіряє кожен push за policy-файлами, які лежать у самому репозиторії:

| Файл | Що вирішує |
| --- | --- |
| [PATHPOLICY](docs/pathpolicy.md) | хто може змінювати певні файли: CI-конфігурацію, deploy-логіку, prod-значення |
| [COMMITPOLICY](docs/commitpolicy.md) | яким має бути формат commit messages |
| [TAGPOLICY](docs/tagpolicy.md) | хто може випускати релізи: тег приймається лише з підписом дозволеного ключа OpenPGP або SSH |
| [BRANCHPOLICY](docs/branchpolicy.md) | як називаються нові гілки і які з них мають бути захищені в GitLab |

## Навіщо

У GitLab немає способу дозволити зміну конкретних файлів лише конкретним людям на будь-якому шляху зміни. Protected branches захищають цілу гілку, Code Owners працюють лише в merge request і обходяться прямим push, push rules є лише в Premium і не знають, хто вносить зміну. Клієнтські перевірки (commitlint, pre-commit) обходяться `--no-verify` і не бачать merge кнопкою та Web IDE.

gitlab-repopolicy перевіряє зміну на сервері, хоч би як її внесли: push по HTTPS чи SSH, merge, auto-merge, Web IDE, API, push із CI. Правила версіонуються і проходять review разом з кодом. Hook може лише відмовити: права GitLab він не розширює.

```yaml
# examples/ci-config/PATHPOLICY
# The platform team owns the CI configuration, the deploy logic and the
# production values; everything else is authorized by GitLab as usual.
version: 1

admins: [platform-lead, sre-lead]

rules:
  - name: ci-config
    paths:
      - ".gitlab-ci.yml"
      - ".gitlab/ci/**"
      - "Makefile"
      - "scripts/deploy/**"
    allow: [platform-lead, sre-lead, ci-maintainer]
    message: >-
      The platform team changes the CI/CD configuration and the deploy logic.
      Ask platform-lead, sre-lead or ci-maintainer to make this change.

  - name: prod-values
    paths: ["helm/**/values-prod.yaml"]
    refs: protected
    allow: [platform-lead, release-manager]
```

## Встановлення за п'ять команд

Перед цим потрібні instance service account з токеном і `custom_hooks_dir` у конфігурації Gitaly. Покроково, зокрема перевірка підпису релізу, - [docs/install.md](docs/install.md).

```sh
sudo apt install ./gitlab-repopolicy_1.0.0_linux_amd64.deb
sudo sh -c 'umask 337; cat > /etc/gitlab-repopolicy/token'     # токен service account, Ctrl-D
sudo gitlab-repopolicy install-hook --hooks-dir /var/opt/gitlab/gitaly/custom_hooks --user git
sudo gitlab-repopolicy selftest
sudo -u git gitlab-repopolicy selftest
```

## Підтримувані версії GitLab

| GitLab CE | Результат e2e |
| --- | --- |
| 19.4 (19.4.2) | підтримується |
| 19.3 (19.3.5) | підтримується |
| 19.2 (19.2.8) | підтримується |

Матриця e2e проходить на поточній і двох попередніх minor-версіях GitLab CE, nightly - на `latest`. EE і будь-який tier підтримуються тим самим кодом: використовуються лише функції Free. GitLab.com і GitLab Dedicated не підтримуються.

## Редактор

Policy-файли не мають розширення, тому редактор треба навчити, що це YAML зі схемою. Для VS Code з розширенням YAML (`yaml-language-server`):

```json
{
  "files.associations": { "PATHPOLICY": "yaml", "COMMITPOLICY": "yaml", "TAGPOLICY": "yaml", "BRANCHPOLICY": "yaml" },
  "yaml.schemas": {
    "https://raw.githubusercontent.com/Kuzmenko-Pavel/gitlab-repopolicy/v1.0.0/schemas/pathpolicy.schema.json": "PATHPOLICY",
    "https://raw.githubusercontent.com/Kuzmenko-Pavel/gitlab-repopolicy/v1.0.0/schemas/commitpolicy.schema.json": "COMMITPOLICY",
    "https://raw.githubusercontent.com/Kuzmenko-Pavel/gitlab-repopolicy/v1.0.0/schemas/tagpolicy.schema.json": "TAGPOLICY",
    "https://raw.githubusercontent.com/Kuzmenko-Pavel/gitlab-repopolicy/v1.0.0/schemas/branchpolicy.schema.json": "BRANCHPOLICY"
  }
}
```

Перевірити файл локально тим самим кодом, що й hook: `gitlab-repopolicy validate`, повідомлення комітів - `gitlab-repopolicy lint-commit`.

## Документація

| Документ | Про що |
| --- | --- |
| [install.md](docs/install.md) | встановлення за типами інсталяцій, перевірка релізу, service account |
| [configuration.md](docs/configuration.md) | `config.yaml` сервера |
| [operations.md](docs/operations.md) | audit і алерти, recovery, break-glass, ротація токена, `settings-check`, оновлення, типові проблеми |
| [pathpolicy.md](docs/pathpolicy.md), [commitpolicy.md](docs/commitpolicy.md), [tagpolicy.md](docs/tagpolicy.md), [branchpolicy.md](docs/branchpolicy.md) | довідник policy-файлів |
| [integration-guide.md](docs/integration-guide.md) | порядок увімкнення на робочому інстансі |
| [architecture.md](docs/architecture.md) | як влаштовано |
| [threat-model.md](docs/threat-model.md) | від чого захищає і від чого ні |
| [development.md](docs/development.md), [release.md](docs/release.md) | розробка і випуск релізу |

## Участь

[CONTRIBUTING.md](CONTRIBUTING.md), [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). Про вразливості - приватно, за [SECURITY.md](SECURITY.md).

## Ліцензія

[Apache-2.0](LICENSE)
