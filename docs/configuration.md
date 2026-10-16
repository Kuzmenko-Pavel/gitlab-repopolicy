[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](configuration.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](configuration.en.md)

# Конфігурація сервера

Конфігурація сервера - `/etc/gitlab-repopolicy/config.yaml`, YAML. Пакет ставить її як conffile: оновлення не перезаписує ваші зміни. Шлях передає wrapper hook (`--config`), той самий шлях - за замовчуванням у всіх командах.

Конфігурація описує інсталяцію: адресу API, токен, лог, ліміти, пресети. Правила проєктів у ній не задаються: вони лежать у policy-файлах репозиторіїв.

Невідомі ключі, YAML anchors і aliases - помилка. Hook із невалідною конфігурацією відмовляє кожному push у проєкти з текстом "the hook configuration on the server is invalid", тому після правки запускайте `selftest`.

## Файл повністю

Це [config/config.yaml](../config/config.yaml) з пакета. Значення - типові, крім прикладу в `settings_check`.

```yaml
# Server config of gitlab-repopolicy: /etc/gitlab-repopolicy/config.yaml.
# The package installs this file as a conffile; values below are the defaults
# unless marked otherwise. Unknown keys are an error.

# off | audit | enforce: the global switch. audit writes every decision to the
# audit log with "enforced": false and lets the push through.
mode: enforce

# GitLab API: the Workhorse socket of the same node, or https:// for Gitaly
# on a separate node. Required.
gitlab_url: http+unix://%2Fvar%2Fopt%2Fgitlab%2Fgitlab-workhorse%2Fsockets%2Fsocket

# Extra CA bundle for https://; not used with http+unix://.
ca_file: ""

# Token of the instance service account: Maintainer in every top-level group,
# scopes exactly read_api and self_rotate. root:<git-user> 0440, written only
# by "gitlab-repopolicy token rotate".
token_file: /etc/gitlab-repopolicy/token

# Expiry of a rotated token: token rotate always sends expires_at = today + this.
token_lifetime: 364d

audit:
  # The hook writes only to this file: its stdout and stderr reach the pusher.
  file: /var/log/gitlab-repopolicy/audit.log
  # Other commands (settings-check, token rotate, breakglass, test) also print
  # events to stdout, for journald or a container runtime.
  stdout: false

# Break-glass grants, written by "gitlab-repopolicy breakglass grant" as root.
breakglass_dir: /etc/gitlab-repopolicy/breakglass

timeouts:
  total: 10s            # one hook invocation
  http: 3s              # one API request
  pattern: 100ms        # the COMMITPOLICY pattern rule, per message
  pattern_budget: 2s    # the same, per push

# API calls per push; more is limit_exceeded.
max_api_calls: 30

# Modules are enabled unless disabled here.
modules:
  pathpolicy: enabled
  commitpolicy: enabled
  tagpolicy: enabled

# What settings-check expects of every project with PATHPOLICY. An empty
# value is not checked; ci_config_path and principals are always checked.
settings_check:
  default_branch: main
  protected_branches: [main]
  protected_tags: ["v*"]
  job_token_push: false

# Presets for "extends:" in COMMITPOLICY; an example is in docs/configuration.md.
commitpolicy_presets: {}
```

## Параметри

| Ключ | За замовчуванням | Значення |
| --- | --- | --- |
| `mode` | `enforce` | `off`, `audit` або `enforce`. Глобальний перемикач, див. нижче |
| `gitlab_url` | обов'язковий | API GitLab: `http+unix://` (socket Workhorse тієї ж ноди) або `https://host` |
| `ca_file` | порожньо | додатковий CA bundle для `https://`. З `http+unix://` не використовується |
| `token_file` | `/etc/gitlab-repopolicy/token` | токен service account ([install.md](install.md#3-service-account-і-токен)) |
| `token_lifetime` | `364d` | новий строк токена під час `token rotate`: сьогодні плюс це значення. Не менше `1d` |
| `audit.file` | `/var/log/gitlab-repopolicy/audit.log` | audit-лог. Hook пише лише сюди, тому для hook він обов'язковий |
| `audit.stdout` | `false` | інші команди (`settings-check`, `token rotate`, `breakglass`, `test`) ще й друкують події в stdout, для journald чи контейнерного runtime |
| `breakglass_dir` | `/etc/gitlab-repopolicy/breakglass` | дозволи break-glass ([operations.md](operations.md#break-glass)) |
| `timeouts.total` | `10s` | один виклик hook. Перевищення - `limit_exceeded` |
| `timeouts.http` | `3s` | один запит до API. Не більше `timeouts.total` |
| `timeouts.pattern` | `100ms` | правило `pattern` COMMITPOLICY на одне повідомлення |
| `timeouts.pattern_budget` | `2s` | те саме на весь push. Не менше `timeouts.pattern` |
| `max_api_calls` | `30` | викликів API на один push. Перевищення - `limit_exceeded` |
| `modules.<ім'я>` | `enabled` | `enabled` або `disabled` для `pathpolicy`, `commitpolicy`, `tagpolicy` |
| `settings_check.*` | порожньо | що очікує `settings-check`, див. нижче |
| `commitpolicy_presets` | порожньо | пресети для `extends` у COMMITPOLICY ([commitpolicy.md](commitpolicy.md#пресети-extends)) |

Тривалість пишеться як у Go (`10s`, `100ms`, `1h30m`) або цілими днями з суфіксом `d` (`364d`). Шляхи - абсолютні.

`gitlab_url` для socket - шлях до socket, закодований як host:

```yaml
gitlab_url: http+unix://%2Fvar%2Fopt%2Fgitlab%2Fgitlab-workhorse%2Fsockets%2Fsocket
```

## `mode`

| `mode` | Що відбувається |
| --- | --- |
| `enforce` | рішення діють: порушення дає відмову |
| `audit` | hook нікому не відмовляє, а рішення пише в audit з `"enforced": false`. Режим для першого запуску і для швидкого відкату, коли причина збою незрозуміла |
| `off` | hook нічого не перевіряє і нічого не пише |

Зміна діє з наступного push: hook читає конфігурацію при кожному запуску, нічого перезапускати не треба.

Перед переходом з `audit` в `enforce` переконайтеся, що в audit немає подій `invalid_policy`: невалідна PATHPOLICY у default branch після перемикання заблокує проєкт повністю, невалідна TAGPOLICY - теги.

## `modules`

Вимкнений модуль hook не завантажує: його policy-файл стає звичайним файлом репозиторію. Нові версії файлу не проходять приймання, а змінювати його можуть ті, кому дозволяють path-правила PATHPOLICY (якщо правило на цей файл є). Модуль варто вимикати лише на всьому інстансі й надовго: після повторного ввімкнення у проєктах можуть опинитися невалідні файли, а невалідна TAGPOLICY блокує теги.

## `settings_check`

Що очікує `settings-check` від кожного проєкту з PATHPOLICY. Порожнє значення не перевіряється.

| Ключ | Що звіряється |
| --- | --- |
| `default_branch` | default branch проєкту |
| `protected_branches` | кожна гілка зі списку захищена. Інші захищені гілки проєкту не заважають |
| `protected_tags` | ці шаблони тегів захищені |
| `job_token_push` | дозвіл push через `CI_JOB_TOKEN` (`false` - має бути вимкнений) |

Незалежно від цих ключів `settings-check` завжди перевіряє шлях CI-конфігурації і те, що всі principals із policy-файлів існують. Докладно - [operations.md](operations.md#settings-check).

## `commitpolicy_presets`

Пресети - це правила COMMITPOLICY, спільні для багатьох репозиторіїв: файл у проєкті посилається на пресет через `extends` ([commitpolicy.md](commitpolicy.md#пресети-extends)). У пакеті секція порожня, бо стандарт комітів у кожної інсталяції свій. Приклад:

```yaml
# /etc/gitlab-repopolicy/config.yaml
commitpolicy_presets:
  org-conventional:
    rules:
      type-enum: { level: error, value: [feat, fix, build, chore, ci, docs, perf, refactor, revert, style, test] }
      subject-case: { level: error, value: lower-first }
      header-max-length: { level: warn, value: 100 }
```

Ім'я пресету - ключ секції, `rules` - правила в тому ж форматі, що й у ruleset COMMITPOLICY. Правила файлу проєкту перекривають правила пресету за іменем.

## Інші конфігурації

[config/release-verify.yaml](../config/release-verify.yaml) - конфігурація, якою проєкт перевіряє підпис власних релізних тегів: увімкнено лише TAGPOLICY, audit у stdout, API не використовується ([release.md](release.md)).
