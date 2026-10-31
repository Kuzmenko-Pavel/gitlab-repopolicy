[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](project-overrides.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](project-overrides.en.md)

# Зміни проєкту поверх пресета

Проєкт підключає стандарт організації, але забороняє `master`, прибирає demo-правила й пояснює власний формат робочих гілок. Треба перевірити не тільки зміни YAML, а й те, яке правило отримає ім'я після видалення спеціального класу.

## Вхідні файли

Цей фрагмент серверної конфігурації збережіть як `presets.yaml` для локальної перевірки. На сервер його доставляє адміністратор.

```yaml
presets:
  branchpolicy:
    org-branches:
      rules:
        release:
          match: ['/^release\/v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[a-z][a-z0-9]*)?$/']
          priority: 100
          protected: true
          message: Release branches look like release/v1.4.0. Protect release/* with "Allowed to push and merge Maintainers".
        release-bad:
          match: ['/^release\//']
          priority: 110
          deny: true
          message: Release branches look like release/v1.4.0.
        hotfix:
          match: ['/^hotfix\/v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[a-z][a-z0-9]*)?$/']
          priority: 200
          protected: true
          message: Hotfix branches look like hotfix/v1.4.1. Protect hotfix/* with "Allowed to push and merge Maintainers".
        hotfix-bad:
          match: ['/^hotfix\//']
          priority: 210
          deny: true
          message: Hotfix branches look like hotfix/v1.4.1.
        epic:
          match: ['/^epic\/[a-z0-9][a-z0-9._-]*$/']
          priority: 300
          protected: true
          message: Epic branches look like epic/payments. Protect epic/* in Protected branches.
        epic-bad:
          match: ['/^epic\//']
          priority: 310
          deny: true
          message: Epic branches look like epic/payments.
        demo:
          match: ['/^demo\/[a-z0-9][a-z0-9._-]*$/']
          priority: 400
          protected: true
          message: Demo branches look like demo/client-a. Protect demo/* in Protected branches.
        demo-bad:
          match: ['/^demo\//']
          priority: 410
          deny: true
          message: Demo branches look like demo/client-a.
        work:
          match: ['/^[a-z0-9][a-z0-9._-]*\/(([0-9]+|[A-Z][A-Z0-9]+-[0-9]+)\/)?[a-z0-9][a-z0-9._-]*$/']
          priority: 900
          user_prefix: true
          message: Work branches are <username>/[<issue>/]<name>, for example kpn/disable_auth or kpn/PAY-12/refunds.
```

У корені проєкту створіть `BRANCHPOLICY`:

```yaml
# examples/branch-standard/BRANCHPOLICY
# The branch standard of the organization comes from the preset org-branches
# of the server config. This project also forbids master, has no demo
# branches and names its own work branches in the message.
version: 1
extends: org-branches

rules:
  no-master:
    match: [master]
    priority: 10
    deny: true
    message: This project uses main; master must not be created.
  demo: null
  demo-bad: null
  work:
    message: Work branches are <username>/[<issue>/]<name>, for example kpn/PAY-12/refunds; PAY is the Jira project of this team.
```

`no-master` має менший `priority` за всі правила пресета. `demo: null` і `demo-bad: null` видаляють класи, але не забороняють їхні імена. `work.message` замінюється, а `match`, `priority` та `user_prefix` успадковуються.

`validate --config` показує, звідки кожне правило і поле (уривок):

```text
$ gitlab-repopolicy validate --config presets.yaml
BRANCHPOLICY: valid
  extends: org-branches
  ...
  rule work: preset:org-branches, file
    match: preset:org-branches
    message: file
    priority: preset:org-branches
    user_prefix: preset:org-branches
  rule no-master: file
    match: file
    priority: file
    deny: file
    message: file
```

`validate --branch` відповідає, яке правило спрацює для імені:

```text
$ gitlab-repopolicy validate --config presets.yaml --branch master
...
branch master: rule no-master (match: [master]; from file)
  result: denied, branches of this class may not be created
  message: This project uses main; master must not be created.

$ gitlab-repopolicy validate --config presets.yaml --branch demo/client-a
...
branch demo/client-a: rule work (match: [/^[a-z0-9][a-z0-9._-]*\/(([0-9]+|[A-Z][A-Z0-9]+-[0-9]+)\/)?[a-z0-9][a-z0-9._-]*$/]; from preset:org-branches, file)
  requires: demo is an active GitLab user (checked with --online)
  result: allowed when the requirements hold
```

Після видалення `demo` і `demo-bad` ім'я `demo/client-a` потрапляє в наступне правило, `work`. Тепер `demo` має бути username: це варто знати, коли прибираєте правило пресета.

<!-- check: branch-push examples/branch-standard/BRANCHPOLICY main,hotfix/* -->
| Нова гілка | Хто створює | Правило | Результат | Чому |
| --- | --- | --- | --- | --- |
| `master` | alice | `no-master` | відмова | `deny` |
| `demo/client-a` | alice | `work` | відмова | правила `demo` немає, а `demo` - не користувач GitLab |

Таблиця передбачає активну policy в основній гілці `main`, глобальний `enforce`, користувача alice й відсутнього користувача demo. GitLab захищає `main` і `hotfix/*`. Для перевірки файлу виконайте `gitlab-repopolicy validate -f BRANCHPOLICY --config presets.yaml` до merge.

Типова помилка - вважати видалення правила забороною класу. `demo/client-a` переходить у `work` і тепер потребує активного користувача demo. Якщо треба заборонити цей префікс незалежно від користувачів, додайте явне правило `deny` до `work` за порядком пріоритетів.

У `validate --branch` код 0 означає виконаний запит. Контрольний push `master` у глобальному `enforce` має бути відхилено з `branch_name_violation`.

[Довідник BRANCHPOLICY](../../policies/branchpolicy.md)
