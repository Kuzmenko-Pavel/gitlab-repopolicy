[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](branchpolicy.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](branchpolicy.en.md)

# BRANCHPOLICY

BRANCHPOLICY перевіряє ім'я кожної нової гілки і те, що гілки певних класів захищені в GitLab. Файл лежить у корені репозиторію з ім'ям `BRANCHPOLICY`, вміст - YAML.

Навіщо. Гілки `release/*` або `epic/*` деплоять, а pipeline незахищеної гілки не бачить protected CI/CD variables і protected runners. Гілку створили, CI нічого не задеплоїв, і причину шукають довго. Hook ловить це одразу, коли гілку створюють, і пише, що налаштувати. Друга мета - стандарт імен гілок: домовленість команди стає правилом на сервері, а не усною традицією.

Це гігієна, а не доступ. Тому BRANCHPOLICY за збою пропускає перевірку з попередженням (fail open), як COMMITPOLICY. Хто змінює сам BRANCHPOLICY, задає path-правило PATHPOLICY ([pathpolicy.md](pathpolicy.md)). Без такого правила файл змінює будь-хто з правом push.

## Приклад

```yaml
# A branch standard: release and hotfix branches are release/vX.Y.Z and
# hotfix/vX.Y.Z, epic and demo branches are created by any developer, and all
# four are protected; work branches are <prefix>/[<issue>/]<name>. A name
# that fits no class is denied. In an installation this list usually lives in
# branchpolicy_presets of the server config, and a project only extends it.
version: 1

rules:
  - name: release
    match: "release/*"
    pattern: '^release/v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[a-z][a-z0-9]*)?$'
    examples:
      valid: [release/v1.4.0, release/v1.4.0-staging]
      invalid: [release/foo, release/v1.2, release/v01.2.3]
    protected: true
    message: Release branches look like release/v1.4.0. Protect release/* with "Allowed to push and merge Maintainers".
  - name: hotfix
    match: "hotfix/*"
    pattern: '^hotfix/v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[a-z][a-z0-9]*)?$'
    examples:
      valid: [hotfix/v1.4.1]
      invalid: [hotfix/foo, hotfix/v1.4]
    protected: true
  - name: epic
    match: "epic/*"
    pattern: '^epic/[a-z0-9][a-z0-9._-]*(/[a-z0-9][a-z0-9._-]*)*$'
    examples:
      valid: [epic/payments]
      invalid: [epic/, epic/Payments]
    protected: true
  - name: demo
    match: "demo/*"
    pattern: '^demo/[a-z0-9][a-z0-9._-]*(/[a-z0-9][a-z0-9._-]*)*$'
    protected: true
  - name: work
    match: "*"
    pattern: '^[a-z0-9][a-z0-9._-]*/(([0-9]+|[A-Z][A-Z0-9]+-[0-9]+)(/[a-z0-9][a-z0-9._-]*)*|[a-z0-9][a-z0-9._-]*(/[a-z0-9][a-z0-9._-]*)*)$'
    examples:
      valid: [kpn/disable_auth, gks/INFRA-1719/change_logic, dmitry.mashoshin/123, dmitry.mashoshin/355/change_proce/auth]
      invalid: [kpn, kpn/, 123-fix-login, Kuzmenko.Pavel-main-patch-48213, revert-1a2b3c4d]
    message: Work branches are <prefix>/[<issue>/]<name>, for example kpn/disable_auth.
```

Як читається:
- нова гілка потрапляє в клас за першим правилом, у якого `match` збігся з ім'ям. `release/foo` потрапляє в клас `release` і там відхиляється, бо не відповідає `pattern`: у клас `work` вона вже не перейде
- `kpn/disable_auth` і `gks/INFRA-1719/change_logic` проходять правило `work`, а `123-fix-login` - ні
- `release/v1.4.0` проходить, лише якщо проєкт захищає `release/*` (Settings > Repository > Protected branches)
- ім'я, яке не потрапило в жоден клас, відхиляється. Тут такого немає: `work` з `match: "*"` ловить усе

## Пресети (`extends`)

Стандарт інсталяції зручно тримати в конфігурації сервера, а в проєктах підключати одним рядком:

```yaml
# /etc/gitlab-repopolicy/config.yaml
branchpolicy_presets:
  org-branches:
    rules:
      - name: release
        match: "release/*"
        pattern: '^release/v[0-9]+\.[0-9]+\.[0-9]+$'
        protected: true
      - name: work
        match: "*"
        pattern: '^[a-z0-9.]+/[a-z0-9._/-]+$'
```

```yaml
# BRANCHPOLICY проєкту
version: 1
extends: org-branches
```

Як збирається список правил:
1. Спершу нові правила проєкту, ім'я яких немає в пресеті, у порядку файлу
2. Потім правила пресету в його порядку. Правило проєкту з тим самим `name`, що в пресеті, стає на місце правила пресету

Так проєкт додає свої класи перед стандартом і перевизначає стандартний catch-all, а той лишається останнім. Наприклад, проєкт на `main`, де не можна створювати `master`:

```yaml
version: 1
extends: org-branches
rules:
  - name: no-master
    match: "master"
    deny: true
    message: This project uses main as the default branch; master must not be created.
```

Строгий проєкт: лише класи стандарту, усе інше заборонено. Правило з ім'ям `work` заміняє catch-all пресету:

```yaml
version: 1
extends: org-branches
rules:
  - name: work
    match: "*"
    deny: true
    message: In this project only release/* branches are created.
```

Міграція `master` → `main`: друга головна гілка дозволена явно, правило видаляють після міграції:

```yaml
version: 1
extends: org-branches
rules:
  - name: migration-main
    match: "main"
    protected: true
```

Проєкт без пресету пише правила повністю сам. Без catch-all усе, що не описано, заборонено.

### Заборонити все, що не описано

Правила `match: "*"` немає, тож ім'я без класу відхиляється з `branch_name_violation`. Головна гілка `main` - default branch, її ім'я перевіряє ядро за `default_branch`, тому окремого правила для неї не треба:

```yaml
version: 1
rules:
  - name: no-master
    match: "master"
    deny: true
    message: This project uses main; master must not be created.
  - name: release
    match: "release/*"
    pattern: '^release/v[0-9]+\.[0-9]+\.[0-9]+(-[a-z][a-z0-9]*)?$'
    protected: true
  - name: hotfix
    match: "hotfix/*"
    pattern: '^hotfix/v[0-9]+\.[0-9]+\.[0-9]+(-[a-z][a-z0-9]*)?$'
    protected: true
  - name: user
    match: "*/*"
    user_prefix: true
```

| Нова гілка | Результат |
| --- | --- |
| `master` | відмова: правило `no-master` |
| `release/v1.4.0` | проходить, якщо проєкт захищає `release/*` |
| `release/foo` | відмова: клас `release`, ім'я не за `pattern`. У клас `user` воно вже не переходить |
| `jdoe/disable_auth`, коли пушить `jdoe` | проходить: префікс - username того, хто пушить |
| `fix-login`, `tmp` | відмова: жоден `match` не збігся |

## Структура файлу

| Поле | Обов'язкове | Значення |
| --- | --- | --- |
| `version` | так | `1` |
| `extends` | ні | ім'я пресету з `branchpolicy_presets` конфігурації сервера ([configuration.md](configuration.md#branchpolicy_presets)) |
| `mode` | ні | `enforce` (за замовчуванням) або `warn`. `warn` робить з кожної відмови попередження |
| `rules` | ні | правила, разом із правилами пресету до 50 |

Поля правила:

| Поле | Обов'язкове | Значення |
| --- | --- | --- |
| `name` | так | `[a-z0-9-]{1,63}`, унікальне у файлі. Збіг з ім'ям правила пресету означає заміну |
| `match` | так | клас гілки, шаблон як у GitLab protected branches: `*` збігається з будь-якими символами, зокрема з `/`. До 255 байт |
| `deny` | ні | `true` - створювати гілки цього класу заборонено. Разом з `pattern`, `user_prefix`, `protected` чи `examples` - помилка |
| `pattern` | ні | RE2 regex, з яким має збігтися все ім'я гілки. Без `pattern` у класі допустиме будь-яке ім'я. До 256 байт |
| `user_prefix` | ні | `true` - частина імені до першого `/` - username активного користувача GitLab |
| `protected` | ні | `true` - нова гілка цього класу має бути protected branch проєкту в GitLab |
| `examples` | ні | лише з `pattern`: `{valid: [...], invalid: [...]}`. Самоперевірка regex під час приймання файлу і в `validate`, у відмові не друкується |
| `message` | ні | текст у відмові, до 1000 символів. Сюди пишуть приклад імені для людини, а для `protected` - які ролі ставити в Protected branches |

Невідомі ключі, YAML anchors і aliases - помилка. Розмір файлу - до 64 KiB.

Перевірки списку правил разом із пресетом, під час приймання нової версії і в `validate`:
- після правила з `match: "*"` правил немає: вони недосяжні
- `match` не повторюється
- кожен `examples.valid` проходить `pattern` свого правила і потрапляє саме в це правило. Так видно затінення: наприклад, проєкт додав `match: "*/*"` перед пресетом, і `release/v1.4.0` пішов у чуже правило
- кожен `examples.invalid` не проходить `pattern` свого правила

Без `examples` затінення ловлять лише перші дві перевірки.

## Що перевіряється

Лише створення гілки (`refs/heads/*`). Оновлення і видалення наявних гілок не перевіряються: вони живуть як є, навіть з ім'ям не за стандартом. Default branch не перевіряється: її ім'я звіряє ядро за `default_branch` ([configuration.md](configuration.md#default_branch)). Теги не перевіряються: захист релізних тегів - поле `protected` у TAGPOLICY ([tagpolicy.md](tagpolicy.md)).

Для нової гілки, по порядку:
1. Клас - перше правило, у якого `match` збігся з ім'ям. Немає такого - відмова `branch_name_violation`
2. `deny: true` - відмова `branch_name_violation`
3. `pattern` не збігся - відмова `branch_name_violation`
4. `user_prefix: true` - частина до першого `/` має бути username. Якщо гілку створює людина зі своїм username як префіксом, API не потрібен: GitLab щойно її автентифікував. Інакше hook один раз питає GitLab, чи є активний користувач з таким ім'ям, без урахування регістру. Немає - відмова `branch_name_violation`. Так можна створити гілку для колеги: `olga/fix` від імені `alice` проходить, якщо `olga` існує
5. `protected: true` - hook питає GitLab, чи захищає проєкт цю гілку. Ні - відмова `branch_not_protected`

Перевірка імені локальна, без API. API потрібен лише крокам 4 і 5 і лише для правил з цими полями.

## GitLab UI

Гілку, створену в UI чи API GitLab (кнопка New branch, гілка з issue, коміт у Web IDE в нову гілку, revert і cherry-pick з новим MR), перевіряють так само. Імена, які пропонує GitLab (`123-title`, `user-main-patch-48213`, `revert-1a2b3c4d`), стандарту зазвичай не відповідають. Перейменуйте гілку в діалозі або створіть гілку за стандартом заздалегідь і виберіть її як цільову.

## Режими і відмови

| Ситуація | Результат |
| --- | --- |
| `mode: enforce`, порушення | відмова з кодом порушення |
| `mode: warn`, порушення | push проходить, попередження у виводі й audit |
| API не відповів під час `user_prefix` чи `protected` | перевірку пропущено, попередження `authorization_backend_failure` або `limit_exceeded` |
| пресету з `extends` немає в конфігурації сервера або список правил не проходить перевірки | модуль пропускається з попередженням, лише для push, що створюють гілку |
| BRANCHPOLICY у default branch невалідна | модуль пропускається з попередженням, лише для push, що створюють гілку; audit `invalid_policy` з `decision: warn` |
| push змінює BRANCHPOLICY на невалідну версію | відмова `invalid_policy_update` |
| особистий namespace | не перевіряється |

Audit-подія `decision` пишеться лише за порушення чи попередження, інакше кожне створення гілки в сотнях проєктів залишало б запис.

| Код | Заголовок у виводі | Коли |
| --- | --- | --- |
| `branch_name_violation` | the name does not follow the branch standard, creating this branch is forbidden, no rule of BRANCHPOLICY allows this name | ім'я не за класом, клас заборонений або класу немає |
| `branch_not_protected` | the branch must be protected, but the project does not protect it | нова гілка класу з `protected: true`, а проєкт її не захищає |

```text
remote: gitlab-repopolicy: push rejected by the rules of this repository (1 problem)
remote:
remote: BRANCHPOLICY rule "release", branch release/foo: the name does not follow the branch standard
remote:   Release branches look like release/v1.4.0. Protect release/* with "Allowed to push and merge Maintainers".
remote:   how to fix: rename the branch locally (git branch -m) and push again
remote:
remote: rules: BRANCHPOLICY in main (9f1c2ab)
remote: checked as: alice (HTTPS)
remote: GL-HOOK-ERR: gitlab-repopolicy: branch release/foo breaks BRANCHPOLICY rule release; rename it (git branch -m) and push again
```

Сирий regex користувачу не показується: формат імені людина бачить з `message`. Якщо `message` немає, відмова все одно називає правило, гілку і причину.

## Як впровадити

1. Пресет у конфігурацію сервера; BRANCHPOLICY з `extends` і `mode: warn` у проєкти; path-правило PATHPOLICY на `BRANCHPOLICY`
2. Тиждень-два попереджень: подивіться в audit події `decision` модуля `branchpolicy`
3. `mode: enforce`

Підводні камені:
- `git push --mirror` legacy-репозиторію створює всі гілки разом і відхиляється цілком, якщо хоч одне ім'я не за стандартом. Додавайте BRANCHPOLICY після імпорту
- у проєкті без PATHPOLICY додавання BRANCHPOLICY дає попередження, що файл ніщо не захищає. Це очікувано
- `settings-check` для проєктів з BRANCHPOLICY перевіряє, що проєкт захищає шаблони правил з `protected: true` ([operations.md](operations.md#settings-check))

## Перевірка без push

```sh
gitlab-repopolicy validate -f BRANCHPOLICY
gitlab-repopolicy validate -f BRANCHPOLICY --config presets.yaml
```

`validate` виконує ті самі перевірки, що й приймання: схему, regex, порядок правил і `examples`. Для файлу з `extends` потрібен `--config` з `branchpolicy_presets`; інші ключі конфігурації не потрібні.
