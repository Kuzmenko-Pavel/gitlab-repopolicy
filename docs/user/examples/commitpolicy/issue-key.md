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
    parser: free
    except:
      merge_commit: true
    checks:
      header-max-length: { level: error, value: 72 }
      subject-full-stop: { level: error, value: forbid }
      pattern:           { level: error, location: header, value: '/^[A-Z][A-Z0-9]+-[0-9]+ \S/' }
    message: Start the header with the issue key, for example PAY-123.
```

`parser: free` трактує весь header як subject. Регулярний вираз із `^` вимагає ключ на початку, а не в довільному місці. `match: default` залишає робочі гілки вільними; стандартний merge-коміт має власний виняток.

<!-- check: lint-commit examples/free-format/COMMITPOLICY -->
| Повідомлення | Результат | Чому |
| --- | --- | --- |
| `PAY-123 Round refunds to cents` | проходить | ключ задачі на початку |
| `Round refunds to cents` | відмова | `pattern`: немає ключа на початку header |
| `pay-123 round refunds` | відмова | `pattern`: ключ задачі великими літерами |
| `PAY-123 Round refunds to cents.` | відмова | `subject-full-stop`: з `parser: free` subject - це весь header |

Перевірте кожне повідомлення через `gitlab-repopolicy lint-commit -m "PAY-123 Round refunds to cents"`, підставляючи рядок таблиці. Коректний рядок дає код 0, порушення - 1. У глобальному `enforce` і policy `enforce` такі помилки блокують відповідні вхідні коміти основної гілки.

Не додавайте `type-enum` чи `scope-enum` до `parser: free`: ці перевірки потребують `conventional`. Якщо ключ задачі має бути в trailer, змініть `location`, а не очікуйте, що `header` шукатиме в усьому повідомленні.

[Довідник COMMITPOLICY](../../policies/commitpolicy.md)
