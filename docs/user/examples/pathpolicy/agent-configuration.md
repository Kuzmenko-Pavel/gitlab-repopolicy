[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](agent-configuration.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](agent-configuration.en.md)

# Конфігурація GitLab Agent

Будь-хто з правом push може запропонувати конфігурацію `.gitlab/agents/**` у своїй гілці. Прийняти її в основну гілку може лише alice, оскільки саме звідти GitLab читає конфігурацію агента.

## Файл і межа перевірки

Додайте в корінь репозиторію цей `PATHPOLICY`, перевірте `gitlab-repopolicy validate -f PATHPOLICY` та злийте першу версію від Maintainer або Owner. Усі username мають існувати, alice має бути активним Maintainer або Owner. Правила `commit-policy` і `tag-policy` у тому самому файлі делегують керування іншими політиками; для сценарію агента діє `kubernetes-agents`.

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
    paths: [".gitlab/agents/**"]
    allow: [alice]
    message: >-
      The agent configuration is read only from the default branch.
      Change it in your branch and ask alice to merge it.
```

| Хто | Що робить | Результат | Чому |
| --- | --- | --- | --- |
| bob | змінює `.gitlab/agents/prod/config.yaml` у `feature` | дозволено | правило діє лише в default branch |
| bob | мержить `feature` у `main` | відмова | зміну в `main` вносить bob |
| alice | мержить `feature` у `main` | дозволено | alice є в `allow` |

У таблиці `main` є основною гілкою, глобальний режим - `enforce`; штатна авторизація GitLab та інші політики мають дозволяти дію. Під час merge перевіряється користувач, який приймає зміну, незалежно від автора комітів.

## Як перевірити результат

bob надсилає `feature` зі зміною `.gitlab/agents/prod/config.yaml`. Спроба bob злити її в `main` має отримати `protected_path_violation`; alice може прийняти той самий merge. Зміна правила та конфігурації одним push ще перевірятиметься за старою політикою.

Не застосовуйте цей вибір `match` до CI-файлів усіх гілок. Тут `default` доречний саме через те, звідки агент читає конфігурацію. Якщо потрібне блокування навіть чернеток, використайте `all`, усвідомлюючи, що bob уже не зможе запушити пропозицію.

[Довідник PATHPOLICY](../../policies/pathpolicy.md)
