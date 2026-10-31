[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](message-results.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](message-results.en.md)

# Чому повідомлення проходить або відхиляється

Потрібно відрізняти помилку формату від попередження й розуміти, як порожній body або неправильно розміщений trailer змінює результат.

## Конфігурація для порівняння

Використайте цей повний `COMMITPOLICY` у корені каталогу. Таблиця нижче перевіряє лише `conventional`; правило `release` не застосовується до цієї локальної перевірки.

```yaml
# examples/conventional-commits/COMMITPOLICY
# Conventional Commits in main and release branches; a stricter type list in
# release branches. Every commit that enters these branches is checked, also
# the commits of a merged branch. Other branches are not checked.
version: 1
mode: enforce

rules:
  conventional:
    match: [main, '/^release\//']
    check_history: incoming
    parser: conventional
    except:
      merge_commit: true
      git_reverts: true
      users: [release-bot]
    checks:
      header-format:
        conventional: { level: error }
      header-max-length:
        limit: { level: warn, value: 100 }
      type-enum:
        allowed: { level: error, value: [feat, fix, build, chore, ci, docs, perf, refactor, revert, style, test] }
      scope:
        optional: { level: error, value: optional }
      breaking-bang:
        optional: { level: error, value: optional }
      subject-starts-lowercase:
        lowercase: { level: error, value: required }
      subject-trailing-dot:
        no-dot: { level: error, value: forbidden }
      body-leading-blank-lines:
        blank-line: { level: error, value: required }
      body-min-line-length:
        feat-fix: { level: error, value: 1, types: [feat, fix] }
      trailer-required:
        dco: { level: error, value: [Signed-off-by] }
      pattern:
        issue-ref: { level: warn, location: message, value: '/[A-Za-z0-9_.\/-]*#[0-9]+/', types: [feat, fix] }
    message: >-
      Commits in main and release branches follow Conventional Commits.
      Reword them with git rebase -i or squash the merge request.

  release:
    match: ['/^release\//']
    check_history: incoming
    parser: conventional
    checks:
      type-enum:
        release: { level: error, value: [fix, docs, chore] }
    message: Release branches take only fix, docs and chore commits.
```

Для кожного рядка запустіть `gitlab-repopolicy lint-commit -f COMMITPOLICY --rule conventional -m "$(cat message.txt)"`, записавши повідомлення в `message.txt` із справжніми переносами рядків. У таблиці `\n` позначає перенос.

<!-- check: lint-commit examples/conventional-commits/COMMITPOLICY conventional -->
| Повідомлення | Результат | Чому |
| --- | --- | --- |
| `fix(billing): round refunds to cents\n\nRefunds lost a cent.\n\nRefs: #665\nSigned-off-by: A <a@example.com>` | проходить | усе на місці |
| `docs: fix a typo\n\nSigned-off-by: A <a@example.com>` | проходить | `body-min-line-length` і `pattern` діють лише для `feat` і `fix` |
| `feat(api)!: drop v1\n\nV1 is gone.\n\nRefs: #2\nSigned-off-by: A <a@example.com>` | проходить | `!` дозволено (`breaking-bang: optional`) |
| `bugfix: round refunds\n\nSigned-off-by: A <a@example.com>` | відмова | `type-enum`: `bugfix` немає в списку |
| `fix: Round refunds\n\nBody.\n\nRefs: #1\nSigned-off-by: A <a@example.com>` | відмова | `subject-starts-lowercase`: subject з великої літери |
| `fix: round refunds.\n\nBody.\n\nRefs: #1\nSigned-off-by: A <a@example.com>` | відмова | `subject-trailing-dot`: крапка в кінці |
| `fix: round refunds\nRefunds lost a cent.\n\nRefs: #1\nSigned-off-by: A <a@example.com>` | відмова | `body-leading-blank-lines`: body починається одразу з другого рядка |
| `fix: round refunds\n\nSigned-off-by: A <a@example.com>` | відмова | `body-min-line-length`: у `fix` немає тексту body |
| `fix: round refunds\n\nBody without a reference.\n\nSigned-off-by: A <a@example.com>` | попередження | `pattern` рівня `warn`: немає посилання на issue |
| `docs: fix a typo` | відмова | `body-leading-blank-lines` і `trailer-required`: у повідомленні з одного рядка немає ні порожнього рядка після header, ні `Signed-off-by` |
| `Revert "feat: x"\n\nThis reverts commit 0123456789abcdef0123456789abcdef01234567.` | проходить | `except.git_reverts` |

Відмова у виводі push:

```text
remote: COMMITPOLICY rule "conventional", branch main: 1 commit message does not follow the format
remote:   required: type(scope): subject, for example feat(api): add refunds
remote:   types: feat, fix, build, chore, ci, docs, perf, refactor, revert, style, test
remote:   commits:
remote:     60a6b73c3 "Fix deploy."
remote:       - header-format conventional: expected "type(scope): subject"
remote:       - body-leading-blank-lines blank-line: an empty line must follow the header
remote:       - trailer-required dco: missing Signed-off-by in the trailer block
remote:   Commits in main and release branches follow Conventional Commits. Reword them with git rebase -i or squash the merge request.
remote:   how to fix: reword the commit (git commit --amend for the last one, git rebase -i for an older one), then push again
```

Кожна причина починається з типу й імені перевірки, наприклад `trailer-required dco`: за іменем видно, яка перевірка з файлу спрацювала. `Signed-off-by` є trailer, тому він не замінює body. У `docs` body необов'язковий, а у `fix` потрібний. Відсутнє посилання на issue дає лише `warning`, бо `pattern` має `level: warn`. Локально відмова дає код 1, попередження - 0; у hook додатково діють `match`, винятки й глобальний режим.

Не копіюйте буквальні символи `\n` у повідомлення коміту. Після виправлення останнього коміту через `git commit --amend` повторіть лінтер. Успіх підтверджує повідомлення за цим правилом, але не доступ до захищених файлів.

[Довідник COMMITPOLICY](../../policies/commitpolicy.md)
