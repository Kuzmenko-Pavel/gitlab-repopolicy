[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](exception-vs-deny.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](exception-vs-deny.en.md)

# Виняток у match не є забороною

Потрібно заборонити `tmp/...`. Виняток у `match` робочого правила здається зручним, але лише вилучає ім'я з класу. Без іншого збігу гілка проходить без перевірки.

## Чому цей файл не забороняє tmp/x

Збережіть як `BRANCHPOLICY` у поточному каталозі:

```yaml
version: 1
rules:
  work:
    match: ['/^[a-z0-9.]+\/[a-z0-9._-]+$/', '!/^tmp\//']
    priority: 20
    message: Work branches are <prefix>/<name>.
```

Файл валідний. `gitlab-repopolicy validate --branch tmp/x` покаже `branch tmp/x: no rule matches, the name is not checked`. Це не помилка parser: правило не збіглося саме так, як написано.

## Явна заборона

Замініть файл повною конфігурацією:

```yaml
version: 1
rules:
  no-tmp:
    match: ['/^tmp\//']
    priority: 10
    deny: true
    message: Temporary branches are not pushed; keep them local.
  work:
    match: ['/^[a-z0-9.]+\/[a-z0-9._-]+$/']
    priority: 20
    message: Work branches are <prefix>/<name>.
```

Правило `no-tmp` із меншим `priority` перехоплює `tmp/x` до `work` і повертає `branch_name_violation` у глобальному та policy `enforce`. `bob/fix` потрапляє в `work`; `fix-login` не збігається й лишається неперевіреним.

Перевірте файл через `gitlab-repopolicy validate -f BRANCHPOLICY`, подивіться `validate --branch tmp/x`, злийте файл в основну гілку й відтворіть контрольне створення `tmp/x` у тестовому проєкті. Не очікуйте, що вже наявна гілка буде видалена або перестане приймати коміти: BRANCHPOLICY перевіряє лише створення.

Код 0 від `validate --branch` означає успіх запиту, навіть коли в тексті `result: denied`. Для перевірки остаточного рішення використайте `test` або справжній push із належними правами GitLab.

[Довідник BRANCHPOLICY](../../policies/branchpolicy.md)
