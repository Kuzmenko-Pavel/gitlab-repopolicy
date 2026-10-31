[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](multiple-requirements.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](multiple-requirements.en.md)

# Кілька незалежних вимог до коміту

У default branch не повинно бути WIP-комітів, і кожен звичайний коміт має посилатися на issue в trailer. Оскільки правило містить один `pattern`, потрібні два правила.

## Файл COMMITPOLICY

Додайте цей файл у корінь репозиторію, перевірте `gitlab-repopolicy validate` й активуйте merge в основну гілку.

```yaml
# examples/commit-patterns/COMMITPOLICY
# Two requirements for the commits of the default branch, one rule each: no
# work-in-progress headers, and a reference to an issue in a trailer.
version: 1

rules:
  no-wip:
    match: default
    parser: free
    checks:
      pattern: { level: error, location: header, value: '!/^(WIP|fixup!|squash!)/i' }
    message: Squash or reword work-in-progress commits before the merge.
  issue-ref:
    match: default
    parser: free
    except:
      merge_commit: true
    checks:
      pattern: { level: error, location: trailer, keys: [Refs, Closes], value: '/^[A-Za-z0-9_.\/-]*#[0-9]+$/' }
    message: 'End the message with a trailer such as "Refs: payments/api#123".'
```

Обидва правила мають `match: default`, тож перевіряються разом. `no-wip` шукає заборонені префікси без урахування регістру. `issue-ref` вимагає одне посилання у `Refs` або `Closes`; для merge-коміту воно пропускається, але `no-wip` залишається.

<!-- check: lint-commit examples/commit-patterns/COMMITPOLICY -->
| Повідомлення | Результат | Чому |
| --- | --- | --- |
| `Fix login\n\nRefs: payments/api#12` | проходить | обидва правила виконані |
| `Fix login\n\nCloses: #12` | проходить | `Closes` теж є в `keys` |
| `Fix login\n\nRefs: 12` | відмова | `issue-ref`: значення без `#` |
| `fixup! Fix login\n\nRefs: #12` | відмова | `no-wip`: header починається з `fixup!` |

Збережіть повідомлення зі справжніми переносами в `message.txt` і виконайте `gitlab-repopolicy lint-commit -m "$(cat message.txt)"`. Перевірка всіх правил тут доречна, бо вони описують одну й ту саму гілку. Для рядка `fixup!` посилання правильне, але цього недостатньо: потрібні обидві вимоги.

Не замінюйте два правила одним виразом із альтернативами: це вимагало б лише одного збігу замість обох. Після видалення WIP-префікса й додавання правильного trailer очікуйте код 0. У policy `warn` та глобальному `audit` застосування відмов відрізняється від таблиці для `enforce`.

[Довідник COMMITPOLICY](../../policies/commitpolicy.md)
