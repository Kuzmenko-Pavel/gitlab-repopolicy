[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](configuration.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](configuration.en.md)

# Конфігурація сервера

Конфігурація сервера - `/etc/gitlab-repopolicy/config.yaml`, YAML. Пакет ставить її як conffile: оновлення не перезаписує ваші зміни. Шлях передає wrapper hook (`--config`), той самий шлях - за замовчуванням у всіх командах.

Конфігурація описує інсталяцію: адресу API, токен, лог, ліміти, пресети. Правила проєктів у ній не задаються: вони лежать у policy-файлах репозиторіїв.

Невідомі ключі, зокрема невідоме ім'я модуля в `modules`, YAML anchors і aliases - помилка. Hook із невалідною конфігурацією відмовляє кожному push у проєкти з текстом "the hook configuration on the server is invalid", тому після правки запускайте `selftest`.

## Файл повністю

Це [config/config.yaml](../config/config.yaml) з пакета. Значення в ньому типові, крім двох прикладів. `default_branch: [main]` і `settings_check` (`protected_tags: ["v*"]`, `job_token_push: false`) за замовчуванням порожні, тобто ці перевірки вимкнені, поки ви їх не задасте. Пакет кладе приклад, щоб було видно формат, тому перед першим запуском вирішіть, чи підходять ці значення вашому інстансу.

```yaml
# Server config of gitlab-repopolicy: /etc/gitlab-repopolicy/config.yaml.
# The package installs this file as a conffile; values below are the defaults
# unless marked as an example. Unknown keys are an error.

# off | audit | enforce: the global switch. audit writes every decision to the
# audit log with "enforced": false and lets the push through.
mode: enforce

# GitLab API: the Workhorse socket of the same node, or https:// for Gitaly
# on a separate node. Required.
gitlab_url: http+unix://%2Fvar%2Fopt%2Fgitlab%2Fgitlab-workhorse%2Fsockets%2Fsocket

# Extra CA bundle for https://; an error with http+unix://.
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
  branchpolicy: enabled

# Allowed names of the default branch, used by the hook and by settings-check.
# The hook rejects every push to a project whose default branch has another
# name; it applies to every project of the instance, so turn it on with
# mode: audit first. Empty turns the check off. [main] is an example; the
# default is empty.
default_branch: [main]

# What settings-check expects of every project with PATHPOLICY. Empty values
# are not checked and are the default; ["v*"] and false below are examples.
# Projects with PATHPOLICY also always get ci_config_path, principals and the
# protection of the default branch checked; projects with PATHPOLICY or
# BRANCHPOLICY the branches that BRANCHPOLICY requires to be protected;
# protected_tags are checked only in projects with PATHPOLICY and TAGPOLICY.
settings_check:
  protected_tags: ["v*"]
  job_token_push: false

# Where a pusher gets help, printed in denials; both are optional. contact is
# named when the server or a policy in the default branch is at fault, docs_url
# next to the rules. Up to 200 characters each.
help:
  contact: ""
  docs_url: ""

# Presets for "extends:" in COMMITPOLICY and in BRANCHPOLICY; examples are in
# docs/configuration.md.
commitpolicy_presets: {}
branchpolicy_presets: {}
```

## Параметри

| Ключ | За замовчуванням | Значення |
| --- | --- | --- |
| `mode` | `enforce` | `off`, `audit` або `enforce`. Глобальний перемикач, див. нижче |
| `gitlab_url` | обов'язковий | API GitLab: `http+unix://` (socket Workhorse тієї ж ноди) або `https://host` |
| `ca_file` | порожньо | додатковий CA bundle для `https://`. Разом з `http+unix://` - помилка конфігурації |
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
| `modules.<ім'я>` | `enabled` | `enabled` або `disabled` для `pathpolicy`, `commitpolicy`, `tagpolicy`, `branchpolicy` |
| `default_branch` | порожньо | допустимі імена default branch для hook і `settings-check`, див. нижче |
| `settings_check.*` | порожньо | що очікує `settings-check`, див. нижче |
| `help.contact` | порожньо | до кого звертатися, наприклад `#gitlab-support`. Див. нижче |
| `help.docs_url` | порожньо | посилання на документацію правил для розробників, `http://` або `https://` |
| `commitpolicy_presets` | порожньо | пресети для `extends` у COMMITPOLICY ([commitpolicy.md](commitpolicy.md#пресети-extends)) |
| `branchpolicy_presets` | порожньо | пресети для `extends` у BRANCHPOLICY, див. нижче |

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

Зміна діє з наступного push. Hook читає конфігурацію при кожному запуску, нічого перезапускати не треба.

Перший запуск на робочому інстансі - з `audit`:

```yaml
mode: audit
```

Push проходять як і раніше, а в audit-лозі видно, кому hook відмовив би: подія `decision` з `"decision": "deny"` і `"enforced": false`. Коли таких подій немає або всі вони очікувані, перемикайте на `enforce`. Порядок кроків - [integration-guide.md](integration-guide.md).

Перед переходом з `audit` в `enforce` переконайтеся, що в audit немає подій `invalid_policy`. Невалідна PATHPOLICY у default branch після перемикання заблокує проєкт повністю, невалідна TAGPOLICY - теги.

## `modules`

Вимкнений модуль hook не завантажує: його policy-файл стає звичайним файлом репозиторію. Нові версії файлу не проходять приймання, а змінювати його можуть ті, кому дозволяють path-правила PATHPOLICY (якщо правило на цей файл є). Модуль варто вимикати лише на всьому інстансі й надовго: після повторного ввімкнення у проєктах можуть опинитися невалідні файли, а невалідна TAGPOLICY блокує теги.

## `default_branch`

Список допустимих імен default branch. Порожній список (за замовчуванням) вимикає перевірку. Імена короткі, як їх показує GitLab, без `refs/heads/` і без `*`.

```yaml
default_branch: [main]            # усі проєкти мають default branch main
default_branch: [main, master]    # на час переходу з master на main
```

Hook читає всі policy-файли з default branch. Maintainer може в Settings > Repository > Branch defaults зробити default гілку, на якій policy немає, і наступний push пройшов би без правил. Тому hook відхиляє кожен push у проєкт, де default branch називається не так, як у списку, з кодом `default_branch_not_allowed`. Модулі при цьому не запускаються.

У порожньому репозиторії GitLab робить default branch першу гілку, яку туди запушили, як би вона не називалася. Тому перший push у репозиторій без гілок і тегів має принести гілку, на яку вказує HEAD (зазвичай `main`). Push лише іншої гілки відхиляється з підказкою спершу запушити `main`, push лише тегів проходить.

Перевірка діє на всі проєкти інстансу, зокрема без policy-файлів, інакше обхід не закрити. Особисті namespace не перевіряються. Вмикайте її через глобальний `mode: audit`. Знайдіть в audit події `decision` з `"code": "default_branch_not_allowed"`, виправте default branch у цих проєктах і лише тоді переходьте на `enforce`.

Той самий список використовує `settings-check` ([operations.md](operations.md#settings-check)).

## `settings_check`

Що очікує `settings-check` від кожного проєкту з PATHPOLICY. Порожнє значення не перевіряється.

```yaml
settings_check:
  protected_tags: ["v*"]   # теги v* захищені в Settings > Repository > Protected tags
  job_token_push: false    # push через CI_JOB_TOKEN вимкнений
```

| Ключ | Що звіряється |
| --- | --- |
| `protected_tags` | ці шаблони тегів захищені. Лише в проєктах з TAGPOLICY у default branch |
| `job_token_push` | дозвіл push через `CI_JOB_TOKEN` (`false` - має бути вимкнений) |

Незалежно від цих ключів `settings-check` перевіряє:
- у проєктах з PATHPOLICY - ім'я default branch за `default_branch`, що default branch захищена, шлях CI-конфігурації і що всі principals із policy-файлів існують
- у проєктах з BRANCHPOLICY - що захищені шаблони її правил з `protected: true`
- у проєктах лише з BRANCHPOLICY, без PATHPOLICY - тільки ім'я default branch і захист шаблонів BRANCHPOLICY

Проєкти без PATHPOLICY і BRANCHPOLICY `settings-check` пропускає. Докладно - [operations.md](operations.md#settings-check).

Очікування одні на весь інстанс, налаштувань на групу чи проєкт немає. Тому в списки варто класти лише те, що вірно для всіх проєктів з PATHPOLICY. `protected_tags` не дає drift у проєктах без релізних тегів, бо перевіряється лише там, де є TAGPOLICY. Перевірку CI-конфігурації варто залишити і для проєктів без `.gitlab-ci.yml`: Maintainer може ввімкнути CI пізніше або вказати шлях до конфігурації в іншому проєкті, а path-правило на `.gitlab-ci.yml` закриває цей обхід заздалегідь.

## `help`

Що побачить людина, якій hook відмовив, і куди їй звернутися. Обидва ключі необов'язкові, до 200 символів, без керуючих символів.

- `help.contact` друкується в рядку `who can help:` у відмовах, де користувач нічого не може виправити сам: збій API, ліміти сервера, внутрішня помилка, зламана policy в default branch. Без цього ключа там стоїть `your GitLab administrators`
- `help.docs_url` друкується в рядку `rules:` наприкінці кожної відмови й попередження

```yaml
help:
  contact: "#gitlab-support"
  docs_url: https://wiki.example.com/gitlab/repopolicy
```

```text
remote: rules: PATHPOLICY in main (0316d44); docs: https://wiki.example.com/gitlab/repopolicy
remote: checked as: alice (HTTPS)
```

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

**Пресети для розробників.** Щоб `validate` і `lint-commit` на ноутбуці бачили ті самі пресети, що й сервер, роздайте файл лише із секцією `commitpolicy_presets`, наприклад у репозиторії з інструментами команди:

```sh
gitlab-repopolicy lint-commit --config presets.yaml -f COMMITPOLICY -m "$(cat .git/COMMIT_EDITMSG)"
```

`validate` і `lint-commit` з `--config` читають лише `commitpolicy_presets` і `branchpolicy_presets`, інші ключі не потрібні й не перевіряються. `validate --online` звертається до API і читає конфігурацію повністю. Hook і серверні команди перевіряють конфігурацію завжди повністю.

## `branchpolicy_presets`

Пресет - стандарт імен гілок, спільний для багатьох проєктів: BRANCHPOLICY проєкту підключає його через `extends` ([branchpolicy.md](branchpolicy.md#пресети-extends)). У пакеті секція порожня. Приклад:

```yaml
# /etc/gitlab-repopolicy/config.yaml
branchpolicy_presets:
  org-branches:
    rules:
      - name: release
        match: "release/*"
        pattern: '^release/v[0-9]+\.[0-9]+\.[0-9]+$'
        protected: true
        message: Release branches look like release/v1.4.0. Protect release/* in Protected branches.
      - name: work
        match: "*"
        pattern: '^[a-z0-9.]+/[a-z0-9._/-]+$'
```

`rules` - правила в тому ж форматі, що й у BRANCHPOLICY. Невалідний пресет `selftest` показує як `FAIL`, а проєкти з `extends` на нього пропускають перевірку з попередженням.

## Інші конфігурації

[config/release-verify.yaml](../config/release-verify.yaml) - конфігурація, якою проєкт перевіряє підпис власних релізних тегів: увімкнено лише TAGPOLICY, audit у stdout, API не використовується ([release.md](release.md)).
