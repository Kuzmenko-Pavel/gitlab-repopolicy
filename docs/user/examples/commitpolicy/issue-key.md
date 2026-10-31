[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](issue-key.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](issue-key.en.md)

# Вільний header із ключем задачі

Команда пише `PAY-123 Round refunds to cents`. Вона не використовує типи `feat` і `fix`, але хоче обов'язковий ключ задачі на початку першого рядка.

## Правило проєкту

Збережіть це як `COMMITPOLICY` у корені репозиторію й перевірте `gitlab-repopolicy validate -f COMMITPOLICY` перед merge в основну гілку.

```yaml
# examples/free-format/COMMITPOLICY
# A free-form header that starts with an issue key, on the default branch only.
version: 1

rules:
  issue-key:
    match: default
    except:
      merge_commit: true
    checks:
      header-format:
        free: { level: error, value: optional }
      header-max-length:
        limit: { level: error, value: 72 }
      subject-trailing-dot:
        no-dot: { level: error, value: forbidden }
      pattern:
        issue-key: { level: error, location: header, value: '/^[A-Z][A-Z0-9]+-[0-9]+ \S/' }
    message: Start the header with the issue key, for example PAY-123.
```

`header-format` з `value: optional` прямо показує, що формат Conventional Commits тут не вимагається. Header `PAY-123 ...` на частини не розбирається, тому subject - увесь header. Регулярний вираз із `^` вимагає ключ на початку, а не в довільному місці. `match: default` залишає робочі гілки вільними; стандартний merge-коміт має власний виняток.

<!-- check: lint-commit examples/free-format/COMMITPOLICY -->
| Повідомлення | Результат | Чому |
| --- | --- | --- |
| `PAY-123 Round refunds to cents` | проходить | ключ задачі на початку |
| `Round refunds to cents` | відмова | `pattern issue-key`: немає ключа на початку header |
| `pay-123 round refunds` | відмова | `pattern issue-key`: ключ задачі великими літерами |
| `PAY-123 Round refunds to cents.` | відмова | `subject-trailing-dot`: header не розбирається, і subject - це весь header |

Перевірте кожне повідомлення через `gitlab-repopolicy lint-commit -m "PAY-123 Round refunds to cents"`, підставляючи рядок таблиці. Коректний рядок дає код 0, порушення - 1. У глобальному `enforce` і policy `enforce` такі помилки блокують відповідні вхідні коміти основної гілки.

`type-enum` і `scope-enum` для header `PAY-123 ...` не виконуються: type і scope такого header невідомі. Якщо ключ задачі має бути в trailer, змініть `location` і додайте `trailer-required` з `value: required` ([пошук за шаблоном](patterns.md)), а не очікуйте, що `header` шукатиме в усьому повідомленні.

[Довідник COMMITPOLICY](../../policies/commitpolicy.md)
