[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](configuration.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](configuration.en.md)

# Конфігурація сервера

Конфігурація сервера - `/etc/gitlab-repopolicy/config.yaml`, YAML. Пакет ставить її як conffile: оновлення не перезаписує ваші зміни. Шлях передає wrapper hook (`--config`). Серверні команди зазвичай використовують його за замовчуванням; локальні `validate` і `lint-commit` читають конфігурацію лише з явним `--config`.

Конфігурація описує інсталяцію: адресу API, токен, лог, ліміти, пресети. Правила проєктів у ній не задаються: вони лежать у policy-файлах репозиторіїв.

Невідомі ключі, зокрема невідоме ім'я модуля в `modules`, YAML anchors і aliases - помилка. Hook із невалідною конфігурацією відмовляє кожному push у проєкти з текстом "the hook configuration on the server is invalid", тому після правки запускайте `selftest`.

## Мінімальна конфігурація

Для початкового спостереження на сервері Linux package з локальним Workhorse достатньо такого `/etc/gitlab-repopolicy/config.yaml`:

```yaml
mode: audit
gitlab_url: http+unix://%2Fvar%2Fopt%2Fgitlab%2Fgitlab-workhorse%2Fsockets%2Fsocket
```

Решту значень заповнює завантажувач конфігурації. Токен він очікує в `/etc/gitlab-repopolicy/token`, журнал - у `/var/log/gitlab-repopolicy/audit.log`; усі чотири модулі ввімкнені. Файли й права готуються за [інструкцією встановлення](install.md). Мінімальний YAML не створює service account, токен або каталоги.

Тут `audit` задано явно, бо програмне значення `mode` за замовчуванням - `enforce`. Відсутній `default_branch` дорівнює `[main, master]`: основна гілка групового проєкту має називатися `main` або `master`, інакше push відхиляється. Для окремого вузла Gitaly замініть `gitlab_url` на HTTPS-адресу свого GitLab і за потреби додайте `ca_file`.

### Перед копіюванням конфігурації з пакета

Поставка нижче є повним зразком, а не мінімальною конфігурацією для першого запуску. У ній `default_branch: [main, master]`, як і програмне значення: у `enforce` воно відхилить push до групового проєкту з іншим ім'ям основної гілки, навіть без policy. `settings_check` теж містить приклади очікувань, які команда почне застосовувати одразу.

Перед підключенням hook задайте `mode: audit`, перевірте, що `[main, master]` підходить інстансу, або задайте свої імена чи `default_branch: []`, а очікування `settings_check` узгодьте з власниками проєктів. Не зменшуйте наявний список допустимих гілок на робочому сервері без перевірки audit.

## Повна конфігурація

Нижче дослівно наведено [config/config.yaml](../../../config/config.yaml) із пакета. Значення у стовпці "За замовчуванням" наступної таблиці є значеннями завантажувача при відсутньому ключі. Вони не завжди збігаються з поставкою: `gitlab_url` не має програмного default, `default_branch` порожній, обидва очікування `settings_check` не задані.

```yaml
# config/config.yaml
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
# name; it applies to every project of the instance, so check it with
# mode: audit first. [] turns the check off. [main, master] is the default.
default_branch: [main, master]

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
# rules; examples are in docs/user/administration/configuration.md.
presets:
  pathpolicy: {}
  commitpolicy: {}
  branchpolicy: {}
  tagpolicy: {}
```

## Параметри конфігурації

| Ключ | За замовчуванням | Значення |
| --- | --- | --- |
| `mode` | `enforce` | `off`, `audit` або `enforce`. Глобальний перемикач, див. нижче |
| `gitlab_url` | обов'язковий | API GitLab: `http+unix://` (socket Workhorse тієї ж ноди) або `https://host` |
| `ca_file` | `""` (порожній рядок) | додатковий CA bundle для `https://`. Непорожнє значення разом з `http+unix://` - помилка конфігурації |
| `token_file` | `/etc/gitlab-repopolicy/token` | токен service account ([install.md](install.md#3-service-account-і-токен)) |
| `token_lifetime` | `364d` | новий строк токена під час `token rotate`: сьогодні плюс це значення. Не менше `1d` |
| `audit.file` | `/var/log/gitlab-repopolicy/audit.log` | audit-лог. Hook пише лише сюди, тому для hook він обов'язковий |
| `audit.stdout` | `false` | `true` вмикає audit-події у stdout для `settings-check`, `token rotate`, `breakglass` і `test`; `false` не вмикає цей вивід. Hook завжди пише події лише у файл |
| `breakglass_dir` | `/etc/gitlab-repopolicy/breakglass` | дозволи break-glass ([operations.md](operations.md#break-glass)) |
| `timeouts.total` | `10s` | один виклик hook. Перевищення - `limit_exceeded` |
| `timeouts.http` | `3s` | один запит до API. Не більше `timeouts.total` |
| `max_api_calls` | `30` | викликів API на один push. Перевищення - `limit_exceeded` |
| `modules.<ім'я>` | `enabled` | `enabled` або `disabled` для `pathpolicy`, `commitpolicy`, `tagpolicy`, `branchpolicy` |
| `default_branch` | `[main, master]` | допустимі імена default branch для hook і `settings-check`; порожній список вимикає обмеження імені |
| `settings_check.protected_tags` | Порожній список (`[]`) | шаблони захисту тегів, яких очікує `settings-check` у проєктах із PATHPOLICY і TAGPOLICY; порожній список пропускає це порівняння |
| `settings_check.job_token_push` | Не задано | очікуваний стан push через `CI_JOB_TOKEN` у проєктах із PATHPOLICY: `true` або `false`. Без ключа це порівняння пропускається |
| `help.contact` | `""` (порожній рядок) | до кого звертатися, наприклад `#gitlab-support`; порожній рядок не додає контакт сервера |
| `help.docs_url` | `""` (порожній рядок) | посилання на документацію правил: `http://` або `https://`; порожній рядок не додає посилання сервера |
| `presets.<модуль>.<ім'я>` | Пресетів не задано | пресети для `extends` у файлах політик, окремо для `pathpolicy`, `commitpolicy`, `branchpolicy`, `tagpolicy`. Див. нижче |

Усі параметри, крім `gitlab_url`, можна пропустити, щоб отримати значення завантажувача з таблиці. Порожній `token_file` не означає "API без токена": поле має бути непорожнім абсолютним шляхом. Значення `audit.file: ""` допустиме лише для окремих команд із `audit.stdout: true`; сам hook потребує непорожній файл журналу.

| Ключ | У поставці пакета | Рішення для першого запуску |
| --- | --- | --- |
| `mode` | `enforce` | явно задайте `audit` до підключення hook |
| `gitlab_url` | socket Workhorse Linux package | перевірте шлях або задайте HTTPS для окремого вузла |
| `token_file` | `/etc/gitlab-repopolicy/token` | створіть файл токена й перевірте читання від git |
| `modules` | усі `enabled` | залиште ввімкненими, якщо немає рішення вимкнути політику для всього інстансу |
| `default_branch` | `[main, master]` | залиште, якщо основні гілки називаються `main` або `master`; інакше задайте свої імена або `[]` для відсутності обмеження |
| `settings_check.protected_tags` | `["v*"]` | задайте лише загальні для потрібних проєктів очікування |
| `settings_check.job_token_push` | `false` | це явне очікування, а не пропущена перевірка |
| `presets` | порожні mapping для чотирьох модулів | додайте тільки набори правил вашої організації |

Тривалість пишеться як у Go (`10s`, `100ms`, `1h30m`) або цілими днями з суфіксом `d` (`364d`). Шляхи - абсолютні.

`gitlab_url` для socket - шлях до socket, закодований як host:

```yaml
gitlab_url: http+unix://%2Fvar%2Fopt%2Fgitlab%2Fgitlab-workhorse%2Fsockets%2Fsocket
```

## `mode`

| `mode` | Що відбувається |
| --- | --- |
| `enforce` | рішення діють: порушення дає відмову |
| `audit` | порушення політик не блокують push; рішення пишуться в audit з `"enforced": false`. Використовуйте для першого запуску або тимчасового вимкнення відмов через політики |
| `off` | hook нічого не перевіряє і нічого не пише |

Режими застосовуються після успішного завантаження конфігурації. Невалідний конфіг, нечитабельний файл або помилка запуску git можуть дати відмову до ядра навіть у `audit`. `off` теж потребує валідного конфігу й непорожнього `audit.file` у wrapper hook.

Зміна діє з наступного push. Hook читає конфігурацію при кожному запуску, нічого перезапускати не треба.

Перший запуск на робочому інстансі - з `audit`:

```yaml
mode: audit
```

В audit-лозі видно, кому hook відмовив би: подія `decision` з `"decision": "deny"` і `"enforced": false`. Коли таких подій немає або всі вони очікувані, переходьте до `enforce` за [порядком інтеграції](integration-guide.md).

Перед переходом з `audit` в `enforce` переконайтеся, що в audit немає подій `invalid_policy`. Невалідна PATHPOLICY у default branch після перемикання заблокує проєкт повністю, невалідна TAGPOLICY - теги.

## `modules`

Вимкнений модуль hook не завантажує: його policy-файл стає звичайним файлом репозиторію. Нові версії файлу не проходять приймання, а змінювати його можуть ті, кому дозволяють path-правила PATHPOLICY (якщо правило на цей файл є). Модуль варто вимикати лише на всьому інстансі й надовго: після повторного ввімкнення у проєктах можуть опинитися невалідні файли, а невалідна TAGPOLICY блокує теги.

## `default_branch`

Список допустимих імен default branch, за замовчуванням `[main, master]`: обидва поширені імена дозволені без налаштування. Порожній список вимикає перевірку. Імена короткі, як їх показує GitLab, без `refs/heads/` і без `*`.

```yaml
default_branch: [main, master]    # за замовчуванням
default_branch: [main]            # усі проєкти мають default branch main
default_branch: []                # без обмеження імені
```

Hook читає всі policy-файли з default branch. Maintainer може в Settings > Repository > Branch defaults зробити default гілку, на якій policy немає, і наступний push пройшов би без правил. Тому hook відхиляє кожен push у проєкт, де default branch називається не так, як у списку, з кодом `default_branch_not_allowed`. Модулі при цьому не запускаються.

У порожньому репозиторії GitLab робить default branch першу гілку, яку туди запушили, як би вона не називалася. Тому перший push у репозиторій без гілок і тегів має принести гілку, на яку вказує HEAD (зазвичай `main`). Push лише іншої гілки відхиляється з підказкою спершу запушити `main`, push лише тегів не порушує саме цю перевірку, але має пройти глобальну перевірку досяжності комітів тегів.

Перевірка діє на всі проєкти інстансу, зокрема без policy-файлів, інакше обхід не закрити. Особисті namespace не перевіряються. Вмикайте її через глобальний `mode: audit`. Знайдіть в audit події `decision` з `"code": "default_branch_not_allowed"`, виправте default branch у цих проєктах і лише тоді переходьте на `enforce`.

Той самий список використовує `settings-check` ([operations.md](operations.md#settings-check)).

## `settings_check`

Що очікує `settings-check` від проєктів з файлами політик. Порожній `protected_tags` не перевіряється; для `job_token_push` пропущений ключ означає відсутність очікування, а явний `false` вимагає вимкненого push через job token.

```yaml
settings_check:
  protected_tags: ["v*"]   # теги v* захищені в Settings > Repository > Protected tags
  job_token_push: false    # push через CI_JOB_TOKEN вимкнений
```

| Ключ | Допустиме значення | Що звіряється |
| --- | --- | --- |
| `protected_tags` | Список шаблонів, наприклад `["v*"]` | ці шаблони захищені в Settings. Перевірка лише в проєктах із PATHPOLICY і TAGPOLICY в основній гілці. Порожній список `[]` пропускає це порівняння |
| `job_token_push` | `true` або `false`; ключ можна пропустити | у проєктах із PATHPOLICY `true` вимагає дозволеного push через `CI_JOB_TOKEN`, `false` - вимкненого. Пропущений ключ означає, що стан цього дозволу не перевіряється |

Незалежно від цих ключів `settings-check` перевіряє:
- у всіх проєктах з файлами політик - що кожен файл у default branch розбирається, а пресети з його `extends` є в серверному конфігу і не зламані
- у проєктах з PATHPOLICY - ім'я default branch за `default_branch`, що default branch захищена, шлях CI-конфігурації і що всі principals із policy-файлів існують
- у проєктах з BRANCHPOLICY - що GitLab захищає наявні гілки, чиє правило має `protected: true`
- у проєктах без PATHPOLICY - лише ім'я default branch, файли політик і захист гілок BRANCHPOLICY

Проєкти без файлів політик `settings-check` пропускає. [Інструкція settings-check](operations.md#settings-check) пояснює запуск перевірки, читання результатів і налаштування таймера.

Очікування одні на весь інстанс, налаштувань на групу чи проєкт немає. Тому в списки варто класти лише те, що вірно для всіх проєктів з файлами політик. `protected_tags` звіряє налаштовані шаблони захисту; відсутність самих тегів не скасовує це порівняння у проєкті з PATHPOLICY і TAGPOLICY. Перевірку CI-конфігурації варто залишити і для проєктів без `.gitlab-ci.yml`: Maintainer може ввімкнути CI пізніше або вказати шлях до конфігурації в іншому проєкті, а path-правило на `.gitlab-ci.yml` закриває цей обхід заздалегідь.

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
- у пресеті лише `rules`, у тому ж форматі, що у файлі модуля. `admins` PATHPOLICY і `keys` TAGPOLICY завжди у файлі проєкту ([чому для ключів](../examples/tagpolicy/organization-preset.md))
- кожне правило пресета повне: усі обов'язкові поля на місці
- `help` правила (`contact`, `docs_url`) друкується у відмові за цим правилом, поруч із глобальним `help` вище

Файл проєкту:

```yaml
version: 1
extends: org-ci             # or a list: [org-ci, team-ci]
admins: [platform-lead]
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
            header-format:
              conventional: { level: error }
            header-max-length:
              limit: { level: error, value: 100 }
            type-enum:
              allowed: { level: error, value: [feat, fix, docs, chore] }
            subject-trailing-dot:
              no-dot: { level: error, value: forbidden }
          message: Commits in the default branch follow Conventional Commits.
    team-strict:
      rules:
        conventional:
          match: default
          parser: conventional
          checks:
            header-max-length:
              limit: { level: error, value: 72 }
            body-min-line-length:
              feat-fix: { level: error, value: 1, types: [feat, fix] }
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
      header-max-length:
        limit: { level: warn }
      subject-trailing-dot:
        no-dot: { value: optional }
EOF
```

```console
$ gitlab-repopolicy validate --config presets.yaml
COMMITPOLICY: valid
  extends: org-commits, team-strict
  rule conventional: preset:org-commits, preset:team-strict, file
    checks: preset:org-commits, preset:team-strict, file
    checks.header-format: preset:org-commits
    checks.header-format.conventional: preset:org-commits
    checks.header-max-length: preset:org-commits, preset:team-strict, file
    checks.header-max-length.limit: preset:org-commits, preset:team-strict, file
    checks.subject-trailing-dot: preset:org-commits, file
    checks.subject-trailing-dot.no-dot: preset:org-commits, file
    checks.type-enum: preset:org-commits
    checks.type-enum.allowed: preset:org-commits
    checks.body-min-line-length: preset:team-strict
    checks.body-min-line-length.feat-fix: preset:team-strict
    match: preset:team-strict
    message: preset:team-strict
    parser: preset:team-strict
    help: preset:team-strict
    help.contact: preset:team-strict
```

Як читати:
- перевірки зливаються за типом і ім'ям. `checks.header-max-length.limit` пройшла всі три шари. `value: 72` з `team-strict` перемогло `100` з `org-commits`, бо `team-strict` пізніше в `extends`. `level: warn` з файлу перемогло `error`
- `checks.subject-trailing-dot.no-dot` лишилася в правилі, але файл задав їй `value: optional`: крапка в кінці subject може бути або не бути. Щоб прибрати перевірку зовсім, файл написав би `no-dot: null`, і рядка `checks.subject-trailing-dot.no-dot` у виводі не було б
- `match`, `message` і `parser` - з `team-strict`: пізніший шар замінив значення `org-commits`. Для `match` і `parser` вони однакові, а `message` став текстом команди
- `checks.type-enum` є лише в `org-commits`, `checks.body-min-line-length` - лише в `team-strict`, і обидві діють

Перевірка повідомлення за злитими правилами:

```console
$ gitlab-repopolicy lint-commit --config presets.yaml -m "fix: round refunds to cents in the payment service and in the monthly reports"
message: [conventional] warning: header-max-length limit: the header is 77 characters, the limit is 72
message: [conventional] error: body-min-line-length feat-fix: the message needs a body
```

### Зламаний або відсутній пресет

Конфіг із невалідним пресетом завантажується, а пресет позначається зламаним. Про нього кажуть `selftest`, `validate --config` і `settings-check`. Файл, який бере такий пресет або пресет, якого немає:

| Модуль | Що відбувається на push |
| --- | --- |
| PATHPOLICY, TAGPOLICY | файл вважається зламаним, як невалідний файл у default branch (fail closed) |
| COMMITPOLICY, BRANCHPOLICY | модуль пропускається з попередженням |

Нова версія файлу з таким `extends` не приймається (`invalid_policy_update`).

### Пресети для розробників

Щоб `validate` і `lint-commit` на ноутбуці бачили ті самі пресети, що й сервер, надайте розробникам файл лише з ключем `presets`, наприклад через репозиторій розгортання. За основу можна взяти [examples/presets.yaml](../../../examples/presets.yaml).

```sh
gitlab-repopolicy validate --config presets.yaml
gitlab-repopolicy lint-commit --config presets.yaml -m "$(cat .git/COMMIT_EDITMSG)"
```

`validate` і `lint-commit` з `--config` читають лише `presets`, інші ключі не потрібні й не перевіряються. `validate --online` звертається до API і читає конфіг повністю. [Посібник validate](../reference/validate.md#пресети) показує, як перевірити файл із пресетом локально.

## Інші конфігурації

[config/release-verify.yaml](../../../config/release-verify.yaml) - конфігурація, якою проєкт перевіряє підпис власних релізних тегів: увімкнено лише TAGPOLICY, audit у stdout, API не використовується ([release.md](../../development/release.md)).

## Потенційно небезпечні налаштування

Зміни конфігурації діють на наступні запуски на відповідному вузлі. Вони не проходять перевірку policy-файлами проєктів.

| Зміна | Вплив | Перевірка й відкат |
| --- | --- | --- |
| Перше підключення hook або звуження `default_branch` | може відхилити всі push групових проєктів, де основна гілка називається не `main` чи `master` або не так, як у новому списку | спершу `audit`, аналіз `default_branch_not_allowed`; повернути попередній список |
| Перехід `audit` → `enforce` | починає застосовувати накопичені відмови, включно зі зламаними policy | перевірити всі `deny` й `invalid_policy`; повернути `audit` |
| `modules.*: disabled` | прибирає перевірки й приймання файлу цього модуля; вимкнення PATHPOLICY прибирає її контроль власників | перевірити проєкти до повторного ввімкнення; повернути попередній набір модулів |
| Зміна чи видалення пресета | змінює всі проєкти з цим `extends`; для PATHPOLICY і TAGPOLICY зламаний пресет може блокувати зміни | `validate --config` на споживачах до доставки; відновити узгоджений пресет на всіх вузлах |
| Зменшення часу або бюджету API | може створити відмови чи пропущені перевірки на звичайному навантаженні | оцінити `duration_ms`, `api_calls`, `limit_exceeded`; повернути попередні межі |
| Зміна `token_file` або `gitlab_url` | потрібні API-перевірки можуть втратити доступ | selftest від root і git; повернути доступний шлях або endpoint |

## Перевірка конфігурації

Після зміни, до розгортання на всі вузли, запустіть на підготовленому вузлі:

```sh
sudo gitlab-repopolicy selftest --config /etc/gitlab-repopolicy/config.yaml
sudo -u git gitlab-repopolicy selftest --config /etc/gitlab-repopolicy/config.yaml
```

`selftest` читає конфіг повністю, перевіряє пресети, локальні файли й API. `validate --config` без `--online` читає лише `presets` і не замінює цю перевірку серверної конфігурації. Нові пресети також перевірте з реальними policy-файлами споживачів.

Після доставки виконайте контрольний push і перевірте журнал. Не обмежуйте аналіз `event == "decision"`: відмова може бути подією `authorization_backend_failure` або `internal_error`. Порядок переходу до блокування описано в [інтеграції](integration-guide.md), інтерпретацію подій - в [експлуатації](operations.md#audit).
