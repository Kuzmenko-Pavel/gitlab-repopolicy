[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](organization-preset.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](organization-preset.en.md)

# Спільні правила організації

Команда платформи веде CI-правила в одному серверному пресеті. Проєкт підключає їх через `extends`, додає свого maintainer kpn до дозволених користувачів і прибирає правило deploy-скриптів, яких у ньому немає.

## Сервер і проєкт

Адміністратор додає цей фрагмент до серверного `config.yaml` на кожному вузлі Gitaly. Для локальної перевірки збережіть його як `presets.yaml`: інших серверних ключів офлайн-команді не потрібно.

```yaml
presets:
  pathpolicy:
    org-ci:
      rules:
        ci-config:
          match: all
          paths: [".gitlab-ci.yml", ".gitlab/ci/**"]
          allow: [platform-lead, sre-lead]
          message: The platform team changes the CI/CD configuration.
          help:
            contact: "#platform"
        deploy:
          match: all
          paths: ["scripts/deploy/**"]
          allow: [platform-lead]
          message: The deploy scripts belong to platform-lead.
```

У корені репозиторію створіть `PATHPOLICY`:

```yaml
# examples/org-ci/PATHPOLICY
# The CI rules of the organization come from the preset org-ci. This project
# lets its own maintainer kpn change the CI configuration too, and drops the
# rule for deploy scripts, which it does not have.
version: 1
extends: org-ci

admins: [platform-lead]

rules:
  ci-config:
    allow: [platform-lead, sre-lead, kpn]
  deploy: null
```

platform-lead має бути активним Maintainer або Owner; інші username мають існувати. Перевірте `gitlab-repopolicy validate --config presets.yaml -f PATHPOLICY` до merge від Maintainer або Owner.

`allow` у файлі замінює весь список, тому platform-lead і sre-lead перелічено повторно. `deploy: null` видаляє правило. `match`, `paths`, `message` і `help` правила `ci-config` залишаються з пресета.

Результат злиття показує `validate`:

```text
$ gitlab-repopolicy validate --config presets.yaml
PATHPOLICY: valid
  extends: org-ci
  rule ci-config: preset:org-ci, file
    allow: file
    help: preset:org-ci
    help.contact: preset:org-ci
    match: preset:org-ci
    message: preset:org-ci
    paths: preset:org-ci
```

`explain` на ноді Gitaly показує те саме для конкретного шляху і людини:

```text
$ gitlab-repopolicy explain --project acme/platform/app --user kpn --path .gitlab-ci.yml --ref feature
acme/platform/app, PATHPOLICY of main; path .gitlab-ci.yml, ref refs/heads/feature, user kpn
  ci-config (match: all; from preset:org-ci, file): allowed; allow: platform-lead, sre-lead, kpn
    fields from: allow file; help preset:org-ci; help.contact preset:org-ci; match preset:org-ci; message preset:org-ci; paths preset:org-ci
result: kpn may change .gitlab-ci.yml on refs/heads/feature (as a person: a deploy key never may)
```

Типова помилка - написати в `allow` лише `[kpn]`, щоб "додати" kpn. Список не доповнюється, а замінюється: після злиття CI змінював би лише kpn.

## Наслідки для команди

У глобальному `enforce` kpn може надсилати зміни `.gitlab-ci.yml`, bob - ні, якщо інші перевірки GitLab дозволяють дію. Deploy-скрипти цим файлом уже не захищені: переконайтеся, що проєкт справді їх не використовує.

Записати лише `allow: [kpn]` означало б забрати право в platform-lead і sre-lead, а не додати користувача. Перейменування або видалення серверного пресета також не є локальною правкою: кожна PATHPOLICY з цим `extends` стане непридатною. Перевірте файли проєктів до доставки й `selftest` після неї; для відкату поверніть узгоджену версію пресета на всі вузли.

[Довідник PATHPOLICY](../../policies/pathpolicy.md)
