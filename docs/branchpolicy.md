[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](branchpolicy.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](branchpolicy.en.md)

# BRANCHPOLICY

BRANCHPOLICY перевіряє ім'я кожної нової гілки і те, що гілки певних видів захищені в GitLab. Файл лежить у корені репозиторію з ім'ям `BRANCHPOLICY`, вміст - YAML.

Спочатку - що таке ім'я гілки і як його змінити. Далі - як BRANCHPOLICY це перевіряє.

## Ім'я гілки

Гілка - це рухоме ім'я для лінії комітів, кожен новий коміт у гілці пересуває її вперед. Ім'я гілки може містити `/`, і команди користуються цим, щоб ім'я одразу казало, що це за гілка:
- `release/v1.4.0` - реліз 1.4.0
- `hotfix/v1.4.1` - термінове виправлення релізу
- `kpn/disable_auth` - робоча гілка розробника `kpn`
- `gks/INFRA-1719/change_logic` - робоча гілка розробника `gks` за issue `INFRA-1719`

Частину до першого `/` називають префіксом. BRANCHPOLICY ділить гілки на класи за таким префіксом або за іншим шаблоном імені і для кожного класу задає свої вимоги.

### Як створити, перейменувати і запушити гілку

```sh
git switch -c kpn/disable_auth        # створити гілку локально і перейти на неї
git branch -m kpn/disable_auth         # перейменувати поточну гілку
git branch -m old-name kpn/disable_auth  # перейменувати іншу гілку
git push -u origin kpn/disable_auth    # створити гілку на сервері
```

BRANCHPOLICY перевіряє гілку лише тоді, коли вона з'являється на сервері, тобто під час першого `git push` з новим ім'ям. Локально гілку можна називати як завгодно.

### Що робити після відмови

Hook відхиляє push, тому гілки з неправильним ім'ям на сервері немає, вона є лише у вас. Перейменуйте її локально за підказкою з відмови і запуште ще раз:

```sh
git branch -m release/foo release/v1.4.0
git push -u origin release/v1.4.0
```

Видаляти на сервері нічого не треба. Видалити гілку з сервера (`git push origin --delete <ім'я>`) доводиться лише тоді, коли неправильно названа гілка вже була там до появи BRANCHPOLICY і її хочуть прибрати.

## Як BRANCHPOLICY це перевіряє

Файл складається з правил. Правило описує клас гілок. `match` каже, які імена потрапляють у клас, а інші поля - що від них вимагається. Нова гілка потрапляє в клас за першим правилом, у якого `match` збігся з ім'ям.

Навіщо це потрібно:
- захист. Гілки `release/*` або `epic/*` деплоять, а pipeline незахищеної гілки не бачить protected CI/CD variables (паролі, токени деплою) і protected runners. Гілку створили, CI нічого не задеплоїв, і причину шукають довго. Hook ловить це одразу, коли гілку створюють, і пише, що налаштувати
- стандарт імен. Домовленість команди стає правилом на сервері, а не усною традицією

BRANCHPOLICY стежить за порядком, а не за доступом. Тому, коли перевірку неможливо виконати (файл у default branch зламаний, API GitLab не відповів), гілка створюється з попередженням, а не блокується, як і в COMMITPOLICY. Хто може змінювати сам BRANCHPOLICY, задає правило PATHPOLICY ([pathpolicy.md](pathpolicy.md)). Без такого правила файл змінює будь-хто з правом push.

## Приклад

```yaml
# examples/branch-standard/BRANCHPOLICY
# A branch standard: release and hotfix branches are release/vX.Y.Z and
# hotfix/vX.Y.Z, epic and demo branches are created by any developer, and all
# four are protected; work branches are <prefix>/[<issue>/]<name>. Each family
# has an exact rule and a deny rule with a higher priority for the names that
# miss it. A name of no class is not checked. In an installation this list
# usually lives in the presets of the server config, and a project only
# extends it.
version: 1

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
    match: ['/^epic\/[a-z0-9][a-z0-9._-]*(\/[a-z0-9][a-z0-9._-]*)*$/']
    priority: 300
    protected: true
    message: Epic branches look like epic/payments. Protect epic/* in Protected branches.
  epic-bad:
    match: ['/^epic\//']
    priority: 310
    deny: true
    message: Epic branches look like epic/payments.
  demo:
    match: ['/^demo\/[a-z0-9][a-z0-9._-]*(\/[a-z0-9][a-z0-9._-]*)*$/']
    priority: 400
    protected: true
    message: Demo branches look like demo/client-a. Protect demo/* in Protected branches.
  demo-bad:
    match: ['/^demo\//']
    priority: 410
    deny: true
    message: Demo branches look like demo/client-a.
  work:
    match: ['/^[a-z0-9][a-z0-9._-]*\/(([0-9]+|[A-Z][A-Z0-9]+-[0-9]+)(\/[a-z0-9][a-z0-9._-]*)*|[a-z0-9][a-z0-9._-]*(\/[a-z0-9][a-z0-9._-]*)*)$/']
    priority: 900
    message: Work branches are <prefix>/[<issue>/]<name>, for example kpn/disable_auth.
```

Як читається:
- `release/v1.4.0` потрапляє в клас `release`, бо `match: "release/*"` збігся. Ім'я відповідає `pattern`, тож гілка проходить, якщо проєкт захищає `release/*` (Settings > Repository > Protected branches)
- `release/foo` теж потрапляє в клас `release` і там відхиляється, бо не відповідає `pattern`. У клас `work` вона вже не переходить, клас визначає лише перше правило, що збіглося
- `kpn/disable_auth` і `gks/INFRA-1719/change_logic` не збігаються з `release/*`, `hotfix/*`, `epic/*`, `demo/*` і потрапляють у `work`, де проходять `pattern`
- `123-fix-login` теж потрапляє в `work`, але не проходить `pattern`, бо в ньому немає префікса з `/`
- ім'я, яке не потрапило в жоден клас, відхиляється. Тут такого немає, `work` з `match: "*"` ловить усе

`*` у `match` збігається з будь-якими символами, зокрема з `/`, як у protected branches GitLab. `release/*` охоплює і `release/v1.4.0`, і `release/v1.4/rc1`, а `*` - будь-яке ім'я. У полі `refs` COMMITPOLICY `*` поводиться інакше і на `/` зупиняється ([commitpolicy.md](commitpolicy.md#для-яких-гілок-діє-ruleset-refs)).

`pattern` - regex (синтаксис RE2), з яким має збігтися все ім'я гілки. Сирий regex людині у відмові не показується, вона бачить текст `message`, тому туди пишуть приклад правильного імені.

## Пресети (`extends`)

Пресет - іменований набір правил у конфігурації сервера. Стандарт імен, спільний для багатьох проєктів, зручно тримати там, а в проєктах підключати одним рядком:

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

Так проєкт додає свої класи перед стандартом і може перевизначити останнє правило стандарту, яке ловить усе (`match: "*"`), а воно лишається останнім. Наприклад, проєкт на `main`, де не можна створювати `master`:

```yaml
version: 1
extends: org-branches
rules:
  - name: no-master
    match: "master"
    deny: true
    message: This project uses main as the default branch; master must not be created.
```

Строгий проєкт, де дозволені лише класи стандарту, а все інше заборонено. Правило з ім'ям `work` заміняє правило пресету `work` з `match: "*"`:

```yaml
version: 1
extends: org-branches
rules:
  - name: work
    match: "*"
    deny: true
    message: In this project only release/* branches are created.
```

Перехід з `master` на `main`. Нову головну гілку дозволено явно, а правило видаляють після переходу:

```yaml
version: 1
extends: org-branches
rules:
  - name: migration-main
    match: "main"
    protected: true
```

Проєкт без пресету пише правила повністю сам. Якщо немає правила з `match: "*"`, усе, що не описано, заборонено.

### Заборонити все, що не описано

Правила `match: "*"` немає, тож ім'я без класу відхиляється з `branch_name_violation`. Головна гілка `main` - default branch проєкту, її ім'я hook звіряє з `default_branch` конфігурації сервера, тому окремого правила для неї не треба:

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
| `master` | відмова, правило `no-master` |
| `release/v1.4.0` | проходить, якщо проєкт захищає `release/*` |
| `release/foo` | відмова, клас `release`, ім'я не за `pattern`. У клас `user` воно вже не переходить |
| `jdoe/disable_auth`, коли пушить `jdoe` | проходить, префікс - username того, хто пушить |
| `fix-login`, `tmp` | відмова, жоден `match` не збігся (`*/*` вимагає `/`) |

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
| `match` | так | які імена потрапляють у клас. Шаблон як у protected branches GitLab, `*` збігається з будь-якими символами, зокрема з `/`. До 255 байт |
| `deny` | ні | `true` - створювати гілки цього класу заборонено. Разом з `pattern`, `user_prefix`, `protected` чи `examples` - помилка |
| `pattern` | ні | regex (синтаксис RE2), з яким має збігтися все ім'я гілки. Без `pattern` у класі допустиме будь-яке ім'я. До 256 байт |
| `user_prefix` | ні | `true` - частина імені до першого `/` - username активного користувача GitLab |
| `protected` | ні | `true` - нову гілку цього класу проєкт має захищати в GitLab (Settings > Repository > Protected branches) |
| `examples` | ні | лише з `pattern`, `{valid: [...], invalid: [...]}`. Імена, на яких hook перевіряє сам regex під час приймання файлу і в `validate`. У відмові не друкуються |
| `message` | ні | текст у відмові, до 1000 символів. Сюди пишуть приклад імені для людини, а для `protected` - які ролі ставити в Protected branches |

Невідомі ключі, YAML anchors і aliases - помилка. Розмір файлу - до 64 KiB.

Перевірки списку правил разом із пресетом, під час приймання нової версії і в `validate`:
- після правила з `match: "*"` правил немає: вони недосяжні
- `match` не повторюється
- кожен `examples.valid` проходить `pattern` свого правила і потрапляє саме в це правило. Так видно, коли правило вище перехоплює чужі імена. Наприклад, проєкт додав `match: "*/*"` перед пресетом, і `release/v1.4.0` пішов у чуже правило
- кожен `examples.invalid` не проходить `pattern` свого правила

Без `examples` таке перехоплення ловлять лише перші дві перевірки.

## Що перевіряється

Лише створення гілки. Новий коміт у наявну гілку і її видалення не перевіряються, наявні гілки живуть як є, навіть з ім'ям не за стандартом. Default branch не перевіряється, її ім'я hook звіряє з `default_branch` ([configuration.md](configuration.md#default_branch)). Теги не перевіряються, захист релізних тегів задає поле `protected` у TAGPOLICY ([tagpolicy.md](tagpolicy.md)).

Для нової гілки, по порядку:
1. Клас - перше правило, у якого `match` збігся з ім'ям. Немає такого - відмова `branch_name_violation`
2. `deny: true` - відмова `branch_name_violation`
3. `pattern` не збігся - відмова `branch_name_violation`
4. `user_prefix: true` - частина до першого `/` має бути username. Якщо гілку створює людина зі своїм username як префіксом, API не потрібен, бо GitLab щойно її автентифікував. Інакше hook один раз питає GitLab, чи є активний користувач з таким ім'ям, без урахування регістру. Немає - відмова `branch_name_violation`. Так можна створити гілку для колеги. `olga/fix` від імені `alice` проходить, якщо `olga` існує
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
| BRANCHPOLICY у default branch невалідна | модуль пропускається з попередженням, лише для push, що створюють гілку. Подія audit `invalid_branch_policy` |
| push змінює BRANCHPOLICY на невалідну версію | відмова `invalid_policy_update` |
| особистий namespace | не перевіряється |

Audit-подія `decision` пишеться лише за порушення чи попередження, інакше кожне створення гілки в сотнях проєктів залишало б запис.

| Код | Заголовок у виводі | Коли |
| --- | --- | --- |
| `branch_name_violation` | the name does not follow the branch standard | ім'я не відповідає `pattern` або `user_prefix` свого класу |
| `branch_name_violation` | creating this branch is forbidden | клас з `deny: true` |
| `branch_name_violation` | no rule of BRANCHPOLICY allows this name | ім'я не потрапило в жоден клас |
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

Як це читати:
- заголовок - яке правило, яка гілка і в чому проблема
- далі текст `message` правила і що робити (`how to fix`)
- `rules` - з якого коміту default branch hook прочитав BRANCHPOLICY
- `checked as` - хто робив push і як (HTTPS, SSH, UI GitLab)
- `GL-HOOK-ERR` - короткий рядок для UI GitLab

Якщо `message` немає, відмова все одно називає правило, гілку і причину. Для імені без класу відмова така:

```text
remote: BRANCHPOLICY, branch fix-login: no rule of BRANCHPOLICY allows this name
remote:   how to fix: rename the branch locally (git branch -m) to a name the rules of this project allow, and push again
```

## Як впровадити

1. Пресет у конфігурацію сервера, BRANCHPOLICY з `extends` і `mode: warn` у проєкти, правило PATHPOLICY на файл `BRANCHPOLICY`
2. Тиждень-два попереджень. Подивіться в audit події `decision` модуля `branchpolicy`, які імена порушують стандарт
3. `mode: enforce`

Підводні камені:
- `git push --mirror` старого репозиторію створює всі гілки разом і відхиляється цілком, якщо хоч одне ім'я не за стандартом. Додавайте BRANCHPOLICY після імпорту
- у проєкті без PATHPOLICY додавання BRANCHPOLICY дає попередження, що файл ніщо не захищає. Це очікувано
- `settings-check` для проєктів з BRANCHPOLICY перевіряє, що проєкт захищає шаблони правил з `protected: true` ([operations.md](operations.md#settings-check))

## Перевірка без push

```sh
gitlab-repopolicy validate -f BRANCHPOLICY
gitlab-repopolicy validate -f BRANCHPOLICY --config presets.yaml
```

`validate` виконує ті самі перевірки, що й приймання нової версії файлу, тобто схему, regex, порядок правил і `examples`. Для файлу з `extends` потрібен `--config` з `branchpolicy_presets`, інші ключі конфігурації не потрібні.
