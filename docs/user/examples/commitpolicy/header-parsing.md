[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](header-parsing.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](header-parsing.en.md)

# Помилки розбору Conventional Commits

Header може не розбиратися ще до перевірок довжини, типів чи регістру. Тоді перевірки частин header, наприклад `type-enum`, не виконуються, а помилку показує `header-format`: спершу треба записати правильний перший рядок.

## Мінімальне правило

Збережіть як `COMMITPOLICY` у поточному каталозі й виконайте `gitlab-repopolicy validate -f COMMITPOLICY`:

```yaml
version: 1
rules:
  r:
    match: all
    checks:
      header-format:
        conventional: {level: error, value: required}
    message: Use a Conventional Commits header.
```

`fix: round refunds` і `fix(billing): round refunds` є коректними header. Scope у дужках необов'язковий. Після двокрапки потрібен рівно один пробіл.

<!-- check: lint-commit header-format=required -->
| Header | Помилка `lint-commit` |
| --- | --- |
| `fix round refunds` | `header-format conventional: expected "type(scope): subject"` |
| `fix:round refunds` | `header-format conventional: expected "type(scope): subject", a space must follow ":"` |
| `fix:  round refunds` | `header-format conventional: expected "type(scope): subject", exactly one space must follow ":"` |
| `fix(billing: round refunds` | `header-format conventional: expected "type(scope): subject", the scope parenthesis is not closed` |
| `fix(): round refunds` | `header-format conventional: the scope in parentheses is empty` |
| `fix:` | `header-format conventional: expected "type(scope): subject", the subject is empty` |
| `fix (billing): round refunds` | `header-format conventional: expected "type(scope): subject"` |
| `[fix] round refunds` | `header-format conventional: expected "type(scope): subject"` |

Підказка після коми з'являється, коли hook розуміє, що саме не так. Без підказки header зовсім не схожий на `type(scope): subject`: пробіл перед дужкою, квадратні дужки, ключ задачі замість type.

Header - завжди перший рядок. `fix: round\nbody right after` розбирається без помилки: header `fix: round`, body `body right after`. Чи потрібен порожній рядок між ними, задає окрема перевірка `body-leading-blank-lines` ([порівняння повідомлень](message-results.md)), а не розбір header.

Для перевірки запишіть один рядок таблиці в `message.txt` зі справжніми переносами й запустіть `gitlab-repopolicy lint-commit -m "$(cat message.txt)"`. Таблиця містить очікувані частини діагностики; кожен рядок дає код 1. Після виправлення до `fix: round refunds` команда має завершитися з кодом 0 без порушень.

`header-format` з `value: optional` дозволяє інший формат header, але це вибір стандарту проєкту ([ключ задачі](issue-key.md)), а не спосіб обійти помилку конкретного Conventional Commits повідомлення. Правило без `header-format` неправильний header не відхиляє: hook розбирає header для інших перевірок, а відмову за формат дає лише `header-format` з `value: required`. `header-format` з `level: warn` робить неправильний header попередженням, а решта перевірок правила лишаються помилками.

[Довідник COMMITPOLICY](../../policies/commitpolicy.md)
