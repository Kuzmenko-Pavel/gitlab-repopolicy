[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](commitizen.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](commitizen.en.md)

# Вимоги commitizen на сервері

Команда використовує список типів, header до 100 символів і trailer `ref`, `refs` або `issue` з номером GitLab issue. COMMITPOLICY може перевіряти ці вимоги на сервері незалежно від клієнтського генератора повідомлень.

## Конфігурація

Збережіть як `COMMITPOLICY` у корені репозиторію, перевірте `gitlab-repopolicy validate -f COMMITPOLICY` і злийте в основну гілку.

```yaml
# examples/commitizen/COMMITPOLICY
# The commitizen schema of a project: the type from a list, a header of up to
# 100 characters and a trailer ref, refs or issue with a GitLab issue. The
# checks cover the format, the list, the length and the empty line after the
# header; the trailer is required and the pattern covers its lines.
version: 1

rules:
  commitizen:
    match: all
    except:
      merge_commit: true
    checks:
      header-format:
        conventional: { level: error, value: required }
      header-max-length:
        commitizen: { level: error, value: 100 }
      type-enum:
        commitizen: { level: error, value: [build, ci, docs, feat, fix, perf, refactor, style, test, chore, revert, bump] }
      body-leading-blank-lines:
        blank-line: { level: error, value: required }
      trailer-required:
        ref: { level: error, value: required }
      pattern:
        ref: { level: error, location: trailer, value: '/(?m)^(ref|refs|issue): [a-zA-Z0-9_.\/-]*#\d+$/' }
    message: 'Commits follow the commitizen schema: type: subject, then a trailer such as "ref: payments/api#123".'
```

`header-format` з `value: required` відхиляє header не у форматі Conventional Commits, `type-enum` задає допустимі типи, `header-max-length` - межу довжини, `body-leading-blank-lines` вимагає порожній рядок після header. `trailer-required` вимагає trailer, останню частину повідомлення, а `pattern` перевіряє, що в ньому є рядок `ref`, `refs` або `issue` з номером issue. `(?m)` робить `^` і `$` межами кожного рядка trailer, тому поруч можуть стояти інші рядки, наприклад `Signed-off-by`. `match: all` поширює вимоги на нові коміти робочих гілок, а не лише merge в основну.

<!-- check: lint-commit examples/commitizen/COMMITPOLICY -->
| Повідомлення | Результат | Чому |
| --- | --- | --- |
| `feat: add x\n\nref: group/project#1` | проходить | type зі списку, trailer `ref` з issue |
| `fix: x\n\nref: group/my.project#1\nSigned-off-by: A <a@example.com>` | проходить | `Signed-off-by` у тому самому абзаці не заважає |
| `feat(: add x\n\nref: group/project#1` | відмова | `header-format`: header не розбирається |
| `feat: add x\nbody right after the header\nref: group/project#1` | відмова | `body-leading-blank-lines`: немає порожнього рядка після header |
| `feat: add x\n\nSee group/project#1 in the tracker` | відмова | `pattern ref`: trailer є, але без рядка `ref:` |
| `feat: add x` | відмова | `body-leading-blank-lines`, `trailer-required` і `pattern ref`: після header нічого немає |

Запишіть повідомлення таблиці в `message.txt` зі справжніми переносами й виконайте `gitlab-repopolicy lint-commit -m "$(cat message.txt)"`. Коректні рядки дають 0, порушення - 1. Це перевірка конкретних наведених вимог, а не всіх можливостей або плагінів commitizen.

Не дописуйте body без порожнього рядка після header. Посилання має стояти в останньому абзаці рядком `ref: ...`: `pattern` шукає його лише в trailer. `Signed-off-by` у тому самому абзаці не заважає, але сам по собі не замінює `ref`.

[Довідник COMMITPOLICY](../../policies/commitpolicy.md)
