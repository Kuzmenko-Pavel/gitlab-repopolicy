[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](validation-walkthrough.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](validation-walkthrough.en.md)

# Практикум validate: помилки, пресети й діагностика

`gitlab-repopolicy validate` перевіряє файли політик так само, як hook перевіряє нову версію файлу під час push: схему, `match`, регулярні вирази, порядок правил BRANCHPOLICY, ключі TAGPOLICY, злиття з пресетами. Помилку видно до push і до merge request, а не тоді, коли GitLab відхилив push.

Цей практикум призначено для послідовного відтворення діагностики на окремому демо-каталозі. Для щоденної перевірки власних файлів почніть із [короткого керівництва validate](../reference/validate.md). У [довіднику CLI](../reference/cli.md) наведено всі прапорці. Для повідомлень комітів є окрема команда `lint-commit` ([commitpolicy.md](../policies/commitpolicy.md#перевірка-без-push)).

## Коли запускати

| Де | Прапорці | Що потрібно |
| --- | --- | --- |
| локально перед push | `validate`, `-f FILE`, `--config presets.yaml` для файлів з `extends` | бінарник і файли. GitLab, токен і git не потрібні |
| у CI merge request | те саме | бінарник з релізу в job ([нижче](#validate-у-ci-merge-request)) |
| на ноді Gitaly | `--online --config /etc/gitlab-repopolicy/config.yaml [--project P]` | серверний конфіг і токен hook, API GitLab |

| Режим | Прапорці | Потрібен GitLab | Де запускати |
| --- | --- | --- | --- |
| усі файли каталогу | без прапорців | ні | будь-де |
| один файл | `-f FILE`, `--module NAME` для іншого імені | ні | будь-де |
| з пресетами | `--config FILE` | ні | будь-де, з копією пресетів |
| правило для імені гілки або тегу | `--branch NAME`, `--tag NAME` | ні | будь-де |
| користувачі існують | `--online --config FILE` | так | нода Gitaly |
| роль admins у проєкті | `--online --config FILE --project P` | так | нода Gitaly |

## Підготовка: демо-каталог

`validate` читає файли з каталогу, git йому не потрібен. Репозиторій тут лише для реалістичності: потім ці файли можна закомітити і запушити.

```sh
mkdir demo && cd demo && git init
```

Чотири файли політик - ті самі, що в прикладах документації:

```sh
cat > PATHPOLICY <<'EOF'
# The platform team, alice and sre-lead, owns the CI configuration and the
# deploy scripts in every branch and the production values in main and
# release branches. Everything else is authorized by GitLab as usual.
version: 1

admins: [alice, sre-lead]

rules:
  ci-config:
    match: all
    paths:
      - ".gitlab-ci.yml"
      - ".gitlab/ci/**"
      - "scripts/deploy/**"
    allow: [alice, sre-lead]
    message: >-
      The platform team changes the CI/CD configuration and the deploy scripts.
      Ask alice or sre-lead to push this change.
    help:
      contact: "#platform"

  prod-values:
    match: [main, '/^release\//']
    paths: ["helm/**/values-prod.yaml"]
    allow: [alice, release-manager]
    message: >-
      Production values are changed in main and release branches by alice or
      release-manager. Propose the change in your branch and ask them to merge it.
EOF
```

```sh
cat > COMMITPOLICY <<'EOF'
# Conventional Commits in main and release branches; a stricter type list in
# release branches. Other branches are not checked.
version: 1
mode: enforce

rules:
  conventional:
    match: [main, '/^release\//']
    parser: conventional
    except:
      merge_commit: true
      git_reverts: true
      users: [release-bot]
    checks:
      type-enum:         { level: error, value: [feat, fix, build, chore, ci, docs, perf, refactor, revert, style, test] }
      scope:             { level: error, value: optional }
      breaking-bang:     { level: error, value: allow }
      header-max-length: { level: warn,  value: 100 }
      subject-case:      { level: error, value: lower-first }
      subject-full-stop: { level: error, value: forbid }
      body-required:     { level: error, types: [feat, fix] }
      pattern:           { level: warn,  location: message, value: '/[A-Za-z0-9_.\/-]*#[0-9]+/', types: [feat, fix] }
      trailer-required:  { level: error, value: [Signed-off-by] }
    message: >-
      Commits in main and release branches follow Conventional Commits.
      Reword them with git rebase -i or squash the merge request.

  release:
    match: ['/^release\//']
    parser: conventional
    checks:
      type-enum: { level: error, value: [fix, docs, chore] }
    message: Release branches take only fix, docs and chore commits.
EOF
```

```sh
cat > TAGPOLICY <<'EOF'
# Release tags: staging is signed by release-lead or dev-lead, prod only by
# release-lead, and a prod tag must be protected in GitLab. Tags that match no
# rule, such as build-42, are not checked.
version: 1

rules:
  staging:
    match: ['/^v\d+\.\d+\.\d+-staging$/']
    signers: [release-lead, dev-lead]
    message: Staging releases are signed by release-lead or dev-lead.
  prod:
    match: ['/^v\d+\.\d+\.\d+-prod$/']
    signers: [release-lead]
    protected: true
    message: >-
      Prod releases are signed by release-lead.
      Protect v*-prod in Settings > Repository > Protected tags.

keys:
  release-lead: |
    -----BEGIN PGP PUBLIC KEY BLOCK-----
    
    mDMEas8yyBYJKwYBBAHaRw8BAQdARgQtR6FZCckYy8pf9+9snMoIb3gcbH9QVotM
    2CEt2za0H1JlbGVhc2UgTGVhZCA8bGVhZEBleGFtcGxlLmNvbT6IkwQTFgoAOxYh
    BGx5V+qHOlRnp48dB1l3WaOCcpNZBQJqzzLIAhsDBQsJCAcCAiICBhUKCQgLAgQW
    AgMBAh4HAheAAAoJEFl3WaOCcpNZf7IBAO02jJtCe9KhJlLAOTz/XX+kpgyY8mth
    6Cku2bukuF6bAP40EeqZQ1ISoo/Agcdncm5Z+KysHKFd7y49xzE0q6ObDA==
    =3XJY
    -----END PGP PUBLIC KEY BLOCK-----
  dev-lead: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIG6MuGxttDci5himWr8891GvUm9MjQljladoClrTOUiE ed25519
EOF
```

```sh
cat > BRANCHPOLICY <<'EOF'
# Strict mode: only release and work branches may be created. The last rule
# has match: all and deny, so a name of no other class is refused instead of
# passing unchecked.
version: 1

rules:
  release:
    match: ['/^release\/v\d+\.\d+\.\d+$/']
    priority: 10
    protected: true
    message: Release branches look like release/v1.4.0.
  work:
    match: ['/^[a-z0-9.]+\/[a-z0-9._-]+$/']
    priority: 20
    message: Work branches are <prefix>/<name>, for example kpn/disable_auth.
  other:
    match: all
    priority: 1000
    deny: true
    message: Use release/vX.Y.Z or <prefix>/<name>.
EOF
```

`presets.yaml` - серверний конфіг лише з ключем `presets`, для сценаріїв з `extends`. Інші ключі конфігу `validate` без `--online` не читає:

```sh
cat > presets.yaml <<'EOF'
presets:
  commitpolicy:
    org-commits:
      rules:
        conventional:
          match: default
          parser: conventional
          except:
            merge_commit: true
            git_reverts: true
          checks:
            type-enum: { level: error, value: [feat, fix, build, chore, ci, docs, perf, refactor, test] }
            header-max-length: { level: error, value: 100 }
            subject-full-stop: { level: error, value: forbid }
          message: Commits in the default branch follow Conventional Commits.
          help:
            docs_url: https://www.conventionalcommits.org/en/v1.0.0/
EOF
```

Чернетки для сценаріїв з помилками лежатимуть у `drafts/`:

```sh
mkdir drafts
```

## Сценарії

Кожен сценарій: що перевіряємо, команда, вивід, код виходу (`echo $?`) і що робити. Коди виходу: `0` - файли валідні (попередження і `not checked` не заважають), `1` - є невалідний файл або помилка читання файлу чи конфігу, `2` - неправильні аргументи команди.

### Усі файли каталогу

Без прапорців `validate` перевіряє всі файли політик поточного каталогу:

```console
$ gitlab-repopolicy validate
PATHPOLICY: valid
COMMITPOLICY: valid
TAGPOLICY: valid
BRANCHPOLICY: valid
$ echo $?
0
```

### Один файл

```console
$ gitlab-repopolicy validate -f COMMITPOLICY
COMMITPOLICY: valid
$ echo $?
0
```

### Чернетка з іншим ім'ям

За ім'ям файлу `validate` визначає, який це файл політики. Для іншого імені потрібен `--module`:

```console
$ cp PATHPOLICY PATHPOLICY.new
$ gitlab-repopolicy validate -f PATHPOLICY.new
gitlab-repopolicy validate: PATHPOLICY.new is not a policy file; use --module with one of branchpolicy, commitpolicy, pathpolicy, tagpolicy
$ echo $?
2
$ gitlab-repopolicy validate -f PATHPOLICY.new --module pathpolicy
PATHPOLICY.new: valid
$ echo $?
0
```

### Порожній каталог

```console
$ mkdir empty
$ cd empty
$ gitlab-repopolicy validate
no policy files in the current directory
$ echo $?
0
$ cd ..
```

Код 0: файлів політик немає, і перевіряти нічого. У CI це означає, що job нічого не зламає в проєкті без політик.

### Неправильні аргументи

Код 2 і текст помилки в stderr:

```console
$ gitlab-repopolicy validate PATHPOLICY
gitlab-repopolicy validate: unexpected arguments ["PATHPOLICY"]
$ echo $?
2
$ gitlab-repopolicy validate --online
gitlab-repopolicy validate: --online needs --config with gitlab_url and token_file
$ echo $?
2
```

Файл задають через `-f`, а не аргументом.

### Невалідні файли

Помилка YAML, з номером рядка:

```sh
cat > drafts/yaml-error <<'EOF'
version: 1
admins: [alice]
rules:
  ci:
    match: all
     paths: [.gitlab-ci.yml]
EOF
```

```console
$ gitlab-repopolicy validate -f drafts/yaml-error --module pathpolicy
drafts/yaml-error: invalid
  yaml: line 6: mapping values are not allowed in this context
$ echo $?
1
```

У рядку 6 зайвий пробіл перед `paths`.

Невідомий ключ:

```sh
cat > drafts/unknown-key <<'EOF'
version: 1
admins: [alice]
rules:
  ci:
    match: all
    path: [.gitlab-ci.yml]
    allow: [alice]
    message: CI belongs to alice.
EOF
```

```console
$ gitlab-repopolicy validate -f drafts/unknown-key --module pathpolicy
drafts/unknown-key: invalid
  line 6: rule ci: unknown field "path" (did you mean "paths"?)
$ echo $?
1
```

Немає обов'язкового поля. `match`, `message`, а в COMMITPOLICY ще й `parser` обов'язкові в кожному правилі:

```sh
cat > drafts/no-match <<'EOF'
version: 1
rules:
  conventional:
    parser: conventional
    message: Commits follow Conventional Commits.
EOF
```

```console
$ gitlab-repopolicy validate -f drafts/no-match --module commitpolicy
drafts/no-match: invalid
  line 4: rule conventional: match: required
$ echo $?
1
```

Glob у `match`:

```sh
cat > drafts/glob <<'EOF'
version: 1
rules:
  release:
    match: ['release/*']
    priority: 10
    protected: true
    message: Release branches are protected.
EOF
```

```console
$ gitlab-repopolicy validate -f drafts/glob --module branchpolicy
drafts/glob: invalid
  line 4: rule release: match: "release/*": a glob is not supported; use a regex such as /^release\//
$ echo $?
1
```

Невалідний регулярний вираз:

```sh
cat > drafts/bad-regex <<'EOF'
version: 1
rules:
  prod:
    match: ['/^v\d+(\.\d+$/']
    signers: [release-lead]
    message: Prod releases are signed by release-lead.
keys:
  release-lead: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl release-lead
EOF
```

```console
$ gitlab-repopolicy validate -f drafts/bad-regex --module tagpolicy
drafts/bad-regex: invalid
  line 4: rule prod: match: "/^v\\d+(\\.\\d+$/": not a valid RE2 regex: error parsing regexp: missing closing ): `^v\d+(\.\d+$`
$ echo $?
1
```

Порядок правил BRANCHPOLICY: `match: all` не останнє і повтор `priority`:

```sh
cat > drafts/order <<'EOF'
version: 1
rules:
  other:
    match: all
    priority: 10
    deny: true
    message: Use release/vX.Y.Z or <prefix>/<name>.
  release:
    match: ['/^release\/v\d+\.\d+\.\d+$/']
    priority: 20
    message: Release branches look like release/v1.4.0.
  hotfix:
    match: ['/^hotfix\//']
    priority: 20
    message: Hotfix branches look like hotfix/v1.4.1.
EOF
```

```console
$ gitlab-repopolicy validate -f drafts/order --module branchpolicy
drafts/order: invalid
  error: rule release: comes after rule other with match: all and is never used; give match: all the highest priority
  error: rule hotfix: priority 20 is also the priority of rule release
  error: rule hotfix: comes after rule other with match: all and is never used; give match: all the highest priority
$ echo $?
1
```

Помилки порядку правил мають префікс `error:`, бо їх знаходить перевірка після злиття з пресетами. Помилки розбору файлу друкуються без префікса.

`signers` посилається на ключ, якого немає:

```sh
cat > drafts/signers <<'EOF'
version: 1
rules:
  prod:
    match: ['/^v\d+\.\d+\.\d+-prod$/']
    signers: [release-lead, sec-lead]
    message: Prod releases are signed by release-lead or sec-lead.
keys:
  release-lead: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl release-lead
EOF
```

```console
$ gitlab-repopolicy validate -f drafts/signers --module tagpolicy
drafts/signers: invalid
  rule prod: signers: "sec-lead" is not a label in keys of the file
$ echo $?
1
```

### Попередження

Ключ, який не використовує жодне правило, - лише попередження, код 0:

```sh
cat > drafts/unused-key <<'EOF'
version: 1
rules:
  prod:
    match: ['/^v\d+\.\d+\.\d+-prod$/']
    signers: [release-lead]
    message: Prod releases are signed by release-lead.
keys:
  release-lead: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl release-lead
  old-lead: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIG6MuGxttDci5himWr8891GvUm9MjQljladoClrTOUiE old-lead
EOF
```

```console
$ gitlab-repopolicy validate -f drafts/unused-key --module tagpolicy
drafts/unused-key: valid
  warning: keys.old-lead is not used by any rule
$ echo $?
0
```

### Прострочений ключ

Прострочений OpenPGP-ключ - помилка, код 1:

```sh
cat > drafts/expired-key <<'EOF'
version: 1
rules:
  prod:
    match: ['/^v\d+\.\d+\.\d+-prod$/']
    signers: [old-lead]
    message: Prod releases are signed by old-lead.
keys:
  old-lead: |
    -----BEGIN PGP PUBLIC KEY BLOCK-----
    
    xjMEZZIAgBYJKwYBBAHaRw8BAQdAiav9rGtFxuzsm74siFSXUl2FL4BUmXHXmfZv
    oOEhbDbNJ09sZCBSZWxlYXNlIExlYWQgPG9sZC1sZWFkQGV4YW1wbGUuY29tPsLA
    AwQTFggAdQWCZZIAgAWJACeNAAILBwkQX6GOCC8qk+s1FAAAAAAAHAAQc2FsdEBu
    b3RhdGlvbnMub3BlbnBncGpzLm9yZ+8iD3ZF0ZPTRpvuVlRt4vICFQgCFgACGQEC
    mwMCHgEWIQS7QNFPPR1WN7ZS6TtfoY4ILyqT6wAA3J8A/Am90Vr3su2c84Aau/+h
    FrtSuh62JOxfKrusWpSllM/vAP9nYQT0nUmZxbMUu9t5bWaIr8l6Bnhuwf1h9+Na
    gHLNBc44BGWSAIASCisGAQQBl1UBBQEBB0BaRZLBtMaeNDOxD8z9VMwDtZAgf/nv
    JUHIXAhFxOYZEQMBCgnCrgQYFggAYAWCZZIAgAkQX6GOCC8qk+s1FAAAAAAAHAAQ
    c2FsdEBub3RhdGlvbnMub3BlbnBncGpzLm9yZ21byLI0mcu5rLYID0ILD8gCmwwW
    IQS7QNFPPR1WN7ZS6TtfoY4ILyqT6wAADwgA+wcFBElmZt/jvswEQqfBFSpDUP2A
    V64xDDUPafdFGrwYAP9pnTRsdqfsBgzMA4+hP+8xuDN5Tbw38UuUeCmd17SwCg==
    =G3gt
    -----END PGP PUBLIC KEY BLOCK-----
EOF
```

```console
$ gitlab-repopolicy validate -f drafts/expired-key --module tagpolicy
drafts/expired-key: invalid
  error: keys.old-lead: the key BB40D14F3D1D5637B652E93B5FA18E082F2A93EB has no signing key valid on 2026-10-20: it expired; extend it or remove the entry
$ echo $?
1
```

`validate` перевіряє файл сам по собі. Під час push hook порівнює нову версію з версією в default branch, і прострочений ключ у записі, який не змінювали, дає лише попередження: чужий прострочений ключ не блокує правку файлу. Інші відмінності між локальною перевіркою й hook пояснює розділ [«Чого validate не знає»](#чого-validate-не-знає).

### Пресети

Файл з `extends` без серверного конфігу не можна перевірити до кінця: пресета немає.

```sh
cat > drafts/org-commits <<'EOF'
# The commit rules of the organization come from the preset org-commits.
# This project only warns about long headers and allows a full stop at the
# end of the subject.
version: 1
extends: org-commits

rules:
  conventional:
    checks:
      header-max-length: { level: warn }
      subject-full-stop: null
EOF
```

```console
$ gitlab-repopolicy validate -f drafts/org-commits --module commitpolicy
drafts/org-commits: invalid
  error: extends: preset "org-commits" is not defined in the server config
$ echo $?
1
```

З `--config` `validate` зливає пресет і файл, як hook, і показує, звідки кожне правило і поле:

```console
$ gitlab-repopolicy validate -f drafts/org-commits --module commitpolicy --config presets.yaml
drafts/org-commits: valid
  extends: org-commits
  rule conventional: preset:org-commits, file
    checks: preset:org-commits, file
    checks.header-max-length: preset:org-commits, file
    checks.type-enum: preset:org-commits
    except: preset:org-commits
    except.git_reverts: preset:org-commits
    except.merge_commit: preset:org-commits
    help: preset:org-commits
    help.docs_url: preset:org-commits
    match: preset:org-commits
    message: preset:org-commits
    parser: preset:org-commits
$ echo $?
0
```

- `checks.header-max-length: preset:org-commits, file` - файл змінив у перевірці пресета лише `level`, `value` залишився з пресета
- `checks.subject-full-stop` у виводі немає: файл видалив її через `null`
- правило без жодного поля з файлу має лише `preset:org-commits`

Пресета немає в конфігу:

```sh
cat > drafts/missing-preset <<'EOF'
version: 1
extends: org-missing
EOF
```

```console
$ gitlab-repopolicy validate -f drafts/missing-preset --module commitpolicy --config presets.yaml
drafts/missing-preset: invalid
  error: extends: preset "org-missing" is not defined in the server config
$ echo $?
1
```

Зламаний пресет у `--config` `validate` показує окремо, навіть якщо жоден файл його не використовує: `presets.yaml: invalid presets` і рядки `error:`.

### Яке правило спрацює: `--branch` і `--tag`

`--branch NAME` показує правило BRANCHPOLICY для нової гілки з таким ім'ям і результат, `--tag NAME` - правила TAGPOLICY і хто може підписати тег. Спершу файл перевіряється як завжди. Код 0 за будь-якого результату: це запит, а не перевірка.

```console
$ gitlab-repopolicy validate -f BRANCHPOLICY --branch release/v1.4.0
BRANCHPOLICY: valid
branch release/v1.4.0: rule release (match: [/^release\/v\d+\.\d+\.\d+$/]; from file)
  requires: the project protects the branch in Settings > Repository > Protected branches
  result: allowed when the requirements hold
$ echo $?
0
$ gitlab-repopolicy validate -f BRANCHPOLICY --branch fix-login
BRANCHPOLICY: valid
branch fix-login: rule other (match: all; from file)
  result: denied, branches of this class may not be created
  message: Use release/vX.Y.Z or <prefix>/<name>.
$ echo $?
0
$ gitlab-repopolicy validate -f TAGPOLICY --tag v1.4.0-staging
TAGPOLICY: valid
tag v1.4.0-staging: rules staging (from file)
  signers: release-lead (OpenPGP), dev-lead (SSH)
  the tag must be signed by one of the signers; validate does not check signatures
$ echo $?
0
```

| Ім'я | Команда | Правило | Результат |
| --- | --- | --- | --- |
| `release/v1.4.0` | `--branch` | `release` | дозволено, якщо проєкт захищає гілку |
| `release/1.4` | `--branch` | `work` | дозволено: ім'я схоже на `<prefix>/<name>`. Щоб таке ім'я не проходило, потрібне правило `deny` для родини `release/` ([branchpolicy.md](branchpolicy/deny-family.md)) |
| `fix-login` | `--branch` | `other` | відмова |
| `v1.4.0-prod` | `--tag` | `prod` | підписує release-lead, тег має бути захищеним |
| `build-42` | `--tag` | немає | не перевіряється |

Без `--branch` чи `--tag` поруч з файлом потрібного модуля - помилка використання: `--branch needs a BRANCHPOLICY file: -f FILE or BRANCHPOLICY in the current directory`.

### `--online`

`--online` перевіряє через API GitLab, що username з файлу існують і активні. З `--project` - ще й що admins PATHPOLICY мають у проєкті роль Maintainer або вище. Команду запускають на ноді Gitaly від користувача, який може прочитати серверний конфіг і файл токена з нього, зазвичай від `git`:

```sh
sudo -u git gitlab-repopolicy validate -f PATHPOLICY --online --config /etc/gitlab-repopolicy/config.yaml
```

Без `--project` username перевірено, а роль admins - ні:

```text
PATHPOLICY: valid
  not checked: admins cannot be checked: --project is not given
```

Код 0. Неактивний або неіснуючий користувач - помилка, код 1:

```text
PATHPOLICY: invalid
  error: "nobody" is not an active GitLab user
  not checked: admins cannot be checked: --project is not given
```

З `--project`, коли один з двох admins - Developer:

```text
PATHPOLICY: valid
  warning: admin "bob" is not an active user with Maintainer or higher in this project
  warning: 1 of 2 admins work
```

Код 0: поки працює хоч один admin, файл приймається. Якщо не працює жоден, push відхиляється з `lockout_prevented`.

API недоступний. Без `--project` код 0, перевірки, які не вдалося виконати, позначено `not checked`:

```text
PATHPOLICY: valid
  not checked: usernames cannot be checked: gitlab /api/graphql: unavailable: Post "https://gitlab.example.com/api/graphql": dial tcp 192.0.2.10:443: connect: connection refused
```

З `--project`, коли проєкт не вдалося прочитати, - помилка, код 1:

```text
gitlab-repopolicy validate: --project acme/platform/app: gitlab /api/v4/projects/acme%2Fplatform%2Fapp: unavailable: Get "https://gitlab.example.com/api/v4/projects/acme%2Fplatform%2Fapp": dial tcp 192.0.2.10:443: connect: connection refused
```

## `validate` у CI merge request

Job запускається, лише якщо merge request змінює файл політики. Бінарник береться з релізу і перевіряється за `checksums.txt`, як під час встановлення ([install.md](../administration/install.md#1-підготувати-й-перевірити-артефакти)):

```yaml
# .gitlab-ci.yml
validate-policies:
  image: alpine:3.20
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
      changes: [PATHPOLICY, COMMITPOLICY, TAGPOLICY, BRANCHPOLICY]
  variables:
    REPOPOLICY_VERSION: "1.0.0"
  script:
    - apk add --no-cache curl
    - archive="gitlab-repopolicy_${REPOPOLICY_VERSION}_linux_amd64.tar.gz"
    - base="https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/releases/download/v${REPOPOLICY_VERSION}"
    - curl -fsSLO "$base/checksums.txt"
    - curl -fsSLO "$base/$archive"
    - grep " $archive\$" checksums.txt | sha256sum -c -
    - tar -xzf "$archive" gitlab-repopolicy
    - ./gitlab-repopolicy validate
```

Якщо файли використовують `extends`, job потребує копії пресетів: додайте `--config presets.yaml` і покладіть файл у репозиторій розгортання або в CI/CD variable типу File. `--online` у CI не запускають: йому потрібен токен hook.

## Чого `validate` не знає

`validate` перевіряє файл сам по собі. Під час push hook знає більше:

| Що знає hook | Що може пройти `validate`, але не push |
| --- | --- |
| версію файлу в default branch | ні, навпаки: прострочений OpenPGP-ключ у записі, який не змінювали, `validate` вважає помилкою, а push - попередженням |
| що файл - звичайний файл git | symlink або submodule з ім'ям файлу політики: `validate` читає файл, на який веде symlink, а push відхиляється з `invalid_policy_update` |
| хто існує в GitLab (без `--online`) | новий username в `allow`, `admins` чи `except.users`, якого немає або який заблокований: push відхиляється з `invalid_policy_update` |
| роль admins у проєкті (без `--project`) | PATHPOLICY, у якій жоден admin не має ролі Maintainer: push відхиляється з `lockout_prevented` |
| серверний конфіг ноди Gitaly | пресет, який у `--config` інший, ніж на сервері, або якого на сервері немає |
| чи захищено файл правилом PATHPOLICY | push приймає файл без такого правила з попередженням, `validate` про це не каже |

## Шпаргалка

| Задача | Команда |
| --- | --- |
| перевірити всі файли каталогу | `gitlab-repopolicy validate` |
| перевірити один файл | `gitlab-repopolicy validate -f COMMITPOLICY` |
| перевірити чернетку | `gitlab-repopolicy validate -f PATHPOLICY.new --module pathpolicy` |
| файл з `extends` | `gitlab-repopolicy validate --config presets.yaml` |
| яке правило для нової гілки | `gitlab-repopolicy validate --branch release/v1.4.0` |
| хто підписує тег | `gitlab-repopolicy validate --tag v1.4.0-prod` |
| префікс гілки - користувач GitLab | `gitlab-repopolicy validate --online --config /etc/gitlab-repopolicy/config.yaml --branch kpn/fix` |
| username файлу існують | `gitlab-repopolicy validate --online --config /etc/gitlab-repopolicy/config.yaml` |
| admins працюють у проєкті | `gitlab-repopolicy validate -f PATHPOLICY --online --config /etc/gitlab-repopolicy/config.yaml --project acme/platform/app` |
| повідомлення комітів | `gitlab-repopolicy lint-commit` ([commitpolicy.md](../policies/commitpolicy.md#перевірка-без-push)) |

[Повернутися до керівництва validate](../reference/validate.md).
