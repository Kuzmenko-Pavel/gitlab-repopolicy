[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](multiple-requirements.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](multiple-requirements.en.md)

# Кілька незалежних вимог до коміту

У default branch не повинно бути WIP-комітів, і кожен звичайний коміт має посилатися на issue в trailer. Обидві вимоги стосуються тих самих комітів, тому це одне правило з двома перевірками `pattern`: у кожної своє ім'я, а `match`, `parser` і `except` спільні.

## Файл COMMITPOLICY

Додайте цей файл у корінь репозиторію, перевірте `gitlab-repopolicy validate` й активуйте merge в основну гілку.

```yaml
# examples/commit-patterns/COMMITPOLICY
# Two requirements for the commits of the default branch in one rule, two
# named pattern checks: no work-in-progress headers, and a reference to an
# issue in a trailer.
version: 1

rules:
  default-branch:
    match: default
    parser: free
    except:
      merge_commit: true
    checks:
      pattern:
        no-wip: { level: error, location: header, value: '!/^(WIP|fixup!|squash!)/i' }
        issue-ref: { level: error, location: trailer, keys: [Refs, Closes], value: '/^[A-Za-z0-9_.\/-]*#[0-9]+$/' }
    message: >-
      Squash or reword work-in-progress commits before the merge, and end the
      message with a trailer such as "Refs: payments/api#123".
```

`no-wip` шукає заборонені префікси без урахування регістру. `issue-ref` вимагає одне посилання у `Refs` або `Closes`. Merge-коміт правило пропускає цілком (`except.merge_commit`): його header пише GitLab, WIP-префікса там не буває, а посилання на issue в ньому немає. Кожна перевірка має свій `level`, тож одну з них можна зробити попередженням, не чіпаючи іншу.

<!-- check: lint-commit examples/commit-patterns/COMMITPOLICY -->
| Повідомлення | Результат | Чому |
| --- | --- | --- |
| `Fix login\n\nRefs: payments/api#12` | проходить | обидві перевірки виконані |
| `Fix login\n\nCloses: #12` | проходить | `Closes` теж є в `keys` |
| `Fix login\n\nRefs: 12` | відмова | `pattern issue-ref`: значення без `#` |
| `fixup! Fix login\n\nRefs: #12` | відмова | `pattern no-wip`: header починається з `fixup!` |

Збережіть повідомлення зі справжніми переносами в `message.txt` і виконайте `gitlab-repopolicy lint-commit -m "$(cat message.txt)"`. Для рядка `fixup!` посилання правильне, але цього недостатньо: потрібні обидві вимоги.

Не замінюйте дві перевірки одним виразом із альтернативами: це вимагало б лише одного збігу замість обох. Пресет, що задає ці перевірки, проєкт перевизначає за іменем: `pattern: {issue-ref: {level: warn}}` робить попередженням лише посилання на issue. Після видалення WIP-префікса й додавання правильного trailer очікуйте код 0. У policy `warn` та глобальному `audit` застосування відмов відрізняється від таблиці для `enforce`.

[Довідник COMMITPOLICY](../../policies/commitpolicy.md)
