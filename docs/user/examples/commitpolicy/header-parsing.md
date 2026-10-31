[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](header-parsing.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](header-parsing.en.md)

# Помилки розбору Conventional Commits

Header може не розбиратися ще до перевірок довжини, типів чи регістру. У такому випадку додавання `type-enum` нічого не виправить: спершу треба записати правильний перший рядок.

## Мінімальне правило

Збережіть як `COMMITPOLICY` у поточному каталозі й виконайте `gitlab-repopolicy validate -f COMMITPOLICY`:

```yaml
version: 1
rules:
  r:
    match: all
    parser: conventional
    message: Use a Conventional Commits header.
```

`fix: round refunds` і `fix(billing): round refunds` є коректними header. Scope у дужках необов'язковий. Після двокрапки потрібен рівно один пробіл, а перед body - порожній рядок.

<!-- check: lint-commit parser=conventional -->
| Header | Помилка `lint-commit` |
| --- | --- |
| `fix round refunds` | `header: expected "type(scope): subject"` |
| `fix:round refunds` | `header: expected "type(scope): subject", a space must follow ":"` |
| `fix:  round refunds` | `header: expected "type(scope): subject", exactly one space must follow ":"` |
| `fix(billing: round refunds` | `header: expected "type(scope): subject", the scope parenthesis is not closed` |
| `fix(): round refunds` | `header: the scope in parentheses is empty` |
| `fix:` | `header: expected "type(scope): subject", the subject is empty` |
| `fix (billing): round refunds` | `header: expected "type(scope): subject"` |
| `[fix] round refunds` | `header: expected "type(scope): subject"` |
| `fix: round\nbody right after` | `the body must be separated from the header by an empty line` |

Підказка після коми з'являється, коли hook розуміє, що саме не так. Без підказки header зовсім не схожий на `type(scope): subject`: пробіл перед дужкою, квадратні дужки, ключ задачі замість type.

Для перевірки запишіть один рядок таблиці в `message.txt` зі справжніми переносами й запустіть `gitlab-repopolicy lint-commit -m "$(cat message.txt)"`. Таблиця містить очікувані частини діагностики; кожен рядок дає код 1. Після виправлення до `fix: round refunds` команда має завершитися з кодом 0 без порушень.

`parser: free` дозволяє інший формат header, але це вибір стандарту проєкту, а не спосіб обійти помилку конкретного Conventional Commits повідомлення. Правило без `checks` усе одно перевіряє синтаксис parser.

[Довідник COMMITPOLICY](../../policies/commitpolicy.md)
