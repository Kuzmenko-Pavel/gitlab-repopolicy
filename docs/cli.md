[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](cli.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](cli.en.md)

# Довідник CLI

Усі команди і прапорці `gitlab-repopolicy`. Покрокові сценарії - в описах, на які посилається кожен розділ.

## Загальне

```text
gitlab-repopolicy <command> [flags]
```

Без аргументів команда друкує список команд. `gitlab-repopolicy <command> -h` друкує прапорці команди. Прапорці пишуться з одним або двома дефісами: `-f` і `--f` однакові.

| Код виходу | Що означає |
| --- | --- |
| `0` | успіх: файли валідні, push було б прийнято, розбіжностей немає |
| `1` | результат перевірки негативний (невалідний файл, відмова, розбіжність) або помилка виконання |
| `2` | неправильні аргументи команди |

`--config` - серверний конфіг. За замовчуванням `/etc/gitlab-repopolicy/config.yaml` у всіх команд, крім `validate` і `lint-commit`: у них конфігу за замовчуванням немає, бо вони працюють без сервера. Опис конфігу - [configuration.md](configuration.md).

## Команди

| Команда | Що робить | Від кого | API і токен | Серверний конфіг | Розділ |
| --- | --- | --- | --- | --- | --- |
| `validate` | перевіряє файли політик | будь-хто | лише з `--online` | лише для пресетів і `--online` | [validate](#validate) |
| `lint-commit` | перевіряє повідомлення комітів | будь-хто | ні | лише для пресетів | [lint-commit](#lint-commit) |
| `explain` | які правила PATHPOLICY діють на шлях | git або root | так | так | [explain](#explain) |
| `admins` | admins PATHPOLICY і recovery | git або root | так | так | [admins](#admins) |
| `test` | рішення hook без push | git або root | якщо правилам потрібен | так | [test](#test) |
| `selftest` | перевіряє встановлення | root і git | так | так | [selftest](#selftest) |
| `settings-check` | звіряє налаштування проєктів | git або root | так | так | [settings-check](#settings-check) |
| `coverage` | групи без service account | root | токен адміністратора | так | [coverage](#coverage) |
| `install-hook` | підключає hook до Gitaly | root | ні | так | [install-hook](#install-hook) |
| `uninstall-hook` | відключає hook | root | ні | ні | [uninstall-hook](#uninstall-hook) |
| `token rotate` | ротує токен service account | root | так | так | [token rotate](#token-rotate) |
| `breakglass grant`, `revoke`, `list` | аварійний обхід для проєкту | root | лише для `--project` | так | [breakglass](#breakglass-grant) |
| `hook pre-receive` | сам hook | Gitaly | так | так | [hook pre-receive](#hook-pre-receive) |
| `version` | версія | будь-хто | ні | ні | [version](#version) |

## Перевірка файлів політик

### `validate`

```text
gitlab-repopolicy validate [-f FILE [--module NAME]] [--config FILE] [--online [--project P]] [--branch NAME] [--tag NAME]
```

| Прапорець | Обов'язковий | За замовчуванням | Що робить |
| --- | --- | --- | --- |
| `-f FILE` | ні | усі файли політик поточного каталогу | файл для перевірки |
| `--module NAME` | для `-f` з іншим ім'ям | за ім'ям файлу | модуль файлу: `pathpolicy`, `commitpolicy`, `tagpolicy`, `branchpolicy` |
| `--config FILE` | для `extends` і `--online` | немає | серверний конфіг: пресети, з `--online` ще `gitlab_url` і `token_file` |
| `--online` | ні | вимкнено | перевіряє через API GitLab, що username існують і активні |
| `--project P` | ні | немає | з `--online`: ID або шлях проєкту, щоб перевірити роль admins |
| `--branch NAME` | ні | немає | правило BRANCHPOLICY для нової гілки з цим ім'ям і результат |
| `--tag NAME` | ні | немає | правила TAGPOLICY для тегу з цим ім'ям і підписанти |

- `--online` без `--config` - код 2
- `--branch` потребує файл BRANCHPOLICY, `--tag` - TAGPOLICY: через `-f` або в поточному каталозі. Немає - код 2
- код 0 за будь-якої відповіді `--branch` і `--tag`: це запит, а не перевірка

Вивід у stdout: `<файл>: valid` або `invalid`, далі рядки `error:`, `warning:`, `not checked:`, походження правил для файлу з `extends`, відповідь `--branch` і `--tag`. Помилки використання - у stderr.

```text
$ gitlab-repopolicy validate
PATHPOLICY: valid
COMMITPOLICY: valid
TAGPOLICY: valid
BRANCHPOLICY: valid
```

Покроково - [validate.md](validate.md).

### `lint-commit`

```text
gitlab-repopolicy lint-commit (-m MSG | --range A..B [--skip-author EMAIL]...) [-f FILE] [--rule NAME]... [--config FILE]
```

| Прапорець | Обов'язковий | За замовчуванням | Що робить |
| --- | --- | --- | --- |
| `-m MSG` | `-m` або `--range` | немає | повідомлення коміту |
| `--range A..B` | `-m` або `--range` | немає | коміти діапазону. Одна ревізія - уся її історія. До 100 000 комітів |
| `--skip-author EMAIL` | ні | немає | з `--range`: пропустити коміти цього автора, можна повторити |
| `-f FILE` | ні | `COMMITPOLICY` | файл COMMITPOLICY |
| `--rule NAME` | ні | усі правила | лише це правило, можна повторити |
| `--config FILE` | для `extends` | немає | серверний конфіг з пресетами. Інші ключі не читаються |

- рівно один з `-m` і `--range`, інакше код 2
- `--skip-author` без `--range` - код 2
- невідоме ім'я в `--rule` - код 1 зі списком правил файлу

Код 0 - порушень рівня `error` немає, 1 - є. Порушення - у stdout рядком на кожне: `<коміт>: [<правило>] error|warning: <перевірка>: <що не так>`.

```text
$ gitlab-repopolicy lint-commit -f examples/conventional-commits/COMMITPOLICY -m "Fix deploy."
message: [conventional] error: header: expected "type(scope): subject"
message: [conventional] error: trailer-required: missing Signed-off-by in the trailer block
message: [release] error: header: expected "type(scope): subject"
```

Детальніше - [commitpolicy.md](commitpolicy.md#перевірка-без-push).

## Діагностика

### `explain`

```text
gitlab-repopolicy explain --project P --user U --path X [--ref R] [--config FILE]
```

| Прапорець | Обов'язковий | За замовчуванням | Що робить |
| --- | --- | --- | --- |
| `--project P` | так | немає | ID або повний шлях проєкту |
| `--user U` | так | немає | username |
| `--path X` | так | немає | шлях у репозиторії |
| `--ref R` | ні | default branch | ім'я гілки або повне ім'я ref |
| `--config FILE` | ні | `/etc/gitlab-repopolicy/config.yaml` | серверний конфіг |

Читає PATHPOLICY з default branch проєкту через API. Код 0 - зміну дозволено або жодне правило не збіглося, 1 - заборонено або помилка (у stderr), 2 - не задано `--project`, `--user` чи `--path`.

```text
$ gitlab-repopolicy explain --project acme/platform/app --user bob --path .gitlab-ci.yml --ref feature
acme/platform/app, PATHPOLICY of main; path .gitlab-ci.yml, ref refs/heads/feature, user bob
  ci-config (match: all; from preset:org-ci, file): NOT allowed; allow: platform-lead, sre-lead, kpn
    fields from: allow file; help preset:org-ci; help.contact preset:org-ci; match preset:org-ci; message preset:org-ci; paths preset:org-ci
result: bob may not change .gitlab-ci.yml on refs/heads/feature: every applying rule must allow the user
```

Детальніше - [pathpolicy.md](pathpolicy.md#перевірка-без-push), [operations.md](operations.md#recovery).

### `admins`

```text
gitlab-repopolicy admins --project P [--config FILE]
```

| Прапорець | Обов'язковий | За замовчуванням | Що робить |
| --- | --- | --- | --- |
| `--project P` | так | немає | ID або повний шлях проєкту |
| `--config FILE` | ні | `/etc/gitlab-repopolicy/config.yaml` | серверний конфіг |

Показує admins із PATHPOLICY default branch, робочих admins і чи ввімкнено recovery. Збій API - код 1, а не "0 admins".

```text
$ sudo gitlab-repopolicy admins --project acme/app
  alice                          missing: access level 30, Maintainer (40) is required
acme/app: configured 1, effective 0, missing 1
recovery active: an Owner of the project may change PATHPOLICY
```

Детальніше - [operations.md](operations.md#recovery).

### `test`

```text
gitlab-repopolicy test --repo DIR --old A --new B --ref R --user U [--gl-id ID] [--protocol P] [--project P] [--project-id N] [--config FILE]
```

| Прапорець | Обов'язковий | За замовчуванням | Що робить |
| --- | --- | --- | --- |
| `--repo DIR` | так | немає | репозиторій з об'єктами оновлення, краще mirror-копія |
| `--old A` | так | немає | старе значення ref, нулі - створення |
| `--new B` | так | немає | нове значення ref, нулі - видалення |
| `--ref R` | так | немає | повне ім'я ref, наприклад `refs/tags/v1.0.0` |
| `--user U` | так | немає | username того, хто пушить |
| `--gl-id ID` | без API | шукається за `--user` | `GL_ID`: `user-<id>` або `key-<id>` |
| `--protocol P` | ні | `ssh` | `ssh`, `http` або `web` |
| `--project P` | ні | `local/test/repository` | шлях проєкту |
| `--project-id N` | ні | `1` | ID проєкту |
| `--config FILE` | ні | `/etc/gitlab-repopolicy/config.yaml` | серверний конфіг |

Код 0 - push було б прийнято, з підсумком у stdout, наприклад `allowed: the rules allow the change, 3 new commits`. 1 - відмова з тим самим текстом, що побачив би користувач, або помилка. Детальніше - [operations.md](operations.md#перевірити-push-без-push).

### `selftest`

```text
gitlab-repopolicy selftest [--config FILE]
```

| Прапорець | Обов'язковий | За замовчуванням | Що робить |
| --- | --- | --- | --- |
| `--config FILE` | ні | `/etc/gitlab-repopolicy/config.yaml` | серверний конфіг |

Перевіряє конфіг, пресети, модулі, audit-лог, break-glass, токен, service account і доступ до API. Рядок на перевірку: `ok`, `info`, `warn` або `FAIL`. Будь-який `FAIL` - код 1. Запускайте від root і від git.

```text
$ sudo -u git gitlab-repopolicy selftest
info version     gitlab-repopolicy 1.0.0
ok   config      /etc/gitlab-repopolicy/config.yaml, mode enforce
ok   modules     enabled: pathpolicy, commitpolicy, tagpolicy, branchpolicy
ok   audit       /var/log/gitlab-repopolicy/audit.log does not exist yet; the directory belongs to 998:998
ok   breakglass  /etc/gitlab-repopolicy/breakglass, no active grants
ok   api         http+unix://%2Fvar%2Fopt%2Fgitlab%2Fgitlab-workhorse%2Fsockets%2Fsocket reachable as gitlab-repopolicy (id 2)
ok   account     gitlab-repopolicy is a service account
ok   groups      Maintainer of 3 groups; gitlab-repopolicy coverage checks every top-level group
ok   token       scopes read_api, self_rotate, expires on 2027-10-13
info git         git version 2.55.0-rc1.g02bb39c in PATH of selftest; the hook uses the git of Gitaly
```

Детальніше - [install.md](install.md#6-перевірити).

### `settings-check`

```text
gitlab-repopolicy settings-check [--config FILE] [--timeout D]
```

| Прапорець | Обов'язковий | За замовчуванням | Що робить |
| --- | --- | --- | --- |
| `--config FILE` | ні | `/etc/gitlab-repopolicy/config.yaml` | серверний конфіг |
| `--timeout D` | ні | `1h0m0s` | граничний час усієї перевірки |

Звіряє налаштування проєктів з файлами політик із серверним конфігом і з BRANCHPOLICY. Розбіжність - рядок `<проєкт>: <перевірка>: <що не так>` і код 1. Без розбіжностей - `N projects with policy files checked, no drift`, код 0.

```text
$ sudo -u git gitlab-repopolicy settings-check
acme/app: protected_tags: not protected: v*
```

Детальніше - [operations.md](operations.md#settings-check).

### `coverage`

```text
gitlab-repopolicy coverage --token-file ADMIN_TOKEN [--config FILE] [--timeout D]
```

| Прапорець | Обов'язковий | За замовчуванням | Що робить |
| --- | --- | --- | --- |
| `--token-file ADMIN_TOKEN` | так | немає | файл з токеном адміністратора, scope `read_api` |
| `--config FILE` | ні | `/etc/gitlab-repopolicy/config.yaml` | серверний конфіг: `gitlab_url` і токен service account |
| `--timeout D` | ні | `10m0s` | граничний час усієї перевірки |

Знаходить top-level групи, де service account не має ролі Maintainer: проєкти в них hook перевірити не зможе. Код 1, якщо такі групи є. Детальніше - [install.md](install.md#3-service-account-і-токен).

## Встановлення й експлуатація

### `install-hook`

```text
gitlab-repopolicy install-hook --hooks-dir DIR [--user git] [--binary PATH] [--config FILE]
```

| Прапорець | Обов'язковий | За замовчуванням | Що робить |
| --- | --- | --- | --- |
| `--hooks-dir DIR` | так | немає | `custom_hooks_dir` Gitaly |
| `--user U` | ні | `git` | користувач, від якого Gitaly запускає hooks |
| `--binary PATH` | ні | `/usr/bin/gitlab-repopolicy` | бінарник, який запускає wrapper |
| `--config FILE` | ні | `/etc/gitlab-repopolicy/config.yaml` | серверний конфіг, який передає wrapper |

Створює wrapper `pre-receive.d/50-gitlab-repopolicy` і виставляє власників конфігу, токена і каталогу логу. Детальніше - [install.md](install.md#5-підключити-hook).

### `uninstall-hook`

```text
gitlab-repopolicy uninstall-hook --hooks-dir DIR
```

| Прапорець | Обов'язковий | За замовчуванням | Що робить |
| --- | --- | --- | --- |
| `--hooks-dir DIR` | так | немає | `custom_hooks_dir` Gitaly |

Видаляє wrapper. Детальніше - [install.md](install.md#вимкнути-і-видалити).

### `token rotate`

```text
gitlab-repopolicy token rotate [--before 30d] [--config FILE]
```

| Прапорець | Обов'язковий | За замовчуванням | Що робить |
| --- | --- | --- | --- |
| `--before D` | ні | `30d` | ротувати, коли до кінця строку токена менше цього часу |
| `--config FILE` | ні | `/etc/gitlab-repopolicy/config.yaml` | серверний конфіг |

Якщо до кінця строку більше `--before`, нічого не робить. Інакше ротує токен з `expires_at` = сьогодні + `token_lifetime` і атомарно записує новий. Детальніше - [operations.md](operations.md#ротація-токена).

### `breakglass grant`

```text
gitlab-repopolicy breakglass grant (--project P | --project-id N) --ttl D --reason TEXT [--config FILE]
```

| Прапорець | Обов'язковий | За замовчуванням | Що робить |
| --- | --- | --- | --- |
| `--project P` | `--project` або `--project-id` | немає | повний шлях проєкту, ID шукається через API |
| `--project-id N` | `--project` або `--project-id` | немає | ID проєкту, коли GitLab не відповідає |
| `--ttl D` | так | немає | як довго діє дозвіл, не більше 24h |
| `--reason TEXT` | так | немає | чому, наприклад номер інциденту |
| `--config FILE` | ні | `/etc/gitlab-repopolicy/config.yaml` | серверний конфіг |

Поки дозвіл діє, усі модулі пропускають push у проєкт. Детальніше - [operations.md](operations.md#break-glass).

### `breakglass revoke`

```text
gitlab-repopolicy breakglass revoke (--project P | --project-id N) [--config FILE]
```

| Прапорець | Обов'язковий | За замовчуванням | Що робить |
| --- | --- | --- | --- |
| `--project P` | `--project` або `--project-id` | немає | повний шлях проєкту |
| `--project-id N` | `--project` або `--project-id` | немає | ID проєкту |
| `--config FILE` | ні | `/etc/gitlab-repopolicy/config.yaml` | серверний конфіг |

### `breakglass list`

```text
gitlab-repopolicy breakglass list [--config FILE]
```

| Прапорець | Обов'язковий | За замовчуванням | Що робить |
| --- | --- | --- | --- |
| `--config FILE` | ні | `/etc/gitlab-repopolicy/config.yaml` | серверний конфіг |

Перелічує дійсні дозволи.

## Службове

### `hook pre-receive`

```text
gitlab-repopolicy hook pre-receive [--config FILE]
```

| Прапорець | Обов'язковий | За замовчуванням | Що робить |
| --- | --- | --- | --- |
| `--config FILE` | ні | `/etc/gitlab-repopolicy/config.yaml` | серверний конфіг |

Запускає Gitaly через wrapper, вручну її не викликають. Оновлення ref читаються з stdin, середовище - від Gitaly (`GL_ID`, `GL_USERNAME`, `GL_PROTOCOL`, `GL_PROJECT_PATH`). Відмова - текст для користувача і код 1. `-h` команда не друкує: усе, що вона виводить, бачить той, хто пушить.

### `version`

```text
gitlab-repopolicy version
```

Прапорців немає.

```text
$ gitlab-repopolicy version
gitlab-repopolicy 1.0.0
commit: 6a67f52
date: 2026-10-17T12:00:00Z
go: go1.25.4
```
