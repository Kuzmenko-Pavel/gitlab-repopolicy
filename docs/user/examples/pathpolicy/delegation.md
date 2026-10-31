[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](delegation.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](delegation.en.md)

# Делегування керування політиками

alice керує PATHPOLICY, dev-lead відповідає за формат комітів, release-lead - за ключі та правила тегів. Потрібно дати власникам змогу змінювати свої policy-файли, не надаючи їм права змінювати саму PATHPOLICY.

## Призначення власників

Збережіть цей файл як `PATHPOLICY` у корені основної гілки. Усі username мають існувати; alice має бути активним Maintainer або Owner. Перед першою активацією запустіть `gitlab-repopolicy validate -f PATHPOLICY`.

```yaml
# examples/delegation/PATHPOLICY
# PATHPOLICY stays with alice. The other policy files and the GitLab agent
# configuration are delegated to their owners by path rules.
version: 1

admins: [alice]

rules:
  commit-policy:
    match: all
    paths: ["COMMITPOLICY"]
    allow: [dev-lead, alice]
    message: COMMITPOLICY is changed by dev-lead or alice.

  tag-policy:
    match: all
    paths: ["TAGPOLICY"]
    allow: [release-lead, alice]
    message: TAGPOLICY is changed by release-lead or alice.

  kubernetes-agents:
    match: default
    check_history: incoming
    paths: [".gitlab/agents/**"]
    allow: [alice]
    message: >-
      The agent configuration is read only from the default branch.
      Change it in your branch and ask alice to merge it.
```

`admins` захищає саму PATHPOLICY. Інші policy-файли не мають автоматичного власника: доступ задають path-правила. Тут `match: all` захищає і робочі чернетки, і видалення файлів. Правило агента незалежне від делегування.

| Хто | Що робить | Результат | Чому |
| --- | --- | --- | --- |
| dev-lead | змінює або видаляє `COMMITPOLICY` | дозволено | dev-lead є в `allow` правила `commit-policy` |
| bob | змінює або видаляє `COMMITPOLICY` | відмова | bob немає в `allow` |
| dev-lead | додає собі правило в `PATHPOLICY` | відмова | `PATHPOLICY` змінюють лише `admins` |

Без такого правила файл політики може змінити будь-хто з правом push. Hook приймає таку версію, але попереджає про це.

Дії в таблиці передбачають глобальний `enforce`, штатний доступ GitLab та валідність нової версії. Навіть dev-lead не може додати невалідну COMMITPOLICY: авторизація й приймання файлу є окремими перевірками.

## Розширення та перевірка

Для BRANCHPOLICY додайте повне правило такого самого виду зі шляхом `BRANCHPOLICY` й потрібним `allow`. Не очікуйте, що `admins: [alice]` автоматично захищає інші policy-файли.

На тестовому проєкті dev-lead змінює валідну COMMITPOLICY, потім bob пробує ту саму зміну. Перший push має пройти, другий отримати `protected_path_violation`. Спроба dev-lead змінити PATHPOLICY дає `policy_admin_violation`.

[Довідник PATHPOLICY](../../policies/pathpolicy.md)
