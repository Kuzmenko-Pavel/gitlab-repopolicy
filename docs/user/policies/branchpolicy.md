[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](branchpolicy.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](branchpolicy.en.md)

# BRANCHPOLICY

## Коротко

BRANCHPOLICY перевіряє ім'я кожної нової гілки і те, що гілки певних класів захищені в GitLab. Так домовленість команди про імена стає правилом на сервері, а release-гілка, яку забули захистити, не залишається без protected CI/CD variables і runners. Файл лежить у корені репозиторію з ім'ям `BRANCHPOLICY`, вміст - YAML. Найменший робочий файл:

```yaml
version: 1
rules:
  no-master:
    match: [master]
    priority: 10
    deny: true
    message: This project uses main; master must not be created.
```

Тут заборонено створювати гілку `master`. Усі інші імена не перевіряються.

## Швидкий старт

1. Опишіть класи гілок правилами з `priority` і перевірте файл: `gitlab-repopolicy validate --branch <ім'я>` показує, яке правило спрацює для імені ([нижче](#перевірка-без-push))
2. Почніть з `mode: warn`: порушення лише показуються і пишуться в audit
3. Злийте файл у default branch. Перевіряються лише нові гілки, наявні залишаються як є
4. Коли попереджень майже немає, приберіть `mode: warn`

Хто може змінювати сам BRANCHPOLICY, задає правило PATHPOLICY ([pathpolicy.md](../examples/pathpolicy/delegation.md)).

## Ім'я гілки

Ім'я гілки може містити `/`, і команди користуються цим, щоб ім'я одразу казало, що це за гілка:
- `release/v1.4.0` - реліз 1.4.0
- `hotfix/v1.4.1` - термінове виправлення релізу
- `kpn/disable_auth` - робоча гілка розробника `kpn`
- `kpn/PAY-12/refunds` - робоча гілка `kpn` за задачею `PAY-12`

BRANCHPOLICY перевіряє гілку, коли вона з'являється на сервері: під час першого `git push` з новим ім'ям або створення в UI GitLab. Локально гілку можна називати як завгодно.

```sh
git switch -c kpn/disable_auth            # створити гілку і перейти на неї
git branch -m old-name kpn/disable_auth   # перейменувати гілку
git push -u origin kpn/disable_auth       # створити гілку на сервері
```

Після відмови гілки на сервері немає, вона лише у вас. Перейменуйте її за підказкою з відмови і запуште ще раз, видаляти на сервері нічого не треба.

## Довідник полів

| Поле | Обов'язкове | Значення |
| --- | --- | --- |
| `version` | так | `1` |
| `mode` | ні | `enforce` (за замовчуванням) або `warn`. `warn` робить з кожної відмови попередження |
| `extends` | ні | ім'я пресета серверного конфігу або список імен ([configuration.md](../administration/configuration.md#presets)) |
| `rules` | ні | правила, до 50: ім'я правила → правило |
| `rules.<ім'я>.match` | так | імена гілок класу ([match.md](match.md)). `default` тут немає |
| `rules.<ім'я>.priority` | так | порядок правил, ціле від 0 до 1 000 000. Нова гілка потрапляє в правило з найменшим `priority`, чий `match` збігся. Унікальне після злиття з пресетами |
| `rules.<ім'я>.deny` | ні | `true` забороняє створення гілки; `false` (за замовчуванням) не додає цієї заборони. З `deny: true` поля `user_prefix` і `protected` не задають, навіть зі значенням `false` |
| `rules.<ім'я>.user_prefix` | ні | `true` вимагає username активного користувача GitLab до першого `/`; `false` (за замовчуванням) вимикає перевірку префікса |
| `rules.<ім'я>.protected` | ні | `true` вимагає захисту нової гілки в GitLab (Settings > Repository > Protected branches); `false` (за замовчуванням) не вимагає цього статусу |
| `rules.<ім'я>.message` | так | текст у відмові, до 1000 символів. Приклад правильного імені, а для `protected` - які ролі ставити |
| `rules.<ім'я>.help` | ні | `contact` і `docs_url`, як у [PATHPOLICY](pathpolicy.md#довідник-полів) |

Значення за замовчуванням наведено для правила після злиття з пресетами. Пропущений у файлі параметр успадковується; явний `false` вимикає відповідну вимогу пресета. `protected: false` не змінює налаштувань захисту гілки в GitLab.

Перевірки списку правил після злиття з пресетами, під час приймання нової версії і в `validate`:
- `priority` не повторюється: інакше порядок правил був би випадковим
- після правила з `match: all` правил немає: вони ніколи не спрацюють
- два правила з однаковим `match` (як множини) - помилка: друге ніколи не спрацює

Невідомі ключі, YAML anchors і aliases - помилка. Розмір файлу - до 64 KiB.

## Що перевіряється

Лише створення гілки. Новий коміт у наявну гілку і її видалення не перевіряються. Default branch не перевіряється, її ім'я hook звіряє з `default_branch` серверного конфігу ([configuration.md](../administration/configuration.md#default_branch)). Теги не перевіряються.

Для нової гілки, по порядку:
1. Правило - те, у якого найменший `priority` серед правил, чий `match` збігся з ім'ям. Не збіглося жодне - ім'я не перевіряється
2. `deny: true` - відмова `branch_name_violation`
3. `user_prefix: true` - частина до першого `/` має бути username. Якщо гілку створює людина зі своїм username як префіксом, API не потрібен. Інакше hook один раз питає GitLab, чи є активний користувач з таким ім'ям. Немає - відмова `branch_name_violation`. Гілку для колеги створити можна: `bob/fix` від імені alice проходить, якщо bob існує
4. `protected: true` - hook питає GitLab, чи захищає проєкт цю гілку. Ні - відмова `branch_not_protected`

Ім'я без класу не перевіряється навмисно: стандарт описує лише ті класи, які важливі. Щоб заборонити все, що не описано, додайте останнім правило з `match: all` і `deny: true` ([приклад 3](../examples/branchpolicy/strict-names.md)).

Гілку, створену в UI чи API GitLab (New branch, гілка з issue, Web IDE, revert і cherry-pick з новим MR), перевіряють так само. Імена, які пропонує GitLab (`123-title`, `user-main-patch-48213`), стандарту зазвичай не відповідають: перейменуйте гілку в діалозі.

## Приклади

| Приклад | Що пояснює |
| --- | --- |
| [Стандарт організації](../examples/branchpolicy/organization-standard.md) | Повний пресет і поведінка різних класів гілок |
| [Заборона родини](../examples/branchpolicy/deny-family.md) | Точний release-формат і `deny` для неправильних імен |
| [Строгий набір імен](../examples/branchpolicy/strict-names.md) | Останнє правило забороняє все неописане |
| [Зміни поверх пресета](../examples/branchpolicy/project-overrides.md) | `null`, перевизначення полів і перехід у наступний клас |
| [Префікс username](../examples/branchpolicy/user-prefix.md) | Власна гілка, гілка колеги й неіснуючий користувач |
| [Protected release-гілки](../examples/branchpolicy/protected-branches.md) | Захист до створення та подальша перевірка settings-check |
| [Виняток і заборона](../examples/branchpolicy/exception-vs-deny.md) | Чому `!` у match не замінює `deny` |

## Відмови

| Ситуація | Результат |
| --- | --- |
| `mode: enforce`, порушення | відмова з кодом порушення |
| `mode: warn`, порушення | push проходить, попередження у виводі й audit |
| API не відповів під час `user_prefix` чи `protected` | перевірку пропущено, попередження |
| пресета з `extends` немає в серверному конфігу, він зламаний, або правила після злиття не проходять перевірки | модуль пропускається з попередженням, лише для push, що створюють гілку |
| BRANCHPOLICY у default branch невалідна | так само, audit-подія `invalid_branch_policy` |
| push змінює BRANCHPOLICY на невалідну версію | відмова `invalid_policy_update` |

BRANCHPOLICY стежить за порядком, а не за доступом. Тому, коли перевірку неможливо виконати, гілка створюється з попередженням.

| Код | Заголовок у виводі | Коли |
| --- | --- | --- |
| `branch_name_violation` | creating this branch is forbidden | правило з `deny: true` |
| `branch_name_violation` | the name does not follow the branch standard | префікс не є username активного користувача |
| `branch_not_protected` | the branch must be protected, but the project does not protect it | нова гілка правила з `protected: true`, а проєкт її не захищає |

Відмова для `master` повністю:

```text
remote: gitlab-repopolicy: push rejected by the rules of this repository (1 problem)
remote:
remote: BRANCHPOLICY rule "no-master", branch master: creating this branch is forbidden
remote:   This project uses main; master must not be created.
remote:
remote: rules: BRANCHPOLICY in main (72ad41f)
remote: checked as: alice (SSH)
remote: GL-HOOK-ERR: gitlab-repopolicy: branch master is forbidden by BRANCHPOLICY rule no-master
```

- заголовок - правило, гілка і в чому проблема
- далі `message` правила і, якщо є, що робити (`how to fix`)
- `rules` - з якого коміту default branch hook прочитав BRANCHPOLICY
- `checked as` - хто робив push і як
- `GL-HOOK-ERR` - короткий рядок для UI GitLab

## Часті помилки

| Симптом | Причина | Що зробити |
| --- | --- | --- |
| `priority 100 is also the priority of rule ...` у `validate` | однаковий `priority` у двох правилах, часто після злиття з пресетом | змініть `priority` правила у файлі |
| `comes after rule other with match: all and is never used` | правило з `match: all` не останнє | дайте правилу з `match: all` найбільший `priority` |
| `release/foo` створилася без перевірки | немає `deny` для родини `release/` | додайте правило `release-bad` ([приклад 2](../examples/branchpolicy/deny-family.md)) |
| `tmp/x` створилася, хоча `!/^tmp\//` є в `match` | виняток лише прибирає ім'я з класу | окреме правило з `deny` ([приклад 7](../examples/branchpolicy/exception-vs-deny.md)) |
| `git push --mirror` старого репозиторію відхиляється цілком | одна гілка з неправильним ім'ям відхиляє весь push | додавайте BRANCHPOLICY після імпорту |
| після `demo: null` гілки `demo/...` вимагають користувача `demo` | ім'я переходить у наступне правило, `work` | перевірте імена командою `validate --branch` |

## Перевірка без push

```sh
gitlab-repopolicy validate -f BRANCHPOLICY
gitlab-repopolicy validate -f BRANCHPOLICY --config presets.yaml --branch release/v1.4.0
gitlab-repopolicy validate -f BRANCHPOLICY --config config.yaml --online --branch kpn/disable_auth
```

`validate` перевіряє валідність файлу. `--branch` додатково показує, яке правило спрацює для імені і з яким результатом. Для валідного файлу інформаційний запит повертає код 0 навіть за відповіді `denied`. З `--online` можна перевірити користувача з префікса `user_prefix` через API GitLab. [Посібник validate](../reference/validate.md) пояснює ці режими та їхні обмеження; [довідник CLI](../reference/cli.md) містить повний перелік параметрів.
