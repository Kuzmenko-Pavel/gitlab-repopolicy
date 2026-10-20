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

# What settings-check expects of the projects with policy files. Empty values
# are not checked and are the default; ["v*"] and false below are examples.
# Every policy file of a project is also checked to parse with its presets;
# projects with PATHPOLICY always get ci_config_path, principals and the
# protection of the default branch checked; projects with BRANCHPOLICY the
# existing branches that its rules require to be protected; protected_tags
# are checked only in projects with PATHPOLICY and TAGPOLICY.
settings_check:
  protected_tags: ["v*"]
  job_token_push: false

# Where a pusher gets help, printed in denials; both are optional. contact is
# named when the server or a policy in the default branch is at fault, docs_url
# next to the rules. Up to 200 characters each.
help:
  contact: ""
  docs_url: ""

# Presets for "extends:" in the policy files, by module. A preset holds only
# rules; examples are in docs/configuration.md.
presets:
  pathpolicy: {}
  commitpolicy: {}
  branchpolicy: {}
  tagpolicy: {}
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
| `max_api_calls` | `30` | викликів API на один push. Перевищення - `limit_exceeded` |
| `modules.<ім'я>` | `enabled` | `enabled` або `disabled` для `pathpolicy`, `commitpolicy`, `tagpolicy`, `branchpolicy` |
| `default_branch` | порожньо | допустимі імена default branch для hook і `settings-check`, див. нижче |
| `settings_check.*` | порожньо | що очікує `settings-check`, див. нижче |
| `help.contact` | порожньо | до кого звертатися, наприклад `#gitlab-support`. Див. нижче |
| `help.docs_url` | порожньо | посилання на документацію правил для розробників, `http://` або `https://` |
| `presets.<модуль>.<ім'я>` | порожньо | пресети для `extends` у файлах політик, окремо для `pathpolicy`, `commitpolicy`, `branchpolicy`, `tagpolicy`. Див. нижче |

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

Що очікує `settings-check` від проєктів з файлами політик. Порожнє значення не перевіряється.

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
- у всіх проєктах з файлами політик - що кожен файл у default branch розбирається, а пресети з його `extends` є в серверному конфігу і не зламані
- у проєктах з PATHPOLICY - ім'я default branch за `default_branch`, що default branch захищена, шлях CI-конфігурації і що всі principals із policy-файлів існують
- у проєктах з BRANCHPOLICY - що GitLab захищає наявні гілки, чиє правило має `protected: true`
- у проєктах без PATHPOLICY - лише ім'я default branch, файли політик і захист гілок BRANCHPOLICY

Проєкти без файлів політик `settings-check` пропускає. Докладно - [operations.md](operations.md#settings-check).

Очікування одні на весь інстанс, налаштувань на групу чи проєкт немає. Тому в списки варто класти лише те, що вірно для всіх проєктів з файлами політик. `protected_tags` не дає drift у проєктах без релізних тегів, бо перевіряється лише там, де є TAGPOLICY. Перевірку CI-конфігурації варто залишити і для проєктів без `.gitlab-ci.yml`: Maintainer може ввімкнути CI пізніше або вказати шлях до конфігурації в іншому проєкті, а path-правило на `.gitlab-ci.yml` закриває цей обхід заздалегідь.

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

## `presets`

Пресет - іменований набір правил одного модуля в серверному конфігу. Стандарт, спільний для багатьох проєктів, лежить в одному місці, а файл проєкту бере його рядком `extends`. Пресети згруповано за модулем, як `modules`:

```yaml
presets:
  pathpolicy:
    org-ci:
      rules:
        ci-config:
          match: all
          paths: [".gitlab-ci.yml"]
          allow: [platform-lead]
          message: The platform team changes the CI configuration.
          help:
            contact: "#platform"
  commitpolicy: {}
  branchpolicy: {}
  tagpolicy: {}
```

- ім'я пресета - ключ, `[a-z0-9-]{1,63}`. Пресети різних модулів можуть мати однакові імена
- у пресеті лише `rules`, у тому ж форматі, що у файлі модуля. `admins` PATHPOLICY і `keys` TAGPOLICY завжди у файлі проєкту ([чому для ключів](tagpolicy.md#4-пресет-з-мітками-підписантів))
- кожне правило пресета повне: усі обов'язкові поля на місці
- `help` правила (`contact`, `docs_url`) друкується у відмові за цим правилом, поруч із глобальним `help` вище

Файл проєкту:

```yaml
version: 1
extends: org-ci             # або список: [org-ci, team-ci]
```

### Як зливаються пресети і файл

Як values-файли Helm: шари зливаються по черзі, пресети в порядку `extends`, файл останнім.
- mapping (правила, поля правила, `checks`, `except`, `help`) зливається ключ за ключем
- список і значення замінюються цілком: `allow: [kpn]` у файлі не додає kpn до списку пресета, а замінює список
- `null` видаляє ключ: правило, поле, перевірку
- пізніший шар перемагає

Обов'язкові поля перевіряються вже після злиття, тому у файлі правило може змінювати лише окремі поля правила з пресета. Нове правило файлу, якого немає в жодному пресеті, має бути повним.

### Покроковий приклад

Два пресети COMMITPOLICY: загальний для організації і суворіший для команди платежів.

```sh
cat > presets.yaml <<'EOF'
presets:
  commitpolicy:
    org-commits:
      rules:
        conventional:
          match: default
          parser: conventional
          checks:
            type-enum: { level: error, value: [feat, fix, docs, chore] }
            header-max-length: { level: error, value: 100 }
            subject-full-stop: { level: error, value: forbid }
          message: Commits in the default branch follow Conventional Commits.
    team-strict:
      rules:
        conventional:
          match: default
          parser: conventional
          checks:
            header-max-length: { level: error, value: 72 }
            body-required: { level: error, types: [feat, fix] }
          message: Commits of the payments team follow Conventional Commits with a body.
          help:
            contact: "#payments-team"
EOF
```

Проєкт бере обидва пресети, лише попереджає про довгі header і дозволяє крапку в кінці subject:

```sh
cat > COMMITPOLICY <<'EOF'
version: 1
extends: [org-commits, team-strict]
rules:
  conventional:
    checks:
      header-max-length: { level: warn }
      subject-full-stop: null
EOF
```

```console
$ gitlab-repopolicy validate --config presets.yaml
COMMITPOLICY: valid
  extends: org-commits, team-strict
  rule conventional: preset:org-commits, preset:team-strict, file
    checks: preset:org-commits, preset:team-strict, file
    checks.header-max-length: preset:org-commits, preset:team-strict, file
    checks.type-enum: preset:org-commits
    checks.body-required: preset:team-strict
    match: preset:team-strict
    message: preset:team-strict
    parser: preset:team-strict
    help: preset:team-strict
    help.contact: preset:team-strict
```

Як читати:
- `checks.header-max-length` пройшла всі три шари. `value: 72` з `team-strict` перемогло `100` з `org-commits`, бо `team-strict` пізніше в `extends`. `level: warn` з файлу перемогло `error`
- `checks.subject-full-stop` у виводі немає: файл видалив її через `null`
- `match`, `message` і `parser` - з `team-strict`: пізніший шар замінив значення `org-commits`. Для `match` і `parser` вони однакові, а `message` став текстом команди
- `checks.type-enum` є лише в `org-commits`, `checks.body-required` - лише в `team-strict`, і обидві діють

Перевірка повідомлення за злитими правилами:

```console
$ gitlab-repopolicy lint-commit --config presets.yaml -m "fix: round refunds to cents in the payment service and in the monthly reports"
message: [conventional] error: body-required: the message needs a body
message: [conventional] warning: header-max-length: the header is 77 characters, the limit is 72
```

### Зламаний або відсутній пресет

Конфіг із невалідним пресетом завантажується, а пресет позначається зламаним. Про нього кажуть `selftest`, `validate --config` і `settings-check`. Файл, який бере такий пресет або пресет, якого немає:

| Модуль | Що відбувається на push |
| --- | --- |
| PATHPOLICY, TAGPOLICY | файл вважається зламаним, як невалідний файл у default branch (fail closed) |
| COMMITPOLICY, BRANCHPOLICY | модуль пропускається з попередженням |

Нова версія файлу з таким `extends` не приймається (`invalid_policy_update`).

### Пресети для розробників

Щоб `validate` і `lint-commit` на ноутбуці бачили ті самі пресети, що й сервер, роздайте файл лише з ключем `presets`, наприклад у репозиторії розгортання. Повний приклад - [examples/presets.yaml](../examples/presets.yaml).

```sh
gitlab-repopolicy validate --config presets.yaml
gitlab-repopolicy lint-commit --config presets.yaml -m "$(cat .git/COMMIT_EDITMSG)"
```

`validate` і `lint-commit` з `--config` читають лише `presets`, інші ключі не потрібні й не перевіряються. `validate --online` звертається до API і читає конфіг повністю. Покроково - [validate.md](validate.md#пресети).

## Інші конфігурації

[config/release-verify.yaml](../config/release-verify.yaml) - конфігурація, якою проєкт перевіряє підпис власних релізних тегів: увімкнено лише TAGPOLICY, audit у stdout, API не використовується ([release.md](release.md)).
