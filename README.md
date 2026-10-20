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

Серверна перевірка правил репозиторію для GitLab Self-Managed. Глобальний `pre-receive` hook Gitaly перевіряє push у репозиторії проєктів за policy-файлами, які лежать у самому репозиторії. Якщо push порушує правила, GitLab його відхиляє і показує, що не так і як виправити.

## З чого почати

| Хто ви | Що вам потрібно | Куди дивитися |
| --- | --- | --- |
| розробник, якому GitLab відхилив push | зрозуміти відмову і виправити коміт, тег чи гілку | документ політики з рядка відмови: [COMMITPOLICY](docs/commitpolicy.md), [TAGPOLICY](docs/tagpolicy.md), [BRANCHPOLICY](docs/branchpolicy.md), [PATHPOLICY](docs/pathpolicy.md) |
| Maintainer проєкту | написати policy-файл, перевірити його локально і ввімкнути без зупинки роботи команди | ті самі документи політик, [validate.md](docs/validate.md), потім [integration-guide.md](docs/integration-guide.md) |
| адміністратор GitLab | встановити hook, налаштувати сервер, стежити за audit | [install.md](docs/install.md), [configuration.md](docs/configuration.md), [operations.md](docs/operations.md), [integration-guide.md](docs/integration-guide.md) |

## Політики

Кожна політика - окремий YAML-файл у корені репозиторію.

| Файл | Що вирішує |
| --- | --- |
| [PATHPOLICY](docs/pathpolicy.md) | хто може змінювати певні файли: CI-конфігурацію, deploy-логіку, prod-значення |
| [COMMITPOLICY](docs/commitpolicy.md) | яким має бути формат commit messages |
| [TAGPOLICY](docs/tagpolicy.md) | хто може випускати релізи: тег приймається лише з підписом дозволеного ключа OpenPGP або SSH |
| [BRANCHPOLICY](docs/branchpolicy.md) | як називаються нові гілки і які з них мають бути захищені в GitLab |

## Навіщо

У GitLab немає способу дозволити зміну конкретних файлів лише конкретним людям на будь-якому шляху зміни. Protected branches захищають цілу гілку, Code Owners працюють лише в merge request і обходяться прямим push, push rules є лише в Premium і не знають, хто вносить зміну. Клієнтські перевірки (commitlint, pre-commit) обходяться `--no-verify` і не бачать merge кнопкою та Web IDE.

gitlab-repopolicy перевіряє зміну на сервері, тому байдуже, як її внесли: push по HTTPS чи SSH, merge кнопкою, auto-merge, Web IDE, API, push із CI. Правила версіонуються і проходять review разом з кодом. Hook може лише відмовити, права GitLab він не розширює.

Коли hook нічого не перевіряє:
- push у проєкт особистого namespace користувача (`alice/my-project`). Перевіряються лише проєкти груп
- push у репозиторії wiki, design і snippets
- у конфігурації сервера `mode: off`

З `mode: audit` hook перевіряє все, але лише записує рішення в audit і нікому не відмовляє ([configuration.md](docs/configuration.md#mode)).

Приклад від файлу до виправлення. Команда платформи (alice і sre-lead) хоче, щоб CI-конфігурацію змінювала лише вона, у будь-якій гілці:

```yaml
# examples/ci-config/PATHPOLICY
# The platform team, alice and sre-lead, owns the CI configuration and the
# deploy scripts in every branch and the production values in main and
# release branches. Everything else is authorized by GitLab as usual.
version: 1

admins: [alice, sre-lead]

rules:
  ci-config:
    match: all
    paths:
      - ".gitlab-ci.yml"
      - ".gitlab/ci/**"
      - "scripts/deploy/**"
    allow: [alice, sre-lead]
    message: >-
      The platform team changes the CI/CD configuration and the deploy scripts.
      Ask alice or sre-lead to push this change.
    help:
      contact: "#platform"

  prod-values:
    match: [main, '/^release\//']
    paths: ["helm/**/values-prod.yaml"]
    allow: [alice, release-manager]
    message: >-
      Production values are changed in main and release branches by alice or
      release-manager. Propose the change in your branch and ask them to merge it.
```

bob, якого немає в `allow`, змінює `.gitlab-ci.yml` у своїй гілці `feature` і пушить. GitLab відхиляє push:

```text
remote: gitlab-repopolicy: push rejected by the rules of this repository (1 problem)
remote:
remote: PATHPOLICY rule "ci-config", branch feature: you may not change these files
remote:   may change them: alice, sre-lead
remote:   files:
remote:     .gitlab-ci.yml
remote:   The platform team changes the CI/CD configuration and the deploy scripts. Ask alice or sre-lead to push this change.
remote:   the content of .gitlab-ci.yml in commit 11e97b832 differs both from this branch and from main; new content is pushed by one of them
remote:   who can help with this rule: #platform
remote:
remote: rules: PATHPOLICY in main (f50c5a0)
remote: checked as: bob (SSH)
remote: GL-HOOK-ERR: gitlab-repopolicy: you may not change .gitlab-ci.yml (PATHPOLICY rule ci-config; may change: alice, sre-lead)
```

Що робить bob: просить alice внести зміну або прибирає її зі своїх комітів і пушить ще раз:

```sh
git restore --source=origin/main -- .gitlab-ci.yml
git commit --amend --no-edit
git push --force-with-lease origin feature
```

Як читати відмову рядок за рядком - [pathpolicy.md](docs/pathpolicy.md#відмови).

## Передумови

Перед встановленням мають бути:
- GitLab Self-Managed, CE або EE, з версіями з таблиці нижче
- права root на кожній ноді, де працює Gitaly
- instance service account з токеном (`read_api`, `self_rotate`), Maintainer у кожній top-level групі ([install.md](docs/install.md#3-service-account-і-токен))
- `custom_hooks_dir` у конфігурації Gitaly, наприклад `/var/opt/gitlab/gitaly/custom_hooks` у `gitlab.rb` і `gitlab-ctl reconfigure`. Без нього Gitaly не запускає глобальні hooks
- пакет `.deb` або `.rpm` з [GitHub Releases](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/releases) з перевіреним підписом ([install.md](docs/install.md#1-завантажити-й-перевірити-реліз))

## Перший запуск

Коротко для Linux package на одній ноді. Docker, окремі ноди Gitaly і всі кроки докладно - [docs/install.md](docs/install.md).

```sh
sudo apt install ./gitlab-repopolicy_1.0.0_linux_amd64.deb
sudo sh -c 'umask 337; cat > /etc/gitlab-repopolicy/token'     # токен service account, Enter, Ctrl-D
sudo gitlab-repopolicy install-hook --hooks-dir /var/opt/gitlab/gitaly/custom_hooks --user git
sudo gitlab-repopolicy selftest
sudo -u git gitlab-repopolicy selftest
```

Що має вийти:
- пакет ставить бінарник `/usr/bin/gitlab-repopolicy`, конфігурацію `/etc/gitlab-repopolicy/config.yaml` і вимкнені таймери
- `cat` нічого не друкує, токен лягає у файл з правами 0440
- `install-hook` друкує `.../pre-receive.d/50-gitlab-repopolicy: created`. З цієї миті hook перевіряє push
- `selftest` друкує рядки `ok`, `info` або `warn`, а будь-який `FAIL` дає код виходу 1. Від root і від `git` результат має бути однаковий, бо hook працює від `git`

Для робочого інстансу перед `install-hook` поставте в конфігурації `mode: audit`. Тоді hook нікому не відмовляє, а лише пише, кому відмовив би ([integration-guide.md](docs/integration-guide.md)).

Перевірити, що hook справді працює, можна на тестовому проєкті. Додайте в нього PATHPOLICY і зробіть push, який її порушує ([pathpolicy.md](docs/pathpolicy.md)). Відмова видна у виводі `git push`, рішення - у `/var/log/gitlab-repopolicy/audit.log`.

## Підтримувані версії GitLab

| GitLab CE | Результат e2e |
| --- | --- |
| 19.4 (19.4.2) | підтримується |
| 19.3 (19.3.5) | підтримується |
| 19.2 (19.2.8) | підтримується |

Матриця e2e проходить на поточній і двох попередніх minor-версіях GitLab CE, nightly - на `latest`. EE і будь-який tier підтримуються тим самим кодом: використовуються лише функції Free. GitLab.com і GitLab Dedicated не підтримуються.

## Редактор

Policy-файли не мають розширення, тому редактор треба навчити, що це YAML зі схемою. Схеми лежать у каталозі `schemas/` репозиторію. Посилання нижче ведуть на гілку `main`. Щоб редактор перевіряв файли за схемою конкретного релізу, замініть у посиланні `main` на тег релізу, наприклад `v1.0.0`.

**VS Code** з розширенням YAML (`yaml-language-server`), у `settings.json`:

```json
{
  "files.associations": { "PATHPOLICY": "yaml", "COMMITPOLICY": "yaml", "TAGPOLICY": "yaml", "BRANCHPOLICY": "yaml" },
  "yaml.schemas": {
    "https://raw.githubusercontent.com/Kuzmenko-Pavel/gitlab-repopolicy/main/schemas/pathpolicy.schema.json": "PATHPOLICY",
    "https://raw.githubusercontent.com/Kuzmenko-Pavel/gitlab-repopolicy/main/schemas/commitpolicy.schema.json": "COMMITPOLICY",
    "https://raw.githubusercontent.com/Kuzmenko-Pavel/gitlab-repopolicy/main/schemas/tagpolicy.schema.json": "TAGPOLICY",
    "https://raw.githubusercontent.com/Kuzmenko-Pavel/gitlab-repopolicy/main/schemas/branchpolicy.schema.json": "BRANCHPOLICY"
  }
}
```

**PyCharm, GoLand, IntelliJ IDEA** та інші IDE JetBrains перевіряють YAML за схемою без плагінів. Схеми написані за JSON Schema draft 2020-12.
1. Settings > Editor > File Types > YAML, у "File name patterns" додайте `PATHPOLICY`, `COMMITPOLICY`, `TAGPOLICY`, `BRANCHPOLICY`. Це налаштування самої IDE, його робить кожен у себе один раз
2. Settings > Languages & Frameworks > Schemas and DTDs > JSON Schema Mappings, кнопка `+` для кожного файлу:
   - Name: `PATHPOLICY`
   - Schema file or URL: `https://raw.githubusercontent.com/Kuzmenko-Pavel/gitlab-repopolicy/main/schemas/pathpolicy.schema.json`
   - Schema version: JSON Schema 2020-12
   - кнопкою `+` під списком додайте File path pattern `PATHPOLICY`

   Так само для `COMMITPOLICY`, `TAGPOLICY` і `BRANCHPOLICY`. Ці прив'язки IDE зберігає в проєкті, у `.idea/jsonSchemas.xml`: закомітьте файл, і в команди схеми підключаться самі

Після цього IDE підсвічує невідомі ключі й неправильні значення і показує підказки з описів полів.

Перевірити файл локально тим самим кодом, що й hook, можна командою `gitlab-repopolicy validate` ([validate.md](docs/validate.md)), повідомлення комітів - `gitlab-repopolicy lint-commit`. Редактор знає лише схему, а `validate` перевіряє ще й злиття з пресетами, регулярні вирази, порядок правил BRANCHPOLICY і ключі TAGPOLICY.

## Документація

| Документ | Про що |
| --- | --- |
| [install.md](docs/install.md) | встановлення за типами інсталяцій, перевірка релізу, service account |
| [configuration.md](docs/configuration.md) | `config.yaml` сервера |
| [operations.md](docs/operations.md) | audit і алерти, recovery, break-glass, ротація токена, `settings-check`, оновлення, типові проблеми |
| [pathpolicy.md](docs/pathpolicy.md), [commitpolicy.md](docs/commitpolicy.md), [tagpolicy.md](docs/tagpolicy.md), [branchpolicy.md](docs/branchpolicy.md) | довідник policy-файлів |
| [match.md](docs/match.md) | поле `match`: для яких гілок і тегів діє правило; позначення діаграм |
| [validate.md](docs/validate.md) | перевірка файлів політик без push, покроково |
| [cli.md](docs/cli.md) | усі команди і прапорці |
| [integration-guide.md](docs/integration-guide.md) | порядок увімкнення на робочому інстансі |
| [architecture.md](docs/architecture.md) | як влаштовано |
| [threat-model.md](docs/threat-model.md) | від чого захищає і від чого ні |
| [development.md](docs/development.md), [release.md](docs/release.md) | розробка і випуск релізу |

## Участь

[CONTRIBUTING.md](CONTRIBUTING.md), [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). Про вразливості - приватно, за [SECURITY.md](SECURITY.md).

## Ліцензія

[Apache-2.0](LICENSE)
