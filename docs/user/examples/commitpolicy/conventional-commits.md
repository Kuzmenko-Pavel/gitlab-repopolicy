[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](conventional-commits.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](conventional-commits.en.md)

# Conventional Commits у main і release-гілках

Команда хоче зрозумілу історію в `main` і `release/`: визначені типи змін, `Signed-off-by`, пояснення для `feat` і `fix`. У release-гілках дозволені лише `fix`, `docs` і `chore`; робочі гілки не перевіряються цим файлом.

## Правила

Збережіть конфігурацію як `COMMITPOLICY` у корені репозиторію. Перед merge в основну гілку запустіть `gitlab-repopolicy validate -f COMMITPOLICY`. `release-bot` має існувати й бути активним: новий запис у `except.users` перевіряється через API під час приймання файлу.

```yaml
# examples/conventional-commits/COMMITPOLICY
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
```

У `main` збігається `conventional`. У `release/1` діють обидва правила: загальні вимоги не зникають, а список типів звужується правилом `release`. `header-max-length` і посилання на issue дають попередження, а не відмову. Винятки дозволяють звичайний merge-коміт, стандартний revert і push розпізнаного release-bot; вони стосуються лише свого правила.

## Повідомлення та перевірка

Цей коміт відповідає обом правилам:

```text
fix(billing): round refunds to cents

Refunds lost a cent.

Refs: #665
Signed-off-by: A <a@example.com>
```

Збережіть повідомлення в `message.txt` і запустіть `gitlab-repopolicy lint-commit -f COMMITPOLICY -m "$(cat message.txt)"`. Код 0 означає відсутність помилок рівня `error`. Для перевірки тільки загального стандарту додайте `--rule conventional`: локальний лінтер сам не обирає правило за `match`.

`feat(api): add validation` з body і trailers може пройти загальне правило, але буде відхилений `release` при локальній перевірці всіх правил або push у release-гілку. Не виправляйте це розширенням release-списку, якщо нові можливості там справді заборонені.

Для спостереження спочатку задайте `mode: warn` у файлі. У глобальному `audit` навіть `mode: enforce` не блокує push; щоб побачити реальну відмову, потрібен серверний `enforce`.

[Довідник COMMITPOLICY](../../policies/commitpolicy.md)
